---
description: Resume work — show the latest checkpoint or last activity
argument-hint: [project] [| task]
allowed-tools: mcp__oplog__oplog_resume
---
Call the `oplog_resume` MCP tool to retrieve resumable context.

Resolve arguments from: $ARGUMENTS
- Format "project | task", or just a project, or empty.
- If empty, call the tool with no `project`/`task` so it resumes the current focus.

The result may be a checkpoint or, when none exists, the most recent entry of
another type (check the `from_checkpoint` and `type` fields). Present it clearly:
project, task, the state/summary, and the next action if present. If it fell
back to a non-checkpoint entry, note that (e.g. "no checkpoint — last activity
was an interrupt"). If nothing is found, say there's no activity for that project
yet. Do nothing else.
