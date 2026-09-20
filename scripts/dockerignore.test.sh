#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# .gitignore keeps a recorded upgrade fixture (.upgrade-fixtures/,
# password and token hashes plus a TLS key -- see docs/upgrades.md) out
# of the history. The Dockerfile's `COPY . .` reads the checkout, not
# the history, so the same directory has to be named in .dockerignore
# too or `make container` on a machine that ran
# scripts/fetch-upgrade-fixtures.sh ships it into the build context and
# the daemon's layer cache. This checks the two files agree on every
# path that must never leave the machine. Same reasoning as
# check-release-surfaces.test.sh: a rule said in one place drifts.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

# Paths that hold secret-shaped material and are gitignored for that
# reason. Add here whenever .gitignore gains another such entry.
for path in .upgrade-fixtures; do
  ok=false
  grep -qxE "/?${path}/?" "$ROOT/.gitignore" && ok=true
  check "$ok" ".gitignore names $path"
  ok=false
  grep -qxE "/?${path}/?" "$ROOT/.dockerignore" && ok=true
  check "$ok" ".dockerignore names $path"
done

if [ "$fails" -gt 0 ]; then
  echo "dockerignore.test.sh: $fails failure(s)" >&2
  exit 1
fi
echo "dockerignore.test.sh: all passed"
