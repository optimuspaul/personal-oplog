package mcp

import (
	"context"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// registerTools wires every oplog_* tool to its service method. The tools are
// deliberately small, orthogonal primitives: a single interpreting skill
// composes them into conversational workflows.
func registerTools(server *mcpsdk.Server, svc *service.Service) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_start",
		Description: "Begin or resume focus on a task. Pass task_id to resume an existing task, or project+name to create and start a new one. Set from_task_id (and origin_rel) to record the task you switched away from and why the new one exists.",
	}, startHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_park",
		Description: "Set a task aside while leaving it open. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus. reason is one of: interrupted, blocked, waiting, switched, paused. cause_task_id records what pulled attention away.",
	}, parkHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_complete",
		Description: "Mark a task finished. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus.",
	}, completeHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_abandon",
		Description: "Drop a task that will not be resumed. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus.",
	}, abandonHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_checkpoint",
		Description: "Capture resumable context: current state, next action, open questions. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus.",
	}, checkpointHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_note",
		Description: "Record a free-form note. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus.",
	}, noteHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_link",
		Description: "Record a task→task edge. rel is one of: blocks, relates_to, originated_from, interrupts. Set resolved=true to clear a prior blocks edge.",
	}, linkHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_focus",
		Description: "Return the task currently being worked on, if any.",
	}, focusHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_tasks",
		Description: "Find tasks by fuzzy name (query), project, and/or status. Use this to resolve which task a phrase refers to before recording work against it.",
	}, tasksHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_projects",
		Description: "List known projects with task and open-task counts. Use this to offer a project to attach a new task to.",
	}, projectsHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_threads",
		Description: "List loose threads: open tasks that are not the current focus, ranked by how actionable they are (ready-to-resume first, then stalest).",
	}, threadsHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_context",
		Description: "Reconstruct context for a task: its latest checkpoint and most recent events. Identify the task by task_id (a ULID) or task (a name to resolve); both omitted defaults to the current focus.",
	}, contextHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_recent",
		Description: "Return the most recent N events, newest first, optionally limited to one event type.",
	}, recentHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_search",
		Description: "Search events by task, project, text, type, or tags. Returns most recent first.",
	}, searchHandler(svc))
}

// --- start ---

type startInput struct {
	TaskID     string `json:"task_id,omitempty" jsonschema:"id of an existing task to resume; omit to create a new task"`
	Project    string `json:"project,omitempty" jsonschema:"project for a new task (required when task_id is empty)"`
	Name       string `json:"name,omitempty" jsonschema:"name for a new task (required when task_id is empty)"`
	FromTaskID string `json:"from_task_id,omitempty" jsonschema:"id of the task focus is moving away from, if any"`
	OriginRel  string `json:"origin_rel,omitempty" jsonschema:"how a new task relates to from_task_id: originated_from or interrupts"`
}

func startHandler(svc *service.Service) mcpsdk.ToolHandlerFor[startInput, taskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in startInput) (*mcpsdk.CallToolResult, taskOutput, error) {
		task, err := svc.Start(ctx, service.StartInput{
			TaskID:     in.TaskID,
			Project:    in.Project,
			Name:       in.Name,
			FromTaskID: in.FromTaskID,
			OriginRel:  types.Relationship(in.OriginRel),
		})
		if err != nil {
			return nil, taskOutput{}, err
		}
		return textResult(fmt.Sprintf("Working on %s / %s.", task.Project, task.Name)), newTaskOutput(task), nil
	}
}

// --- park ---

type parkInput struct {
	TaskID      string `json:"task_id,omitempty" jsonschema:"id of the task to park; defaults to the current focus"`
	Task        string `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to park, resolved to a single task; alternative to task_id"`
	Reason      string `json:"reason,omitempty" jsonschema:"interrupted, blocked, waiting, switched, or paused"`
	CauseTaskID string `json:"cause_task_id,omitempty" jsonschema:"id of the task that pulled attention away"`
}

func parkHandler(svc *service.Service) mcpsdk.ToolHandlerFor[parkInput, taskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in parkInput) (*mcpsdk.CallToolResult, taskOutput, error) {
		task, err := svc.Park(ctx, service.ParkInput{
			TaskID:      in.TaskID,
			Query:       in.Task,
			Reason:      types.ParkReason(in.Reason),
			CauseTaskID: in.CauseTaskID,
		})
		if err != nil {
			return nil, taskOutput{}, err
		}
		return textResult(fmt.Sprintf("Parked %s (%s).", task.Name, task.ParkReason)), newTaskOutput(task), nil
	}
}

// --- complete ---

type completeInput struct {
	TaskID  string `json:"task_id,omitempty" jsonschema:"id of the task to complete; defaults to the current focus"`
	Task    string `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to complete, resolved to a single task; alternative to task_id"`
	Summary string `json:"summary,omitempty" jsonschema:"summary of the completed work"`
}

func completeHandler(svc *service.Service) mcpsdk.ToolHandlerFor[completeInput, taskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in completeInput) (*mcpsdk.CallToolResult, taskOutput, error) {
		task, err := svc.Complete(ctx, service.CompleteInput{TaskID: in.TaskID, Query: in.Task, Summary: in.Summary})
		if err != nil {
			return nil, taskOutput{}, err
		}
		return textResult(fmt.Sprintf("Completed %s.", task.Name)), newTaskOutput(task), nil
	}
}

// --- abandon ---

type abandonInput struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"id of the task to abandon; defaults to the current focus"`
	Task   string `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to abandon, resolved to a single task; alternative to task_id"`
	Reason string `json:"reason,omitempty" jsonschema:"why the task is being dropped"`
}

func abandonHandler(svc *service.Service) mcpsdk.ToolHandlerFor[abandonInput, taskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in abandonInput) (*mcpsdk.CallToolResult, taskOutput, error) {
		task, err := svc.Abandon(ctx, service.AbandonInput{TaskID: in.TaskID, Query: in.Task, Reason: in.Reason})
		if err != nil {
			return nil, taskOutput{}, err
		}
		return textResult(fmt.Sprintf("Abandoned %s.", task.Name)), newTaskOutput(task), nil
	}
}

// --- checkpoint ---

type checkpointInput struct {
	TaskID        string   `json:"task_id,omitempty" jsonschema:"id of the task to checkpoint; defaults to the current focus"`
	Task          string   `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to checkpoint, resolved to a single task; alternative to task_id"`
	Summary       string   `json:"summary" jsonschema:"the current state of the work"`
	NextAction    string   `json:"next_action,omitempty" jsonschema:"the next concrete step to take"`
	OpenQuestions []string `json:"open_questions,omitempty" jsonschema:"unresolved questions"`
	Files         []string `json:"files,omitempty" jsonschema:"relevant file paths"`
	Tags          []string `json:"tags,omitempty" jsonschema:"optional tags"`
}

func checkpointHandler(svc *service.Service) mcpsdk.ToolHandlerFor[checkpointInput, eventOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in checkpointInput) (*mcpsdk.CallToolResult, eventOutput, error) {
		e, err := svc.Checkpoint(ctx, service.CheckpointInput{
			TaskID:        in.TaskID,
			Query:         in.Task,
			Summary:       in.Summary,
			NextAction:    in.NextAction,
			OpenQuestions: in.OpenQuestions,
			Files:         in.Files,
			Tags:          in.Tags,
		})
		if err != nil {
			return nil, eventOutput{}, err
		}
		return textResult("Checkpoint saved."), newEventOutput(e), nil
	}
}

// --- note ---

type noteInput struct {
	TaskID string   `json:"task_id,omitempty" jsonschema:"id of the task to note against; defaults to the current focus"`
	Task   string   `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to note against, resolved to a single task; alternative to task_id"`
	Text   string   `json:"text" jsonschema:"the note to record"`
	Tags   []string `json:"tags,omitempty" jsonschema:"optional tags"`
	Files  []string `json:"files,omitempty" jsonschema:"optional related file paths"`
}

func noteHandler(svc *service.Service) mcpsdk.ToolHandlerFor[noteInput, eventOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in noteInput) (*mcpsdk.CallToolResult, eventOutput, error) {
		e, err := svc.Note(ctx, service.NoteInput{TaskID: in.TaskID, Query: in.Task, Text: in.Text, Tags: in.Tags, Files: in.Files})
		if err != nil {
			return nil, eventOutput{}, err
		}
		return textResult("Note recorded."), newEventOutput(e), nil
	}
}

// --- link ---

type linkInput struct {
	FromTaskID string `json:"from_task_id" jsonschema:"the source task"`
	ToTaskID   string `json:"to_task_id" jsonschema:"the target task"`
	Rel        string `json:"rel" jsonschema:"blocks, relates_to, originated_from, or interrupts"`
	Resolved   bool   `json:"resolved,omitempty" jsonschema:"true to clear a prior blocks edge"`
}

func linkHandler(svc *service.Service) mcpsdk.ToolHandlerFor[linkInput, eventOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in linkInput) (*mcpsdk.CallToolResult, eventOutput, error) {
		e, err := svc.Link(ctx, service.LinkInput{
			FromTaskID: in.FromTaskID,
			ToTaskID:   in.ToTaskID,
			Rel:        types.Relationship(in.Rel),
			Resolved:   in.Resolved,
		})
		if err != nil {
			return nil, eventOutput{}, err
		}
		verb := "Linked"
		if in.Resolved {
			verb = "Resolved link"
		}
		return textResult(fmt.Sprintf("%s %s → %s (%s).", verb, in.FromTaskID, in.ToTaskID, in.Rel)), newEventOutput(e), nil
	}
}

// --- focus ---

type focusInput struct{}

func focusHandler(svc *service.Service) mcpsdk.ToolHandlerFor[focusInput, focusOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ focusInput) (*mcpsdk.CallToolResult, focusOutput, error) {
		task, err := svc.Focus(ctx)
		if err != nil {
			return nil, focusOutput{}, err
		}
		if task == nil {
			return textResult("No active focus."), focusOutput{Active: false}, nil
		}
		text := fmt.Sprintf("Currently working on %s / %s (since %s).", task.Project, task.Name, formatTime(task.LastEventAt))
		return textResult(text), newFocusOutput(task), nil
	}
}

// --- tasks ---

type tasksInput struct {
	Query   string `json:"query,omitempty" jsonschema:"case-insensitive substring to match against task names"`
	Project string `json:"project,omitempty" jsonschema:"limit to this project"`
	Status  string `json:"status,omitempty" jsonschema:"limit to this status: new, active, parked, blocked, done, abandoned"`
}

func tasksHandler(svc *service.Service) mcpsdk.ToolHandlerFor[tasksInput, tasksOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in tasksInput) (*mcpsdk.CallToolResult, tasksOutput, error) {
		tasks, err := svc.ListTasks(ctx, service.ListTasksInput{
			Query:   in.Query,
			Project: in.Project,
			Status:  projection.TaskStatus(in.Status),
		})
		if err != nil {
			return nil, tasksOutput{}, err
		}
		out := newTasksOutput(tasks)
		return textResult(fmt.Sprintf("Found %d matching %s.", out.Count, plural(out.Count, "task", "tasks"))), out, nil
	}
}

// --- projects ---

type projectsInput struct{}

func projectsHandler(svc *service.Service) mcpsdk.ToolHandlerFor[projectsInput, projectsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ projectsInput) (*mcpsdk.CallToolResult, projectsOutput, error) {
		projects, err := svc.Projects(ctx)
		if err != nil {
			return nil, projectsOutput{}, err
		}
		out := newProjectsOutput(projects)
		return textResult(fmt.Sprintf("%d %s.", out.Count, plural(out.Count, "project", "projects"))), out, nil
	}
}

// --- threads ---

type threadsInput struct{}

func threadsHandler(svc *service.Service) mcpsdk.ToolHandlerFor[threadsInput, threadsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ threadsInput) (*mcpsdk.CallToolResult, threadsOutput, error) {
		threads, err := svc.LooseThreads(ctx)
		if err != nil {
			return nil, threadsOutput{}, err
		}
		out := newThreadsOutput(threads)
		return textResult(formatThreads(threads)), out, nil
	}
}

// --- context ---

type contextInput struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"id of the task to reconstruct; defaults to the current focus"`
	Task   string `json:"task,omitempty" jsonschema:"name (fuzzy) of the task to reconstruct, resolved to a single task; alternative to task_id"`
}

func contextHandler(svc *service.Service) mcpsdk.ToolHandlerFor[contextInput, contextOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextInput) (*mcpsdk.CallToolResult, contextOutput, error) {
		c, err := svc.Context(ctx, in.TaskID, in.Task)
		if err != nil {
			return nil, contextOutput{}, err
		}
		return textResult(formatContext(c)), newContextOutput(c), nil
	}
}

// --- recent ---

type recentInput struct {
	Limit int    `json:"limit,omitempty" jsonschema:"number of events to return (default 10)"`
	Type  string `json:"type,omitempty" jsonschema:"optional event type filter"`
}

func recentHandler(svc *service.Service) mcpsdk.ToolHandlerFor[recentInput, eventsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in recentInput) (*mcpsdk.CallToolResult, eventsOutput, error) {
		events, err := svc.Recent(ctx, service.RecentInput{Limit: in.Limit, Type: types.EventType(in.Type)})
		if err != nil {
			return nil, eventsOutput{}, err
		}
		out := newEventsOutput(events)
		return textResult(fmt.Sprintf("%d most recent %s.", out.Count, plural(out.Count, "event", "events"))), out, nil
	}
}

// --- search ---

type searchInput struct {
	TaskID  string   `json:"task_id,omitempty" jsonschema:"limit to this task"`
	Project string   `json:"project,omitempty" jsonschema:"limit to this project"`
	Text    string   `json:"text,omitempty" jsonschema:"case-insensitive text to match"`
	Type    string   `json:"type,omitempty" jsonschema:"filter by event type"`
	Tags    []string `json:"tags,omitempty" jsonschema:"require all of these tags"`
	Limit   int      `json:"limit,omitempty" jsonschema:"maximum number of events to return"`
}

func searchHandler(svc *service.Service) mcpsdk.ToolHandlerFor[searchInput, eventsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput) (*mcpsdk.CallToolResult, eventsOutput, error) {
		events, err := svc.Search(ctx, service.SearchInput{
			TaskID:  in.TaskID,
			Project: in.Project,
			Text:    in.Text,
			Type:    types.EventType(in.Type),
			Tags:    in.Tags,
			Limit:   in.Limit,
		})
		if err != nil {
			return nil, eventsOutput{}, err
		}
		out := newEventsOutput(events)
		return textResult(fmt.Sprintf("Found %d matching %s.", out.Count, plural(out.Count, "event", "events"))), out, nil
	}
}

// --- helpers ---

func textResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}
}

func formatThreads(threads []projection.Thread) string {
	if len(threads) == 0 {
		return "No loose threads — everything open is either in focus or done."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d loose %s:\n", len(threads), plural(len(threads), "thread", "threads"))
	for _, th := range threads {
		marker := ""
		if th.ReadyToResume {
			marker = " [ready to resume]"
		}
		fmt.Fprintf(&b, "- %s / %s — %s, idle %s%s\n",
			th.Project, th.Name, th.Status, humanizeDuration(th.Idle), marker)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatContext(c projection.Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s / %s (%s)\n", c.Task.Project, c.Task.Name, c.Task.Status)
	if c.LatestCheckpoint != nil {
		cp := c.LatestCheckpoint
		if cp.Summary != "" {
			fmt.Fprintf(&b, "\nState:\n%s\n", cp.Summary)
		}
		if cp.NextAction != "" {
			fmt.Fprintf(&b, "\nNext action:\n%s\n", cp.NextAction)
		}
		if len(cp.OpenQuestions) > 0 {
			b.WriteString("\nOpen questions:\n")
			for _, q := range cp.OpenQuestions {
				fmt.Fprintf(&b, "- %s\n", q)
			}
		}
	} else {
		b.WriteString("\n(No checkpoint saved for this task.)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
