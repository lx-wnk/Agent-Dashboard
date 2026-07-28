#!/usr/bin/env bash
set -euo pipefail

# Emits the CI job matrices as GitHub Actions outputs (key=json-array lines).
# Every matrix in ci.yml is derived from here, so a new plugin is picked up by
# test, lint, security and build without editing the workflow.
#
# Run it locally to see exactly what CI will fan out over:
#   bash scripts/ci-matrix.sh

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

# Workspace modules built and tested on Linux. desktop/ is deliberately absent:
# it is //go:build darwin and has its own macOS job.
WORKSPACE_MODULES=(server sdk)

plugins=()
plugin_binaries=()

for manifest in plugins/*/go.mod; do
  [[ -e "${manifest}" ]] || continue
  dir="$(dirname "${manifest}")"
  plugins+=("$(basename "${dir}")")
  # A plugin without a main package is a shared library (e.g. oauthkit) — it is
  # tested, linted and scanned, but there is no binary to build.
  if grep -lq '^package main' "${dir}"/*.go 2>/dev/null; then
    plugin_binaries+=("${dir}")
  fi
done

if [[ ${#plugins[@]} -eq 0 ]]; then
  echo "ERROR: no plugin modules found under plugins/*/go.mod" >&2
  exit 1
fi

json_array() {
  if [[ $# -eq 0 ]]; then
    echo '[]'
    return
  fi
  printf '%s\n' "$@" | jq -R . | jq -sc .
}

echo "workspace_modules=$(json_array "${WORKSPACE_MODULES[@]}")"
echo "plugins=$(json_array "${plugins[@]}")"
echo "scan_modules=$(json_array "${WORKSPACE_MODULES[@]}" "${plugins[@]/#/plugins/}")"
echo "plugin_binaries=$(json_array "${plugin_binaries[@]}")"
