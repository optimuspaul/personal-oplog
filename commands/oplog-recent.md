---
description: Show the most recent N oplog entries
argument-hint: <n> [type]
allowed-tools: mcp__oplog__oplog_recent
---
Call the `oplog_recent` MCP tool to list the most recent journal entries.

Resolve arguments from: $ARGUMENTS
- The first token is `limit` (integer). If it is missing or not a number, omit it (the server defaults to 10).
- An optional second token is an entry type (log, checkpoint, interrupt, start_work, end_work) → pass it as `type`.

Present the results most-recent-first as a compact list — one line each:
timestamp, type, project/task, and a short summary. Do nothing else.
