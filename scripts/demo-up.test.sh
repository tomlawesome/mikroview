#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/demo-up.sh --dry-run: the demo procedure in docs/demos.md
# (history envelope, live-env up with the two demo variables, then the
# seed-demo.py subcommands push/entities/accounts/feed -- never `all`)
# has to come out as the same commands in the same order, ending with the
# build stamp line. Nothing is run: --dry-run only prints.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/demo-up.sh"

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

# Line number (1-based) of the first line matching an extended regex, or 0.
lineno() {
  local n
  n="$(grep -nE -m1 -- "$1" "$TMP/out" | cut -d: -f1 || true)"
  echo "${n:-0}"
}

rc=0
bash "$SCRIPT" --dry-run >"$TMP/out" 2>"$TMP/err" || rc=$?
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "--dry-run exits 0 (got $rc)"

hist="$(lineno 'cmd/demo-history')"
up="$(lineno 'live-env\.sh up')"
push="$(lineno 'seed-demo\.py push')"
ent="$(lineno 'seed-demo\.py entities')"
acc="$(lineno 'seed-demo\.py accounts')"
feed="$(lineno 'seed-demo\.py feed')"

check "$([ "$hist" -gt 0 ] && echo true || echo false)" "history step (cmd/demo-history) appears"
check "$([ "$up" -gt 0 ] && echo true || echo false)" "live-env.sh up appears"
check "$([ "$push" -gt 0 ] && echo true || echo false)" "seed-demo.py push appears"
check "$([ "$ent" -gt 0 ] && echo true || echo false)" "seed-demo.py entities appears"
check "$([ "$acc" -gt 0 ] && echo true || echo false)" "seed-demo.py accounts appears"
check "$([ "$feed" -gt 0 ] && echo true || echo false)" "seed-demo.py feed appears"

ordered=false
if [ "$hist" -gt 0 ] && [ "$hist" -lt "$up" ] && [ "$up" -lt "$push" ] \
  && [ "$push" -lt "$ent" ] && [ "$ent" -lt "$acc" ] && [ "$acc" -lt "$feed" ]; then
  ordered=true
fi
check "$ordered" "order is history < live-env up < push < entities < accounts < feed"

upline="$(grep -E -m1 -- 'live-env\.sh up' "$TMP/out" || true)"
check "$([[ "$upline" == *MV_DEMO_DEVICES=1* ]] && echo true || echo false)" "live-env up line carries MV_DEMO_DEVICES=1"
check "$([[ "$upline" == *MV_DEMO_BUILD=1* ]] && echo true || echo false)" "live-env up line carries MV_DEMO_BUILD=1"

allok=false
[ -s "$TMP/out" ] && allok=true
grep -qE -- 'seed-demo\.py[[:space:]]+all' "$TMP/out" && allok=false
check "$allok" "no line uses seed-demo.py all"

prefixed=false
[ -s "$TMP/out" ] && prefixed=true
for pat in 'cmd/demo-history' 'live-env\.sh up' 'seed-demo\.py (push|entities|accounts|feed)'; do
  grep -E -- "$pat" "$TMP/out" | grep -qvE '^\+ ' && prefixed=false
done
check "$prefixed" "every command line is prefixed '+ '"

last="$(tail -n 1 "$TMP/out")"
check "$([[ "$last" == "Build stamp:"* ]] && echo true || echo false)" "last line starts 'Build stamp:' (got: $last)"

rc=0
bash "$SCRIPT" --bogus >"$TMP/bout" 2>"$TMP/berr" || rc=$?
check "$([ "$rc" -eq 2 ] && echo true || echo false)" "unknown flag exits 2 (got $rc)"
check "$([ "$rc" -eq 2 ] && grep -qi usage "$TMP/berr" && echo true || echo false)" "unknown flag prints usage to stderr"

if [ "$fails" -gt 0 ]; then
  echo "demo-up.test.sh: $fails failure(s)" >&2
  exit 1
fi
echo "demo-up.test.sh: all passed"
