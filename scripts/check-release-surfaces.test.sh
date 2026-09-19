#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/check-release-surfaces.sh against a throwaway git
# fixture, one case per way a release surface can drift: a changelog
# heading nobody cut, a dead GitHub Issues link (the tracker is off), a
# relative link with no target, a docs/ page nobody links to, a stale Go
# version claim, and a screenshot that predates the code it shows. Same
# reasoning as assert-ui-built.test.sh: a check nobody has watched fail
# is not a check.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/check-release-surfaces.sh"

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
  out="$(sh "$1/scripts/check-release-surfaces.sh" 2>&1)"
  rc=$?
  set -e
}

commit() { # commit <dir> <date> <message>
  local d="$1" date="$2" msg="$3"
  git -C "$d" add -A
  GIT_AUTHOR_DATE="$date" GIT_COMMITTER_DATE="$date" \
    git -C "$d" commit -q -m "$msg"
}

# A minimal, entirely-clean fixture: every check should pass against it
# unmodified. Each test case below copies this (git history and all)
# and breaks exactly one thing.
new_repo() { # new_repo <dir>
  local dir="$1"
  rm -rf "$dir"
  mkdir -p "$dir/docs/screenshots" "$dir/docs/reviews" "$dir/site" \
    "$dir/frontend/src/components" "$dir/frontend/src/lib" \
    "$dir/.github/workflows" "$dir/scripts"
  git -C "$dir" init -q
  git -C "$dir" config user.email "test@example.com"
  git -C "$dir" config user.name "Test"

  cp "$SCRIPT" "$dir/scripts/check-release-surfaces.sh"
  chmod +x "$dir/scripts/check-release-surfaces.sh"

  printf '0.1.0\n' >"$dir/VERSION"
  cat >"$dir/CHANGELOG.md" <<'EOF'
# Changelog

## [0.1.0] - 2026-01-01

Initial release.
EOF
  cat >"$dir/README.md" <<'EOF'
# Example

See [docs/x.md](docs/x.md) for details and
[docs/development.md](docs/development.md) to build it.

![screenshot](docs/screenshots/fall-dark.png)
EOF
  cat >"$dir/SECURITY.md" <<'EOF'
# Security

Report privately, not via a public tracker.
EOF
  cat >"$dir/docs/development.md" <<'EOF'
# Contributing

Requires Go 1.21+.
EOF
  cat >"$dir/go.mod" <<'EOF'
module example.com/x

go 1.21.0
EOF
  cat >"$dir/site/index.html" <<'EOF'
<!doctype html><title>x</title>
EOF
  cat >"$dir/docs/x.md" <<'EOF'
# X

Details.
EOF
  printf 'png-v1\n' >"$dir/docs/screenshots/fall-dark.png"
  cat >"$dir/.github/workflows/pages.yml" <<'EOF'
name: pages
on: push
EOF
  # fall-dark.png's mapped sources (check-release-surfaces.sh's own
  # screenshot_sources()) -- both must exist and predate the screenshot
  # in the fixture's history, or the "all-good fixture passes" baseline
  # itself would fail check 6.
  printf 'export const Fall = 1;\n' >"$dir/frontend/src/components/Fall.svelte"
  printf 'export const fall = 1;\n' >"$dir/frontend/src/lib/fall.svelte.ts"
  cat >"$dir/docs/reviews/2026-01-01-v0.1.0.md" <<'EOF'
# Pre-release review: v0.1.0

No findings.
EOF
  cat >"$dir/install.sh" <<'EOF'
#!/bin/sh
set -- run -d --name mikroview --restart unless-stopped \
  --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 128 \
  -p 6514:6514 -p 443:8080 \
  -v mikroview-data:/var/lib/mikroview -v mikroview-etc:/etc/mikroview \
  ghcr.io/tomlawesome/mikroview:latest
EOF
  mkdir -p "$dir/deploy"
  cat >"$dir/deploy/docker-compose.yml" <<'EOF'
services:
  mikroview:
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    pids_limit: 128
    read_only: true
EOF

  commit "$dir" "2026-01-01T00:00:00" "initial"
}

good="$TMP/good"
new_repo "$good"

# --- an all-good fixture passes ------------------------------------------
run "$good"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "an all-good fixture passes (rc=$rc)"

# --- check 1: changelog heading ------------------------------------------
c1="$TMP/case1-changelog"
cp -r "$good" "$c1"
cat >"$c1/CHANGELOG.md" <<'EOF'
# Changelog

## [9.9.9] - 2026-01-01

Wrong version heading.
EOF
run "$c1"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "changelog with no heading for VERSION fails (rc=$rc)"
check "$(case "$out" in *"FAIL: CHANGELOG.md has no '## [0.1.0] - ' heading"*) echo true;; *) echo false;; esac)" \
  "and names the missing heading"

# --- check 2: dead tracker ------------------------------------------------
c2="$TMP/case2-tracker"
cp -r "$good" "$c2"
printf '\nSee https://github.com/tomlawesome/mikroview/issues/5 for background.\n' >>"$c2/README.md"
run "$c2"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a dead GitHub Issues link fails (rc=$rc)"
check "$(case "$out" in *"FAIL: README.md:"*"dead GitHub Issues link"*) echo true;; *) echo false;; esac)" \
  "and names the file and line"

# --- check 3: relative links resolve --------------------------------------
c3="$TMP/case3-link"
cp -r "$good" "$c3"
printf '\nSee [missing](docs/missing.md) for more.\n' >>"$c3/README.md"
run "$c3"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a broken relative link fails (rc=$rc)"
check "$(case "$out" in *"FAIL: README.md:"*"docs/missing.md"*) echo true;; *) echo false;; esac)" \
  "and names the missing target"

# --- check 4: docs listed --------------------------------------------------
c4="$TMP/case4-docs"
cp -r "$good" "$c4"
cat >"$c4/docs/orphan.md" <<'EOF'
# Orphan

Not linked from anywhere.
EOF
run "$c4"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "an unreferenced docs page fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/orphan.md -- not referenced"*) echo true;; *) echo false;; esac)" \
  "and names the orphaned doc"

# --- check 5: Go version ----------------------------------------------------
c5="$TMP/case5-go"
cp -r "$good" "$c5"
cat >"$c5/docs/development.md" <<'EOF'
# Contributing

Requires Go 1.20+.
EOF
run "$c5"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a stale Go version claim fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/development.md says Go 1.20+ but go.mod requires 1.21"*) echo true;; *) echo false;; esac)" \
  "and names both versions"

# --- check 6: screenshots checked against their own mapped source files ---
#
# Pre-existing, unrelated to #1273: the shared new_repo() fixture's
# synthetic install.sh/deploy/docker-compose.yml already fail check 8's
# hardening-parity pairing (missing the ":ro" suffix on one volume line)
# at HEAD, before any of this file's changes -- confirmed by running
# this same test file, unmodified, checked out on its own. That makes
# the script's overall rc always 1 for every fixture derived from
# new_repo(), regardless of section 6. So the "passes" cases below
# assert the section's own ok:/FAIL: lines directly rather than the
# whole-script rc, which section 8's bug would otherwise poison. Report
# check 8's bug separately; fixing it is out of scope here.
shots="$TMP/screens"
new_repo "$shots"

# the all-good fixture's own screenshot passes against its mapped sources
run "$shots"
check "$(case "$out" in *"ok: screenshot fresh: docs/screenshots/fall-dark.png"*) echo true;; *) echo false;; esac)" \
  "a screenshot fresh against its mapped sources passes"
check "$(case "$out" in *"FAIL:"*"fall-dark.png"*) echo false;; *) echo true;; esac)" \
  "and no FAIL line names it"

# one of the two mapped sources changes after the screenshot -> FAIL,
# naming that source specifically (not just "something in frontend/src")
printf 'export const Fall = 2;\n' >"$shots/frontend/src/components/Fall.svelte"
commit "$shots" "2026-01-02T00:00:00" "change Fall.svelte"
run "$shots"
check "$(case "$out" in *"FAIL: docs/screenshots/fall-dark.png -- frontend/src/components/Fall.svelte changed at"*) echo true;; *) echo false;; esac)" \
  "a screenshot whose mapped source moved after capture fails, naming the specific source file"

# an unrelated component changing does NOT flag this screenshot (the old
# blanket frontend/src diff would have) -- based on $good, not $shots,
# so it does not inherit the staleness just introduced above
c6b="$TMP/case6b-unrelated"
cp -r "$good" "$c6b"
mkdir -p "$c6b/frontend/src/components"
printf 'export const Other = 1;\n' >"$c6b/frontend/src/components/Other.svelte"
commit "$c6b" "2026-01-02T12:00:00" "add unrelated component"
run "$c6b"
check "$(case "$out" in *"FAIL:"*"fall-dark.png"*) echo false;; *) echo true;; esac)" \
  "a change to an unmapped component does not flag fall-dark.png"

# recapturing the screenshot after its source moved clears it -> pass
printf 'png-v2\n' >"$shots/docs/screenshots/fall-dark.png"
commit "$shots" "2026-01-03T00:00:00" "recapture screenshot"
run "$shots"
check "$(case "$out" in *"ok: screenshot fresh: docs/screenshots/fall-dark.png"*) echo true;; *) echo false;; esac)" \
  "a recaptured screenshot passes"
check "$(case "$out" in *"FAIL:"*"fall-dark.png"*) echo false;; *) echo true;; esac)" \
  "and no FAIL line names it"

# a screenshot with no entry in screenshot_sources() fails loudly, naming
# itself, rather than silently passing or guessing
c6c="$TMP/case6c-unmapped"
cp -r "$shots" "$c6c"
printf '\n![other](docs/screenshots/unmapped.png)\n' >>"$c6c/README.md"
printf 'png\n' >"$c6c/docs/screenshots/unmapped.png"
commit "$c6c" "2026-01-04T00:00:00" "add an unmapped screenshot"
run "$c6c"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a screenshot with no screenshot_sources() entry fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/screenshots/unmapped.png -- no entry in check-release-surfaces.sh's screenshot_sources()"*) echo true;; *) echo false;; esac)" \
  "and names it as unmapped rather than guessing"

# a screenshot referenced but never committed fails as untracked
c6d="$TMP/case6d-untracked"
cp -r "$shots" "$c6d"
printf '\n![other](docs/screenshots/stream-dark.png)\n' >>"$c6d/README.md"
# deliberately not committed -- README references it, nothing ever added it
run "$c6d"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a screenshot referenced but never committed fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/screenshots/stream-dark.png -- untracked (never committed)"*) echo true;; *) echo false;; esac)" \
  "and names it as untracked"

# --- check 7: review records -----------------------------------------------

# no record for VERSION -> FAIL, names the missing version
c7a="$TMP/case7a-missing-review"
cp -r "$good" "$c7a"
rm "$c7a/docs/reviews/2026-01-01-v0.1.0.md"
run "$c7a"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a missing review record for VERSION fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/reviews has no record for v0.1.0 (docs/quality-strategy.md: every release gets one)"*) echo true;; *) echo false;; esac)" \
  "and names the missing version"

# an older record still carrying the pending-disclosure marker -> FAIL, names the file
c7b="$TMP/case7b-pending-older"
cp -r "$good" "$c7b"
printf '0.2.0\n' >"$c7b/VERSION"
printf '\n<!-- pending-disclosure: #1 #2 -->\n' >>"$c7b/docs/reviews/2026-01-01-v0.1.0.md"
run "$c7b"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "an older review record with a pending-disclosure marker fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/reviews/2026-01-01-v0.1.0.md -- still carries a pending-disclosure marker"*) echo true;; *) echo false;; esac)" \
  "and names the file"

# the current version's own record may still carry the marker -> passes
c7c="$TMP/case7c-pending-current"
cp -r "$good" "$c7c"
printf '\n<!-- pending-disclosure: #3 -->\n' >>"$c7c/docs/reviews/2026-01-01-v0.1.0.md"
run "$c7c"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "the current version's own pending-disclosure marker does not fail (rc=$rc)"

# --- check 8: install/compose hardening parity ------------------------------

# removing a flag from install.sh's docker run fails, names the drift
c8a="$TMP/case8a-install-drift"
cp -r "$good" "$c8a"
sed 's/--read-only //' "$c8a/install.sh" >"$c8a/install.sh.new" && mv "$c8a/install.sh.new" "$c8a/install.sh"
run "$c8a"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "install.sh missing --read-only fails hardening parity (rc=$rc)"
check "$(case "$out" in *"FAIL: hardening drift"*"--read-only"*) echo true;; *) echo false;; esac)" \
  "and names the drifted flag"

# removing the matching line from deploy/docker-compose.yml fails too
c8b="$TMP/case8b-compose-drift"
cp -r "$good" "$c8b"
grep -v 'pids_limit: 128' "$c8b/deploy/docker-compose.yml" >"$c8b/deploy/docker-compose.yml.new" && mv "$c8b/deploy/docker-compose.yml.new" "$c8b/deploy/docker-compose.yml"
run "$c8b"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "deploy/docker-compose.yml missing pids_limit fails hardening parity (rc=$rc)"
check "$(case "$out" in *"FAIL: hardening drift"*"pids_limit: 128"*) echo true;; *) echo false;; esac)" \
  "and names the drifted line"

# neither file present (a repo mid-migration, or another project's fixture): skip, not a failure
c8c="$TMP/case8c-no-install-files"
cp -r "$good" "$c8c"
rm -f "$c8c/install.sh"
rm -rf "$c8c/deploy"
run "$c8c"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "no install.sh/docker-compose.yml present passes (rc=$rc)"
check "$(case "$out" in *"skip: install/compose hardening parity"*) echo true;; *) echo false;; esac)" \
  "and says so"

echo
if [ "$fails" -ne 0 ]; then
  echo "check-release-surfaces.test.sh: $fails check(s) failed"
  exit 1
fi
echo "check-release-surfaces.test.sh: all checks passed"
