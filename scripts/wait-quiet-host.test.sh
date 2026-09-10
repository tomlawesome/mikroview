#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/wait-quiet-host.sh against fake /proc files: a
# background loop rewrites them each second to play the host through a
# script of (busy %, load1) states, so the test proves the wait ends
# only once the CPU is quiet -- not when the load average says so.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/wait-quiet-host.sh"

TMP="$(mktemp -d)"
PLAYER_PID=""
cleanup() {
  [ -n "$PLAYER_PID" ] && kill "$PLAYER_PID" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

export QH_PROC_STAT="$TMP/stat"
export QH_PROC_LOADAVG="$TMP/loadavg"
export POLL_INTERVAL_S=1

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}

total=0
# Advance the fake counters by one second at the given busy percentage
# (100 jiffies per second), and set load1.
tick() {
  busy="$1"
  total=$((total + 100))
  idle_acc=$((idle_acc + 100 - busy))
  printf 'cpu  %d 0 0 %d 0 0 0 0 0 0\n' "$((total - idle_acc))" "$idle_acc" >"$QH_PROC_STAT.tmp"
  mv "$QH_PROC_STAT.tmp" "$QH_PROC_STAT"
  printf '%s 0.00 0.00 1/100 1\n' "$2" >"$QH_PROC_LOADAVG"
}

# Play a list of "busy:load1" states one per second, holding the last.
play() {
  idle_acc=0; total=0
  tick "${1%%:*}" "${1##*:}"
  (
    for s in "$@"; do tick "${s%%:*}" "${s##*:}"; sleep 1; done
    while true; do tick "${s%%:*}" "${s##*:}"; sleep 1; done
  ) &
  PLAYER_PID=$!
}
stop() { kill "$PLAYER_PID" 2>/dev/null; wait "$PLAYER_PID" 2>/dev/null || true; PLAYER_PID=""; }

# Load average says quiet from the first read, but the CPU is flat out
# for the first six seconds (a sibling shard building): the wait must
# hold until the CPU quietens, then need two quiet windows.
play 95:3.00 95:3.00 95:3.00 95:3.00 95:3.00 95:3.00 5:3.00 5:3.00 5:3.00 5:3.00 5:3.00 5:3.00
start=$(date +%s)
"$SCRIPT" >"$TMP/out" 2>&1; rc=$?
took=$(( $(date +%s) - start ))
stop
check "$rc" "0" "quietens: exits 0"
[ "$took" -ge 7 ] && echo "ok - waited through the busy CPU (${took}s)" || { echo "FAIL - returned after ${took}s, while the CPU was still busy"; fail=1; }
grep -q 'busy=9[0-9]%' "$TMP/out" && echo "ok - reported the busy window" || { echo "FAIL - no busy reading printed"; cat "$TMP/out"; fail=1; }

# Quiet CPU but a load average above the core count: still not quiet.
play 2:99999.00
MAX_WAIT_S=3 "$SCRIPT" >"$TMP/out" 2>&1 && rc=0 || rc=$?
stop
check "$rc" "1" "high load average: exits 1"
grep -q 'host not quiet' "$TMP/out" && echo "ok - high-load message" || { echo "FAIL - high-load message"; cat "$TMP/out"; fail=1; }

# Never quietens: gives up at MAX_WAIT_S.
play 80:1.00
MAX_WAIT_S=3 "$SCRIPT" >"$TMP/out" 2>&1 && rc=0 || rc=$?
stop
check "$rc" "1" "stays busy: exits 1"

exit "$fail"
