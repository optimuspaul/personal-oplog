---
description: Resume work — show the latest oplog checkpoint
argument-hint: [project] [| task]
allowed-tools: mcp__oplog__oplog_resume, mcp__oplog__oplog_current_focus
---
Call the `oplog_resume` MCP tool to retrieve the most recent checkpoint.

Resolve arguments from: $ARGUMENTS
- Format "project | task", or just a project, or empty.
- If empty, call the tool with no `project`/`task` so it resumes the current focus.

Present the result clearly: project, task, known state (summary), and the next
action. If no checkpoint is found, say so. Do nothing else.
