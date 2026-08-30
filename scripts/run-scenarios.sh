#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# Runs every browser scenario against whatever instance the environment
# already points at (MV_URL/MV_USER/MV_PASS, and MV_ENV_SCRIPT for the
# feeders). Owns no lifecycle: bringing something up and tearing it down
# belongs to the caller, which is what lets the identical set of
# scenarios run against a local binary, against the shipped container,
# and against the container with Postgres behind it.
#
# One file rather than the same loop pasted into three Makefile targets.
# The loop had already been copied three times, and the exclusion list
# below is exactly the kind of thing that gets updated in one copy and
# not the other two -- a scenario needing a booted router would then
# appear to fail in the two plain targets rather than being skipped.
set -eu

status=0
for scenario in frontend/scripts/live-*.mjs; do
  case "$scenario" in
    # The shared helper every scenario imports, not a scenario itself.
    *live-browser.mjs) continue ;;
    # Needs a real RouterOS CHR booted alongside the instance, which the
    # plain targets do not stand up. Run by `make live-routeros-container`.
    *live-routeros-real.mjs) continue ;;
  esac
  echo "== $scenario"
  # A scenario that *throws* -- a stale selector, an import error -- dies
  # before it can print its own RESULT line, so the log showed a header,
  # some passing checks and then nothing. Reading a run by counting
  # RESULT: PASS against RESULT: FAIL therefore showed a clean browser
  # phase while a scenario was timing out in it, which is exactly what
  # happened with #661 and went unnoticed across two full runs.
  #
  # The exit status was always right and `make live-check` always failed.
  # What was missing was a line saying so where a reader looks for one.
  if ( cd frontend && node "../$scenario" ); then
    :
  else
    echo "RESULT: FAIL ($scenario exited $?, before printing its own result)"
    status=1
  fi
done
exit $status
