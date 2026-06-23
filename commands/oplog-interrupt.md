---
description: Mark the current oplog task as interrupted and clear focus
argument-hint: <reason>
allowed-tools: mcp__oplog__oplog_interrupt
---
Call the `oplog_interrupt` MCP tool with `reason` set to: $ARGUMENTS

If $ARGUMENTS is empty, ask the user: "Why are you interrupting? (a brief reason)"
— then wait for their reply and use it as the reason. Once you have it, call the
tool and confirm in one line. Do nothing else.
