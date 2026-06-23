---
description: End the current oplog work session
argument-hint: <summary>
allowed-tools: mcp__oplog__oplog_end_work
---
Call the `oplog_end_work` MCP tool with `summary` set to: $ARGUMENTS

If $ARGUMENTS is empty, ask the user: "How would you summarize what you
accomplished this session?" — then wait for their reply and use it as the
summary. Do not summarize the conversation yourself. Once you have it, call the
tool and confirm in one line. Do nothing else.
