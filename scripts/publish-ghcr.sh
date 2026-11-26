#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Copies the digest this pipeline built, tested and signed to GHCR, and
# signs it there too (#1563). The pattern: push from the trusted side; the
# signed evidence travels with the digest; verify at the destination.
#
# Why GitLab pushes rather than GitHub pulling: the owner's GitLab server
# geo-blocks GitHub's hosted runners, and that block stays (owner,
# 2026-11-25). Since #1472 the GitHub workflows pulled the image and its
# evidence from GitLab's API and registry, and timed out (v0.7.0's copy
# failed four times). Now this job, on the protected signing runner, writes
# GHCR, and .github/workflows/publish-from-gitlab.yml, release-on-tag.yml
# and countersign.yml read only GHCR: they verify the key-based signature
# and SPDX attestation this script puts there against the committed
# cosign.pub before GitHub attests anything.
#
# What it does, after a preflight that refuses -- writing nothing -- on a
# missing token, key, evidence or SBOM, or on a ref it does not publish:
#
#   1. logs in to GHCR with GHCR_PUSH_TOKEN on stdin;
#   2. copies the recorded digest, by digest, to its immutable tag
#      (`docker buildx imagetools create --prefer-index=false`, so the GHCR
#      digest is the GitLab digest) -- preview-<short7> on a push to
#      preview, vX.Y.Z on a release tag -- and reads it back, refusing
#      unless it resolves to the recorded digest;
#   3. signs the GHCR digest with the fenced key and attests the SPDX SBOM
#      to it, idempotently, as scripts/sign-release-digest.sh does in the
#      GitLab registry: an existing signature or attestation that verifies
#      against cosign.pub is left alone;
#   4. only then moves the floating tag, the same way: preview while this
#      commit is still preview's head, latest when this is the highest
#      vX.Y.Z tag. A failed copy, sign or attest exits before this, so
#      preview and latest never name a digest that is not signed and
#      attested on GHCR; they keep their previous digest instead.
#
# Inputs (environment):
#   CI_COMMIT_BRANCH / CI_COMMIT_TAG
#                        Which ref this is. Only `preview` and a `vX.Y.Z`
#                        tag publish; anything else refuses.
#   CI_COMMIT_SHA        This pipeline's commit; preview-<short7> is its
#                        first seven characters.
#   MV_PREVIEW_HEAD      On preview: the commit preview's head is at now.
#                        The job reads it with `git ls-remote`; the script
#                        does no git of its own.
#   MV_HIGHEST_TAG       On a tag: the highest vX.Y.Z tag now, read the
#                        same way.
#   GHCR_PUSH_TOKEN      A GitHub token that can write this one package
#                        (protected, masked CI/CD variable). Piped to
#                        `docker login --password-stdin`; never an argument.
#   MV_GHCR_IMAGE        Destination repository. Default:
#                        ghcr.io/tomlawesome/mikroview.
#   MV_GHCR_REGISTRY     Registry to log in to. Default: ghcr.io.
#   MV_GHCR_USER         User name for that login. Default: tomlawesome.
#   CI_REGISTRY_IMAGE    The GitLab repository publish:image pushed to; the
#                        evidence must name it. The job logs in to it
#                        before calling this script.
#   MV_SIGNING_DIR, COSIGN_PRIVATE_KEY, COSIGN_PASSWORD
#                        Key material, exactly as scripts/sign-release-digest.sh
#                        reads it (default /etc/mikroview-signing).
#   MV_EVIDENCE          Default .mv-supply-chain/gitlab-tested-image.json
#                        (publish:image).
#   MV_SBOM              Default image.spdx.json (security:trivy-image).
#   MV_DOCKER, MV_COSIGN Overridable only so tests can stub them.
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

fail() { printf 'publish-ghcr: %s\n' "$1" >&2; exit 1; }
say() { printf 'publish-ghcr: %s\n' "$1"; }

docker_cmd="${MV_DOCKER:-docker}"
ghcr_image="${MV_GHCR_IMAGE:-ghcr.io/tomlawesome/mikroview}"
ghcr_registry="${MV_GHCR_REGISTRY:-ghcr.io}"
ghcr_user="${MV_GHCR_USER:-tomlawesome}"
source_repo="${CI_REGISTRY_IMAGE:-}"
evidence="${MV_EVIDENCE:-.mv-supply-chain/gitlab-tested-image.json}"
sbom="${MV_SBOM:-image.spdx.json}"

# --- which tags: only preview and vX.Y.Z publish ---
commit="${CI_COMMIT_SHA:-}"
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "CI_COMMIT_SHA is not a 40-hex commit: '${commit}'"
short="${commit:0:7}"

immutable=""
floating=""
if [[ -n "${CI_COMMIT_TAG:-}" ]]; then
  version="$CI_COMMIT_TAG"
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
    fail "tag '${version}' is not vX.Y.Z -- only release tags publish to GHCR"
  highest="${MV_HIGHEST_TAG:-}"
  [[ "$highest" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
    fail "MV_HIGHEST_TAG is not vX.Y.Z ('${highest}') -- cannot tell whether latest should move"
  immutable="$version"
  if [[ "$highest" == "$version" ]]; then
    floating="latest"
  else
    say "latest not moved: ${version} is not the highest release tag (${highest} is)"
  fi
elif [[ "${CI_COMMIT_BRANCH:-}" == "preview" ]]; then
  head="${MV_PREVIEW_HEAD:-}"
  [[ "$head" =~ ^[0-9a-f]{40}$ ]] ||
    fail "MV_PREVIEW_HEAD is not a 40-hex commit ('${head}') -- cannot tell whether preview should move"
  immutable="preview-${short}"
  if [[ "$head" == "$commit" ]]; then
    floating="preview"
  else
    say "preview not moved: preview is now at ${head}, not ${commit}; publishing preview-${short} only"
  fi
else
  fail "ref '${CI_COMMIT_BRANCH:-${CI_COMMIT_TAG:-<none>}}' does not publish to GHCR -- only preview and vX.Y.Z tags do"
fi

# --- preflight: refuse before any login or write ---
[[ -n "${GHCR_PUSH_TOKEN:-}" ]] ||
  fail "GHCR_PUSH_TOKEN is empty -- the owner stores it as a protected, masked CI/CD variable"
[[ -n "$source_repo" ]] || fail "CI_REGISTRY_IMAGE is not set -- this runs in a GitLab job, which predefines it"

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

# gitlab-tested-image.py refuses a record of another repository or of
# another pipeline's commit, and a digest that is not sha256:<64 hex>.
digest="$(python3 -I scripts/gitlab-tested-image.py digest "$evidence" "$source_repo")" ||
  fail "no usable evidence at ${evidence} -- publish:image must run and pass first"
[[ -s "$sbom" ]] || fail "no SBOM at ${sbom} -- security:trivy-image must run and pass first"

source_ref="${source_repo}@${digest}"
target="${ghcr_image}@${digest}"

# --- log in, copy the immutable tag by digest, read it back ---
printf '%s' "$GHCR_PUSH_TOKEN" |
  "$docker_cmd" login "$ghcr_registry" -u "$ghcr_user" --password-stdin ||
  fail "docker login to ${ghcr_registry} failed"

copy_tag() {
  local dest="${ghcr_image}:$1" inspect_out got
  # --prefer-index=false: a single-manifest source is not wrapped in a new
  # index under a different digest (#1407).
  "$docker_cmd" buildx imagetools create --prefer-index=false -t "$dest" "$source_ref" ||
    fail "could not copy ${source_ref} to ${dest}"
  inspect_out="$("$docker_cmd" buildx imagetools inspect "$dest" 2>&1)" ||
    fail "could not read back ${dest}: ${inspect_out}"
  got="$(printf '%s\n' "$inspect_out" | awk '$1 == "Digest:" { print $2; exit }')"
  [[ "$got" == "$digest" ]] ||
    fail "${dest} resolves to ${got:-nothing}, not the recorded ${digest} -- refusing"
  say "copied ${source_ref} to ${dest}"
}

copy_tag "$immutable"

# --- sign and attest the GHCR digest, idempotently ---
cosign_cmd="${MV_COSIGN:-}"
if [ -z "$cosign_cmd" ]; then
  cosign_cmd="$(bash scripts/ensure-cosign.sh)"
fi
pubkey="${repo_root}/cosign.pub"

# Same rule as sign-release-digest.sh: only a verify that exits 0 *and*
# returns what a verification returns counts as already done.
already_signed() {
  local out
  out="$("$cosign_cmd" verify --key "$pubkey" --output json "$target" 2>/dev/null)" || return 1
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
  out="$("$cosign_cmd" verify-attestation --key "$pubkey" --type spdxjson "$target" 2>/dev/null)" || return 1
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
  say "${target} already signed, skipping"
else
  "$cosign_cmd" sign --key "$key_path" --yes "$target" ||
    fail "cosign could not sign ${target}"
  say "signed ${target}"
fi

if already_attested; then
  say "${target} already attested, skipping"
else
  "$cosign_cmd" attest \
    --key "$key_path" \
    --yes \
    --type spdxjson \
    --predicate "$sbom" \
    "$target" ||
    fail "cosign could not attest ${sbom} to ${target}"
  say "attested ${sbom} (spdxjson) to ${target}"
fi

# --- only now, signed and attested: the floating tag ---
if [[ -n "$floating" ]]; then
  copy_tag "$floating"
fi
