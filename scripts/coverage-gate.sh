#!/usr/bin/env sh
# coverage-gate.sh — enforce a per-package coverage floor on a Go coverage profile.
#
# Usage:
#   scripts/coverage-gate.sh COVERAGE_PROFILE PATTERN [PATTERN...]
#
# COVERAGE_PROFILE is a raw `go test -coverprofile` file (the one with a leading
# `mode:` line), NOT the `go tool cover -func` text output. Each PATTERN is
# matched against package paths; every matching package must have a
# statement-weighted coverage >= COVERAGE_THRESHOLD.
#
# Env:
#   COVERAGE_THRESHOLD  (default 70)
#
# Exit codes:
#   0 — every matching package meets the threshold
#   1 — at least one matching package is below the threshold
#   2 — usage / missing inputs

set -eu

if [ "$#" -lt 2 ]; then
  echo "Usage: $0 COVERAGE_PROFILE PATTERN [PATTERN...]" >&2
  exit 2
fi

COVERAGE_FILE="$1"
shift
THRESHOLD="${COVERAGE_THRESHOLD:-70}"

if [ ! -f "$COVERAGE_FILE" ]; then
  echo "ERROR: coverage file not found: $COVERAGE_FILE" >&2
  exit 2
fi

# Build an alternation regex from the patterns.
RE=""
for p in "$@"; do
  if [ -z "$RE" ]; then
    RE="$p"
  else
    RE="$RE|$p"
  fi
done

# Raw coverage-profile format (after the `mode:` header line):
#   github.com/.../internal/pipeline/foo.go:12.34,56.78 5 1
#   <file>:<startLine.col>,<endLine.col> <numStatements> <execCount>
# Per-package coverage is statement-weighted: the sum of executed statements
# (execCount > 0) over the sum of all statements in that package — the same
# measure `go tool cover -func` reports on its `total:` line, but scoped per
# package. This does NOT equal the unweighted mean of per-function percentages.

# shellcheck disable=SC2016
awk -v re="$RE" -v thr="$THRESHOLD" '
  /^mode:/ { next }
  NF >= 3 {
    path = $1
    # strip the trailing ":startLine.col,endLine.col" position span
    sub(/:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/, "", path)

    numStmt = $2 + 0
    execCount = $3 + 0

    # package path = dirname of the file path
    n = split(path, parts, "/")
    pkg = ""
    for (i = 1; i < n; i++) {
      pkg = (i == 1) ? parts[i] : pkg "/" parts[i]
    }

    if (pkg ~ re) {
      total[pkg] += numStmt
      if (execCount > 0) covered[pkg] += numStmt
    }
  }
  END {
    fail = 0
    matched = 0
    for (pkg in total) {
      matched++
      pct = (total[pkg] > 0) ? (100.0 * covered[pkg] / total[pkg]) : 0
      status = (pct + 0 >= thr + 0) ? "OK" : "FAIL"
      printf "%-6s %6.2f%%  %s  (threshold %s%%)\n", status, pct, pkg, thr
      if (pct + 0 < thr + 0) fail = 1
    }
    if (matched == 0) {
      print "ERROR: no packages matched the supplied patterns" > "/dev/stderr"
      exit 2
    }
    if (fail) exit 1
    exit 0
  }
' "$COVERAGE_FILE"
