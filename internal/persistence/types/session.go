package types

import "time"

// SessionStatus enumerates the lifecycle states of a work session.
type SessionStatus string

const (
	// SessionStatusActive indicates the session is currently in progress.
	SessionStatusActive SessionStatus = "active"
	// SessionStatusInterrupted indicates the session was interrupted before completion.
	SessionStatusInterrupted SessionStatus = "interrupted"
	// SessionStatusCompleted indicates the session finished normally.
	SessionStatusCompleted SessionStatus = "completed"
)

// Session groups a span of work on a single task.
type Session struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Task    string `json:"task"`

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`

	Status SessionStatus `json:"status"`
}
