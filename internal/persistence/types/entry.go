package types

import "time"

// EntryType enumerates the kinds of journal entries Oplog records.
type EntryType string

const (
	// EntryTypeLog is a simple free-form journal note.
	EntryTypeLog EntryType = "log"
	// EntryTypeCheckpoint captures resumable working context.
	EntryTypeCheckpoint EntryType = "checkpoint"
	// EntryTypeInterrupt marks the point at which work was interrupted.
	EntryTypeInterrupt EntryType = "interrupt"
	// EntryTypeStartWork marks the beginning of a work session.
	EntryTypeStartWork EntryType = "start_work"
	// EntryTypeEndWork marks the completion of a work session.
	EntryTypeEndWork EntryType = "end_work"
)

// Entry is a single append-only record in the journal.
type Entry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`

	Type EntryType `json:"type"`

	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`

	Summary    string `json:"summary,omitempty"`
	NextAction string `json:"next_action,omitempty"`

	OpenQuestions []string `json:"open_questions,omitempty"`
	Files         []string `json:"files,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}
