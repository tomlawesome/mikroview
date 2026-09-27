#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises mv_gate_summary (gate-summary.sh) against fabricated logs.
# #1337: the summary claimed scenarios "died without reporting" on every
# run, including clean ones, because a fudge factor in the count covered
# for two scripts' broken output. #661 is why the count exists at all --
# a scenario that throws prints no verdict, so PASS-against-FAIL cannot
# see it. Both directions have to hold: a clean run must not cry wolf,
# and a real silent death must still be caught.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/gate-summary.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

# A clean run: every scenario that started also reported. Started and
# reported must come out equal, and the summary must say so without any
# "may exceed by exactly 1" excuse -- there is nothing left to excuse.
clean_log="$TMP/clean.log"
cat >"$clean_log" <<'EOF'
== scripts/live-cert-reload.sh
PASS: SIGHUP swaps the certificate on both listeners.
== scripts/live-logspam-check.sh
PASS: rejection logging is throttled and counted.
== scripts/live-migrate-data.sh
ok    a real instance started against the source directory
-- migrate-data, against a real instance --
PASS: a deployment survives -migrate-data and starts from its new home.
== scripts/live-tls-log-lines.sh
PASS: handshake failures are explained, actionable, and not repeated.
EOF

clean_out="$(mv_gate_summary "$clean_log")"
check "$(echo "$clean_out" | grep -q 'started: 4   reported: 4' && echo true || echo false)" \
  "a clean run counts equal started and reported"
check "$(echo "$clean_out" | grep -q 'may exceed reported' && echo false || echo true)" \
  "a clean run's summary carries no excuse clause"
check "$(echo "$clean_out" | grep -q 'died without reporting' && echo false || echo true)" \
  "a clean run is not accused of a silent death"

# The nested "-- migrate-data --" line above is deliberately not "== ",
# proving the rename (#1337) keeps it from being counted as a second
# start. If it regressed to "== migrate-data..." this log would count 5
# starts against 4 reports and the first check above would fail.

# #661: a scenario that throws dies before printing any verdict. Prove
# the count still catches that -- this is the failure mode the whole
# check exists for, and it must survive the #1337 fix.
dying_log="$TMP/dying.log"
cat >"$dying_log" <<'EOF'
== scripts/live-cert-reload.sh
PASS: SIGHUP swaps the certificate on both listeners.
== scripts/live-logspam-check.sh
TypeError: cannot read properties of undefined (reading 'foo')
EOF

dying_out="$(mv_gate_summary "$dying_log")"
check "$(echo "$dying_out" | grep -q 'started: 2   reported: 1' && echo true || echo false)" \
  "a scenario that throws is still counted as started without reporting"
check "$(echo "$dying_out" | grep -q '^==> 1 scenario(s) died without reporting' && echo true || echo false)" \
  "the summary names exactly one silent death"

echo
if [ "$fails" -ne 0 ]; then
  echo "RESULT: FAIL"
  exit 1
fi
echo "PASS: the started-vs-reported count is exact on a clean run and still catches a silent death."
