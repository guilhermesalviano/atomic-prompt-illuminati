#!/usr/bin/env bash
set -euo pipefail

PROJECT_NAME="prompter-illuminati"
BINARY_NAME="api"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/atomic-prompt-illuminati"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/api"

REMOVE_DATA=0
ASSUME_YES="${ASSUME_YES:-0}"

say()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mnote:\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<EOF
Usage: ./uninstall.sh [options]

Options:
  -d, --data   Also remove configuration (${CONFIG_DIR}) and run
               state (${STATE_DIR}), including run worktrees.
  -y, --yes    Do not prompt for confirmation.
  -h, --help   Show this help.

Environment:
  INSTALL_DIR  Where the binary was installed (default: ~/.local/bin)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    -d|--data) REMOVE_DATA=1 ;;
    -y|--yes)  ASSUME_YES=1 ;;
    -h|--help) usage; exit 0 ;;
    *)         usage >&2; err "unknown option: $1" ;;
  esac
  shift
done

cd "$(dirname "$0")"

confirm() {
  [ "$ASSUME_YES" -eq 1 ] && return 0
  local reply
  printf '%s [y/N] ' "$1"
  read -r reply
  [[ "$reply" =~ ^[Yy]$ ]]
}

# --- Remove installed binary -------------------------------------------------
if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
  say "Removing ${INSTALL_DIR}/${BINARY_NAME}..."
  rm -f "${INSTALL_DIR}/${BINARY_NAME}"
else
  warn "No ${BINARY_NAME} binary found in ${INSTALL_DIR}; skipping."
fi

# --- Remove local build output ------------------------------------------------
if [ -d bin ]; then
  say "Removing local build output (bin/)..."
  rm -rf bin
fi

# --- Optionally remove configuration and run state -----------------------------
if [ "$REMOVE_DATA" -eq 1 ]; then
  if confirm "Remove run state and worktrees under ${STATE_DIR}?"; then
    # Collect the target repos recorded in run.json before deleting the state
    # dir, then prune after: pruning only clears entries whose worktree
    # directory is gone.
    repos=""
    if [ -d "$STATE_DIR/runs" ]; then
      repos=$(find "$STATE_DIR/runs" -name run.json -exec sed -n 's/.*"repo": *"\([^"]*\)".*/\1/p' {} \; 2>/dev/null | sort -u || true)
    fi
    if [ -d "$STATE_DIR" ]; then
      say "Removing ${STATE_DIR}..."
      rm -rf "$STATE_DIR"
    fi
    if [ -n "$repos" ] && command -v git >/dev/null 2>&1; then
      say "Pruning git worktrees in affected repositories..."
      while IFS= read -r repo; do
        [ -d "$repo/.git" ] || continue
        git -C "$repo" worktree prune 2>/dev/null && say "  pruned ${repo}" || true
      done <<< "$repos"
    fi
  else
    warn "Keeping run state in ${STATE_DIR}."
  fi

  if [ -d "$CONFIG_DIR" ]; then
    if confirm "Remove configuration in ${CONFIG_DIR}?"; then
      say "Removing ${CONFIG_DIR}..."
      rm -rf "$CONFIG_DIR"
    else
      warn "Keeping configuration in ${CONFIG_DIR}."
    fi
  fi
else
  [ -d "$STATE_DIR" ] && warn "Run state kept in ${STATE_DIR} (re-run with --data to remove)."
fi

# Branches created by runs (api/<branch> or custom names) are left untouched;
# delete them manually with 'git branch -d <name>' if no longer wanted.

say "Done. ${PROJECT_NAME} uninstalled."
