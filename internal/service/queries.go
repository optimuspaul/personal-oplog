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
// task names (case-insensitive substring), Project and Status narrow further.
type ListTasksInput struct {
	Query   string
	Project string
	Status  projection.TaskStatus
}

// ListTasks returns tasks matching the input, most recently active first. It
// powers task resolution ("which project is the monkey task in?").
func (s *Service) ListTasks(ctx context.Context, in ListTasksInput) ([]projection.Task, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	tasks := projection.Match(w.Tasks(), in.Query)
	out := tasks[:0]
	for _, t := range tasks {
		if in.Project != "" && t.Project != in.Project {
			continue
		}
		if in.Status != "" && t.Status != in.Status {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Projects returns the known projects, most recent activity first.
func (s *Service) Projects(ctx context.Context) ([]projection.Project, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("projects: %w", err)
	}
	return w.Projects(), nil
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
	id, err := s.resolveTaskID(ctx, taskID, query)
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
		if events[i].Type == types.EventCheckpoint {
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

// RecentInput selects the most recent events to return.
type RecentInput struct {
	Limit int
	Type  types.EventType
}

// Recent returns the most recent events, newest first, optionally limited to a
// single type.
func (s *Service) Recent(ctx context.Context, in RecentInput) ([]types.Event, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultRecentLimit
	}
	filter := types.EventFilter{Limit: limit}
	if in.Type != "" {
		filter.Types = []types.EventType{in.Type}
	}
	events, err := s.store.ListEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	return events, nil
}

// SearchInput constrains a journal search. Project filters by task membership;
// the remaining fields map onto the store's event filter.
type SearchInput struct {
	TaskID  string
	Project string
	Text    string
	Type    types.EventType
	Tags    []string
	Limit   int
}

// Search returns events matching the input, most recent first.
func (s *Service) Search(ctx context.Context, in SearchInput) ([]types.Event, error) {
	filter := types.EventFilter{
		TaskID: in.TaskID,
		Text:   in.Text,
		Tags:   in.Tags,
	}
	if in.Type != "" {
		filter.Types = []types.EventType{in.Type}
	}
	// When filtering by project we must see every match before narrowing, so
	// the limit is applied after project membership is resolved.
	if in.Project == "" {
		filter.Limit = in.Limit
	}

	events, err := s.store.ListEvents(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if in.Project == "" {
		return events, nil
	}

	w, err := s.world(ctx)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	inProject := make(map[string]bool)
	for _, t := range w.Tasks() {
		if t.Project == in.Project {
			inProject[t.ID] = true
		}
	}
	kept := make([]types.Event, 0, len(events))
	for _, e := range events {
		if inProject[e.TaskID] {
			kept = append(kept, e)
		}
	}
	if in.Limit > 0 && len(kept) > in.Limit {
		kept = kept[:in.Limit]
	}
	return kept, nil
}
