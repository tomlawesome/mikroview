#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/ci-changed-paths-guard.py against the real
# .gitlab-ci.yml lists, plus the two diff-base cases that must pass
# without consulting git at all.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/ci-changed-paths-guard.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected exit [$2] got [$1]"
    fail=1
  fi
}
run_stdin() {  # run_stdin <expected-exit> <label> <paths...>
  local want="$1" label="$2"; shift 2
  set +e
  printf '%s\n' "$@" | python3 "$SCRIPT" --stdin >"$TMP/out" 2>"$TMP/err"
  local rc=$?
  set -e
  check "$rc" "$want" "$label"
}

run_stdin 0 "docs-only diff passes" README.md docs/configuration.md docs/screenshots/x.png LICENSE
run_stdin 0 "code diff passes" main.go frontend/src/App.svelte go.mod
run_stdin 0 "empty diff passes"
run_stdin 1 "unlisted path fails" newdir/thing.rs
grep -q 'newdir/thing.rs' "$TMP/err" && echo "ok - failure names the path" || { echo "FAIL - failure names the path"; fail=1; }
grep -q 'code_paths' "$TMP/err" && echo "ok - failure says where to add it" || { echo "FAIL - failure says where to add it"; fail=1; }
run_stdin 1 "doc + unlisted path fails" README.md newdir/thing.rs
run_stdin 0 "THIRD-PARTY-NOTICES.md is listed (as code)" THIRD-PARTY-NOTICES.md
run_stdin 0 "docs mockup is listed (as code)" docs/design/concepts/round-46/index.html

# The lists must come from the CI file: a file without them is exit 2.
printf 'stages: [lint]\n' >"$TMP/no-lists.yml"
set +e
echo main.go | python3 "$SCRIPT" --stdin --ci-file "$TMP/no-lists.yml" >/dev/null 2>"$TMP/err"; rc=$?
set -e
check "$rc" "2" "missing lists is a red job, not a pass"

# Diff-base cases that pass by design (GitLab's changes: runs everything there).
set +e
CI_PIPELINE_SOURCE=push CI_COMMIT_BEFORE_SHA=0000000000000000000000000000000000000000 \
  python3 "$SCRIPT" --git >"$TMP/out" 2>&1; rc=$?
set -e
check "$rc" "0" "all-zeros before SHA passes"
grep -q 'nothing to guard' "$TMP/out" && echo "ok - all-zeros prints its note" || { echo "FAIL - all-zeros note"; fail=1; }
set +e
CI_PIPELINE_SOURCE=merge_request_event CI_MERGE_REQUEST_DIFF_BASE_SHA=deadbeefdeadbeefdeadbeefdeadbeefdeadbeef \
  python3 "$SCRIPT" --git >"$TMP/out" 2>&1; rc=$?
set -e
check "$rc" "0" "unreachable base passes"
grep -q 'not in this clone' "$TMP/out" && echo "ok - unreachable prints its note" || { echo "FAIL - unreachable note"; fail=1; }

exit "$fail"
