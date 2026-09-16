#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# scripts/fetch-upgrade-fixtures.sh -- downloads every recorded upgrade
# fixture (one per testdata/upgrade/<version>/manifest.json) into
# .upgrade-fixtures/, gitignored, so internal/persist/upgrade_test.go
# has something to open. Skips a file already present, so a second run
# (or a rerun of a partially-cached CI job) costs nothing.
#
# In CI it authenticates with the job's own token, never a personal one:
#
#   JOB-TOKEN: $CI_JOB_TOKEN
#   $CI_API_V4_URL/projects/$CI_PROJECT_ID/packages/generic/upgrade-fixtures/<version>/<file>
#
# Locally it uses `glab api` GET against the same package, relying on
# whatever GitLab credentials the caller's shell already has configured
# (see ~/.config/agents/skills/github-credentials) -- this script never
# sets GLAB_CONFIG_DIR/GITLAB_HOST itself.
#
# See docs/upgrades.md, "How this is tested", and
# scripts/record-upgrade-fixture.sh, which is the other half of this.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TESTDATA_DIR="$ROOT/testdata/upgrade"
FIXTURE_DIR="$ROOT/.upgrade-fixtures"
GITLAB_PROJECT_ID="${MIKROVIEW_GITLAB_PROJECT_ID:-53}"

log() { echo "fetch-upgrade-fixtures: $*" >&2; }

if [ ! -d "$TESTDATA_DIR" ]; then
  log "no $TESTDATA_DIR -- nothing to fetch"
  exit 0
fi

mkdir -p "$FIXTURE_DIR"

fetched=0
skipped=0
failed=0

for manifest in "$TESTDATA_DIR"/*/manifest.json; do
  [ -e "$manifest" ] || continue
  version="$(basename "$(dirname "$manifest")")"
  file="upgrade-fixture-${version}.tar.gz"
  dest="$FIXTURE_DIR/$file"

  if [ -s "$dest" ]; then
    log "$file already present, skipping"
    skipped=$((skipped + 1))
    continue
  fi

  log "fetching $file"
  if [ -n "${CI_JOB_TOKEN:-}" ]; then
    : "${CI_API_V4_URL:?fetch-upgrade-fixtures: CI_JOB_TOKEN is set but CI_API_V4_URL is not}"
    : "${CI_PROJECT_ID:?fetch-upgrade-fixtures: CI_JOB_TOKEN is set but CI_PROJECT_ID is not}"
    url="${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/packages/generic/upgrade-fixtures/${version}/${file}"
    if ! curl -fsSL -H "JOB-TOKEN: ${CI_JOB_TOKEN}" -o "$dest.tmp" "$url"; then
      log "failed to fetch $file from $url"
      rm -f "$dest.tmp"
      failed=$((failed + 1))
      continue
    fi
  else
    if ! glab api "projects/${GITLAB_PROJECT_ID}/packages/generic/upgrade-fixtures/${version}/${file}" > "$dest.tmp" 2>/dev/null; then
      log "failed to fetch $file via glab api (is this version recorded and uploaded yet? see scripts/record-upgrade-fixture.sh)"
      rm -f "$dest.tmp"
      failed=$((failed + 1))
      continue
    fi
  fi

  if [ ! -s "$dest.tmp" ]; then
    log "fetched $file but it was empty"
    rm -f "$dest.tmp"
    failed=$((failed + 1))
    continue
  fi
  mv "$dest.tmp" "$dest"
  fetched=$((fetched + 1))
done

log "$fetched fetched, $skipped already present, $failed failed"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
