#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/ensure-cosign.sh. The case that matters is the third:
# a script that downloads a binary and runs it is only as good as its
# refusal, and a happy-path test passes just as well with the checksum
# check deleted.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/ensure-cosign.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

PINNED_SHA="$(sed -n 's/^readonly COSIGN_SHA256="\(.*\)"$/\1/p' "$SCRIPT")"
fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}

# 1. A fresh cache downloads, and what lands is the pinned bytes.
out="$(env PATH=/usr/bin:/bin MV_COSIGN_DIR="$TMP/cache" bash "$SCRIPT" 2>/dev/null)"
check "$?" "0" "a fresh cache installs cosign"
got="$(sha256sum "$out" 2>/dev/null | cut -d' ' -f1)"
check "$got" "$PINNED_SHA" "the installed binary is the pinned checksum"
check "$([ -x "$out" ] && echo yes || echo no)" "yes" "the installed binary is executable"

# 2. A second run reuses the cache rather than downloading again.
again="$(env PATH=/usr/bin:/bin MV_COSIGN_DIR="$TMP/cache" bash "$SCRIPT" 2>"$TMP/err2")"
check "$again" "$out" "a second run returns the cached path"
check "$(grep -c downloading "$TMP/err2")" "0" "a second run does not download"

# 3. The refusal. Same script, one character of the expected checksum
# changed, so the download is genuine and only the comparison differs.
sed 's/^readonly COSIGN_SHA256=".*"$/readonly COSIGN_SHA256="0000000000000000000000000000000000000000000000000000000000000000"/' \
  "$SCRIPT" > "$TMP/bad-sha.sh"
env PATH=/usr/bin:/bin MV_COSIGN_DIR="$TMP/bad" bash "$TMP/bad-sha.sh" >"$TMP/out3" 2>"$TMP/err3"
check "$?" "1" "a checksum mismatch refuses"
grep -q "checksum mismatch" "$TMP/err3" && echo "ok - the refusal says why" || { echo "FAIL - the refusal says why"; fail=1; }
check "$(ls "$TMP/bad" 2>/dev/null | grep -c '^cosign-[0-9]')" "0" "a mismatched download is not left installed"

# 4. A corrupted cache entry is not handed back as if it were the pin.
printf 'not cosign' > "$TMP/cache/cosign-$(sed -n 's/^readonly COSIGN_VERSION="\(.*\)"$/\1/p' "$SCRIPT")"
repaired="$(env PATH=/usr/bin:/bin MV_COSIGN_DIR="$TMP/cache" bash "$SCRIPT" 2>/dev/null)"
check "$(sha256sum "$repaired" 2>/dev/null | cut -d' ' -f1)" "$PINNED_SHA" "a corrupted cache entry is replaced, not trusted"

exit "$fail"
