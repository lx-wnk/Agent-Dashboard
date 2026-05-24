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
#   JS_MAX_GZIP_KB  (default 200)
#   CSS_MAX_GZIP_KB (default 30)
#
# Exit codes:
#   0 — all bundles within budget
#   1 — at least one bundle exceeds budget
#   2 — usage / missing inputs

set -eu

DIST_DIR="${1:-dist/assets}"
JS_MAX_GZIP_KB="${JS_MAX_GZIP_KB:-200}"
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

status=0
js_breaches=""
css_breaches=""

printf '%-50s %10s %10s %10s\n' "FILE" "RAW(B)" "GZIP(KB)" "BUDGET(KB)"
printf '%-50s %10s %10s %10s\n' "----" "------" "--------" "----------"

# Check JS bundles — match index-*.js
for f in "$DIST_DIR"/index-*.js; do
  [ -e "$f" ] || continue
  raw=$(wc -c < "$f" | tr -d ' ')
  gz=$(gzip_size_bytes "$f")
  gz_kb=$(bytes_to_kb_ceil "$gz")
  name=$(basename "$f")
  printf '%-50s %10s %10s %10s\n' "$name" "$raw" "$gz_kb" "$JS_MAX_GZIP_KB"
  if [ "$gz_kb" -gt "$JS_MAX_GZIP_KB" ]; then
    js_breaches="$js_breaches $name(${gz_kb}KB)"
    status=1
  fi
done

# Check CSS bundles — match index-*.css
for f in "$DIST_DIR"/index-*.css; do
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
  [ -n "$js_breaches" ] && echo "  JS  > ${JS_MAX_GZIP_KB}KB gzip:$js_breaches" >&2
  [ -n "$css_breaches" ] && echo "  CSS > ${CSS_MAX_GZIP_KB}KB gzip:$css_breaches" >&2
  echo "" >&2
  echo "Override budgets via env: JS_MAX_GZIP_KB=$JS_MAX_GZIP_KB CSS_MAX_GZIP_KB=$CSS_MAX_GZIP_KB" >&2
  exit 1
fi

echo "OK: all bundles within budget (JS<=${JS_MAX_GZIP_KB}KB, CSS<=${CSS_MAX_GZIP_KB}KB gzip)."
exit 0
