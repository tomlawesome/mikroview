#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Mints the first of two signatures over a published image (#1309): a
# cosign key-based signature from a private key that exists only on this
# dedicated, protected `mikroview-signing` runner's host. The second is
# GitHub's keyless (Sigstore/OIDC) signature over the same digest, added
# by .github/workflows/countersign.yml -- deliberately not automatic: the
# owner starts it by hand, after it has verified this key-based signature
# against the committed cosign.pub (see docs/release-signing.md). Two
# signatures from two unrelated trust roots, one of them requiring the
# owner's own GitHub login: compromising GitHub's OIDC issuer alone is not
# enough to forge both.
#
# Since #1472 the image is built, tested and pushed by this same GitLab
# pipeline (publish:image), so there is nothing to wait for: the script
# signs the digest publish:image recorded in its evidence file, in this
# project's registry, and attests security:trivy-image's SPDX SBOM to that
# same digest (`cosign attest --key ... --type spdxjson --predicate
# image.spdx.json <repository>@<digest>`). It runs on every publishing pipeline -- a push to preview
# and a v* tag (owner, #1472: preview images are signed too).
#
# Usage:
#   scripts/sign-release-digest.sh          sign the recorded digest
#   scripts/sign-release-digest.sh <tag>    exercise path (#1309): sign
#                                           ${MV_IMAGE}:<tag>, only if it
#                                           resolves to the recorded
#                                           digest; no SBOM attestation
#
# Inputs (environment):
#   MV_SIGNING_DIR      The signing runner's read-only key mount. Must
#                        hold `cosign.key` and `password`.
#                        Default: /etc/mikroview-signing, confirmed by the
#                        owner against the signing runner's config.toml on
#                        2026-09-22: the runner mounts
#                        "/etc/mikroview-signing:/etc/mikroview-signing:ro".
#                        The default is therefore the real path, not a
#                        guess; MV_SIGNING_DIR exists for tests and for a
#                        future runner that mounts it elsewhere.
#                        Note the mount is read-only, so nothing here can
#                        write to it even by accident.
#   COSIGN_PRIVATE_KEY   Path to the private key. Takes priority over
#                        MV_SIGNING_DIR when set; settable directly so
#                        tests never need a fake /etc directory.
#   COSIGN_PASSWORD      The key's password. Takes priority over
#                        MV_SIGNING_DIR when set, for the same reason.
#                        cosign reads this from the environment itself.
#   CI_REGISTRY, CI_REGISTRY_USER, CI_REGISTRY_PASSWORD
#                        GitLab's predefined job-token registry login.
#                        The password is piped to `docker login` on stdin --
#                        never a command-line argument, so it never appears
#                        in a process listing.
#   MV_IMAGE             The repository to sign in. Default:
#                        $CI_REGISTRY_IMAGE (registry.tomlawson.io/ai/mikroview).
#   MV_EVIDENCE          Optional; default
#                        .mv-supply-chain/gitlab-tested-image.json, the
#                        artifact publish:image writes
#                        (scripts/gitlab-record-tested-image.sh).
#   MV_SBOM              Optional; default image.spdx.json, the artifact
#                        security:trivy-image writes.
#   MV_DOCKER            Optional; the docker command to run. Overridable
#                        only so tests can stub it; real use takes the
#                        default `docker`.
#   MV_COSIGN            Optional; the cosign command to run. Overridable
#                        only so tests can stub it; real use resolves the
#                        pinned binary via scripts/ensure-cosign.sh.
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

fail() { printf 'sign-release-digest: %s\n' "$1" >&2; exit 1; }

tag="${1:-}"

docker_cmd="${MV_DOCKER:-docker}"
repository="${MV_IMAGE:-${CI_REGISTRY_IMAGE:-}}"
[[ -n "$repository" ]] || fail "no repository to sign in: CI_REGISTRY_IMAGE (or MV_IMAGE) is not set"
: "${CI_REGISTRY:?CI_REGISTRY is not set -- this runs in a GitLab job, which predefines it}"
: "${CI_REGISTRY_USER:?CI_REGISTRY_USER is not set -- this runs in a GitLab job, which predefines it}"
: "${CI_REGISTRY_PASSWORD:?CI_REGISTRY_PASSWORD is not set -- this runs in a GitLab job, which predefines it}"

evidence="${MV_EVIDENCE:-.mv-supply-chain/gitlab-tested-image.json}"
sbom="${MV_SBOM:-image.spdx.json}"

# --- key material preflight, before anything touches the registry ---
signing_dir="${MV_SIGNING_DIR:-/etc/mikroview-signing}"

if [[ -n "${COSIGN_PRIVATE_KEY:-}" ]]; then
  key_path="$COSIGN_PRIVATE_KEY"
else
  key_path="${signing_dir}/cosign.key"
fi
[[ -f "$key_path" ]] ||
  fail "no private key at ${key_path} -- the signing runner mounts it read-only from the host; set MV_SIGNING_DIR or COSIGN_PRIVATE_KEY if it lives elsewhere"

if [[ -n "${COSIGN_PASSWORD:-}" ]]; then
  password="$COSIGN_PASSWORD"
else
  password_path="${signing_dir}/password"
  [[ -f "$password_path" ]] ||
    fail "no key password at ${password_path} -- the signing runner mounts it read-only alongside cosign.key; set MV_SIGNING_DIR or COSIGN_PASSWORD if it lives elsewhere"
  password="$(<"$password_path")"
fi
export COSIGN_PASSWORD="$password"

# --- what to sign: the recorded digest, or (exercise path) a named tag ---
#
# Both paths sign only the digest this pipeline's own publish:image tested
# and recorded. A tag is never signed on its own say-so: any job token
# with push access -- a merge-request job included -- can move a tag in
# the GitLab registry, so the exercise path's tag must resolve to the
# recorded digest too. gitlab-tested-image.py refuses a record of another
# repository or of another pipeline's commit, and a digest that is not
# sha256:<64 hex>.
recorded="$(python3 -I scripts/gitlab-tested-image.py digest "$evidence" "$repository")" ||
  fail "no usable evidence at ${evidence} -- publish:image must run and pass first"
if [[ -z "$tag" ]]; then
  # The SBOM is part of what a publishing run signs: refuse before the
  # signature rather than leave a signed digest with no attestation.
  [[ -s "$sbom" ]] || fail "no SBOM at ${sbom} -- security:trivy-image must run and pass first"
  target="${repository}@${recorded}"
else
  target="${repository}:${tag}"
fi

printf '%s' "$CI_REGISTRY_PASSWORD" |
  "$docker_cmd" login "$CI_REGISTRY" -u "$CI_REGISTRY_USER" --password-stdin ||
  fail "docker login to ${CI_REGISTRY} failed"

# --- resolve the digest, refusing rather than guessing ---
#
# `imagetools inspect` always prints exactly one top-level `Digest:` line --
# the digest of whatever the reference names, list or plain manifest --
# so counting that line can never catch a multi-arch image (verified
# against a real multi-platform image, 2026-09-22). What makes an image
# ambiguous is more than one *real* platform manifest under it: build:image
# makes one linux/amd64 manifest with no attestation manifests, and an
# older image may carry attestation manifests that buildx reports as
# `Platform: unknown/unknown`, which are not a second image to be confused
# with the first. A genuine second platform (say linux/arm64 added later)
# is the case this refuses on, rather than silently signing one
# platform's digest and calling it the image's.
inspect_out="$("$docker_cmd" buildx imagetools inspect "$target" 2>&1)" ||
  fail "could not inspect $target: $inspect_out"

top_digest="$(printf '%s\n' "$inspect_out" | awk '$1 == "Digest:" { print $2; exit }')"
[ -n "$top_digest" ] || fail "$target: buildx imagetools inspect produced no Digest: line"

platform_count="$(printf '%s\n' "$inspect_out" |
  awk '$1 == "Platform:" && $2 != "unknown/unknown" { c++ } END { print c + 0 }')"
if [ "$platform_count" -gt 1 ]; then
  fail "$target resolves to $platform_count platform manifests, not one -- refusing to guess which digest to sign for a multi-arch image"
fi

[[ "$top_digest" =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail "resolved digest for $target is not sha256:<64 hex>: ${top_digest}"

if [[ "$top_digest" != "$recorded" ]]; then
  fail "$target resolves to $top_digest, not the recorded $recorded -- refusing"
fi

digest="$top_digest"

# --- sign, then attest the SBOM ---
cosign_cmd="${MV_COSIGN:-}"
if [ -z "$cosign_cmd" ]; then
  cosign_cmd="$(bash scripts/ensure-cosign.sh)"
fi

# The signature goes into the transparency log, unlike orbit's key-based
# signing, which sets --tlog-upload=false. That is the one place the two
# projects genuinely differ: since #1472 step 3 this signature stays in the
# GitLab registry (GitHub copies the image only), so the Rekor entry is its
# one public record, and it outlives the registry's retention.
#
# It is not a preference. `cosign verify --key` requires a log entry by
# default (--insecure-ignore-tlog defaults to false, and cosign's own help
# says an artefact "cannot be publicly verified when not included in a
# log"), so a signature made with --tlog-upload=false would fail
# publish:channel's verify step, the GitHub countersignature's, and the
# command SECURITY.md hands anyone checking the GitLab digest -- and telling them to pass
# --insecure-ignore-tlog to check a release is not a story worth having.
# Rekor also timestamps the signature, which only strengthens the second
# custody.
#
# A retried job is a no-op, not a second signature (#1472 note 59703):
# before signing, a signature that already verifies against cosign.pub on
# this digest is left alone; before attesting, so is an SPDX attestation
# that already verifies (`attest --replace` does not replace in cosign
# 3.1.3). Only a verify that exits 0 *and* returns what a verification
# returns counts: anything else is "not yet", and signing proceeds.
pubkey="cosign.pub"

already_signed() {
  local out
  out="$("$cosign_cmd" verify --key "$pubkey" --output json "${repository}@${digest}" 2>/dev/null)" || return 1
  python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
except ValueError:
    sys.exit(1)
sys.exit(0 if isinstance(data, list) and len(data) > 0 else 1)
' <<< "$out"
}

already_attested() {
  local out
  out="$("$cosign_cmd" verify-attestation --key "$pubkey" --type spdxjson "${repository}@${digest}" 2>/dev/null)" || return 1
  python3 -c '
import json, sys
n = 0
for line in sys.stdin:
    if not line.strip():
        continue
    try:
        doc = json.loads(line)
    except ValueError:
        sys.exit(1)
    if not isinstance(doc, dict) or "payload" not in doc:
        sys.exit(1)
    n += 1
sys.exit(0 if n > 0 else 1)
' <<< "$out"
}

if already_signed; then
  printf 'sign-release-digest: %s already signed, skipping\n' "${repository}@${digest}"
else
  "$cosign_cmd" sign \
    --key "$key_path" \
    --yes \
    "${repository}@${digest}" ||
    fail "cosign could not sign ${repository}@${digest}"
  printf 'sign-release-digest: signed %s\n' "${repository}@${digest}"
fi

if [[ -z "$tag" ]] && already_attested; then
  printf 'sign-release-digest: %s already attested, skipping\n' "${repository}@${digest}"
elif [[ -z "$tag" ]]; then
  "$cosign_cmd" attest \
    --key "$key_path" \
    --yes \
    --type spdxjson \
    --predicate "$sbom" \
    "${repository}@${digest}" ||
    fail "cosign could not attest ${sbom} to ${repository}@${digest}"
  printf 'sign-release-digest: attested %s (spdxjson) to %s\n' "$sbom" "${repository}@${digest}"
else
  printf 'sign-release-digest: exercise path (tag %s): no SBOM attestation\n' "$tag"
fi
