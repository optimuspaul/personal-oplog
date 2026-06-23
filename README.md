# Oplog — Local-First Work Journal (MCP)

Oplog is a local-first work journal and context-tracking system. It helps you
(and your agents) recover from interruptions and resume work quickly by
capturing checkpoints of what you were doing, what you discovered, what's
unresolved, and the next action to take.

The journal is append-only and stored as plain files on disk, so it works
completely offline. Functionality is exposed over the
[Model Context Protocol (MCP)](https://modelcontextprotocol.io), letting agents
participate directly.

## How it works

```
MCP client (e.g. Claude Code)
        │  stdio (JSON-RPC)
        ▼
  cmd/poplog-local-mcp       ← entrypoint
        │
  internal/mcp               ← MCP tool adapters
        │
  internal/service           ← application logic (IDs, timestamps, validation, focus)
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
├── log.jsonl            append-only journal entries (one JSON object per line)
├── current_focus.json  the active task, if any
├── projects/
├── sessions/
└── backups/
```

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

## Slash commands

The `commands/` directory holds ready-made slash commands (`/oplog-start`,
`/oplog-checkpoint`, `/oplog-resume`, …) that call the MCP tools directly, so
you don't have to phrase a prompt. Install them into your client(s) with:

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

The commands require the `oplog` MCP server to be connected in that client, and
take effect after restarting it (or reopening its command palette).

## MCP tools

| Tool                  | Purpose                                                            |
| --------------------- | ----------------------------------------------------------------- |
| `oplog_start_work`    | Begin a session on a project/task; sets the current focus.        |
| `oplog_log`           | Record a free-form note. Project/task default to the focus.       |
| `oplog_checkpoint`    | Capture resumable context: state, next action, open questions.    |
| `oplog_interrupt`     | Mark the current task interrupted and clear the focus.            |
| `oplog_resume`        | Retrieve the most recent checkpoint for a project/task.           |
| `oplog_current_focus` | Return the task currently in progress, if any.                    |
| `oplog_search`        | Search entries by project, task, tags, text, or type.            |
| `oplog_end_work`      | Mark the session complete and clear the focus.                    |

> Tool names use underscores rather than dots (`oplog_start_work`, not
> `oplog.start_work`): the Anthropic API restricts tool names to
> `[a-zA-Z0-9_-]`.

### Typical workflow

1. `oplog_start_work` — `{ "project": "DERS", "task": "OAuth compliance tests" }`
2. `oplog_checkpoint` — `{ "summary": "Password grant passes. Client credentials failing.", "next_action": "Inspect audience parameter." }`
3. `oplog_interrupt` — `{ "reason": "Production issue" }`
4. Later: `oplog_resume` — `{ "project": "DERS" }` reconstructs where you left off.

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
│   ├── store.go       Store interface
│   ├── types/         domain types (Entry, Focus, Session, EntryFilter)
│   └── jsonl/         append-only JSONL Store implementation
├── service/           journal + focus application logic
└── mcp/               MCP tool definitions and adapters
```

## Design principles

- **Local first** — all data on disk; fully offline.
- **Append-only** — entries are never mutated, for auditability and easy sync.
- **AI-agnostic core** — agents are clients of the journal, not part of it.
- **Future-proof storage** — persistence hidden behind an interface.
