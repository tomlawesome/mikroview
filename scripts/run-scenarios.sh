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
  # before printing its own RESULT line. The log then showed a header,
  # some passing checks and a stack trace, with no RESULT anywhere, so
  # reading a run by counting RESULT: PASS against RESULT: FAIL reported
  # a clean browser phase while a scenario was timing out in it. That is
  # #661, and it went unnoticed across two full runs.
  #
  # The exit status was always right and `make live-check` always failed.
  # What was missing was a line saying so where a reader looks for one.
  # So: capture the output to tell "failed and said so" from "died
  # without saying anything", while still streaming it, because a silent
  # forty minutes is its own problem. tee is a pipeline and this is
  # /bin/sh with no PIPESTATUS, hence the exit code going via a file.
  log="$(mktemp)"; rc_file="$log.rc"
  # The && / || is not style: under `set -e` a bare failing command in
  # the brace group kills the subshell before the exit code is recorded,
  # so the rc file is never written. A condition context suppresses that.
  { ( cd frontend && node "../$scenario" ) 2>&1 && echo 0 > "$rc_file" \
      || echo $? > "$rc_file"; } | tee "$log"
  rc="$(cat "$rc_file")"
  if [ "$rc" -ne 0 ]; then
    status=1
    # Only when the scenario never got as far as its own verdict. A
    # scenario that throws -- a stale selector, an import error -- dies
    # before printing one, so the log showed a header, some passing
    # checks, a stack trace, and no RESULT line anywhere. Reading a run
    # by counting RESULT: PASS against RESULT: FAIL therefore showed a
    # clean browser phase while a scenario was timing out in it (#661,
    # missed across two full runs that way).
    grep -q '^RESULT: ' "$log" || \
      echo "RESULT: FAIL ($scenario exited $rc without printing a result)"
  fi
  rm -f "$log" "$rc_file"
done
exit $status
