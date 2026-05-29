#!/usr/bin/env sh
# doctor.sh — verify the local dev toolchain for Agent Dashboard.
#
# Exit codes:
#   0 — all required tools present
#   1 — at least one required tool missing
#
# Run via: task doctor

set -u

GREEN=$(printf '\033[32m'); RED=$(printf '\033[31m'); YEL=$(printf '\033[33m'); NC=$(printf '\033[0m')
missing=0

# check NAME [REQUIRED=1] [HINT]
check() {
  name="$1"; required="${2:-1}"; hint="${3:-}"
  if command -v "$name" >/dev/null 2>&1; then
    ver=$("$name" --version 2>/dev/null | head -1)
    printf "  %s✓%s %-16s %s\n" "$GREEN" "$NC" "$name" "$ver"
  elif [ "$required" -eq 1 ]; then
    printf "  %s✗%s %-16s MISSING — %s\n" "$RED" "$NC" "$name" "$hint"
    missing=$((missing + 1))
  else
    printf "  %s•%s %-16s not found (optional) — %s\n" "$YEL" "$NC" "$name" "$hint"
  fi
}

echo "Agent Dashboard — toolchain check"
echo
echo "Required:"
check go            1 "https://go.dev/  (need 1.26+)"
check task          1 "brew install go-task/tap/go-task"
check air           1 "task setup  (or: go install github.com/air-verse/air@latest)"
check golangci-lint 1 "task setup"
check node          1 "https://nodejs.org/  (need 22+)"
check pnpm          1 "https://pnpm.io/installation"

echo
echo "Optional:"
check claude   0 "Claude Code — required at runtime to monitor live agents"
check tygo     0 "task setup  (needed for 'task generate')"
check govulncheck 0 "task setup  (needed for 'task vuln')"

echo
if [ "$missing" -eq 0 ]; then
  printf "%s✓ all required tools present%s\n" "$GREEN" "$NC"
  exit 0
fi
printf "%s✗ %d required tool(s) missing — run 'task setup' or see the hints above%s\n" "$RED" "$missing" "$NC"
exit 1
