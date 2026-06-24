package mcp

import (
	"fmt"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// taskOutput is the structured view of a derived task.
type taskOutput struct {
	ID           string   `json:"id"`
	Project      string   `json:"project,omitempty"`
	Name         string   `json:"name,omitempty"`
	Status       string   `json:"status"`
	CreatedAt    string   `json:"created_at,omitempty"`
	LastEventAt  string   `json:"last_event_at,omitempty"`
	OriginTaskID string   `json:"origin_task_id,omitempty"`
	OriginRel    string   `json:"origin_rel,omitempty"`
	ParkReason   string   `json:"park_reason,omitempty"`
	BlockedBy    []string `json:"blocked_by,omitempty"`
	Blocks       []string `json:"blocks,omitempty"`
}

func newTaskOutput(t projection.Task) taskOutput {
	return taskOutput{
		ID:           t.ID,
		Project:      t.Project,
		Name:         t.Name,
		Status:       string(t.Status),
		CreatedAt:    formatTime(t.CreatedAt),
		LastEventAt:  formatTime(t.LastEventAt),
		OriginTaskID: t.OriginTaskID,
		OriginRel:    string(t.OriginRel),
		ParkReason:   string(t.ParkReason),
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

// projectOutput is the structured view of a project.
type projectOutput struct {
	Name        string `json:"name"`
	TaskCount   int    `json:"task_count"`
	OpenCount   int    `json:"open_count"`
	LastEventAt string `json:"last_event_at,omitempty"`
}

type projectsOutput struct {
	Count    int             `json:"count"`
	Projects []projectOutput `json:"projects"`
}

func newProjectsOutput(projects []projection.Project) projectsOutput {
	out := projectsOutput{Count: len(projects), Projects: make([]projectOutput, 0, len(projects))}
	for _, p := range projects {
		out.Projects = append(out.Projects, projectOutput{
			Name:        p.Name,
			TaskCount:   p.TaskCount,
			OpenCount:   p.OpenCount,
			LastEventAt: formatTime(p.LastEventAt),
		})
	}
	return out
}

// eventOutput mirrors a stored event with stable JSON field names.
type eventOutput struct {
	ID           string   `json:"id"`
	Timestamp    string   `json:"timestamp"`
	Type         string   `json:"type"`
	TaskID       string   `json:"task_id,omitempty"`
	Project      string   `json:"project,omitempty"`
	Name         string   `json:"name,omitempty"`
	OriginTaskID string   `json:"origin_task_id,omitempty"`
	OriginRel    string   `json:"origin_rel,omitempty"`
	FromTaskID   string   `json:"from_task_id,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	CauseTaskID  string   `json:"cause_task_id,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Text         string   `json:"text,omitempty"`
	NextAction   string   `json:"next_action,omitempty"`
	OpenQuestion []string `json:"open_questions,omitempty"`
	Files        []string `json:"files,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	ToTaskID     string   `json:"to_task_id,omitempty"`
	Rel          string   `json:"rel,omitempty"`
	Resolved     bool     `json:"resolved,omitempty"`
}

func newEventOutput(e types.Event) eventOutput {
	return eventOutput{
		ID:           e.ID,
		Timestamp:    formatTime(e.Timestamp),
		Type:         string(e.Type),
		TaskID:       e.TaskID,
		Project:      e.Project,
		Name:         e.Name,
		OriginTaskID: e.OriginTaskID,
		OriginRel:    string(e.OriginRel),
		FromTaskID:   e.FromTaskID,
		Reason:       string(e.Reason),
		CauseTaskID:  e.CauseTaskID,
		Summary:      e.Summary,
		Text:         e.Text,
		NextAction:   e.NextAction,
		OpenQuestion: e.OpenQuestions,
		Files:        e.Files,
		Tags:         e.Tags,
		ToTaskID:     e.ToTaskID,
		Rel:          string(e.Rel),
		Resolved:     e.Resolved,
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
