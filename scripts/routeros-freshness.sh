#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Fails when MikroTik has published a stable RouterOS newer than the
# release mikroview's command knowledge was last reviewed against
# (routeros.ReviewedVersion, internal/routeros/versions.go).
#
# What it is not: a check that anything is broken. RouterOS command
# syntax drifts between releases, mikroview presents commands the
# operator pastes into their router, and nothing was watching for a
# release nobody had read. Full semantic diffing of the command set is
# not automatable; a loud "a release shipped that nobody has reviewed"
# is, and is the actual requirement (#436).
#
# Acting on a failure means reading that release's notes against the
# commands in frontend/src/lib/setupsteps.ts and docs/routeros-setup.md,
# fixing anything that moved, and only then bumping ReviewedVersion.
# Bumping it to silence this without reading anything is the one wrong
# response.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO"

# MikroTik publish the current stable of each channel as a plain
# "<version> <unix-timestamp>" line. No account, no key, no licence
# attached to a version number -- nothing is stored or redistributed
# here, it is read and compared.
FEED="${ROUTEROS_VERSION_FEED:-https://upgrade.mikrotik.com/routeros/NEWESTa7.stable}"

if ! raw="$(curl -fsS --max-time 30 "$FEED")"; then
  echo "routeros-freshness: could not reach $FEED" >&2
  echo "Failing rather than passing: an unreachable feed is not evidence that nothing shipped." >&2
  exit 2
fi

upstream="${raw%% *}"
if [ -z "$upstream" ]; then
  echo "routeros-freshness: $FEED returned nothing that looks like a version: $raw" >&2
  exit 2
fi

reviewed="$(go run ./scripts/routerosfreshness -print-reviewed)"

# stderr is dropped only for the comparison call: `go run` prints its own
# "exit status 1" line for a non-zero child, which would sit above the
# explanation below and read as the failure. A genuine build or version
# error still exits 2 and is reported by the branch after this one.
set +e
go run ./scripts/routerosfreshness -candidate "$upstream" 2>/dev/null
cmp_status=$?
set -e
if [ "$cmp_status" -eq 2 ]; then
  echo "routeros-freshness: could not compare $upstream against $reviewed" >&2
  go run ./scripts/routerosfreshness -candidate "$upstream" >/dev/null || true
  exit 2
fi
if [ "$cmp_status" -eq 0 ]; then
  echo "routeros-freshness: RouterOS $upstream is current stable; reviewed up to $reviewed. Nothing to do."
  exit 0
fi

cat >&2 <<MSG
routeros-freshness: RouterOS $upstream has shipped, and mikroview's command
knowledge is only reviewed up to $reviewed.

Nothing is necessarily broken. What this means is that nobody has read
$upstream's release notes against the commands mikroview asks operators to
paste into their routers.

To clear it:
  1. Read the release notes for command-syntax changes affecting
     frontend/src/lib/setupsteps.ts and docs/routeros-setup.md.
  2. Fix anything that moved.
  3. Bump routeros.ReviewedVersion in internal/routeros/versions.go to $upstream.

Do not do step 3 alone.
MSG
exit 1
