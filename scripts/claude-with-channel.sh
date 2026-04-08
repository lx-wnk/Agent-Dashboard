#!/usr/bin/env bash
# Wrapper: starts claude with the dashboard-channel MCP server loaded.
# All arguments are passed through to claude.
#
# Usage:
#   claude-with-channel --resume "my-session"
#   claude-with-channel -p "do something" --model claude-sonnet-4-6
#   claude-with-channel   # interactive session with channel
#   claude-with-channel --yolo -p "quick fix"  # skip permission prompts
#
# Options:
#   --yolo   Add --dangerously-skip-permissions (no tool approval prompts)

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CHANNEL_TSX="$SCRIPT_DIR/channel/node_modules/.bin/tsx"
CHANNEL_SCRIPT="$SCRIPT_DIR/channel/dashboard-channel.ts"

if [ ! -f "$CHANNEL_TSX" ]; then
  echo "Error: tsx not found. Run 'cd $SCRIPT_DIR/channel && npm install' first." >&2
  exit 1
fi

MCP_CONFIG=$(cat <<EOF
{"mcpServers":{"dashboard-channel":{"command":"$CHANNEL_TSX","args":["$CHANNEL_SCRIPT"]}}}
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
