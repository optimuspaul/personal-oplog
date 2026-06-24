package types

import "time"

// EventType enumerates the kinds of append-only events Oplog records. Every
// piece of derived state — tasks, projects, focus, loose threads — is a
// projection over a stream of these events.
type EventType string

const (
	// EventTaskCreated introduces a new task into a project. It is the only
	// event that carries Project and Name; every later event references the
	// task by TaskID.
	EventTaskCreated EventType = "task_created"
	// EventFocusStart begins or resumes active work on a task.
	EventFocusStart EventType = "focus_start"
	// EventPark sets a task aside while leaving it open (see ParkReason).
	EventPark EventType = "park"
	// EventCheckpoint captures resumable context: state, next action, questions.
	EventCheckpoint EventType = "checkpoint"
	// EventNote records a free-form note against a task.
	EventNote EventType = "note"
	// EventComplete marks a task finished.
	EventComplete EventType = "complete"
	// EventAbandon marks a task dropped — it will not be resumed.
	EventAbandon EventType = "abandon"
	// EventLink records a durable task→task edge (see Relationship).
	EventLink EventType = "link"
)

// ParkReason explains why a task was set aside. The reason drives how a parked
// task surfaces as a loose thread.
type ParkReason string

const (
	// ParkInterrupted: attention was pulled away to another task.
	ParkInterrupted ParkReason = "interrupted"
	// ParkBlocked: the task cannot proceed until something else is resolved.
	ParkBlocked ParkReason = "blocked"
	// ParkWaiting: waiting on an external party or process.
	ParkWaiting ParkReason = "waiting"
	// ParkSwitched: deliberately switched to other work.
	ParkSwitched ParkReason = "switched"
	// ParkPaused: a neutral pause with no specific cause.
	ParkPaused ParkReason = "paused"
)

// Relationship enumerates the kinds of edges between tasks.
type Relationship string

const (
	// RelOriginatedFrom: this task was spawned out of another.
	RelOriginatedFrom Relationship = "originated_from"
	// RelInterrupts: this task interrupted another (a spawned interruption).
	RelInterrupts Relationship = "interrupts"
	// RelBlocks: the source task blocks the target task.
	RelBlocks Relationship = "blocks"
	// RelRelatesTo: a loose, non-directional association.
	RelRelatesTo Relationship = "relates_to"
)

// Event is a single append-only record in the journal. It is a flat,
// union-style struct: which fields are meaningful depends on Type. Zero-valued
// fields are omitted from JSON so each line stays small.
type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`

	// TaskID is the task this event concerns. For EventLink it is the source
	// ("from") side of the edge. Empty only on malformed events.
	TaskID string `json:"task_id,omitempty"`

	// task_created fields.
	Project      string       `json:"project,omitempty"`
	Name         string       `json:"name,omitempty"`
	OriginTaskID string       `json:"origin_task_id,omitempty"`
	OriginRel    Relationship `json:"origin_rel,omitempty"`

	// focus_start: the task focus moved away from, if any.
	FromTaskID string `json:"from_task_id,omitempty"`

	// park fields.
	Reason      ParkReason `json:"reason,omitempty"`
	CauseTaskID string     `json:"cause_task_id,omitempty"`

	// checkpoint / note / complete / abandon payloads. Summary holds the
	// checkpoint state, the completion summary, or the abandon reason; Text
	// holds a note body.
	Summary       string   `json:"summary,omitempty"`
	Text          string   `json:"text,omitempty"`
	NextAction    string   `json:"next_action,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	Files         []string `json:"files,omitempty"`
	Tags          []string `json:"tags,omitempty"`

	// link fields. TaskID is the source; ToTaskID and Rel describe the edge.
	// Resolved marks a previously-recorded edge (a blocks edge) as cleared.
	ToTaskID string       `json:"to_task_id,omitempty"`
	Rel      Relationship `json:"rel,omitempty"`
	Resolved bool         `json:"resolved,omitempty"`
}
