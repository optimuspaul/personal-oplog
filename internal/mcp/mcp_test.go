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
	if res.IsError {
		t.Fatalf("CallTool(%s) returned error: %s", name, resultText(res))
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

// startTask creates and starts a task, returning its id.
func startTask(t *testing.T, s *mcpsdk.ClientSession, name string) string {
	t.Helper()
	res := call(t, s, "oplog_log", map[string]any{"task": name, "action": "start"})
	var out struct {
		ID string `json:"id"`
	}
	decodeStructured(t, res, &out)
	if out.ID == "" {
		t.Fatal("start returned no task id")
	}
	return out.ID
}

func TestListToolsExposesFullSurface(t *testing.T) {
	s := newClient(t)
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	want := map[string]bool{
		"oplog_log": false, "oplog_focus": false, "oplog_tasks": false,
		"oplog_threads": false, "oplog_context": false,
		"oplog_recent": false, "oplog_search": false,
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
	if len(res.Tools) != len(want) {
		t.Errorf("expected exactly %d tools, got %d", len(want), len(res.Tools))
	}
}

func TestStartAndFocusLifecycle(t *testing.T) {
	s := newClient(t)

	// No focus initially.
	res := call(t, s, "oplog_focus", map[string]any{})
	var focus struct {
		Active bool `json:"active"`
	}
	decodeStructured(t, res, &focus)
	if focus.Active {
		t.Error("expected no active focus initially")
	}

	startTask(t, s, "OAuth")

	res = call(t, s, "oplog_focus", map[string]any{})
	var focus2 struct {
		Active bool `json:"active"`
		Task   struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"task"`
	}
	decodeStructured(t, res, &focus2)
	if !focus2.Active || focus2.Task.Name != "OAuth" || focus2.Task.Status != "active" {
		t.Errorf("unexpected focus: %+v", focus2)
	}
}

func TestLogMissingTaskIsError(t *testing.T) {
	s := newClient(t)
	res, err := s.CallTool(context.Background(),
		&mcpsdk.CallToolParams{Name: "oplog_log", Arguments: map[string]any{"action": "start"}})
	if err == nil && !res.IsError {
		t.Error("expected error when logging without a task reference")
	}
}

func TestTasksFuzzyResolution(t *testing.T) {
	s := newClient(t)
	startTask(t, s, "monkey task")
	startTask(t, s, "banana split")

	res := call(t, s, "oplog_tasks", map[string]any{"query": "monkey"})
	var out struct {
		Count int `json:"count"`
		Tasks []struct {
			Name string `json:"name"`
		} `json:"tasks"`
	}
	decodeStructured(t, res, &out)
	if out.Count != 1 || out.Tasks[0].Name != "monkey task" {
		t.Errorf("fuzzy resolution failed: %+v", out)
	}
}

func TestCheckpointAndContextRoundTrip(t *testing.T) {
	s := newClient(t)
	id := startTask(t, s, "OAuth")

	call(t, s, "oplog_log", map[string]any{
		"task":        id,
		"action":      "checkpoint",
		"message":     "Password grant passes. Client credentials failing.",
		"next_action": "Inspect audience parameter.",
	})

	res := call(t, s, "oplog_context", map[string]any{})
	if !strings.Contains(resultText(res), "Inspect audience parameter.") {
		t.Errorf("context text missing next action: %q", resultText(res))
	}
	var out struct {
		LatestCheckpoint struct {
			NextAction string `json:"next_action"`
		} `json:"latest_checkpoint"`
	}
	decodeStructured(t, res, &out)
	if out.LatestCheckpoint.NextAction != "Inspect audience parameter." {
		t.Errorf("unexpected context output: %+v", out)
	}
}

func TestParkSurfacesAsLooseThread(t *testing.T) {
	s := newClient(t)
	id := startTask(t, s, "RPV query")
	call(t, s, "oplog_log", map[string]any{"task": id, "action": "park"})

	res := call(t, s, "oplog_threads", map[string]any{})
	var out struct {
		Count   int `json:"count"`
		Threads []struct {
			Task struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"task"`
		} `json:"threads"`
	}
	decodeStructured(t, res, &out)
	if out.Count != 1 || out.Threads[0].Task.Name != "RPV query" || out.Threads[0].Task.Status != "parked" {
		t.Errorf("expected RPV query as a parked loose thread, got %+v", out)
	}
}

func TestCompleteRemovesFromFocus(t *testing.T) {
	s := newClient(t)
	id := startTask(t, s, "OAuth")

	call(t, s, "oplog_log", map[string]any{"task": id, "action": "complete", "message": "done"})

	res := call(t, s, "oplog_focus", map[string]any{})
	var out struct {
		Active bool `json:"active"`
	}
	decodeStructured(t, res, &out)
	if out.Active {
		t.Error("expected no focus after completing the active task")
	}
}

func TestLogUnknownTaskIsError(t *testing.T) {
	s := newClient(t)
	// A non-start action on a task that does not exist must error.
	res := call0(t, s, "oplog_log", map[string]any{"task": "ghost", "action": "park"})
	if !res.IsError {
		t.Error("expected tool error when parking an unknown task")
	}
}

func TestSearchAndRecent(t *testing.T) {
	s := newClient(t)
	id := startTask(t, s, "OAuth")
	call(t, s, "oplog_log", map[string]any{"task": id, "action": "checkpoint", "message": "keep me"})
	call(t, s, "oplog_log", map[string]any{"task": id, "action": "note", "message": "a note"})

	res := call(t, s, "oplog_search", map[string]any{"action": "checkpoint"})
	var search struct {
		Count  int `json:"count"`
		Events []struct {
			Message string `json:"message"`
		} `json:"events"`
	}
	decodeStructured(t, res, &search)
	if search.Count != 1 || search.Events[0].Message != "keep me" {
		t.Errorf("unexpected search output: %+v", search)
	}

	res = call(t, s, "oplog_recent", map[string]any{"limit": 2})
	var recent struct {
		Count int `json:"count"`
	}
	decodeStructured(t, res, &recent)
	if recent.Count != 2 {
		t.Errorf("expected 2 recent events, got %d", recent.Count)
	}
}

// call0 issues a tool call without failing the test on a tool error, for the
// cases that assert on IsError.
func call0(t *testing.T, s *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}
