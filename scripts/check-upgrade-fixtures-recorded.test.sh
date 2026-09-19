#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/check-upgrade-fixtures-recorded.sh (#1274) against a
# throwaway git fixture with real tags, a stub `glab` standing in for
# the generic package registry (no network, no real GitLab credential),
# and testdata/upgrade/<version>/manifest.json files it creates itself.
# Same reasoning as check-release-surfaces.test.sh: a check nobody has
# watched fail is not a check.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/check-upgrade-fixtures-recorded.sh"

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

# stub_glab <dir> [versions...] -- a fake glab whose `api` subcommand
# answers the one endpoint this script calls (listing generic packages
# named upgrade-fixtures) with one package entry per version given.
# STUB_GLAB_FAIL=1 makes it fail outright, simulating a registry outage.
stub_glab() {
  local dir="$1"; shift
  mkdir -p "$dir"
  if [ "${STUB_GLAB_FAIL:-0}" = "1" ]; then
    cat >"$dir/glab" <<'EOF'
#!/bin/sh
echo "glab: simulated failure" >&2
exit 1
EOF
  else
    local body="[" first=1 v
    for v in "$@"; do
      [ "$first" = 1 ] || body="$body,"
      body="$body{\"name\":\"upgrade-fixtures\",\"version\":\"$v\"}"
      first=0
    done
    body="$body]"
    cat >"$dir/glab" <<EOF
#!/bin/sh
echo '$body'
EOF
  fi
  chmod +x "$dir/glab"
}

new_repo() { # new_repo <dir>
  local dir="$1"
  rm -rf "$dir"
  mkdir -p "$dir/testdata/upgrade" "$dir/scripts"
  git -C "$dir" init -q
  git -C "$dir" config user.email "test@example.com"
  git -C "$dir" config user.name "Test"
  cp "$SCRIPT" "$dir/scripts/check-upgrade-fixtures-recorded.sh"
  chmod +x "$dir/scripts/check-upgrade-fixtures-recorded.sh"
  printf 'x\n' >"$dir/README.md"
  git -C "$dir" add -A
  git -C "$dir" commit -q -m initial
}

commit_manifest() { # commit_manifest <dir> <version>
  local dir="$1" v="$2"
  mkdir -p "$dir/testdata/upgrade/$v"
  echo '{}' >"$dir/testdata/upgrade/$v/manifest.json"
  git -C "$dir" add -A
  git -C "$dir" commit -q -m "record $v manifest"
}

run() { # run <dir> <stub-bin-dir>
  local dir="$1" binpath="$2"
  set +e
  out="$(cd "$dir" && env -u CI_JOB_TOKEN -u CI_API_V4_URL -u CI_PROJECT_ID \
    PATH="$binpath:$PATH" bash scripts/check-upgrade-fixtures-recorded.sh 2>&1)"
  rc=$?
  set -e
}

# --- every released tag has a committed manifest -> passes ---------------
r1="$TMP/r1"
new_repo "$r1"
git -C "$r1" tag v0.1.0
commit_manifest "$r1" v0.1.0
bin1="$TMP/bin1"; stub_glab "$bin1" v0.1.0
run "$r1" "$bin1"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "every released tag with a committed manifest passes (rc=$rc)"
check "$(case "$out" in *"every released tag"*"has a recorded fixture"*) echo true;; *) echo false;; esac)" \
  "and says so"

# --- a tag recorded via the registry alone (no committed manifest yet) ---
r2="$TMP/r2"
new_repo "$r2"
git -C "$r2" tag v0.2.0
bin2="$TMP/bin2"; stub_glab "$bin2" v0.2.0
run "$r2" "$bin2"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "a tag recorded in the registry alone (manifest not yet committed) passes (rc=$rc)"

# --- a tag with no committed manifest and not in the registry -> FAIL ----
r3="$TMP/r3"
new_repo "$r3"
git -C "$r3" tag v0.3.0
bin3="$TMP/bin3"; stub_glab "$bin3"   # empty registry
run "$r3" "$bin3"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a tag with no fixture anywhere fails (rc=$rc)"
check "$(case "$out" in *"v0.3.0 -- run: scripts/record-upgrade-fixture.sh v0.3.0"*) echo true;; *) echo false;; esac)" \
  "and names the exact command to record it"

# --- two tags, only one recorded -> only the unrecorded one is named -----
r4="$TMP/r4"
new_repo "$r4"
git -C "$r4" tag v0.1.0
git -C "$r4" tag v0.4.0
commit_manifest "$r4" v0.1.0
bin4="$TMP/bin4"; stub_glab "$bin4" v0.1.0
run "$r4" "$bin4"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "one recorded tag alongside one unrecorded tag still fails (rc=$rc)"
check "$(case "$out" in *"v0.4.0 -- run: scripts/record-upgrade-fixture.sh v0.4.0"*) echo true;; *) echo false;; esac)" \
  "and names only the unrecorded one"
check "$(case "$out" in *"v0.1.0 -- run:"*) echo false;; *) echo true;; esac)" \
  "not the already-recorded one"

# --- no v* tags at all -> passes, says so ---------------------------------
r5="$TMP/r5"
new_repo "$r5"
bin5="$TMP/bin5"; stub_glab "$bin5"
run "$r5" "$bin5"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "no released tags yet passes (rc=$rc)"
check "$(case "$out" in *"nothing to check"*) echo true;; *) echo false;; esac)" \
  "and says so"

# --- registry outage, but committed manifests already cover everything ----
r6="$TMP/r6"
new_repo "$r6"
git -C "$r6" tag v0.1.0
commit_manifest "$r6" v0.1.0
bin6="$TMP/bin6"; STUB_GLAB_FAIL=1 stub_glab "$bin6"
run "$r6" "$bin6"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "a registry outage does not fail the check when committed manifests already cover everything (rc=$rc)"

# --- registry outage AND a real gap -> fails, with the caveat noted -------
r7="$TMP/r7"
new_repo "$r7"
git -C "$r7" tag v0.5.0
bin7="$TMP/bin7"; STUB_GLAB_FAIL=1 stub_glab "$bin7"
run "$r7" "$bin7"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a registry outage with a real gap still fails (rc=$rc)"
check "$(case "$out" in *"registry listing failed"*) echo true;; *) echo false;; esac)" \
  "and notes the registry could not be checked, rather than pretending it confirmed the gap"

echo
if [ "$fails" -ne 0 ]; then
  echo "check-upgrade-fixtures-recorded.test.sh: $fails check(s) failed"
  exit 1
fi
echo "check-upgrade-fixtures-recorded.test.sh: all checks passed"
