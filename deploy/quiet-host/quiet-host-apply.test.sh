#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises quiet-host-apply.sh against a temp dir and a fake config, with
# a stubbed systemctl, so the test never touches the real host.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/quiet-host-apply.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export QH_DIR="$TMP/qh"
export CONFIG="$TMP/config.toml"
mkdir -p "$QH_DIR" "$TMP/bin"

cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$TMP/bin/systemctl"
export PATH="$TMP/bin:$PATH"

fail=0
check() {
  if [ "$1" = "$2" ]; then
    echo "ok - $3"
  else
    echo "FAIL - $3: expected [$2] got [$1]"
    fail=1
  fi
}

write_config() {
  printf 'concurrent = 4\ncheck_interval = 3\n[[runners]]\n  name = "big"\n' >"$CONFIG"
}

write_flag() {
  printf 'job=999\nurl=https://example.invalid/jobs/999\nstarted=%s\nexpires=%s\n' \
    "$(date +%s)" "$1" >"$QH_DIR/hold"
}

# hold
write_config
write_flag "$(($(date +%s) + 300))"
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "hold sets concurrent=1"
check "$(cat "$QH_DIR/concurrent.orig")" "4" "orig saved"
[ -f "$QH_DIR/applied" ] && echo "ok - applied written" || { echo "FAIL - applied written"; fail=1; }

# idempotent re-run while held
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "idempotent hold leaves concurrent=1"
check "$(cat "$QH_DIR/concurrent.orig")" "4" "idempotent leaves orig untouched"

# release restores the original value
rm -f "$QH_DIR/hold"
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "release restores concurrent=4"
[ -f "$QH_DIR/concurrent.orig" ] && { echo "FAIL - orig should be gone"; fail=1; } || echo "ok - orig removed"
[ -f "$QH_DIR/applied" ] && { echo "FAIL - applied should be gone"; fail=1; } || echo "ok - applied removed"

# expired flag releases and is deleted
write_config
write_flag "$(($(date +%s) + 300))"
"$SCRIPT" >/dev/null
write_flag "$(($(date +%s) - 10))"
out=$("$SCRIPT")
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "expired flag restores concurrent=4"
[ -f "$QH_DIR/hold" ] && { echo "FAIL - expired hold should be deleted"; fail=1; } || echo "ok - expired hold deleted"
echo "$out" | grep -q '^EXPIRED ' && echo "ok - EXPIRED logged" || { echo "FAIL - EXPIRED not logged: $out"; fail=1; }

# missing concurrent line is refused
printf 'check_interval = 3\n' >"$CONFIG"
rm -f "$QH_DIR/hold" "$QH_DIR/concurrent.orig" "$QH_DIR/applied"
write_flag "$(($(date +%s) + 300))"
if "$SCRIPT" >/dev/null 2>"$TMP/err"; then
  echo "FAIL - missing concurrent line should refuse"; fail=1
else
  echo "ok - missing concurrent line refused"
fi

# duplicate concurrent line is refused
printf 'concurrent = 4\nconcurrent = 5\n' >"$CONFIG"
if "$SCRIPT" >/dev/null 2>"$TMP/err"; then
  echo "FAIL - duplicate concurrent line should refuse"; fail=1
else
  echo "ok - duplicate concurrent line refused"
fi

exit $fail
