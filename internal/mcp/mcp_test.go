package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/mcp"
	"github.com/optimuspaul/personal-oplog/internal/persistence/jsonl"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// newClient stands up an in-process Oplog server backed by a temp store and
// returns a connected client session.
func newClient(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()

	store, err := jsonl.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	server := mcp.NewServer(service.New(store), "test")

	serverT, clientT := mcpsdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// The server must be connected before the client; Run blocks for the
	// lifetime of the connection, so it runs in the background.
	go func() { _ = server.Run(ctx, serverT) }()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func call(t *testing.T, s *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func resultText(res *mcpsdk.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// decodeStructured re-decodes the structured content into target.
func decodeStructured(t *testing.T, res *mcpsdk.CallToolResult, target any) {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatal("expected structured content, got nil")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

func TestListToolsExposesAllEight(t *testing.T) {
	s := newClient(t)
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := map[string]bool{
		"oplog_start_work":    false,
		"oplog_log":           false,
		"oplog_checkpoint":    false,
		"oplog_interrupt":     false,
		"oplog_resume":        false,
		"oplog_current_focus": false,
		"oplog_search":        false,
		"oplog_end_work":      false,
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestStartWorkTool(t *testing.T) {
	s := newClient(t)
	res := call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "DERS") {
		t.Errorf("text missing project: %q", resultText(res))
	}

	var out struct {
		Active  bool   `json:"active"`
		Project string `json:"project"`
		Task    string `json:"task"`
	}
	decodeStructured(t, res, &out)
	if !out.Active || out.Project != "DERS" || out.Task != "OAuth" {
		t.Errorf("unexpected focus output: %+v", out)
	}
}

func TestStartWorkMissingFieldIsError(t *testing.T) {
	s := newClient(t)
	res, err := s.CallTool(context.Background(),
		&mcpsdk.CallToolParams{Name: "oplog_start_work", Arguments: map[string]any{"task": "OAuth"}})
	// Missing required input is rejected — either as a protocol error or a
	// tool error result; both are acceptable, a silent success is not.
	if err == nil && !res.IsError {
		t.Error("expected error for missing project")
	}
}

func TestCheckpointAndResumeRoundTrip(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})

	cp := call(t, s, "oplog_checkpoint", map[string]any{
		"summary":        "Password grant passes. Client credentials failing.",
		"next_action":    "Inspect audience parameter.",
		"open_questions": []string{"Is hey-api sending audience correctly?"},
	})
	if cp.IsError {
		t.Fatalf("checkpoint error: %s", resultText(cp))
	}

	res := call(t, s, "oplog_resume", map[string]any{"project": "DERS"})
	text := resultText(res)
	if !strings.Contains(text, "Inspect audience parameter.") {
		t.Errorf("resume text missing next action: %q", text)
	}

	var out struct {
		Found      bool   `json:"found"`
		NextAction string `json:"next_action"`
	}
	decodeStructured(t, res, &out)
	if !out.Found || out.NextAction != "Inspect audience parameter." {
		t.Errorf("unexpected resume output: %+v", out)
	}
}

func TestResumeFallsBackToLastEntry(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})
	call(t, s, "oplog_log", map[string]any{"text": "investigated scopes"})
	call(t, s, "oplog_interrupt", map[string]any{"reason": "prod issue"})

	// No checkpoint exists; resume should fall back to the interrupt.
	res := call(t, s, "oplog_resume", map[string]any{"project": "DERS"})
	if res.IsError {
		t.Fatalf("resume error: %s", resultText(res))
	}
	var out struct {
		Found          bool   `json:"found"`
		FromCheckpoint bool   `json:"from_checkpoint"`
		Type           string `json:"type"`
		Summary        string `json:"summary"`
	}
	decodeStructured(t, res, &out)
	if !out.Found || out.FromCheckpoint || out.Type != "interrupt" || out.Summary != "prod issue" {
		t.Errorf("expected fallback to interrupt entry, got %+v", out)
	}
	if !strings.Contains(resultText(res), "last activity") {
		t.Errorf("expected fallback note in text, got: %q", resultText(res))
	}
}

func TestResumeNoCheckpoint(t *testing.T) {
	s := newClient(t)
	res := call(t, s, "oplog_resume", map[string]any{"project": "NOPE"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", resultText(res))
	}
	var out struct {
		Found bool `json:"found"`
	}
	decodeStructured(t, res, &out)
	if out.Found {
		t.Error("expected found=false")
	}
}

func TestCurrentFocusLifecycle(t *testing.T) {
	s := newClient(t)

	// No focus yet.
	res := call(t, s, "oplog_current_focus", map[string]any{})
	var out struct {
		Active bool `json:"active"`
	}
	decodeStructured(t, res, &out)
	if out.Active {
		t.Error("expected no active focus initially")
	}

	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})

	res = call(t, s, "oplog_current_focus", map[string]any{})
	decodeStructured(t, res, &out)
	if !out.Active {
		t.Error("expected active focus after start_work")
	}
}

func TestLogFallsBackToFocus(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})

	res := call(t, s, "oplog_log", map[string]any{"text": "Investigated Auth0 scopes."})
	if res.IsError {
		t.Fatalf("log error: %s", resultText(res))
	}
	var out struct {
		Project string `json:"project"`
		Task    string `json:"task"`
		Type    string `json:"type"`
	}
	decodeStructured(t, res, &out)
	if out.Project != "DERS" || out.Task != "OAuth" || out.Type != "log" {
		t.Errorf("log did not inherit focus: %+v", out)
	}
}

func TestSearchTool(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})
	call(t, s, "oplog_checkpoint", map[string]any{"summary": "keep me"})
	call(t, s, "oplog_log", map[string]any{"text": "a note"})

	res := call(t, s, "oplog_search", map[string]any{"type": "checkpoint"})
	if res.IsError {
		t.Fatalf("search error: %s", resultText(res))
	}
	var out struct {
		Count   int `json:"count"`
		Entries []struct {
			Summary string `json:"summary"`
		} `json:"entries"`
	}
	decodeStructured(t, res, &out)
	if out.Count != 1 || len(out.Entries) != 1 || out.Entries[0].Summary != "keep me" {
		t.Errorf("unexpected search output: %+v", out)
	}
}

func TestRecentTool(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})
	call(t, s, "oplog_log", map[string]any{"text": "first"})
	call(t, s, "oplog_checkpoint", map[string]any{"summary": "second"})
	call(t, s, "oplog_log", map[string]any{"text": "third"})

	// limit=2 → the two newest entries, newest first.
	res := call(t, s, "oplog_recent", map[string]any{"limit": 2})
	if res.IsError {
		t.Fatalf("recent error: %s", resultText(res))
	}
	var out struct {
		Count   int `json:"count"`
		Entries []struct {
			Type    string `json:"type"`
			Summary string `json:"summary"`
		} `json:"entries"`
	}
	decodeStructured(t, res, &out)
	if out.Count != 2 || len(out.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", out.Count)
	}
	if out.Entries[0].Summary != "third" {
		t.Errorf("newest entry = %q, want %q", out.Entries[0].Summary, "third")
	}

	// type filter restricts to checkpoints.
	res = call(t, s, "oplog_recent", map[string]any{"limit": 10, "type": "checkpoint"})
	decodeStructured(t, res, &out)
	if out.Count != 1 || out.Entries[0].Type != "checkpoint" {
		t.Errorf("type filter failed: %+v", out)
	}
}

func TestInterruptClearsFocus(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})

	res := call(t, s, "oplog_interrupt", map[string]any{"reason": "Production issue"})
	if res.IsError {
		t.Fatalf("interrupt error: %s", resultText(res))
	}

	res = call(t, s, "oplog_current_focus", map[string]any{})
	var out struct {
		Active bool `json:"active"`
	}
	decodeStructured(t, res, &out)
	if out.Active {
		t.Error("expected focus cleared after interrupt")
	}
}

func TestInterruptWithoutFocusIsError(t *testing.T) {
	s := newClient(t)
	res := call(t, s, "oplog_interrupt", map[string]any{"reason": "x"})
	if !res.IsError {
		t.Error("expected tool error when interrupting with no active focus")
	}
}

func TestEndWorkClearsFocus(t *testing.T) {
	s := newClient(t)
	call(t, s, "oplog_start_work", map[string]any{"project": "DERS", "task": "OAuth"})

	res := call(t, s, "oplog_end_work", map[string]any{"summary": "All tests passing."})
	if res.IsError {
		t.Fatalf("end_work error: %s", resultText(res))
	}

	res = call(t, s, "oplog_current_focus", map[string]any{})
	var out struct {
		Active bool `json:"active"`
	}
	decodeStructured(t, res, &out)
	if out.Active {
		t.Error("expected focus cleared after end_work")
	}
}
