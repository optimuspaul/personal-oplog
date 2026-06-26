package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// ErrNoActiveFocus is returned by operations that fall back to the current
// focus when none is set.
var ErrNoActiveFocus = errors.New("no active focus")

// ErrTaskNotFound is returned when an operation references an unknown task ID.
var ErrTaskNotFound = errors.New("task not found")

// ErrAmbiguousTask is returned when a name query matches more than one task and
// can't be narrowed to a single open one.
var ErrAmbiguousTask = errors.New("ambiguous task")

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

// resolveTaskID picks the task an operation acts on, in priority order:
//  1. an explicit task ID (validated against the log);
//  2. a fuzzy name query, resolved to a single task;
//  3. the current focus, when neither is given.
func (s *Service) resolveTaskID(ctx context.Context, taskID, query string) (string, error) {
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
	if q := strings.TrimSpace(query); q != "" {
		return resolveByName(w, q)
	}
	focus := w.Focus()
	if focus == nil {
		return "", ErrNoActiveFocus
	}
	return focus.ID, nil
}

// resolveByName turns a fuzzy name query into a single task ID. A lone match
// wins outright; when several match, a single open task among them breaks the
// tie. Anything else is reported as not-found or ambiguous so the caller can
// fall back to oplog_tasks.
func resolveByName(w *projection.World, query string) (string, error) {
	matches := projection.Match(w.Tasks(), query)
	if len(matches) == 0 {
		return "", fmt.Errorf("%w matching %q", ErrTaskNotFound, query)
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	var open []projection.Task
	for _, t := range matches {
		if t.Status.IsOpen() {
			open = append(open, t)
		}
	}
	if len(open) == 1 {
		return open[0].ID, nil
	}
	candidates := matches
	if len(open) > 1 {
		candidates = open
	}
	return "", fmt.Errorf("%w: %q matches %s", ErrAmbiguousTask, query, describeCandidates(candidates))
}

// describeCandidates renders the competing tasks so an ambiguity error tells the
// caller exactly which IDs to choose between.
func describeCandidates(tasks []projection.Task) string {
	parts := make([]string, 0, len(tasks))
	for _, t := range tasks {
		parts = append(parts, fmt.Sprintf("%s (%s/%s, %s)", t.ID, t.Project, t.Name, t.Status))
	}
	return strings.Join(parts, "; ")
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
	} else if _, err := s.resolveTaskID(ctx, taskID, ""); err != nil {
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
	Query       string
	Reason      types.ParkReason
	CauseTaskID string
}

// Park records a park event for the resolved task.
func (s *Service) Park(ctx context.Context, in ParkInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID, in.Query)
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
	Query   string
	Summary string
}

// Complete records a complete event for the resolved task.
func (s *Service) Complete(ctx context.Context, in CompleteInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID, in.Query)
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
	Query  string
	Reason string
}

// Abandon records an abandon event for the resolved task.
func (s *Service) Abandon(ctx context.Context, in AbandonInput) (projection.Task, error) {
	taskID, err := s.resolveTaskID(ctx, in.TaskID, in.Query)
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
	Query         string
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
	taskID, err := s.resolveTaskID(ctx, in.TaskID, in.Query)
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
	Query  string
	Text   string
	Tags   []string
	Files  []string
}

// Note records a note event and returns it.
func (s *Service) Note(ctx context.Context, in NoteInput) (types.Event, error) {
	if in.Text == "" {
		return types.Event{}, fmt.Errorf("note: %w", errFieldRequired("text"))
	}
	taskID, err := s.resolveTaskID(ctx, in.TaskID, in.Query)
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
