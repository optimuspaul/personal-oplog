package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// ErrNoActiveFocus is returned by operations that fall back to the current
// focus when none is set.
var ErrNoActiveFocus = errors.New("no active focus")

// ErrTaskNotFound is returned when an operation references an unknown task ID.
var ErrTaskNotFound = errors.New("task not found")

func errFieldRequired(field string) error {
	return fmt.Errorf("%s is required", field)
}

// world reads the full event log and folds it into a queryable projection.
func (s *Service) world(ctx context.Context) (*projection.World, error) {
	events, err := s.store.ListEvents(ctx, types.EventFilter{})
	if err != nil {
		return nil, err
	}
	return projection.Build(events), nil
}

// resolveTaskID validates an explicit task ID, or falls back to the current
// focus when taskID is empty.
func (s *Service) resolveTaskID(ctx context.Context, taskID string) (string, error) {
	w, err := s.world(ctx)
	if err != nil {
		return "", err
	}
	if taskID != "" {
		if w.Task(taskID) == nil {
			return "", fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
		}
		return taskID, nil
	}
	focus := w.Focus()
	if focus == nil {
		return "", ErrNoActiveFocus
	}
	return focus.ID, nil
}

// appendAndProject appends e, then returns the resulting projected view of
// taskID so callers see the task's derived status after the event lands.
func (s *Service) appendAndProject(ctx context.Context, taskID string, e types.Event) (projection.Task, error) {
	if err := s.store.AppendEvent(ctx, e); err != nil {
		return projection.Task{}, err
	}
	w, err := s.world(ctx)
	if err != nil {
		return projection.Task{}, err
	}
	t := w.Task(taskID)
	if t == nil {
		return projection.Task{}, fmt.Errorf("%w: %s", ErrTaskNotFound, taskID)
	}
	return *t, nil
}

// CreateTaskInput introduces a new task. Origin links it to the task it was
// spawned from, when OriginRel is set.
type CreateTaskInput struct {
	Project   string
	Name      string
	OriginID  string
	OriginRel types.Relationship
}

// CreateTask records a task_created event and returns the new task. It does
// not change focus — pair it with Start to begin work.
func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (projection.Task, error) {
	if in.Project == "" {
		return projection.Task{}, fmt.Errorf("create task: %w", errFieldRequired("project"))
	}
	if in.Name == "" {
		return projection.Task{}, fmt.Errorf("create task: %w", errFieldRequired("name"))
	}

	taskID := s.newID()
	e := types.Event{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EventTaskCreated,
		TaskID:    taskID,
		Project:   in.Project,
		Name:      in.Name,
	}
	if in.OriginRel != "" && in.OriginID != "" {
		e.OriginTaskID = in.OriginID
		e.OriginRel = in.OriginRel
	}
	return s.appendAndProject(ctx, taskID, e)
}

// StartInput begins or resumes focus on a task. Provide TaskID to resume an
// existing task, or Project+Name to create one and start it in a single step.
// FromTaskID records the task focus moved away from; with OriginRel set it
// also records why the new task exists (spawned from / interrupting it).
type StartInput struct {
	TaskID     string
	Project    string
	Name       string
	FromTaskID string
	OriginRel  types.Relationship
}

// Start records a focus_start, creating the task first when only Project+Name
// are given. It returns the now-active task.
func (s *Service) Start(ctx context.Context, in StartInput) (projection.Task, error) {
	taskID := in.TaskID
	if taskID == "" {
		created, err := s.CreateTask(ctx, CreateTaskInput{
			Project:   in.Project,
			Name:      in.Name,
			OriginID:  in.FromTaskID,
			OriginRel: in.OriginRel,
		})
		if err != nil {
			return projection.Task{}, fmt.Errorf("start: %w", err)
		}
		taskID = created.ID
	} else if _, err := s.resolveTaskID(ctx, taskID); err != nil {
		return projection.Task{}, fmt.Errorf("start: %w", err)
	}

	e := types.Event{
		ID:         s.newID(),
		Timestamp:  s.now(),
		Type:       types.EventFocusStart,
		TaskID:     taskID,
		FromTaskID: in.FromTaskID,
	}
	return s.appendAndProject(ctx, taskID, e)
}

// ParkInput sets a task aside. TaskID defaults to the current focus. Reason
// defaults to "paused". CauseTaskID records what pulled attention away.
type ParkInput struct {
	TaskID      string
	Reason      types.ParkReason
	CauseTaskID string
}

// Park records a park event for the resolved task.
func (s *Service) Park(ctx context.Context, in ParkInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID)
	if err != nil {
		return projection.Task{}, fmt.Errorf("park: %w", err)
	}
	reason := in.Reason
	if reason == "" {
		reason = types.ParkPaused
	}
	e := types.Event{
		ID:          s.newID(),
		Timestamp:   s.now(),
		Type:        types.EventPark,
		TaskID:      taskID,
		Reason:      reason,
		CauseTaskID: in.CauseTaskID,
	}
	return s.appendAndProject(ctx, taskID, e)
}

// CompleteInput marks a task done. TaskID defaults to the current focus.
type CompleteInput struct {
	TaskID  string
	Summary string
}

// Complete records a complete event for the resolved task.
func (s *Service) Complete(ctx context.Context, in CompleteInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID)
	if err != nil {
		return projection.Task{}, fmt.Errorf("complete: %w", err)
	}
	e := types.Event{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EventComplete,
		TaskID:    taskID,
		Summary:   in.Summary,
	}
	return s.appendAndProject(ctx, taskID, e)
}

// AbandonInput drops a task. TaskID defaults to the current focus.
type AbandonInput struct {
	TaskID string
	Reason string
}

// Abandon records an abandon event for the resolved task.
func (s *Service) Abandon(ctx context.Context, in AbandonInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID)
	if err != nil {
		return projection.Task{}, fmt.Errorf("abandon: %w", err)
	}
	e := types.Event{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EventAbandon,
		TaskID:    taskID,
		Summary:   in.Reason,
	}
	return s.appendAndProject(ctx, taskID, e)
}

// CheckpointInput captures resumable context. TaskID defaults to the focus.
type CheckpointInput struct {
	TaskID        string
	Summary       string
	NextAction    string
	OpenQuestions []string
	Files         []string
	Tags          []string
}

// Checkpoint records a checkpoint event and returns it.
func (s *Service) Checkpoint(ctx context.Context, in CheckpointInput) (types.Event, error) {
	if in.Summary == "" {
		return types.Event{}, fmt.Errorf("checkpoint: %w", errFieldRequired("summary"))
	}
	taskID, err := s.resolveTaskID(ctx, in.TaskID)
	if err != nil {
		return types.Event{}, fmt.Errorf("checkpoint: %w", err)
	}
	e := types.Event{
		ID:            s.newID(),
		Timestamp:     s.now(),
		Type:          types.EventCheckpoint,
		TaskID:        taskID,
		Summary:       in.Summary,
		NextAction:    in.NextAction,
		OpenQuestions: in.OpenQuestions,
		Files:         in.Files,
		Tags:          in.Tags,
	}
	if err := s.store.AppendEvent(ctx, e); err != nil {
		return types.Event{}, fmt.Errorf("checkpoint: %w", err)
	}
	return e, nil
}

// NoteInput records a free-form note. TaskID defaults to the focus.
type NoteInput struct {
	TaskID string
	Text   string
	Tags   []string
	Files  []string
}

// Note records a note event and returns it.
func (s *Service) Note(ctx context.Context, in NoteInput) (types.Event, error) {
	if in.Text == "" {
		return types.Event{}, fmt.Errorf("note: %w", errFieldRequired("text"))
	}
	taskID, err := s.resolveTaskID(ctx, in.TaskID)
	if err != nil {
		return types.Event{}, fmt.Errorf("note: %w", err)
	}
	e := types.Event{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EventNote,
		TaskID:    taskID,
		Text:      in.Text,
		Tags:      in.Tags,
		Files:     in.Files,
	}
	if err := s.store.AppendEvent(ctx, e); err != nil {
		return types.Event{}, fmt.Errorf("note: %w", err)
	}
	return e, nil
}

// LinkInput records a task→task edge. Resolved clears a prior blocks edge.
type LinkInput struct {
	FromTaskID string
	ToTaskID   string
	Rel        types.Relationship
	Resolved   bool
}

// Link records a link event and returns it.
func (s *Service) Link(ctx context.Context, in LinkInput) (types.Event, error) {
	if in.FromTaskID == "" || in.ToTaskID == "" {
		return types.Event{}, fmt.Errorf("link: %w", errFieldRequired("from_task_id and to_task_id"))
	}
	if in.Rel == "" {
		return types.Event{}, fmt.Errorf("link: %w", errFieldRequired("rel"))
	}
	e := types.Event{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EventLink,
		TaskID:    in.FromTaskID,
		ToTaskID:  in.ToTaskID,
		Rel:       in.Rel,
		Resolved:  in.Resolved,
	}
	if err := s.store.AppendEvent(ctx, e); err != nil {
		return types.Event{}, fmt.Errorf("link: %w", err)
	}
	return e, nil
}
