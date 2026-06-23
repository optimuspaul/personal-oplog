package mcp

import (
	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
)

// entryOutput is the structured result for tools that produce a single
// journal entry. It mirrors types.Entry with stable JSON field names.
type entryOutput struct {
	ID            string   `json:"id"`
	Timestamp     string   `json:"timestamp"`
	Type          string   `json:"type"`
	Project       string   `json:"project,omitempty"`
	Task          string   `json:"task,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	NextAction    string   `json:"next_action,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	Files         []string `json:"files,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

func newEntryOutput(e types.Entry) entryOutput {
	return entryOutput{
		ID:            e.ID,
		Timestamp:     formatTime(e.Timestamp),
		Type:          string(e.Type),
		Project:       e.Project,
		Task:          e.Task,
		Summary:       e.Summary,
		NextAction:    e.NextAction,
		OpenQuestions: e.OpenQuestions,
		Files:         e.Files,
		Tags:          e.Tags,
	}
}

// focusOutput is the structured result for focus-related tools. Active
// reports whether a task is in progress; the remaining fields are populated
// only when Active is true.
type focusOutput struct {
	Active    bool   `json:"active"`
	Project   string `json:"project,omitempty"`
	Task      string `json:"task,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
}

func newFocusOutput(f *types.Focus) focusOutput {
	if f == nil {
		return focusOutput{Active: false}
	}
	return focusOutput{
		Active:    true,
		Project:   f.Project,
		Task:      f.Task,
		SessionID: f.SessionID,
		StartedAt: formatTime(f.StartedAt),
	}
}

// resumeOutput is the structured result of a resume. Found reports whether
// a checkpoint existed; the remaining fields describe it when it did.
type resumeOutput struct {
	Found         bool     `json:"found"`
	Project       string   `json:"project,omitempty"`
	Task          string   `json:"task,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	NextAction    string   `json:"next_action,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	Timestamp     string   `json:"timestamp,omitempty"`
}

func newResumeOutput(e types.Entry) resumeOutput {
	return resumeOutput{
		Found:         true,
		Project:       e.Project,
		Task:          e.Task,
		Summary:       e.Summary,
		NextAction:    e.NextAction,
		OpenQuestions: e.OpenQuestions,
		Timestamp:     formatTime(e.Timestamp),
	}
}

// searchOutput is the structured result of a search.
type searchOutput struct {
	Count   int           `json:"count"`
	Entries []entryOutput `json:"entries"`
}

func newSearchOutput(entries []types.Entry) searchOutput {
	out := searchOutput{
		Count:   len(entries),
		Entries: make([]entryOutput, 0, len(entries)),
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, newEntryOutput(e))
	}
	return out
}
