#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# Runs every browser scenario -- or, with MV_SHARD=i/N, one slice of them,
# see plan() below -- against whatever instance the environment already
# points at (MV_URL/MV_USER/MV_PASS, and MV_ENV_SCRIPT for the
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

# The scenarios this run drives, in filename order, one per line. The
# exclusions live here rather than in the loop so the shard planner below
# and the loop agree on what a scenario is.
scenarios() {
  for scenario in frontend/scripts/live-*.mjs; do
    case "$scenario" in
      # The shared helper every scenario imports, not a scenario itself.
      *live-browser.mjs) continue ;;
      # Needs a real RouterOS CHR booted alongside the instance, which the
      # plain targets do not stand up. Run by `make live-routeros-container`.
      *live-routeros-real.mjs) continue ;;
    esac
    echo "$scenario"
  done
}

# Sharding (#1004). MV_SHARD=i/N runs the i-th of N contiguous slices of
# the list above against this instance, so N instances can take the suite
# in a fraction of the time. The whole suite ran at ~36 minutes on ~3
# cores; the point of slicing is a shorter window on a host CI shares,
# not more throughput -- #1004 has the measurements.
#
# The slices are contiguous, never interleaved, and they are cut only
# between *families* -- the word after `live-` (`city`, `history`,
# `topography`, `watchlist`...). Scenarios share one instance in filename
# order and a few lean on what a sibling left: live-history.mjs expects
# the day files live-history-control.mjs wrote, and
# live-watchlist-coverage.mjs starts from the non-logging table
# live-watchlist-broken-ring.mjs resets. Every one of those pairs shares a
# family, so a slice that never splits a family never splits a
# dependency -- and a new scenario that needs its sibling's leftovers has
# only to be named into the same family to stay beside it. The
# `live-before-*` pair, which asserts nothing has been pushed yet, keeps
# sorting first in the first slice, where nothing ahead of it pushes.
#
# The cut points are chosen by count, so the slices differ by at most one
# family. Time per scenario is printed below (`-- <scenario> Ns`) so the
# balance can be read off a run rather than guessed at.
#
# Validated here, at top level, not inside plan(): plan() runs in a command
# substitution below, where an exit would only empty the list and let the
# run "pass" having driven nothing.
if [ -n "${MV_SHARD:-}" ]; then
  case "$MV_SHARD" in
    [1-8]/[1-8]) ;;
    *) echo "run-scenarios: MV_SHARD must be i/N with 1 <= i <= N <= 8, got '$MV_SHARD'" >&2; exit 2 ;;
  esac
  if [ "${MV_SHARD%/*}" -gt "${MV_SHARD#*/}" ]; then
    echo "run-scenarios: MV_SHARD index exceeds the shard count: '$MV_SHARD'" >&2; exit 2
  fi
fi
plan() {
  if [ -z "${MV_SHARD:-}" ]; then
    scenarios
    return
  fi
  scenarios | awk -v i="${MV_SHARD%/*}" -v n="${MV_SHARD#*/}" '
    {
      family = $0
      sub(/^frontend\/scripts\/live-/, "", family)
      sub(/[-.].*$/, "", family)
      fam[NR] = family; file[NR] = $0
    }
    END {
      k = 1
      for (r = 1; r <= NR; r++) {
        # Move to the next slice at a family boundary once this one holds
        # its share: slice k ends when k*NR/n scenarios have been dealt.
        if (k < n && r - 1 >= k * NR / n && fam[r] != fam[r - 1]) k++
        if (k == i) print file[r]
      }
    }'
}

# --list prints the plan and runs nothing: what `MV_SHARD=2/4` would
# drive, for checking a split before spending a run on it.
if [ "${1:-}" = "--list" ]; then
  plan
  exit 0
fi

# A floor of traffic before the first scenario. The suite was written
# against one instance that the earlier scenarios had already been
# feeding for half an hour: live-group-mode.mjs waits for 200 rows and
# feeds none, live-entities-views.mjs 60, live-scroll-position.mjs 60,
# live-setup-wizard.mjs relies on events having arrived so the wizard's
# first auto-launch is already behind it, and live-journey.mjs says in so
# many words that "this instance has been pouring events since long
# before the page loaded". A slice that starts mid-suite starts on an
# empty instance, so the floor is fed here, in every run and every slice
# alike -- the same generator every scenario's own feed uses, under a
# label none of them filters on, so the first slice sees the same floor as
# the last and an unsharded run differs from before only by these rows.
env_script="${MV_ENV_SCRIPT:-scripts/live-env.sh}"
"$env_script" syslog 300 live-baseline >/dev/null 2>&1 || {
  echo "run-scenarios: could not feed the baseline through $env_script" >&2
  exit 1
}

status=0
ran=0
failed=0
for scenario in $(plan); do
  echo "== $scenario"
  started_at=$(date +%s)
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
  ran=$((ran + 1))
  # Wall time per scenario, on its own prefix: gate-remote.sh counts
  # `== ` lines as scenarios started, so this must not look like one.
  echo "-- $scenario $(( $(date +%s) - started_at ))s"
  if [ "$rc" -ne 0 ]; then
    status=1
    failed=$((failed + 1))
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
# One line a reader can find, whichever slice this was.
echo "SCENARIOS${MV_SHARD:+ shard $MV_SHARD}: $ran run, $failed failed"
exit $status
