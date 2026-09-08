#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Refuses to let a build go ahead with nothing in web/dist.
#
# #1003. An empty web/dist is a *legal* build: API-only is a supported
# product, and web/embed.go keeps a committed .gitkeep precisely so
# `go build` succeeds without a UI. Nothing in the Go toolchain objects.
# So when scripts/live-env.sh's copy step was refused on the `big`
# runner -- "cannot remove 'web/dist/.gitkeep': Permission denied" --
# the build still produced a binary, and the only symptom arrived 30
# seconds later as a probe timing out looking for a login field. Three
# perf runs were measured against a binary with no app in it before the
# cause was found.
#
# A check lives here, in its own file, rather than inline in live-env.sh
# so that it can actually be tested (scripts/assert-ui-built.test.sh).
# An assertion nobody has ever watched fail is not an assertion.
#
# Usage: scripts/assert-ui-built.sh [dir]   (default web/dist)
set -euo pipefail

DIR="${1:-web/dist}"
me="assert-ui-built"

contents() {
  if [ -d "$DIR" ]; then
    ls -A "$DIR" 2>/dev/null | tr '\n' ' '
  else
    echo "(no such directory)"
  fi
}

if [ ! -d "$DIR" ]; then
  echo "$me: $DIR does not exist -- the frontend was never copied in." >&2
  exit 1
fi

if [ ! -f "$DIR/index.html" ]; then
  echo "$me: $DIR has no index.html, so a binary built from it would have no app in it." >&2
  echo "$me: contents: $(contents)" >&2
  echo "$me: run 'npm run build' in frontend/ and copy its output here (the Makefile's build target does this)." >&2
  exit 1
fi

# index.html alone is not enough: a half-copied or stale dist has the
# page but not the bundle it loads, which serves a blank screen whose
# scripts 404 -- the exact failure #353 recorded when a placeholder
# index.html used to be committed.
if ! ls "$DIR"/assets/*.js >/dev/null 2>&1; then
  echo "$me: $DIR has index.html but no built asset bundle under assets/ -- a stale or half-copied dist." >&2
  echo "$me: contents: $(contents)" >&2
  exit 1
fi

exit 0
