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
for f in README.md SECURITY.md docs/development.md site/index.html docs/*.md; do
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
#    target in README.md, SECURITY.md, docs/development.md must exist
# ---------------------------------------------------------------------
link_fails=0
for f in README.md SECURITY.md docs/development.md; do
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
    # Markdown resolves a relative link against the file's own directory,
    # so docs/development.md's "../SECURITY.md" is checked from docs/.
    case "$path" in /*) ;; *) path="$(dirname "$f")/$path" ;; esac
    if [ ! -e "$path" ]; then
      fail "$f:$lineno -- relative link target '$target' does not exist, fix the path or remove the link"
      link_fails=$((link_fails + 1))
    fi
  done <"$tmpd/links-raw"
done
if [ "$link_fails" -eq 0 ]; then
  ok "relative links resolve (README.md, SECURITY.md, docs/development.md)"
fi

# ---------------------------------------------------------------------
# 4. docs listed: every top-level docs/*.md must be referenced from
#    README.md or docs/development.md
# ---------------------------------------------------------------------
for d in docs/*.md; do
  [ -f "$d" ] || continue
  if grep -qF -- "$d" README.md docs/development.md 2>/dev/null; then
    ok "docs listed: $d is referenced"
  else
    fail "$d -- not referenced from README.md or docs/development.md, link it or remove the doc"
  fi
done

# ---------------------------------------------------------------------
# 5. Go version: docs/development.md's "Go X.Y+" must match go.mod's go
#    directive's major.minor
# ---------------------------------------------------------------------
gomod_line=$(grep '^go ' go.mod | head -n1)
gomod_ver=$(echo "$gomod_line" | awk '{print $2}' | cut -d. -f1,2)
ctrib_match=$(grep -oE 'Go [0-9]+\.[0-9]+\+' docs/development.md 2>/dev/null | head -n1)
ctrib_ver=$(echo "$ctrib_match" | sed -E 's/^Go ([0-9]+\.[0-9]+)\+$/\1/')
if [ -z "$ctrib_match" ]; then
  fail "docs/development.md has no 'Go X.Y+' line -- state the minimum Go version ($gomod_ver+ per go.mod)"
elif [ "$ctrib_ver" = "$gomod_ver" ]; then
  ok "docs/development.md's Go version matches go.mod ($gomod_ver)"
else
  fail "docs/development.md says Go $ctrib_ver+ but go.mod requires $gomod_ver -- update docs/development.md's Go version line"
fi

# ---------------------------------------------------------------------
# 6. screenshots fresh: a screenshot referenced from README.md, docs/*.md
#    or .github/workflows/pages.yml must not predate the source files it
#    actually depicts (#1273 -- comparing against the previous release
#    tag plus a blanket frontend/src diff flagged every screenshot on
#    any unrelated frontend change, and separately missed a screenshot
#    that had been recaptured once but whose own surface kept moving
#    afterward -- neither direction is what "did this image go stale"
#    actually asks). Needs full history (merge-base --is-ancestor needs
#    every commit reachable, not a shallow clone) -- CI sets
#    GIT_DEPTH: 0 for this job for that reason. Tags do not enter into
#    it at all any more: a screenshot can go stale mid-cycle too.
#
#    screenshot_sources() is the map from image to the file(s) it shows,
#    confirmed against the actual fix commits for each image's last
#    known drift (#1257/#1268). Deliberately narrow: a discovered
#    screenshot with no entry there fails loudly naming itself, rather
#    than falling back to a guess this check cannot stand behind. Add
#    an entry (and keep it current) whenever a screenshot is added or
#    the component it depicts changes shape.
# ---------------------------------------------------------------------
screenshot_sources() {
  case "$1" in
    docs/screenshots/fall-dark.png)
      echo "frontend/src/components/Fall.svelte frontend/src/lib/fall.svelte.ts" ;;
    docs/screenshots/stream-dark.png)
      echo "frontend/src/components/EventRow.svelte frontend/src/components/LiveTable.svelte frontend/src/components/FilterBar.svelte frontend/src/lib/columns.svelte.ts frontend/src/lib/tokenBar.ts" ;;
    docs/screenshots/topography-map-dark.png)
      echo "frontend/src/components/Topography.svelte" ;;
    docs/screenshots/engine-room-people-door.png)
      echo "frontend/src/components/EngineRoom.svelte frontend/src/components/ResetCodeOverlay.svelte" ;;
    docs/screenshots/setup-wizard-ledger.png)
      echo "frontend/src/components/SetupWizard.svelte frontend/src/lib/setupsteps.ts frontend/src/lib/wizard.svelte.ts" ;;
    docs/screenshots/entities-add-a-router.png)
      echo "frontend/src/components/Entities.svelte" ;;
    docs/screenshots/entities-refused-sender.png)
      echo "frontend/src/components/Entities.svelte" ;;
    docs/screenshots/setup-wizard-send-logs.png)
      echo "frontend/src/components/SetupWizard.svelte frontend/src/lib/setupsteps.ts frontend/src/lib/wizard.svelte.ts" ;;
    *)
      return 1 ;;
  esac
}

grep -ohE '(docs/)?screenshots/[A-Za-z0-9_.-]+\.png' README.md docs/*.md .github/workflows/pages.yml 2>/dev/null \
  | sed -E 's#^screenshots/#docs/screenshots/#' | sort -u >"$tmpd/screens"
while IFS= read -r png; do
  [ -n "$png" ] || continue
  last=$(git log -1 --format=%H -- "$png" 2>/dev/null || true)
  if [ -z "$last" ]; then
    fail "$png -- untracked (never committed), capture and commit a real screenshot before release"
    continue
  fi
  sources=$(screenshot_sources "$png") || {
    fail "$png -- no entry in check-release-surfaces.sh's screenshot_sources(), add one naming the file(s) it depicts so freshness can be checked"
    continue
  }
  last_short=$(git rev-parse --short "$last")
  stale=0
  for src in $sources; do
    src_last=$(git log -1 --format=%H -- "$src" 2>/dev/null || true)
    if [ -z "$src_last" ]; then
      fail "$png -- its mapped source $src has no commits (typo in screenshot_sources()?)"
      stale=1
    elif ! git merge-base --is-ancestor "$src_last" "$last" 2>/dev/null; then
      fail "$png -- $src changed at $(git rev-parse --short "$src_last"), after the screenshot's own last capture $last_short -- recapture from a seeded demo of this version"
      stale=1
    fi
  done
  [ "$stale" = 1 ] || ok "screenshot fresh: $png"
done <"$tmpd/screens"

# ---------------------------------------------------------------------
# 7. review records: every release gets one (docs/quality-strategy.md
#    "Pre-release reviews"), and an older record cannot still be sitting
#    on a deferred security disclosure once a newer version is cut.
#    Convention: a review record defers disclosure with the literal
#    line `<!-- pending-disclosure: #n #m -->`; it must be replaced by
#    the actual findings before the NEXT release is cut. A record for
#    the version being checked, or a newer one, may still carry it.
# ---------------------------------------------------------------------

# version_lt a b -- true (exit 0) if version a is older than version b.
# Compares the three dot-separated numeric parts field by field; no
# sort -V dependency (not confirmed present in busybox sort).
version_lt() {
  a1=$(echo "$1" | cut -d. -f1); a2=$(echo "$1" | cut -d. -f2); a3=$(echo "$1" | cut -d. -f3)
  b1=$(echo "$2" | cut -d. -f1); b2=$(echo "$2" | cut -d. -f2); b3=$(echo "$2" | cut -d. -f3)
  [ "$a1" -lt "$b1" ] && return 0
  [ "$a1" -gt "$b1" ] && return 1
  [ "$a2" -lt "$b2" ] && return 0
  [ "$a2" -gt "$b2" ] && return 1
  [ "$a3" -lt "$b3" ] && return 0
  return 1
}

found_review=0
for f in docs/reviews/*-v"$version".md; do
  [ -f "$f" ] || continue
  found_review=1
done
if [ "$found_review" -eq 1 ]; then
  ok "docs/reviews has a record for v$version"
else
  fail "docs/reviews has no record for v$version (docs/quality-strategy.md: every release gets one)"
fi

pending_fails=0
for f in docs/reviews/*-v*.md; do
  [ -f "$f" ] || continue
  fver=$(echo "$f" | sed -nE 's#^docs/reviews/.*-v([0-9]+\.[0-9]+\.[0-9]+)\.md$#\1#p')
  [ -n "$fver" ] || continue
  if version_lt "$fver" "$version" && grep -qF 'pending-disclosure' "$f" 2>/dev/null; then
    fail "$f -- still carries a pending-disclosure marker from before v$version, replace it with the findings before this release"
    pending_fails=$((pending_fails + 1))
  fi
done
if [ "$pending_fails" -eq 0 ]; then
  ok "no older docs/reviews record still has a pending-disclosure marker"
fi

# ---------------------------------------------------------------------
# 8. install/compose hardening parity: install.sh's one `docker run` and
#    deploy/docker-compose.yml's hardening block must carry the same set
#    of flags (owner ruling 2026-09-19, #1286) -- this is the one place
#    that set is written down, so removing a flag from either file without
#    the other is what this check exists to catch. Each pair below is
#    "<install.sh docker-run flag>|<deploy/docker-compose.yml line>";
#    memory/CPU limits are deliberately not in this set -- see install.sh's
#    comment above its `docker run` for why.
# ---------------------------------------------------------------------
if [ -f install.sh ] && [ -f deploy/docker-compose.yml ]; then
  cat >"$tmpd/hardening-pairs" <<'EOF'
--read-only|read_only: true
--cap-drop ALL|- ALL
--security-opt no-new-privileges|- no-new-privileges:true
--pids-limit 128|pids_limit: 128
:/etc/mikroview:ro|/etc/mikroview:ro
EOF
  # The install.sh side is matched against its `docker run` argument
  # list alone, not the whole file: a flag named only in a comment is
  # not a flag the installer applies, and matching the file would let
  # one pass this check while the container ran without it.
  sed -n '/^set -- run /,/^  "\$image"/p' install.sh >"$tmpd/install-run-line"
  while IFS='|' read -r install_flag compose_line; do
    [ -n "$install_flag" ] || continue
    in_install=0
    in_compose=0
    grep -qF -- "$install_flag" "$tmpd/install-run-line" && in_install=1
    grep -qF -- "$compose_line" deploy/docker-compose.yml && in_compose=1
    if [ "$in_install" = "1" ] && [ "$in_compose" = "1" ]; then
      ok "hardening parity: '$install_flag' is in install.sh and '$compose_line' is in deploy/docker-compose.yml"
    else
      fail "hardening drift -- '$install_flag' ($([ "$in_install" = "1" ] && echo present || echo missing) in install.sh's docker run) vs '$compose_line' ($([ "$in_compose" = "1" ] && echo present || echo missing) in deploy/docker-compose.yml) -- keep install.sh's docker run and deploy/docker-compose.yml's hardening block in sync"
    fi
  done <"$tmpd/hardening-pairs"
else
  echo "skip: install/compose hardening parity (install.sh or deploy/docker-compose.yml not present)"
fi

# ---------------------------------------------------------------------
# 9. shot markers realized: a `<!-- shot: ... -->` marker in README.md,
#    docs/*.md or .github/workflows/pages.yml marks where a screenshot
#    belongs. Capturing the image replaces the marker with an
#    ![alt](screenshots/x.png) line in its place (see 37ed20af) -- so a
#    marker still standing means no image was ever captured for it,
#    whatever filename it will eventually get. Check 6 above only ever
#    saw markers that had already become images; two markers that never
#    did sat unnoticed in docs/routeros-setup.md until a human audit
#    found them (#1297).
# ---------------------------------------------------------------------
shot_fails=0
for f in README.md docs/*.md .github/workflows/pages.yml; do
  [ -f "$f" ] || continue
  grep -noE '<!-- shot:.*-->' "$f" 2>/dev/null >"$tmpd/shot-hits" || true
  while IFS= read -r hit; do
    [ -n "$hit" ] || continue
    lineno=${hit%%:*}
    marker=${hit#*:}
    desc=$(echo "$marker" | sed -E 's/^<!-- shot: *//; s/ *-->$//')
    fail "$f:$lineno -- shot marker '$desc' has no screenshot yet, capture the image and replace the marker with it"
    shot_fails=$((shot_fails + 1))
  done <"$tmpd/shot-hits"
done
if [ "$shot_fails" -eq 0 ]; then
  ok "no unrealized shot markers"
fi

echo
if [ "$fails" -gt 0 ]; then
  echo "check-release-surfaces: $fails check(s) failed"
  exit 1
fi
echo "check-release-surfaces: all checks passed"
