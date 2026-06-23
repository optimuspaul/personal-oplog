package service

import (
	"context"
	"fmt"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// errFieldRequired builds a consistent "field is required" error.
func errFieldRequired(field string) error {
	return fmt.Errorf("%s is required", field)
}

// LogInput records a simple free-form journal note. Project and Task fall
// back to the current focus when left empty.
type LogInput struct {
	Project string
	Task    string
	Text    string
	Tags    []string
	Files   []string
}

// Log appends a free-form note to the journal and returns the stored entry.
func (s *Service) Log(ctx context.Context, in LogInput) (types.Entry, error) {
	if in.Text == "" {
		return types.Entry{}, fmt.Errorf("log: %w", errFieldRequired("text"))
	}

	project, task, err := s.resolveProjectTask(ctx, in.Project, in.Task)
	if err != nil {
		return types.Entry{}, fmt.Errorf("log: %w", err)
	}

	entry := types.Entry{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EntryTypeLog,
		Project:   project,
		Task:      task,
		Summary:   in.Text,
		Tags:      in.Tags,
		Files:     in.Files,
	}
	if err := s.store.AppendEntry(ctx, entry); err != nil {
		return types.Entry{}, fmt.Errorf("log: %w", err)
	}
	return entry, nil
}

// CheckpointInput captures resumable working context. Project and Task fall
// back to the current focus when left empty.
type CheckpointInput struct {
	Project       string
	Task          string
	Summary       string
	NextAction    string
	OpenQuestions []string
	Files         []string
	Tags          []string
}

// Checkpoint appends a checkpoint entry and returns the stored entry. This
// is expected to be the most frequently used operation.
func (s *Service) Checkpoint(ctx context.Context, in CheckpointInput) (types.Entry, error) {
	if in.Summary == "" {
		return types.Entry{}, fmt.Errorf("checkpoint: %w", errFieldRequired("summary"))
	}

	project, task, err := s.resolveProjectTask(ctx, in.Project, in.Task)
	if err != nil {
		return types.Entry{}, fmt.Errorf("checkpoint: %w", err)
	}

	entry := types.Entry{
		ID:            s.newID(),
		Timestamp:     s.now(),
		Type:          types.EntryTypeCheckpoint,
		Project:       project,
		Task:          task,
		Summary:       in.Summary,
		NextAction:    in.NextAction,
		OpenQuestions: in.OpenQuestions,
		Files:         in.Files,
		Tags:          in.Tags,
	}
	if err := s.store.AppendEntry(ctx, entry); err != nil {
		return types.Entry{}, fmt.Errorf("checkpoint: %w", err)
	}
	return entry, nil
}

// Search returns journal entries matching filter, most recent first.
func (s *Service) Search(ctx context.Context, filter types.EntryFilter) ([]types.Entry, error) {
	entries, err := s.store.ListEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return entries, nil
}

// DefaultRecentLimit is the number of entries Recent returns when no positive
// limit is supplied.
const DefaultRecentLimit = 10

// RecentInput selects the most recent entries to return.
type RecentInput struct {
	// Limit caps the number of entries; values <= 0 use DefaultRecentLimit.
	Limit int
	// Type, when set, restricts results to that entry type.
	Type types.EntryType
}

// Recent returns the most recent entries, newest first, optionally limited to
// a single entry type. It is a purpose-built shortcut over Search for the
// common "show me the last N" case.
func (s *Service) Recent(ctx context.Context, in RecentInput) ([]types.Entry, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultRecentLimit
	}
	filter := types.EntryFilter{Limit: limit}
	if in.Type != "" {
		filter.Types = []types.EntryType{in.Type}
	}

	entries, err := s.store.ListEntries(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	return entries, nil
}

// ResumeInput selects the context to resume. At least one of Project or
// Task must be set; empty fields fall back to the current focus.
type ResumeInput struct {
	Project string
	Task    string
}

// Resume returns the most recent checkpoint for the requested project/task,
// or nil if there is none. When both fields are empty it resumes the
// current focus.
func (s *Service) Resume(ctx context.Context, in ResumeInput) (*types.Entry, error) {
	project, task := in.Project, in.Task
	if project == "" && task == "" {
		focus, err := s.store.GetCurrentFocus(ctx)
		if err != nil {
			return nil, fmt.Errorf("resume: %w", err)
		}
		if focus == nil {
			return nil, fmt.Errorf("resume: %w", ErrNoActiveFocus)
		}
		project, task = focus.Project, focus.Task
	}

	entries, err := s.store.ListEntries(ctx, types.EntryFilter{
		Project: project,
		Task:    task,
		Types:   []types.EntryType{types.EntryTypeCheckpoint},
		Limit:   1,
	})
	if err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

// resolveProjectTask fills empty project/task fields from the current focus
// and requires that a project be known either way.
func (s *Service) resolveProjectTask(ctx context.Context, project, task string) (string, string, error) {
	if project != "" && task != "" {
		return project, task, nil
	}

	focus, err := s.store.GetCurrentFocus(ctx)
	if err != nil {
		return "", "", err
	}
	if focus != nil {
		if project == "" {
			project = focus.Project
		}
		if task == "" {
			task = focus.Task
		}
	}

	if project == "" {
		return "", "", errFieldRequired("project")
	}
	return project, task, nil
}
