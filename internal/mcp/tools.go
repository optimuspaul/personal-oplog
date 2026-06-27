package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/optimuspaul/personal-oplog/internal/persistence/types"
	"github.com/optimuspaul/personal-oplog/internal/projection"
	"github.com/optimuspaul/personal-oplog/internal/service"
)

// registerTools wires the oplog tools to their service methods: one write tool
// (oplog_log) and a handful of read projections. A single interpreting skill
// composes them into conversational workflows.
func registerTools(server *mcpsdk.Server, svc *service.Service) {
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_log",
		Description: "Record one journal event — the only write tool. Identify the task by task (a ULID id or a fuzzy name; a name with no match creates the task on a start). action is one of: start, resume, restart, park, block, note, checkpoint, complete. message is free text describing the task and/or what happened. link optionally references a related task (the blocker for block, the origin for start); its relationship is inferred from action. next_action is the resumable next step for a checkpoint. timestamp (RFC3339) overrides the default of now.",
	}, logHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_focus",
		Description: "Return the task currently being worked on, if any.",
	}, focusHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_tasks",
		Description: "Find tasks by fuzzy name (query) and/or status. Use this to resolve which task a phrase refers to before recording work against it.",
	}, tasksHandler(svc))

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
		Description: "Return the most recent N events, newest first, optionally limited to one action.",
	}, recentHandler(svc))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "oplog_search",
		Description: "Search events by task, text, or action. Returns most recent first.",
	}, searchHandler(svc))
}

// --- log ---

type logInput struct {
	Task       string `json:"task" jsonschema:"task ULID id or fuzzy name; a name with no match creates the task on a start (required)"`
	Action     string `json:"action" jsonschema:"one of: start, resume, restart, park, block, note, checkpoint, complete"`
	Message    string `json:"message,omitempty" jsonschema:"free text describing the task and/or what happened"`
	Link       string `json:"link,omitempty" jsonschema:"id or fuzzy name of a related task: the blocker for block, the origin for start"`
	NextAction string `json:"next_action,omitempty" jsonschema:"the resumable next step, for a checkpoint"`
	Timestamp  string `json:"timestamp,omitempty" jsonschema:"RFC3339 time the event happened; defaults to now"`
}

func logHandler(svc *service.Service) mcpsdk.ToolHandlerFor[logInput, taskOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in logInput) (*mcpsdk.CallToolResult, taskOutput, error) {
		var ts *time.Time
		if s := strings.TrimSpace(in.Timestamp); s != "" {
			parsed, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return nil, taskOutput{}, fmt.Errorf("invalid timestamp %q: %w", s, err)
			}
			ts = &parsed
		}
		task, err := svc.Log(ctx, service.LogInput{
			Task:       in.Task,
			Action:     types.Action(in.Action),
			Message:    in.Message,
			Link:       in.Link,
			NextAction: in.NextAction,
			Timestamp:  ts,
		})
		if err != nil {
			return nil, taskOutput{}, err
		}
		return textResult(fmt.Sprintf("Logged %s on %s (%s).", in.Action, task.Name, task.Status)), newTaskOutput(task), nil
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
		text := fmt.Sprintf("Currently working on %s (since %s).", task.Name, formatTime(task.LastEventAt))
		return textResult(text), newFocusOutput(task), nil
	}
}

// --- tasks ---

type tasksInput struct {
	Query  string `json:"query,omitempty" jsonschema:"case-insensitive substring to match against task names"`
	Status string `json:"status,omitempty" jsonschema:"limit to this status: new, active, parked, blocked, done"`
}

func tasksHandler(svc *service.Service) mcpsdk.ToolHandlerFor[tasksInput, tasksOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in tasksInput) (*mcpsdk.CallToolResult, tasksOutput, error) {
		tasks, err := svc.ListTasks(ctx, service.ListTasksInput{
			Query:  in.Query,
			Status: projection.TaskStatus(in.Status),
		})
		if err != nil {
			return nil, tasksOutput{}, err
		}
		out := newTasksOutput(tasks)
		return textResult(fmt.Sprintf("Found %d matching %s.", out.Count, plural(out.Count, "task", "tasks"))), out, nil
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
	Limit  int    `json:"limit,omitempty" jsonschema:"number of events to return (default 10)"`
	Action string `json:"action,omitempty" jsonschema:"optional action filter"`
}

func recentHandler(svc *service.Service) mcpsdk.ToolHandlerFor[recentInput, eventsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in recentInput) (*mcpsdk.CallToolResult, eventsOutput, error) {
		events, err := svc.Recent(ctx, service.RecentInput{Limit: in.Limit, Action: types.Action(in.Action)})
		if err != nil {
			return nil, eventsOutput{}, err
		}
		out := newEventsOutput(events)
		return textResult(fmt.Sprintf("%d most recent %s.", out.Count, plural(out.Count, "event", "events"))), out, nil
	}
}

// --- search ---

type searchInput struct {
	TaskID string `json:"task_id,omitempty" jsonschema:"limit to this task"`
	Text   string `json:"text,omitempty" jsonschema:"case-insensitive text to match"`
	Action string `json:"action,omitempty" jsonschema:"filter by action"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of events to return"`
}

func searchHandler(svc *service.Service) mcpsdk.ToolHandlerFor[searchInput, eventsOutput] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput) (*mcpsdk.CallToolResult, eventsOutput, error) {
		events, err := svc.Search(ctx, service.SearchInput{
			TaskID: in.TaskID,
			Text:   in.Text,
			Action: types.Action(in.Action),
			Limit:  in.Limit,
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
		fmt.Fprintf(&b, "- %s — %s, idle %s%s\n",
			th.Name, th.Status, humanizeDuration(th.Idle), marker)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatContext(c projection.Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", c.Task.Name, c.Task.Status)
	if c.LatestCheckpoint != nil {
		cp := c.LatestCheckpoint
		if cp.Message != "" {
			fmt.Fprintf(&b, "\nState:\n%s\n", cp.Message)
		}
		if cp.NextAction != "" {
			fmt.Fprintf(&b, "\nNext action:\n%s\n", cp.NextAction)
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
