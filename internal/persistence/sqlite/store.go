package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	_ "modernc.org/sqlite"
)

// timeLayout is the single, sortable representation used for the timestamp
// column. UTC + a fixed layout means lexical ordering matches chronological
// ordering and range filters compare correctly.
const timeLayout = time.RFC3339Nano

var dbSchema = `
CREATE TABLE IF NOT EXISTS events (
    id          TEXT PRIMARY KEY,
    timestamp   TEXT NOT NULL,
    task_id     TEXT NOT NULL,
    action      TEXT NOT NULL,
    name        TEXT NOT NULL,
    message     TEXT NOT NULL,
    next_action TEXT NOT NULL,
    raw         TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_task   ON events(task_id);
CREATE INDEX IF NOT EXISTS idx_events_action ON events(action);
`

type SqliteStore struct {
	db *sql.DB
}

func NewSqliteStore(dir string) (*SqliteStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %q: %w", dir, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "oplog.db"))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if _, err := db.Exec(dbSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("execute schema: %w", err)
	}
	return &SqliteStore{db: db}, nil
}

func (s *SqliteStore) AppendEvent(ctx context.Context, event types.Event) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
        INSERT INTO events (id, timestamp, task_id, action, name, message, next_action, raw)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `,
		event.ID,
		event.Timestamp.UTC().Format(timeLayout),
		event.TaskID,
		string(event.Action),
		event.Name,
		event.Message,
		event.NextAction,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

func (s *SqliteStore) ListEvents(ctx context.Context, filter types.EventFilter) ([]types.Event, error) {
	query, args := makeQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []types.Event
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		var event types.Event
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, fmt.Errorf("unmarshal event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

func makeQuery(filter types.EventFilter) (string, []any) {
	// The indexed columns exist only for filtering/ordering; raw is the source
	// of truth, so that is all we select and unmarshal.
	query := "SELECT raw FROM events"
	var conditions []string
	var args []any

	if filter.TaskID != "" {
		conditions = append(conditions, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if len(filter.Actions) > 0 {
		placeholders := make([]string, len(filter.Actions))
		for i, a := range filter.Actions {
			placeholders[i] = "?"
			args = append(args, string(a))
		}
		conditions = append(conditions, "action IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.Since != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format(timeLayout))
	}
	if filter.Until != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.Until.UTC().Format(timeLayout))
	}
	if filter.Text != "" {
		// Mirror the JSONL backend: match across name, message, and next_action.
		like := "%" + filter.Text + "%"
		conditions = append(conditions,
			"(name LIKE ? COLLATE NOCASE OR message LIKE ? COLLATE NOCASE OR next_action LIKE ? COLLATE NOCASE)")
		args = append(args, like, like, like)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	return query, args
}
