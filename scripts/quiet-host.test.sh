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
    # Like the real host: (re-)mark `applied` for whichever job holds now.
    job=$(grep -m1 '^job=' "$QH_MOUNT/hold" 2>/dev/null | cut -d= -f2- || true)
    if [ -f "$QH_MOUNT/hold" ] && ! grep -q "job=${job}\$" "$QH_MOUNT/applied" 2>/dev/null; then
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

[ "$(stat -c %a "$QH_MOUNT/hold")" = 644 ] && echo "ok - hold flag readable by other holders" || { echo "FAIL - hold flag mode $(stat -c %a "$QH_MOUNT/hold")"; fail=1; }

# a second hold by a different job queues rather than failing or holding
# alongside, and gives up with exit 3 at its queue limit, leaving the
# first job's hold alone
if CI_JOB_ID=5678 QH_QUEUE_POLL_S=1 QH_QUEUE_MAX_S=2 "$SCRIPT" hold >"$TMP/hold2.out" 2>&1; then
  echo "FAIL - second hold should not be taken while the first is active"; fail=1
else
  check "$?" "3" "second hold gives up at the queue limit with exit 3"
  grep -q "queued" "$TMP/hold2.out" && echo "ok - second hold queued first" || { echo "FAIL - second hold queued: $(cat "$TMP/hold2.out")"; fail=1; }
  check "$(grep -m1 '^job=' "$QH_MOUNT/hold")" "job=1234" "the first job's hold is untouched"
fi
[ -z "$(find "$QH_MOUNT" -name '.hold.*')" ] && echo "ok - no temp flag left behind" || { echo "FAIL - temp flag left behind"; fail=1; }

# and takes the host once the first hold clears (orbit-base-image's build
# holding it, say): never alongside it
( sleep 2; grep -q '^job=1234$' "$QH_MOUNT/hold" && touch "$TMP/first-still-held"; CI_JOB_ID=1234 "$SCRIPT" release >/dev/null 2>&1 ) &
releaser=$!
if CI_JOB_ID=5678 QH_QUEUE_POLL_S=1 "$SCRIPT" hold >"$TMP/hold3.out" 2>&1; then
  [ -f "$TMP/first-still-held" ] && echo "ok - second hold waited while the first was held" || { echo "FAIL - second hold did not wait"; fail=1; }
  check "$(grep -m1 '^job=' "$QH_MOUNT/hold")" "job=5678" "second hold taken once the first cleared"
else
  echo "FAIL - second hold after the first cleared: $(cat "$TMP/hold3.out")"; fail=1
fi
wait "$releaser"

# the flag is gone but the host has not confirmed the release yet: still
# queued, so the new hold cannot inherit the old one's start time
CI_JOB_ID=5678 "$SCRIPT" release >/dev/null 2>&1
kill "$FAKE_UNIT_PID"; wait "$FAKE_UNIT_PID" 2>/dev/null || true
printf 'concurrent=1 at 0 for job=4321\n' >"$QH_MOUNT/applied"
if CI_JOB_ID=5678 QH_QUEUE_POLL_S=1 QH_QUEUE_MAX_S=2 "$SCRIPT" hold >"$TMP/hold4.out" 2>&1; then
  echo "FAIL - hold taken before the host confirmed the last release"; fail=1
else
  grep -q "waiting for the host to confirm the last release" "$TMP/hold4.out" && [ ! -f "$QH_MOUNT/hold" ] \
    && echo "ok - waits for the host to confirm the last release" \
    || { echo "FAIL - unconfirmed release: $(cat "$TMP/hold4.out")"; fail=1; }
fi
rm -f "$QH_MOUNT/applied"

# a hold just taken, before the host has marked it applied, is still a
# hold: the next job queues rather than overwriting it
printf 'job=77\nurl=\nstarted=%s\nexpires=9999999999\n' "$(date +%s)" >"$QH_MOUNT/hold"
if CI_JOB_ID=5678 QH_QUEUE_POLL_S=1 QH_QUEUE_MAX_S=2 "$SCRIPT" hold >"$TMP/hold5.out" 2>&1; then
  echo "FAIL - took over a hold the host had not applied yet"; fail=1
else
  check "$(grep -m1 '^job=' "$QH_MOUNT/hold")" "job=77" "a fresh, unapplied hold is not overwritten"
fi
rm -f "$QH_MOUNT/hold"

fake_unit &
FAKE_UNIT_PID=$!

# hand it back to 1234 for the release checks below
CI_JOB_ID=1234 "$SCRIPT" hold >/dev/null 2>&1

# release by the owning job restores state
CI_JOB_ID=1234 "$SCRIPT" release >"$TMP/release.out" 2>&1
grep -q "released hold" "$TMP/release.out" && echo "ok - release reported" || { echo "FAIL - release reported: $(cat "$TMP/release.out")"; fail=1; }
[ -f "$QH_MOUNT/hold" ] && { echo "FAIL - hold should be gone"; fail=1; } || echo "ok - hold removed"

# releasing with nothing held is a no-op
"$SCRIPT" release >"$TMP/release2.out" 2>&1
grep -q "nothing to release" "$TMP/release2.out" && echo "ok - nothing-to-release" || { echo "FAIL - nothing-to-release: $(cat "$TMP/release2.out")"; fail=1; }

# #1092: a FAILED marker (written by quiet-host-apply.sh's host side when
# it cannot confirm gitlab-runner picked up the reload) makes hold fail
# loudly and promptly with that reason, rather than either reading a
# stale 90s timeout as the only signal or -- before this fix -- never
# checking for it and reporting success it never confirmed. Written
# directly rather than through fake_unit, which only ever writes
# applied: this is the host's failure path, not its success one.
printf 'systemctl reload gitlab-runner exited 1 for job=9999 at %s\n' "$(date +%s)" >"$QH_MOUNT/failed"
if CI_JOB_ID=9999 "$SCRIPT" hold >"$TMP/hold-failed.out" 2>&1; then
  echo "FAIL - hold should fail loudly when the host reports FAILED"; fail=1
else
  check "$?" "5" "a FAILED marker exits 5"
  grep -q "systemctl reload gitlab-runner exited 1" "$TMP/hold-failed.out" \
    && echo "ok - hold reports the host's failure reason" \
    || { echo "FAIL - hold reports the host's failure reason: $(cat "$TMP/hold-failed.out")"; fail=1; }
fi
rm -f "$QH_MOUNT/hold" "$QH_MOUNT/applied" "$QH_MOUNT/failed"

exit $fail
