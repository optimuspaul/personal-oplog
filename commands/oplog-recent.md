---
description: Show the most recent N oplog entries
argument-hint: <n> [type]
allowed-tools: mcp__oplog__oplog_search
---
Call the `oplog_search` MCP tool to list the most recent journal entries.

Resolve arguments from: $ARGUMENTS
- The first token is the number of entries (`limit`). If it is missing or not a number, use 10.
- An optional second token is an entry type (log, checkpoint, interrupt, start_work, end_work) → pass it as `type`.
- Pass no other filters (no project, task, tags, or text).

Call the tool with the resolved `limit` (and `type` if given). Present the
results most-recent-first as a compact list — one line each: timestamp, type,
project/task, and a short summary. Do nothing else.
