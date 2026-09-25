#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises mv_rebuild_web_dist (live-web-dist.sh) against a temp tree.
# #1292: the four standalone live-* scripts that inline this step never
# restored web/dist/.gitkeep, so every run of one of them left that
# tracked file deleted and the tree dirty. The one thing this helper
# must never regress is that .gitkeep comes back.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/live-web-dist.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

web_dist="$TMP/web/dist"
frontend_dist="$TMP/frontend/dist"
mkdir -p "$web_dist" "$frontend_dist"
touch "$web_dist/.gitkeep"
echo '<!doctype html>' >"$frontend_dist/index.html"

mv_rebuild_web_dist "$web_dist" "$frontend_dist"

check "$([ -f "$web_dist/.gitkeep" ] && echo true || echo false)" \
  "web/dist/.gitkeep survives the rebuild"
check "$([ -f "$web_dist/index.html" ] && echo true || echo false)" \
  "frontend/dist's file was copied in"

echo
if [ "$fails" -ne 0 ]; then
  echo "live-web-dist.test.sh: $fails check(s) failed"
  exit 1
fi
echo "live-web-dist.test.sh: all checks passed"
