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

# #671: an outside kill of the process running a scenario (or of
# whatever started this run and forwards the signal on, see the
# Makefile) used to leave the scenario's node process orphaned to init,
# still driving Chromium against an instance nobody owns any more. The
# old code ran node inside a plain foreground pipeline (`{ ... } | tee`),
# and a plain foreground command is not interruptible: dash's default
# reaction to a signal it has no trap for is to die on the spot without
# touching its own children, so the shell went away and node kept going.
#
# The fix is the same shape as every other live-* script's `trap
# cleanup EXIT` (live-cert-reload.sh, live-migrate-data.sh, ...), plus
# the two pieces that make it fire promptly instead of only on a normal
# exit:
#
#   - the node process is backgrounded and `wait`-ed on, because `wait`
#     is the one blocking construct POSIX carves out as interruptible --
#     a signal for which a trap is set makes `wait` return immediately
#     (128+signal), rather than deferring the trap until the foreground
#     command finishes, which is what a plain foreground pipeline does.
#   - the subshell that changes into frontend `exec`s node rather than
#     just running it, so the backgrounded PID this script tracks *is*
#     node's own PID and not a wrapper shell that would itself orphan
#     node a second time if killed.
child=""
log=""
# shellcheck disable=SC2317  # only invoked indirectly, via 'trap ... stop N' below
stop() {
  [ -n "$child" ] && kill "$child" 2>/dev/null
  [ -n "$log" ] && rm -f "$log"
  # $1 is the signal number the caller's trap was armed with; 128+n is
  # the conventional exit status for death by signal n.
  exit "$((128 + $1))"
}
trap 'stop 15' TERM
trap 'stop 2' INT

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
  # So: capture node's output to a file rather than only streaming it,
  # to tell "failed and said so" from "died without saying anything".
  log="$(mktemp)"
  ( cd frontend && exec node "../$scenario" ) >"$log" 2>&1 &
  child=$!
  # Still streamed live -- a silent forty minutes is its own problem --
  # but from the file rather than through node's own stdout, so this
  # script blocks on `wait`, not on the pipeline itself. `--pid` is a
  # GNU coreutils extension; live-check only ever runs on the Linux
  # hosts this project targets (AGENTS.md's "second host"), so that is
  # not a portability cost paid here. It stops following (after a final
  # read) once node exits, so it never outlives the scenario the way an
  # unmanaged `tail -f` would.
  tail -f -n +1 --pid="$child" "$log" 2>/dev/null &
  tail_pid=$!
  rc=0
  wait "$child" || rc=$?
  child=""
  wait "$tail_pid" 2>/dev/null || true
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
  rm -f "$log"
done
exit $status
