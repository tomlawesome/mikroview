#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Tests for coverage-floor.py (#1333). A ratchet that cannot fail ratchets
# nothing, so every rule it enforces has a case here that proves it fires.
# Ported from birdcage's scripts/coverage-floor.test.sh.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
checker="$here/coverage-floor.py"
repo_root="$(cd "$here/.." && pwd)"
module="$(awk '/^module /{print $2; exit}' "$repo_root/go.mod")"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
pass=0; fail=0

# expect <want-exit> <name> <profile-content-or-PATH:x> <floors-content-or-PATH:x> [checker-arg...]
# The checker resolves the module path from go.mod above the current
# directory, so it is always run with repo_root as the working directory.
# Trailing args (e.g. --section postgres-floors) go before the profile and
# floors paths, matching the checker's own argv order.
expect() {
  local want="$1" name="$2" profile="$3" floors="$4"; shift 4
  local extra=("$@") got=0 out
  local prof_arg="$work/cover.out" floors_arg="$work/floors.yml"

  if [[ "$profile" == PATH:* ]]; then
    prof_arg="${profile#PATH:}"
  else
    printf '%s\n' "$profile" > "$prof_arg"
  fi
  if [[ "$floors" == PATH:* ]]; then
    floors_arg="${floors#PATH:}"
  else
    printf '%s\n' "$floors" > "$floors_arg"
  fi

  out="$(cd "$repo_root" && python3 "$checker" "${extra[@]}" "$prof_arg" "$floors_arg" 2>&1)" || got=$?
  if [ "$got" = "$want" ]; then
    echo "ok   $name"; pass=$((pass + 1))
  else
    echo "FAIL $name: exit $got, want $want"; echo "$out" | sed 's/^/       /'
    fail=$((fail + 1))
  fi
}

# A Go coverage profile line is "file:pos numstmt count" -- count is a hit
# count for the whole block, not "how many of numstmt were covered": if
# count > 0 every statement in that block counts as covered, otherwise none
# do. So a fractional package percentage in a fixture needs two blocks, one
# hit and one not, not one line with a fractional-looking count.
#
# block <pkg-dir> <covered-stmts> <uncovered-stmts> -- one profile line for
# the covered statements (count 1) and, if any, one more for the uncovered
# ones (count 0), both attributed to <pkg-dir> via its synthetic file path.
block() {
  local pkg="$1" covered="$2" uncovered="$3" f
  f="$module/$pkg/x.go"
  printf '%s:1.1,2.1 %d 1\n' "$f" "$covered"
  if [ "$uncovered" -gt 0 ]; then
    printf '%s:3.1,4.1 %d 0\n' "$f" "$uncovered"
  fi
}

# A root-level package (no directory component) is the one place mikroview
# diverges from birdcage's fixture: mikroview's `package main` lives at the
# module root, not under a subdirectory, and coverage-floor.py maps that to
# the key ".". Exercised on its own here since birdcage's original test
# never had a root package to cover.
root_block() {
  local covered="$1" uncovered="$2" f
  f="$module/x.go"
  printf '%s:1.1,2.1 %d 1\n' "$f" "$covered"
  if [ "$uncovered" -gt 0 ]; then
    printf '%s:3.1,4.1 %d 0\n' "$f" "$uncovered"
  fi
}

# A package well within its floor (80% against a floor of 79), used as
# filler in fixtures that need one clean package alongside the package
# under test, so the fixture's only problem is the one being tested for.
clean_block="$(block internal/clean 8 2)"
clean_floor="  internal/clean: 79"

expect 1 "a package under its floor is caught" \
"mode: set
$clean_block
$(block internal/low 5 5)" \
"floors:
$clean_floor
  internal/low: 90"

expect 1 "a package in the profile but missing from the floors file is caught" \
"mode: set
$clean_block
$(block internal/unlisted 9 1)" \
"floors:
$clean_floor"

# The stale-ratchet boundary. These two cases sit either side of
# coverage-floor.py's RATCHET_SLACK, which is 5: 85% against a floor of 80
# is exactly at it and passes, 86% is one point past it and is caught. The
# number is written out here on purpose rather than read from the script --
# a test that follows the constant pins nothing.
expect 0 "a package exactly at the ratchet slack (5 points) still passes" \
"mode: set
$clean_block
$(block internal/edge 85 15)" \
"floors:
$clean_floor
  internal/edge: 80"

expect 1 "a package one point past the ratchet slack is caught" \
"mode: set
$clean_block
$(block internal/high 86 14)" \
"floors:
$clean_floor
  internal/high: 80"

expect 0 "the root package (no directory component) maps to '.'" \
"mode: set
$clean_block
$(root_block 8 2)" \
"floors:
$clean_floor
  .: 79"

expect 2 "an unreadable profile fails red, not green" \
"PATH:$work/does-not-exist.out" \
"floors:
$clean_floor"

expect 2 "an unreadable floors file fails red, not green" \
"mode: set
$clean_block" \
"PATH:$work/does-not-exist.yml"

expect 0 "a clean pass across multiple packages" \
"mode: set
$clean_block
$(block internal/ok 79 21)" \
"floors:
$clean_floor
  internal/ok: 79"

# #1290: postgres-floors:, the second section test:postgres checks instead
# of test:go. Four cases, one per rule the build notes name.

expect 0 "default mode skips packages listed under postgres-floors, even ones that would otherwise fail" \
"mode: set
$clean_block
$(block internal/persist 5 95)
$(block internal/matchlog 5 95)" \
"floors:
$clean_floor

postgres-floors:
  internal/persist: 62
  internal/matchlog: 58"

expect 1 "section mode: a postgres-floors package under its floor is caught" \
"mode: set
$(block internal/persist 50 50)" \
"postgres-floors:
  internal/persist: 62" \
--section postgres-floors

expect 1 "section mode flags a postgres-floors package never measured in the profile" \
"mode: set
$(block internal/persist 70 30)" \
"postgres-floors:
  internal/persist: 62
  internal/matchlog: 58" \
--section postgres-floors

expect 2 "an unknown --section exits 2, same as a missing floors mapping" \
"mode: set
$clean_block" \
"floors:
$clean_floor" \
--section no-such-section

echo "$pass passed, $fail failed"
[ "$fail" = 0 ]
