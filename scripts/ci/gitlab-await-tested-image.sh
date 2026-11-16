#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Finds the GitLab pipeline that built, tested, signed and published this
# exact commit, waits for it if it is still running, and fetches the evidence
# it left (#1472 step 3): .mv-supply-chain/gitlab-tested-image.json, written
# by scripts/gitlab-record-tested-image.sh in publish:image, and the SPDX SBOM
# of the same image, image.spdx.json from security:trivy-image. Both are
# checked before anything is handed on, because the caller copies to a public
# registry on the strength of what this script says. Ported from orbit's
# scripts/ci/gitlab-await-tested-image.sh, with this repo's job names and the
# MV_ prefix.
#
# It refuses (non-zero exit, a reason on stderr, nothing in $GITHUB_OUTPUT),
# rather than guessing, when:
#   - the ref is not `preview` or a release tag vX.Y.Z (checked before any API
#     call);
#   - no push pipeline exists for the commit and ref within the wait budget,
#     or one exists but is still failed when the budget runs out. A failed
#     pipeline is retried in place on GitLab (same id, new status), so a
#     failed pipeline is polled until the deadline rather than given up on;
#   - the newest such pipeline is canceled or skipped (deliberate, so at once);
#   - publish:image, sign:release-digest, publish:channel or
#     security:trivy-image did not succeed in it. publish:channel fails on a
#     preview push that a newer push superseded, so an older commit is refused
#     here and the newer commit's run publishes;
#   - the evidence is over 64 KiB, not a JSON object, names another commit,
#     ref or pipeline, has a digest that is not sha256:<64 lowercase hex>,
#     points outside the project's own registry image, or was built more than
#     seven days ago (or in the future);
#   - the SBOM is missing or not an SPDX document.
#
# Inputs (environment):
#   GITLAB_API_URL     e.g. https://gitlab.tomlawson.io/api/v4
#   GITLAB_PROJECT_ID  numeric project id (ai/mikroview is 53)
#   GITLAB_READ_TOKEN  a read_api token; never printed, never on a command line
#   GITLAB_REGISTRY    registry host the evidence must point into
#   MV_GITLAB_IMAGE    optional; the image the evidence must name, default
#                      ${GITLAB_REGISTRY}/ai/mikroview
#   MV_COMMIT          the commit being published (GITHUB_SHA)
#   MV_REF             `preview` or the vX.Y.Z tag (GITHUB_REF_NAME)
#   MV_WAIT_MINUTES    optional; how long to wait for GitLab, default 120
#   MV_POLL_SECONDS    optional; seconds between polls, default 60
#   MV_EVIDENCE_DIR    optional; where to write the fetched files,
#                      default .mv-supply-chain
#
# Output: the digest alone on stdout (progress goes to stderr); and, when
# $GITHUB_OUTPUT is set, digest, source_reference, pipeline_url, evidence and
# sbom appended to it.
set -Eeuo pipefail

fail() { printf 'gitlab-await-tested-image: %s\n' "$1" >&2; exit 1; }
say() { printf 'gitlab-await-tested-image: %s\n' "$1" >&2; }

: "${GITLAB_API_URL:?the GitLab API URL is required}"
: "${GITLAB_PROJECT_ID:?the GitLab project id is required}"
: "${GITLAB_READ_TOKEN:?a read-only GitLab token is required}"
: "${GITLAB_REGISTRY:?the GitLab registry host is required}"
: "${MV_COMMIT:?the commit to publish is required}"
ref="${MV_REF:-}"

[[ "$MV_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail "MV_COMMIT is not an exact commit SHA: ${MV_COMMIT}"
# Only the two refs GitLab publishes from: the preview branch and a release
# tag (release:check-version holds the tag to v<VERSION>, VERSION is X.Y.Z).
[[ "$ref" == preview || "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
  fail "MV_REF must be preview or a vX.Y.Z tag, not '${ref}'; nothing is published from any other ref"
[[ "$GITLAB_PROJECT_ID" =~ ^[0-9]+$ ]] || fail "GITLAB_PROJECT_ID is not numeric: ${GITLAB_PROJECT_ID}"

wait_minutes="${MV_WAIT_MINUTES:-120}"
[[ "$wait_minutes" =~ ^[0-9]+$ ]] || fail "MV_WAIT_MINUTES is not a whole number: ${wait_minutes}"
poll_seconds="${MV_POLL_SECONDS:-60}"
[[ "$poll_seconds" =~ ^[0-9]+$ ]] || fail "MV_POLL_SECONDS is not a whole number: ${poll_seconds}"
evidence_dir="${MV_EVIDENCE_DIR:-.mv-supply-chain}"
gitlab_image="${MV_GITLAB_IMAGE:-${GITLAB_REGISTRY}/ai/mikroview}"
project="${GITLAB_API_URL%/}/projects/${GITLAB_PROJECT_ID}"

# The token travels in a header file, not on the command line, so it never
# appears in a process listing or a shell trace.
# A curl that carries it never follows a redirect (no --location): GitLab
# answers an artifact download with a redirect to object storage when that is
# configured, and the token must not travel to that host. Every curl is
# https-only.
header_file="$(mktemp)"
response_headers="$(mktemp)"
trap 'rm -f "$header_file" "$response_headers"' EXIT
chmod 600 "$header_file"
printf 'PRIVATE-TOKEN: %s\n' "$GITLAB_READ_TOKEN" > "$header_file"
api() { curl --silent --show-error --fail --proto =https --max-time 60 --header @"$header_file" "$@"; }

# fetch URL FILE MAX_BYTES: an artifact download, capped in size. A redirect
# is not followed by the token-carrying curl; its target is fetched by a
# second curl with no token header (one hop, https only, same cap).
fetch() {
  local url="$1" out="$2" cap="$3" redirect
  redirect="$(api --max-filesize "$cap" --output "$out" --write-out '%{redirect_url}' "$url")" || return 1
  if [[ -n "$redirect" ]]; then
    [[ "$redirect" =~ ^https:// ]] || { say "artifact redirect is not https: ${redirect}"; return 1; }
    curl --silent --show-error --fail --proto =https --max-time 300 --max-filesize "$cap" \
      --output "$out" "$redirect" || return 1
  fi
  [[ -s "$out" ]]
}

# Reads one value out of the JSON on stdin with node (no jq on every image;
# node is on GitHub's runners and in the tests). $1 is a JavaScript
# expression over `input` and `arg` ($2); a missing value prints nothing and
# a document that is not JSON exits 3.
json() {
  node -e '
    let raw = "";
    process.stdin.on("data", (chunk) => { raw += chunk; }).on("end", () => {
      let input;
      try { input = JSON.parse(raw); } catch { process.exit(3); }
      const value = new Function("input", "arg", "return (" + process.argv[1] + ");")(input, process.argv[2]);
      process.stdout.write(value === undefined || value === null ? "" : String(value));
    });
  ' -- "$1" "${2:-}"
}

# GitLab lists pipelines newest first. Only a push pipeline for this exact
# commit on this exact ref counts: a merge-request pipeline for the same SHA
# tested another ref and never ran publish:image.
pipeline_query="${project}/pipelines?sha=${MV_COMMIT}&ref=${ref}&source=push&order_by=id&sort=desc&per_page=1"
deadline=$((SECONDS + wait_minutes * 60))
while :; do
  listing="$(api "$pipeline_query")" || fail "could not list GitLab pipelines for ${MV_COMMIT} on ${ref}"
  pipeline_id="$(json 'input[0]?.id' <<< "$listing")" || fail 'the GitLab pipeline listing is not JSON'
  status="$(json 'input[0]?.status' <<< "$listing")"
  case "$status" in
    success) break ;;
    canceled|skipped)
      fail "GitLab pipeline ${pipeline_id} for ${MV_COMMIT} on ${ref} ended ${status}; nothing to publish" ;;
    failed) say "GitLab pipeline ${pipeline_id} is failed; waiting until the deadline for a retry." ;;
    "") say "no push pipeline for ${MV_COMMIT} on ${ref} yet; waiting." ;;
    *) say "GitLab pipeline ${pipeline_id} is ${status}; waiting." ;;
  esac
  ((SECONDS < deadline)) ||
    fail "gave up after ${wait_minutes} minutes waiting for a successful GitLab pipeline for ${MV_COMMIT} on ${ref}; retry it on GitLab, then re-run this workflow"
  sleep "$poll_seconds"
done
[[ "$pipeline_id" =~ ^[0-9]+$ ]] || fail "GitLab pipeline id is not numeric: ${pipeline_id}"
pipeline_url="$(json 'input[0].web_url' <<< "$listing")"
[[ "$pipeline_url" =~ ^https://[A-Za-z0-9._:/-]+$ ]] || fail "GitLab pipeline URL is not a plain https URL: ${pipeline_url}"
say "GitLab pipeline ${pipeline_id} succeeded: ${pipeline_url}"

# include_retried is off, so a job retried into success is listed once.
: > "$response_headers"
jobs="$(api --dump-header "$response_headers" "${project}/pipelines/${pipeline_id}/jobs?per_page=100")" ||
  fail "could not list the jobs of GitLab pipeline ${pipeline_id}"
# One page of 100 is far more than this pipeline has; a second page means the
# listing is not what this script expects, so it refuses rather than read half.
if tr -d '\r' < "$response_headers" | grep -Eiq '^x-next-page:[[:space:]]*[0-9]+'; then
  fail "pipeline ${pipeline_id} lists more than 100 jobs; refusing to read a partial job list"
fi
job_id() {
  json 'input.filter((job) => job.name === arg && job.status === "success").sort((a, b) => a.id - b.id).at(-1)?.id' "$1" <<< "$jobs"
}
for name in publish:image sign:release-digest publish:channel security:trivy-image; do
  [[ -n "$(job_id "$name")" ]] || fail "pipeline ${pipeline_id} has no successful ${name} job; nothing is published"
done
publish_job="$(job_id publish:image)"
sbom_job="$(job_id security:trivy-image)"

mkdir -p "$evidence_dir"
evidence="${evidence_dir}/gitlab-tested-image.json"
sbom="${evidence_dir}/image.spdx.json"
fetch "${project}/jobs/${publish_job}/artifacts/.mv-supply-chain/gitlab-tested-image.json" "$evidence" 65536 ||
  fail "publish:image job ${publish_job} kept no gitlab-tested-image.json artifact (or it is over 64 KiB)"
# 16 MiB: a trivy SPDX SBOM of this image is well under 1 MiB.
fetch "${project}/jobs/${sbom_job}/artifacts/image.spdx.json" "$sbom" 16777216 ||
  fail "security:trivy-image job ${sbom_job} kept no image.spdx.json artifact (or it is over 16 MiB)"

# The evidence is a handful of short fields; anything large is not it.
size="$(wc -c < "$evidence")"
((size <= 65536)) || fail "gitlab-tested-image.json is ${size} bytes, over the 64 KiB limit"
[[ "$(json 'typeof input === "object" && input !== null && !Array.isArray(input)' < "$evidence" || :)" == true ]] ||
  fail 'gitlab-tested-image.json is not a JSON object'
field() { json 'input[arg]' "$1" < "$evidence"; }
recorded_commit="$(field commit)"
recorded_ref="$(field ref)"
recorded_pipeline="$(field pipelineId)"
digest="$(field imageDigest)"
source_reference="$(field imageReference)"
built_at="$(field builtAt)"

[[ "$recorded_commit" == "$MV_COMMIT" ]] || fail "evidence is for commit ${recorded_commit:-<missing>}, not ${MV_COMMIT}"
[[ "$recorded_ref" == "$ref" ]] || fail "evidence is for ref ${recorded_ref:-<missing>}, not ${ref}"
[[ "$recorded_pipeline" == "$pipeline_id" ]] || fail "evidence names pipeline ${recorded_pipeline:-<missing>}, not ${pipeline_id}"
[[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || fail "evidence digest is not a sha256 manifest digest: ${digest:-<missing>}"
[[ "$source_reference" == "${gitlab_image}@${digest}" ]] ||
  fail "evidence image reference ${source_reference:-<missing>} is not ${gitlab_image}@${digest}"
[[ "$built_at" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] ||
  fail "evidence builtAt is not a UTC timestamp: ${built_at:-<missing>}"
built_epoch="$(date -u -d "$built_at" +%s)" || fail "evidence builtAt is not a real time: ${built_at}"
now_epoch="$(date -u +%s)"
((now_epoch - built_epoch <= 7 * 24 * 3600)) || fail "evidence built at ${built_at} is older than seven days"
((built_epoch <= now_epoch + 300)) || fail "evidence built at ${built_at} is in the future"

[[ -n "$(json 'input.spdxVersion ?? input.SPDXID' < "$sbom" || :)" ]] || fail 'image.spdx.json is not an SPDX document'

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    printf 'digest=%s\n' "$digest"
    printf 'source_reference=%s\n' "$source_reference"
    printf 'pipeline_url=%s\n' "$pipeline_url"
    printf 'evidence=%s\n' "$evidence"
    printf 'sbom=%s\n' "$sbom"
  } >> "$GITHUB_OUTPUT"
fi
say "${MV_COMMIT} on ${ref} tested as ${source_reference}"
printf '%s\n' "$digest"
