// Package projection derives every read-side view Oplog exposes — tasks and
// their status, the current focus, and loose threads — from the append-only
// event stream.
//
// Nothing here mutates storage. A World is built by folding events in
// chronological order; callers then ask it questions. Because the event log
// is the single source of truth, the same events can be reinterpreted in new
// ways simply by adding methods here.
package projection

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// TaskStatus is the lifecycle state of a task, derived from its events.
type TaskStatus string

const (
	// StatusNew: referenced but never started — a not-yet-picked-up thread.
	StatusNew TaskStatus = "new"
	// StatusActive: currently being worked on (the focus).
	StatusActive TaskStatus = "active"
	// StatusParked: started, then set aside while still open.
	StatusParked TaskStatus = "parked"
	// StatusBlocked: set aside with an open blocker.
	StatusBlocked TaskStatus = "blocked"
	// StatusDone: closed — completed, dropped, or subsumed.
	StatusDone TaskStatus = "done"
)

// IsOpen reports whether a status represents unfinished work.
func (s TaskStatus) IsOpen() bool {
	return s != StatusDone
}

// Task is the derived view of a single task at the point the World was built.
type Task struct {
	ID          string
	Name        string
	Status      TaskStatus
	CreatedAt   time.Time
	LastEventAt time.Time

	// Origin links this task back to the task it was spawned from, if any.
	OriginTaskID string
	OriginRel    types.Relationship

	// BlockedBy lists task IDs with an open "blocks" edge into this task.
	BlockedBy []string
	// Blocks lists open task IDs this task currently blocks.
	Blocks []string

	// hadBlocker records that a block was once recorded against this task, even
	// after the blocker cleared — used to surface "now unblocked" threads.
	hadBlocker     bool
	blockerIDs     []string // every task ever recorded as blocking this one
	blockingIDs    []string // every task this one was ever recorded blocking
	lastFocusStart time.Time
}

// Thread is a loose thread: an open task that is not the current focus,
// annotated with how stale it is and whether it is ready to pick back up.
type Thread struct {
	Task
	// Idle is how long since the task's last event.
	Idle time.Duration
	// ReadyToResume is true when the task was held by a blocker that has since
	// completed — the highest-signal thread to revisit.
	ReadyToResume bool
}

// Context bundles everything needed to resume a task: the task itself, its
// most recent checkpoint (if any), and its most recent events.
type Context struct {
	Task             Task
	LatestCheckpoint *types.Event
	RecentEvents     []types.Event
}

// World holds the folded state of all events. Build it once, then query it.
type World struct {
	tasks map[string]*Task
	order []string // task IDs in creation (first-seen) order
}

// Build folds events into a World. Events may arrive in any order; they are
// sorted chronologically (ID breaks ties) before folding.
func Build(events []types.Event) *World {
	sorted := make([]types.Event, len(events))
	copy(sorted, events)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Timestamp.Equal(sorted[j].Timestamp) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})

	w := &World{tasks: make(map[string]*Task)}
	for _, e := range sorted {
		w.apply(e)
	}
	w.finalize()
	return w
}

// ensure returns the task for id, creating a placeholder if the stream
// references a task before it has been named.
func (w *World) ensure(id string) *Task {
	if id == "" {
		return nil
	}
	t, ok := w.tasks[id]
	if !ok {
		t = &Task{ID: id, Status: StatusNew}
		w.tasks[id] = t
		w.order = append(w.order, id)
	}
	return t
}

func (w *World) apply(e types.Event) {
	t := w.ensure(e.TaskID)
	if t == nil {
		return
	}
	if t.Name == "" && e.Name != "" {
		t.Name = e.Name
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = e.Timestamp
	}
	t.touch(e.Timestamp)

	switch e.Action {
	case types.ActionStart, types.ActionResume, types.ActionRestart:
		t.Status = StatusActive
		t.lastFocusStart = e.Timestamp
		if e.Action == types.ActionStart && e.LinkTaskID != "" {
			t.OriginTaskID = e.LinkTaskID
			t.OriginRel = types.RelOriginatedFrom
		}

	case types.ActionPark:
		t.Status = StatusParked

	case types.ActionBlock:
		t.Status = StatusBlocked
		if blocker := w.ensure(e.LinkTaskID); blocker != nil {
			t.blockerIDs = addString(t.blockerIDs, blocker.ID)
			blocker.blockingIDs = addString(blocker.blockingIDs, t.ID)
			t.hadBlocker = true
		}

	case types.ActionComplete:
		t.Status = StatusDone

	case types.ActionCheckpoint, types.ActionNote:
		// No status change; the touch above records the activity.
	}
}

// finalize resolves block edges against final task states: a block clears once
// its blocker is done. An open task with any open blocker reads as blocked; one
// whose blockers have all cleared falls back to parked (and surfaces as
// ready-to-resume).
func (w *World) finalize() {
	// Pass 1: recompute each task's open blockers and adjust status.
	for _, t := range w.tasks {
		open := t.blockerIDs[:0:0]
		for _, id := range t.blockerIDs {
			if b := w.tasks[id]; b != nil && b.Status.IsOpen() {
				open = append(open, id)
			}
		}
		t.BlockedBy = open
		if t.Status.IsOpen() {
			if len(open) > 0 {
				t.Status = StatusBlocked
			} else if t.hadBlocker && t.Status == StatusBlocked {
				t.Status = StatusParked
			}
		}
	}

	// Pass 2: invert BlockedBy into each blocker's Blocks list.
	for _, t := range w.tasks {
		for _, id := range t.BlockedBy {
			if b := w.tasks[id]; b != nil {
				b.Blocks = addString(b.Blocks, t.ID)
			}
		}
	}

	for _, t := range w.tasks {
		sort.Strings(t.BlockedBy)
		sort.Strings(t.Blocks)
	}
}

func (t *Task) touch(ts time.Time) {
	if ts.After(t.LastEventAt) {
		t.LastEventAt = ts
	}
}

// Tasks returns all tasks in creation order.
func (w *World) Tasks() []Task {
	out := make([]Task, 0, len(w.order))
	for _, id := range w.order {
		out = append(out, *w.tasks[id])
	}
	return out
}

// Task returns a single task by ID, or nil if unknown.
func (w *World) Task(id string) *Task {
	t, ok := w.tasks[id]
	if !ok {
		return nil
	}
	cp := *t
	return &cp
}

// Focus returns the task currently being worked on — the active task with the
// most recent focus-taking event — or nil when nothing is active.
func (w *World) Focus() *Task {
	var best *Task
	for _, t := range w.tasks {
		if t.Status != StatusActive {
			continue
		}
		if best == nil || t.lastFocusStart.After(best.lastFocusStart) {
			best = t
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

// LooseThreads returns open tasks that are not the current focus, ranked so
// the most actionable surface first: ready-to-resume threads, then by
// staleness (longest idle first). now is used to compute idle time.
func (w *World) LooseThreads(now time.Time) []Thread {
	focus := w.Focus()
	var focusID string
	if focus != nil {
		focusID = focus.ID
	}

	var threads []Thread
	for _, id := range w.order {
		t := w.tasks[id]
		if !t.Status.IsOpen() || t.ID == focusID {
			continue
		}
		ready := t.hadBlocker && len(t.BlockedBy) == 0 && t.Status != StatusActive
		threads = append(threads, Thread{
			Task:          *t,
			Idle:          now.Sub(t.LastEventAt),
			ReadyToResume: ready,
		})
	}

	sort.SliceStable(threads, func(i, j int) bool {
		if threads[i].ReadyToResume != threads[j].ReadyToResume {
			return threads[i].ReadyToResume // ready first
		}
		return threads[i].Idle > threads[j].Idle // stalest first
	})
	return threads
}

// Match returns tasks whose name contains query (case-insensitive), most
// recently active first. An empty query returns all tasks in that order.
func Match(tasks []Task, query string) []Task {
	needle := strings.ToLower(strings.TrimSpace(query))
	var out []Task
	for _, t := range tasks {
		if needle == "" || strings.Contains(strings.ToLower(t.Name), needle) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastEventAt.After(out[j].LastEventAt)
	})
	return out
}

func addString(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}
