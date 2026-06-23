---
description: Save an oplog checkpoint (current state + next action)
argument-hint: <summary> [; next: <next action>]
allowed-tools: mcp__oplog__oplog_checkpoint
---
Call the `oplog_checkpoint` MCP tool to capture resumable context.

Resolve arguments from: $ARGUMENTS
- Everything before a "next:" marker is the `summary`; everything after it is the `next_action`.
- If there is no "next:" marker, use all of it as `summary`.
- Leave `project` and `task` empty so the server uses the current focus.

If $ARGUMENTS is empty, ask the user: "What's the current state, and what's the
next action?" — then wait for their reply and use it. Do not summarize the
conversation yourself. Once you have the details, call the tool and confirm in
one line. Do nothing else.
