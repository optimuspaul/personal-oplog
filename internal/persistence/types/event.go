package types

import "time"

// Action enumerates the kinds of append-only events Oplog records. Every piece
// of derived state — tasks, focus, loose threads — is a projection over a
// stream of these events. Action is the single discriminator on Event.
type Action string

const (
	// ActionStart begins work on a task, creating it when the reference names a
	// task that does not yet exist, and takes focus. A Link records the task
	// this one originated from.
	ActionStart Action = "start"
	// ActionResume returns to a parked or blocked task and takes focus.
	ActionResume Action = "resume"
	// ActionRestart reopens a finished task and begins again, taking focus.
	ActionRestart Action = "restart"
	// ActionPark sets a task aside while leaving it open.
	ActionPark Action = "park"
	// ActionBlock sets a task aside because something blocks it. A Link records
	// the blocking task; the block clears once that blocker completes.
	ActionBlock Action = "block"
	// ActionCheckpoint captures resumable context: Message holds the state,
	// NextAction the next concrete step.
	ActionCheckpoint Action = "checkpoint"
	// ActionNote records a free-form note against a task.
	ActionNote Action = "note"
	// ActionComplete closes a task; Message records why or how. There is no
	// separate "abandon" — closed is closed.
	ActionComplete Action = "complete"
)

// IsValid reports whether a is a known action.
func (a Action) IsValid() bool {
	switch a {
	case ActionStart, ActionResume, ActionRestart, ActionPark,
		ActionBlock, ActionCheckpoint, ActionNote, ActionComplete:
		return true
	}
	return false
}

// TakesFocus reports whether the action makes its task the current focus.
func (a Action) TakesFocus() bool {
	return a == ActionStart || a == ActionResume || a == ActionRestart
}

// Relationship enumerates the kinds of edges between tasks. The relationship of
// a Link is inferred from the Action that recorded it, not supplied directly.
type Relationship string

const (
	// RelOriginatedFrom: this task was spawned out of the linked task (start).
	RelOriginatedFrom Relationship = "originated_from"
	// RelBlocks: the linked task blocks this one (block).
	RelBlocks Relationship = "blocks"
	// RelRelatesTo: a loose association recorded by any other action.
	RelRelatesTo Relationship = "relates_to"
)

// RelForAction returns the relationship a Link carries when recorded by action.
func RelForAction(a Action) Relationship {
	switch a {
	case ActionBlock:
		return RelBlocks
	case ActionStart:
		return RelOriginatedFrom
	default:
		return RelRelatesTo
	}
}

// Event is a single append-only record in the journal. It is a flat,
// union-style struct: which fields are meaningful depends on Action. Zero-valued
// fields are omitted from JSON so each line stays small.
type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Action    Action    `json:"action"`

	// TaskID is the task this event concerns.
	TaskID string `json:"task_id,omitempty"`
	// Name is the task's name, set on the event that first introduces it.
	Name string `json:"name,omitempty"`

	// Message is free text describing the task and/or what happened. It carries
	// the note body, the checkpoint state, and the completion reason.
	Message string `json:"message,omitempty"`
	// NextAction is the resumable next step, set on checkpoints.
	NextAction string `json:"next_action,omitempty"`

	// LinkTaskID optionally points at a related task; Rel is the relationship,
	// inferred from Action.
	LinkTaskID string       `json:"link_task_id,omitempty"`
	Rel        Relationship `json:"rel,omitempty"`
}
