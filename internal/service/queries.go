package service

import (
	"context"
	"fmt"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// DefaultRecentLimit is the number of events Recent returns when no positive
// limit is supplied.
const DefaultRecentLimit = 10

// defaultContextEvents is how many recent events Context returns per task.
const defaultContextEvents = 10

// Focus returns the task currently being worked on, or nil when none is active.
func (s *Service) Focus(ctx context.Context) (*projection.Task, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("focus: %w", err)
	}
	return w.Focus(), nil
}

// ListTasksInput filters the task list. All fields are optional: Query matches
// task names (case-insensitive substring) and Status narrows further.
type ListTasksInput struct {
	Query  string
	Status projection.TaskStatus
}

// ListTasks returns tasks matching the input, most recently active first. It
// powers task resolution ("which task does this phrase mean?").
func (s *Service) ListTasks(ctx context.Context, in ListTasksInput) ([]projection.Task, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	tasks := projection.Match(w.Tasks(), in.Query)
	out := tasks[:0]
	for _, t := range tasks {
		if in.Status != "" && t.Status != in.Status {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// LooseThreads returns open, unfocused tasks ranked by how actionable they are.
func (s *Service) LooseThreads(ctx context.Context) ([]projection.Thread, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("loose threads: %w", err)
	}
	return w.LooseThreads(s.now()), nil
}

// Context returns the resume bundle for a task: the task, its latest
// checkpoint, and its most recent events. taskID takes precedence; query
// resolves a task by name; both empty falls back to the focus.
func (s *Service) Context(ctx context.Context, taskID, query string) (projection.Context, error) {
	id, err := s.resolveContextTask(ctx, taskID, query)
	if err != nil {
		return projection.Context{}, fmt.Errorf("context: %w", err)
	}
	w, err := s.world(ctx)
	if err != nil {
		return projection.Context{}, fmt.Errorf("context: %w", err)
	}
	task := w.Task(id)
	if task == nil {
		return projection.Context{}, fmt.Errorf("context: %w: %s", ErrTaskNotFound, id)
	}

	events, err := s.store.ListEvents(ctx, types.EventFilter{TaskID: id})
	if err != nil {
		return projection.Context{}, fmt.Errorf("context: %w", err)
	}

	out := projection.Context{Task: *task}
	for i := range events {
		if events[i].Action == types.ActionCheckpoint {
			cp := events[i]
			out.LatestCheckpoint = &cp
			break
		}
	}
	if len(events) > defaultContextEvents {
		events = events[:defaultContextEvents]
	}
	out.RecentEvents = events
	return out, nil
}

// resolveContextTask resolves a read target: an explicit ID, a fuzzy name, or
// the current focus when neither is given.
func (s *Service) resolveContextTask(ctx context.Context, taskID, query string) (string, error) {
	if ref := firstNonEmpty(taskID, query); ref != "" {
		return s.resolveExisting(ctx, ref)
	}
	focus, err := s.Focus(ctx)
	if err != nil {
		return "", err
	}
	if focus == nil {
		return "", ErrNoActiveFocus
	}
	return focus.ID, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// RecentInput selects the most recent events to return.
type RecentInput struct {
	Limit  int
	Action types.Action
}

// Recent returns the most recent events, newest first, optionally limited to a
// single action.
func (s *Service) Recent(ctx context.Context, in RecentInput) ([]types.Event, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultRecentLimit
	}
	filter := types.EventFilter{Limit: limit}
	if in.Action != "" {
		filter.Actions = []types.Action{in.Action}
	}
	events, err := s.store.ListEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	return events, nil
}

// SearchInput constrains a journal search; its fields map onto the store's
// event filter.
type SearchInput struct {
	TaskID string
	Text   string
	Action types.Action
	Limit  int
}

// Search returns events matching the input, most recent first.
func (s *Service) Search(ctx context.Context, in SearchInput) ([]types.Event, error) {
	filter := types.EventFilter{
		TaskID: in.TaskID,
		Text:   in.Text,
		Limit:  in.Limit,
	}
	if in.Action != "" {
		filter.Actions = []types.Action{in.Action}
	}
	events, err := s.store.ListEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return events, nil
}
