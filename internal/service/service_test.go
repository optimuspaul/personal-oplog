package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence"
	"github.com/optimuspaul/personal-oplog/internal/persistence/jsonl"
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

var baseTime = time.Date(2026, 6, 23, 20, 15, 0, 0, time.UTC)

// newTestService wires the service to a real JSONL store with a clock that
// advances one second per call (so events are deterministically ordered) and
// sequential IDs.
func newTestService(t *testing.T) (*service.Service, persistence.Store) {
	t.Helper()
	store, err := jsonl.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tick := baseTime
	clock := func() time.Time {
		now := tick
		tick = tick.Add(time.Second)
		return now
	}
	var counter int
	ids := func() string {
		counter++
		return fmt.Sprintf("id-%03d", counter)
	}

	svc := service.New(store, service.WithClock(clock), service.WithIDGenerator(ids))
	return svc, store
}

func allEvents(t *testing.T, store persistence.Store) []types.Event {
	t.Helper()
	events, err := store.ListEvents(context.Background(), types.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return events
}

func mustStart(t *testing.T, svc *service.Service, in service.StartInput) projection.Task {
	t.Helper()
	task, err := svc.Start(context.Background(), in)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return task
}

func TestStartCreatesTaskSetsActiveAndRecordsEvents(t *testing.T) {
	svc, store := newTestService(t)

	task := mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	if task.Project != "DERS" || task.Name != "OAuth" {
		t.Errorf("task project/name = %q/%q", task.Project, task.Name)
	}
	if task.Status != projection.StatusActive {
		t.Errorf("status = %q, want active", task.Status)
	}

	// task_created + focus_start.
	events := allEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	focus, err := svc.Focus(context.Background())
	if err != nil || focus == nil {
		t.Fatalf("Focus: %v / %v", focus, err)
	}
	if focus.ID != task.ID {
		t.Errorf("focus id = %q, want %q", focus.ID, task.ID)
	}
}

func TestStartRequiresProjectAndNameWhenCreating(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Start(context.Background(), service.StartInput{Name: "x"}); err == nil {
		t.Error("expected error when project missing")
	}
	if _, err := svc.Start(context.Background(), service.StartInput{Project: "x"}); err == nil {
		t.Error("expected error when name missing")
	}
}

func TestStartResumesExistingTask(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	task := mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	if _, err := svc.Park(ctx, service.ParkInput{Reason: types.ParkPaused}); err != nil {
		t.Fatalf("Park: %v", err)
	}

	resumed := mustStart(t, svc, service.StartInput{TaskID: task.ID})
	if resumed.ID != task.ID || resumed.Status != projection.StatusActive {
		t.Errorf("resume did not reactivate task: %+v", resumed)
	}
}

func TestStartUnknownTaskErrors(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Start(context.Background(), service.StartInput{TaskID: "nope"}); !errors.Is(err, service.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestParkAndCompleteDeriveStatus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	task := mustStart(t, svc, service.StartInput{Project: "A", Name: "one"})

	parked, err := svc.Park(ctx, service.ParkInput{Reason: types.ParkInterrupted})
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if parked.Status != projection.StatusParked || parked.ParkReason != types.ParkInterrupted {
		t.Errorf("unexpected parked task: %+v", parked)
	}

	// After parking, nothing is in focus, so Complete needs an explicit id.
	done, err := svc.Complete(ctx, service.CompleteInput{TaskID: task.ID, Summary: "shipped"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if done.Status != projection.StatusDone {
		t.Errorf("status = %q, want done", done.Status)
	}
}

func TestParkBlockedDerivesBlockedStatus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "A", Name: "one"})
	parked, err := svc.Park(ctx, service.ParkInput{Reason: types.ParkBlocked})
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if parked.Status != projection.StatusBlocked {
		t.Errorf("park reason blocked should derive blocked status, got %q", parked.Status)
	}
}

func TestResolveFallsBackToFocus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	e, err := svc.Note(ctx, service.NoteInput{Text: "investigated scopes"})
	if err != nil {
		t.Fatalf("Note: %v", err)
	}
	if e.Type != types.EventNote || e.Text != "investigated scopes" {
		t.Errorf("unexpected note: %+v", e)
	}
}

func TestNoteAndCheckpointValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	mustStart(t, svc, service.StartInput{Project: "A", Name: "one"})

	if _, err := svc.Note(ctx, service.NoteInput{}); err == nil {
		t.Error("expected error when note text missing")
	}
	if _, err := svc.Checkpoint(ctx, service.CheckpointInput{}); err == nil {
		t.Error("expected error when checkpoint summary missing")
	}
}

func TestOperationsWithoutFocusError(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.Park(ctx, service.ParkInput{}); !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("Park: expected ErrNoActiveFocus, got %v", err)
	}
	if _, err := svc.Complete(ctx, service.CompleteInput{}); !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("Complete: expected ErrNoActiveFocus, got %v", err)
	}
}

func TestInterruptionLineageAndLooseThreads(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	// Start task A, then get pulled into B (interruption): park A, start B
	// originating from A.
	taskA := mustStart(t, svc, service.StartInput{Project: "ADS", Name: "RPV query"})
	if _, err := svc.Park(ctx, service.ParkInput{Reason: types.ParkInterrupted, CauseTaskID: ""}); err != nil {
		t.Fatalf("Park: %v", err)
	}
	taskB := mustStart(t, svc, service.StartInput{
		Project: "ADS", Name: "prod fire drill",
		FromTaskID: taskA.ID, OriginRel: types.RelInterrupts,
	})

	if taskB.OriginTaskID != taskA.ID || taskB.OriginRel != types.RelInterrupts {
		t.Errorf("lineage not recorded on B: %+v", taskB)
	}

	// A is now a loose thread; B is the focus and must be excluded.
	threads, err := svc.LooseThreads(ctx)
	if err != nil {
		t.Fatalf("LooseThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].ID != taskA.ID {
		t.Fatalf("expected A as the sole loose thread, got %+v", threads)
	}
}

func TestBlockedThenUnblockedIsReadyToResume(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	blocked := mustStart(t, svc, service.StartInput{Project: "P", Name: "needs schema"})
	blocker := mustStart(t, svc, service.StartInput{Project: "P", Name: "schema change"})

	// schema change blocks needs-schema; park the blocked task.
	if _, err := svc.Link(ctx, service.LinkInput{FromTaskID: blocker.ID, ToTaskID: blocked.ID, Rel: types.RelBlocks}); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if _, err := svc.Park(ctx, service.ParkInput{TaskID: blocked.ID, Reason: types.ParkBlocked}); err != nil {
		t.Fatalf("Park: %v", err)
	}

	// Resolve the blocker.
	if _, err := svc.Link(ctx, service.LinkInput{FromTaskID: blocker.ID, ToTaskID: blocked.ID, Rel: types.RelBlocks, Resolved: true}); err != nil {
		t.Fatalf("resolve Link: %v", err)
	}
	// Complete the blocker so it isn't itself a loose thread.
	if _, err := svc.Complete(ctx, service.CompleteInput{TaskID: blocker.ID}); err != nil {
		t.Fatalf("Complete blocker: %v", err)
	}

	threads, err := svc.LooseThreads(ctx)
	if err != nil {
		t.Fatalf("LooseThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].ID != blocked.ID {
		t.Fatalf("expected the unblocked task as the loose thread, got %+v", threads)
	}
	if !threads[0].ReadyToResume {
		t.Error("expected ready_to_resume after blocker resolved")
	}
}

func TestListTasksAndProjects(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "ADS", Name: "monkey task"})
	mustStart(t, svc, service.StartInput{Project: "ADS", Name: "banana task"})
	mustStart(t, svc, service.StartInput{Project: "OTHER", Name: "monkey wrench"})

	matches, err := svc.ListTasks(ctx, service.ListTasksInput{Query: "monkey"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 monkey matches, got %d", len(matches))
	}

	scoped, err := svc.ListTasks(ctx, service.ListTasksInput{Query: "monkey", Project: "ADS"})
	if err != nil {
		t.Fatalf("ListTasks scoped: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Project != "ADS" {
		t.Errorf("project scope failed: %+v", scoped)
	}

	projects, err := svc.Projects(ctx)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(projects))
	}
}

func TestContextReturnsLatestCheckpoint(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	if _, err := svc.Checkpoint(ctx, service.CheckpointInput{Summary: "first"}); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if _, err := svc.Note(ctx, service.NoteInput{Text: "a later note"}); err != nil {
		t.Fatalf("Note: %v", err)
	}
	if _, err := svc.Checkpoint(ctx, service.CheckpointInput{Summary: "second", NextAction: "ship it"}); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	c, err := svc.Context(ctx, "")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if c.LatestCheckpoint == nil || c.LatestCheckpoint.Summary != "second" {
		t.Errorf("expected latest checkpoint 'second', got %+v", c.LatestCheckpoint)
	}
}

func TestRecentNewestFirstWithDefaultLimit(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	for i := range 12 {
		if _, err := svc.Note(ctx, service.NoteInput{Text: fmt.Sprintf("note %02d", i)}); err != nil {
			t.Fatalf("Note: %v", err)
		}
	}

	got, err := svc.Recent(ctx, service.RecentInput{})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != service.DefaultRecentLimit {
		t.Fatalf("got %d events, want default %d", len(got), service.DefaultRecentLimit)
	}
	if got[0].Text != "note 11" {
		t.Errorf("first event = %q, want %q (newest first)", got[0].Text, "note 11")
	}
}

func TestSearchByProjectAndType(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartInput{Project: "DERS", Name: "OAuth"})
	if _, err := svc.Checkpoint(ctx, service.CheckpointInput{Summary: "keep me"}); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	mustStart(t, svc, service.StartInput{Project: "OTHER", Name: "x"})
	if _, err := svc.Checkpoint(ctx, service.CheckpointInput{Summary: "elsewhere"}); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	got, err := svc.Search(ctx, service.SearchInput{Project: "DERS", Type: types.EventCheckpoint})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Summary != "keep me" {
		t.Errorf("expected only the DERS checkpoint, got %+v", got)
	}
}

// --- error propagation via a failing store ---

type failingStore struct {
	appendErr bool
}

func (f failingStore) AppendEvent(context.Context, types.Event) error {
	if f.appendErr {
		return errors.New("disk full")
	}
	return nil
}

func (failingStore) ListEvents(context.Context, types.EventFilter) ([]types.Event, error) {
	return nil, errors.New("read failed")
}

func TestCheckpointPropagatesStoreError(t *testing.T) {
	// ListEvents fails first (during focus resolution), so any error suffices.
	svc := service.New(failingStore{appendErr: true})
	if _, err := svc.Checkpoint(context.Background(), service.CheckpointInput{TaskID: "t", Summary: "s"}); err == nil {
		t.Error("expected store error to propagate")
	}
}

func TestProjectsPropagatesStoreError(t *testing.T) {
	svc := service.New(failingStore{})
	if _, err := svc.Projects(context.Background()); err == nil {
		t.Error("expected store error to propagate")
	}
}
