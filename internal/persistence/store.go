// Package persistence defines the storage abstraction for Oplog.
//
// All persistence is hidden behind the Store interface so the initial
// JSONL file backend can later be replaced with SQLite, Postgres, or a
// remote HTTP API without changing the service or MCP tool contracts.
package persistence

import (
	"context"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// Store is the persistence contract for the journal.
//
// The journal is a pure append-only event log: AppendEvent adds one record,
// ListEvents reads them back. There is no mutable state — current focus, task
// status, and every other view are projections derived from the event stream
// by the projection package.
type Store interface {
	// AppendEvent durably appends a single event to the journal.
	AppendEvent(ctx context.Context, event types.Event) error

	// ListEvents returns events matching the filter, ordered from most recent
	// to oldest.
	ListEvents(ctx context.Context, filter types.EventFilter) ([]types.Event, error)
}
