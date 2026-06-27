// Package jsonl implements the persistence.Store interface on top of a
// local JSON Lines file, the default local-first backend.
//
// Layout (rooted at the directory passed to NewStore):
//
//	<dir>/
//	└── events.jsonl   append-only event log, one JSON object per line
//
// There is no separate focus or state file: the journal is the single source
// of truth, and all derived views are computed from it.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/optimuspaul/personal-oplog/internal/persistence"
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

const (
	eventsFileName = "events.jsonl"

	// maxLineBytes bounds the size of a single event when scanning.
	maxLineBytes = 4 * 1024 * 1024
)

// Store persists the journal as a JSON Lines file under a directory.
//
// A mutex serializes appends; reads are line-oriented and tolerate concurrent
// appends.
type Store struct {
	dir string
	mu  sync.Mutex
}

// compile-time assertion that *Store satisfies the persistence contract.
var _ persistence.Store = (*Store)(nil)

// NewStore opens (creating if necessary) a JSONL store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) eventsPath() string { return filepath.Join(s.dir, eventsFileName) }

// AppendEvent durably appends a single event to the journal.
func (s *Store) AppendEvent(ctx context.Context, event types.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open events log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync events log: %w", err)
	}
	return nil
}

// ListEvents returns events matching the filter, most recent first.
func (s *Store) ListEvents(ctx context.Context, filter types.EventFilter) ([]types.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := os.Open(s.eventsPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events log: %w", err)
	}
	defer f.Close()

	var matches []types.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var event types.Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("parse event: %w", err)
		}
		if matchesFilter(event, filter) {
			matches = append(matches, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read events log: %w", err)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

// matchesFilter reports whether event satisfies every set constraint in
// filter. Zero-valued fields impose no constraint.
func matchesFilter(event types.Event, filter types.EventFilter) bool {
	if filter.TaskID != "" && event.TaskID != filter.TaskID {
		return false
	}
	if len(filter.Actions) > 0 && !slices.Contains(filter.Actions, event.Action) {
		return false
	}
	if filter.Since != nil && event.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && event.Timestamp.After(*filter.Until) {
		return false
	}
	if filter.Text != "" && !matchesText(event, filter.Text) {
		return false
	}
	return true
}

func matchesText(event types.Event, text string) bool {
	needle := strings.ToLower(text)
	for _, field := range []string{event.Name, event.Message, event.NextAction} {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}
