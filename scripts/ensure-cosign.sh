#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Puts a pinned cosign on PATH and prints its path. The one place the pin
# lives, shared by the signing job (scripts/sign-release-digest.sh) and anything
# that verifies, so the two can never drift onto different releases (#1309).
#
# Why a downloaded binary rather than an image or an action. The GitLab
# signing job needs cosign and a shell in the same container: the official
# cosign image is distroless, so GitLab cannot run `script:` in it, and
# running cosign as a nested container cannot reach the registry
# credentials, which live in the job container's docker config while
# `docker run -v` mounts from the host. Passing --registry-password instead
# would put the token on a command line and in process listings. A pinned,
# checksum-verified binary is what is left, and it is what orbit does
# (scripts/ci/ensure-cosign.sh there).
#
# Stronger than this repository's other tool pins, which are bare versions
# (`govulncheck@v1.4.0`, `go-licenses@v2.0.1`): a version says which release
# was asked for, a checksum says which bytes arrived. Like those, this pin is
# invisible to Renovate -- see #1321, which is about exactly that gap.
#
# v3.1.3 was the current release on 2026-09-22 and the checksum below was
# verified against the downloaded artifact that day, not copied.
#
# Usage:
#   cosign="$(bash scripts/ensure-cosign.sh)"
#
# Output: the absolute path of a verified cosign binary on stdout; progress on
# stderr. A cosign already on PATH is used only at exactly the pinned
# version -- "some cosign" is not a pin. Anything else is downloaded,
# checksum-verified and cached under MV_COSIGN_DIR (default: .mv-cosign/ in
# the repository root).
set -Eeuo pipefail

readonly COSIGN_VERSION="3.1.3"
readonly COSIGN_SHA256="4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71"
readonly COSIGN_URL="https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-amd64"

repo_root="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"

fail() { printf 'ensure-cosign: %s\n' "$1" >&2; exit 1; }

verify() {
  printf '%s  %s\n' "$COSIGN_SHA256" "$1" | sha256sum -c - > /dev/null 2>&1
}

if command -v cosign > /dev/null 2>&1; then
  installed="$(cosign version --json 2> /dev/null | sed -n 's/.*"gitVersion": *"v\{0,1\}\([0-9.]*\)".*/\1/p' || :)"
  if [ "$installed" = "$COSIGN_VERSION" ]; then
    command -v cosign
    exit 0
  fi
  printf 'ensure-cosign: PATH has cosign %s, not the pinned %s; installing the pin.\n' \
    "${installed:-<unknown>}" "$COSIGN_VERSION" >&2
fi

cache_dir="${MV_COSIGN_DIR:-${repo_root}/.mv-cosign}"
binary="${cache_dir}/cosign-${COSIGN_VERSION}"

if [ -x "$binary" ] && verify "$binary"; then
  printf '%s\n' "$binary"
  exit 0
fi

mkdir -p "$cache_dir"
printf 'ensure-cosign: downloading cosign %s\n' "$COSIGN_VERSION" >&2
tmp="${binary}.part.$$"
trap 'rm -f "$tmp"' EXIT
curl -fsSL --retry 3 --retry-delay 5 --max-time 300 -o "$tmp" "$COSIGN_URL" \
  || fail "could not download $COSIGN_URL"

# Verified before it is ever made executable, so a wrong or tampered
# download is never a runnable file on this machine even briefly.
verify "$tmp" || fail "checksum mismatch for cosign ${COSIGN_VERSION} -- refusing to install
  expected $COSIGN_SHA256
  got      $(sha256sum "$tmp" | cut -d' ' -f1)"

chmod 0755 "$tmp"
mv "$tmp" "$binary"
trap - EXIT
printf '%s\n' "$binary"
