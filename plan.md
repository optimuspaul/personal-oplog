# Oplog — Design

## What it is

Oplog is a local-first work journal. A working day is not a list of sessions —
it is a *reticulum*: tasks spawn other tasks, interruptions pull you away, and
threads get parked and forgotten. Oplog models that shape directly and, above
all, surfaces the **loose threads** you set aside and never closed.

## The core idea: one event log, many projections

The journal is a single append-only stream of **events**. Nothing else is
stored — not "current focus", not task status, not a project list. Every
read-side view is *derived* by folding the events:

```
events (truth)  ──fold──▶  tasks · focus · projects · loose threads
```

This is what makes the model flexible: to reinterpret history a new way, add a
projection — never a migration. It also removes the only piece of mutable state
the old design had (`current_focus.json`), and with it the bug where starting a
new task silently dropped the previous one.

### Three layers, kept distinct

The earlier sketch conflated them; keeping them separate is what makes loose
threads fall out cleanly:

1. **Durable task graph** — tasks and the permanent edges between them.
2. **Temporal event log** — what happened, in order.
3. **Live attention state** — what's active vs. set aside. *Loose threads live
   here, cross-referenced with the graph.*

## Events

One flat, union-style record (`internal/persistence/types.Event`); which fields
matter depends on `Type`:

| Type           | Meaning                                              | Key fields |
| -------------- | ---------------------------------------------------- | ---------- |
| `task_created` | introduce a task into a project                      | `task_id`, `project`, `name`, `origin_task_id`, `origin_rel` |
| `focus_start`  | begin or resume active work                          | `task_id`, `from_task_id` |
| `park`         | set aside while still open                           | `task_id`, `reason`, `cause_task_id` |
| `checkpoint`   | resumable context                                    | `summary`, `next_action`, `open_questions` |
| `note`         | free-form note                                       | `text` |
| `complete`     | finished                                             | `task_id`, `summary` |
| `abandon`      | dropped, won't resume                                | `task_id`, `summary` (reason) |
| `link`         | task→task edge                                       | `task_id` (from), `to_task_id`, `rel`, `resolved` |

`park.reason` ∈ {`interrupted`, `blocked`, `waiting`, `switched`, `paused`}.
`rel` ∈ {`originated_from`, `interrupts`, `blocks`, `relates_to`}.

Note that an **interruption** is not a graph edge — it is a moment in time: a
`park(reason=interrupted)` on the old task plus a `focus_start` on the new one
(optionally with `origin_rel=interrupts`). Only `blocks`/`relates_to`/
`originated_from` are durable edges; `blocks` is resolvable via a later
`link(..., resolved=true)`.

## Projections (`internal/projection`)

`Build(events)` folds the stream into a `World`, which answers:

- **Tasks** — each with derived `status`: `new` → `active` → `parked`/`blocked`
  → `done`/`abandoned`. `blocked` is an overlay: an open task with an unresolved
  incoming `blocks` edge (or parked specifically because it was blocked).
- **Focus** — the active task with the most recent `focus_start`, or none.
- **Projects** — derived namespaces with task/open counts.
- **LooseThreads(now)** — open tasks that aren't the focus, ranked
  ready-to-resume first (a task once held by a blocker that's now resolved),
  then stalest first.
- **Match(tasks, query)** — fuzzy name lookup for task resolution.

Projections are pure functions over `[]Event`, so they are trivially testable
and never touch storage.

## Layers

```
internal/mcp          MCP tool adapters (thin)
internal/service      append events; answer queries via projections
internal/projection   fold events → tasks/focus/projects/threads (pure)
internal/persistence  Store interface: AppendEvent / ListEvents
  └── jsonl           append-only events.jsonl backend (default)
```

`Store` stays a dumb append/scan boundary so JSONL can later become SQLite,
Postgres, or a remote API without touching tool contracts.

## Interface: one interpreting command

Instead of a command per operation, a single `/oplog <plain language>` command
interprets intent and orchestrates the primitives:

1. Classify the message (start/note/checkpoint/park/complete/abandon/link/read).
2. Resolve the task with `oplog_tasks` (fuzzy). No match → pick/﻿create a project
   via `oplog_projects`. Several → ask.
3. On a switch, check `oplog_focus`: if the wording signals an interruption,
   auto-park the old task and record the lineage; otherwise ask once whether the
   previous task was finished or set aside. Never silently supersede.
4. Record the event(s) and confirm, surfacing any ready-to-resume thread.

The MCP server does no NLP — it offers deterministic primitives and lookups; the
command supplies the interpretation and conversation.

## Storage

```
~/.oplog/
└── events.jsonl   one JSON object per line, append-only
```

## Principles

- **Local first** — all data on disk; fully offline.
- **Append-only, event-sourced** — events are truth; state is derived.
- **AI-agnostic core** — agents are clients of the journal, not part of it.
- **Future-proof storage** — persistence hidden behind an interface.

## Possible future work

- Materialized projection cache / SQLite backend for large logs.
- Timeline and reticulum (spawn-tree) visualizations.
- Daily report: what moved, what's still loose, what's newly unblocked.
- Sync (git / S3 / remote API) and multi-device.
