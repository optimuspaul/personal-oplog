# Oplog MCP - Local First Work Journal

## Overview

Oplog is a local-first work journal and context-tracking system designed to help users recover from interruptions and resume work quickly.

The initial implementation will be a local MCP server written in Go that stores work logs in a directory on disk. Future versions can evolve into a networked service while maintaining compatibility with the original storage and tool interfaces.

---

# Goals

The primary goal is to capture and restore working context.

Instead of relying on memory, users and agents can create checkpoints containing:

* What they were working on
* What they discovered
* What remains unresolved
* The next action to take

A user should be able to return to a task after hours or days and reconstruct their mental state in under a minute.

---

# Design Principles

## Local First

All data is stored locally by default.

The application must function completely offline.

## Append-Only

Journal entries should be append-only wherever possible.

This provides:

* Auditability
* Simplicity
* Reliability
* Easy synchronization later

## AI-Agnostic Core

The journal system should not depend on AI.

Agents are clients of the journal, not part of the journal itself.

## MCP Friendly

All core functionality should be exposed through MCP tools.

## Future-Proof Storage

Storage should be abstracted behind interfaces so local files can later be replaced with:

* SQLite
* PostgreSQL
* Remote HTTP APIs
* Cloud synchronization

without changing MCP tool contracts.

---

# Initial Directory Structure

```text
~/.oplog/
├── log.jsonl
├── current_focus.json
├── projects/
├── sessions/
└── backups/
```

---

# Storage Format

## Journal Entries

Store entries in JSONL format.

File:

```text
~/.oplog/log.jsonl
```

Example entry:

```json
{
  "id": "01JXYZABC123",
  "timestamp": "2026-06-23T21:15:00-05:00",
  "type": "checkpoint",
  "project": "DERS",
  "task": "OAuth compliance tests",
  "summary": "Password grant passes. Client credentials failing.",
  "next_action": "Inspect audience parameter.",
  "open_questions": [
    "Is hey-api sending audience correctly?"
  ],
  "files": [
    "auth_test.go",
    "oauth_test.go"
  ],
  "tags": [
    "oauth",
    "auth0"
  ]
}
```

---

# Core Domain Model

## Entry

```go
type Entry struct {
    ID            string
    Timestamp     time.Time

    Type          string

    Project       string
    Task          string

    Summary       string
    NextAction    string

    OpenQuestions []string
    Files         []string
    Tags          []string
}
```

---

## Focus

Represents what the user is currently working on.

```go
type Focus struct {
    Project     string
    Task        string
    SessionID   string
    StartedAt   time.Time
}
```

---

## Session

```go
type Session struct {
    ID          string
    Project     string
    Task        string

    StartedAt   time.Time
    EndedAt     *time.Time

    Status      string
}
```

Status examples:

* active
* interrupted
* completed

---

# Package Structure

```text
cmd/
└── poplog-local-mcp/
    └── main.go

internal/
├── domain/
│   ├── entry.go
│   ├── focus.go
│   └── session.go
│
├── store/
│   ├── store.go
│   └── jsonl_store.go
│
├── search/
│   └── search.go
│
├── service/
│   ├── journal.go
│   └── focus.go
│
└── mcp/
    ├── server.go
    └── tools.go
```

---

# Storage Interface

All persistence should be hidden behind a Store interface.

```go
type Store interface {
    AppendEntry(
        ctx context.Context,
        entry Entry,
    ) error

    ListEntries(
        ctx context.Context,
        filter EntryFilter,
    ) ([]Entry, error)

    GetCurrentFocus(
        ctx context.Context,
    ) (*Focus, error)

    SetCurrentFocus(
        ctx context.Context,
        focus Focus,
    ) error
}
```

Initial implementation:

```go
type JSONLStore struct{}
```

Future implementations:

```go
type SQLiteStore struct{}
type PostgresStore struct{}
type HTTPStore struct{}
```

---

# MCP Tools

## oplog.start_work

Start a work session.

Input:

```json
{
  "project": "DERS",
  "task": "OAuth compliance tests"
}
```

---

## oplog.log

Create a simple journal entry.

Input:

```json
{
  "project": "DERS",
  "task": "OAuth compliance tests",
  "text": "Investigated Auth0 scopes."
}
```

---

## oplog.checkpoint

Capture resumable context.

Input:

```json
{
  "project": "DERS",
  "task": "OAuth compliance tests",
  "summary": "Password flow passes. Client credentials failing.",
  "next_action": "Inspect audience parameter.",
  "open_questions": [
    "Is hey-api sending audience correctly?"
  ]
}
```

This is expected to become the most frequently used tool.

---

## oplog.interrupt

Mark the current task as interrupted.

Input:

```json
{
  "reason": "Production issue"
}
```

Behavior:

* Capture current state
* Mark session interrupted
* Clear active focus

---

## oplog.resume

Retrieve the most recent checkpoint for a project or task.

Input:

```json
{
  "project": "DERS"
}
```

Example output:

```text
Project: DERS
Task: OAuth compliance tests

Last checkpoint:
Password grant passes.
Client credentials fails.

Next action:
Inspect audience parameter.
```

---

## oplog.current_focus

Returns the currently active task.

Example output:

```json
{
  "project": "DERS",
  "task": "OAuth compliance tests",
  "started_at": "2026-06-23T20:15:00-05:00"
}
```

---

## oplog.search

Search journal entries.

Search fields:

* project
* task
* tags
* text

---

## oplog.end_work

Mark a session as completed.

Input:

```json
{
  "summary": "OAuth compliance tests passing."
}
```

---

# Example Workflow

## Begin Work

```text
oplog.start_work
```

Working on:

```text
DERS
OAuth compliance tests
```

---

## Capture Progress

```text
oplog.checkpoint
```

Summary:

```text
Password grant passes.
Client credentials failing.
```

Next action:

```text
Inspect audience parameter.
```

---

## Interrupted

```text
oplog.interrupt
```

Reason:

```text
Production issue.
```

---

## Resume Later

```text
oplog.resume
```

Output:

```text
You were working on:

Project:
DERS

Task:
OAuth compliance tests

Known state:
Password grant passes.
Client credentials failing.

Next action:
Inspect audience parameter.
```

---

# Future Enhancements

## SQLite Backend

Replace JSONL scanning with indexed queries.

## Full-Text Search

Search:

* notes
* summaries
* checkpoints
* tags

## Synchronization

Support:

* Git
* S3
* Remote API

## Multi-Device Support

Use a remote Oplog service.

## Agent Summaries

Allow agents to:

* summarize sessions
* generate daily reports
* identify stale work
* recommend next actions

## Timeline View

Generate a chronological activity history.

## Automatic Context Recovery

Agents can reconstruct context from:

* checkpoints
* recent logs
* active files
* linked repositories

---

# Long-Term Vision

Oplog is an append-only operational journal for humans and agents.

It serves as a durable memory layer that captures work context, interruptions, discoveries, and next actions.

The local file-based implementation is the foundation. MCP enables agents to participate immediately, while the storage abstraction allows the system to evolve into a synchronized service without changing user workflows.
