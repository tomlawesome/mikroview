#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/precheck.sh --dry-run and the `make precheck` target:
# the checks CI runs that are easiest to forget (gofmt over tracked Go
# files, go test ./..., svelte-check against tsconfig.app.json) have to
# come out in that order, and the Makefile has to route to the script.
# Nothing is run: --dry-run only prints. The empty-file-list refusal is
# not covered here: the spec gives no way to reach it without knowing the
# script's internals.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$HERE/precheck.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

lineno() {
  local n
  n="$(grep -nE -m1 -- "$1" "$TMP/out" | cut -d: -f1 || true)"
  echo "${n:-0}"
}

rc=0
bash "$SCRIPT" --dry-run >"$TMP/out" 2>"$TMP/err" || rc=$?
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "--dry-run exits 0 (got $rc)"

fmt="$(lineno 'gofmt')"
gt="$(lineno 'go test')"
sv="$(lineno 'svelte-check')"

check "$([ "$fmt" -gt 0 ] && echo true || echo false)" "gofmt step appears"
check "$([ "$gt" -gt 0 ] && echo true || echo false)" "go test step appears"
check "$([ "$sv" -gt 0 ] && echo true || echo false)" "svelte-check step appears"

ordered=false
if [ "$fmt" -gt 0 ] && [ "$fmt" -lt "$gt" ] && [ "$gt" -lt "$sv" ]; then
  ordered=true
fi
check "$ordered" "order is gofmt < go test < svelte-check"

gofmt_line="$(grep -E -m1 -- 'gofmt' "$TMP/out" || true)"
check "$([[ "$gofmt_line" == *"gofmt -l"* ]] && echo true || echo false)" "gofmt line uses 'gofmt -l'"
check "$([[ "$gofmt_line" == *"git ls-files"* && "$gofmt_line" == *"*.go"* ]] && echo true || echo false)" "gofmt line takes tracked files from git ls-files '*.go'"

gt_line="$(grep -E -m1 -- 'go test' "$TMP/out" || true)"
check "$([[ "$gt_line" == *"go test ./..."* ]] && echo true || echo false)" "go test line is 'go test ./...'"

sv_line="$(grep -E -m1 -- 'svelte-check' "$TMP/out" || true)"
check "$([[ "$sv_line" == *"tsconfig.app.json"* ]] && echo true || echo false)" "svelte-check line names tsconfig.app.json"
check "$([[ -n "$sv_line" && "$sv_line" != *" ./tsconfig.json"* && "$sv_line" != *"tsconfig.json"* ]] && echo true || echo false)" "svelte-check line does not use tsconfig.json"

prefixed=false
[ -s "$TMP/out" ] && prefixed=true
for pat in 'gofmt' 'go test' 'svelte-check'; do
  grep -E -- "$pat" "$TMP/out" | grep -qvE '^\+ ' && prefixed=false
done
check "$prefixed" "every command line is prefixed '+ '"

rc=0
make -C "$ROOT" -n precheck >"$TMP/mout" 2>"$TMP/merr" || rc=$?
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "make -n precheck succeeds (got $rc)"
check "$(grep -q 'scripts/precheck\.sh' "$TMP/mout" && echo true || echo false)" "make -n precheck runs scripts/precheck.sh"

rc=0
bash "$SCRIPT" --bogus >"$TMP/bout" 2>"$TMP/berr" || rc=$?
check "$([ "$rc" -eq 2 ] && echo true || echo false)" "unknown flag exits 2 (got $rc)"

if [ "$fails" -gt 0 ]; then
  echo "precheck.test.sh: $fails failure(s)" >&2
  exit 1
fi
echo "precheck.test.sh: all passed"
