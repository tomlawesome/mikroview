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

# Q6C-F1: gate-local.sh has --shard i/N (run one slice, e.g. to reproduce a
# single red CI shard without four browsers' worth of contention); this
# host script had no way to pass that through at all. Its argument parsing
# and validation run before the dirty-tree check and before anything
# touches the network, so the invalid/conflicting cases below can be run
# directly -- deterministic regardless of this checkout's own git status --
# without ever attempting SSH. A *valid* --shard is checked by shape (grep)
# instead, since actually running it would either hit the dirty-tree
# refusal or a real (and here unreachable) second host.
has "--shard)   SHARD=\"\$2\"; shift 2 ;;" "--shard is a recognised flag"
has "MV_SHARD=\$SHARD live-check" "a valid --shard threads MV_SHARD into the remote make invocation, same as gate-local.sh"
has "[--shards N | --shard i/N]" "the usage string mentions --shard"

set +e
out="$("$SCRIPT" --shard 9/2 2>&1)"; rc=$?
set -e
check "$rc" "2" "--shard with i>8 exits 2"
grep -qF "must be i/N with 1 <= i <= N <= 8" <<< "$out" && echo "ok - out-of-range --shard names the rule" || { echo "FAIL - out-of-range --shard names the rule: $out"; fail=1; }

set +e
out="$("$SCRIPT" --shard 2/1 2>&1)"; rc=$?
set -e
check "$rc" "2" "--shard with index > count exits 2"
grep -qF "exceeds the shard count" <<< "$out" && echo "ok - --shard 2/1 is refused as index-exceeds-count" || { echo "FAIL - --shard 2/1 is refused as index-exceeds-count: $out"; fail=1; }

set +e
out="$("$SCRIPT" --shards 2 --shard 1/2 2>&1)"; rc=$?
set -e
check "$rc" "2" "--shards and --shard together exits 2"
grep -qF "pick one" <<< "$out" && echo "ok - --shards and --shard together is refused" || { echo "FAIL - --shards and --shard together is refused: $out"; fail=1; }

exit "$fail"
