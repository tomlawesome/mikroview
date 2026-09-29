#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/verify-release-signature.sh's four-way classification
# (absence / expiry / mismatch / ambiguous) plus the happy path, against a
# stubbed cosign -- never a real registry or key. Q6-F1: this logic used
# to live only as inline shell in .github/workflows/countersign.yml, run
# for real only on a manual, owner-triggered workflow_dispatch, so a
# broken grep pattern or a flipped comparison would have shipped unnoticed.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/verify-release-signature.sh"
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

TARGET="ghcr.io/tomlawesome/mikroview@sha256:$(printf 'a%.0s' $(seq 1 64))"

# The cosign stub: prints whatever the test asks for on stdout/stderr and
# exits with the requested status, standing in for `cosign verify --key
# ... --output json`.
COSIGN_STUB="$TMP/cosign"
cat > "$COSIGN_STUB" <<'STUB'
#!/usr/bin/env bash
printf '%s' "${STUB_STDOUT:-}"
printf '%s' "${STUB_STDERR:-}" >&2
exit "${STUB_EXIT:-0}"
STUB
chmod +x "$COSIGN_STUB"

run() {  # run <name>=<value>... -- runs the script against $TARGET
  env -i PATH="/usr/bin:/bin" MV_COSIGN="$COSIGN_STUB" "$@" bash "$SCRIPT" "$TARGET" "$TMP/cosign.pub"
}

# 1. Happy path: cosign exits 0 with exactly one verified signature.
out1="$(run STUB_STDOUT='[{"critical":{}}]' 2>&1)"; rc=$?
check "$rc" "0" "happy path exits 0"
grep -q "Verified exactly one" <<< "$out1" && echo "ok - happy path reports success" || { echo "FAIL - happy path reports success: $out1"; fail=1; }

# 2. Absence: cosign refuses, stderr says no matching signatures.
out2="$(run STUB_EXIT=1 STUB_STDERR='Error: no matching signatures: crypto/rsa: verification error' 2>&1)"; rc=$?
check "$rc" "1" "absence refuses"
grep -q "refusing (absence)" <<< "$out2" && echo "ok - absence classified correctly" || { echo "FAIL - absence classified correctly: $out2"; fail=1; }

# 3. Expiry: cosign refuses, stderr says the manifest is unknown (digest
# does not resolve in the registry -- expired, pruned, or never pushed).
out3="$(run STUB_EXIT=1 STUB_STDERR='Error: MANIFEST_UNKNOWN: manifest unknown' 2>&1)"; rc=$?
check "$rc" "1" "expiry refuses"
grep -q "refusing (expiry)" <<< "$out3" && echo "ok - expiry classified correctly" || { echo "FAIL - expiry classified correctly: $out3"; fail=1; }

# 3b. Expiry via the 404 form some registries return instead.
out3b="$(run STUB_EXIT=1 STUB_STDERR='Error: GET https://ghcr.io/...: 404 Not Found' 2>&1)"; rc=$?
check "$rc" "1" "expiry (404 form) refuses"
grep -q "refusing (expiry)" <<< "$out3b" && echo "ok - expiry (404 form) classified correctly" || { echo "FAIL - expiry (404 form) classified correctly: $out3b"; fail=1; }

# 4. Mismatch: cosign refuses for any other reason -- a signature exists
# but does not verify against the pinned public key.
out4="$(run STUB_EXIT=1 STUB_STDERR='Error: signature does not match the provided public key' 2>&1)"; rc=$?
check "$rc" "1" "mismatch refuses"
grep -q "refusing (mismatch)" <<< "$out4" && echo "ok - mismatch classified correctly" || { echo "FAIL - mismatch classified correctly: $out4"; fail=1; }

# 5. Ambiguous: cosign verifies successfully but returns more than one
# matching signature -- never fall through to signing.
out5="$(run STUB_STDOUT='[{"critical":{}},{"critical":{}}]' 2>&1)"; rc=$?
check "$rc" "1" "ambiguous (2 signatures) refuses"
grep -q "2 verified key-based signatures" <<< "$out5" && echo "ok - ambiguous count is named" || { echo "FAIL - ambiguous count is named: $out5"; fail=1; }

# 6. Malformed output: cosign exits 0 but the output is not a JSON array
# at all -- refuse rather than guess.
out6="$(run STUB_STDOUT='not json' 2>&1)"; rc=$?
check "$rc" "1" "malformed output refuses"
grep -q "could not read cosign's verification output" <<< "$out6" && echo "ok - malformed output is refused" || { echo "FAIL - malformed output is refused: $out6"; fail=1; }

exit "$fail"
