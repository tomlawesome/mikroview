#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# scripts/fetch-upgrade-fixtures.sh -- downloads every recorded upgrade
# fixture into .upgrade-fixtures/, gitignored, so
# internal/persist/upgrade_test.go and
# internal/persist/upgrade_postgres_test.go have something to open. Skips
# a file already present, so a second run (or a rerun of a partially-
# cached CI job) costs nothing.
#
# Three files per version, of which only the first must exist:
#
#   upgrade-fixture-<version>.tar.gz          the file backend's data dir
#   upgrade-fixture-<version>-postgres.sql.gz a pg_dump, only for the
#                                             releases that first carried
#                                             a schema version (v0.1.0,
#                                             v0.2.0, v0.3.0 today) -- a
#                                             404 for any other version is
#                                             expected and logged
#   manifest.json                             fetched only where the repo
#                                             has no committed
#                                             testdata/upgrade/<version>/
#                                             manifest.json, which is the
#                                             case for a release the tag
#                                             job recorded and nobody has
#                                             committed yet (#1247's
#                                             decision: a job token can
#                                             upload a package but cannot
#                                             open a merge request)
#
# Which versions: every one with a committed manifest, plus every version
# the registry itself holds. The second half is what makes a tag-job
# recording testable on dev's next run with no hand step.
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
# In CI the job token is scoped to the project the job belongs to, so the
# id has to be that one rather than the default above -- they are the
# same project, but a fork or a moved project would not be.
PROJECT_ID="${GITLAB_PROJECT_ID}"
[ -z "${CI_JOB_TOKEN:-}" ] || PROJECT_ID="${CI_PROJECT_ID:?fetch-upgrade-fixtures: CI_JOB_TOKEN is set but CI_PROJECT_ID is not}"

log() { echo "fetch-upgrade-fixtures: $*" >&2; }

mkdir -p "$FIXTURE_DIR"

fetched=0
skipped=0
missing=0
failed=0

# api_get <api-path> <destination> -- GET one thing from this project's
# API with whichever credential the environment offers. Returns non-zero
# for anything that is not a 200, without distinguishing why: every
# caller here decides for itself whether a miss is fatal.
api_get() {
  local path="$1" dest="$2"
  if [ -n "${CI_JOB_TOKEN:-}" ]; then
    : "${CI_API_V4_URL:?fetch-upgrade-fixtures: CI_JOB_TOKEN is set but CI_API_V4_URL is not}"
    curl -fsSL -H "JOB-TOKEN: ${CI_JOB_TOKEN}" -o "$dest" "${CI_API_V4_URL}/${path}" 2>/dev/null
  else
    glab api "$path" > "$dest" 2>/dev/null
  fi
}

# package_file <version> <file-name> <destination> <required>
package_file() {
  local version="$1" name="$2" dest="$3" required="$4"

  if [ -s "$dest" ]; then
    log "$version/$name already present, skipping"
    skipped=$((skipped + 1))
    return 0
  fi

  mkdir -p "$(dirname "$dest")"
  if ! api_get "projects/${PROJECT_ID}/packages/generic/upgrade-fixtures/${version}/${name}" "$dest.tmp"; then
    rm -f "$dest.tmp"
    if [ "$required" = "required" ]; then
      log "failed to fetch $version/$name (is this version recorded and uploaded yet? see scripts/record-upgrade-fixture.sh)"
      failed=$((failed + 1))
    else
      log "$version/$name is not in the registry -- expected for a version that has none"
      missing=$((missing + 1))
    fi
    return 0
  fi
  if [ ! -s "$dest.tmp" ]; then
    log "fetched $version/$name but it was empty"
    rm -f "$dest.tmp"
    failed=$((failed + 1))
    return 0
  fi
  mv "$dest.tmp" "$dest"
  log "fetched $version/$name"
  fetched=$((fetched + 1))
}

# The versions to fetch: committed manifests, plus whatever the registry
# holds. `|| true` on the listing, not a failure: a network hiccup there
# must not stop the committed set -- those are what the gate tests today.
versions_file="$(mktemp)"
trap 'rm -f "$versions_file"' EXIT

if [ -d "$TESTDATA_DIR" ]; then
  for manifest in "$TESTDATA_DIR"/*/manifest.json; do
    [ -e "$manifest" ] || continue
    basename "$(dirname "$manifest")" >> "$versions_file"
  done
fi

registry_list="$(mktemp)"
if api_get "projects/${PROJECT_ID}/packages?package_type=generic&package_name=upgrade-fixtures&per_page=100" "$registry_list"; then
  # grep rather than a JSON parser on purpose: this script runs in
  # whichever image the job that needs the fixtures already uses
  # (golang, alpine), and the only field wanted is a version string in a
  # response the query has already filtered to one package name. A
  # listing that parses to nothing is reported rather than shrugged off,
  # since silently falling back to the committed set is exactly how a
  # tag-job recording would go untested.
  found="$(grep -oE '"version":[[:space:]]*"[^"]*"' "$registry_list" | sed -E 's/.*"([^"]*)"$/\1/' | sort -u)"
  if [ -z "$found" ]; then
    log "the registry lists no upgrade-fixtures versions -- committed versions only"
  else
    echo "$found" >> "$versions_file"
  fi
else
  log "could not list registry packages -- committed versions only"
fi
rm -f "$registry_list"

if [ ! -s "$versions_file" ]; then
  log "no recorded versions, in the repository or the registry -- nothing to fetch"
  exit 0
fi

while read -r version; do
  [ -z "$version" ] && continue
  package_file "$version" "upgrade-fixture-${version}.tar.gz" \
    "$FIXTURE_DIR/upgrade-fixture-${version}.tar.gz" required
  package_file "$version" "upgrade-fixture-${version}-postgres.sql.gz" \
    "$FIXTURE_DIR/upgrade-fixture-${version}-postgres.sql.gz" optional
  if [ -s "$TESTDATA_DIR/$version/manifest.json" ]; then
    log "$version/manifest.json is committed -- not fetching the registry copy"
  else
    package_file "$version" "manifest.json" "$FIXTURE_DIR/$version/manifest.json" optional
  fi
done < <(sort -u "$versions_file")

log "$fetched fetched, $skipped already present, $missing absent from the registry, $failed failed"
if [ "$failed" -gt 0 ]; then
  exit 1
fi
