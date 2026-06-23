package types

import "time"

// EntryFilter constrains the entries returned by a list or search query.
// Zero-valued fields are ignored, so an empty filter matches everything.
type EntryFilter struct {
	// Project, when set, limits results to entries for this project.
	Project string
	// Task, when set, limits results to entries for this task.
	Task string

	// Types, when non-empty, limits results to entries of these types.
	Types []EntryType
	// Tags, when non-empty, limits results to entries carrying all of these tags.
	Tags []string

	// Text, when set, limits results to entries whose textual fields match.
	Text string

	// Since, when set, limits results to entries at or after this time.
	Since *time.Time
	// Until, when set, limits results to entries at or before this time.
	Until *time.Time

	// Limit, when greater than zero, caps the number of entries returned.
	// Implementations return the most recent matching entries.
	Limit int
}
