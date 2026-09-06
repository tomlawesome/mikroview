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
# Polls /proc/loadavg every 15s for up to 10 minutes, printing each
# reading, until the 1-minute load average is below the number of cores.
# Exits 0 the moment it is; exits 1 with "host not quiet: load X on N
# cores" if it never is within the 10 minutes.
#
# Usage: scripts/wait-quiet-host.sh
set -euo pipefail

readonly POLL_INTERVAL_S=15
readonly MAX_WAIT_S=600

cores="$(nproc)"
elapsed=0

while true; do
  load1="$(awk '{print $1}' /proc/loadavg)"
  echo "load1=${load1} cores=${cores} elapsed=${elapsed}s"

  if awk -v load="$load1" -v cores="$cores" 'BEGIN { exit !(load < cores) }'; then
    exit 0
  fi

  if [ "$elapsed" -ge "$MAX_WAIT_S" ]; then
    echo "host not quiet: load ${load1} on ${cores} cores" >&2
    exit 1
  fi

  sleep "$POLL_INTERVAL_S"
  elapsed=$((elapsed + POLL_INTERVAL_S))
done
