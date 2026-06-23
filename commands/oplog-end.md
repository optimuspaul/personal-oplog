---
description: End the current oplog work session
argument-hint: [summary]
allowed-tools: mcp__oplog__oplog_end_work
---
Call the `oplog_end_work` MCP tool with `summary` set to: $ARGUMENTS

If $ARGUMENTS is empty, write a one-line summary of what was accomplished in this
session and use that. Confirm in one line. Do nothing else.
