package mcp

import (
	"fmt"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// taskOutput is the structured view of a derived task.
type taskOutput struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Status       string   `json:"status"`
	CreatedAt    string   `json:"created_at,omitempty"`
	LastEventAt  string   `json:"last_event_at,omitempty"`
	OriginTaskID string   `json:"origin_task_id,omitempty"`
	OriginRel    string   `json:"origin_rel,omitempty"`
	BlockedBy    []string `json:"blocked_by,omitempty"`
	Blocks       []string `json:"blocks,omitempty"`
}

func newTaskOutput(t projection.Task) taskOutput {
	return taskOutput{
		ID:           t.ID,
		Name:         t.Name,
		Status:       string(t.Status),
		CreatedAt:    formatTime(t.CreatedAt),
		LastEventAt:  formatTime(t.LastEventAt),
		OriginTaskID: t.OriginTaskID,
		OriginRel:    string(t.OriginRel),
		BlockedBy:    t.BlockedBy,
		Blocks:       t.Blocks,
	}
}

// focusOutput reports the current focus. Active is false when nothing is in
// progress; Task is populated only when Active is true.
type focusOutput struct {
	Active bool        `json:"active"`
	Task   *taskOutput `json:"task,omitempty"`
}

func newFocusOutput(t *projection.Task) focusOutput {
	if t == nil {
		return focusOutput{Active: false}
	}
	out := newTaskOutput(*t)
	return focusOutput{Active: true, Task: &out}
}

// tasksOutput is the structured result of a task listing.
type tasksOutput struct {
	Count int          `json:"count"`
	Tasks []taskOutput `json:"tasks"`
}

func newTasksOutput(tasks []projection.Task) tasksOutput {
	out := tasksOutput{Count: len(tasks), Tasks: make([]taskOutput, 0, len(tasks))}
	for _, t := range tasks {
		out.Tasks = append(out.Tasks, newTaskOutput(t))
	}
	return out
}

// threadOutput annotates a loose-thread task with staleness and readiness.
type threadOutput struct {
	Task          taskOutput `json:"task"`
	IdleSeconds   int64      `json:"idle_seconds"`
	Idle          string     `json:"idle"`
	ReadyToResume bool       `json:"ready_to_resume"`
}

type threadsOutput struct {
	Count   int            `json:"count"`
	Threads []threadOutput `json:"threads"`
}

func newThreadsOutput(threads []projection.Thread) threadsOutput {
	out := threadsOutput{Count: len(threads), Threads: make([]threadOutput, 0, len(threads))}
	for _, th := range threads {
		out.Threads = append(out.Threads, threadOutput{
			Task:          newTaskOutput(th.Task),
			IdleSeconds:   int64(th.Idle.Seconds()),
			Idle:          humanizeDuration(th.Idle),
			ReadyToResume: th.ReadyToResume,
		})
	}
	return out
}

// eventOutput mirrors a stored event with stable JSON field names.
type eventOutput struct {
	ID         string `json:"id"`
	Timestamp  string `json:"timestamp"`
	Action     string `json:"action"`
	TaskID     string `json:"task_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Message    string `json:"message,omitempty"`
	NextAction string `json:"next_action,omitempty"`
	LinkTaskID string `json:"link_task_id,omitempty"`
	Rel        string `json:"rel,omitempty"`
}

func newEventOutput(e types.Event) eventOutput {
	return eventOutput{
		ID:         e.ID,
		Timestamp:  formatTime(e.Timestamp),
		Action:     string(e.Action),
		TaskID:     e.TaskID,
		Name:       e.Name,
		Message:    e.Message,
		NextAction: e.NextAction,
		LinkTaskID: e.LinkTaskID,
		Rel:        string(e.Rel),
	}
}

type eventsOutput struct {
	Count  int           `json:"count"`
	Events []eventOutput `json:"events"`
}

func newEventsOutput(events []types.Event) eventsOutput {
	out := eventsOutput{Count: len(events), Events: make([]eventOutput, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, newEventOutput(e))
	}
	return out
}

// contextOutput is the structured resume bundle for a task.
type contextOutput struct {
	Task             taskOutput    `json:"task"`
	LatestCheckpoint *eventOutput  `json:"latest_checkpoint,omitempty"`
	RecentEvents     []eventOutput `json:"recent_events"`
}

func newContextOutput(c projection.Context) contextOutput {
	out := contextOutput{Task: newTaskOutput(c.Task)}
	if c.LatestCheckpoint != nil {
		cp := newEventOutput(*c.LatestCheckpoint)
		out.LatestCheckpoint = &cp
	}
	out.RecentEvents = make([]eventOutput, 0, len(c.RecentEvents))
	for _, e := range c.RecentEvents {
		out.RecentEvents = append(out.RecentEvents, newEventOutput(e))
	}
	return out
}

// graphOutput is the structured result of a journal git-graph render. Mermaid
// always carries the gitGraph source; SVG is populated only when that format was
// requested.
type graphOutput struct {
	Format    string `json:"format"`
	Mermaid   string `json:"mermaid"`
	SVG       string `json:"svg,omitempty"`
	TaskCount int    `json:"task_count"`
	Scoped    bool   `json:"scoped"`
}

func newGraphOutput(r service.GraphResult) graphOutput {
	return graphOutput{
		Format:    string(r.Format),
		Mermaid:   r.Mermaid,
		SVG:       r.SVG,
		TaskCount: r.TaskCount,
		Scoped:    r.Scoped,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// humanizeDuration renders a coarse, human-readable idle time.
func humanizeDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
