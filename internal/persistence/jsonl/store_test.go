package jsonl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence"
	"github.com/optimuspaul/personal-oplog/internal/persistence/storetest"
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.NewStore {
		dir := t.TempDir()
		return func() persistence.Store {
			s, err := NewStore(dir)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}
			return s
		}
	})
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func mkNote(t *testing.T, s *Store, id string) {
	t.Helper()
	e := types.Event{ID: id, Timestamp: time.Now(), Action: types.ActionNote, TaskID: "task-1", Message: "a note"}
	if err := s.AppendEvent(context.Background(), e); err != nil {
		t.Fatalf("AppendEvent(%s): %v", id, err)
	}
}

func TestNewStoreCreatesDir(t *testing.T) {
	dir := t.TempDir() + "/nested/store"
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Errorf("expected store dir to exist: %v", err)
	}
}

func TestNewStoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("first NewStore: %v", err)
	}
	if _, err := NewStore(dir); err != nil {
		t.Fatalf("second NewStore on existing dir: %v", err)
	}
}

func TestListEventsIgnoresBlankLines(t *testing.T) {
	s := newTestStore(t)
	mkNote(t, s, "a")

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("\n   \n"); err != nil {
		t.Fatalf("write blank lines: %v", err)
	}
	f.Close()

	got, err := s.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d events, want 1", len(got))
	}
}

func TestListEventsCorruptLineErrors(t *testing.T) {
	s := newTestStore(t)
	mkNote(t, s, "a")

	f, err := os.OpenFile(s.eventsPath(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString("{not valid json\n"); err != nil {
		t.Fatalf("write corrupt line: %v", err)
	}
	f.Close()

	if _, err := s.ListEvents(context.Background(), types.EventFilter{}); err == nil {
		t.Error("expected error on corrupt log line, got nil")
	}
}
