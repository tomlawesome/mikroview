#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/assert-ui-built.sh against temp directories, one per
# way the copy can go wrong. #1003's whole lesson is that an unwatched
# assertion is worth nothing, so each case here is run and its exit code
# and message checked, rather than the script merely being present.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/assert-ui-built.sh"

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

run() { # run <dir>; sets rc and out
  set +e
  out="$("$SCRIPT" "$1" 2>&1)"
  rc=$?
  set -e
}

# --- a good dist passes --------------------------------------------------
good="$TMP/good"
mkdir -p "$good/assets"
echo '<!doctype html>' >"$good/index.html"
echo '// bundle' >"$good/assets/index-abc123.js"
touch "$good/.gitkeep"
run "$good"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "a dist with index.html and a bundle passes (rc=$rc)"

# --- the #1003 case: the copy never ran, only .gitkeep is there ----------
empty="$TMP/empty"
mkdir -p "$empty"
touch "$empty/.gitkeep"
run "$empty"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a dist holding only .gitkeep fails (rc=$rc)"
check "$(case "$out" in *"no index.html"*) echo true;; *) echo false;; esac)" \
  "and says index.html is missing"
check "$(case "$out" in *".gitkeep"*) echo true;; *) echo false;; esac)" \
  "and prints what was actually in there"

# --- half-copied: the page but not the bundle it loads -------------------
half="$TMP/half"
mkdir -p "$half"
echo '<!doctype html>' >"$half/index.html"
run "$half"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "index.html with no assets/ bundle fails (rc=$rc)"
check "$(case "$out" in *"no built asset bundle"*) echo true;; *) echo false;; esac)" \
  "and says the bundle is missing rather than repeating the index.html message"

# --- an assets dir with no js in it is still half-copied -----------------
nojs="$TMP/nojs"
mkdir -p "$nojs/assets"
echo '<!doctype html>' >"$nojs/index.html"
echo 'body{}' >"$nojs/assets/index-abc123.css"
run "$nojs"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "an assets/ dir with only css fails (rc=$rc)"

# --- missing directory ---------------------------------------------------
run "$TMP/not-there"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a missing directory fails (rc=$rc)"
check "$(case "$out" in *"does not exist"*) echo true;; *) echo false;; esac)" \
  "and says so plainly"

echo
if [ "$fails" -ne 0 ]; then
  echo "assert-ui-built.test.sh: $fails check(s) failed"
  exit 1
fi
echo "assert-ui-built.test.sh: all checks passed"
