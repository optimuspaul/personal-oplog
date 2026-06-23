package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// ErrNoActiveFocus is returned by operations that require an active task
// (interrupt, end work) when none is set.
var ErrNoActiveFocus = errors.New("no active focus")

// StartWorkInput begins a work session on a project/task.
type StartWorkInput struct {
	Project string
	Task    string
}

// StartWork sets the current focus to a fresh session and records a
// start_work entry in the journal. It overwrites any existing focus, so a
// prior unfinished task is silently superseded.
func (s *Service) StartWork(ctx context.Context, in StartWorkInput) (types.Focus, error) {
	if in.Project == "" {
		return types.Focus{}, fmt.Errorf("start work: %w", errFieldRequired("project"))
	}
	if in.Task == "" {
		return types.Focus{}, fmt.Errorf("start work: %w", errFieldRequired("task"))
	}

	now := s.now()
	focus := types.Focus{
		Project:   in.Project,
		Task:      in.Task,
		SessionID: s.newID(),
		StartedAt: now,
	}

	if err := s.store.SetCurrentFocus(ctx, focus); err != nil {
		return types.Focus{}, fmt.Errorf("start work: %w", err)
	}

	entry := types.Entry{
		ID:        s.newID(),
		Timestamp: now,
		Type:      types.EntryTypeStartWork,
		Project:   in.Project,
		Task:      in.Task,
	}
	if err := s.store.AppendEntry(ctx, entry); err != nil {
		return types.Focus{}, fmt.Errorf("start work: record entry: %w", err)
	}
	return focus, nil
}

// CurrentFocus returns the active focus, or nil when no task is active.
func (s *Service) CurrentFocus(ctx context.Context) (*types.Focus, error) {
	focus, err := s.store.GetCurrentFocus(ctx)
	if err != nil {
		return nil, fmt.Errorf("current focus: %w", err)
	}
	return focus, nil
}

// InterruptInput captures why the current task is being set aside.
type InterruptInput struct {
	Reason string
}

// Interrupt records an interrupt entry capturing the active task and clears
// the focus. It returns ErrNoActiveFocus if nothing is active.
func (s *Service) Interrupt(ctx context.Context, in InterruptInput) (types.Entry, error) {
	focus, err := s.store.GetCurrentFocus(ctx)
	if err != nil {
		return types.Entry{}, fmt.Errorf("interrupt: %w", err)
	}
	if focus == nil {
		return types.Entry{}, ErrNoActiveFocus
	}

	entry := types.Entry{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EntryTypeInterrupt,
		Project:   focus.Project,
		Task:      focus.Task,
		Summary:   in.Reason,
	}
	if err := s.store.AppendEntry(ctx, entry); err != nil {
		return types.Entry{}, fmt.Errorf("interrupt: record entry: %w", err)
	}
	if err := s.store.ClearCurrentFocus(ctx); err != nil {
		return types.Entry{}, fmt.Errorf("interrupt: clear focus: %w", err)
	}
	return entry, nil
}

// EndWorkInput closes out the active session.
type EndWorkInput struct {
	Summary string
}

// EndWork records an end_work entry for the active task and clears the
// focus. It returns ErrNoActiveFocus if nothing is active.
func (s *Service) EndWork(ctx context.Context, in EndWorkInput) (types.Entry, error) {
	focus, err := s.store.GetCurrentFocus(ctx)
	if err != nil {
		return types.Entry{}, fmt.Errorf("end work: %w", err)
	}
	if focus == nil {
		return types.Entry{}, ErrNoActiveFocus
	}

	entry := types.Entry{
		ID:        s.newID(),
		Timestamp: s.now(),
		Type:      types.EntryTypeEndWork,
		Project:   focus.Project,
		Task:      focus.Task,
		Summary:   in.Summary,
	}
	if err := s.store.AppendEntry(ctx, entry); err != nil {
		return types.Entry{}, fmt.Errorf("end work: record entry: %w", err)
	}
	if err := s.store.ClearCurrentFocus(ctx); err != nil {
		return types.Entry{}, fmt.Errorf("end work: clear focus: %w", err)
	}
	return entry, nil
}
