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
  ( cd frontend && node "../$scenario" ) || status=1
done
exit $status
