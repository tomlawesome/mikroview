#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/ci-reuse-gate.js and ci-reuse-inputs.js end to end
# against a real scratch git repository -- the fall-through cases (not a
# merge request, no token, unknown job) and the property the whole
# feature depends on: a job's hash moves when a path it declares moves,
# and stays put when an unrelated path changes. The network-lookup half
# (walking earlier pipelines) is unit-tested with a fake fetch in
# ci-reuse-gate.test.js; this script never talks to a network.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
SCRIPT="$HERE/ci-reuse-gate.js"
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

# A scratch repo, not this one: the real tree's size and history are
# irrelevant, and computeHash must work from any git checkout.
GATE_REPO="$TMP/repo"
mkdir -p "$GATE_REPO/frontend" "$GATE_REPO/scripts" "$GATE_REPO/internal"
git -C "$GATE_REPO" init -q
git -C "$GATE_REPO" config user.email test@example.invalid
git -C "$GATE_REPO" config user.name test

# ci-reuse-gate.js requires its own two files as inputs to compute a hash
# in-repo, so the scratch tree needs harmless stand-ins at the same paths.
cp "$SCRIPT" "$GATE_REPO/scripts/ci-reuse-gate.js"
cp "$HERE/ci-reuse-inputs.js" "$GATE_REPO/scripts/ci-reuse-inputs.js"
printf 'stages: [lint]\n' > "$GATE_REPO/.gitlab-ci.yml"
printf 'package main\n' > "$GATE_REPO/internal/main.go"
echo 'module example' > "$GATE_REPO/go.mod"
echo '<svelte/>' > "$GATE_REPO/frontend/App.svelte"
echo '# readme' > "$GATE_REPO/README.md"
git -C "$GATE_REPO" add -A
git -C "$GATE_REPO" commit -q -m "seed"

run_gate() {  # run_gate <expected-exit> <label> <job> [env=val ...]
  local want="$1" label="$2" job="$3"; shift 3
  set +e
  ( cd "$GATE_REPO" && env "$@" node "$SCRIPT" gate "$job" ) >"$TMP/out" 2>&1
  local rc=$?
  set -e
  check "$rc" "$want" "$label"
}

run_gate 1 "not a merge request: runs" "test:postgres"
run_gate 1 "merge request, no token: runs" "test:postgres" CI_PIPELINE_SOURCE=merge_request_event
run_gate 1 "unknown job: runs" "no-such-job" CI_PIPELINE_SOURCE=merge_request_event CI_REUSE_TOKEN=t \
  CI_API_V4_URL=https://gitlab.example/api/v4 CI_PROJECT_ID=1 CI_MERGE_REQUEST_IID=2
grep -q "not in ci-reuse-inputs.js's JOB_INPUTS" "$TMP/out" && echo "ok - unknown job names the reason" \
  || { echo "FAIL - unknown job should name the reason"; fail=1; }

hash_for() {  # hash_for <job>
  ( cd "$GATE_REPO" && node "$SCRIPT" hash "$1" )
}

before="$(hash_for test:postgres)"
echo '# edited' >> "$GATE_REPO/README.md"
git -C "$GATE_REPO" -c user.email=test@example.invalid -c user.name=test commit -aqm "docs only"
after_docs="$(hash_for test:postgres)"
check "$before" "$after_docs" "test:postgres hash is unaffected by a docs-only change"

echo 'package main // changed' > "$GATE_REPO/internal/main.go"
git -C "$GATE_REPO" -c user.email=test@example.invalid -c user.name=test commit -aqm "go change"
after_go="$(hash_for test:postgres)"
if [ "$after_docs" = "$after_go" ]; then
  echo "FAIL - test:postgres hash should move when the Go tree changes"
  fail=1
else
  echo "ok - test:postgres hash moves when the Go tree changes"
fi

scenarios_1="$(hash_for 'gate:scenarios 1/4')"
scenarios_4="$(hash_for 'gate:scenarios 4/4')"
check "$scenarios_1" "$scenarios_4" "the four gate:scenarios shards share one hash for one tree"

before_ci="$(hash_for gate:image)"
printf 'stages: [lint, test]\n' > "$GATE_REPO/.gitlab-ci.yml"
git -C "$GATE_REPO" -c user.email=test@example.invalid -c user.name=test commit -aqm "ci change"
after_ci="$(hash_for gate:image)"
if [ "$before_ci" = "$after_ci" ]; then
  echo "FAIL - gate:image hash should move when .gitlab-ci.yml changes (COMMON)"
  fail=1
else
  echo "ok - gate:image hash moves when .gitlab-ci.yml changes (COMMON)"
fi

# evidence: writes a file naming the same hash `hash` reports, and reading
# it back is exactly what the lookup half compares against.
expected_pg_hash="$(hash_for test:postgres)"
( cd "$GATE_REPO" && env CI_PIPELINE_ID=555 CI_JOB_ID=777 node "$SCRIPT" evidence test:postgres ) >/dev/null
recorded="$(python3 -c "import json;print(json.load(open('$GATE_REPO/ci-reuse-evidence/test_postgres.json'))['inputs'])")"
check "$recorded" "$expected_pg_hash" "evidence records the same hash the CLI computes"

exit "$fail"
