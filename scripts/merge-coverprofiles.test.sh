#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Tests for merge-coverprofiles.py (#1451): a block in both profiles is
# written once, covered if either run covered it, and inputs that cannot be
# merged honestly fail red.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
merger="$here/merge-coverprofiles.py"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
pass=0; fail=0

check() {
  if [ "$1" = "$2" ]; then
    echo "ok   $3"; pass=$((pass + 1))
  else
    echo "FAIL $3"; printf '       got:  %s\n       want: %s\n' "$1" "$2"
    fail=$((fail + 1))
  fi
}

m=example.com/m
printf 'mode: atomic\n%s/a.go:1.1,2.1 3 0\n%s/a.go:3.1,4.1 2 5\n%s/a.go:5.1,6.1 1 0\n' $m $m $m > "$work/unit.out"
printf 'mode: atomic\n%s/a.go:1.1,2.1 3 4\n%s/a.go:3.1,4.1 2 1\n%s/b.go:1.1,2.1 7 0\n' $m $m $m > "$work/bin.out"

python3 "$merger" "$work/merged.out" "$work/unit.out" "$work/bin.out"
check "$(cat "$work/merged.out")" "mode: atomic
$m/a.go:1.1,2.1 3 4
$m/a.go:3.1,4.1 2 6
$m/a.go:5.1,6.1 1 0
$m/b.go:1.1,2.1 7 0" "each block once, counts added, blocks from either side kept"

printf 'mode: set\n%s/a.go:1.1,2.1 3 1\n' $m > "$work/s1.out"
printf 'mode: set\n%s/a.go:1.1,2.1 3 1\n' $m > "$work/s2.out"
python3 "$merger" "$work/set.out" "$work/s1.out" "$work/s2.out"
check "$(tail -1 "$work/set.out")" "$m/a.go:1.1,2.1 3 1" "set mode stays 0 or 1"

expect_exit() {
  local want="$1" name="$2"; shift 2
  local got=0
  python3 "$merger" "$work/x.out" "$@" >/dev/null 2>&1 || got=$?
  check "$got" "$want" "$name"
}
expect_exit 2 "modes differ" "$work/unit.out" "$work/s1.out"
printf 'mode: atomic\n%s/a.go:1.1,2.1 4 1\n' $m > "$work/mismatch.out"
expect_exit 2 "same block, different statement count" "$work/unit.out" "$work/mismatch.out"
printf '%s/a.go:1.1,2.1 3 1\n' $m > "$work/nomode.out"
expect_exit 2 "no mode line" "$work/nomode.out"
expect_exit 2 "missing input" "$work/absent.out"
expect_exit 2 "no inputs"

echo "merge-coverprofiles.test.sh: $pass passed, $fail failed"
[ "$fail" = 0 ]
