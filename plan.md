# Oplog — Design

## What it is

Oplog is a local-first work journal. A working day is not a list of sessions —
it is a *reticulum*: tasks spawn other tasks, interruptions pull you away, and
threads get parked and forgotten. Oplog models that shape directly and, above
all, surfaces the **loose threads** you set aside and never closed.

## The core idea: one event log, many projections

The journal is a single append-only stream of **events**. Nothing else is
stored — not "current focus", not task status, not a task list. Every read-side
view is *derived* by folding the events:

```
events (truth)  ──fold──▶  tasks · focus · loose threads
```

This is what makes the model flexible: to reinterpret history a new way, add a
projection — never a migration. It also removes the only piece of mutable state
the old design had (`current_focus.json`), and with it the bug where starting a
new task silently dropped the previous one.

### No project concept

An earlier design grouped tasks under a *project*. That layer is gone. A task is
just a task, identified by its id and a free-text name; the graph of task→task
links carries whatever grouping you actually need (a task can originate from, or
relate to, another). Removing projects collapses a required field, a filter
dimension, and a whole read tool, and stops forcing every log entry to declare a
namespace it rarely cares about.

### Three layers, kept distinct

Keeping them separate is what makes loose threads fall out cleanly:

1. **Durable task graph** — tasks and the permanent links between them.
2. **Temporal event log** — what happened, in order.
3. **Live attention state** — what's active vs. set aside. *Loose threads live
   here, cross-referenced with the graph.*

## Events

There is exactly **one write operation**: append a log event. Every event is the
same flat record (`internal/persistence/types.Event`); the `Action` field is the
discriminator and decides which other fields matter:

| Field          | Meaning                                                        |
| -------------- | ------------------------------------------------------------- |
| `id`           | event ULID (sortable)                                          |
| `timestamp`    | when it happened (caller-supplied or now)                     |
| `action`       | one of the eight actions below                                |
| `task_id`      | the task this event concerns                                  |
| `name`         | task name — set when the task is first seen (created on demand) |
| `message`      | free-text describing the task and/or what happened           |
| `link_task_id` | optional related task (the task→task edge)                    |
| `rel`          | relationship of the link, inferred from `action`             |
| `next_action`  | optional, for `checkpoint`: the resumable next step          |

### Actions

`action` ∈ {`start`, `resume`, `complete`, `block`, `note`, `park`, `restart`,
`checkpoint`}.

| Action       | Meaning                                                   | Status effect      | `link` means |
| ------------ | -------------------------------------------------------- | ------------------ | ------------ |
| `start`      | begin a task; **creates it** if the name is new; takes focus | → `active`, focus  | originated-from task |
| `resume`     | return to a parked/blocked task; takes focus            | → `active`, focus  | (rarely used) related task |
| `restart`    | reopen a finished task and begin again; takes focus     | → `active`, focus  | related task |
| `park`       | set aside while still open                              | → `parked`         | what pulled you away |
| `block`      | set aside because something blocks it                  | → `blocked`        | the blocking task |
| `checkpoint` | capture resumable context (`message` + `next_action`)  | (no change)        | related task |
| `note`       | free-form note                                          | (no change)        | related task |
| `complete`   | close a task — `message` says why/how (done, dropped, …) | → `done`           | — |

`complete` is the **only** way to close a task: there is no separate "abandon".
Whether the work shipped, was dropped, or was subsumed is expressed in the
`message`, not in a distinct status — closed is closed.

A task is **created implicitly** by the first `start` that references a name with
no match — there is no separate "create" action. The task's `name` is the
reference you logged against.

### Links replace the link tool

The optional `link_task_id` is the only edge mechanism; its `rel` is inferred
from the action rather than supplied:

- `block` → `blocks` (the linked task blocks this one)
- `start` → `originated_from` (this task spawned from the linked one)
- anything else → `relates_to`

An **interruption** is therefore not a special edge — it is a moment in time:
`park` the task you're leaving, then `start`/`resume` the next one, optionally
`link`ing back to where you came from.

**Block resolution is derived, not declared.** A task blocked on another is
flagged *ready to resume* once the linked blocker reaches `complete`. There is no
`resolved` flag to remember to set.

## Projections (`internal/projection`)

`Build(events)` folds the stream into a `World`, which answers:

- **Tasks** — each with derived `status`: `new` → `active` →
  `parked`/`blocked` → `done`. `blocked` is an overlay: an open task whose most
  recent action is `block` and whose blocker is not yet complete.
- **Focus** — the task whose most recent focus-taking action (`start`/`resume`/
  `restart`) has not since been parked, blocked, or completed; or none.
- **LooseThreads(now)** — open tasks that aren't the focus, ranked
  ready-to-resume first (blocked on a task that has since completed), then
  stalest first.
- **Match(tasks, query)** — fuzzy name lookup for task resolution.

Projections are pure functions over `[]Event`, so they are trivially testable
and never touch storage.

## Layers

```
internal/mcp          MCP tool adapters (thin)
internal/service      append events; answer queries via projections
internal/projection   fold events → tasks/focus/threads (pure)
internal/persistence  Store interface: AppendEvent / ListEvents
  └── jsonl           append-only events.jsonl backend (default)
```

`Store` stays a dumb append/scan boundary so JSONL can later become SQLite,
Postgres, or a remote API without touching tool contracts.

## MCP tools

One write tool, six read tools.

**Write:**

- `oplog_log` — append one event: `{ task, action, message?, timestamp?, link?,
  next_action? }`. `task` and `link` are each an id or a fuzzy name; `action` is
  one of the eight above. This is the *only* write surface.

**Read (derived projections):**

- `oplog_focus` — the task currently in progress, if any.
- `oplog_tasks` — find tasks by fuzzy name / status (task resolution).
- `oplog_threads` — loose threads, ranked ready-to-resume first, then stalest.
- `oplog_context` — reconstruct a task: latest checkpoint + recent events.
- `oplog_recent` — the most recent N events (optionally one action).
- `oplog_search` — search events by task, text, or action.

Dropped relative to the previous design: the seven single-purpose write tools
(`oplog_start`/`park`/`complete`/`abandon`/`checkpoint`/`note`/`link`) collapse
into `oplog_log`, and `oplog_projects` is gone with the project concept.

## Interface: one interpreting command

A single `/oplog <plain language>` command interprets intent and calls
`oplog_log` (and the read tools) for you:

1. Classify the message into an action.
2. Resolve the task with `oplog_tasks` (fuzzy); on no match, a `start` creates it.
3. On a switch, check `oplog_focus`: if the wording signals an interruption,
   auto-`park` the old task and `link` the new one back to it; otherwise ask once
   whether the previous task was finished or set aside. Never silently supersede.
4. Record the event and confirm, surfacing any ready-to-resume thread.

The MCP server does no NLP — it offers one deterministic write and a handful of
lookups; the command supplies the interpretation and conversation.

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

### SQLite backend (estimate)

The `Store` interface is just two methods (`AppendEvent`, `ListEvents`) and
projections are pure folds over `ListEvents` output, so a SQLite backend is
self-contained and low-risk — nothing above persistence changes.

- **Driver:** use `modernc.org/sqlite` (pure Go, cgo-free). This matters because
  `install.sh` ships cross-platform release binaries; cgo would break those
  builds.
- **Schema:** a single `events` table — indexed columns for the filterable
  fields (`id` PK, `ts`, `action`, `task_id`) plus a JSON `raw` blob holding the
  full event. `ListEvents` unmarshals `raw`, so the projection layer is
  untouched and the schema barely changes when events evolve.
- **Indexes:** a B-tree index on `task_id` (the task-id → description lookup that
  drives task resolution and `oplog_context`), and an **FTS5** virtual table over
  `message` so `oplog_search` text queries become a real full-text match instead
  of the current full scan. `modernc.org/sqlite` bundles FTS5, so no extra
  driver work.
- **Filter → SQL:** `EventFilter` maps directly — `task_id =`, `action IN`,
  `ts BETWEEN`, `ORDER BY id DESC LIMIT`; ULIDs sort chronologically so ordering
  is free. Text search hits the FTS5 table.
- **Migration:** a one-shot `events.jsonl` → SQLite importer, and a `--backend`
  flag (or extension sniff) to select the store.
- **Tests:** promote the existing JSONL store tests into a shared `Store`
  conformance suite run against both backends.

**Rough effort: ~2–3 focused days.** Backend + filter mapping ~1 day, shared
conformance suite ~0.5 day, importer ~0.5 day, wiring + install/docs ~0.5 day.

### Other

- Materialized projection cache for very large logs.
- Timeline and reticulum (spawn-tree) visualizations.
- Daily report: what moved, what's still loose, what's newly unblocked.
- Sync (git / S3 / remote API) and multi-device.
