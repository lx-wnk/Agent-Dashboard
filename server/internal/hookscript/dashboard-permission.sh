#!/usr/bin/env bash
# Permission bridge for the agent dashboard.
#
# Registered as a PreToolUse and a Notification hook. Claude Code writes the
# event as JSON on stdin and reads this script's stdout; whatever the dashboard
# answers is printed verbatim.
#
# The script holds no policy of its own on purpose. Every failure — dashboard
# down, no secret, curl missing, request timed out — prints nothing, and Claude
# Code then does exactly what it does without this hook installed: it asks in
# the terminal. Silence is the safe answer, so it is also the default.
#
# Environment:
#   DASHBOARD_URL           default http://127.0.0.1:13120
#   DASHBOARD_HOOKS_SECRET  overrides the secret file below
#
# The secret is read from ~/.claude/dashboard-hooks-secret (0600, written by the
# dashboard on first boot) rather than required in the environment: a session
# started by hand in a terminal inherits no dashboard variables, and putting the
# secret in settings.json would leave it in a file meant to be shared.
set -u

url="${DASHBOARD_URL:-http://127.0.0.1:13120}"
secret_file="${HOME}/.claude/dashboard-hooks-secret"
secret="${DASHBOARD_HOOKS_SECRET:-}"
if [ -z "$secret" ] && [ -r "$secret_file" ]; then
  secret="$(tr -d '[:space:]' < "$secret_file")"
fi
payload="$(cat)"

[ -n "$secret" ] || exit 0
command -v curl >/dev/null 2>&1 || exit 0

case "${1:-permission}" in
  notification)
    # Fire-and-forget: this only records that the terminal is asking.
    curl -sS -m 5 -o /dev/null \
      -H "Authorization: Bearer $secret" \
      -H 'Content-Type: application/json' \
      --data-binary "$payload" \
      "$url/api/hooks/notification" >/dev/null 2>&1
    exit 0
    ;;
esac

# --max-time must outlast the server's own hold (25s) so the server is the one
# that decides to give up, and must stay under the `timeout` configured for this
# hook in settings so Claude Code's fallback is not what cuts us off.
response="$(curl -sS -m 28 \
  -H "Authorization: Bearer $secret" \
  -H 'Content-Type: application/json' \
  --data-binary "$payload" \
  "$url/api/hooks/permission" 2>/dev/null)" || exit 0

# An empty object is the dashboard's "no decision". Printing it would be
# harmless, but printing nothing is unambiguous.
case "$response" in
  ''|'{}'|'{}'$'\n') exit 0 ;;
esac

printf '%s\n' "$response"
exit 0
