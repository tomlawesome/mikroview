#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Verifies, against a real RouterOS, that the filter-rule push carries
# the fields #274's "this entry can never match" check depends on: `log`,
# `dstAddress` and `srcAddress`.
#
# A script rather than a shell one-liner because the first attempt was
# one: python embedded in bash embedded in a subshell, whose quoting
# broke, and whose teardown then ran anyway so the whole thing reported
# exit 0. The failure was in the middle of a pipeline nobody was checking
# -- the same shape as the harness bugs #273 already recorded.
#
# Usage: scripts/live-rule-coverage-probe.sh
# Brings its own container and router up, and tears them down.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

cleanup() {
  scripts/live-routeros.sh down >/dev/null 2>&1 || true
  scripts/live-container.sh down >/dev/null 2>&1 || true
}
trap cleanup EXIT

export MV_ENV_SCRIPT=scripts/live-container.sh
MV_BIND="$(scripts/live-routeros.sh host-addr)"
export MV_BIND

eval "$(scripts/live-container.sh up)"
eval "$(scripts/live-routeros.sh up)"
scripts/live-routeros.sh setup "$MV_URL" "$MV_BIND" "$MV_SYSLOG_TLS_PORT"

# The address shapes this feature turns on, on top of what setup adds.
# The last one deliberately does not log: it is the case `log` exists to
# make visible, and the one LogPrefix-presence guessing got wrong.
scripts/live-routeros.sh run \
  '/ip firewall filter add chain=forward action=drop dst-address=203.0.113.9 log=yes log-prefix="D|one-ip|"' \
  '/ip firewall filter add chain=forward action=drop dst-address=!10.0.0.0/8 log=yes log-prefix="D|negated|"' \
  '/ip firewall filter add chain=forward action=drop dst-address=10.0.0.1-10.0.0.5 src-address=192.168.88.0/24 log=yes log-prefix="D|range|"' \
  '/ip firewall filter add chain=forward action=drop dst-address=198.51.100.0/24 comment="silent"' >/dev/null

jar="$(mktemp)"
curl -fsS -k -c "$jar" -X POST -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d "{\"username\":\"$MV_USER\",\"password\":\"$MV_PASS\"}" "$MV_URL/api/auth/login" -o /dev/null

# Traffic first, so mikroview knows which device the router is before a
# token gets scoped to it.
scripts/live-routeros.sh traffic 2 >/dev/null

device="$(curl -fsS -k -b "$jar" "$MV_URL/api/devices" | python3 -c 'import json,sys; print(json.load(sys.stdin)["devices"][0]["id"])')"
token="$(curl -fsS -k -b "$jar" -X POST -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d "{\"name\":\"rule-coverage-probe\",\"kind\":\"ingest\",\"device\":\"$device\"}" \
  "$MV_URL/api/tokens" | python3 -c 'import json,sys; print(json.load(sys.stdin)["value"])')"

scripts/live-routeros.sh push "$MV_URL" "$token" >/dev/null

curl -fsS -k -b "$jar" "$MV_URL/api/routeros/$device/rules" \
  | python3 "$REPO/scripts/live-rule-coverage-check.py"
