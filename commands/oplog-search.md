---
description: Search the oplog journal
argument-hint: <text to match, or tag:foo, or project:bar>
allowed-tools: mcp__oplog__oplog_search
---
Call the `oplog_search` MCP tool to search journal entries.

Interpret $ARGUMENTS:
- A bare phrase → pass as the `text` filter.
- `tag:foo` → pass `foo` in the `tags` filter.
- `project:bar` → pass `bar` as the `project` filter.
- `type:checkpoint` (or log/interrupt/start_work/end_work) → pass as the `type` filter.
- Combinations may be mixed; everything not matching a prefix becomes `text`.

Summarize the matches (most recent first): timestamp, project/task, and summary.
Do nothing else.
