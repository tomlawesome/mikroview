#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# Publishes one CHR-exercise run's result to GitLab (#929, moved off GitHub
# by #943). The exercise (scripts/routeros-chr-exercise.sh, via
# .gitlab-ci.yml's chr-exercise:run) only ever runs on the GitLab runner,
# and nothing else knows it happened -- no check, nothing elsewhere, nothing
# but whatever a person remembers to type into an issue. This writes
# chr/last-run.json to its own branch on this GitLab project, chr-reports,
# so a scheduled GitLab job (chr-watch:run, the other half of #929 and #943)
# can watch that file instead.
#
# Its own branch, not dev or main: a bot commit there triggers no CI and
# never lands in the code tree. The branch carries nothing but this one
# file's history -- one commit per run is the run history. It does not
# exist on first use, so this script creates it as an ORPHAN commit (no
# parent, no code history) the first time, and adds an ordinary commit on
# top of it every run after that. Nothing here ever touches any other
# branch.
#
# Reads, all via environment (so the caller sets only what it has):
#   CHR_REPORT_RESULT            "pass" or "fail" -- required, except that
#                                 --dry-run will run without it (substituting
#                                 a placeholder) so the JSON shape can still
#                                 be checked with nothing else set up.
#   CHR_REPORT_SUMMARY           one line of plain text -- same exception
#                                 as CHR_REPORT_RESULT under --dry-run.
#   CHR_REPORT_ROUTEROS_VERSION  RouterOS version the run targeted.
#                                 Default: "unknown".
#   CHR_REPORT_COMMIT            Default: $CI_COMMIT_SHA.
#   CHR_REPORT_PIPELINE_URL      Default: $CI_PIPELINE_URL.
#   CHR_REPORT_JOB_URL           Default: $CI_JOB_URL.
#   GITLAB_MR_TOKEN              GitLab project access token (`api` +
#                                 `write_repository`) -- the same CI/CD
#                                 variable scripts/routeros-chr-open-mr.sh
#                                 already uses. Required to push; not
#                                 needed for --dry-run.
#   CI_SERVER_HOST, CI_PROJECT_PATH
#                                 GitLab's own predefined job variables --
#                                 nothing to configure, every job gets
#                                 them. Required to push; not needed for
#                                 --dry-run.
#
# Usage:
#   scripts/chr-report.sh             # build the report and push it
#   scripts/chr-report.sh --dry-run   # print the JSON to stdout, push nothing
set -eu

REPORT_BRANCH="chr-reports"

# Overridable so the push path can be exercised against a throwaway local
# repository. Without it the only way to find out whether the orphan
# branch is created correctly is to run it for real against GitLab, which
# is exactly the "never tested until it matters" trap this whole job
# exists to close (#929).
CHR_REPORT_REMOTE="${CHR_REPORT_REMOTE:-}"

log() { printf '%s\n' "$*" >&2; }

dry_run=0
case "${1:-}" in
  --dry-run) dry_run=1 ;;
  "") : ;;
  *)
    log "chr-report: unknown argument: $1"
    log "usage: $0 [--dry-run]"
    exit 2
    ;;
esac

if [ "$dry_run" = 1 ]; then
  : "${CHR_REPORT_RESULT:=pass}"
  : "${CHR_REPORT_SUMMARY:=dry run -- no real exercise result}"
else
  : "${CHR_REPORT_RESULT:?chr-report: CHR_REPORT_RESULT must be set to pass or fail}"
  : "${CHR_REPORT_SUMMARY:?chr-report: CHR_REPORT_SUMMARY must be set}"
fi

case "$CHR_REPORT_RESULT" in
  pass | fail) : ;;
  *)
    log "chr-report: CHR_REPORT_RESULT must be 'pass' or 'fail', got '$CHR_REPORT_RESULT'"
    exit 2
    ;;
esac

: "${CHR_REPORT_ROUTEROS_VERSION:=unknown}"
: "${CHR_REPORT_COMMIT:=${CI_COMMIT_SHA:-unknown}}"
: "${CHR_REPORT_PIPELINE_URL:=${CI_PIPELINE_URL:-unknown}}"
: "${CHR_REPORT_JOB_URL:=${CI_JOB_URL:-unknown}}"

finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# python3 builds and escapes the JSON, the same tool
# routeros-chr-open-mr.sh already uses for its merge-request payload --
# one dependency this job already installs (see .gitlab-ci.yml's
# chr-exercise:run before_script), rather than hand-rolling shell string
# escaping for a summary line whose exact contents this script does not
# control.
json="$(python3 - "$CHR_REPORT_RESULT" "$finished_at" "$CHR_REPORT_ROUTEROS_VERSION" \
  "$CHR_REPORT_COMMIT" "$CHR_REPORT_PIPELINE_URL" "$CHR_REPORT_JOB_URL" "$CHR_REPORT_SUMMARY" <<'PY'
import json, sys
result, finished_at, routeros_version, commit, pipeline_url, job_url, summary = sys.argv[1:8]
print(json.dumps({
    "schema": 1,
    "result": result,
    "finished_at": finished_at,
    "routeros_version": routeros_version,
    "commit": commit,
    "pipeline_url": pipeline_url,
    "job_url": job_url,
    "summary": summary,
}, indent=2))
PY
)"

if [ "$dry_run" = 1 ]; then
  printf '%s\n' "$json"
  exit 0
fi

: "${GITLAB_MR_TOKEN:?chr-report: GITLAB_MR_TOKEN is not set -- cannot publish. See the header comment above for what it needs.}"
: "${CI_SERVER_HOST:?chr-report: CI_SERVER_HOST is not set -- this script must run inside a GitLab CI job}"
: "${CI_PROJECT_PATH:?chr-report: CI_PROJECT_PATH is not set -- this script must run inside a GitLab CI job}"

workdir="$(mktemp -d)"

cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT

remote_url="${CHR_REPORT_REMOTE:-https://oauth2:${GITLAB_MR_TOKEN}@${CI_SERVER_HOST}/${CI_PROJECT_PATH}.git}"
# GitLab redacts any job-log occurrence of a variable's value once it is
# marked "masked" (required by scripts/routeros-chr-open-mr.sh's own
# header) -- the same protection that script already relies on for its
# own token, rather than this script adding a second, independent
# mechanism.

if git ls-remote --exit-code --heads "$remote_url" "$REPORT_BRANCH" >/dev/null 2>&1; then
  log "chr-report: $REPORT_BRANCH exists -- adding a commit to it"
  git clone --quiet --depth 1 --branch "$REPORT_BRANCH" "$remote_url" "$workdir"
else
  log "chr-report: $REPORT_BRANCH does not exist yet -- creating it as an orphan branch"
  git init --quiet "$workdir"
  git -C "$workdir" checkout --quiet --orphan "$REPORT_BRANCH"
fi

mkdir -p "$workdir/chr"
printf '%s\n' "$json" >"$workdir/chr/last-run.json"

git -C "$workdir" -c user.name="mikroview CHR exercise" -c user.email="chr-exercise@invalid" \
  add chr/last-run.json
git -C "$workdir" -c user.name="mikroview CHR exercise" -c user.email="chr-exercise@invalid" \
  commit --quiet -m "chr: $CHR_REPORT_RESULT at $finished_at"

git -C "$workdir" push --quiet "$remote_url" "HEAD:refs/heads/$REPORT_BRANCH"
log "chr-report: published chr/last-run.json ($CHR_REPORT_RESULT) to $REPORT_BRANCH"
