package jsonl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// baseTime is a fixed instant so tests are deterministic across time zones.
var baseTime = time.Date(2026, 6, 23, 21, 15, 0, 0, time.UTC)

func mkEvent(id string, offset time.Duration, mutate func(*types.Event)) types.Event {
	e := types.Event{
		ID:        id,
		Timestamp: baseTime.Add(offset),
		Type:      types.EventNote,
		TaskID:    "task-1",
		Text:      "a note",
	}
	if mutate != nil {
		mutate(&e)
	}
	return e
}

func appendAll(t *testing.T, s *Store, events ...types.Event) {
	t.Helper()
	for _, e := range events {
		if err := s.AppendEvent(context.Background(), e); err != nil {
			t.Fatalf("AppendEvent(%s): %v", e.ID, err)
		}
	}
}

func ids(events []types.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
}

func TestNewStoreCreatesDir(t *testing.T) {
	dir := t.TempDir() + "/nested/store"
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("expected store dir to exist: %v", err)
	}
}

func TestNewStoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("second NewStore on existing dir: %v", err)
	}
}

func TestAppendAndListRoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := mkEvent("01", 0, func(e *types.Event) {
		e.Type = types.EventCheckpoint
		e.Text = ""
		e.Summary = "Password grant passes."
		e.NextAction = "Inspect audience parameter."
		e.OpenQuestions = []string{"Is hey-api sending audience correctly?"}
		e.Files = []string{"auth_test.go", "oauth_test.go"}
		e.Tags = []string{"oauth", "auth0"}
	})
	appendAll(t, s, want)

	got, err := s.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	if !got[0].Timestamp.Equal(want.Timestamp) {
		t.Errorf("timestamp: got %v, want %v", got[0].Timestamp, want.Timestamp)
	}
	if got[0].Summary != want.Summary || got[0].NextAction != want.NextAction {
		t.Errorf("summary/next_action mismatch: %+v", got[0])
	}
	if len(got[0].OpenQuestions) != 1 || len(got[0].Files) != 2 || len(got[0].Tags) != 2 {
		t.Errorf("slice fields not preserved: %+v", got[0])
	}
}

func TestListEventsMostRecentFirst(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEvent("middle", 1*time.Hour, nil),
		mkEvent("oldest", 0, nil),
		mkEvent("newest", 2*time.Hour, nil),
	)

	got, err := s.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	want := []string{"newest", "middle", "oldest"}
	if g := ids(got); !equalStrings(g, want) {
		t.Errorf("order: got %v, want %v", g, want)
	}
}

func TestListEventsEmptyWhenNoLog(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents on empty store: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d events, want 0", len(got))
	}
}

func TestListEventsFilters(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEvent("a", 0, func(e *types.Event) {
			e.TaskID = "t1"
			e.Type = types.EventCheckpoint
			e.Text = ""
			e.Tags = []string{"oauth", "auth0"}
			e.Summary = "Client credentials failing."
		}),
		mkEvent("b", 1*time.Hour, func(e *types.Event) {
			e.TaskID = "t2"
			e.Type = types.EventNote
			e.Tags = []string{"oauth"}
			e.Text = "Refactored invoices."
		}),
		mkEvent("c", 2*time.Hour, func(e *types.Event) {
			e.TaskID = "t1"
			e.Type = types.EventCheckpoint
			e.Text = ""
			e.Tags = []string{"auth0"}
			e.Summary = "Unrelated."
		}),
	)

	tests := []struct {
		name   string
		filter types.EventFilter
		want   []string
	}{
		{"task", types.EventFilter{TaskID: "t1"}, []string{"c", "a"}},
		{"type", types.EventFilter{Types: []types.EventType{types.EventCheckpoint}}, []string{"c", "a"}},
		{"tags AND", types.EventFilter{Tags: []string{"oauth", "auth0"}}, []string{"a"}},
		{"single tag", types.EventFilter{Tags: []string{"oauth"}}, []string{"b", "a"}},
		{"text case-insensitive", types.EventFilter{Text: "CREDENTIALS"}, []string{"a"}},
		{"text matches tag", types.EventFilter{Text: "auth0"}, []string{"c", "a"}},
		{"task+type", types.EventFilter{TaskID: "t1", Types: []types.EventType{types.EventCheckpoint}}, []string{"c", "a"}},
		{"no match", types.EventFilter{TaskID: "NOPE"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListEvents(context.Background(), tt.filter)
			if err != nil {
				t.Fatalf("ListEvents: %v", err)
			}
			if g := ids(got); !equalStrings(g, tt.want) {
				t.Errorf("got %v, want %v", g, tt.want)
			}
		})
	}
}

func TestListEventsSinceUntil(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEvent("t0", 0, nil),
		mkEvent("t1", 1*time.Hour, nil),
		mkEvent("t2", 2*time.Hour, nil),
	)

	since := baseTime.Add(1 * time.Hour)
	until := baseTime.Add(1 * time.Hour)

	got, err := s.ListEvents(context.Background(), types.EventFilter{Since: &since})
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t2", "t1"}) {
		t.Errorf("since: got %v", g)
	}

	got, err = s.ListEvents(context.Background(), types.EventFilter{Until: &until})
	if err != nil {
		t.Fatalf("until: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t1", "t0"}) {
		t.Errorf("until: got %v", g)
	}
}

func TestListEventsLimitKeepsMostRecent(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEvent("t0", 0, nil),
		mkEvent("t1", 1*time.Hour, nil),
		mkEvent("t2", 2*time.Hour, nil),
	)

	got, err := s.ListEvents(context.Background(), types.EventFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t2", "t1"}) {
		t.Errorf("limit: got %v, want [t2 t1]", g)
	}
}

func TestListEventsIgnoresBlankLines(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s, mkEvent("a", 0, nil))

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("\n   \n"); err != nil {
		t.Fatalf("write blank lines: %v", err)
	}
	f.Close()

	got, err := s.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d events, want 1", len(got))
	}
}

func TestListEventsCorruptLineErrors(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s, mkEvent("a", 0, nil))

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("{not valid json\n"); err != nil {
		t.Fatalf("write corrupt line: %v", err)
	}
	f.Close()

	if _, err := s.ListEvents(context.Background(), types.EventFilter{}); err == nil {
		t.Error("expected error on corrupt log line, got nil")
	}
}

func TestContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.AppendEvent(ctx, mkEvent("a", 0, nil)); err == nil {
		t.Error("AppendEvent: expected context error, got nil")
	}
	if _, err := s.ListEvents(ctx, types.EventFilter{}); err == nil {
		t.Error("ListEvents: expected context error, got nil")
	}
}

func TestAppendPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	appendAll(t, s1, mkEvent("a", 0, nil), mkEvent("b", 1*time.Hour, nil))

	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	got, err := s2.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"b", "a"}) {
		t.Errorf("after reopen: got %v, want [b a]", g)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
