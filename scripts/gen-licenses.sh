#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OUTPUT="${REPO_ROOT}/THIRD_PARTY_LICENSES.md"
GO_LICENSES="$(go env GOPATH)/bin/go-licenses"

# go-licenses v1.6.0 errored on every stdlib package under Go 1.26 ("Package
# ... does not have module info. Non go modules projects are no longer
# supported"), which collect_go() tolerated via `|| true` — silently emptying
# the Go section. Bumped 2026-07-14 to go-licenses/v2@v2.0.1, which runs cleanly
# under Go 1.26. The v2 bump corrected one long-standing misclassification:
# modernc.org/libc was reported MIT by v1.6.0 (it had classified the bundled
# LICENSE-3RD-PARTY.md), but the module's own LICENSE is 3-clause BSD — v2
# reads the right file and reports BSD-3-Clause. Install with:
#   go install github.com/google/go-licenses/v2@v2.0.1
GO_LICENSES_VERSION="v2.0.1"
MIN_GO_DEP_ROWS=20

# ── Tool check ─────────────────────────────────────────────────────────────────

if [[ ! -x "${GO_LICENSES}" ]]; then
  echo "ERROR: go-licenses not found at ${GO_LICENSES}" >&2
  echo "Install it with: go install github.com/google/go-licenses/v2@${GO_LICENSES_VERSION}" >&2
  exit 1
fi

# ── Known license overrides ────────────────────────────────────────────────────
# go-licenses cannot classify these modules automatically; licenses verified
# manually against the module source.
#
# modernc.org/mathutil: BSD-3-Clause (file: LICENSE in module root, standard
#   3-clause BSD header; go-licenses fails because it uses a non-standard
#   copyright header format that the classifier does not recognise).
declare -A LICENSE_OVERRIDES=(
  ["modernc.org/mathutil"]="BSD-3-Clause"
)

# ── Temp files ─────────────────────────────────────────────────────────────────

TMP_GO_RAW="$(mktemp)"
TMP_GO_FIXED="$(mktemp)"
TMP_FRONTEND_JSON="$(mktemp)"
TMP_OUTPUT="$(mktemp)"
trap 'rm -f "${TMP_GO_RAW}" "${TMP_GO_FIXED}" "${TMP_FRONTEND_JSON}" "${TMP_OUTPUT}"' EXIT

# ── Collect Go deps ────────────────────────────────────────────────────────────

# The Go build list is GOOS-dependent (build constraints pull in different
# packages per platform, e.g. mattn/go-isatty on darwin only). Pin GOOS/GOARCH
# to the linux/amd64 CI target so the attribution is canonical no matter which
# platform regenerates it.
export GOOS=linux
export GOARCH=amd64

# go-licenses exits non-zero when it cannot classify a module; that case is
# deliberately tolerated here and resolved downstream by LICENSE_OVERRIDES and
# the ',Unknown,' gate. Stderr is left visible so genuine failures surface in
# CI logs instead of being silently swallowed.
collect_go() {
  local dir="$1"
  local gowork_off="${2:-false}"
  # Optional GOOS override for platform-gated modules (e.g. desktop/ is
  # //go:build darwin). go-licenses cross-lists via packages.Load, so a fixed
  # non-host GOOS stays deterministic across CI/dev. Defaults to the global pin.
  local goos="${3:-${GOOS}}"

  if [[ "${gowork_off}" == "true" ]]; then
    (cd "${dir}" && GOWORK=off GOOS="${goos}" go build ./... 2>/dev/null || true)
    (cd "${dir}" && GOWORK=off GOOS="${goos}" "${GO_LICENSES}" report ./... \
      --ignore github.com/lx-wnk/agent-dashboard) || true
  else
    (cd "${dir}" && GOOS="${goos}" go build ./... 2>/dev/null || true)
    (cd "${dir}" && GOOS="${goos}" "${GO_LICENSES}" report ./... \
      --ignore github.com/lx-wnk/agent-dashboard) || true
  fi
}

echo "Collecting Go deps: server + sdk (workspace)..."
collect_go "${REPO_ROOT}/server" false >> "${TMP_GO_RAW}"

# sdk has no external deps currently; included so future additions are captured
echo "Collecting Go deps: sdk..."
collect_go "${REPO_ROOT}/sdk" false >> "${TMP_GO_RAW}" || true

echo "Collecting Go deps: plugins (GOWORK=off)..."
for plugin_dir in "${REPO_ROOT}"/plugins/*/; do
  [[ -f "${plugin_dir}go.mod" ]] || continue
  collect_go "${plugin_dir}" true >> "${TMP_GO_RAW}" || true
done

# desktop/ is a macOS-only wails app (//go:build darwin); its real dependency
# graph only exists under GOOS=darwin. go-licenses cross-lists it from any host.
echo "Collecting Go deps: desktop/wails (GOWORK=off, GOOS=darwin)..."
collect_go "${REPO_ROOT}/desktop" true darwin >> "${TMP_GO_RAW}" || true

# ── Apply license overrides ────────────────────────────────────────────────────

while IFS= read -r line; do
  module="$(echo "${line}" | cut -d',' -f1)"
  if [[ -n "${LICENSE_OVERRIDES[${module}]+x}" ]]; then
    url="$(echo "${line}" | cut -d',' -f2)"
    [[ "${url}" == "Unknown" ]] && url="https://pkg.go.dev/${module}"
    echo "${module},${url},${LICENSE_OVERRIDES[${module}]}"
  else
    echo "${line}"
  fi
done < "${TMP_GO_RAW}" > "${TMP_GO_FIXED}"

# Deduplicate by module path (first occurrence wins after stable sort).
# LC_ALL=C forces byte/codepoint ordering so the output is identical across
# platforms (a locale-sensitive sort places uppercase module paths differently
# on macOS vs the Linux CI runner, producing a spurious freshness diff).
GO_SORTED="$(LC_ALL=C sort -t',' -k1,1 "${TMP_GO_FIXED}" | awk -F',' '!seen[$1]++')"

# ── Check for Unknown licenses ─────────────────────────────────────────────────

UNKNOWNS="$(echo "${GO_SORTED}" | grep ',Unknown,' || true)"
if [[ -n "${UNKNOWNS}" ]]; then
  echo "" >&2
  echo "ERROR: Unknown license type(s) detected — investigate before shipping:" >&2
  while IFS= read -r u; do
    echo "  ${u}" >&2
  done <<< "${UNKNOWNS}"
  echo "" >&2
  echo "Add a manual override to LICENSE_OVERRIDES in scripts/gen-licenses.sh" >&2
  echo "(verify by reading the module source), or resolve upstream." >&2
  exit 1
fi

# ── Collect frontend deps ──────────────────────────────────────────────────────

echo "Collecting frontend deps..."
(cd "${REPO_ROOT}" && pnpm licenses list --prod --json 2>/dev/null) > "${TMP_FRONTEND_JSON}"

FRONTEND_TABLE="$(python3 - "${TMP_FRONTEND_JSON}" <<'PYEOF'
import sys, json

with open(sys.argv[1]) as f:
    data = json.load(f)

rows = []
for license_type, packages in data.items():
    for pkg in packages:
        name = pkg.get('name', '')
        versions = ', '.join(pkg.get('versions', []))
        rows.append((name, versions, license_type))

rows.sort(key=lambda r: r[0].lower())
for name, versions, lic in rows:
    print(f"| {name} | {versions} | {lic} |")
PYEOF
)"

# ── Counts ─────────────────────────────────────────────────────────────────────

GO_COUNT="$(echo "${GO_SORTED}" | grep -c ',' || true)"
FE_COUNT="$(echo "${FRONTEND_TABLE}" | grep -c '^|' || true)"

# ── Guard: refuse to wipe a good file with a broken run ───────────────────────
# collect_go() tolerates go-licenses failures (see comment above it), so a
# fully broken toolchain — e.g. go-licenses v1.6.0 under Go 1.26, which errors
# on every stdlib package with "does not have module info. Non go modules
# projects are no longer supported" — silently produces an empty GO_SORTED
# instead of a script failure. Catch that here instead of writing the result.
if (( GO_COUNT < MIN_GO_DEP_ROWS )); then
  echo "" >&2
  echo "ERROR: only ${GO_COUNT} Go dependency rows collected (expected >= ${MIN_GO_DEP_ROWS})." >&2
  echo "This almost always means go-licenses ${GO_LICENSES_VERSION} is failing under the" >&2
  echo "current Go toolchain (known incompatible with Go 1.26: 'Package ... does not" >&2
  echo "have module info. Non go modules projects are no longer supported')." >&2
  echo "Refusing to overwrite ${OUTPUT} with a wiped Go section." >&2
  echo "" >&2
  echo "Workaround: manually splice the existing Go Dependencies table from the" >&2
  echo "current ${OUTPUT} into a freshly regenerated file (see git history for prior" >&2
  echo "splices), or investigate a go-licenses version compatible with this Go version." >&2
  exit 1
fi

echo "Writing ${OUTPUT} (${GO_COUNT} Go deps, ${FE_COUNT} frontend deps)..."

# ── Emit output file ───────────────────────────────────────────────────────────

{
  printf '<!-- AUTO-GENERATED — do not edit by hand. Regenerate via: task licenses -->\n'
  printf '<!-- Script: scripts/gen-licenses.sh -->\n'
  printf '\n'
  printf '# Third-Party License Attribution\n'
  printf '\n'
  printf 'This project is released under the MIT License (see [LICENSE](LICENSE)).\n'
  printf 'The following third-party packages are used as transitive dependencies.\n'
  printf '\n'
  printf '## Go Dependencies\n'
  printf '\n'
  printf '| Module | License | License URL |\n'
  printf '|--------|---------|-------------|\n'
  echo "${GO_SORTED}" | while IFS=',' read -r module url license_type; do
    printf '| %s | %s | %s |\n' "${module}" "${license_type}" "${url}"
  done
  printf '\n'
  printf '## Frontend Dependencies\n'
  printf '\n'
  printf '| Package | Version | License |\n'
  printf '|---------|---------|----------|\n'
  echo "${FRONTEND_TABLE}"
} > "${TMP_OUTPUT}"

mv "${TMP_OUTPUT}" "${OUTPUT}"

echo "Done: ${OUTPUT}"
