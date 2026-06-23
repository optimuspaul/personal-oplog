package jsonl

import (
	"context"
	"os"
	"path/filepath"
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

func mkEntry(id string, offset time.Duration, mutate func(*types.Entry)) types.Entry {
	e := types.Entry{
		ID:        id,
		Timestamp: baseTime.Add(offset),
		Type:      types.EntryTypeLog,
		Project:   "DERS",
		Task:      "OAuth compliance tests",
	}
	if mutate != nil {
		mutate(&e)
	}
	return e
}

func appendAll(t *testing.T, s *Store, entries ...types.Entry) {
	t.Helper()
	for _, e := range entries {
		if err := s.AppendEntry(context.Background(), e); err != nil {
			t.Fatalf("AppendEntry(%s): %v", e.ID, err)
		}
	}
}

func ids(entries []types.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

func TestNewStoreCreatesLayout(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, sub := range []string{"projects", "sessions", "backups"} {
		info, err := os.Stat(filepath.Join(dir, sub))
		if err != nil {
			t.Errorf("expected %q to exist: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", sub)
		}
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
	want := mkEntry("01", 0, func(e *types.Entry) {
		e.Type = types.EntryTypeCheckpoint
		e.Summary = "Password grant passes."
		e.NextAction = "Inspect audience parameter."
		e.OpenQuestions = []string{"Is hey-api sending audience correctly?"}
		e.Files = []string{"auth_test.go", "oauth_test.go"}
		e.Tags = []string{"oauth", "auth0"}
	})
	appendAll(t, s, want)

	got, err := s.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if !got[0].Timestamp.Equal(want.Timestamp) {
		t.Errorf("timestamp: got %v, want %v", got[0].Timestamp, want.Timestamp)
	}
	got[0].Timestamp = want.Timestamp // compared above; normalize for DeepEqual-style checks
	if got[0].Summary != want.Summary || got[0].NextAction != want.NextAction {
		t.Errorf("summary/next_action mismatch: %+v", got[0])
	}
	if len(got[0].OpenQuestions) != 1 || len(got[0].Files) != 2 || len(got[0].Tags) != 2 {
		t.Errorf("slice fields not preserved: %+v", got[0])
	}
}

func TestListEntriesMostRecentFirst(t *testing.T) {
	s := newTestStore(t)
	// Append out of chronological order to prove sorting, not insertion order.
	appendAll(t, s,
		mkEntry("middle", 1*time.Hour, nil),
		mkEntry("oldest", 0, nil),
		mkEntry("newest", 2*time.Hour, nil),
	)

	got, err := s.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	want := []string{"newest", "middle", "oldest"}
	if g := ids(got); !equalStrings(g, want) {
		t.Errorf("order: got %v, want %v", g, want)
	}
}

func TestListEntriesEmptyWhenNoLog(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries on empty store: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

func TestListEntriesFilters(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEntry("a", 0, func(e *types.Entry) {
			e.Project = "DERS"
			e.Task = "OAuth"
			e.Type = types.EntryTypeCheckpoint
			e.Tags = []string{"oauth", "auth0"}
			e.Summary = "Client credentials failing."
		}),
		mkEntry("b", 1*time.Hour, func(e *types.Entry) {
			e.Project = "DERS"
			e.Task = "Billing"
			e.Type = types.EntryTypeLog
			e.Tags = []string{"oauth"}
			e.Summary = "Refactored invoices."
		}),
		mkEntry("c", 2*time.Hour, func(e *types.Entry) {
			e.Project = "OTHER"
			e.Task = "OAuth"
			e.Type = types.EntryTypeCheckpoint
			e.Tags = []string{"auth0"}
			e.Summary = "Unrelated."
		}),
	)

	tests := []struct {
		name   string
		filter types.EntryFilter
		want   []string
	}{
		{"project", types.EntryFilter{Project: "DERS"}, []string{"b", "a"}},
		{"task", types.EntryFilter{Task: "OAuth"}, []string{"c", "a"}},
		{"type", types.EntryFilter{Types: []types.EntryType{types.EntryTypeCheckpoint}}, []string{"c", "a"}},
		{"tags AND", types.EntryFilter{Tags: []string{"oauth", "auth0"}}, []string{"a"}},
		{"single tag", types.EntryFilter{Tags: []string{"oauth"}}, []string{"b", "a"}},
		{"text case-insensitive", types.EntryFilter{Text: "CREDENTIALS"}, []string{"a"}},
		{"text matches tag", types.EntryFilter{Text: "auth0"}, []string{"c", "a"}},
		{"project+type", types.EntryFilter{Project: "DERS", Types: []types.EntryType{types.EntryTypeCheckpoint}}, []string{"a"}},
		{"no match", types.EntryFilter{Project: "NOPE"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListEntries(context.Background(), tt.filter)
			if err != nil {
				t.Fatalf("ListEntries: %v", err)
			}
			if g := ids(got); !equalStrings(g, tt.want) {
				t.Errorf("got %v, want %v", g, tt.want)
			}
		})
	}
}

func TestListEntriesSinceUntil(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEntry("t0", 0, nil),
		mkEntry("t1", 1*time.Hour, nil),
		mkEntry("t2", 2*time.Hour, nil),
	)

	since := baseTime.Add(1 * time.Hour)
	until := baseTime.Add(1 * time.Hour)

	got, err := s.ListEntries(context.Background(), types.EntryFilter{Since: &since})
	if err != nil {
		t.Fatalf("since: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t2", "t1"}) {
		t.Errorf("since: got %v", g)
	}

	got, err = s.ListEntries(context.Background(), types.EntryFilter{Until: &until})
	if err != nil {
		t.Fatalf("until: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t1", "t0"}) {
		t.Errorf("until: got %v", g)
	}
}

func TestListEntriesLimitKeepsMostRecent(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s,
		mkEntry("t0", 0, nil),
		mkEntry("t1", 1*time.Hour, nil),
		mkEntry("t2", 2*time.Hour, nil),
	)

	got, err := s.ListEntries(context.Background(), types.EntryFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if g := ids(got); !equalStrings(g, []string{"t2", "t1"}) {
		t.Errorf("limit: got %v, want [t2 t1]", g)
	}
}

func TestListEntriesIgnoresBlankLines(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s, mkEntry("a", 0, nil))

	// Inject stray blank lines into the log to mimic manual edits.
	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("\n   \n"); err != nil {
		t.Fatalf("write blank lines: %v", err)
	}
	f.Close()

	got, err := s.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d entries, want 1", len(got))
	}
}

func TestListEntriesCorruptLineErrors(t *testing.T) {
	s := newTestStore(t)
	appendAll(t, s, mkEntry("a", 0, nil))

	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("{not valid json\n"); err != nil {
		t.Fatalf("write corrupt line: %v", err)
	}
	f.Close()

	if _, err := s.ListEntries(context.Background(), types.EntryFilter{}); err == nil {
		t.Error("expected error on corrupt log line, got nil")
	}
}

func TestFocusLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Absent focus reads as nil, not an error.
	got, err := s.GetCurrentFocus(ctx)
	if err != nil {
		t.Fatalf("GetCurrentFocus (absent): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil focus, got %+v", got)
	}

	focus := types.Focus{
		Project:   "DERS",
		Task:      "OAuth compliance tests",
		SessionID: "sess-1",
		StartedAt: baseTime,
	}
	if err := s.SetCurrentFocus(ctx, focus); err != nil {
		t.Fatalf("SetCurrentFocus: %v", err)
	}

	got, err = s.GetCurrentFocus(ctx)
	if err != nil {
		t.Fatalf("GetCurrentFocus: %v", err)
	}
	if got == nil {
		t.Fatal("expected focus, got nil")
	}
	if got.Project != focus.Project || got.Task != focus.Task || got.SessionID != focus.SessionID {
		t.Errorf("focus mismatch: got %+v, want %+v", *got, focus)
	}
	if !got.StartedAt.Equal(focus.StartedAt) {
		t.Errorf("StartedAt: got %v, want %v", got.StartedAt, focus.StartedAt)
	}
}

func TestSetCurrentFocusOverwrites(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetCurrentFocus(ctx, types.Focus{Task: "first"}); err != nil {
		t.Fatalf("first SetCurrentFocus: %v", err)
	}
	if err := s.SetCurrentFocus(ctx, types.Focus{Task: "second"}); err != nil {
		t.Fatalf("second SetCurrentFocus: %v", err)
	}

	got, err := s.GetCurrentFocus(ctx)
	if err != nil {
		t.Fatalf("GetCurrentFocus: %v", err)
	}
	if got == nil || got.Task != "second" {
		t.Errorf("expected task=second, got %+v", got)
	}
}

func TestClearCurrentFocus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Clearing when nothing is set is a no-op, not an error.
	if err := s.ClearCurrentFocus(ctx); err != nil {
		t.Fatalf("ClearCurrentFocus (absent): %v", err)
	}

	if err := s.SetCurrentFocus(ctx, types.Focus{Task: "active"}); err != nil {
		t.Fatalf("SetCurrentFocus: %v", err)
	}
	if err := s.ClearCurrentFocus(ctx); err != nil {
		t.Fatalf("ClearCurrentFocus: %v", err)
	}

	got, err := s.GetCurrentFocus(ctx)
	if err != nil {
		t.Fatalf("GetCurrentFocus after clear: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil focus after clear, got %+v", got)
	}
}

func TestContextCancellation(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.AppendEntry(ctx, mkEntry("a", 0, nil)); err == nil {
		t.Error("AppendEntry: expected context error, got nil")
	}
	if _, err := s.ListEntries(ctx, types.EntryFilter{}); err == nil {
		t.Error("ListEntries: expected context error, got nil")
	}
	if _, err := s.GetCurrentFocus(ctx); err == nil {
		t.Error("GetCurrentFocus: expected context error, got nil")
	}
	if err := s.SetCurrentFocus(ctx, types.Focus{}); err == nil {
		t.Error("SetCurrentFocus: expected context error, got nil")
	}
	if err := s.ClearCurrentFocus(ctx); err == nil {
		t.Error("ClearCurrentFocus: expected context error, got nil")
	}
}

func TestAppendPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s1, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	appendAll(t, s1, mkEntry("a", 0, nil), mkEntry("b", 1*time.Hour, nil))

	// A fresh store over the same dir must see prior writes.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	got, err := s2.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
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
