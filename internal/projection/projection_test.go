package projection_test

import (
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

var base = time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)

// builder accumulates events with monotonically increasing timestamps so the
// fold order is unambiguous.
type builder struct {
	events []types.Event
	n      int
}

func (b *builder) add(e types.Event) *builder {
	b.n++
	e.ID = time.Duration(b.n).String()
	e.Timestamp = base.Add(time.Duration(b.n) * time.Minute)
	b.events = append(b.events, e)
	return b
}

func (b *builder) created(id, project, name string) *builder {
	return b.add(types.Event{Type: types.EventTaskCreated, TaskID: id, Project: project, Name: name})
}
func (b *builder) start(id string) *builder {
	return b.add(types.Event{Type: types.EventFocusStart, TaskID: id})
}
func (b *builder) park(id string, reason types.ParkReason) *builder {
	return b.add(types.Event{Type: types.EventPark, TaskID: id, Reason: reason})
}
func (b *builder) complete(id string) *builder {
	return b.add(types.Event{Type: types.EventComplete, TaskID: id})
}
func (b *builder) link(from, to string, rel types.Relationship, resolved bool) *builder {
	return b.add(types.Event{Type: types.EventLink, TaskID: from, ToTaskID: to, Rel: rel, Resolved: resolved})
}

func (b *builder) build() *projection.World { return projection.Build(b.events) }

func TestStatusTransitions(t *testing.T) {
	w := (&builder{}).
		created("a", "P", "alpha").                                        // new
		created("b", "P", "beta").start("b").                              // active
		created("c", "P", "gamma").start("c").park("c", types.ParkPaused). // parked
		created("d", "P", "delta").start("d").complete("d").               // done
		build()

	want := map[string]projection.TaskStatus{
		"a": projection.StatusNew,
		"b": projection.StatusActive,
		"c": projection.StatusParked,
		"d": projection.StatusDone,
	}
	for _, tk := range w.Tasks() {
		if want[tk.ID] != tk.Status {
			t.Errorf("task %s: status = %q, want %q", tk.ID, tk.Status, want[tk.ID])
		}
	}
}

func TestFocusIsMostRecentActive(t *testing.T) {
	w := (&builder{}).
		created("a", "P", "alpha").start("a").
		created("b", "P", "beta").start("b").
		build()

	focus := w.Focus()
	if focus == nil || focus.ID != "b" {
		t.Fatalf("expected focus on b (most recent start), got %+v", focus)
	}
}

func TestFocusNilWhenAllClosed(t *testing.T) {
	w := (&builder{}).
		created("a", "P", "alpha").start("a").park("a", types.ParkPaused).
		build()
	if f := w.Focus(); f != nil {
		t.Errorf("expected no focus, got %+v", f)
	}
}

func TestBlockedOverlayAndResolution(t *testing.T) {
	// b blocks a; a should read blocked even though it was only parked.
	w := (&builder{}).
		created("a", "P", "alpha").start("a").park("a", types.ParkWaiting).
		created("b", "P", "beta").
		link("b", "a", types.RelBlocks, false).
		build()

	a := w.Task("a")
	if a.Status != projection.StatusBlocked {
		t.Errorf("a status = %q, want blocked", a.Status)
	}
	if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "b" {
		t.Errorf("a.BlockedBy = %v, want [b]", a.BlockedBy)
	}

	// Resolve the block: a is no longer blocked.
	w2 := (&builder{}).
		created("a", "P", "alpha").start("a").park("a", types.ParkWaiting).
		created("b", "P", "beta").
		link("b", "a", types.RelBlocks, false).
		link("b", "a", types.RelBlocks, true).
		build()
	if w2.Task("a").Status == projection.StatusBlocked {
		t.Error("a should not be blocked after the edge is resolved")
	}
}

func TestLooseThreadsRankReadyFirstThenStalest(t *testing.T) {
	// a: parked long ago, never blocked.
	// b: was blocked, now resolved (ready to resume), parked recently.
	// c: the current focus, must be excluded.
	w := (&builder{}).
		created("a", "P", "alpha").start("a").park("a", types.ParkPaused).
		created("b", "P", "beta").start("b").park("b", types.ParkBlocked).
		created("x", "P", "blocker").link("x", "b", types.RelBlocks, false).
		link("x", "b", types.RelBlocks, true).complete("x").
		created("c", "P", "gamma").start("c").
		build()

	now := base.Add(48 * time.Hour)
	threads := w.LooseThreads(now)

	// Focus c excluded; blocker x completed and excluded; a and b remain.
	if len(threads) != 2 {
		t.Fatalf("expected 2 loose threads, got %d: %+v", len(threads), threads)
	}
	if !threads[0].ReadyToResume || threads[0].ID != "b" {
		t.Errorf("expected ready-to-resume b first, got %+v", threads[0])
	}
	if threads[1].ID != "a" {
		t.Errorf("expected a second, got %+v", threads[1])
	}
}

func TestProjectsCountOpenAndTotal(t *testing.T) {
	w := (&builder{}).
		created("a", "ADS", "alpha").start("a").
		created("b", "ADS", "beta").start("b").complete("b").
		created("c", "OTHER", "gamma").
		build()

	projects := w.Projects()
	got := map[string]projection.Project{}
	for _, p := range projects {
		got[p.Name] = p
	}
	if got["ADS"].TaskCount != 2 || got["ADS"].OpenCount != 1 {
		t.Errorf("ADS = %+v, want 2 tasks / 1 open", got["ADS"])
	}
	if got["OTHER"].TaskCount != 1 || got["OTHER"].OpenCount != 1 {
		t.Errorf("OTHER = %+v, want 1 task / 1 open", got["OTHER"])
	}
}

func TestMatchByNameRecencyOrdered(t *testing.T) {
	w := (&builder{}).
		created("a", "P", "monkey task").start("a").
		created("b", "P", "monkey wrench").start("b").
		created("c", "P", "banana").
		build()

	matches := projection.Match(w.Tasks(), "monkey")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	// b was active most recently, so it sorts first.
	if matches[0].ID != "b" {
		t.Errorf("expected most-recent match b first, got %s", matches[0].ID)
	}
}
