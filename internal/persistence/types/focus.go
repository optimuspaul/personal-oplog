package types

import "time"

// Focus represents what the user is currently working on.
type Focus struct {
	Project   string    `json:"project"`
	Task      string    `json:"task"`
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
}
