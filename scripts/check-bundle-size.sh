#!/usr/bin/env sh
# check-bundle-size.sh — enforce frontend bundle-size budget.
#
# Usage:
#   scripts/check-bundle-size.sh [DIST_DIR]
#
# Defaults:
#   DIST_DIR = dist/assets
#
# Thresholds (override via env):
#   JS_MAX_GZIP_KB        (default 100) — applies to index-* and any unmatched JS chunk
#   JS_VENDOR_MAX_GZIP_KB (default 200) — applies to vendor-* chunks (e.g. vendor-[hash].js)
#   JS_CHARTS_MAX_GZIP_KB (default 400) — applies to charts-* chunks (e.g. charts-[hash].js)
#   CSS_MAX_GZIP_KB       (default 30)  — applies to all CSS chunks
#
# Exit codes:
#   0 — all bundles within budget
#   1 — at least one bundle exceeds budget
#   2 — usage / missing inputs

set -eu

DIST_DIR="${1:-dist/assets}"
JS_MAX_GZIP_KB="${JS_MAX_GZIP_KB:-100}"
JS_VENDOR_MAX_GZIP_KB="${JS_VENDOR_MAX_GZIP_KB:-200}"
JS_CHARTS_MAX_GZIP_KB="${JS_CHARTS_MAX_GZIP_KB:-400}"
CSS_MAX_GZIP_KB="${CSS_MAX_GZIP_KB:-30}"

if [ ! -d "$DIST_DIR" ]; then
  echo "ERROR: dist directory not found: $DIST_DIR" >&2
  echo "Hint: run 'pnpm install && pnpm build' first." >&2
  exit 2
fi

# Portable gzip size: pipe through gzip -c then count bytes.
gzip_size_bytes() {
  gzip -c "$1" | wc -c | tr -d ' '
}

bytes_to_kb_ceil() {
  # ceil division by 1024 — keeps the report a bit conservative.
  echo "$(( ( $1 + 1023 ) / 1024 ))"
}

# Resolve per-chunk JS budget based on file name prefix.
js_budget_for() {
  name="$1"
  case "$name" in
    vendor-*) echo "$JS_VENDOR_MAX_GZIP_KB" ;;
    charts-*) echo "$JS_CHARTS_MAX_GZIP_KB" ;;
    *)        echo "$JS_MAX_GZIP_KB" ;;
  esac
}

status=0
js_breaches=""
css_breaches=""

printf '%-50s %10s %10s %10s\n' "FILE" "RAW(B)" "GZIP(KB)" "BUDGET(KB)"
printf '%-50s %10s %10s %10s\n' "----" "------" "--------" "----------"

# Check all JS chunks (index-*, vendor-*, charts-*, and any others Vite emits).
for f in "$DIST_DIR"/*.js; do
  [ -e "$f" ] || continue
  raw=$(wc -c < "$f" | tr -d ' ')
  gz=$(gzip_size_bytes "$f")
  gz_kb=$(bytes_to_kb_ceil "$gz")
  name=$(basename "$f")
  budget=$(js_budget_for "$name")
  printf '%-50s %10s %10s %10s\n' "$name" "$raw" "$gz_kb" "$budget"
  if [ "$gz_kb" -gt "$budget" ]; then
    js_breaches="$js_breaches $name(${gz_kb}KB>budget:${budget}KB)"
    status=1
  fi
done

# Check CSS bundles.
for f in "$DIST_DIR"/*.css; do
  [ -e "$f" ] || continue
  raw=$(wc -c < "$f" | tr -d ' ')
  gz=$(gzip_size_bytes "$f")
  gz_kb=$(bytes_to_kb_ceil "$gz")
  name=$(basename "$f")
  printf '%-50s %10s %10s %10s\n' "$name" "$raw" "$gz_kb" "$CSS_MAX_GZIP_KB"
  if [ "$gz_kb" -gt "$CSS_MAX_GZIP_KB" ]; then
    css_breaches="$css_breaches $name(${gz_kb}KB)"
    status=1
  fi
done

echo ""
if [ "$status" -ne 0 ]; then
  echo "FAIL: bundle-size budget exceeded." >&2
  [ -n "$js_breaches" ] && echo "  JS  breaches:$js_breaches" >&2
  [ -n "$css_breaches" ] && echo "  CSS > ${CSS_MAX_GZIP_KB}KB gzip:$css_breaches" >&2
  echo "" >&2
  echo "Override budgets via env: JS_MAX_GZIP_KB JS_VENDOR_MAX_GZIP_KB JS_CHARTS_MAX_GZIP_KB CSS_MAX_GZIP_KB" >&2
  exit 1
fi

echo "OK: all bundles within budget (index<=${JS_MAX_GZIP_KB}KB, vendor<=${JS_VENDOR_MAX_GZIP_KB}KB, charts<=${JS_CHARTS_MAX_GZIP_KB}KB, CSS<=${CSS_MAX_GZIP_KB}KB gzip)."
exit 0
