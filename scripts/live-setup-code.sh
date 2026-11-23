#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# #1415: drives frontend/scripts/live-setup-code.mjs -- the create-account
# screen asking for the setup code from the server's log -- against its
# own instance with no account, rather than the shared one
# scripts/run-scenarios.sh's browser phase drives. That one already holds
# its admin, so it logs no code and never shows the screen; and creating
# the first admin here is a one-way step no other scenario could share.
# So this runs standalone, like live-freshness-reload.sh: its own ports,
# after the shared instance is down (run-live-scripts.sh's own comment).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
. "$REPO/scripts/live-slot.sh"

mv_require_free_port "$MV_STANDALONE_HTTP_PORT" "the setup-code check's server"
mv_require_free_port "$MV_STANDALONE_SYSLOG_TLS_PORT" "the setup-code check's syslog-TLS listener"

export MV_DIR
MV_DIR="$(mktemp -d /tmp/mikroview-live-setup-code.XXXXXX)"
export MV_HTTP_PORT="$MV_STANDALONE_HTTP_PORT"
export MV_SYSLOG_TLS_PORT="$MV_STANDALONE_SYSLOG_TLS_PORT"

cleanup() {
  scripts/live-env.sh down >/dev/null 2>&1 || true
  rm -rf "$MV_DIR"
}
trap cleanup EXIT

# MV_FIRST_RUN=1: no admin, so the setup code is still waiting in
# $MV_DIR/server.log for the scenario to read.
mv_env="$(MV_FIRST_RUN=1 scripts/live-env.sh up)"
eval "$mv_env"

( cd frontend && node scripts/live-setup-code.mjs )
