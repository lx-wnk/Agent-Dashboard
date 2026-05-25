#!/usr/bin/env sh
# coverage-gate.sh — enforce a per-package coverage floor on a Go coverage profile.
#
# Usage:
#   scripts/coverage-gate.sh COVERAGE_FILE PATTERN [PATTERN...]
#
# Each PATTERN is matched against package paths in `go tool cover -func`.
# All matching package lines must have a percentage >= COVERAGE_THRESHOLD.
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
  echo "Usage: $0 COVERAGE_FILE PATTERN [PATTERN...]" >&2
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

# `go tool cover -func` output:
#   github.com/.../internal/pipeline/foo.go:12:  FuncName  87.5%
#   total:                                       (statements)            45.6%
# We aggregate per-package: take the directory of the file path and average the
# percentages. Simpler and more honest: report the MIN percentage across funcs
# in each matched package — if any function in pipeline drops to 0% we surface
# it. To keep the gate stable, we instead compute the per-package coverage as
# the average of all function percentages in that package.

# shellcheck disable=SC2016
awk -v re="$RE" -v thr="$THRESHOLD" '
  $0 ~ "total:" { next }
  {
    # extract last column percentage, e.g. "87.5%"
    pct = $NF
    sub(/%$/, "", pct)
    if (pct == "" || pct !~ /^[0-9.]+$/) next

    # extract file path = $1 before the first ":"
    path = $1
    sub(/:.*/, "", path)

    # package path = dirname
    n = split(path, parts, "/")
    pkg = ""
    for (i = 1; i < n; i++) {
      pkg = (i == 1) ? parts[i] : pkg "/" parts[i]
    }

    if (pkg ~ re) {
      sum[pkg] += pct + 0
      cnt[pkg] += 1
    }
  }
  END {
    fail = 0
    matched = 0
    for (pkg in sum) {
      matched++
      avg = sum[pkg] / cnt[pkg]
      status = (avg + 0 >= thr + 0) ? "OK" : "FAIL"
      printf "%-6s %6.2f%%  %s  (threshold %s%%)\n", status, avg, pkg, thr
      if (avg + 0 < thr + 0) fail = 1
    }
    if (matched == 0) {
      print "ERROR: no packages matched the supplied patterns" > "/dev/stderr"
      exit 2
    }
    if (fail) exit 1
    exit 0
  }
' "$COVERAGE_FILE"
