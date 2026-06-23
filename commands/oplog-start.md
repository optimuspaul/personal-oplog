---
description: Start an oplog work session for a project and task
argument-hint: <project> | <task>
allowed-tools: mcp__oplog__oplog_start_work
---
Call the `oplog_start_work` MCP tool to begin a work session.

Resolve arguments from: $ARGUMENTS
- Text before the first "|" is the `project`; text after it is the `task`.
- If there is no "|", treat the first word as the `project` and the rest as the `task`.
- If `project` is still empty, use the current git repository or directory name.

Call the tool with the resolved `project` and `task`, then confirm in one line. Do nothing else.
