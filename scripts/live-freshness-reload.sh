#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# #1363: drives frontend/scripts/live-freshness-reload.mjs -- the case
# #1362 was actually about, an already-open tab whose server was
# upgraded out from under it -- against its own dedicated instance
# rather than the shared one scripts/run-scenarios.sh's browser phase
# drives.
#
# That check restarts the server mid-run (live-env.sh's `upgrade`) to
# simulate a real upgrade, with a real service worker handing over
# control before the page reloads. Every browser scenario shares one
# instance and most depend on state an earlier one left in it (the
# live-check skill, "a scenario cannot be judged on its own") --
# restarting that instance here would silently wipe it for everything
# that runs after this one in filename order. So this runs standalone,
# like live-cert-reload.sh and its siblings: its own ports, after the
# shared instance is already down (run-live-scripts.sh's own doc
# comment).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
. "$REPO/scripts/live-slot.sh"

mv_require_free_port "$MV_STANDALONE_HTTP_PORT" "the freshness-reload check's server"
mv_require_free_port "$MV_STANDALONE_SYSLOG_TLS_PORT" "the freshness-reload check's syslog-TLS listener"

export MV_DIR
MV_DIR="$(mktemp -d /tmp/mikroview-live-freshness.XXXXXX)"
export MV_HTTP_PORT="$MV_STANDALONE_HTTP_PORT"
export MV_SYSLOG_TLS_PORT="$MV_STANDALONE_SYSLOG_TLS_PORT"

cleanup() {
  scripts/live-env.sh down >/dev/null 2>&1 || true
}
trap cleanup EXIT

mv_env="$(scripts/live-env.sh up)"
eval "$mv_env"

( cd frontend && node scripts/live-freshness-reload.mjs )
