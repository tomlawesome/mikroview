#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/quiet-host.sh against a temp mount, with a background
# loop standing in for the root unit: it writes `applied` shortly after
# `hold` appears and removes it shortly after `hold` goes, the same shape
# quiet-host-apply.sh gives the real host.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/quiet-host.sh"

TMP="$(mktemp -d)"
FAKE_UNIT_PID=""
cleanup() {
  [ -n "$FAKE_UNIT_PID" ] && kill "$FAKE_UNIT_PID" 2>/dev/null
  rm -rf "$TMP"
}
trap cleanup EXIT

export QH_MOUNT="$TMP/qh"
mkdir -p "$QH_MOUNT"

fake_unit() {
  while true; do
    if [ -f "$QH_MOUNT/hold" ] && [ ! -f "$QH_MOUNT/applied" ]; then
      job=$(grep -m1 '^job=' "$QH_MOUNT/hold" | cut -d= -f2-)
      printf 'concurrent=1 at %s for job=%s\n' "$(date +%s)" "$job" >"$QH_MOUNT/applied"
    elif [ ! -f "$QH_MOUNT/hold" ] && [ -f "$QH_MOUNT/applied" ]; then
      rm -f "$QH_MOUNT/applied"
    fi
    sleep 1
  done
}
fake_unit &
FAKE_UNIT_PID=$!

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}

# not mounted
saved_mount="$QH_MOUNT"
export QH_MOUNT="$TMP/not-there"
if "$SCRIPT" hold >/dev/null 2>"$TMP/err"; then
  echo "FAIL - hold should refuse when not mounted"; fail=1
else
  check "$?" "2" "not-mounted exits 2" || true
  grep -q "is not mounted" "$TMP/err" && echo "ok - not-mounted message" || { echo "FAIL - not-mounted message"; fail=1; }
fi
export QH_MOUNT="$saved_mount"

# hold succeeds once the fake unit applies it
CI_JOB_ID=1234 "$SCRIPT" hold >"$TMP/hold.out" 2>&1
grep -q "hold applied" "$TMP/hold.out" && echo "ok - hold applied" || { echo "FAIL - hold applied: $(cat "$TMP/hold.out")"; fail=1; }

# a second hold by a different job is refused
if CI_JOB_ID=5678 "$SCRIPT" hold >"$TMP/hold2.out" 2>&1; then
  echo "FAIL - second hold should refuse while the first is active"; fail=1
else
  grep -q "already held" "$TMP/hold2.out" && echo "ok - second hold refused" || { echo "FAIL - second hold refused: $(cat "$TMP/hold2.out")"; fail=1; }
fi

# release by the owning job restores state
CI_JOB_ID=1234 "$SCRIPT" release >"$TMP/release.out" 2>&1
grep -q "released hold" "$TMP/release.out" && echo "ok - release reported" || { echo "FAIL - release reported: $(cat "$TMP/release.out")"; fail=1; }
[ -f "$QH_MOUNT/hold" ] && { echo "FAIL - hold should be gone"; fail=1; } || echo "ok - hold removed"

# releasing with nothing held is a no-op
"$SCRIPT" release >"$TMP/release2.out" 2>&1
grep -q "nothing to release" "$TMP/release2.out" && echo "ok - nothing-to-release" || { echo "FAIL - nothing-to-release: $(cat "$TMP/release2.out")"; fail=1; }

exit $fail
