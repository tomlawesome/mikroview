#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Applies or releases the quiet-host hold on this box's one gitlab-runner
# service (issue #1003). A perf:promotion job writes $QH_DIR/hold to ask
# every other runner job to back off while it measures; this script is
# root's side of that: it sets `concurrent = 1` in the runner config while
# a hold is active, and puts the original value back once it is not.
#
# Run by quiet-host.service, triggered by quiet-host.path (on any change
# under $QH_DIR) and by quiet-host-expire.timer (once a minute), so an
# expired hold is released even if nothing else touches the directory.
#
# Idempotent: running this with the same state twice changes nothing the
# second time and says so.
set -euo pipefail

# The largest legitimate hold is a job timeout plus 300s (scripts/quiet-host.sh
# bounds it to now + CI_JOB_TIMEOUT + 300, under now+1800 for perf:promotion's
# 25m timeout); 3600 sits above the longest job that holds one.
MAX_HOLD_S=3600

QH_DIR="${QH_DIR:-/srv/quiet-host}"
# The saved value lives outside the watched directory, so saving it does
# not re-trigger quiet-host.path (five starts in ten seconds hit systemd's
# start limit on the first install, 2026-09-06).
QH_STATE="${QH_STATE:-/var/lib/quiet-host}"
CONFIG="${CONFIG:-/etc/gitlab-runner/config.toml}"

HOLD="$QH_DIR/hold"
ORIG="$QH_STATE/concurrent.orig"
APPLIED="$QH_DIR/applied"
FAILED="$QH_DIR/failed"

apply_hold() {
  job="$1"
  match_count=$(grep -c '^concurrent = ' "$CONFIG" || true)
  if [ "$match_count" -ne 1 ]; then
    echo "refusing: expected exactly one '^concurrent = ' line in $CONFIG, found $match_count" >&2
    exit 1
  fi
  if [ ! -f "$ORIG" ]; then
    mkdir -p "$QH_STATE"
    value=$(sed -n 's/^concurrent = \(.*\)$/\1/p' "$CONFIG")
    printf '%s\n' "$value" >"$ORIG"
  fi
  sed -i 's/^concurrent = .*/concurrent = 1/' "$CONFIG"

  # #1092: the old `|| true` swallowed a reload failure and wrote APPLIED
  # regardless, so perf:promotion believed the host was quiet while other
  # jobs still ran alongside it. Check the exit status for real.
  reload_rc=0
  systemctl reload gitlab-runner || reload_rc=$?
  if [ "$reload_rc" -ne 0 ]; then
    printf 'systemctl reload gitlab-runner exited %s for job=%s at %s\n' \
      "$reload_rc" "$job" "$(date +%s)" >"$FAILED"
    echo "FAILED: systemctl reload gitlab-runner exited $reload_rc -- hold not confirmed for job=$job" >&2
    return 1
  fi

  # A successful reload only proves the signal was delivered and
  # accepted, not that gitlab-runner re-read the file -- there is no CLI
  # to ask it its live concurrency. The strongest check reachable from a
  # script (README.md, "Verify it") is that the config still reads what
  # this run wrote to it.
  picked_up=$(sed -n 's/^concurrent = \(.*\)$/\1/p' "$CONFIG")
  if [ "$picked_up" != "1" ]; then
    printf '%s reads concurrent=%s after reload, not 1, for job=%s at %s\n' \
      "$CONFIG" "$picked_up" "$job" "$(date +%s)" >"$FAILED"
    echo "FAILED: $CONFIG reads concurrent=$picked_up after reload, not 1 -- hold not confirmed for job=$job" >&2
    return 1
  fi

  rm -f "$FAILED"
  printf 'concurrent=1 at %s for job=%s\n' "$(date +%s)" "$job" >"$APPLIED"
  echo "HOLD applied: concurrent=1 for job=$job"
}

release_hold() {
  value=$(cat "$ORIG")
  sed -i "s/^concurrent = .*/concurrent = $value/" "$CONFIG"
  systemctl reload gitlab-runner 2>/dev/null || true
  rm -f "$ORIG" "$APPLIED" "$FAILED"
  echo "RELEASE: restored concurrent=$value"
}

now=$(date +%s)
flag_job=""
flag_expires=""
held=0
expired=0

if [ -f "$HOLD" ]; then
  flag_job=$(grep -m1 '^job=' "$HOLD" | cut -d= -f2-)
  flag_expires=$(grep -m1 '^expires=' "$HOLD" | cut -d= -f2-)
  case "$flag_expires" in
    ''|*[!0-9]*) flag_expires=0 ;; # malformed -- treat as already expired
  esac

  flag_started=$(grep -m1 '^started=' "$HOLD" | cut -d= -f2-)
  case "$flag_started" in
    ''|*[!0-9]*) flag_started=$now ;; # malformed -- fall back to now
  esac

  # #1094: the hold file's expires= is written by an unprivileged job and
  # not otherwise trusted -- cap it at MAX_HOLD_S past the hold's own
  # started= (or now, if that is missing) regardless of what the file
  # claims.
  ceiling=$((flag_started + MAX_HOLD_S))
  if [ "$flag_expires" -gt "$ceiling" ]; then
    echo "hold expiry capped: file requested expires=$flag_expires, capping to $ceiling for job=$flag_job"
    flag_expires=$ceiling
  fi

  if [ "$flag_expires" -gt "$now" ]; then
    held=1
  else
    expired=1
  fi
fi

if [ "$held" -eq 1 ]; then
  if [ -f "$FAILED" ]; then
    # #1092: the previous attempt could not confirm the reload, so there
    # is nothing to stand on -- retry the reload and verification rather
    # than re-marking APPLIED on faith. apply_hold's own ORIG guard makes
    # this safe to call again: it will not re-save the original value,
    # only redo the (idempotent) sed and the reload+verify.
    apply_hold "$flag_job"
  elif [ -f "$ORIG" ]; then
    # A fresh flag can land right after an expired one was released but
    # before this run: keep the marker naming the job that holds now, so
    # the job side's wait for "job=<its id>" sees it.
    if ! grep -q "job=${flag_job}\$" "$APPLIED" 2>/dev/null; then
      printf 'concurrent=1 at %s for job=%s\n' "$now" "$flag_job" >"$APPLIED"
      echo "HOLD re-marked for job=$flag_job (concurrent already 1)"
    else
      echo "hold already applied for job=$flag_job (expires $flag_expires) -- nothing to do"
    fi
  else
    apply_hold "$flag_job"
  fi
else
  if [ -f "$ORIG" ]; then
    release_hold
  fi
  if [ "$expired" -eq 1 ]; then
    echo "EXPIRED job=$flag_job expires=$flag_expires -- released"
    rm -f "$HOLD"
  fi
fi
