#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Fails when MikroTik has published a stable RouterOS with no row
# covering it in internal/routeros/dialects.go (Rows) -- mechanically,
# that means newer than routeros.ReviewedVersion
# (internal/routeros/versions.go), which is kept equal to the table's own
# newest row by TestReviewedVersionMatchesNewest, so comparing against
# the constant and comparing against the table agree by construction.
#
# What it is not: a check that anything is broken. RouterOS command
# syntax drifts between releases, mikroview presents commands the
# operator pastes into their router, and nothing was watching for a
# release nobody had read. Full semantic diffing of the command set is
# not automatable; a loud "a stable release exists with no row" is, and
# is the actual requirement (#436).
#
# Acting on a failure means reading that release's notes against the
# commands in internal/routeros/commands.go and docs/routeros-setup.md
# (or exercising them against a real router, the stronger claim a row's
# VerifiedBy can make), fixing anything that moved, and only then adding
# or extending a row in internal/routeros/dialects.go and bumping
# ReviewedVersion to match. Bumping ReviewedVersion to silence this
# without reading anything is the one wrong response.
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
routeros-freshness: a stable release exists with no row in
internal/routeros/dialects.go -- RouterOS $upstream has shipped, and the
newest row only covers up to $reviewed.

Nothing is necessarily broken. What this means is that nobody has
exercised or reviewed $upstream against the commands mikroview asks
operators to paste into their routers.

To clear it:
  1. Read the release notes (or exercise the commands against a real
     router) for changes affecting internal/routeros/commands.go and
     docs/routeros-setup.md.
  2. Fix anything that moved.
  3. Add or extend a row in internal/routeros/dialects.go's Rows,
     with an honest VerifiedBy, and bump ReviewedVersion in
     internal/routeros/versions.go to match its newest bound.

Bumping ReviewedVersion without doing 1 and 2 -- exercising or reviewing
the release -- is the one wrong response.
MSG
exit 1
