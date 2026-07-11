#!/usr/bin/env bash
# Boot the real dashboard server for Playwright E2E: the embedded Vue SPA + the
# live /api on 127.0.0.1:13120 — the same shape the wails desktop app runs at
# runtime. Rebuild only when the binary is missing or a tracked source is newer,
# so warm runs start fast.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN=bin/agent-dashboard

if [[ ! -x "$BIN" ]] || [[ -n "$(find server sdk src -type f \( -name '*.go' -o -name '*.vue' -o -name '*.ts' \) -newer "$BIN" -print -quit 2>/dev/null)" ]]; then
  echo "e2e-server: building (missing or stale binary)…"
  task build:all
fi

exec "$BIN" serve
