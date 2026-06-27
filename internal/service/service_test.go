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

func mustLog(t *testing.T, svc *service.Service, in service.LogInput) projection.Task {
	t.Helper()
	task, err := svc.Log(context.Background(), in)
	if err != nil {
		t.Fatalf("Log(%s %s): %v", in.Action, in.Task, err)
	}
	return task
}

// start is the common case: a start action that creates and focuses a task.
func start(t *testing.T, svc *service.Service, name string) projection.Task {
	t.Helper()
	return mustLog(t, svc, service.LogInput{Task: name, Action: types.ActionStart})
}

func TestStartCreatesTaskSetsActiveAndRecordsOneEvent(t *testing.T) {
	svc, store := newTestService(t)

	task := start(t, svc, "OAuth")
	if task.Name != "OAuth" {
		t.Errorf("task name = %q, want OAuth", task.Name)
	}
	if task.Status != projection.StatusActive {
		t.Errorf("status = %q, want active", task.Status)
	}

	// A start creates the task implicitly — a single event, no separate create.
	if events := allEvents(t, store); len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	focus, err := svc.Focus(context.Background())
	if err != nil || focus == nil {
		t.Fatalf("Focus: %v / %v", focus, err)
	}
	if focus.ID != task.ID {
		t.Errorf("focus id = %q, want %q", focus.ID, task.ID)
	}
}

func TestLogRequiresTaskAndValidAction(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.Log(ctx, service.LogInput{Action: types.ActionStart}); err == nil {
		t.Error("expected error when task reference is missing")
	}
	if _, err := svc.Log(ctx, service.LogInput{Task: "x", Action: "frobnicate"}); !errors.Is(err, service.ErrInvalidAction) {
		t.Errorf("expected ErrInvalidAction, got %v", err)
	}
	if _, err := svc.Log(ctx, service.LogInput{Task: "x", Action: ""}); !errors.Is(err, service.ErrInvalidAction) {
		t.Errorf("expected ErrInvalidAction for empty action, got %v", err)
	}
}

func TestResumeReactivatesParkedTask(t *testing.T) {
	svc, _ := newTestService(t)

	task := start(t, svc, "OAuth")
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionPark})

	resumed := mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionResume})
	if resumed.ID != task.ID || resumed.Status != projection.StatusActive {
		t.Errorf("resume did not reactivate task: %+v", resumed)
	}
}

func TestNonStartActionOnUnknownTaskErrors(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	// resume/note/etc. never create; only start does.
	if _, err := svc.Log(ctx, service.LogInput{Task: "nope", Action: types.ActionResume}); !errors.Is(err, service.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
	if _, err := svc.Log(ctx, service.LogInput{Task: "nope", Action: types.ActionNote, Message: "x"}); !errors.Is(err, service.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound for note, got %v", err)
	}
}

func TestParkAndCompleteDeriveStatus(t *testing.T) {
	svc, _ := newTestService(t)

	task := start(t, svc, "one")

	parked := mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionPark})
	if parked.Status != projection.StatusParked {
		t.Errorf("status = %q, want parked", parked.Status)
	}

	done := mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionComplete, Message: "shipped"})
	if done.Status != projection.StatusDone {
		t.Errorf("status = %q, want done", done.Status)
	}
}

func TestBlockDerivesBlockedStatus(t *testing.T) {
	svc, _ := newTestService(t)

	blocked := start(t, svc, "needs schema")
	blocker := start(t, svc, "schema change")

	got := mustLog(t, svc, service.LogInput{Task: blocked.ID, Action: types.ActionBlock, Link: blocker.ID})
	if got.Status != projection.StatusBlocked {
		t.Errorf("block should derive blocked status, got %q", got.Status)
	}
	if len(got.BlockedBy) != 1 || got.BlockedBy[0] != blocker.ID {
		t.Errorf("BlockedBy = %v, want [%s]", got.BlockedBy, blocker.ID)
	}
}

func TestResolveByNameRecordsAgainstMatch(t *testing.T) {
	svc, _ := newTestService(t)

	target := start(t, svc, "banana split")
	start(t, svc, "unrelated") // move focus elsewhere

	got := mustLog(t, svc, service.LogInput{Task: "banana", Action: types.ActionNote, Message: "peeled"})
	if got.ID != target.ID {
		t.Errorf("note recorded against %q, want %q", got.ID, target.ID)
	}
}

func TestResolveByNameAmbiguousErrors(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	start(t, svc, "monkey task")
	start(t, svc, "monkey wrench")
	if _, err := svc.Log(ctx, service.LogInput{Task: "monkey", Action: types.ActionNote, Message: "x"}); !errors.Is(err, service.ErrAmbiguousTask) {
		t.Errorf("expected ErrAmbiguousTask, got %v", err)
	}
}

func TestResolveByNamePrefersSingleOpenTask(t *testing.T) {
	svc, _ := newTestService(t)

	done := start(t, svc, "monkey task")
	open := start(t, svc, "monkey wrench")
	mustLog(t, svc, service.LogInput{Task: done.ID, Action: types.ActionComplete})

	// "monkey" matches both, but only one is still open, so it resolves.
	got := mustLog(t, svc, service.LogInput{Task: "monkey", Action: types.ActionNote, Message: "x"})
	if got.ID != open.ID {
		t.Errorf("note recorded against %q, want open task %q", got.ID, open.ID)
	}
}

func TestExactIDResolvesOverFuzzyName(t *testing.T) {
	svc, _ := newTestService(t)
	byID := start(t, svc, "monkey task")
	start(t, svc, "monkey wrench") // would be a fuzzy candidate

	got := mustLog(t, svc, service.LogInput{Task: byID.ID, Action: types.ActionNote, Message: "x"})
	if got.ID != byID.ID {
		t.Errorf("note recorded against %q, want %q", got.ID, byID.ID)
	}
}

func TestStartLinkRecordsOrigin(t *testing.T) {
	svc, _ := newTestService(t)

	// Start task A, then get pulled into B: park A, start B linked to A.
	taskA := start(t, svc, "RPV query")
	mustLog(t, svc, service.LogInput{Task: taskA.ID, Action: types.ActionPark})
	taskB := mustLog(t, svc, service.LogInput{Task: "prod fire drill", Action: types.ActionStart, Link: taskA.ID})

	if taskB.OriginTaskID != taskA.ID || taskB.OriginRel != types.RelOriginatedFrom {
		t.Errorf("origin not recorded on B: %+v", taskB)
	}

	// A is now a loose thread; B is the focus and must be excluded.
	threads, err := svc.LooseThreads(context.Background())
	if err != nil {
		t.Fatalf("LooseThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].ID != taskA.ID {
		t.Fatalf("expected A as the sole loose thread, got %+v", threads)
	}
}

func TestBlockedThenUnblockedIsReadyToResume(t *testing.T) {
	svc, _ := newTestService(t)

	blocked := start(t, svc, "needs schema")
	blocker := start(t, svc, "schema change")

	// schema change blocks needs-schema.
	mustLog(t, svc, service.LogInput{Task: blocked.ID, Action: types.ActionBlock, Link: blocker.ID})
	// Completing the blocker clears the block.
	mustLog(t, svc, service.LogInput{Task: blocker.ID, Action: types.ActionComplete})

	threads, err := svc.LooseThreads(context.Background())
	if err != nil {
		t.Fatalf("LooseThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].ID != blocked.ID {
		t.Fatalf("expected the unblocked task as the loose thread, got %+v", threads)
	}
	if !threads[0].ReadyToResume {
		t.Error("expected ready_to_resume after blocker completed")
	}
}

func TestListTasksByNameAndStatus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	start(t, svc, "monkey task")
	start(t, svc, "banana task")
	wrench := start(t, svc, "monkey wrench")
	mustLog(t, svc, service.LogInput{Task: wrench.ID, Action: types.ActionComplete})

	matches, err := svc.ListTasks(ctx, service.ListTasksInput{Query: "monkey"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 monkey matches, got %d", len(matches))
	}

	open, err := svc.ListTasks(ctx, service.ListTasksInput{Query: "monkey", Status: projection.StatusActive})
	if err != nil {
		t.Fatalf("ListTasks scoped: %v", err)
	}
	if len(open) != 1 || open[0].Name != "monkey task" {
		t.Errorf("status scope failed: %+v", open)
	}
}

func TestContextReturnsLatestCheckpoint(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	task := start(t, svc, "OAuth")
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionCheckpoint, Message: "first"})
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionNote, Message: "a later note"})
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionCheckpoint, Message: "second", NextAction: "ship it"})

	c, err := svc.Context(ctx, "", "")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}
	if c.LatestCheckpoint == nil || c.LatestCheckpoint.Message != "second" {
		t.Errorf("expected latest checkpoint 'second', got %+v", c.LatestCheckpoint)
	}
}

func TestContextWithoutFocusErrors(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Context(context.Background(), "", ""); !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("expected ErrNoActiveFocus, got %v", err)
	}
}

func TestRecentNewestFirstWithDefaultLimit(t *testing.T) {
	svc, _ := newTestService(t)

	task := start(t, svc, "OAuth")
	for i := range 12 {
		mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionNote, Message: fmt.Sprintf("note %02d", i)})
	}

	got, err := svc.Recent(context.Background(), service.RecentInput{})
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != service.DefaultRecentLimit {
		t.Fatalf("got %d events, want default %d", len(got), service.DefaultRecentLimit)
	}
	if got[0].Message != "note 11" {
		t.Errorf("first event = %q, want %q (newest first)", got[0].Message, "note 11")
	}
}

func TestSearchByActionAndText(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	task := start(t, svc, "OAuth")
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionCheckpoint, Message: "keep me"})
	mustLog(t, svc, service.LogInput{Task: task.ID, Action: types.ActionNote, Message: "discard"})

	got, err := svc.Search(ctx, service.SearchInput{Action: types.ActionCheckpoint})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Message != "keep me" {
		t.Errorf("expected only the checkpoint, got %+v", got)
	}

	byText, err := svc.Search(ctx, service.SearchInput{Text: "keep"})
	if err != nil {
		t.Fatalf("Search text: %v", err)
	}
	if len(byText) != 1 || byText[0].Message != "keep me" {
		t.Errorf("text search = %+v", byText)
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

func TestLogPropagatesStoreError(t *testing.T) {
	// ListEvents fails first (during resolution), so any error suffices.
	svc := service.New(failingStore{appendErr: true})
	if _, err := svc.Log(context.Background(), service.LogInput{Task: "t", Action: types.ActionStart}); err == nil {
		t.Error("expected store error to propagate")
	}
}

func TestRecentPropagatesStoreError(t *testing.T) {
	svc := service.New(failingStore{})
	if _, err := svc.Recent(context.Background(), service.RecentInput{}); err == nil {
		t.Error("expected store error to propagate")
	}
}
