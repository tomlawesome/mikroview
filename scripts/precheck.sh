#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The three local checks that CI runs and that are easiest to skip or get
# wrong before pushing. Run from anywhere; also `make precheck`.
#
#   1. gofmt -l over every tracked Go file. build, test and vet do NOT
#      catch formatting, so a clean `go test` says nothing about gofmt.
#      An empty file list is refused: a broken git makes `git ls-files`
#      print nothing, gofmt then reads stdin, and the check "passes"
#      having examined nothing (same guard as .gitlab-ci.yml, #908).
#   2. go test ./... -- even for a frontend-only change, because
#      injection_sinks_test.go scans frontend/src as text.
#   3. svelte-check against ./tsconfig.app.json, exactly as `make test`
#      runs it. The default tsconfig.json is a solution file with no
#      "include", so pointing at it reports 0 errors having checked nothing.
#
# Usage: scripts/precheck.sh [--dry-run]
#   --dry-run   print each command prefixed "+ " and run nothing.
#
# Healthy result: all three steps pass and the last line is
# "precheck: all passed". Any failure exits non-zero at that step.
set -euo pipefail

usage() {
  echo "usage: $0 [--dry-run]" >&2
}

DRY=0
case "${1:-}" in
  "") ;;
  --dry-run) DRY=1 ;;
  *) usage; exit 2 ;;
esac
[ "$#" -le 1 ] || { usage; exit 2; }

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

echo "+ gofmt -l \$(git ls-files '*.go')"
if [ "$DRY" != 1 ]; then
  files=$(git ls-files '*.go')
  if [ -z "$files" ]; then
    echo "git ls-files returned no Go files -- refusing to report a gofmt pass on an empty list" >&2
    exit 1
  fi
  # shellcheck disable=SC2086 # one path per word is the point
  unformatted=$(gofmt -l $files)
  if [ -n "$unformatted" ]; then
    echo "Not gofmt-clean:" >&2
    echo "$unformatted" >&2
    exit 1
  fi
fi

echo "+ go test ./..."
[ "$DRY" = 1 ] || go test ./...

echo "+ cd frontend && npx svelte-check --tsconfig ./tsconfig.app.json"
[ "$DRY" = 1 ] || (cd frontend && npx svelte-check --tsconfig ./tsconfig.app.json)

[ "$DRY" = 1 ] || echo "precheck: all passed"
