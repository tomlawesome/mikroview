#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The gate's started-vs-reported count. Never judge a run by counting
# PASS against FAIL: a scenario that throws -- a stale selector, an
# import error -- dies before printing any verdict, so counting verdicts
# cannot see it. That is #661, and it read as a clean browser phase
# across two full runs. Counting scenarios started against scenarios
# that reported can see it: equal means every one of them spoke.
#
# Over-reporting and under-reporting are the same defect -- a log that is
# not a faithful record of what happened (#1337). So this must come out
# equal on every clean run, with no fudge factor: a scenario or
# standalone script counts as one start and must print exactly one
# verdict line, `^== ` and `^RESULT: |^PASS: ` respectively. A script that
# prints its own nested `== ` sub-heading (as live-migrate-data.sh used
# to) inflates "started" against the same one verdict; a script that
# ends some other way (as the live-web-dist scripts used to) never
# satisfies "reported" at all. Both are bugs in the script, not
# exceptions the count should carry.
#
# Shared by gate-local.sh and gate-remote.sh so the two summaries cannot
# drift apart, and so gate-summary.test.sh can prove the count both
# ways without a live gate run.
#
# Sourced, never run.
mv_gate_summary() {
  local log="$1"
  local started reported silent
  started=$(grep -c '^== ' "$log" || true)
  reported=$(grep -cE '^RESULT: |^PASS: ' "$log" || true)

  echo
  echo "==> scenarios started: $started   reported: $reported"
  silent=$(( started - reported ))
  if [ "$silent" -gt 0 ]; then
    echo "==> $silent scenario(s) died without reporting -- see gate-run.log"
  fi
}
