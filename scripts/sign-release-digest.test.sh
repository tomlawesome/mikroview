#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/sign-release-digest.sh against stubbed docker and
# cosign commands -- never the network or a real registry. The refusals
# matter more than the happy path here: this script is the one place a
# multi-arch registry response, a missing key, a malformed digest or
# evidence that names some other image must stop a signature from being
# minted rather than guessed at.
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

REPO="registry.example.test/ai/mikroview"
COMMIT="$(printf '1%.0s' $(seq 1 40))"
GOOD_DIGEST="sha256:$(printf 'a%.0s' $(seq 1 64))"
AMD64_DIGEST="sha256:$(printf 'b%.0s' $(seq 1 64))"
ARM64_DIGEST="sha256:$(printf 'c%.0s' $(seq 1 64))"
ATTEST_DIGEST="sha256:$(printf 'd%.0s' $(seq 1 64))"
OTHER_DIGEST="sha256:$(printf 'e%.0s' $(seq 1 64))"

# A signing runner mount with both files, for the tests that are not about
# a missing key.
KEYDIR="$TMP/keydir"
mkdir -p "$KEYDIR"
printf 'not a real key -- test fixture\n' > "$KEYDIR/cosign.key"
printf 'hunter2' > "$KEYDIR/password"

# publish:image's evidence and security:trivy-image's SBOM, as fixtures.
evidence() {  # evidence <file> <reference> <digest>
  cat > "$1" <<JSON
{
  "imageReference": "$2",
  "imageDigest": "$3",
  "commit": "${COMMIT}",
  "ref": "preview",
  "pipelineId": 42,
  "pipelineUrl": "https://gitlab.example.test/ai/mikroview/-/pipelines/42",
  "builtAt": "2026-11-15T00:00:00Z"
}
JSON
}
EVIDENCE="$TMP/evidence.json"
evidence "$EVIDENCE" "${REPO}@${GOOD_DIGEST}" "$GOOD_DIGEST"
SBOM="$TMP/image.spdx.json"
printf '{"spdxVersion":"SPDX-2.3","name":"test fixture"}\n' > "$SBOM"

# The docker stub: one script standing in for `docker buildx imagetools
# inspect` and `docker login`, each controlled by an env var so one file
# can drive every case below.
DOCKER_STUB="$TMP/docker"
cat > "$DOCKER_STUB" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  buildx)
    printf '%s\n' "$*" >> "${STUB_INSPECT_CALLS:-/dev/null}"
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

# The cosign stub: appends each call's arguments and the password cosign
# would have read from the environment, so the tests can assert on both
# the signature and the attestation without ever running the real binary.
# `verify` and `verify-attestation` (ruling v2's already-signed checks)
# default to exit 1, a digest not yet signed, so every case above case 16
# models a fresh digest; STUB_VERIFY_* and STUB_VERIFY_ATT_* set them.
COSIGN_STUB="$TMP/cosign"
cat > "$COSIGN_STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${STUB_COSIGN_CALL:-/dev/null}"
case "$1" in
  verify)
    printf '%s' "${STUB_VERIFY_OUTPUT:-}"
    exit "${STUB_VERIFY_EXIT:-1}"
    ;;
  verify-attestation)
    printf '%s' "${STUB_VERIFY_ATT_OUTPUT:-}"
    exit "${STUB_VERIFY_ATT_EXIT:-1}"
    ;;
esac
printf '%s' "${COSIGN_PASSWORD:-}" > "${STUB_COSIGN_PASSWORD_SEEN:-/dev/null}"
exit "${STUB_COSIGN_EXIT:-0}"
STUB
chmod +x "$COSIGN_STUB"

single_platform_output() {  # single_platform_output [digest]
  printf 'Name:      %s@%s\n' "$REPO" "${1:-$GOOD_DIGEST}"
  printf 'MediaType: application/vnd.oci.image.index.v1+json\n'
  printf 'Digest:    %s\n\n' "${1:-$GOOD_DIGEST}"
  printf 'Manifests:\n'
  printf '  Name:      %s@%s\n' "$REPO" "$AMD64_DIGEST"
  printf '  Platform:  linux/amd64\n\n'
  printf '  Name:      %s@%s\n' "$REPO" "$ATTEST_DIGEST"
  printf '  Platform:  unknown/unknown\n'
}

multi_platform_output() {
  single_platform_output
  printf '\n  Name:      %s@%s\n' "$REPO" "$ARM64_DIGEST"
  printf '  Platform:  linux/arm64\n'
}

run() {  # run [<tag>] -- <name>=<value>...
  local args=()
  while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do args+=("$1"); shift; done
  shift
  env -i PATH="/usr/bin:/bin" \
    CI_REGISTRY=registry.example.test \
    CI_REGISTRY_USER=gitlab-ci-token \
    CI_REGISTRY_PASSWORD=dummy-job-token \
    CI_REGISTRY_IMAGE="$REPO" \
    CI_COMMIT_SHA="$COMMIT" \
    MV_DOCKER="$DOCKER_STUB" \
    MV_COSIGN="$COSIGN_STUB" \
    MV_EVIDENCE="$EVIDENCE" \
    MV_SBOM="$SBOM" \
    "$@" bash "$SCRIPT" "${args[@]}"
}

# 1. Happy path: the recorded digest resolves to a single real platform
# (plus an attestation manifest, which is not a second image); the job
# token logs in to the GitLab registry; cosign signs that digest with the
# mounted key and the runner's own password, then attests the SBOM to it.
COSIGN_CALL="$TMP/cosign-call-1"
COSIGN_PASSWORD_SEEN="$TMP/cosign-password-1"
LOGIN_CALLS="$TMP/login-1"
INSPECT_CALLS="$TMP/inspect-1"
out="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_INSPECT_CALLS="$INSPECT_CALLS" \
  STUB_LOGIN_CALLS="$LOGIN_CALLS" \
  STUB_COSIGN_CALL="$COSIGN_CALL" \
  STUB_COSIGN_PASSWORD_SEEN="$COSIGN_PASSWORD_SEEN" \
  2>"$TMP/err1")"
rc=$?
check "$rc" "0" "happy path exits 0"
check "$(cat "$COSIGN_PASSWORD_SEEN" 2>/dev/null)" "hunter2" "cosign saw the runner's key password"
grep -q "login registry.example.test -u gitlab-ci-token --password-stdin" "$LOGIN_CALLS" 2>/dev/null &&
  echo "ok - the job token logs in to the GitLab registry on stdin" || { echo "FAIL - the job token logs in to the GitLab registry on stdin"; fail=1; }
grep -q "imagetools inspect ${REPO}@${GOOD_DIGEST}" "$INSPECT_CALLS" 2>/dev/null &&
  echo "ok - the recorded digest is what gets inspected" || { echo "FAIL - the recorded digest is what gets inspected"; fail=1; }
grep -q -- "^sign --key ${KEYDIR}/cosign.key --yes ${REPO}@${GOOD_DIGEST}$" "$COSIGN_CALL" 2>/dev/null &&
  echo "ok - cosign signed the recorded digest with the mounted key" || { echo "FAIL - cosign signed the recorded digest with the mounted key: $(cat "$COSIGN_CALL" 2>/dev/null)"; fail=1; }
grep -q -- "^attest --key ${KEYDIR}/cosign.key --yes --type spdxjson --predicate ${SBOM} ${REPO}@${GOOD_DIGEST}$" "$COSIGN_CALL" 2>/dev/null &&
  echo "ok - cosign attested the SPDX SBOM to the same digest" || { echo "FAIL - cosign attested the SPDX SBOM to the same digest: $(cat "$COSIGN_CALL" 2>/dev/null)"; fail=1; }
# Ruling v2: a verify call may now come first, so compare the first sign
# and first attest lines rather than the first call.
check "$(grep -nE '^(sign|attest) ' "$COSIGN_CALL" | head -n 1 | cut -d: -f2 | cut -d' ' -f1)" "sign" "the signature comes before the attestation"
grep -q "$GOOD_DIGEST" <<< "$out" && echo "ok - the signed digest is reported" || { echo "FAIL - the signed digest is reported"; fail=1; }

# 2. Missing key: refuses, naming the exact path expected.
out2="$(run -- MV_SIGNING_DIR="$TMP/no-key-dir" 2>&1)"
rc=$?
check "$rc" "1" "a missing key refuses"
grep -q "$TMP/no-key-dir/cosign.key" <<< "$out2" && echo "ok - the refusal names the expected key path" || { echo "FAIL - the refusal names the expected key path"; fail=1; }

# 3. Missing password: key present, password file absent.
NOPASSDIR="$TMP/no-pass-dir"
mkdir -p "$NOPASSDIR"
printf 'not a real key -- test fixture\n' > "$NOPASSDIR/cosign.key"
out3="$(run -- MV_SIGNING_DIR="$NOPASSDIR" 2>&1)"
rc=$?
check "$rc" "1" "a missing password file refuses"
grep -q "$NOPASSDIR/password" <<< "$out3" && echo "ok - the refusal names the expected password path" || { echo "FAIL - the refusal names the expected password path"; fail=1; }

# 4. Unresolvable digest: imagetools inspect itself fails -- nothing to
# sign, and the script must say so rather than push on with an empty or
# partial digest.
COSIGN_CALL4="$TMP/cosign-call-4"
out4="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_EXIT=1 STUB_INSPECT_OUTPUT="unauthorized: authentication required" \
  STUB_COSIGN_CALL="$COSIGN_CALL4" \
  2>&1)"
rc=$?
check "$rc" "1" "an unresolvable digest refuses"
grep -q "could not inspect" <<< "$out4" && echo "ok - the refusal says the inspect failed" || { echo "FAIL - the refusal says the inspect failed"; fail=1; }
check "$([ -e "$COSIGN_CALL4" ] && echo called || echo not-called)" "not-called" "nothing is signed when the inspect fails"

# 5. Ambiguous digest: a genuine second platform manifest (not an
# attestation) is refused rather than silently signing one of them.
out5="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(multi_platform_output)" \
  2>&1)"
rc=$?
check "$rc" "1" "an ambiguous (multi-arch) digest refuses"
grep -q "2 platform manifests" <<< "$out5" && echo "ok - the refusal names the platform count" || { echo "FAIL - the refusal names the platform count"; fail=1; }

# 6. Malformed digest, on the exercise path (a named tag, so nothing but
# the registry says what the digest is): imagetools inspect succeeds with
# exactly one real platform but a value that is not a manifest digest.
out6="$(run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(printf 'Name: x\nDigest: not-a-real-digest\n\nManifests:\n  Platform: linux/amd64\n')" \
  2>&1)"
rc=$?
check "$rc" "1" "a malformed digest refuses"
grep -q "is not sha256:<64 hex>" <<< "$out6" && echo "ok - the refusal says the digest shape is wrong" || { echo "FAIL - the refusal says the digest shape is wrong"; fail=1; }

# 7. No evidence at all (publish:image did not run): refuse before the
# registry is even asked, never fall back to signing a tag.
COSIGN_CALL7="$TMP/cosign-call-7"
LOGIN_CALLS7="$TMP/login-7"
out7="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  MV_EVIDENCE="$TMP/no-such-evidence.json" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_LOGIN_CALLS="$LOGIN_CALLS7" \
  STUB_COSIGN_CALL="$COSIGN_CALL7" \
  2>&1)"
rc=$?
check "$rc" "1" "missing evidence refuses"
grep -q "publish:image must run and pass first" <<< "$out7" && echo "ok - the refusal names publish:image" || { echo "FAIL - the refusal names publish:image: $out7"; fail=1; }
check "$([ -e "$COSIGN_CALL7" ] || [ -e "$LOGIN_CALLS7" ] && echo touched || echo untouched)" "untouched" "missing evidence touches neither the registry nor cosign"

# 8. Evidence naming another repository: refused, never signed.
EVIDENCE8="$TMP/evidence-other-repo.json"
evidence "$EVIDENCE8" "registry.example.test/someone/else@${GOOD_DIGEST}" "$GOOD_DIGEST"
COSIGN_CALL8="$TMP/cosign-call-8"
out8="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  MV_EVIDENCE="$EVIDENCE8" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$COSIGN_CALL8" \
  2>&1)"
rc=$?
check "$rc" "1" "evidence for another repository refuses"
check "$([ -e "$COSIGN_CALL8" ] && echo called || echo not-called)" "not-called" "nothing is signed for another repository's evidence"

# 9. Evidence from another commit's pipeline: refused.
COSIGN_CALL9="$TMP/cosign-call-9"
out9="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  CI_COMMIT_SHA="$(printf '2%.0s' $(seq 1 40))" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$COSIGN_CALL9" \
  2>&1)"
rc=$?
check "$rc" "1" "evidence from another commit refuses"
grep -q "records commit .*this pipeline is" <<< "$out9" && echo "ok - the refusal says the evidence is another pipeline's commit" || { echo "FAIL - the refusal says the evidence is another pipeline's commit: $out9"; fail=1; }
check "$([ -e "$COSIGN_CALL9" ] && echo called || echo not-called)" "not-called" "nothing is signed for another commit's evidence"

# 10. No SBOM: refuse before signing, rather than leave a signed digest
# with no attestation beside it.
COSIGN_CALL10="$TMP/cosign-call-10"
out10="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  MV_SBOM="$TMP/no-such-sbom.json" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$COSIGN_CALL10" \
  2>&1)"
rc=$?
check "$rc" "1" "a missing SBOM refuses"
grep -q "security:trivy-image must run and pass first" <<< "$out10" && echo "ok - the refusal names security:trivy-image" || { echo "FAIL - the refusal names security:trivy-image: $out10"; fail=1; }
check "$([ -e "$COSIGN_CALL10" ] && echo called || echo not-called)" "not-called" "nothing is signed without the SBOM"

# 11. The registry answers with a digest other than the recorded one.
out11="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output "$OTHER_DIGEST")" \
  2>&1)"
rc=$?
check "$rc" "1" "a digest other than the recorded one refuses"
grep -q "not the recorded $GOOD_DIGEST" <<< "$out11" && echo "ok - the refusal names the recorded digest" || { echo "FAIL - the refusal names the recorded digest: $out11"; fail=1; }

# 12. The exercise path (#1309): a named tag resolves in the GitLab
# registry and, only because it is the digest this pipeline's evidence
# records, is signed; there is no SBOM to attest.
COSIGN_CALL12="$TMP/cosign-call-12"
INSPECT_CALLS12="$TMP/inspect-12"
out12="$(run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_INSPECT_CALLS="$INSPECT_CALLS12" \
  STUB_COSIGN_CALL="$COSIGN_CALL12" \
  2>&1)"
rc=$?
check "$rc" "0" "the exercise path exits 0"
grep -q "imagetools inspect ${REPO}:v9.9.9" "$INSPECT_CALLS12" 2>/dev/null &&
  echo "ok - the exercise path resolves the tag in the GitLab registry" || { echo "FAIL - the exercise path resolves the tag in the GitLab registry"; fail=1; }
grep -q -- "^sign --key ${KEYDIR}/cosign.key --yes ${REPO}@${GOOD_DIGEST}$" "$COSIGN_CALL12" 2>/dev/null &&
  echo "ok - the exercise path signs the tag's digest" || { echo "FAIL - the exercise path signs the tag's digest"; fail=1; }
check "$(grep -c '^attest' "$COSIGN_CALL12" 2>/dev/null)" "0" "the exercise path attests nothing"

# 13. Exercise path, no evidence: any merge-request job can push a tag into
# the registry, so a tag is never signed on its own say-so.
COSIGN_CALL13="$TMP/cosign-call-13"
out13="$(run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  MV_EVIDENCE="$TMP/no-such-evidence.json" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$COSIGN_CALL13" \
  2>&1)"
rc=$?
check "$rc" "1" "the exercise path without evidence refuses"
grep -q "publish:image must run and pass first" <<< "$out13" && echo "ok - the exercise refusal names publish:image" || { echo "FAIL - the exercise refusal names publish:image: $out13"; fail=1; }
check "$([ -e "$COSIGN_CALL13" ] && echo called || echo not-called)" "not-called" "the exercise path signs nothing without evidence"

# 14. Exercise path whose tag now names a digest this pipeline did not test.
COSIGN_CALL14="$TMP/cosign-call-14"
out14="$(run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output "$OTHER_DIGEST")" \
  STUB_COSIGN_CALL="$COSIGN_CALL14" \
  2>&1)"
rc=$?
check "$rc" "1" "an exercise tag whose digest differs from the evidence refuses"
grep -q "not the recorded $GOOD_DIGEST" <<< "$out14" && echo "ok - the exercise refusal names the recorded digest" || { echo "FAIL - the exercise refusal names the recorded digest: $out14"; fail=1; }
check "$([ -e "$COSIGN_CALL14" ] && echo called || echo not-called)" "not-called" "the exercise path signs nothing for an untested digest"

# 15. The GitLab job's predefined registry variables must be set.
for var in CI_REGISTRY CI_REGISTRY_USER CI_REGISTRY_PASSWORD; do
  cc="$TMP/cosign-call-15-$var"
  o="$(run -- MV_SIGNING_DIR="$KEYDIR" "$var=" STUB_COSIGN_CALL="$cc" 2>&1)"
  rc=$?
  check "$rc" "1" "$var unset refuses"
  grep -q "${var} is not set" <<< "$o" && echo "ok - the refusal names $var" || { echo "FAIL - the refusal names $var: $o"; fail=1; }
  check "$([ -e "$cc" ] && echo called || echo not-called)" "not-called" "nothing is signed with $var unset"
done
cc="$TMP/cosign-call-15-image"
o="$(run -- MV_SIGNING_DIR="$KEYDIR" CI_REGISTRY_IMAGE= STUB_COSIGN_CALL="$cc" 2>&1)"
rc=$?
check "$rc" "1" "no repository (CI_REGISTRY_IMAGE unset) refuses"
grep -q "CI_REGISTRY_IMAGE (or MV_IMAGE) is not set" <<< "$o" && echo "ok - the refusal names CI_REGISTRY_IMAGE" || { echo "FAIL - the refusal names CI_REGISTRY_IMAGE: $o"; fail=1; }
check "$([ -e "$cc" ] && echo called || echo not-called)" "not-called" "nothing is signed with no repository"

# ---------------------------------------------------------------------------
# #1472 step 3, ruling v2: a retried job is a no-op, not a second signature.
# Before `cosign sign`, a `cosign verify --key` that already verifies the
# digest skips the signature; before `cosign attest`, a verifying
# `verify-attestation --type spdxjson` skips the attestation.
# ---------------------------------------------------------------------------
SIGNED_JSON="[{\"critical\":{\"identity\":{\"docker-reference\":\"${REPO}\"},\"image\":{\"docker-manifest-digest\":\"${GOOD_DIGEST}\"},\"type\":\"cosign container image signature\"},\"optional\":null}]"
ATTESTED_LINE='{"payloadType":"application/vnd.in-toto+json","payload":"e30=","signatures":[{"keyid":"","sig":"c2ln"}]}'
count_calls() { grep -c "^$1 " "$2" 2>/dev/null; true; }

# 16. Already signed: verify --key exits 0 with a signature array for
# <repo>@<digest>, so no second signature is minted; the attestation still
# proceeds because verify-attestation does not verify.
CC16="$TMP/cosign-call-16"
out16="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$CC16" \
  STUB_VERIFY_EXIT=0 STUB_VERIFY_OUTPUT="$SIGNED_JSON" \
  2>"$TMP/err16")"
rc=$?
check "$rc" "0" "already signed: exits 0"
grep -qE -- "^verify .*--key .*${REPO}@${GOOD_DIGEST}( |$)" "$CC16" 2>/dev/null &&
  echo "ok - already signed: verify --key is asked about the recorded digest" || { echo "FAIL - already signed: verify --key is asked about the recorded digest: $(cat "$CC16" 2>/dev/null)"; fail=1; }
check "$(count_calls sign "$CC16")" "0" "already signed: no sign call"
grep -q "already signed" <<< "$out16" && echo "ok - already signed: stdout says already signed" || { echo "FAIL - already signed: stdout says already signed: $out16"; fail=1; }
check "$(count_calls attest "$CC16")" "1" "already signed: the attestation still runs once"

# 17. Already attested: verify-attestation --type spdxjson exits 0, so no
# second attestation; the signature still proceeds.
CC17="$TMP/cosign-call-17"
out17="$(run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$CC17" \
  STUB_VERIFY_ATT_EXIT=0 STUB_VERIFY_ATT_OUTPUT="$ATTESTED_LINE" \
  2>"$TMP/err17")"
rc=$?
check "$rc" "0" "already attested: exits 0"
grep -qE -- "^verify-attestation .*--type spdxjson.*${REPO}@${GOOD_DIGEST}( |$)" "$CC17" 2>/dev/null &&
  echo "ok - already attested: verify-attestation --type spdxjson is asked about the recorded digest" || { echo "FAIL - already attested: verify-attestation --type spdxjson is asked about the recorded digest: $(cat "$CC17" 2>/dev/null)"; fail=1; }
check "$(count_calls attest "$CC17")" "0" "already attested: no attest call"
grep -q "already attested" <<< "$out17" && echo "ok - already attested: stdout says already attested" || { echo "FAIL - already attested: stdout says already attested: $out17"; fail=1; }
check "$(count_calls sign "$CC17")" "1" "already attested: the signature still runs once"

# 18. Neither yet: both verifies exit 1, so sign then attest, once each.
CC18="$TMP/cosign-call-18"
run -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$CC18" \
  >/dev/null 2>"$TMP/err18"
rc=$?
check "$rc" "0" "fresh digest: exits 0"
check "$(count_calls sign "$CC18")" "1" "fresh digest: sign called once"
check "$(count_calls attest "$CC18")" "1" "fresh digest: attest called once"
check "$(grep -nE '^(sign|attest) ' "$CC18" | head -n 1 | cut -d: -f2 | cut -d' ' -f1)" "sign" "fresh digest: sign before attest"

# 19. Exercise path (tag argument): the signature check is consulted; a
# fresh digest is signed once, an already-signed one not at all; never an
# attestation.
CC19="$TMP/cosign-call-19"
run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$CC19" \
  >/dev/null 2>"$TMP/err19"
rc=$?
check "$rc" "0" "exercise path, fresh: exits 0"
check "$([ "$(count_calls verify "$CC19")" -ge 1 ] && echo yes || echo no)" "yes" "exercise path, fresh: verify is checked"
check "$(count_calls sign "$CC19")" "1" "exercise path, fresh: sign called once"
check "$(count_calls attest "$CC19")" "0" "exercise path, fresh: no attest"
CC19b="$TMP/cosign-call-19b"
out19b="$(run v9.9.9 -- \
  MV_SIGNING_DIR="$KEYDIR" \
  STUB_INSPECT_OUTPUT="$(single_platform_output)" \
  STUB_COSIGN_CALL="$CC19b" \
  STUB_VERIFY_EXIT=0 STUB_VERIFY_OUTPUT="$SIGNED_JSON" \
  2>"$TMP/err19b")"
rc=$?
check "$rc" "0" "exercise path, already signed: exits 0"
check "$(count_calls sign "$CC19b")" "0" "exercise path, already signed: no sign call"
check "$(count_calls attest "$CC19b")" "0" "exercise path, already signed: no attest"
grep -q "already signed" <<< "$out19b" && echo "ok - exercise path, already signed: stdout says already signed" || { echo "FAIL - exercise path, already signed: stdout says already signed: $out19b"; fail=1; }

# 20. A verify that exits 0 but prints no JSON array is not proof of a
# signature: sign proceeds, and the run does not fail on it.
for case in empty text; do
  if [ "$case" = empty ]; then o=""; else o="Verification for ${REPO}@${GOOD_DIGEST} --"; fi
  cc="$TMP/cosign-call-20-$case"
  run -- \
    MV_SIGNING_DIR="$KEYDIR" \
    STUB_INSPECT_OUTPUT="$(single_platform_output)" \
    STUB_COSIGN_CALL="$cc" \
    STUB_VERIFY_EXIT=0 STUB_VERIFY_OUTPUT="$o" \
    >/dev/null 2>"$TMP/err20-$case"
  rc=$?
  check "$rc" "0" "verify exit 0 with $case output: exits 0"
  check "$(count_calls sign "$cc")" "1" "verify exit 0 with $case output: still signs"
done

exit "$fail"
