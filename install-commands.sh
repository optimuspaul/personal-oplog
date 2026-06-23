#!/usr/bin/env bash
#
# Install the oplog slash commands from ./commands into one or more clients.
#
# Each client stores custom commands in a different place, and only Claude
# Code understands the YAML frontmatter (description/argument-hint/allowed-tools)
# and $ARGUMENTS. For Codex and Cursor the frontmatter is stripped, leaving the
# instruction body (the command name comes from the file name either way).
#
#   Claude Code -> ~/.claude/commands/      (frontmatter kept)
#   Codex CLI   -> ~/.codex/prompts/        (frontmatter stripped)
#   Cursor      -> ~/.cursor/commands/      (frontmatter stripped)
#
# Usage:
#   ./install-commands.sh [options]
#
# Options:
#   --client <claude|codex|cursor|all>  Which client(s) to install into (default: all).
#   --src <dir>     Source directory of command .md files (default: ./commands).
#   --project       Install at project scope (./.claude/commands, ./.cursor/commands).
#                   Codex has no project scope; it always uses ~/.codex/prompts.
#   -h, --help      Show this help.
#
set -euo pipefail

CLIENT="all"
PROJECT=0
SRC=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || true)"

if [ -t 1 ]; then
  BOLD="$(printf '\033[1m')"; DIM="$(printf '\033[2m')"
  GREEN="$(printf '\033[32m')"; YELLOW="$(printf '\033[33m')"; RED="$(printf '\033[31m')"
  RESET="$(printf '\033[0m')"
else
  BOLD=""; DIM=""; GREEN=""; YELLOW=""; RED=""; RESET=""
fi
info() { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$*"; }
step() { printf '%s •%s %s\n' "$DIM" "$RESET" "$*"; }
warn() { printf '%swarning:%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
err()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }
usage() { awk 'NR==1{next} /^#/{sub(/^# ?/,""); print; next} {exit}' "$0"; exit 0; }

while [ $# -gt 0 ]; do
  case "$1" in
    --client)  CLIENT="${2:-}"; shift 2;;
    --src)     SRC="${2:-}"; shift 2;;
    --project) PROJECT=1; shift;;
    -h|--help) usage;;
    *) err "unknown option: $1 (use --help)";;
  esac
done

[ -n "$SRC" ] || SRC="$SCRIPT_DIR/commands"
[ -d "$SRC" ] || err "source command directory not found: $SRC"

shopt -s nullglob
FILES=("$SRC"/*.md)
shopt -u nullglob
[ ${#FILES[@]} -gt 0 ] || err "no .md command files found in $SRC"

# strip_frontmatter removes a leading '---' YAML block and the blank lines
# immediately after it, passing everything else through unchanged. Files with
# no frontmatter are emitted verbatim.
strip_frontmatter() {
  awk '
    NR==1 && $0=="---" { fm=1; next }
    fm==1 && $0=="---" { fm=2; next }
    fm==1              { next }
    fm==2 && body==0 && $0=="" { next }
    { body=1; print }
  '
}

# install_into <dest-dir> <keep|strip>
install_into() {
  local destdir="$1" mode="$2" f name n=0
  mkdir -p "$destdir"
  for f in "${FILES[@]}"; do
    name="$(basename "$f")"
    if [ "$mode" = "strip" ]; then
      strip_frontmatter < "$f" > "$destdir/$name"
    else
      cp "$f" "$destdir/$name"
    fi
    n=$((n + 1))
  done
  info "Installed $n command(s) into ${BOLD}$destdir${RESET}"
}

install_claude() {
  local destdir
  if [ "$PROJECT" -eq 1 ]; then destdir="$PWD/.claude/commands"; else destdir="$HOME/.claude/commands"; fi
  install_into "$destdir" keep
}

install_codex() {
  if [ "$PROJECT" -eq 1 ]; then
    warn "Codex has no project scope; installing to ~/.codex/prompts anyway."
  fi
  install_into "$HOME/.codex/prompts" strip
}

install_cursor() {
  local destdir
  if [ "$PROJECT" -eq 1 ]; then destdir="$PWD/.cursor/commands"; else destdir="$HOME/.cursor/commands"; fi
  install_into "$destdir" strip
}

case "$CLIENT" in
  claude) install_claude;;
  codex)  install_codex;;
  cursor) install_cursor;;
  all)    install_claude; install_codex; install_cursor;;
  *) err "unknown client '$CLIENT' (expected claude|codex|cursor|all)";;
esac

info "Done. Restart each client (or reopen its command palette) to pick up the commands."
