// Package projection derives every read-side view Oplog exposes — tasks and
// their status, the current focus, projects, and loose threads — from the
// append-only event stream.
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
	// StatusNew: created but never started — a not-yet-picked-up thread.
	StatusNew TaskStatus = "new"
	// StatusActive: currently being worked on (the focus).
	StatusActive TaskStatus = "active"
	// StatusParked: started, then set aside while still open.
	StatusParked TaskStatus = "parked"
	// StatusBlocked: parked because of, or held by, an unresolved blocker.
	StatusBlocked TaskStatus = "blocked"
	// StatusDone: completed.
	StatusDone TaskStatus = "done"
	// StatusAbandoned: dropped, will not be resumed.
	StatusAbandoned TaskStatus = "abandoned"
)

// IsOpen reports whether a status represents unfinished work.
func (s TaskStatus) IsOpen() bool {
	return s != StatusDone && s != StatusAbandoned
}

// Task is the derived view of a single task at the point the World was built.
type Task struct {
	ID          string
	Project     string
	Name        string
	Status      TaskStatus
	CreatedAt   time.Time
	LastEventAt time.Time

	// Origin links this task back to the task it was spawned from, if any.
	OriginTaskID string
	OriginRel    types.Relationship

	// ParkReason is the reason from the most recent park, when parked/blocked.
	ParkReason types.ParkReason

	// BlockedBy lists task IDs with an unresolved "blocks" edge into this task.
	BlockedBy []string
	// Blocks lists task IDs this task has an unresolved "blocks" edge into.
	Blocks []string

	// hadBlocker records that a blocks edge was once recorded against this
	// task, even if later resolved — used to surface "now unblocked" threads.
	hadBlocker        bool
	lastFocusStart    time.Time
	hasLatestCheckpnt bool
}

// Thread is a loose thread: an open task that is not the current focus,
// annotated with how stale it is and whether it is ready to pick back up.
type Thread struct {
	Task
	// Idle is how long since the task's last event.
	Idle time.Duration
	// ReadyToResume is true when the task was held by a blocker that is now
	// resolved — the highest-signal thread to revisit.
	ReadyToResume bool
}

// Project is the derived view of a project: a namespace tasks belong to.
type Project struct {
	Name        string
	TaskCount   int
	OpenCount   int
	LastEventAt time.Time
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
// references a task before (or without) a task_created event.
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
	switch e.Type {
	case types.EventTaskCreated:
		t := w.ensure(e.TaskID)
		t.Project = e.Project
		t.Name = e.Name
		t.OriginTaskID = e.OriginTaskID
		t.OriginRel = e.OriginRel
		if t.CreatedAt.IsZero() {
			t.CreatedAt = e.Timestamp
		}
		t.touch(e.Timestamp)

	case types.EventFocusStart:
		t := w.ensure(e.TaskID)
		t.Status = StatusActive
		t.ParkReason = ""
		t.lastFocusStart = e.Timestamp
		t.touch(e.Timestamp)

	case types.EventPark:
		t := w.ensure(e.TaskID)
		t.Status = StatusParked
		t.ParkReason = e.Reason
		t.touch(e.Timestamp)

	case types.EventComplete:
		t := w.ensure(e.TaskID)
		t.Status = StatusDone
		t.touch(e.Timestamp)

	case types.EventAbandon:
		t := w.ensure(e.TaskID)
		t.Status = StatusAbandoned
		t.touch(e.Timestamp)

	case types.EventCheckpoint, types.EventNote:
		t := w.ensure(e.TaskID)
		t.touch(e.Timestamp)

	case types.EventLink:
		w.applyLink(e)
	}
}

func (w *World) applyLink(e types.Event) {
	from := w.ensure(e.TaskID)
	to := w.ensure(e.ToTaskID)
	if from == nil || to == nil || e.Rel != types.RelBlocks {
		// Only blocks edges affect derived status; other relationships are
		// recorded in the log but carry no projected state today.
		return
	}
	if e.Resolved {
		from.Blocks = removeString(from.Blocks, to.ID)
		to.BlockedBy = removeString(to.BlockedBy, from.ID)
		return
	}
	from.Blocks = addString(from.Blocks, to.ID)
	to.BlockedBy = addString(to.BlockedBy, from.ID)
	to.hadBlocker = true
}

// finalize applies the blocked overlay once all events are folded: any open
// task with unresolved blockers (or parked specifically because it was
// blocked) reads as blocked.
func (w *World) finalize() {
	for _, t := range w.tasks {
		if !t.Status.IsOpen() {
			continue
		}
		if len(t.BlockedBy) > 0 || t.ParkReason == types.ParkBlocked {
			t.Status = StatusBlocked
		}
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
// most recent focus_start — or nil when nothing is active.
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

// Projects returns the known projects, ordered by most recent activity.
func (w *World) Projects() []Project {
	byName := make(map[string]*Project)
	var order []string
	for _, id := range w.order {
		t := w.tasks[id]
		name := t.Project
		p, ok := byName[name]
		if !ok {
			p = &Project{Name: name}
			byName[name] = p
			order = append(order, name)
		}
		p.TaskCount++
		if t.Status.IsOpen() {
			p.OpenCount++
		}
		if t.LastEventAt.After(p.LastEventAt) {
			p.LastEventAt = t.LastEventAt
		}
	}

	out := make([]Project, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastEventAt.After(out[j].LastEventAt)
	})
	return out
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

func removeString(s []string, v string) []string {
	out := s[:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
