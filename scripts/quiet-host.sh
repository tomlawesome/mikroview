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

cmd_hold() {
  if [ ! -d "$QH_MOUNT" ]; then
    echo "host quieting is not installed: /quiet-host is not mounted -- see deploy/quiet-host/README.md" >&2
    exit 2
  fi

  now=$(date +%s)
  if [ -f "$HOLD" ]; then
    existing_expires=$(field "$HOLD" expires)
    case "$existing_expires" in ''|*[!0-9]*) existing_expires=0 ;; esac
    if [ "$existing_expires" -gt "$now" ]; then
      echo "host quieting is already held: job=$(field "$HOLD" job) url=$(field "$HOLD" url)" >&2
      exit 3
    fi
  fi

  job="${CI_JOB_ID:-local}"
  url="${CI_JOB_URL:-}"
  expires=$((now + ${CI_JOB_TIMEOUT:-1800} + 300))

  tmp=$(mktemp "$QH_MOUNT/.hold.XXXXXX")
  {
    echo "job=$job"
    echo "url=$url"
    echo "started=$now"
    echo "expires=$expires"
  } >"$tmp"
  mv "$tmp" "$HOLD"

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
    if [ ! -f "$APPLIED" ]; then
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
