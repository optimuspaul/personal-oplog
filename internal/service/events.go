package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
)

// ErrNoActiveFocus is returned by operations that fall back to the current
// focus when none is set.
var ErrNoActiveFocus = errors.New("no active focus")

// ErrTaskNotFound is returned when an operation references an unknown task.
var ErrTaskNotFound = errors.New("task not found")

// ErrAmbiguousTask is returned when a name query matches more than one task and
// can't be narrowed to a single open one.
var ErrAmbiguousTask = errors.New("ambiguous task")

// ErrInvalidAction is returned when a log action is empty or unrecognized.
var ErrInvalidAction = errors.New("invalid action")

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

// resolveExisting picks an existing task from a reference that is either a task
// ID or a fuzzy name. It does not create tasks.
func (s *Service) resolveExisting(ctx context.Context, ref string) (string, error) {
	w, err := s.world(ctx)
	if err != nil {
		return "", err
	}
	return resolveRef(w, ref)
}

// resolveRef turns a task reference (ID or fuzzy name) into a single task ID
// against an already-built world.
func resolveRef(w *projection.World, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errFieldRequired("task")
	}
	if w.Task(ref) != nil {
		return ref, nil
	}
	return resolveByName(w, ref)
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
		parts = append(parts, fmt.Sprintf("%s (%s, %s)", t.ID, t.Name, t.Status))
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

// LogInput records a single journal event — the only write operation. Task is a
// reference (ID or fuzzy name); an unmatched name creates a task, but only for a
// start action. Link optionally points at a related task; its relationship is
// inferred from Action. Timestamp defaults to now.
type LogInput struct {
	Task       string
	Action     types.Action
	Message    string
	Link       string
	NextAction string
	Timestamp  *time.Time
}

// Log appends one event and returns the task's resulting derived view. It
// resolves (or, for start, creates) the referenced task, resolves any link, and
// records the relationship the action implies.
func (s *Service) Log(ctx context.Context, in LogInput) (projection.Task, error) {
	if !in.Action.IsValid() {
		return projection.Task{}, fmt.Errorf("log: %w: %q", ErrInvalidAction, in.Action)
	}
	ref := strings.TrimSpace(in.Task)
	if ref == "" {
		return projection.Task{}, fmt.Errorf("log: %w", errFieldRequired("task"))
	}

	w, err := s.world(ctx)
	if err != nil {
		return projection.Task{}, fmt.Errorf("log: %w", err)
	}

	// Resolve the task; a start may create one when nothing matches.
	taskID, err := resolveRef(w, ref)
	name := ""
	if err != nil {
		if in.Action == types.ActionStart && errors.Is(err, ErrTaskNotFound) {
			taskID = s.newID()
			name = ref
		} else {
			return projection.Task{}, fmt.Errorf("log: %w", err)
		}
	}

	// Resolve an optional link to an existing task.
	var linkID string
	if l := strings.TrimSpace(in.Link); l != "" {
		linkID, err = resolveRef(w, l)
		if err != nil {
			return projection.Task{}, fmt.Errorf("log: link: %w", err)
		}
	}

	ts := s.now()
	if in.Timestamp != nil {
		ts = *in.Timestamp
	}

	e := types.Event{
		ID:         s.newID(),
		Timestamp:  ts,
		Action:     in.Action,
		TaskID:     taskID,
		Name:       name,
		Message:    in.Message,
		NextAction: in.NextAction,
	}
	if linkID != "" {
		e.LinkTaskID = linkID
		e.Rel = types.RelForAction(in.Action)
	}
	return s.appendAndProject(ctx, taskID, e)
}
