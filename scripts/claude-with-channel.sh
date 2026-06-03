#!/usr/bin/env bash
# Wrapper: starts claude with the dashboard-channel MCP server loaded, so the
# dashboard can inject prompts into THIS running session (the bridge writes a
# discovery file the dashboard detects → ChannelAvailable=true). It uses the
# exact same bridge the task pipeline injects: the Go binary's `channel` subcommand.
#
# All arguments are passed through to claude.
#
# Usage:
#   claude-with-channel --resume "my-session"
#   claude-with-channel -p "do something" --model claude-sonnet-4-6
#   claude-with-channel              # interactive session with channel
#
# Options:
#   --yolo                            # Add --dangerously-skip-permissions
#
# Env:
#   AGENT_DASHBOARD_BIN               # override the agent-dashboard binary path

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${AGENT_DASHBOARD_BIN:-$SCRIPT_DIR/bin/agent-dashboard}"

if [ ! -x "$BIN" ]; then
  echo "Error: agent-dashboard binary not found at '$BIN'. Run 'task build' first, or set AGENT_DASHBOARD_BIN." >&2
  exit 1
fi

MCP_CONFIG=$(cat <<EOF
{"mcpServers":{"dashboard-channel":{"command":"$BIN","args":["channel"]}}}
EOF
)

# Check for --yolo flag and remove it from args before passing to claude
SKIP_PERMISSIONS=""
ARGS=()
for arg in "$@"; do
  if [ "$arg" = "--yolo" ]; then
    SKIP_PERMISSIONS="--dangerously-skip-permissions"
  else
    ARGS+=("$arg")
  fi
done

exec claude $SKIP_PERMISSIONS --mcp-config "$MCP_CONFIG" "${ARGS[@]}"
