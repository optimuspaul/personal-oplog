---
description: Add a quick note to the oplog journal
argument-hint: <note text>
allowed-tools: mcp__oplog__oplog_log
---
Call the `oplog_log` MCP tool with `text` set to: $ARGUMENTS

Leave `project` and `task` empty so the note inherits the current focus.
Confirm in one line. Do nothing else.
