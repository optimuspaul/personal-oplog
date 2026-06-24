package types

import "time"

// EventFilter constrains the events returned by ListEvents. Zero-valued fields
// impose no constraint, so an empty filter matches every event.
//
// The store deliberately filters only on fields every event can carry. Higher
// level concerns — "events for a project" or "tasks matching a name" — are
// resolved in the projection layer, which knows how events roll up into tasks.
type EventFilter struct {
	// TaskID, when set, limits results to events concerning this task.
	TaskID string

	// Types, when non-empty, limits results to events of these types.
	Types []EventType
	// Tags, when non-empty, limits results to events carrying all of these tags.
	Tags []string

	// Text, when set, limits results to events whose textual fields match,
	// case-insensitively.
	Text string

	// Since, when set, limits results to events at or after this time.
	Since *time.Time
	// Until, when set, limits results to events at or before this time.
	Until *time.Time

	// Limit, when greater than zero, caps the number of events returned.
	// Implementations return the most recent matching events.
	Limit int
}
