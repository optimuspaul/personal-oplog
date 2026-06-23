---
description: Add a quick note to the oplog journal
argument-hint: <note text>
allowed-tools: mcp__oplog__oplog_log
---
Call the `oplog_log` MCP tool with `text` set to: $ARGUMENTS

If $ARGUMENTS is empty, ask the user: "What's the note?" — then wait for their
reply and use it as the text. Leave `project` and `task` empty so the note
inherits the current focus. Once you have the note, call the tool and confirm in
one line. Do nothing else.
