#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Weekly CHR exercise (#894): boots MikroTik's own CHR image under
# software-emulated QEMU -- no /dev/kvm needed, per the owner's decision
# on #894: this exercises configuration commands, not throughput -- at
# the newest stable RouterOS release, and runs the wizard's starting
# command set (CA trust, syslog, rule tagging) against it. Green means
# every command parsed; red names the first one RouterOS refused.
#
# This is its own container, not `make live-check`: it never runs the
# live gate and never touches ~/gate-lock (AGENTS.md, "The second host
# live-check runs on"). It reuses scripts/live-routeros.sh to boot and
# talk to the router, and the same freshness comparison
# scripts/routeros-freshness.sh uses (via scripts/routerosfreshness) to
# decide whether there is anything to exercise at all.
#
# What "refused" means here, and its one known blind spot: RouterOS
# rejects a command it does not understand immediately, with wording
# like "bad command name" or "syntax error" -- that is what this script
# treats as red. The CA-trust step's `/tool fetch` also always fails at
# runtime here, because MVCHR_EXERCISE_ADDRESS is a placeholder
# (RFC 5737 TEST-NET-3) with nothing listening on it -- there is no live
# mikroview instance in this job for a real router to reach. That
# failure prints RouterOS's own "failure: ..." text, which is
# deliberately NOT one of the refusal patterns below: it is what an
# unreachable host is supposed to look like, not a syntax problem. This
# has not been checked against a real booted CHR (the constraint this
# script was written under forbids running one) -- treat the first real
# job run's transcript as the thing that calibrates this, and widen or
# narrow the pattern list from what it actually shows.
#
# Usage: scripts/routeros-chr-exercise.sh
# Exit 0: nothing to do (already covered), or a green exercise ran and
#         a row-appending PR was opened.
# Exit 1: RouterOS refused a command -- see stderr for which one.
# Exit 2: could not even determine what to exercise (feed unreachable,
#         CHR would not boot, etc).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO"

FEED="${ROUTEROS_VERSION_FEED:-https://upgrade.mikrotik.com/routeros/NEWESTa7.stable}"
# The same placeholder address docs/routeros-setup.md's own examples
# use. Nothing this exercise runs needs it to be reachable, only to be
# well-formed input to RouterOS's command syntax.
EXERCISE_ADDRESS="${MVCHR_EXERCISE_ADDRESS:-203.0.113.10:8443}"
EXERCISE_SYSLOG_PORT="${MVCHR_EXERCISE_SYSLOG_PORT:-6514}"

log() { printf '%s\n' "$*" >&2; }

if ! raw="$(curl -fsS --max-time 30 "$FEED")"; then
  log "routeros-chr-exercise: could not reach $FEED"
  exit 2
fi
candidate="${raw%% *}"
if [ -z "$candidate" ]; then
  log "routeros-chr-exercise: $FEED returned nothing that looks like a version: $raw"
  exit 2
fi

reviewed="$(go run ./scripts/routerosfreshness -print-reviewed)"

# Exit status is the answer, same contract routeros-freshness.sh reads:
# 0 not newer (a row already covers it), 1 newer (nothing covers it
# yet), 2 could not compare. stderr is dropped only for this call, same
# reason routeros-freshness.sh drops it: `go run` prints its own "exit
# status 1" line for a non-zero child, which would sit above and read
# as the failure.
set +e
go run ./scripts/routerosfreshness -candidate "$candidate" 2>/dev/null
cmp_status=$?
set -e

if [ "$cmp_status" -eq 2 ]; then
  log "routeros-chr-exercise: could not compare $candidate against $reviewed"
  exit 2
fi
if [ "$cmp_status" -eq 0 ]; then
  log "routeros-chr-exercise: RouterOS $candidate is already covered by a row (reviewed up to $reviewed). Nothing to exercise."
  exit 0
fi

log "routeros-chr-exercise: RouterOS $candidate has no row yet -- booting CHR $candidate to exercise it"

export CHR_VERSION="$candidate"

# shellcheck disable=SC2317  # only invoked indirectly, via 'trap cleanup EXIT' below
cleanup() {
  scripts/live-routeros.sh down >/dev/null 2>&1 || true
}
trap cleanup EXIT

boot_start=$(date +%s)
if ! up_output="$(scripts/live-routeros.sh up)"; then
  log "routeros-chr-exercise: CHR $candidate would not boot"
  exit 2
fi
eval "$up_output"
boot_end=$(date +%s)
log "routeros-chr-exercise: CHR $candidate reached a prompt in $((boot_end - boot_start))s"

commands="$(go run ./scripts/routeroscommands -step all -address "$EXERCISE_ADDRESS" -syslog-port "$EXERCISE_SYSLOG_PORT")"

LOG_FILE="$(mktemp)"

# RouterOS's own vocabulary for "this command does not parse" -- as
# opposed to a runtime failure like an unreachable host, see the header
# comment above for why "failure:" (the /tool fetch runtime error) is
# deliberately not here.
REFUSAL_PATTERN='bad command name|no such command|syntax error|expected end of command|unknown parameter|not enough arguments|bad command syntax|extra arguments|invalid value for'

refused=""
run_start=$(date +%s)
while IFS= read -r cmd; do
  case "$cmd" in
    '' | '#'*) continue ;;
  esac
  out="$(scripts/live-routeros.sh run "$cmd" 2>&1 || true)"
  {
    printf '== %s\n' "$cmd"
    printf '%s\n' "$out"
  } >>"$LOG_FILE"
  if printf '%s\n' "$out" | grep -qiE "$REFUSAL_PATTERN"; then
    refused="$cmd"
    break
  fi
done <<<"$commands"
run_end=$(date +%s)
log "routeros-chr-exercise: ran the starting commands in $((run_end - run_start))s"

if [ -n "$refused" ]; then
  log ""
  log "routeros-chr-exercise: RouterOS $candidate refused a command:"
  log "  $refused"
  log "transcript:"
  cat "$LOG_FILE" >&2
  rm -f "$LOG_FILE"
  exit 1
fi

log "routeros-chr-exercise: every starting command parsed on RouterOS $candidate ($(( $(date +%s) - boot_start ))s total)"

"$REPO/scripts/routeros-chr-open-mr.sh" "$candidate" "$LOG_FILE"
status=$?
rm -f "$LOG_FILE"
exit "$status"
