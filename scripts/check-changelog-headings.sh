#!/bin/sh
# Guard against duplicate ### headings within a single release section of
# CHANGELOG.md. Wired into CI so parallel agents that each open their own
# ### Added (or any other KAC heading) are caught before merge.
#
# Usage: check-changelog-headings.sh [FILE]
#   FILE defaults to CHANGELOG.md in the current directory.
#   Exit 0 = clean, 1 = duplicates found, 2 = file missing.

set -e

file="${1:-CHANGELOG.md}"

if [ ! -f "$file" ]; then
  echo "error: $file not found" >&2
  exit 2
fi

awk '
  /^## \[/ {
    section = $0
    # Reset the seen-headings map for each release section.
    delete seen
    next
  }
  /^### / {
    heading = $0
    key = section SUBSEP heading
    if (key in seen) {
      printf "duplicate: %s under %s (lines %d and %d)\n", heading, section, seen[key], NR > "/dev/stderr"
      rc = 1
    } else {
      seen[key] = NR
    }
  }
  END { exit (rc ? 1 : 0) }
' "$file"
