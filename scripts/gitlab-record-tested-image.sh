#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Records the image a GitLab pipeline built, tested and pushed to this
# project's registry (#1472), so the jobs after publish:image -- and,
# later, a GitHub workflow copying the digest to GHCR -- act on that exact
# digest instead of a tag or a rebuild. Modelled on orbit's
# scripts/ci/gitlab-record-tested-image.sh.
#
# The evidence is written the moment the push it describes has been
# checked, before anything else is attempted: a record that depends on a
# later step publishes nothing when that step fails, and the digest is
# lost with the job.
#
# Usage:
#   scripts/gitlab-record-tested-image.sh <image-reference> <image-digest> <built-at>
#
#   <image-reference>  <registry>/<path>@sha256:<64 hex>, naming <image-digest>
#   <image-digest>     sha256:<64 hex>
#   <built-at>         the image's org.opencontainers.image.created label,
#                      YYYY-MM-DDTHH:MM:SSZ (build:image stamps it)
#
# Everything else comes from the predefined GitLab job environment
# (CI_COMMIT_SHA, CI_COMMIT_REF_NAME, CI_PIPELINE_ID, CI_PIPELINE_URL).
# Every value is validated before it is written: this file decides what
# gets signed and published, so a malformed field must fail here rather
# than travel. Nothing written needs JSON escaping, by construction.
#
# Output: .mv-supply-chain/gitlab-tested-image.json in the repository
# root (MV_EVIDENCE_DIR overrides the directory, for tests). Read back by
# scripts/gitlab-tested-image.py.
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"

fail() { printf 'gitlab-record-tested-image: %s\n' "$1" >&2; exit 1; }

image_reference="${1:-}"
image_digest="${2:-}"
built_at="${3:-}"

[[ -n "$image_reference" ]] || fail 'an image reference is required as the first argument'
[[ -n "$image_digest" ]] || fail 'an image digest is required as the second argument'
[[ -n "$built_at" ]] || fail 'the build time is required as the third argument'

[[ "$image_digest" =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail "image digest is not an immutable manifest digest: ${image_digest}"
[[ "$image_reference" =~ ^[A-Za-z0-9._-]+(:[0-9]+)?(/[A-Za-z0-9._-]+)+@sha256:[0-9a-f]{64}$ ]] ||
  fail "image reference is not a plain registry reference pinned to a digest: ${image_reference}"
[[ "$image_reference" == *"@${image_digest}" ]] ||
  fail "image reference ${image_reference} does not name digest ${image_digest}"
[[ "$built_at" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] ||
  fail "build time is not a UTC timestamp (YYYY-MM-DDTHH:MM:SSZ): ${built_at}"

commit="${CI_COMMIT_SHA:-}"
ref="${CI_COMMIT_REF_NAME:-}"
pipeline_id="${CI_PIPELINE_ID:-}"
pipeline_url="${CI_PIPELINE_URL:-}"

[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "CI_COMMIT_SHA is not an exact commit SHA: ${commit:-<unset>}"
[[ "$ref" =~ ^[A-Za-z0-9._/-]+$ ]] || fail "CI_COMMIT_REF_NAME is not a plain ref name: ${ref:-<unset>}"
[[ "$pipeline_id" =~ ^[0-9]+$ ]] || fail "CI_PIPELINE_ID is not numeric: ${pipeline_id:-<unset>}"
[[ "$pipeline_url" =~ ^https://[A-Za-z0-9._:/-]+$ ]] || fail "CI_PIPELINE_URL is not an https URL: ${pipeline_url:-<unset>}"

output_dir="${MV_EVIDENCE_DIR:-${repo_root}/.mv-supply-chain}"
output="${output_dir}/gitlab-tested-image.json"

mkdir -p "$output_dir"
cat > "$output" <<JSON
{
  "imageReference": "${image_reference}",
  "imageDigest": "${image_digest}",
  "commit": "${commit}",
  "ref": "${ref}",
  "pipelineId": ${pipeline_id},
  "pipelineUrl": "${pipeline_url}",
  "builtAt": "${built_at}"
}
JSON

printf 'gitlab-record-tested-image: wrote %s\n' "$output"
