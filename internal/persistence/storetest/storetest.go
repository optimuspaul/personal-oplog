// Package storetest provides a backend-agnostic conformance suite for
// persistence.Store implementations. Each backend's test file supplies a
// Factory and calls Run; the suite exercises the behavior every Store must
// share (round-tripping, filtering, ordering, limits, durability).
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence"
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// NewStore returns a Store backed by a fixed, test-scoped location. Calling it
// again must return a new handle to the SAME underlying data, so the reopen
// test can verify durability across instances.
type NewStore func() persistence.Store

// Factory binds a NewStore to a fresh location for a single test (t.TempDir).
type Factory func(t *testing.T) NewStore

// baseTime is a fixed instant so tests are deterministic across time zones.
var baseTime = time.Date(2026, 6, 23, 21, 15, 0, 0, time.UTC)

func mkEvent(id string, offset time.Duration, mutate func(*types.Event)) types.Event {
	e := types.Event{
		ID:        id,
		Timestamp: baseTime.Add(offset),
		Action:    types.ActionNote,
		TaskID:    "task-1",
		Message:   "a note",
	}
	if mutate != nil {
		mutate(&e)
	}
	return e
}

func appendAll(t *testing.T, s persistence.Store, events ...types.Event) {
	t.Helper()
	for _, e := range events {
		if err := s.AppendEvent(context.Background(), e); err != nil {
			t.Fatalf("AppendEvent(%s): %v", e.ID, err)
		}
	}
}

func listIDs(t *testing.T, s persistence.Store, filter types.EventFilter) []string {
	t.Helper()
	events, err := s.ListEvents(context.Background(), filter)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return ids(events)
}

func ids(events []types.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.ID
	}
	return out
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

// Run executes the conformance suite against stores produced by factory.
func Run(t *testing.T, factory Factory) {
	t.Run("AppendAndListRoundTrip", func(t *testing.T) {
		s := factory(t)()
		want := mkEvent("01", 0, func(e *types.Event) {
			e.Action = types.ActionCheckpoint
			e.Message = "Password grant passes."
			e.NextAction = "Inspect audience parameter."
			e.LinkTaskID = "task-2"
			e.Rel = types.RelRelatesTo
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
		if got[0].Action != want.Action || got[0].Message != want.Message || got[0].NextAction != want.NextAction {
			t.Errorf("core fields not preserved: %+v", got[0])
		}
		if got[0].LinkTaskID != want.LinkTaskID || got[0].Rel != want.Rel {
			t.Errorf("link fields not preserved: %+v", got[0])
		}
	})

	t.Run("ListEventsMostRecentFirst", func(t *testing.T) {
		s := factory(t)()
		appendAll(t, s,
			mkEvent("middle", 1*time.Hour, nil),
			mkEvent("oldest", 0, nil),
			mkEvent("newest", 2*time.Hour, nil),
		)
		if g := listIDs(t, s, types.EventFilter{}); !equalStrings(g, []string{"newest", "middle", "oldest"}) {
			t.Errorf("order: got %v", g)
		}
	})

	t.Run("ListEventsEmpty", func(t *testing.T) {
		s := factory(t)()
		if g := listIDs(t, s, types.EventFilter{}); len(g) != 0 {
			t.Errorf("got %v, want empty", g)
		}
	})

	t.Run("ListEventsFilters", func(t *testing.T) {
		s := factory(t)()
		appendAll(t, s,
			mkEvent("a", 0, func(e *types.Event) {
				e.TaskID = "t1"
				e.Action = types.ActionCheckpoint
				e.Message = "Client credentials failing."
			}),
			mkEvent("b", 1*time.Hour, func(e *types.Event) {
				e.TaskID = "t2"
				e.Action = types.ActionNote
				e.Message = "Refactored invoices."
			}),
			mkEvent("c", 2*time.Hour, func(e *types.Event) {
				e.TaskID = "t1"
				e.Action = types.ActionCheckpoint
				e.Message = "Unrelated."
				e.NextAction = "audit auth0 scopes"
			}),
		)

		tests := []struct {
			name   string
			filter types.EventFilter
			want   []string
		}{
			{"task", types.EventFilter{TaskID: "t1"}, []string{"c", "a"}},
			{"action", types.EventFilter{Actions: []types.Action{types.ActionCheckpoint}}, []string{"c", "a"}},
			{"actions multi", types.EventFilter{Actions: []types.Action{types.ActionCheckpoint, types.ActionNote}}, []string{"c", "b", "a"}},
			{"text case-insensitive", types.EventFilter{Text: "CREDENTIALS"}, []string{"a"}},
			{"text matches next_action", types.EventFilter{Text: "auth0"}, []string{"c"}},
			{"task+action", types.EventFilter{TaskID: "t1", Actions: []types.Action{types.ActionCheckpoint}}, []string{"c", "a"}},
			{"no match", types.EventFilter{TaskID: "NOPE"}, nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if g := listIDs(t, s, tt.filter); !equalStrings(g, tt.want) {
					t.Errorf("got %v, want %v", g, tt.want)
				}
			})
		}
	})

	t.Run("ListEventsSinceUntil", func(t *testing.T) {
		s := factory(t)()
		appendAll(t, s,
			mkEvent("t0", 0, nil),
			mkEvent("t1", 1*time.Hour, nil),
			mkEvent("t2", 2*time.Hour, nil),
		)
		boundary := baseTime.Add(1 * time.Hour)

		if g := listIDs(t, s, types.EventFilter{Since: &boundary}); !equalStrings(g, []string{"t2", "t1"}) {
			t.Errorf("since: got %v", g)
		}
		if g := listIDs(t, s, types.EventFilter{Until: &boundary}); !equalStrings(g, []string{"t1", "t0"}) {
			t.Errorf("until: got %v", g)
		}
	})

	t.Run("ListEventsLimitKeepsMostRecent", func(t *testing.T) {
		s := factory(t)()
		appendAll(t, s,
			mkEvent("t0", 0, nil),
			mkEvent("t1", 1*time.Hour, nil),
			mkEvent("t2", 2*time.Hour, nil),
		)
		if g := listIDs(t, s, types.EventFilter{Limit: 2}); !equalStrings(g, []string{"t2", "t1"}) {
			t.Errorf("limit: got %v, want [t2 t1]", g)
		}
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		s := factory(t)()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := s.AppendEvent(ctx, mkEvent("a", 0, nil)); err == nil {
			t.Error("AppendEvent: expected context error, got nil")
		}
		if _, err := s.ListEvents(ctx, types.EventFilter{}); err == nil {
			t.Error("ListEvents: expected context error, got nil")
		}
	})

	t.Run("PersistsAcrossReopen", func(t *testing.T) {
		newStore := factory(t)
		appendAll(t, newStore(), mkEvent("a", 0, nil), mkEvent("b", 1*time.Hour, nil))

		reopened := newStore()
		if g := listIDs(t, reopened, types.EventFilter{}); !equalStrings(g, []string{"b", "a"}) {
			t.Errorf("after reopen: got %v, want [b a]", g)
		}
	})
}
