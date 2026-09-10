#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Waits for the host to go quiet before a perf measurement runs on it
# (issue #1003's promotion gate: perf:promotion in .gitlab-ci.yml). A
# busy host makes a roll or a frame look slow for reasons that have
# nothing to do with the code under test, and GitLab has no built-in
# "wait for idle" -- this is the enforcement until a dedicated
# always-quiet runner exists (see .gitlab-ci.yml's comment above the
# job for that follow-up).
#
# Two readings, both host-wide because /proc is the host's inside a
# container, and both must pass:
#   - CPU busy over the last poll window, from /proc/stat, below
#     BUSY_MAX_PCT for two windows running. This is what actually
#     decides whether a frame is late. The load average alone let the
#     probe run while sibling shards were still building (#1099): the
#     shards start a second or two before this job, and a one-minute
#     average has not moved yet when the first poll reads it.
#   - 1-minute load average below the number of cores, as before, so a
#     queue of runnable work that has not landed on a CPU yet still
#     counts.
#
# Polls every POLL_INTERVAL_S for up to MAX_WAIT_S, printing each
# reading. Exits 0 as soon as both hold; exits 1 with "host not quiet"
# if they never do within MAX_WAIT_S.
#
# Usage: scripts/wait-quiet-host.sh
# QH_PROC_STAT / QH_PROC_LOADAVG name the files to read (for tests).
set -euo pipefail

readonly POLL_INTERVAL_S="${POLL_INTERVAL_S:-10}"
readonly MAX_WAIT_S="${MAX_WAIT_S:-600}"
readonly BUSY_MAX_PCT="${BUSY_MAX_PCT:-20}"
readonly QUIET_WINDOWS_NEEDED=2
readonly PROC_STAT="${QH_PROC_STAT:-/proc/stat}"
readonly PROC_LOADAVG="${QH_PROC_LOADAVG:-/proc/loadavg}"

cores="$(nproc)"
elapsed=0
quiet_windows=0

# "cpu  user nice system idle iowait irq softirq steal ..." -> "idle total"
cpu_sample() {
  awk '$1 == "cpu" { idle = $5 + $6; total = 0; for (i = 2; i <= NF; i++) total += $i; print idle, total; exit }' "$PROC_STAT"
}

read -r prev_idle prev_total <<<"$(cpu_sample)"

while true; do
  sleep "$POLL_INTERVAL_S"
  elapsed=$((elapsed + POLL_INTERVAL_S))

  read -r idle total <<<"$(cpu_sample)"
  busy_pct=$(awk -v i0="$prev_idle" -v t0="$prev_total" -v i1="$idle" -v t1="$total" \
    'BEGIN { d = t1 - t0; if (d <= 0) { print 0; exit }; printf "%d", 100 * (1 - (i1 - i0) / d) }')
  prev_idle=$idle
  prev_total=$total

  load1="$(awk '{print $1}' "$PROC_LOADAVG")"
  echo "busy=${busy_pct}% load1=${load1} cores=${cores} elapsed=${elapsed}s"

  if [ "$busy_pct" -lt "$BUSY_MAX_PCT" ] && awk -v load="$load1" -v cores="$cores" 'BEGIN { exit !(load < cores) }'; then
    quiet_windows=$((quiet_windows + 1))
    if [ "$quiet_windows" -ge "$QUIET_WINDOWS_NEEDED" ]; then
      exit 0
    fi
  else
    quiet_windows=0
  fi

  if [ "$elapsed" -ge "$MAX_WAIT_S" ]; then
    echo "host not quiet: busy ${busy_pct}%, load ${load1} on ${cores} cores" >&2
    exit 1
  fi
done
