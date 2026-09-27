#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# gate-remote.sh reaches a real second host over SSH and runs a live
# instance there, so nothing here can exercise it end to end -- this
# checks its shape instead: it parses, and it contains the tidy step
# #1387 added (both prune commands, the non-fatal guard, the log prefix).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/gate-remote.sh"

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}
has() {  # has <pattern> <label>
  if grep -qF -- "$1" "$SCRIPT"; then
    echo "ok - $2"
  else
    echo "FAIL - $2 (looking for: $1)"
    fail=1
  fi
}

set +e
bash -n "$SCRIPT"
check "$?" "0" "gate-remote.sh parses (bash -n)"
set -e

has "docker image prune -f" "prunes dangling images (no -a, so the tagged image stays)"
has "docker builder prune -af" "prunes the build cache"
has "|| true" "a prune failure is guarded so it cannot fail the gate"
has "==> tidy:" "tidy output is prefixed for gate-run.log"

# The image prune must not carry -a/-af, or the tagged mv-gate:local image
# the next run relies on as its cache would go too.
if grep -qF -- "docker image prune -af" "$SCRIPT" || grep -qF -- "docker image prune -a " "$SCRIPT"; then
  echo "FAIL - image prune must stay dangling-only (no -a), or mv-gate:local would be pruned"
  fail=1
else
  echo "ok - image prune is dangling-only"
fi

exit "$fail"
