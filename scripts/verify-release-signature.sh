#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The four-way classification .github/workflows/countersign.yml uses to
# decide whether GitLab's key-based signature on a released digest is
# present, expired, mismatched or ambiguous, before adding GitHub's
# keyless signature (#1309). Extracted from the workflow's inline shell
# (Q6-F1) so this branching -- which gates a release's second signature --
# has a test that would fail if a grep pattern or the ambiguity check
# broke, rather than only ever running for real on a manual,
# owner-triggered workflow_dispatch.
#
# Usage:
#   scripts/verify-release-signature.sh <image>@<digest> [pubkey]
#
# <pubkey> defaults to cosign.pub in the repository root.
#
# Exit 0 and "Verified exactly one ..." on stdout once exactly one
# key-based signature verifies. Exit 1 and an ::error:: line (absence,
# expiry, mismatch or ambiguous) otherwise -- never a silent fall-through.
#
# Inputs (environment):
#   MV_COSIGN   Optional; the cosign command to run. Default: cosign, i.e.
#               whatever sigstore/cosign-installer already put on PATH in
#               the countersign workflow. Overridable only so tests can
#               stub it.
set -Eeuo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"

target="${1:?usage: $0 <image>@<digest> [pubkey]}"
pubkey="${2:-$repo_root/cosign.pub}"
cosign_cmd="${MV_COSIGN:-cosign}"

errfile="$(mktemp)"
trap 'rm -f "$errfile"' EXIT

# stdout and stderr are captured separately and never merged: cosign
# writes its "Verification for ..." banner and checks-performed list to
# *stderr* even on success, so a 2>&1 here would prepend prose to the
# JSON and make the jq below fail on the path that is supposed to succeed.
set +e
output="$("$cosign_cmd" verify --key "$pubkey" --output json "$target" 2>"$errfile")"
status=$?
set -e
errors="$(cat "$errfile")"

if [ "$status" -ne 0 ]; then
  if grep -qiE 'no matching signatures' <<< "$errors"; then
    echo "::error::no key-based signature found for ${target} -- refusing (absence)"
  elif grep -qiE 'MANIFEST_UNKNOWN|manifest unknown|NAME_UNKNOWN|404 Not Found' <<< "$errors"; then
    echo "::error::${target} does not resolve in the registry (expired, pruned, or never pushed) -- refusing (expiry)"
  else
    echo "::error::the key-based signature on ${target} does not verify against ${pubkey} -- refusing (mismatch)"
  fi
  echo "$errors" >&2
  exit 1
fi

# A refusal is also the right answer to output that is not the JSON array
# this expects -- never a fall-through to signing. python3 rather than jq:
# this runs both in the countersign workflow (ubuntu-latest, no jq
# guaranteed beyond what that image happens to carry) and in gate:scripts'
# image, where python3 is already relied on by sibling *.test.sh files but
# jq is not installed at all.
if ! count="$(python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
except ValueError:
    sys.exit(1)
if not isinstance(data, list):
    sys.exit(1)
print(len(data))
' <<< "$output")"; then
  echo "::error::could not read cosign's verification output for ${target} -- refusing"
  echo "$output" >&2
  echo "$errors" >&2
  exit 1
fi
if [ "$count" -ne 1 ]; then
  echo "::error::${target} resolved to ${count} verified key-based signatures, expected exactly 1 -- refusing (ambiguous)"
  echo "$output" >&2
  exit 1
fi

echo "Verified exactly one key-based signature on ${target} against ${pubkey}."
