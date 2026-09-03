#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Companion to scripts/routeros-chr-exercise.sh (#894): called only
# after every starting command has parsed cleanly on a booted CHR.
# Appends the verified row (scripts/routerosappendrow), commits it with
# the exercise's console log, and opens a merge request on the GitLab
# instance itself -- gitlab.tomlawson.io, project ai/mikroview -- since
# that is where this job runs and where dev's review already happens
# (AGENTS.md, "Where code review happens"). It never touches GitHub.
#
# Never auto-merged: this only opens the merge request. A human still
# reads the release notes and bumps ReviewedVersion
# (internal/routeros/versions.go) before merging -- see the row-append
# tool's own doc comment for why leaving that constant alone is
# deliberate, not an omission.
#
# Usage: scripts/routeros-chr-open-mr.sh <routeros-version> <log-file>
#
# Needs, from the CI job's environment:
#   GITLAB_MR_TOKEN   a GitLab CI/CD variable (masked, protected) the
#                     owner creates: a project access token scoped to
#                     this project only, with `write_repository` and
#                     `api` (merge-request creation needs `api`) -- never
#                     written to disk, only read from the environment.
#   CI_SERVER_HOST, CI_PROJECT_PATH, CI_API_V4_URL, CI_PROJECT_ID
#                     GitLab's own predefined job variables -- nothing
#                     to configure, every job gets them.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO"

version="${1:?usage: routeros-chr-open-mr.sh <routeros-version> <log-file>}"
log_file="${2:?usage: routeros-chr-open-mr.sh <routeros-version> <log-file>}"

log() { printf '%s\n' "$*" >&2; }

if [ -z "${GITLAB_MR_TOKEN:-}" ]; then
  log "routeros-chr-open-mr: GITLAB_MR_TOKEN is not set -- cannot open a merge request. See this script's header for what to create."
  exit 2
fi
: "${CI_SERVER_HOST:?routeros-chr-open-mr: CI_SERVER_HOST is not set -- this script must run inside a GitLab CI job}"
: "${CI_PROJECT_PATH:?routeros-chr-open-mr: CI_PROJECT_PATH is not set -- this script must run inside a GitLab CI job}"
: "${CI_API_V4_URL:?routeros-chr-open-mr: CI_API_V4_URL is not set -- this script must run inside a GitLab CI job}"
: "${CI_PROJECT_ID:?routeros-chr-open-mr: CI_PROJECT_ID is not set -- this script must run inside a GitLab CI job}"

branch="chore/routeros-${version}-row"
date="$(date -u +%Y-%m-%d)"
log_dest="docs/routeros-verification-logs/${version}.log"

# append_out carries the exact row this printed (dialect included), so
# the commit message and MR description quote what was actually
# recorded rather than re-deriving it.
append_out="$(go run ./scripts/routerosappendrow -version "$version" -date "$date")"
log "routeros-chr-open-mr: $append_out"

mkdir -p "$(dirname "$log_dest")"
cp "$log_file" "$log_dest"

git config user.name "mikroview CHR exercise"
git config user.email "chr-exercise@invalid"

git checkout -b "$branch"
git add internal/routeros/dialects.go "$log_dest"
git commit -m "$(cat <<EOF
Record RouterOS $version as exercised on CHR (#894)

$append_out

This is mechanical: the wizard's starting commands (CA trust, syslog,
rule tagging), rendered by scripts/routeroscommands from
internal/routeros, parsed cleanly against RouterOS $version booted as a
CHR under software-emulated QEMU. Full console transcript at
$log_dest.

This does not claim the release notes were read. ReviewedVersion
(internal/routeros/versions.go) is unchanged, so
TestReviewedVersionMatchesNewest is expected to fail until a human
reads $version's release notes against
internal/routeros/commands.go and docs/routeros-setup.md, fixes
anything that moved, and bumps ReviewedVersion in this branch --
scripts/routeros-freshness.sh's own review step, done here by a human
rather than skipped.
EOF
)"

remote_url="https://oauth2:${GITLAB_MR_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git"
# GitLab redacts any job-log occurrence of a variable's value once it is
# marked "masked" (required above) -- the same protection
# sync:mirror-to-github already relies on in .gitlab-ci.yml for its own
# token, rather than this script adding a second, independent mechanism.
if ! git push "$remote_url" "HEAD:refs/heads/$branch"; then
  log "routeros-chr-open-mr: pushing $branch failed"
  exit 1
fi

# Idempotent against a retried job: if a merge request from this branch
# is already open, do not open a second one.
existing="$(curl -fsS -H "PRIVATE-TOKEN: $GITLAB_MR_TOKEN" \
  "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/merge_requests?state=opened&source_branch=${branch}")"
if [ "$existing" != "[]" ]; then
  log "routeros-chr-open-mr: a merge request from $branch is already open -- not opening another"
  exit 0
fi

title="RouterOS $version: exercised on CHR, needs a release-notes read"
description="$(cat <<EOF
Mechanical, from the weekly CHR exercise (#894): RouterOS $version's
starting commands (CA trust, syslog, rule tagging) parsed cleanly
against a booted CHR $version under software-emulated QEMU (no
/dev/kvm). Full console transcript: \`$log_dest\`.

**This does not mean the release notes have been read.** \`ReviewedVersion\`
(\`internal/routeros/versions.go\`) has not moved, so
\`TestReviewedVersionMatchesNewest\` is expected to fail on this branch.
Before merging: read $version's release notes against
\`internal/routeros/commands.go\` and \`docs/routeros-setup.md\`, fix
anything that moved, and bump \`ReviewedVersion\` to $version in this
same branch.

Never auto-merge this.
EOF
)"

payload="$(python3 - "$branch" "$title" "$description" <<'PY'
import json, sys
branch, title, description = sys.argv[1:4]
print(json.dumps({
    "source_branch": branch,
    "target_branch": "dev",
    "title": title,
    "description": description,
    "remove_source_branch": False,
}))
PY
)"

if ! curl -fsS -H "PRIVATE-TOKEN: $GITLAB_MR_TOKEN" -H "Content-Type: application/json" \
  -X POST "${CI_API_V4_URL}/projects/${CI_PROJECT_ID}/merge_requests" \
  -d "$payload" >/dev/null; then
  log "routeros-chr-open-mr: $branch pushed, but creating the merge request failed -- open it by hand from that branch"
  exit 1
fi

log "routeros-chr-open-mr: opened a merge request from $branch into dev"
