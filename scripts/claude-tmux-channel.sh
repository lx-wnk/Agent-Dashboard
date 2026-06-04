#!/usr/bin/env bash
# Run a Claude session that the dashboard can drive LIVE.
#
# It launches claude-with-channel.sh (dashboard-channel MCP, for agent→dashboard
# replies/permissions) INSIDE tmux. The channel bridge records the tmux pane in
# its discovery file, so the dashboard delivers prompts as real keyboard input
# via `tmux send-keys` — which actually drives the interactive session, unlike
# MCP log delivery.
#
# If you are already inside tmux, it runs directly in the current pane.
# Otherwise it creates a new tmux session and attaches you to it.
#
# All arguments are passed through to claude.
#
# Usage:
#   claude-tmux-channel.sh --permission-mode auto
#   claude-tmux-channel.sh --resume <session-id>

set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
WRAPPER="$DIR/claude-with-channel.sh"

if [ ! -x "$WRAPPER" ]; then
  echo "Error: $WRAPPER not found or not executable." >&2
  exit 1
fi

if [ -n "${TMUX:-}" ]; then
  # Already inside tmux — the bridge will inherit $TMUX_PANE / $TMUX.
  exec "$WRAPPER" "$@"
fi

if ! command -v tmux >/dev/null 2>&1; then
  echo "Error: tmux is not installed. Install tmux, or run inside an existing tmux session." >&2
  exit 1
fi

SESSION="claude-channel-$$"
# tmux runs the command + args directly (no shell), so arguments with spaces are
# preserved. The new session is attached so you interact with claude normally.
exec tmux new-session -s "$SESSION" "$WRAPPER" "$@"
