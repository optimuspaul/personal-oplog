# Oplog — Local-First Work Journal (MCP)

Oplog is a local-first work journal and context-tracking system. It models a
day as what it really is — a web of tasks that spawn, interrupt, and block one
another — and surfaces the **loose threads** you set aside and never closed.

The journal is a single **append-only event stream**. Every read-side view —
the current focus, each task's status, the open projects, the loose threads — is
*derived* from those events, so the same history can be reinterpreted in new
ways without migrating data. It is stored as a plain file on disk, works
completely offline, and is exposed over the
[Model Context Protocol (MCP)](https://modelcontextprotocol.io), letting agents
participate directly.

### Model

- **Event** — one append-only record: a task created, focus started, parked,
  checkpointed, noted, completed, abandoned, or two tasks linked.
- **Task** — belongs to a project; its status (`new`, `active`, `parked`,
  `blocked`, `done`, `abandoned`) is derived from its events.
- **Relationship** — a task→task edge: `originated_from`, `interrupts`,
  `blocks` (resolvable), or `relates_to`.
- **Focus** — the one active task, derived (no stored mutable state).
- **Loose thread** — an open task that isn't the focus, ranked by staleness;
  a task held by a blocker that has since been resolved is flagged
  *ready to resume*.

## How it works

```
MCP client (e.g. Claude Code)
        │  stdio (JSON-RPC)
        ▼
  cmd/poplog-local-mcp       ← entrypoint
        │
  internal/mcp               ← MCP tool adapters
        │
  internal/service           ← write events + answer queries (IDs, timestamps, validation)
        │
  internal/projection        ← folds the event stream into tasks, focus, threads
        │
  internal/persistence       ← Store interface
   └── internal/persistence/jsonl  ← append-only JSONL backend (default)
```

Storage lives behind a `Store` interface, so the JSONL backend can later be
swapped for SQLite, Postgres, or a remote API without changing the MCP tool
contracts.

### Data layout

Data is written under `~/.oplog` by default (override with `--dir`):

```
~/.oplog/
└── events.jsonl   append-only event log (one JSON object per line)
```

That single file is the whole journal — there is no separate state or focus
file to keep in sync.

## Install

The `install.sh` script builds (or downloads) the server, installs it, and
configures your MCP client.

```bash
# From a checkout (builds from source; requires Go 1.26+):
./install.sh

# Standalone (downloads the latest GitHub release):
curl -fsSL https://raw.githubusercontent.com/optimuspaul/personal-oplog/main/install.sh | bash
```

It auto-detects whether it is running inside a checkout: if so it builds from
source, otherwise it downloads the latest release binary for your platform.
Run interactively to pick a client, or pass options:

```bash
./install.sh --client claude            # claude | claude-desktop | cursor | codex | all | none
./install.sh --client all --dir ~/work-journal
./install.sh --prefix ~/.local --version v0.1.0
```

| Option            | Description                                               |
| ----------------- | --------------------------------------------------------- |
| `--client`        | Which client to configure (`claude`/`claude-desktop`/`cursor`/`codex`/`all`/`none`). |
| `--prefix DIR`    | Install prefix; binary goes in `DIR/bin`.                 |
| `--dir DATA_DIR`  | Oplog data directory (passed to the server as `--dir`).   |
| `--version vX.Y.Z`| Release tag to download (default: latest).                |
| `--repo owner/repo` | GitHub repo for releases.                               |

### Manual build

Requires Go 1.26+.

```bash
task build        # builds into ./bin
# or:
go build -o bin/poplog-local-mcp ./cmd/poplog-local-mcp
```

### Configure a client manually

```bash
# Claude Code
claude mcp add --scope user oplog -- "$(pwd)/bin/poplog-local-mcp"

# with a custom data directory
claude mcp add --scope user oplog -- "$(pwd)/bin/poplog-local-mcp" --dir /path/to/store
```

Other clients (the install script does this for you):

- **Claude desktop** (separate from Claude Code) — `mcpServers` entry in
  `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS),
  `%APPDATA%\Claude\claude_desktop_config.json` (Windows), or
  `~/.config/Claude/claude_desktop_config.json` (Linux). Fully quit and reopen
  the app after editing.
- **Cursor** — `mcpServers` entry in `~/.cursor/mcp.json`.
- **Codex** — `[mcp_servers.oplog]` block in `~/.codex/config.toml`.

## The `/oplog` command

There is a single command, `/oplog`, that interprets plain language and records
it for you. You don't pick a tool or phrase a checkpoint — you just say what
happened:

```
/oplog started on the monkey task
/oplog got pulled into a prod fire drill
/oplog checkpoint: backfill done, next is the RPV triggers
/oplog the schema change blocks the RPV query
/oplog what are my loose threads?
```

It resolves which task and project you mean (asking only when genuinely
ambiguous), parks or completes whatever you were on when you switch — detecting
interruptions from your wording so it doesn't nag — and surfaces threads that
are ready to pick back up. Install it into your client(s) with:

```bash
./install-commands.sh                 # all clients (default)
./install-commands.sh --client claude # just one client
./install-commands.sh --project       # install into ./.claude and ./.cursor instead of $HOME
```

| Client      | Destination            | Frontmatter |
| ----------- | ---------------------- | ----------- |
| Claude Code | `~/.claude/commands/`  | kept (uses `allowed-tools` + `$ARGUMENTS`) |
| Codex CLI   | `~/.codex/prompts/`    | stripped (Codex also supports `$ARGUMENTS`) |
| Cursor      | `~/.cursor/commands/`  | stripped |

The command requires the `oplog` MCP server to be connected in that client, and
takes effect after restarting it (or reopening its command palette).

## MCP tools

The tools are small, orthogonal primitives; the `/oplog` command composes them.
You can also call them directly.

**Write** (append events):

| Tool               | Purpose                                                           |
| ------------------ | ----------------------------------------------------------------- |
| `oplog_start`      | Begin or resume focus on a task; creates the task if new.         |
| `oplog_park`       | Set a task aside (`interrupted`/`blocked`/`waiting`/`switched`/`paused`). |
| `oplog_complete`   | Mark a task finished.                                             |
| `oplog_abandon`    | Drop a task that won't be resumed.                               |
| `oplog_checkpoint` | Capture resumable context: state, next action, open questions.    |
| `oplog_note`       | Record a free-form note against a task.                          |
| `oplog_link`       | Record a task→task edge; `resolved: true` clears a blocks edge.   |

> `park`, `complete`, `abandon`, `checkpoint`, `note`, and `context` target a
> task by `task_id` (a ULID), or by `task` (a fuzzy name resolved to a single
> task), or — with both omitted — the current focus. A name that matches more
> than one open task is rejected as ambiguous; use `oplog_tasks` to pick the id.

**Read** (derived projections):

| Tool              | Purpose                                                            |
| ----------------- | ----------------------------------------------------------------- |
| `oplog_focus`     | The task currently in progress, if any.                           |
| `oplog_tasks`     | Find tasks by fuzzy name / project / status (task resolution).    |
| `oplog_projects`  | Known projects with task and open-task counts.                    |
| `oplog_threads`   | Loose threads, ranked: ready-to-resume first, then stalest.       |
| `oplog_context`   | Reconstruct a task: latest checkpoint + recent events.            |
| `oplog_recent`    | The most recent N events (optionally one type).                   |
| `oplog_search`    | Search events by task, project, text, type, or tags.             |

> Tool names use underscores rather than dots (`oplog_start`, not
> `oplog.start`): the Anthropic API restricts tool names to `[a-zA-Z0-9_-]`.

### A messy day, recorded

1. `oplog_start` — `{ "project": "ADS", "name": "RPV query" }`
2. Pulled into a fire drill: `oplog_park` — `{ "reason": "interrupted" }`, then
   `oplog_start` — `{ "project": "ADS", "name": "prod fire drill", "from_task_id": "<RPV id>", "origin_rel": "interrupts" }`
3. Back later: `oplog_complete` the drill, `oplog_start` — `{ "task_id": "<RPV id>" }` to resume.
4. `oplog_threads` shows anything you parked and never closed — including tasks
   whose blocker has since been resolved, flagged *ready to resume*.

## Development

```bash
task            # list tasks
task go:test    # run tests with the race detector
task check      # fmt check + vet + tests (pre-commit gate)
task cover      # HTML coverage report in dist/
task lint       # golangci-lint if installed, else vet + gofmt check
```

## Project layout

```
cmd/poplog-local-mcp/  stdio MCP server entrypoint
internal/
├── id/                ULID generator (sortable, dependency-free)
├── persistence/
│   ├── store.go       Store interface (AppendEvent / ListEvents)
│   ├── types/         domain types (Event, EventFilter, enums)
│   └── jsonl/         append-only JSONL Store implementation
├── projection/        folds events into tasks, focus, projects, loose threads
├── service/           event-writing + projection-querying application logic
└── mcp/               MCP tool definitions and adapters
```

## Design principles

- **Local first** — all data on disk; fully offline.
- **Append-only** — entries are never mutated, for auditability and easy sync.
- **AI-agnostic core** — agents are clients of the journal, not part of it.
- **Future-proof storage** — persistence hidden behind an interface.
