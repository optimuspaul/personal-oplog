// Package jsonl implements the persistence.Store interface on top of a
// local directory of JSON Lines files, the default local-first backend.
//
// Layout (rooted at the directory passed to NewStore):
//
//	<dir>/
//	├── log.jsonl          append-only journal entries, one JSON object per line
//	├── current_focus.json the active focus, or absent when no task is active
//	├── projects/
//	├── sessions/
//	└── backups/
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
	logFileName   = "log.jsonl"
	focusFileName = "current_focus.json"

	// maxLineBytes bounds the size of a single journal entry when scanning.
	maxLineBytes = 4 * 1024 * 1024
)

// Store persists the journal as JSON Lines files under a directory.
//
// A mutex serializes writes and protects the focus file against torn reads;
// reads of the append-only log are line-oriented and tolerate concurrent
// appends.
type Store struct {
	dir string
	mu  sync.Mutex
}

// compile-time assertion that *Store satisfies the persistence contract.
var _ persistence.Store = (*Store)(nil)

// NewStore opens (creating if necessary) a JSONL store rooted at dir.
func NewStore(dir string) (*Store, error) {
	for _, sub := range []string{"", "projects", "sessions", "backups"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create %q: %w", filepath.Join(dir, sub), err)
		}
	}
	return &Store{dir: dir}, nil
}

func (s *Store) logPath() string   { return filepath.Join(s.dir, logFileName) }
func (s *Store) focusPath() string { return filepath.Join(s.dir, focusFileName) }

// AppendEntry durably appends a single entry to the journal.
func (s *Store) AppendEntry(ctx context.Context, entry types.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	line = append(line, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync log: %w", err)
	}
	return nil
}

// ListEntries returns entries matching the filter, most recent first.
func (s *Store) ListEntries(ctx context.Context, filter types.EntryFilter) ([]types.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	f, err := os.Open(s.logPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	var matches []types.Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var entry types.Entry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, fmt.Errorf("parse log entry: %w", err)
		}
		if matchesFilter(entry, filter) {
			matches = append(matches, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Timestamp.After(matches[j].Timestamp)
	})

	if filter.Limit > 0 && len(matches) > filter.Limit {
		matches = matches[:filter.Limit]
	}
	return matches, nil
}

// GetCurrentFocus returns the active focus, or nil if no task is active.
func (s *Store) GetCurrentFocus(ctx context.Context) (*types.Focus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.focusPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read focus: %w", err)
	}

	var focus types.Focus
	if err := json.Unmarshal(data, &focus); err != nil {
		return nil, fmt.Errorf("parse focus: %w", err)
	}
	return &focus, nil
}

// SetCurrentFocus replaces the active focus.
func (s *Store) SetCurrentFocus(ctx context.Context, focus types.Focus) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(focus, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal focus: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeAtomic(s.focusPath(), data)
}

// ClearCurrentFocus removes the active focus, leaving no task active.
func (s *Store) ClearCurrentFocus(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(s.focusPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear focus: %w", err)
	}
	return nil
}

// writeAtomic writes data to path via a temp file and rename, so readers
// never observe a partially written file.
func (s *Store) writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}

// matchesFilter reports whether entry satisfies every set constraint in
// filter. Zero-valued fields impose no constraint.
func matchesFilter(entry types.Entry, filter types.EntryFilter) bool {
	if filter.Project != "" && entry.Project != filter.Project {
		return false
	}
	if filter.Task != "" && entry.Task != filter.Task {
		return false
	}
	if len(filter.Types) > 0 && !slices.Contains(filter.Types, entry.Type) {
		return false
	}
	for _, tag := range filter.Tags {
		if !slices.Contains(entry.Tags, tag) {
			return false
		}
	}
	if filter.Since != nil && entry.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && entry.Timestamp.After(*filter.Until) {
		return false
	}
	if filter.Text != "" && !matchesText(entry, filter.Text) {
		return false
	}
	return true
}

func matchesText(entry types.Entry, text string) bool {
	needle := strings.ToLower(text)
	fields := []string{entry.Project, entry.Task, entry.Summary, entry.NextAction}
	fields = append(fields, entry.OpenQuestions...)
	fields = append(fields, entry.Tags...)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), needle) {
			return true
		}
	}
	return false
}
