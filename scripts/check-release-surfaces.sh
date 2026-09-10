#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# Drift check for the project's public release surfaces (owner decision
# 2026-09-10) -- see docs/decisions/release-versioning.md "Release
# surfaces". Runs on promotion merge requests so a changelog heading that
# was never cut, a dead GitHub Issues link (the tracker is switched off),
# a broken relative link, an undocumented docs/ page, a stale Go version
# claim, or a stale screenshot cannot ride a release out the door.
#
# POSIX sh only: this runs in plain alpine:3.24 with git added and
# nothing else -- no bash, no python. Run from the repo root; this
# script cd's there itself so it works from any invocation directory.
#
# Usage: scripts/check-release-surfaces.sh
set -eu

cd "$(dirname "$0")/.."

fails=0

fail() { # fail "<what> -- <how to fix, one clause>"
  echo "FAIL: $1"
  fails=$((fails + 1))
}

ok() { # ok "<check>"
  echo "ok: $1"
}

tmpd=$(mktemp -d)
trap 'rm -rf "$tmpd"' EXIT

# ---------------------------------------------------------------------
# 1. changelog heading: VERSION must have a dated section in CHANGELOG.md
# ---------------------------------------------------------------------
version=$(head -n1 VERSION | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
if grep -qF "## [$version] - " CHANGELOG.md 2>/dev/null; then
  ok "CHANGELOG.md has a '## [$version] - ' heading"
else
  fail "CHANGELOG.md has no '## [$version] - ' heading -- cut a dated changelog section for $version"
fi

# ---------------------------------------------------------------------
# 2. dead tracker: GitHub Issues is switched off, nothing should link to it
# ---------------------------------------------------------------------
tracker="github.com/tomlawesome/mikroview/issues"
: >"$tmpd/tracker-hits"
for f in README.md SECURITY.md CONTRIBUTING.md site/index.html docs/*.md; do
  [ -f "$f" ] || continue
  grep -n "$tracker" "$f" 2>/dev/null | sed "s#^#$f:#" >>"$tmpd/tracker-hits"
done
if [ -s "$tmpd/tracker-hits" ]; then
  while IFS= read -r hit; do
    fail "$hit -- dead GitHub Issues link (the tracker is off), point at the GitLab issue or drop it"
  done <"$tmpd/tracker-hits"
else
  ok "no dead GitHub Issues links"
fi

# ---------------------------------------------------------------------
# 3. relative links resolve: every non-http/#/mailto: markdown link
#    target in README.md, SECURITY.md, CONTRIBUTING.md must exist
# ---------------------------------------------------------------------
link_fails=0
for f in README.md SECURITY.md CONTRIBUTING.md; do
  [ -f "$f" ] || continue
  grep -noE '\]\([^)]+\)' "$f" 2>/dev/null >"$tmpd/links-raw" || true
  while IFS= read -r rawline; do
    [ -n "$rawline" ] || continue
    lineno=${rawline%%:*}
    match=${rawline#*:}
    target=${match#](}
    target=${target%)}
    case "$target" in
      http*|\#*|mailto:*) continue ;;
    esac
    path=${target%%#*}
    if [ ! -e "$path" ]; then
      fail "$f:$lineno -- relative link target '$target' does not exist, fix the path or remove the link"
      link_fails=$((link_fails + 1))
    fi
  done <"$tmpd/links-raw"
done
if [ "$link_fails" -eq 0 ]; then
  ok "relative links resolve (README.md, SECURITY.md, CONTRIBUTING.md)"
fi

# ---------------------------------------------------------------------
# 4. docs listed: every top-level docs/*.md must be referenced from
#    README.md or CONTRIBUTING.md
# ---------------------------------------------------------------------
for d in docs/*.md; do
  [ -f "$d" ] || continue
  if grep -qF -- "$d" README.md CONTRIBUTING.md 2>/dev/null; then
    ok "docs listed: $d is referenced"
  else
    fail "$d -- not referenced from README.md or CONTRIBUTING.md, link it or remove the doc"
  fi
done

# ---------------------------------------------------------------------
# 5. Go version: CONTRIBUTING.md's "Go X.Y+" must match go.mod's go
#    directive's major.minor
# ---------------------------------------------------------------------
gomod_line=$(grep '^go ' go.mod | head -n1)
gomod_ver=$(echo "$gomod_line" | awk '{print $2}' | cut -d. -f1,2)
ctrib_match=$(grep -oE 'Go [0-9]+\.[0-9]+\+' CONTRIBUTING.md 2>/dev/null | head -n1)
ctrib_ver=$(echo "$ctrib_match" | sed -E 's/^Go ([0-9]+\.[0-9]+)\+$/\1/')
if [ -z "$ctrib_match" ]; then
  fail "CONTRIBUTING.md has no 'Go X.Y+' line -- state the minimum Go version ($gomod_ver+ per go.mod)"
elif [ "$ctrib_ver" = "$gomod_ver" ]; then
  ok "CONTRIBUTING.md's Go version matches go.mod ($gomod_ver)"
else
  fail "CONTRIBUTING.md says Go $ctrib_ver+ but go.mod requires $gomod_ver -- update CONTRIBUTING.md's Go version line"
fi

# ---------------------------------------------------------------------
# 6. screenshots fresh: a screenshot referenced from README.md, docs/*.md
#    or .github/workflows/pages.yml must postdate the last frontend/src
#    change since the previous release tag. Needs full history (git
#    describe and merge-base --is-ancestor both need every commit and
#    tag reachable, not a shallow clone) -- CI sets GIT_DEPTH: 0 for
#    this job for that reason.
# ---------------------------------------------------------------------
prev=$(git describe --tags --match 'v*' --abbrev=0 HEAD 2>/dev/null || true)
if [ -z "$prev" ]; then
  echo "skip: screenshots (no v* tag reachable)"
elif git diff --quiet "$prev" HEAD -- frontend/src 2>/dev/null; then
  ok "screenshots: no frontend/src changes since $prev"
else
  grep -ohE '(docs/)?screenshots/[A-Za-z0-9_.-]+\.png' README.md docs/*.md .github/workflows/pages.yml 2>/dev/null \
    | sed -E 's#^screenshots/#docs/screenshots/#' | sort -u >"$tmpd/screens"
  while IFS= read -r png; do
    [ -n "$png" ] || continue
    last=$(git log -1 --format=%H -- "$png" 2>/dev/null || true)
    if [ -z "$last" ]; then
      fail "$png -- untracked (never committed), capture and commit a real screenshot before release"
    elif git merge-base --is-ancestor "$last" "$prev" 2>/dev/null; then
      fail "$png -- last captured before $prev, recapture from a seeded demo of this version"
    else
      ok "screenshot fresh: $png"
    fi
  done <"$tmpd/screens"
fi

echo
if [ "$fails" -gt 0 ]; then
  echo "check-release-surfaces: $fails check(s) failed"
  exit 1
fi
echo "check-release-surfaces: all checks passed"
