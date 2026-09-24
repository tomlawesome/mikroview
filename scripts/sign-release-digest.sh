#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Mints the first of two signatures over a release image (#1309): a
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
# The image is published by GitHub Actions, not this pipeline, so this
# script waits for it to appear on GHCR before it can resolve anything to
# sign. That wait loop is copied from the `record:upgrade-fixture` job in
# .gitlab-ci.yml (also triggered by a v* tag, also waiting on the same
# GHCR image) rather than re-invented.
#
# Usage:
#   scripts/sign-release-digest.sh <tag>
#
# Argument:
#   <tag>   The `v*` release tag naming the ghcr.io/tomlawesome/mikroview
#           image to sign. Falls back to $CI_COMMIT_TAG when omitted --
#           that is how the CI job below calls this; the argument form is
#           for running it by hand.
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
#   GHCR_PUBLISH_TOKEN   A GHCR token scoped to write:packages (already a
#                        masked, protected CI/CD variable). Piped to
#                        `docker login` on stdin -- never a command-line
#                        argument, so it never appears in a process
#                        listing.
#   MV_IMAGE             Optional; default ghcr.io/tomlawesome/mikroview.
#   MV_DOCKER            Optional; the docker command to run. Overridable
#                        only so tests can stub it; real use takes the
#                        default `docker`.
#   MV_COSIGN            Optional; the cosign command to run. Overridable
#                        only so tests can stub it; real use resolves the
#                        pinned binary via scripts/ensure-cosign.sh.
#   MV_SIGN_WAIT_ATTEMPTS,
#   MV_SIGN_WAIT_INTERVAL  Optional; default 45 and 60 (seconds), the same
#                        45-minute budget record:upgrade-fixture uses.
#                        Overridable only so tests do not actually wait.
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

fail() { printf 'sign-release-digest: %s\n' "$1" >&2; exit 1; }

tag="${1:-${CI_COMMIT_TAG:-}}"
[[ -n "$tag" ]] || fail "an image tag is required, as \$1 or \$CI_COMMIT_TAG"

: "${GHCR_PUBLISH_TOKEN:?GHCR_PUBLISH_TOKEN is not set -- it is a masked, protected CI/CD variable scoped to write:packages only}"

docker_cmd="${MV_DOCKER:-docker}"
repository="${MV_IMAGE:-ghcr.io/tomlawesome/mikroview}"
image="${repository}:${tag}"

# --- key material preflight, before the 45-minute wait below can even start ---
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

# --- wait for GHCR to have the image (copied from record:upgrade-fixture, .gitlab-ci.yml) ---
attempts="${MV_SIGN_WAIT_ATTEMPTS:-45}"
interval="${MV_SIGN_WAIT_INTERVAL:-60}"
echo "waiting for $image on GHCR (every ${interval}s, up to ${attempts} attempts)"
found=0
for attempt in $(seq 1 "$attempts"); do
  if "$docker_cmd" manifest inspect "$image" > /dev/null 2>&1; then
    found=1
    break
  fi
  echo "  attempt $attempt/$attempts: not published yet"
  [ "$attempt" = "$attempts" ] || sleep "$interval"
done
[ "$found" = 1 ] || fail "$image never appeared on GHCR after ${attempts} attempts"
echo "$image is published"

# --- resolve the digest, refusing rather than guessing ---
#
# `imagetools inspect` always prints exactly one top-level `Digest:` line --
# the digest of whatever the tag currently names, list or plain manifest --
# so counting that line can never catch a multi-arch image (verified
# against a real multi-platform image, 2026-09-22). What makes an image
# ambiguous is more than one *real* platform manifest under it: this
# project's release build has no `platforms:` input, so today's image is a
# single linux/amd64 manifest plus provenance/SBOM attestation manifests,
# which buildx reports as `Platform: unknown/unknown` and which are not a
# second image to be confused with the first. A genuine second platform
# (say linux/arm64 added later) is the case this refuses on, rather than
# silently signing one platform's digest and calling it the image's.
inspect_out="$("$docker_cmd" buildx imagetools inspect "$image" 2>&1)" ||
  fail "could not inspect $image: $inspect_out"

top_digest="$(printf '%s\n' "$inspect_out" | awk '$1 == "Digest:" { print $2; exit }')"
[ -n "$top_digest" ] || fail "$image: buildx imagetools inspect produced no Digest: line"

platform_count="$(printf '%s\n' "$inspect_out" |
  awk '$1 == "Platform:" && $2 != "unknown/unknown" { c++ } END { print c + 0 }')"
if [ "$platform_count" -gt 1 ]; then
  fail "$image resolves to $platform_count platform manifests, not one -- refusing to guess which digest to sign for a multi-arch image"
fi

[[ "$top_digest" =~ ^sha256:[0-9a-f]{64}$ ]] ||
  fail "resolved digest for $image is not sha256:<64 hex>: ${top_digest}"

digest="$top_digest"

# --- sign ---
cosign_cmd="${MV_COSIGN:-}"
if [ -z "$cosign_cmd" ]; then
  cosign_cmd="$(bash scripts/ensure-cosign.sh)"
fi

printf '%s' "$GHCR_PUBLISH_TOKEN" | "$docker_cmd" login ghcr.io -u tomlawesome --password-stdin ||
  fail "docker login to ghcr.io failed"

# The signature goes into the transparency log, unlike orbit's key-based
# signing, which sets --tlog-upload=false. That is the one place the two
# projects genuinely differ: orbit signs into its own private registry,
# where nobody outside can verify anything anyway, while this image is
# public on GHCR and the whole point of the pair is that an operator can
# check both signatures themselves.
#
# It is not a preference. `cosign verify --key` requires a log entry by
# default (--insecure-ignore-tlog defaults to false, and cosign's own help
# says an artefact "cannot be publicly verified when not included in a
# log"), so a signature made with --tlog-upload=false would fail both the
# GitHub countersignature's verify step and the plain command SECURITY.md
# hands operators -- and telling them to pass --insecure-ignore-tlog to
# check a release is not a story worth having. Rekor also timestamps the
# signature, which only strengthens the second custody.
"$cosign_cmd" sign \
  --key "$key_path" \
  --yes \
  "${repository}@${digest}" ||
  fail "cosign could not sign ${repository}@${digest}"

printf 'sign-release-digest: signed %s\n' "${repository}@${digest}"
