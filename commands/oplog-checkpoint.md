---
description: Save an oplog checkpoint (current state + next action)
argument-hint: <summary> [; next: <next action>]
allowed-tools: mcp__oplog__oplog_checkpoint
---
Call the `oplog_checkpoint` MCP tool to capture resumable context.

Resolve arguments from: $ARGUMENTS
- Everything before a "next:" marker is the `summary`; everything after it is the `next_action`.
- If there is no "next:" marker, use all of it as `summary` and leave `next_action` empty.
- Leave `project` and `task` empty so the server uses the current focus.

If $ARGUMENTS is empty, write a concise `summary` of what we just did in this
conversation and set `next_action` to the obvious next step.

Call the tool, then confirm in one line. Do nothing else.
