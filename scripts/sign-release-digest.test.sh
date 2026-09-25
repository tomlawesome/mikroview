#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/sign-release-digest.sh against stubbed docker and
# cosign commands -- never the network or a real registry. The refusals
# matter more than the happy path here: this script is the one place a
# multi-arch registry response, a missing key or a malformed digest must
# stop a signature from being minted rather than guessed at.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/sign-release-digest.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}

GOOD_DIGEST="sha256:$(printf 'a%.0s' $(seq 1 64))"
AMD64_DIGEST="sha256:$(printf 'b%.0s' $(seq 1 64))"
ARM64_DIGEST="sha256:$(printf 'c%.0s' $(seq 1 64))"
ATTEST_DIGEST="sha256:$(printf 'd%.0s' $(seq 1 64))"

# A signing runner mount with both files, for the tests that are not about
# a missing key.
KEYDIR="$TMP/keydir"
mkdir -p "$KEYDIR"
printf 'not a real key -- test fixture\n' > "$KEYDIR/cosign.key"
printf 'hunter2' > "$KEYDIR/password"

# The docker stub: a single script standing in for `docker manifest
# inspect`, `docker buildx imagetools inspect` and `docker login`, each
# controlled by an env var so one file can drive every case below.
DOCKER_STUB="$TMP/docker"
cat > "$DOCKER_STUB" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  manifest)
    [ "${STUB_PUBLISHED:-1}" = "1" ] && exit 0 || exit 1
    ;;
  buildx)
    printf '%s' "${STUB_INSPECT_OUTPUT:-}"
    exit "${STUB_INSPECT_EXIT:-0}"
    ;;
  login)
    cat > /dev/null
    printf '%s\n' "$*" >> "${STUB_LOGIN_CALLS:-/dev/null}"
    exit "${STUB_LOGIN_EXIT:-0}"
    ;;
  *)
    echo "unstubbed docker invocation: $*" >&2
    exit 99
    ;;
esac
STUB
chmod +x "$DOCKER_STUB"

# The cosign stub: records its arguments and the password cosign would
# have read from the environment, so the happy-path test can assert both
# without ever running the real binary.
COSIGN_STUB="$TMP/cosign"
cat > "$COSIGN_STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" > "${STUB_COSIGN_CALL:-/dev/null}"
printf '%s' "${COSIGN_PASSWORD:-}" > "${STUB_COSIGN_PASSWORD_SEEN:-/dev/null}"
exit "${STUB_COSIGN_EXIT:-0}"
STUB
chmod +x "$COSIGN_STUB"

single_platform_output() {
  printf 'Name:      ghcr.io/tomlawesome/mikroview:v9.9.9\n'
  printf 'MediaType: application/vnd.oci.image.index.v1+json\n'
  printf 'Digest:    %s\n\n' "$GOOD_DIGEST"
  printf 'Manifests:\n'
  printf '  Name:      ghcr.io/tomlawesome/mikroview@%s\n' "$AMD64_DIGEST"
  printf '  Platform:  linux/amd64\n\n'
  printf '  Name:      ghcr.io/tomlawesome/mikroview@%s\n' "$ATTEST_DIGEST"
  printf '  Platform:  unknown/unknown\n'
}

multi_platform_output() {
  single_platform_output
  printf '\n  Name:      ghcr.io/tomlawesome/mikroview@%s\n' "$ARM64_DIGEST"
  printf '  Platform:  linux/arm64\n'
}

run() {  # run <name>=<value>... -- runs the script with tag v9.9.9
  env -i PATH="/usr/bin:/bin" "$@" bash "$SCRIPT" v9.9.9
}

# 1. Happy path: single real platform (plus an attestation manifest, which
# is not a second image) resolves, GHCR login and cosign both run once,
# with the right key, the right digest and the runner's own password.
COSIGN_CALL="$TMP/cosign-call-1"
COSIGN_PASSWORD_SEEN="$TMP/cosign-password-1"
out="$(run \
  MV_SIGNING_DIR="$KEYDIR" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" \
  MV_COSIGN="$COSIGN_STUB" \
  MV_SIGN_WAIT_ATTEMPTS=2 MV_SIGN_WAIT_INTERVAL=0 \
  STUB_PUBLISHED=1 \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$COSIGN_CALL" \
  STUB_COSIGN_PASSWORD_SEEN="$COSIGN_PASSWORD_SEEN" \
  2>"$TMP/err1")"
rc=$?
check "$rc" "0" "happy path exits 0"
check "$(cat "$COSIGN_PASSWORD_SEEN" 2>/dev/null)" "hunter2" "cosign saw the runner's key password"
grep -q -- "--key ${KEYDIR}/cosign.key" "$COSIGN_CALL" 2>/dev/null &&
  echo "ok - cosign was given the mounted key" || { echo "FAIL - cosign was given the mounted key"; fail=1; }
grep -q "ghcr.io/tomlawesome/mikroview@${GOOD_DIGEST}" "$COSIGN_CALL" 2>/dev/null &&
  echo "ok - cosign signed the resolved index digest" || { echo "FAIL - cosign signed the resolved index digest"; fail=1; }
grep -q "$GOOD_DIGEST" <<< "$out" && echo "ok - the signed digest is reported" || { echo "FAIL - the signed digest is reported"; fail=1; }

# 2. Missing key: refuses, naming the exact path expected.
out2="$(run \
  MV_SIGNING_DIR="$TMP/no-key-dir" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" MV_COSIGN="$COSIGN_STUB" \
  2>&1)"
rc=$?
check "$rc" "1" "a missing key refuses"
grep -q "$TMP/no-key-dir/cosign.key" <<< "$out2" && echo "ok - the refusal names the expected key path" || { echo "FAIL - the refusal names the expected key path"; fail=1; }

# 3. Missing password: key present, password file absent.
NOPASSDIR="$TMP/no-pass-dir"
mkdir -p "$NOPASSDIR"
printf 'not a real key -- test fixture\n' > "$NOPASSDIR/cosign.key"
out3="$(run \
  MV_SIGNING_DIR="$NOPASSDIR" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" MV_COSIGN="$COSIGN_STUB" \
  2>&1)"
rc=$?
check "$rc" "1" "a missing password file refuses"
grep -q "$NOPASSDIR/password" <<< "$out3" && echo "ok - the refusal names the expected password path" || { echo "FAIL - the refusal names the expected password path"; fail=1; }

# 4. Unresolvable digest: the image is on GHCR (manifest inspect succeeds)
# but imagetools inspect itself fails -- nothing to sign, and the script
# must say so rather than pushing on with an empty or partial digest.
out4="$(run \
  MV_SIGNING_DIR="$KEYDIR" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" MV_COSIGN="$COSIGN_STUB" \
  MV_SIGN_WAIT_ATTEMPTS=2 MV_SIGN_WAIT_INTERVAL=0 \
  STUB_PUBLISHED=1 \
  STUB_INSPECT_EXIT=1 STUB_INSPECT_OUTPUT="unauthorized: authentication required" \
  2>&1)"
rc=$?
check "$rc" "1" "an unresolvable digest refuses"
grep -q "could not inspect" <<< "$out4" && echo "ok - the refusal says the inspect failed" || { echo "FAIL - the refusal says the inspect failed"; fail=1; }

# 5. Ambiguous digest: a genuine second platform manifest (not an
# attestation) is refused rather than silently signing one of them.
out5="$(run \
  MV_SIGNING_DIR="$KEYDIR" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" MV_COSIGN="$COSIGN_STUB" \
  MV_SIGN_WAIT_ATTEMPTS=2 MV_SIGN_WAIT_INTERVAL=0 \
  STUB_PUBLISHED=1 \
  STUB_INSPECT_OUTPUT="$(multi_platform_output)" \
  2>&1)"
rc=$?
check "$rc" "1" "an ambiguous (multi-arch) digest refuses"
grep -q "2 platform manifests" <<< "$out5" && echo "ok - the refusal names the platform count" || { echo "FAIL - the refusal names the platform count"; fail=1; }

# 6. Malformed digest: imagetools inspect succeeds with exactly one real
# platform but a value that is not a well-formed manifest digest.
out6="$(run \
  MV_SIGNING_DIR="$KEYDIR" \
  GHCR_PUBLISH_TOKEN="dummy-ghcr-token" \
  MV_DOCKER="$DOCKER_STUB" MV_COSIGN="$COSIGN_STUB" \
  MV_SIGN_WAIT_ATTEMPTS=2 MV_SIGN_WAIT_INTERVAL=0 \
  STUB_PUBLISHED=1 \
  STUB_INSPECT_OUTPUT="$(printf 'Name: x\nDigest: not-a-real-digest\n\nManifests:\n  Platform: linux/amd64\n')" \
  2>&1)"
rc=$?
check "$rc" "1" "a malformed digest refuses"
grep -q "is not sha256:<64 hex>" <<< "$out6" && echo "ok - the refusal says the digest shape is wrong" || { echo "FAIL - the refusal says the digest shape is wrong"; fail=1; }

exit "$fail"
