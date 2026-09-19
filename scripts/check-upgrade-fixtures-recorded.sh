#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# scripts/check-upgrade-fixtures-recorded.sh -- #1274: the tag
# pipeline's record:upgrade-fixture job is deliberately allow_failure
# (a slow build or a GHCR blip must not redden a tag that is already
# cut -- see that job's own header in .gitlab-ci.yml), which means it
# can fail and nobody notices. The interim answer was a manual
# checklist line on the "Promote to main" issue template telling a
# human to go and check; owner ruling 2026-09-18: "Ideally this is all
# automated." This is that automation, replacing the checklist line.
#
# Asserts every released v[0-9]* tag has a fixture recorded somewhere --
# either committed to testdata/upgrade/<version>/manifest.json, or
# uploaded to this project's generic package registry
# (scripts/record-upgrade-fixture.sh's upload target, since #1247: a job
# token can upload a package but cannot open a merge request, so a
# fresh release's manifest is normally uncommitted for a while) -- and
# fails naming the exact command to record whichever tag is missing.
#
# Runs on the ordinary dev pipeline (.gitlab-ci.yml), not the tag
# pipeline: the tag just cut stays green regardless of what this finds;
# the next push to dev is what goes red if a recording never happened.
#
# Needs full tag history, not a shallow clone -- CI sets GIT_DEPTH: 0
# for this job, same reason as check-release-surfaces.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GITLAB_PROJECT_ID="${MIKROVIEW_GITLAB_PROJECT_ID:-53}"
PROJECT_ID="${GITLAB_PROJECT_ID}"
[ -z "${CI_JOB_TOKEN:-}" ] || PROJECT_ID="${CI_PROJECT_ID:?check-upgrade-fixtures-recorded: CI_JOB_TOKEN is set but CI_PROJECT_ID is not}"

log() { echo "check-upgrade-fixtures-recorded: $*" >&2; }

# api_get <api-path> <destination> -- same shape as
# fetch-upgrade-fixtures.sh's: CI_JOB_TOKEN in CI, `glab api` locally
# against whatever GitLab credential the caller's shell already has
# (see ~/.config/agents/skills/github-credentials -- this script never
# sets GLAB_CONFIG_DIR/GITLAB_HOST itself). Non-zero for anything that
# is not a 200.
api_get() {
  local path="$1" dest="$2"
  if [ -n "${CI_JOB_TOKEN:-}" ]; then
    : "${CI_API_V4_URL:?check-upgrade-fixtures-recorded: CI_JOB_TOKEN is set but CI_API_V4_URL is not}"
    curl -fsSL --connect-timeout 10 --max-time 120 --retry 3 --retry-connrefused --retry-delay 2 \
      -H "JOB-TOKEN: ${CI_JOB_TOKEN}" -o "$dest" "${CI_API_V4_URL}/${path}" 2>/dev/null
  else
    glab api "$path" >"$dest" 2>/dev/null
  fi
}

# Every released tag: v[0-9]* by the same pattern the tag pipeline's
# record:upgrade-fixture job rules on ($CI_COMMIT_TAG =~ /^v[0-9]/).
released="$(git tag --list 'v[0-9]*' | sort -u)"
if [ -z "$released" ]; then
  log "no v[0-9]* tags reachable -- nothing to check"
  exit 0
fi

# Committed manifests: proof a version's fixture was recorded, uploaded,
# and its manifest later committed to the repo.
committed="$(mktemp)"
if [ -d testdata/upgrade ]; then
  for manifest in testdata/upgrade/*/manifest.json; do
    [ -e "$manifest" ] || continue
    basename "$(dirname "$manifest")" >>"$committed"
  done
fi

# The registry itself: proof a version's fixture was recorded and
# uploaded, whether or not its manifest has been committed yet.
registry_list="$(mktemp)"
registry_ok=1
if api_get "projects/${PROJECT_ID}/packages?package_type=generic&package_name=upgrade-fixtures&per_page=100" "$registry_list"; then
  # `|| true`: an empty/no-match listing is a real, valid case (a
  # project with nothing recorded yet) -- without it, grep's exit 1 on
  # zero matches trips `set -o pipefail` and kills the script right
  # here, silently, before it ever gets to compare anything. Caught by
  # this script's own test suite (case "a tag with no fixture anywhere
  # fails") the first time it ran.
  grep -oE '"version":[[:space:]]*"[^"]*"' "$registry_list" | sed -E 's/.*"([^"]*)"$/\1/' | sort -u >>"$committed" || true
else
  registry_ok=0
fi
rm -f "$registry_list"

recorded="$(sort -u "$committed")"
rm -f "$committed"

missing=""
while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  if ! grep -qxF "$tag" <<<"$recorded"; then
    missing="$missing $tag"
  fi
done <<<"$released"

if [ -n "$missing" ]; then
  echo "check-upgrade-fixtures-recorded: the following released tags have no fixture on record:"
  for tag in $missing; do
    echo "  $tag -- run: scripts/record-upgrade-fixture.sh $tag"
  done
  if [ "$registry_ok" = 0 ]; then
    echo "check-upgrade-fixtures-recorded: the registry listing failed this run (network or credential issue), so this list may include a version that is actually recorded but not committed yet -- rerun once the registry is reachable before trusting it fully" >&2
  fi
  exit 1
fi

log "every released tag ($(echo "$released" | wc -l | tr -d ' ')) has a recorded fixture"
