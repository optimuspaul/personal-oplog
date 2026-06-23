package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// registerTools wires every oplog_* tool to its service method.
func registerTools(server *mcpsdk.Server, svc *service.Service) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_start_work",
		Description: "Begin a work session on a project and task. Sets the current focus.",
	}, startWorkHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_log",
		Description: "Record a free-form note. Project and task default to the current focus.",
	}, logHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_checkpoint",
		Description: "Capture resumable context: current state, next action, and open questions. The primary way to save progress.",
	}, checkpointHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_interrupt",
		Description: "Mark the current task as interrupted, recording the reason and clearing the active focus.",
	}, interruptHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_resume",
		Description: "Retrieve the most recent checkpoint for a project or task so work can be resumed. Defaults to the current focus.",
	}, resumeHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_current_focus",
		Description: "Return the task currently being worked on, if any.",
	}, currentFocusHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_search",
		Description: "Search journal entries by project, task, tags, text, or type. Returns most recent first.",
	}, searchHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_recent",
		Description: "Return the most recent N journal entries, newest first, optionally limited to one entry type.",
	}, recentHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_end_work",
		Description: "Mark the current session complete, recording a summary and clearing the active focus.",
	}, endWorkHandler(svc))
}

// --- start_work ---

type startWorkInput struct {
	Project string `json:"project" jsonschema:"the project or repository being worked on"`
	Task    string `json:"task" jsonschema:"the task or objective within the project"`
}

func startWorkHandler(svc *service.Service) mcpsdk.ToolHandlerFor[startWorkInput, focusOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in startWorkInput) (*mcpsdk.CallToolResult, focusOutput, error) {
		focus, err := svc.StartWork(ctx, service.StartWorkInput{Project: in.Project, Task: in.Task})
		if err != nil {
			return nil, focusOutput{}, err
		}
		out := newFocusOutput(&focus)
		text := fmt.Sprintf("Started work on %s / %s.", focus.Project, focus.Task)
		return textResult(text), out, nil
	}
}

// --- log ---

type logInput struct {
	Project string   `json:"project,omitempty" jsonschema:"project; defaults to the current focus"`
	Task    string   `json:"task,omitempty" jsonschema:"task; defaults to the current focus"`
	Text    string   `json:"text" jsonschema:"the note to record"`
	Tags    []string `json:"tags,omitempty" jsonschema:"optional tags"`
	Files   []string `json:"files,omitempty" jsonschema:"optional related file paths"`
}

func logHandler(svc *service.Service) mcpsdk.ToolHandlerFor[logInput, entryOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in logInput) (*mcpsdk.CallToolResult, entryOutput, error) {
		entry, err := svc.Log(ctx, service.LogInput{
			Project: in.Project,
			Task:    in.Task,
			Text:    in.Text,
			Tags:    in.Tags,
			Files:   in.Files,
		})
		if err != nil {
			return nil, entryOutput{}, err
		}
		text := fmt.Sprintf("Logged note for %s / %s.", entry.Project, entry.Task)
		return textResult(text), newEntryOutput(entry), nil
	}
}

// --- checkpoint ---

type checkpointInput struct {
	Project       string   `json:"project,omitempty" jsonschema:"project; defaults to the current focus"`
	Task          string   `json:"task,omitempty" jsonschema:"task; defaults to the current focus"`
	Summary       string   `json:"summary" jsonschema:"the current state of the work"`
	NextAction    string   `json:"next_action,omitempty" jsonschema:"the next concrete step to take"`
	OpenQuestions []string `json:"open_questions,omitempty" jsonschema:"unresolved questions"`
	Files         []string `json:"files,omitempty" jsonschema:"relevant file paths"`
	Tags          []string `json:"tags,omitempty" jsonschema:"optional tags"`
}

func checkpointHandler(svc *service.Service) mcpsdk.ToolHandlerFor[checkpointInput, entryOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in checkpointInput) (*mcpsdk.CallToolResult, entryOutput, error) {
		entry, err := svc.Checkpoint(ctx, service.CheckpointInput{
			Project:       in.Project,
			Task:          in.Task,
			Summary:       in.Summary,
			NextAction:    in.NextAction,
			OpenQuestions: in.OpenQuestions,
			Files:         in.Files,
			Tags:          in.Tags,
		})
		if err != nil {
			return nil, entryOutput{}, err
		}
		text := fmt.Sprintf("Checkpoint saved for %s / %s.", entry.Project, entry.Task)
		return textResult(text), newEntryOutput(entry), nil
	}
}

// --- interrupt ---

type interruptInput struct {
	Reason string `json:"reason,omitempty" jsonschema:"why work is being interrupted"`
}

func interruptHandler(svc *service.Service) mcpsdk.ToolHandlerFor[interruptInput, entryOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in interruptInput) (*mcpsdk.CallToolResult, entryOutput, error) {
		entry, err := svc.Interrupt(ctx, service.InterruptInput{Reason: in.Reason})
		if err != nil {
			return nil, entryOutput{}, err
		}
		text := fmt.Sprintf("Interrupted %s / %s.", entry.Project, entry.Task)
		return textResult(text), newEntryOutput(entry), nil
	}
}

// --- resume ---

type resumeInput struct {
	Project string `json:"project,omitempty" jsonschema:"project to resume; defaults to the current focus"`
	Task    string `json:"task,omitempty" jsonschema:"task to resume; defaults to the current focus"`
}

func resumeHandler(svc *service.Service) mcpsdk.ToolHandlerFor[resumeInput, resumeOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in resumeInput) (*mcpsdk.CallToolResult, resumeOutput, error) {
		entry, err := svc.Resume(ctx, service.ResumeInput{Project: in.Project, Task: in.Task})
		if err != nil {
			return nil, resumeOutput{}, err
		}
		if entry == nil {
			return textResult("No checkpoint found."), resumeOutput{Found: false}, nil
		}
		return textResult(formatResume(*entry)), newResumeOutput(*entry), nil
	}
}

// --- current_focus ---

type currentFocusInput struct{}

func currentFocusHandler(svc *service.Service) mcpsdk.ToolHandlerFor[currentFocusInput, focusOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ currentFocusInput) (*mcpsdk.CallToolResult, focusOutput, error) {
		focus, err := svc.CurrentFocus(ctx)
		if err != nil {
			return nil, focusOutput{}, err
		}
		if focus == nil {
			return textResult("No active focus."), focusOutput{Active: false}, nil
		}
		text := fmt.Sprintf("Currently working on %s / %s (since %s).",
			focus.Project, focus.Task, formatTime(focus.StartedAt))
		return textResult(text), newFocusOutput(focus), nil
	}
}

// --- search ---

type searchInput struct {
	Project string   `json:"project,omitempty" jsonschema:"limit to this project"`
	Task    string   `json:"task,omitempty" jsonschema:"limit to this task"`
	Tags    []string `json:"tags,omitempty" jsonschema:"require all of these tags"`
	Text    string   `json:"text,omitempty" jsonschema:"case-insensitive text to match"`
	Type    string   `json:"type,omitempty" jsonschema:"filter by entry type: log, checkpoint, interrupt, start_work, end_work"`
	Limit   int      `json:"limit,omitempty" jsonschema:"maximum number of entries to return"`
}

func searchHandler(svc *service.Service) mcpsdk.ToolHandlerFor[searchInput, searchOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput) (*mcpsdk.CallToolResult, searchOutput, error) {
		filter := types.EntryFilter{
			Project: in.Project,
			Task:    in.Task,
			Tags:    in.Tags,
			Text:    in.Text,
			Limit:   in.Limit,
		}
		if in.Type != "" {
			filter.Types = []types.EntryType{types.EntryType(in.Type)}
		}

		entries, err := svc.Search(ctx, filter)
		if err != nil {
			return nil, searchOutput{}, err
		}
		out := newSearchOutput(entries)
		text := fmt.Sprintf("Found %d matching %s.", out.Count, plural(out.Count, "entry", "entries"))
		return textResult(text), out, nil
	}
}

// --- recent ---

type recentInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"number of entries to return (default 10)"`
	Type  string `json:"type,omitempty" jsonschema:"optional entry type filter: log, checkpoint, interrupt, start_work, end_work"`
}

func recentHandler(svc *service.Service) mcpsdk.ToolHandlerFor[recentInput, searchOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in recentInput) (*mcpsdk.CallToolResult, searchOutput, error) {
		entries, err := svc.Recent(ctx, service.RecentInput{
			Limit: in.Limit,
			Type:  types.EntryType(in.Type),
		})
		if err != nil {
			return nil, searchOutput{}, err
		}
		out := newSearchOutput(entries)
		text := fmt.Sprintf("%d most recent %s.", out.Count, plural(out.Count, "entry", "entries"))
		return textResult(text), out, nil
	}
}

// --- end_work ---

type endWorkInput struct {
	Summary string `json:"summary,omitempty" jsonschema:"summary of the completed work"`
}

func endWorkHandler(svc *service.Service) mcpsdk.ToolHandlerFor[endWorkInput, entryOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in endWorkInput) (*mcpsdk.CallToolResult, entryOutput, error) {
		entry, err := svc.EndWork(ctx, service.EndWorkInput{Summary: in.Summary})
		if err != nil {
			return nil, entryOutput{}, err
		}
		text := fmt.Sprintf("Completed work on %s / %s.", entry.Project, entry.Task)
		return textResult(text), newEntryOutput(entry), nil
	}
}

// --- helpers ---

func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}
}

func formatResume(e types.Entry) string {
	var b strings.Builder
	b.WriteString("You were working on:\n\n")
	fmt.Fprintf(&b, "Project: %s\n", e.Project)
	fmt.Fprintf(&b, "Task: %s\n", e.Task)

	if e.Type != types.EntryTypeCheckpoint {
		// No checkpoint existed; this is the most recent entry of another type.
		fmt.Fprintf(&b, "\n(No checkpoint saved — showing the last activity: %s)\n", e.Type)
	}

	if e.Summary != "" {
		label := "Known state"
		switch e.Type {
		case types.EntryTypeInterrupt:
			label = "Interrupted because"
		case types.EntryTypeLog:
			label = "Last note"
		}
		fmt.Fprintf(&b, "\n%s:\n%s\n", label, e.Summary)
	}
	if e.NextAction != "" {
		fmt.Fprintf(&b, "\nNext action:\n%s\n", e.NextAction)
	}
	if len(e.OpenQuestions) > 0 {
		b.WriteString("\nOpen questions:\n")
		for _, q := range e.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
