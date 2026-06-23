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
// Entries are append-only. Focus is the single mutable piece of state,
// representing the task the user is currently working on.
type Store interface {
	// AppendEntry durably appends a single entry to the journal.
	AppendEntry(ctx context.Context, entry types.Entry) error

	// ListEntries returns entries matching the filter, ordered from most
	// recent to oldest.
	ListEntries(ctx context.Context, filter types.EntryFilter) ([]types.Entry, error)

	// GetCurrentFocus returns the active focus, or nil if no task is active.
	GetCurrentFocus(ctx context.Context) (*types.Focus, error)

	// SetCurrentFocus replaces the active focus.
	SetCurrentFocus(ctx context.Context, focus types.Focus) error

	// ClearCurrentFocus removes the active focus, leaving no task active.
	ClearCurrentFocus(ctx context.Context) error
}
