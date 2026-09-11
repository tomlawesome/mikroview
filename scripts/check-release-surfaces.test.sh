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
  mkdir -p "$dir/docs/screenshots" "$dir/docs/reviews" "$dir/site" "$dir/frontend/src" \
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

See [docs/x.md](docs/x.md) for details.

![screenshot](docs/screenshots/a.png)
EOF
  cat >"$dir/SECURITY.md" <<'EOF'
# Security

Report privately, not via a public tracker.
EOF
  cat >"$dir/CONTRIBUTING.md" <<'EOF'
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
  printf 'png-v1\n' >"$dir/docs/screenshots/a.png"
  cat >"$dir/.github/workflows/pages.yml" <<'EOF'
name: pages
on: push
EOF
  printf 'export const x = 1;\n' >"$dir/frontend/src/app.js"
  cat >"$dir/docs/reviews/2026-01-01-v0.1.0.md" <<'EOF'
# Pre-release review: v0.1.0

No findings.
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
cat >"$c5/CONTRIBUTING.md" <<'EOF'
# Contributing

Requires Go 1.20+.
EOF
run "$c5"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a stale Go version claim fails (rc=$rc)"
check "$(case "$out" in *"FAIL: CONTRIBUTING.md says Go 1.20+ but go.mod requires 1.21"*) echo true;; *) echo false;; esac)" \
  "and names both versions"

# --- check 6: screenshots fresh, all three states -------------------------
shots="$TMP/screens"
new_repo "$shots"

# no tag reachable yet -> skip, not a failure
run "$shots"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "screenshots: no v* tag reachable passes (rc=$rc)"
check "$(case "$out" in *"skip: screenshots (no v* tag reachable)"*) echo true;; *) echo false;; esac)" \
  "and says so"

git -C "$shots" tag v0.1.0

# frontend/src changes since the tag, screenshot does not -> FAIL
printf 'export const x = 2;\n' >"$shots/frontend/src/app.js"
commit "$shots" "2026-01-02T00:00:00" "change frontend"
run "$shots"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a screenshot older than the last tag fails (rc=$rc)"
check "$(case "$out" in *"FAIL: docs/screenshots/a.png -- last captured before v0.1.0"*) echo true;; *) echo false;; esac)" \
  "and names the stale png and the tag it predates"

# recapturing the screenshot clears it -> pass
printf 'png-v2\n' >"$shots/docs/screenshots/a.png"
commit "$shots" "2026-01-03T00:00:00" "recapture screenshot"
run "$shots"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "a recaptured screenshot passes (rc=$rc)"
check "$(case "$out" in *"ok: screenshot fresh: docs/screenshots/a.png"*) echo true;; *) echo false;; esac)" \
  "and confirms it"

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

echo
if [ "$fails" -ne 0 ]; then
  echo "check-release-surfaces.test.sh: $fails check(s) failed"
  exit 1
fi
echo "check-release-surfaces.test.sh: all checks passed"
