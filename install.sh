#!/usr/bin/env bash
#
# Install the poplog-local-mcp server and register it with an MCP client.
#
# Behavior:
#   - Run from inside a checkout (with Go installed): builds from source.
#   - Run standalone (e.g. curl | bash): downloads the latest GitHub release.
#   - Then installs the binary and configures Claude Code, Claude desktop,
#     Cursor, and/or Codex.
#
# Usage:
#   ./install.sh [options]
#
# Options:
#   --client <claude|claude-desktop|cursor|codex|all|none>  Client(s) to configure.
#   --prefix <dir>     Install prefix; binary goes in <dir>/bin.
#   --dir <data-dir>   Oplog data directory (passed to the server as --dir).
#   --version <vX.Y.Z> Release tag to download (default: latest).
#   --repo <owner/repo> GitHub repo for releases (default: optimuspaul/personal-oplog).
#   -h, --help         Show this help.
#
set -euo pipefail

REPO="${OPLOG_REPO:-optimuspaul/personal-oplog}"
BIN_NAME="poplog-local-mcp"
SERVER_NAME="oplog"
PKG_PATH="./cmd/poplog-local-mcp"

CLIENT=""
PREFIX=""
DATA_DIR=""
VERSION=""

# Resolve the directory this script lives in (empty/garbage when piped).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"

# --- output helpers ---------------------------------------------------------

if [ -t 1 ]; then
  BOLD="$(printf '\033[1m')"; DIM="$(printf '\033[2m')"
  GREEN="$(printf '\033[32m')"; YELLOW="$(printf '\033[33m')"; RED="$(printf '\033[31m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi

info()  { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$*"; }
step()  { printf '%s •%s %s\n' "$DIM" "$RESET" "$*"; }
warn()  { printf '%swarning:%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
err()   { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

# usage prints the leading comment block (after the shebang) as help text.
usage() {
  awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"
  exit 0
}

# --- argument parsing -------------------------------------------------------

while [ $# -gt 0 ]; do
  case "$1" in
    --client)  CLIENT="${2:-}"; shift 2;;
    --prefix)  PREFIX="${2:-}"; shift 2;;
    --dir)     DATA_DIR="${2:-}"; shift 2;;
    --version) VERSION="${2:-}"; shift 2;;
    --repo)    REPO="${2:-}"; shift 2;;
    -h|--help) usage;;
    *) err "unknown option: $1 (use --help)";;
  esac
done

# --- platform detection -----------------------------------------------------

GOOS=""; GOARCH=""; EXT=""
detect_platform() {
  local os arch
  os="$(uname -s)"; arch="$(uname -m)"
  case "$os" in
    Darwin) GOOS=darwin;;
    Linux)  GOOS=linux;;
    MINGW*|MSYS*|CYGWIN*) GOOS=windows; EXT=".exe";;
    *) err "unsupported OS: $os";;
  esac
  case "$arch" in
    arm64|aarch64) GOARCH=arm64;;
    x86_64|amd64)  GOARCH=amd64;;
    *) err "unsupported architecture: $arch";;
  esac
}

# --- temp workspace ---------------------------------------------------------

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

BUILT=""  # path to the binary to install, set by build/download.

# --- build from source ------------------------------------------------------

is_checkout() {
  [ -n "$SCRIPT_DIR" ] && [ -f "$SCRIPT_DIR/$PKG_PATH/main.go" ]
}

build_from_source() {
  command -v go >/dev/null 2>&1 || err "Go is required to build from source but was not found."
  local version
  version="$(git -C "$SCRIPT_DIR" describe --tags --always --dirty 2>/dev/null || echo dev)"
  step "Building $BIN_NAME ($version) for $GOOS/$GOARCH"
  ( cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${version}" \
      -o "$TMP/${BIN_NAME}${EXT}" "$PKG_PATH" )
  BUILT="$TMP/${BIN_NAME}${EXT}"
}

# --- download release -------------------------------------------------------

download_release() {
  command -v curl >/dev/null 2>&1 || err "curl is required to download releases."

  local api
  if [ -n "$VERSION" ]; then
    api="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
  else
    api="https://api.github.com/repos/${REPO}/releases/latest"
  fi

  step "Querying $REPO for ${VERSION:-latest} release"
  local json urls asset
  json="$(curl -fsSL "$api")" || err "failed to query GitHub releases API."

  urls="$(printf '%s' "$json" \
    | grep -o '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]*"' \
    | sed 's/.*"\(https[^"]*\)"/\1/')"

  asset="$(printf '%s\n' "$urls" | grep -E "_${GOOS}_${GOARCH}\.(tar\.gz|zip)$" | head -n1 || true)"

  if [ -z "$asset" ]; then
    warn "No prebuilt asset for ${GOOS}/${GOARCH} in this release."
    if command -v go >/dev/null 2>&1; then
      step "Falling back to 'go install' for this platform"
      GOBIN="$TMP" CGO_ENABLED=0 go install \
        "github.com/${REPO}/cmd/poplog-local-mcp@${VERSION:-latest}"
      BUILT="$TMP/${BIN_NAME}${EXT}"
      return
    fi
    err "No matching release asset and Go is not installed to build one."
  fi

  step "Downloading $(basename "$asset")"
  local archive="$TMP/$(basename "$asset")"
  curl -fsSL "$asset" -o "$archive" || err "download failed."

  case "$archive" in
    *.tar.gz) tar -xzf "$archive" -C "$TMP";;
    *.zip)    command -v unzip >/dev/null 2>&1 || err "unzip is required for .zip assets."; unzip -oq "$archive" -d "$TMP";;
    *) err "unrecognized archive: $archive";;
  esac

  [ -f "$TMP/${BIN_NAME}${EXT}" ] || err "binary not found inside the downloaded archive."
  BUILT="$TMP/${BIN_NAME}${EXT}"
}

# --- install ----------------------------------------------------------------

DEST=""
install_binary() {
  local bindir
  if [ -n "$PREFIX" ]; then
    bindir="$PREFIX/bin"
  elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    bindir="/usr/local/bin"
  else
    bindir="$HOME/.local/bin"
  fi

  mkdir -p "$bindir"
  DEST="$bindir/${BIN_NAME}${EXT}"
  cp "$BUILT" "$DEST"
  chmod 0755 "$DEST"
  info "Installed ${BOLD}$DEST${RESET}"

  case ":$PATH:" in
    *":$bindir:"*) ;;
    *) warn "$bindir is not on your PATH. Add it, e.g.:  export PATH=\"$bindir:\$PATH\"";;
  esac
}

# --- client configuration ---------------------------------------------------

# server_args echoes the extra CLI args (currently just --dir) if a data
# directory was requested.
server_args() {
  [ -n "$DATA_DIR" ] && printf -- '--dir\n%s\n' "$DATA_DIR"
}

configure_claude() {
  local auto="${1:-}"
  if ! command -v claude >/dev/null 2>&1; then
    [ "$auto" = "auto" ] && { step "Claude CLI not found; skipping."; return; }
    warn "Claude CLI ('claude') not found. Configure manually:"
    warn "  claude mcp add --scope user $SERVER_NAME -- $DEST $([ -n "$DATA_DIR" ] && echo "--dir $DATA_DIR")"
    return
  fi
  claude mcp remove "$SERVER_NAME" >/dev/null 2>&1 || true
  if [ -n "$DATA_DIR" ]; then
    claude mcp add --scope user "$SERVER_NAME" -- "$DEST" --dir "$DATA_DIR"
  else
    claude mcp add --scope user "$SERVER_NAME" -- "$DEST"
  fi
  info "Configured Claude (user scope)."
}

# merge_mcp_json adds (or replaces) our server under "mcpServers" in a JSON
# config file, preserving any other servers and keys. Used by clients that
# share the Claude-style config shape (Cursor, Claude desktop). Backs up an
# existing file to <cfg>.bak before writing.
merge_mcp_json() {
  local cfg="$1"
  command -v python3 >/dev/null 2>&1 || { warn "python3 not found; cannot edit $cfg. Skipping."; return 1; }
  [ -f "$cfg" ] && cp "$cfg" "$cfg.bak"

  DEST="$DEST" SERVER_NAME="$SERVER_NAME" DATA_DIR="$DATA_DIR" CFG="$cfg" python3 - <<'PY'
import json, os
cfg = os.environ["CFG"]
name = os.environ["SERVER_NAME"]
data = {}
if os.path.exists(cfg):
    try:
        with open(cfg) as f:
            data = json.load(f) or {}
    except (json.JSONDecodeError, OSError):
        data = {}
servers = data.setdefault("mcpServers", {})
entry = {"command": os.environ["DEST"]}
if os.environ["DATA_DIR"]:
    entry["args"] = ["--dir", os.environ["DATA_DIR"]]
else:
    entry["args"] = []
servers[name] = entry
os.makedirs(os.path.dirname(cfg) or ".", exist_ok=True)
with open(cfg, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")
PY
}

configure_cursor() {
  local auto="${1:-}"
  local cfg="$HOME/.cursor/mcp.json"
  if [ "$auto" = "auto" ] && [ ! -d "$HOME/.cursor" ] && ! command -v cursor >/dev/null 2>&1; then
    step "Cursor not detected; skipping."
    return
  fi
  merge_mcp_json "$cfg" && info "Configured Cursor ($cfg)."
}

# claude_desktop_config echoes the desktop app's config path for this OS.
claude_desktop_config() {
  case "$GOOS" in
    darwin)  printf '%s' "$HOME/Library/Application Support/Claude/claude_desktop_config.json";;
    windows) printf '%s' "${APPDATA:-$HOME/AppData/Roaming}/Claude/claude_desktop_config.json";;
    *)       printf '%s' "$HOME/.config/Claude/claude_desktop_config.json";;
  esac
}

configure_claude_desktop() {
  local auto="${1:-}"
  local cfg
  cfg="$(claude_desktop_config)"
  if [ "$auto" = "auto" ] && [ ! -d "$(dirname "$cfg")" ]; then
    step "Claude desktop not detected; skipping."
    return
  fi
  merge_mcp_json "$cfg" && info "Configured Claude desktop ($cfg). Fully quit and reopen the app."
}

configure_codex() {
  local auto="${1:-}"
  local cfg="$HOME/.codex/config.toml"
  if [ "$auto" = "auto" ] && [ ! -d "$HOME/.codex" ] && ! command -v codex >/dev/null 2>&1; then
    step "Codex not detected; skipping."
    return
  fi
  mkdir -p "$(dirname "$cfg")"
  touch "$cfg"
  if grep -qE "^\[mcp_servers\.${SERVER_NAME}\]" "$cfg"; then
    warn "Codex already has an [mcp_servers.$SERVER_NAME] entry in $cfg; leaving it unchanged."
    return
  fi
  {
    printf '\n[mcp_servers.%s]\n' "$SERVER_NAME"
    printf 'command = "%s"\n' "$DEST"
    if [ -n "$DATA_DIR" ]; then
      printf 'args = ["--dir", "%s"]\n' "$DATA_DIR"
    fi
  } >> "$cfg"
  info "Configured Codex ($cfg)."
}

choose_client() {
  if [ -n "$CLIENT" ]; then return; fi
  if [ -t 0 ]; then
    printf '%sConfigure which MCP client?%s\n' "$BOLD" "$RESET"
    select c in claude claude-desktop cursor codex all none; do
      [ -n "${c:-}" ] && { CLIENT="$c"; break; }
    done
  else
    CLIENT="all"
  fi
}

configure_clients() {
  case "$CLIENT" in
    claude)         configure_claude;;
    claude-desktop) configure_claude_desktop;;
    cursor)         configure_cursor;;
    codex)          configure_codex;;
    all)
      configure_claude auto
      configure_claude_desktop auto
      configure_cursor auto
      configure_codex auto
      ;;
    none)   step "Skipping client configuration.";;
    *) err "unknown client '$CLIENT' (expected claude|claude-desktop|cursor|codex|all|none)";;
  esac
}

# --- main -------------------------------------------------------------------

main() {
  detect_platform

  if is_checkout; then
    info "Source checkout detected — building from source."
    build_from_source
  else
    info "No source checkout — installing from GitHub releases."
    download_release
  fi

  install_binary
  choose_client
  configure_clients

  info "Done. Restart your client to pick up the '${SERVER_NAME}' MCP server."
}

main "$@"
