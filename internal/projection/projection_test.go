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

func (b *builder) ev(action types.Action, id, name, link string) *builder {
	e := types.Event{Action: action, TaskID: id, Name: name}
	if link != "" {
		e.LinkTaskID = link
		e.Rel = types.RelForAction(action)
	}
	return b.add(e)
}

func (b *builder) start(id, name string) *builder { return b.ev(types.ActionStart, id, name, "") }
func (b *builder) note(id, name string) *builder  { return b.ev(types.ActionNote, id, name, "") }
func (b *builder) park(id string) *builder        { return b.ev(types.ActionPark, id, "", "") }
func (b *builder) complete(id string) *builder    { return b.ev(types.ActionComplete, id, "", "") }

// block records that id is blocked by blocker.
func (b *builder) block(id, blocker string) *builder {
	return b.ev(types.ActionBlock, id, "", blocker)
}

func (b *builder) build() *projection.World { return projection.Build(b.events) }

func TestStatusTransitions(t *testing.T) {
	w := (&builder{}).
		note("a", "alpha").                // new: referenced, never started
		start("b", "beta").                // active
		start("c", "gamma").park("c").     // parked
		start("d", "delta").complete("d"). // done
		start("e", "eps").block("e", "b"). // blocked (by active b)
		build()

	want := map[string]projection.TaskStatus{
		"a": projection.StatusNew,
		"b": projection.StatusActive,
		"c": projection.StatusParked,
		"d": projection.StatusDone,
		"e": projection.StatusBlocked,
	}
	for _, tk := range w.Tasks() {
		if want[tk.ID] != tk.Status {
			t.Errorf("task %s: status = %q, want %q", tk.ID, tk.Status, want[tk.ID])
		}
	}
}

func TestFocusIsMostRecentActive(t *testing.T) {
	w := (&builder{}).
		start("a", "alpha").
		start("b", "beta").
		build()

	focus := w.Focus()
	if focus == nil || focus.ID != "b" {
		t.Fatalf("expected focus on b (most recent start), got %+v", focus)
	}
}

func TestFocusNilWhenAllClosed(t *testing.T) {
	w := (&builder{}).
		start("a", "alpha").park("a").
		build()
	if f := w.Focus(); f != nil {
		t.Errorf("expected no focus, got %+v", f)
	}
}

func TestBlockedAndResolutionByBlockerCompletion(t *testing.T) {
	// a is blocked by b; a reads blocked.
	w := (&builder{}).
		start("a", "alpha").park("a").
		start("b", "beta").
		block("a", "b").
		build()

	a := w.Task("a")
	if a.Status != projection.StatusBlocked {
		t.Errorf("a status = %q, want blocked", a.Status)
	}
	if len(a.BlockedBy) != 1 || a.BlockedBy[0] != "b" {
		t.Errorf("a.BlockedBy = %v, want [b]", a.BlockedBy)
	}

	// Completing the blocker clears the block; a is no longer blocked.
	w2 := (&builder{}).
		start("a", "alpha").park("a").
		start("b", "beta").
		block("a", "b").
		complete("b").
		build()
	if got := w2.Task("a"); got.Status == projection.StatusBlocked {
		t.Errorf("a should not be blocked after blocker completes, got %q", got.Status)
	}
}

func TestLooseThreadsRankReadyFirstThenStalest(t *testing.T) {
	// a: parked long ago, never blocked.
	// b: was blocked, blocker since completed (ready to resume).
	// c: the current focus, must be excluded.
	w := (&builder{}).
		start("a", "alpha").park("a").
		start("b", "beta").
		start("x", "blocker").
		block("b", "x").
		complete("x").
		start("c", "gamma").
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

func TestMatchByNameRecencyOrdered(t *testing.T) {
	w := (&builder{}).
		start("a", "monkey task").
		start("b", "monkey wrench").
		note("c", "banana").
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
