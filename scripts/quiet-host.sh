#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Job side of issue #1003's host quieting. `perf:promotion` calls
# `hold` before it measures and `release` in its after_script. What it
# does: writes/removes a flag file under the mount `QH_MOUNT`
# (`/quiet-host` by default, bind-mounted from the host's
# `/srv/quiet-host`) and waits for a root-owned unit on the host --
# installed once from `deploy/quiet-host/` -- to confirm it applied or
# released. No token is involved on either side.
#
# Recovery: if a job dies without running its after_script, the flag
# carries its own expiry (job timeout plus 5 minutes) and the host's
# `quiet-host-expire.timer` releases it within a minute of expiring, so
# a stuck hold self-heals without anyone touching the box.
#
# Usage: scripts/quiet-host.sh hold|release [--any-job]|status
set -euo pipefail

QH_MOUNT="${QH_MOUNT:-/quiet-host}"
HOLD="$QH_MOUNT/hold"
APPLIED="$QH_MOUNT/applied"
FAILED="$QH_MOUNT/failed"

field() { grep -m1 "^$2=" "$1" 2>/dev/null | cut -d= -f2-; }
stamp() {
  now=$(date +%s)
  printf 'job=%s\nurl=%s\nstarted=%s\nexpires=%s\n' "$2" "$3" "$now" "$((now + ${CI_JOB_TIMEOUT:-1800} + 300))" >"$1"
}

cmd_hold() {
  if [ ! -d "$QH_MOUNT" ]; then
    echo "host quieting is not installed: /quiet-host is not mounted -- see deploy/quiet-host/README.md" >&2
    exit 2
  fi

  job="${CI_JOB_ID:-local}"
  url="${CI_JOB_URL:-}"
  # One hold at a time, and holds queue (owner, 2026-09-24): a performance
  # run must never measure beside someone else's hold -- the orbit-base-image
  # Node compile takes one for about forty minutes (orbit#1106) -- and must
  # not fail because of one either. So wait for the host to be free, then
  # take it. `ln` refuses if the flag already exists, so two jobs cannot
  # both think they took it (a check followed by `mv` could). An expired
  # flag is not touched here: the host's expire timer removes it within a
  # minute, and the host caps every hold at one hour, so the queue always
  # ends.
  tmp=$(mktemp "$QH_MOUNT/.hold.XXXXXX")
  # mktemp makes it 0600; other holders read `expires` from it.
  chmod 0644 "$tmp"
  queue_max="${QH_QUEUE_MAX_S:-3900}"
  queue_poll="${QH_QUEUE_POLL_S:-30}"
  queued=0
  # Stamped afresh before every attempt, so queueing time does not count
  # against the host's cap on this hold. Never rewritten once it is the
  # hold: the host reads it on every change, and a half-written flag reads
  # as expired.
  #
  # Free means the host has also confirmed the last release (no `applied`),
  # not just that the flag is gone: while a hold stands the host keeps the
  # time it began, and a hold taken over before the host has seen the last
  # one end would inherit that start and be capped early -- mid-measurement.
  until [ ! -f "$APPLIED" ] && stamp "$tmp" "$job" "$url" && ln "$tmp" "$HOLD" 2>/dev/null; do
    if [ "$queued" -ge "$queue_max" ]; then
      rm -f "$tmp"
      echo "host quieting is still held after ${queued}s: job=$(field "$HOLD" job) url=$(field "$HOLD" url)" >&2
      exit 3
    fi
    if [ -f "$HOLD" ]; then
      echo "quiet-host: host held by job=$(field "$HOLD" job) $(field "$HOLD" url); queued ${queued}s"
    else
      echo "quiet-host: waiting for the host to confirm the last release; queued ${queued}s"
    fi
    sleep "$queue_poll"
    queued=$((queued + queue_poll))
  done
  rm -f "$tmp"
  waited=0
  while [ "$waited" -lt 90 ]; do
    # #1092: the host writes $FAILED instead of $APPLIED when it could
    # not confirm gitlab-runner picked up concurrent=1 -- fail loudly
    # with that reason now, rather than reading a 90s timeout as the only
    # signal, or (before this fix) not failing at all.
    if [ -f "$FAILED" ]; then
      echo "host quieting failed: $(cat "$FAILED")" >&2
      exit 5
    fi
    if [ -f "$APPLIED" ] && grep -qE "job=${job}\$" "$APPLIED"; then
      echo "quiet-host: hold applied (${waited}s)"
      return 0
    fi
    echo "quiet-host: waiting for the host to apply the hold... ${waited}s"
    sleep 5
    waited=$((waited + 5))
  done
  echo "host quieting did not apply within 90s: is quiet-host.path enabled on the host?" >&2
  exit 4
}

cmd_release() {
  any_job=0
  if [ "${1:-}" = "--any-job" ]; then any_job=1; fi

  if [ ! -f "$HOLD" ]; then
    echo "quiet-host: nothing to release"
    return 0
  fi

  job="${CI_JOB_ID:-local}"
  holder=$(field "$HOLD" job)
  if [ "$holder" != "$job" ] && [ "$any_job" -ne 1 ]; then
    echo "quiet-host: hold belongs to job=$holder, not this job=$job -- not releasing (use --any-job to force)"
    return 0
  fi

  rm -f "$HOLD"
  echo "quiet-host: released hold for job=$holder"

  waited=0
  while [ "$waited" -lt 60 ]; do
    # Confirmed once `applied` no longer names this job: gone, or already
    # re-marked for the next job in the queue.
    if ! grep -qE "job=${holder}\$" "$APPLIED" 2>/dev/null; then
      echo "quiet-host: host confirmed release (${waited}s)"
      return 0
    fi
    sleep 5
    waited=$((waited + 5))
  done
  echo "quiet-host: warning -- applied flag still present after 60s; not failing the job over it" >&2
}

cmd_status() {
  if [ -f "$HOLD" ]; then
    echo "hold:"; cat "$HOLD"
  else
    echo "no hold"
  fi
  if [ -f "$APPLIED" ]; then
    echo "applied:"; cat "$APPLIED"
  else
    echo "not applied"
  fi
}

case "${1:-}" in
  hold) cmd_hold ;;
  release) shift; cmd_release "${1:-}" ;;
  status) cmd_status ;;
  *) echo "usage: scripts/quiet-host.sh hold|release [--any-job]|status" >&2; exit 2 ;;
esac
