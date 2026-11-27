#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/publish-ghcr.sh (#1563) against stubbed docker and
# cosign commands -- never the network. The script copies the digest that
# publish:image recorded to GHCR and signs it there; the refusals (no
# token, no evidence, no SBOM, no key, a tag that moved, an unexpected ref)
# matter more than the happy path.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/publish-ghcr.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
check() {
  if [ "$1" = "$2" ]; then echo "ok - $3"; else echo "FAIL - $3: expected [$2] got [$1]"; fail=1; fi
}

SRC="registry.example.test/ai/mikroview"
GHCR="ghcr.example.test/owner/mikroview"
TOKEN="ghp_FAKEtokenForTestsOnly0123456789"
KEYPASS="hunter2-fake-password"
COMMIT="$(printf '1%.0s' $(seq 1 40))"
SHORT="1111111"
OTHER_COMMIT="$(printf '2%.0s' $(seq 1 40))"
DIGEST="sha256:$(printf 'a%.0s' $(seq 1 64))"
OTHER_DIGEST="sha256:$(printf 'e%.0s' $(seq 1 64))"

KEYDIR="$TMP/keydir"
mkdir -p "$KEYDIR"
printf 'not a real key -- test fixture\n' > "$KEYDIR/cosign.key"
printf '%s' "$KEYPASS" > "$KEYDIR/password"

EVIDENCE="$TMP/evidence.json"
cat > "$EVIDENCE" <<JSON
{
  "imageReference": "${SRC}@${DIGEST}",
  "imageDigest": "${DIGEST}",
  "commit": "${COMMIT}",
  "ref": "preview",
  "pipelineId": 42,
  "pipelineUrl": "https://gitlab.example.test/ai/mikroview/-/pipelines/42",
  "builtAt": "2026-11-15T00:00:00Z"
}
JSON
SBOM="$TMP/image.spdx.json"
printf '{"spdxVersion":"SPDX-2.3","name":"test fixture"}\n' > "$SBOM"

# Every docker call is logged as one argv line; login's stdin is saved.
# `imagetools inspect` answers with STUB_INSPECT_DIGEST.
DOCKER_STUB="$TMP/docker"
cat > "$DOCKER_STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${STUB_DOCKER_CALLS:-/dev/null}"
printf 'docker %s\n' "$*" >> "${STUB_ORDER_LOG:-/dev/null}"
case "$1" in
  login)
    cat >> "${STUB_LOGIN_STDIN:-/dev/null}"
    exit "${STUB_LOGIN_EXIT:-0}" ;;
  logout) exit 0 ;;
  buildx)
    if [ "$2 $3" = "imagetools inspect" ]; then
      printf 'Name:      %s\nMediaType: application/vnd.oci.image.index.v1+json\nDigest:    %s\n' "$4" "${STUB_INSPECT_DIGEST}"
      exit 0
    fi
    exit "${STUB_CREATE_EXIT:-0}" ;;
  *) echo "unstubbed docker invocation: $*" >&2; exit 99 ;;
esac
STUB
chmod +x "$DOCKER_STUB"

COSIGN_STUB="$TMP/cosign"
cat > "$COSIGN_STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${STUB_COSIGN_CALLS:-/dev/null}"
printf 'cosign %s\n' "$*" >> "${STUB_ORDER_LOG:-/dev/null}"
case "$1" in
  verify) printf '%s' "${STUB_VERIFY_OUTPUT:-}"; exit "${STUB_VERIFY_EXIT:-1}" ;;
  verify-attestation) printf '%s' "${STUB_VERIFY_ATT_OUTPUT:-}"; exit "${STUB_VERIFY_ATT_EXIT:-1}" ;;
esac
exit "${STUB_COSIGN_EXIT:-0}"
STUB
chmod +x "$COSIGN_STUB"

LAST_OUT=""
run() {  # run <name>=<value>...  (fresh logs each time)
  rm -f "$TMP/docker.log" "$TMP/cosign.log" "$TMP/stdin.log" "$TMP/order.log"
  : > "$TMP/docker.log"; : > "$TMP/cosign.log"; : > "$TMP/stdin.log"
  # shellcheck disable=SC2034  # kept for debugging a failing case
  LAST_OUT="$(env -i PATH="/usr/bin:/bin" \
    CI_REGISTRY=registry.example.test \
    CI_REGISTRY_USER=gitlab-ci-token \
    CI_REGISTRY_PASSWORD=dummy-job-token \
    CI_REGISTRY_IMAGE="$SRC" \
    CI_COMMIT_SHA="$COMMIT" \
    MV_GHCR_IMAGE="$GHCR" \
    GHCR_PUSH_TOKEN="$TOKEN" \
    COSIGN_PASSWORD="$KEYPASS" \
    MV_SIGNING_DIR="$KEYDIR" \
    MV_DOCKER="$DOCKER_STUB" \
    MV_COSIGN="$COSIGN_STUB" \
    MV_EVIDENCE="$EVIDENCE" \
    MV_SBOM="$SBOM" \
    STUB_ORDER_LOG="$TMP/order.log" \
    STUB_DOCKER_CALLS="$TMP/docker.log" \
    STUB_COSIGN_CALLS="$TMP/cosign.log" \
    STUB_LOGIN_STDIN="$TMP/stdin.log" \
    STUB_INSPECT_DIGEST="$DIGEST" \
    "$@" bash "$SCRIPT" 2>&1)"
}

count() { grep -c -- "$1" "$2" 2>/dev/null || true; }
has() { grep -qF -- "$1" "$2" && echo yes || echo no; }
nothing_written() {  # no create, no cosign call, no login
  local n; n=$(( $(count 'imagetools create' "$TMP/docker.log") + $(count '^login' "$TMP/docker.log") + $(wc -l < "$TMP/cosign.log") ))
  echo "$n"
}
CREATE_PRE="buildx imagetools create --prefer-index=false -t"

# 1. Happy path, preview head: both tags copied by digest, read back,
# signed and attested; token on stdin only.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT"; status=$?
check "$status" "0" "preview head: exits 0"
check "$(has "${CREATE_PRE} ${GHCR}:preview-${SHORT} ${SRC}@${DIGEST}" "$TMP/docker.log")" "yes" "preview head: preview-<short7> copied by recorded digest"
check "$(has "${CREATE_PRE} ${GHCR}:preview ${SRC}@${DIGEST}" "$TMP/docker.log")" "yes" "preview head: preview copied by recorded digest"
check "$(count "imagetools inspect ${GHCR}:preview-${SHORT}" "$TMP/docker.log")" "1" "preview head: preview-<short7> read back"
check "$(count "imagetools inspect ${GHCR}:preview\$" "$TMP/docker.log")" "1" "preview head: preview read back"
check "$(has "login ghcr.io" "$TMP/docker.log")" "yes" "login goes to ghcr.io"
check "$(has "--password-stdin" "$TMP/docker.log")" "yes" "login uses --password-stdin"
check "$(cat "$TMP/stdin.log")" "$TOKEN" "GHCR token arrives on login's stdin"
check "$(has "$TOKEN" "$TMP/docker.log")" "no" "GHCR token absent from every docker argv"
check "$(has "$TOKEN" "$TMP/cosign.log")" "no" "GHCR token absent from every cosign argv"
check "$(has "$KEYPASS" "$TMP/docker.log")$(has "$KEYPASS" "$TMP/cosign.log")" "nono" "cosign password absent from every argv"
check "$(has "dummy-job-token" "$TMP/docker.log")" "no" "GitLab job token absent from docker argv"
check "$(count '^sign .*--key .*'"${GHCR}@${DIGEST}" "$TMP/cosign.log")" "1" "cosign signs the GHCR digest with --key"
check "$(count '^attest .*--key .*--type spdxjson .*--predicate '"$SBOM"' '"${GHCR}@${DIGEST}" "$TMP/cosign.log")" "1" "cosign attests the SBOM to the GHCR digest"
check "$(grep -c -- "${SRC}:" "$TMP/docker.log")" "0" "source is never addressed by tag"

# 2. Preview, no longer the head: only the immutable preview-<short7> tag.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$OTHER_COMMIT"; status=$?
check "$status" "0" "stale preview: exits 0"
check "$(has "-t ${GHCR}:preview-${SHORT} " "$TMP/docker.log")" "yes" "stale preview: preview-<short7> still pushed"
check "$(has "-t ${GHCR}:preview ${SRC}" "$TMP/docker.log")" "no" "stale preview: moving preview tag not pushed"

# 3. Tag push, highest: vX.Y.Z and latest.
run CI_COMMIT_TAG=v1.2.3 MV_HIGHEST_TAG=v1.2.3; status=$?
check "$status" "0" "highest tag: exits 0"
check "$(has "${CREATE_PRE} ${GHCR}:v1.2.3 ${SRC}@${DIGEST}" "$TMP/docker.log")" "yes" "highest tag: vX.Y.Z copied by digest"
check "$(has "${CREATE_PRE} ${GHCR}:latest ${SRC}@${DIGEST}" "$TMP/docker.log")" "yes" "highest tag: latest copied by digest"
check "$(count '^sign ' "$TMP/cosign.log")" "1" "highest tag: signed once"

# 4. Tag push, not the highest: no latest.
run CI_COMMIT_TAG=v1.2.3 MV_HIGHEST_TAG=v1.3.0; status=$?
check "$status" "0" "older tag: exits 0"
check "$(has "-t ${GHCR}:v1.2.3 " "$TMP/docker.log")" "yes" "older tag: vX.Y.Z pushed"
check "$(has "-t ${GHCR}:latest" "$TMP/docker.log")" "no" "older tag: latest not pushed"

# 5. Any other ref refuses, writing nothing.
run CI_COMMIT_BRANCH=feature/x MV_PREVIEW_HEAD="$COMMIT"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "other branch: refuses"
check "$(nothing_written)" "0" "other branch: nothing written"
run MV_HIGHEST_TAG=v1.0.0; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "no ref at all: refuses"
check "$(nothing_written)" "0" "no ref at all: nothing written"
run CI_COMMIT_TAG=not-a-version MV_HIGHEST_TAG=v1.0.0; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "tag that is not vX.Y.Z: refuses"
check "$(nothing_written)" "0" "tag that is not vX.Y.Z: nothing written"

# 6. Preflight refusals: non-zero, no create, no cosign, no login.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN=; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "empty GHCR_PUSH_TOKEN: refuses"
check "$(nothing_written)" "0" "empty GHCR_PUSH_TOKEN: nothing written"

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" MV_EVIDENCE="$TMP/missing.json"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "missing evidence: refuses"
check "$(nothing_written)" "0" "missing evidence: nothing written"

printf 'not json' > "$TMP/bad.json"
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" MV_EVIDENCE="$TMP/bad.json"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "invalid evidence: refuses"
check "$(nothing_written)" "0" "invalid evidence: nothing written"

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" MV_SBOM="$TMP/missing.spdx.json"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "missing SBOM: refuses"
check "$(nothing_written)" "0" "missing SBOM: nothing written"

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" MV_SIGNING_DIR="$TMP/nokeys"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "missing key: refuses"
check "$(nothing_written)" "0" "missing key: nothing written"

# 7. A GHCR tag that reads back as another digest is refused, unsigned.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_INSPECT_DIGEST="$OTHER_DIGEST"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "read-back digest mismatch: refuses"
check "$(wc -l < "$TMP/cosign.log" | tr -d ' ')" "0" "read-back digest mismatch: no cosign call"

# 8. Failed copy refuses and signs nothing.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_CREATE_EXIT=1; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "failed imagetools create: refuses"
check "$(wc -l < "$TMP/cosign.log" | tr -d ' ')" "0" "failed imagetools create: no cosign call"

# 9. Idempotent retry: an existing verifying signature / attestation is
# left alone, each independently.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_VERIFY_EXIT=0 STUB_VERIFY_OUTPUT='[{"critical":{}}]'; status=$?
check "$status" "0" "already signed: exits 0"
check "$(count '^sign ' "$TMP/cosign.log")" "0" "already signed: no second signature"
check "$(count '^attest ' "$TMP/cosign.log")" "1" "already signed: still attests"

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_VERIFY_ATT_EXIT=0 STUB_VERIFY_ATT_OUTPUT='{"payload":"x"}'; status=$?
check "$status" "0" "already attested: exits 0"
check "$(count '^sign ' "$TMP/cosign.log")" "1" "already attested: still signs"
check "$(count '^attest ' "$TMP/cosign.log")" "0" "already attested: no second attestation"
check "$(has "verify --key" "$TMP/cosign.log")" "yes" "skip check uses cosign verify --key"
check "$(has "verify-attestation --key" "$TMP/cosign.log")" "yes" "skip check uses cosign verify-attestation --key"

# 10. Default destination when MV_GHCR_IMAGE is unset.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" MV_GHCR_IMAGE=; status=$?
check "$(has "-t ghcr.io/tomlawesome/mikroview:preview-${SHORT} " "$TMP/docker.log")" "yes" "default destination is ghcr.io/tomlawesome/mikroview"

# 11. Order: the immutable tag first, then sign and attest the GHCR digest,
# and only then the floating tags (preview / latest).
line_of() {  # first line number in the order log matching a fixed string, or 0
  grep -nF -- "$1" "$TMP/order.log" | head -1 | cut -d: -f1 | grep . || echo 0
}
last_line_of() { grep -nF -- "$1" "$TMP/order.log" | tail -1 | cut -d: -f1 | grep . || echo 0; }
before() { [ "$1" -gt 0 ] && [ "$2" -gt 0 ] && [ "$1" -lt "$2" ] && echo yes || echo no; }

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT"
imm="$(line_of "-t ${GHCR}:preview-${SHORT} ")"
flo="$(line_of "-t ${GHCR}:preview ")"
sgn="$(last_line_of "cosign sign ")"
att="$(last_line_of "cosign attest ")"
check "$(before "$imm" "$sgn")" "yes" "preview: immutable tag written before cosign sign"
check "$(before "$sgn" "$flo")" "yes" "preview: cosign sign before the floating preview tag"
check "$(before "$att" "$flo")" "yes" "preview: cosign attest before the floating preview tag"

run CI_COMMIT_TAG=v1.2.3 MV_HIGHEST_TAG=v1.2.3
imm="$(line_of "-t ${GHCR}:v1.2.3 ")"
flo="$(line_of "-t ${GHCR}:latest ")"
sgn="$(last_line_of "cosign sign ")"
att="$(last_line_of "cosign attest ")"
check "$(before "$imm" "$sgn")" "yes" "tag: vX.Y.Z written before cosign sign"
check "$(before "$sgn" "$flo")" "yes" "tag: cosign sign before the latest tag"
check "$(before "$att" "$flo")" "yes" "tag: cosign attest before the latest tag"

# 12. Failed sign or attest: no floating tag is written at all.
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_COSIGN_EXIT=1; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "cosign failure (preview): refuses"
check "$(has "-t ${GHCR}:preview ${SRC}" "$TMP/order.log")" "no" "cosign failure (preview): no floating preview tag"
run CI_COMMIT_TAG=v1.2.3 MV_HIGHEST_TAG=v1.2.3 STUB_COSIGN_EXIT=1; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "cosign failure (tag): refuses"
check "$(has "-t ${GHCR}:latest " "$TMP/order.log")" "no" "cosign failure (tag): no latest tag"
# attest alone failing (sign already verifies): still no floating tag
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" STUB_VERIFY_EXIT=0 STUB_VERIFY_OUTPUT='[{"critical":{}}]' STUB_COSIGN_EXIT=1; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "attest failure: refuses"
check "$(has "-t ${GHCR}:preview ${SRC}" "$TMP/order.log")" "no" "attest failure: no floating preview tag"

# 13. The push token comes from KEYDIR/ghcr-token (#1569); GHCR_PUSH_TOKEN
# only overrides it so tests can set it.
FILE_TOKEN="ghp_FAKEfileTokenForTestsOnly9876543210"
printf '%s\n' "$FILE_TOKEN" > "$KEYDIR/ghcr-token"
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN=; status=$?
file_out="$LAST_OUT"
check "$status" "0" "token file only: exits 0"
check "$(tr -d '\n' < "$TMP/stdin.log")" "$FILE_TOKEN" "token file only: file's token arrives on login's stdin"
check "$(has "$FILE_TOKEN" "$TMP/docker.log")$(has "$FILE_TOKEN" "$TMP/cosign.log")" "nono" "token file only: token absent from every argv"
check "$(printf '%s' "$file_out" | grep -cF -- "$FILE_TOKEN" || true)" "0" "token file only: token absent from script output"
check "$(count '^logout ghcr.io$' "$TMP/docker.log")" "1" "token file only: docker logout ghcr.io on success"

run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN="$TOKEN"; status=$?
env_out="$LAST_OUT"
check "$status" "0" "env and file both set: exits 0"
check "$(cat "$TMP/stdin.log")" "$TOKEN" "env and file both set: the env value is used"
check "$(printf '%s%s' "$env_out" "$(cat "$TMP/docker.log")" | grep -cF -- "$FILE_TOKEN" || true)" "0" "env and file both set: file token unused"
check "$(printf '%s' "$env_out" | grep -cF -- "$TOKEN" || true)" "0" "env set: token absent from script output"

# a failure after login still logs out
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN= STUB_CREATE_EXIT=1; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "copy failure after login: refuses"
check "$(count '^login' "$TMP/docker.log")" "1" "copy failure after login: login happened"
check "$(count '^logout ghcr.io$' "$TMP/docker.log")" "1" "copy failure after login: docker logout ghcr.io"
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN= STUB_INSPECT_DIGEST="$OTHER_DIGEST"; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "read-back failure after login: refuses"
check "$(count '^logout ghcr.io$' "$TMP/docker.log")" "1" "read-back failure after login: docker logout ghcr.io"
check "$(printf '%s' "$LAST_OUT" | grep -cF -- "$FILE_TOKEN" || true)" "0" "read-back failure: token absent from script output"

# neither file nor variable: refuses before any registry call, names the path
rm -f "$KEYDIR/ghcr-token"
run CI_COMMIT_BRANCH=preview MV_PREVIEW_HEAD="$COMMIT" GHCR_PUSH_TOKEN=; status=$?
check "$([ "$status" -ne 0 ] && echo refused)" "refused" "no token file, no variable: refuses"
check "$(nothing_written)" "0" "no token file, no variable: nothing written"
check "$(wc -c < "$TMP/docker.log" | tr -d ' ')" "0" "no token file, no variable: no docker call at all"
check "$(printf '%s' "$LAST_OUT" | grep -cF -- "$KEYDIR/ghcr-token" || true)" "1" "no token file, no variable: error names the ghcr-token path"

exit "$fail"
