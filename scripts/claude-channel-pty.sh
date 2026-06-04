#!/usr/bin/env bash
# Run a Claude session the dashboard can drive LIVE — without tmux.
#
# It launches claude under the dashboard pty broker (`agent-dashboard ptyhost`),
# which owns the pseudo-terminal: you interact with claude normally in this
# terminal, and the dashboard injects prompts as real keyboard input via a
# loopback HTTP endpoint (discovered through the usual channel discovery file).
# Works on macOS and Linux with no external multiplexer.
#
# All arguments are passed through to claude.
#
# Usage:
#   claude-channel-pty.sh --permission-mode auto
#   claude-channel-pty.sh --resume <session-id>
#
# Env:
#   AGENT_DASHBOARD_BIN  override the agent-dashboard binary path

set -euo pipefail
DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${AGENT_DASHBOARD_BIN:-$DIR/bin/agent-dashboard}"

if [ ! -x "$BIN" ]; then
  echo "Error: agent-dashboard binary not found at '$BIN'. Run 'task build' first, or set AGENT_DASHBOARD_BIN." >&2
  exit 1
fi

exec "$BIN" ptyhost -- claude "$@"
