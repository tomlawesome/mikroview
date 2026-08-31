#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Runs every standalone live check -- the ones that stand up their own
# server rather than driving the shared instance scripts/run-scenarios.sh
# uses.
#
# Found by glob, deliberately. Its sibling has never had a check rot,
# because adding `frontend/scripts/live-<thing>.mjs` is sufficient: the
# runner finds it. These scripts had no runner at all, so wiring one in
# was a second, separate edit, and nobody made it. Three of them drifted
# into being unable to start a server at all and produced no signal for
# months (#595, #624). A convention that needs a second edit will
# eventually not get it.
#
# They cannot join run-scenarios.sh: that runs against an instance
# already standing, and each of these starts and stops its own on fixed
# ports. So they run as their own phase, after the shared instance is
# down.
#
# Each is expected to print its own checks and exit non-zero on failure.
set -eu

status=0
for script in scripts/live-*.sh; do
  case "$script" in
    # Not checks: the shared environment helpers the checks and the
    # scenarios both use. live-slot.sh is sourced, never run -- executed
    # as a check it defines its variables in a subshell, exits 0 and
    # reports nothing, which reads as a check that passed.
    scripts/live-env.sh|scripts/live-container.sh|scripts/live-stores.sh|scripts/live-slot.sh) continue ;;
    # Needs a real RouterOS CHR booted alongside the instance. Run by
    # `make live-routeros-container`.
    scripts/live-routeros.sh) continue ;;
    # Probes driven by live-routeros.sh rather than standalone checks.
    scripts/live-routeros-step0.sh|scripts/live-rule-coverage-probe.sh) continue ;;
  esac
  echo "== $script"
  bash "$script" || status=1
done
exit $status
