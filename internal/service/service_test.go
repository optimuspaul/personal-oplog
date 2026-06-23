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
	"github.com/optimuspaul/personal-oplog/internal/service"
)

var baseTime = time.Date(2026, 6, 23, 20, 15, 0, 0, time.UTC)

// newTestService wires the service to a real JSONL store with a clock that
// advances one second per call (so entries are deterministically ordered)
// and sequential IDs.
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

func allEntries(t *testing.T, store persistence.Store) []types.Entry {
	t.Helper()
	entries, err := store.ListEntries(context.Background(), types.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	return entries
}

func TestStartWorkSetsFocusAndRecordsEntry(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	focus, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"})
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if focus.Project != "DERS" || focus.Task != "OAuth" {
		t.Errorf("focus project/task = %q/%q", focus.Project, focus.Task)
	}
	if focus.SessionID == "" {
		t.Error("expected a session id")
	}
	if !focus.StartedAt.Equal(baseTime) {
		t.Errorf("StartedAt = %v, want %v", focus.StartedAt, baseTime)
	}

	stored, err := store.GetCurrentFocus(ctx)
	if err != nil || stored == nil {
		t.Fatalf("GetCurrentFocus: %v / %v", stored, err)
	}
	if stored.SessionID != focus.SessionID {
		t.Errorf("stored session id = %q, want %q", stored.SessionID, focus.SessionID)
	}

	entries := allEntries(t, store)
	if len(entries) != 1 || entries[0].Type != types.EntryTypeStartWork {
		t.Fatalf("expected one start_work entry, got %+v", entries)
	}
}

func TestStartWorkValidation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Task: "OAuth"}); err == nil {
		t.Error("expected error when project missing")
	}
	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS"}); err == nil {
		t.Error("expected error when task missing")
	}
}

func TestStartWorkOverwritesFocus(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "A", Task: "one"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "B", Task: "two"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	focus, _ := store.GetCurrentFocus(ctx)
	if focus == nil || focus.Project != "B" {
		t.Errorf("expected focus on B, got %+v", focus)
	}
}

func TestLogUsesFocusFallback(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}

	entry, err := svc.Log(ctx, service.LogInput{Text: "Investigated Auth0 scopes."})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if entry.Project != "DERS" || entry.Task != "OAuth" {
		t.Errorf("expected project/task from focus, got %q/%q", entry.Project, entry.Task)
	}
	if entry.Type != types.EntryTypeLog || entry.Summary != "Investigated Auth0 scopes." {
		t.Errorf("unexpected entry %+v", entry)
	}
}

func TestLogExplicitOverridesFocus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	entry, err := svc.Log(ctx, service.LogInput{Project: "OTHER", Task: "Billing", Text: "note"})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if entry.Project != "OTHER" || entry.Task != "Billing" {
		t.Errorf("explicit project/task not honored: %+v", entry)
	}
}

func TestLogRequiresText(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Log(context.Background(), service.LogInput{Project: "DERS"}); err == nil {
		t.Error("expected error when text missing")
	}
}

func TestLogRequiresProjectWithoutFocus(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.Log(context.Background(), service.LogInput{Text: "orphan note"}); err == nil {
		t.Error("expected error when no project and no focus")
	}
}

func TestCheckpointRequiresSummary(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Checkpoint(context.Background(), service.CheckpointInput{Project: "DERS", Task: "OAuth"})
	if err == nil {
		t.Error("expected error when summary missing")
	}
}

func TestCheckpointStoresAllFields(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	in := service.CheckpointInput{
		Project:       "DERS",
		Task:          "OAuth",
		Summary:       "Password grant passes.",
		NextAction:    "Inspect audience parameter.",
		OpenQuestions: []string{"Is hey-api sending audience correctly?"},
		Files:         []string{"oauth_test.go"},
		Tags:          []string{"oauth"},
	}
	entry, err := svc.Checkpoint(ctx, in)
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if entry.Type != types.EntryTypeCheckpoint {
		t.Errorf("type = %q", entry.Type)
	}

	stored := allEntries(t, store)
	if len(stored) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(stored))
	}
	got := stored[0]
	if got.NextAction != in.NextAction || len(got.OpenQuestions) != 1 || len(got.Files) != 1 || len(got.Tags) != 1 {
		t.Errorf("fields not persisted: %+v", got)
	}
}

func TestResumeReturnsMostRecentCheckpoint(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustCheckpoint(t, svc, service.CheckpointInput{Project: "DERS", Task: "OAuth", Summary: "first"})
	mustLog(t, svc, service.LogInput{Project: "DERS", Task: "OAuth", Text: "a log between checkpoints"})
	mustCheckpoint(t, svc, service.CheckpointInput{Project: "DERS", Task: "OAuth", Summary: "second"})
	// A checkpoint for a different project must not be returned.
	mustCheckpoint(t, svc, service.CheckpointInput{Project: "OTHER", Task: "x", Summary: "elsewhere"})

	got, err := svc.Resume(ctx, service.ResumeInput{Project: "DERS"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got == nil || got.Summary != "second" {
		t.Errorf("expected most recent checkpoint 'second', got %+v", got)
	}
}

func TestResumeNoCheckpointReturnsNil(t *testing.T) {
	svc, _ := newTestService(t)
	got, err := svc.Resume(context.Background(), service.ResumeInput{Project: "DERS"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestResumeFromCurrentFocus(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	mustCheckpoint(t, svc, service.CheckpointInput{Summary: "from focus"})

	got, err := svc.Resume(ctx, service.ResumeInput{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got == nil || got.Summary != "from focus" {
		t.Errorf("expected checkpoint 'from focus', got %+v", got)
	}
}

func TestResumeNoArgsNoFocusErrors(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Resume(context.Background(), service.ResumeInput{})
	if !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("expected ErrNoActiveFocus, got %v", err)
	}
}

func TestInterruptRecordsEntryAndClearsFocus(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	entry, err := svc.Interrupt(ctx, service.InterruptInput{Reason: "Production issue"})
	if err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if entry.Type != types.EntryTypeInterrupt || entry.Summary != "Production issue" {
		t.Errorf("unexpected interrupt entry %+v", entry)
	}
	if entry.Project != "DERS" || entry.Task != "OAuth" {
		t.Errorf("interrupt should capture focused task, got %+v", entry)
	}

	focus, _ := store.GetCurrentFocus(ctx)
	if focus != nil {
		t.Errorf("expected focus cleared, got %+v", focus)
	}
}

func TestInterruptWithoutFocusErrors(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Interrupt(context.Background(), service.InterruptInput{Reason: "x"})
	if !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("expected ErrNoActiveFocus, got %v", err)
	}
}

func TestEndWorkRecordsEntryAndClearsFocus(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.StartWork(ctx, service.StartWorkInput{Project: "DERS", Task: "OAuth"}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	entry, err := svc.EndWork(ctx, service.EndWorkInput{Summary: "OAuth tests passing."})
	if err != nil {
		t.Fatalf("EndWork: %v", err)
	}
	if entry.Type != types.EntryTypeEndWork || entry.Summary != "OAuth tests passing." {
		t.Errorf("unexpected end_work entry %+v", entry)
	}

	focus, _ := store.GetCurrentFocus(ctx)
	if focus != nil {
		t.Errorf("expected focus cleared, got %+v", focus)
	}
}

func TestEndWorkWithoutFocusErrors(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.EndWork(context.Background(), service.EndWorkInput{Summary: "x"})
	if !errors.Is(err, service.ErrNoActiveFocus) {
		t.Errorf("expected ErrNoActiveFocus, got %v", err)
	}
}

func TestSearchDelegatesFilter(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	mustCheckpoint(t, svc, service.CheckpointInput{Project: "DERS", Task: "OAuth", Summary: "keep"})
	mustLog(t, svc, service.LogInput{Project: "OTHER", Task: "x", Text: "drop"})

	got, err := svc.Search(ctx, types.EntryFilter{Types: []types.EntryType{types.EntryTypeCheckpoint}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 || got[0].Summary != "keep" {
		t.Errorf("expected only the checkpoint, got %+v", got)
	}
}

func TestFullWorkflow(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()

	mustStart(t, svc, service.StartWorkInput{Project: "DERS", Task: "OAuth"})
	mustCheckpoint(t, svc, service.CheckpointInput{
		Summary:    "Password grant passes. Client credentials failing.",
		NextAction: "Inspect audience parameter.",
	})
	if _, err := svc.Interrupt(ctx, service.InterruptInput{Reason: "Production issue"}); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	got, err := svc.Resume(ctx, service.ResumeInput{Project: "DERS"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got == nil || got.NextAction != "Inspect audience parameter." {
		t.Errorf("resume lost the checkpoint: %+v", got)
	}

	// start, checkpoint, interrupt => 3 entries.
	if entries := allEntries(t, store); len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}
}

// --- error propagation via a failing store ---

type failingStore struct {
	appendErr bool
}

func (f failingStore) AppendEntry(context.Context, types.Entry) error {
	if f.appendErr {
		return errors.New("disk full")
	}
	return nil
}
func (failingStore) ListEntries(context.Context, types.EntryFilter) ([]types.Entry, error) {
	return nil, errors.New("read failed")
}
func (failingStore) GetCurrentFocus(context.Context) (*types.Focus, error) { return nil, nil }
func (failingStore) SetCurrentFocus(context.Context, types.Focus) error    { return nil }
func (failingStore) ClearCurrentFocus(context.Context) error               { return nil }

func TestCheckpointPropagatesStoreError(t *testing.T) {
	svc := service.New(failingStore{appendErr: true})
	_, err := svc.Checkpoint(context.Background(), service.CheckpointInput{
		Project: "DERS", Task: "OAuth", Summary: "s",
	})
	if err == nil {
		t.Error("expected store error to propagate")
	}
}

func TestSearchPropagatesStoreError(t *testing.T) {
	svc := service.New(failingStore{})
	if _, err := svc.Search(context.Background(), types.EntryFilter{}); err == nil {
		t.Error("expected store error to propagate")
	}
}

// --- helpers ---

func mustStart(t *testing.T, svc *service.Service, in service.StartWorkInput) {
	t.Helper()
	if _, err := svc.StartWork(context.Background(), in); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
}

func mustLog(t *testing.T, svc *service.Service, in service.LogInput) {
	t.Helper()
	if _, err := svc.Log(context.Background(), in); err != nil {
		t.Fatalf("Log: %v", err)
	}
}

func mustCheckpoint(t *testing.T, svc *service.Service, in service.CheckpointInput) {
	t.Helper()
	if _, err := svc.Checkpoint(context.Background(), in); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
}
