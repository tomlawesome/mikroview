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
export QH_STATE="$TMP/state"
export CONFIG="$TMP/config.toml"
mkdir -p "$QH_DIR" "$TMP/bin"

# The stub records its arguments: #1103 found `systemctl reload` exiting 3
# on a unit with no ExecReload=, which a stub that ignores its arguments
# can never catch, so the hold test below checks the exact call.
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${SYSTEMCTL_LOG:?}"
exit 0
STUB
chmod +x "$TMP/bin/systemctl"
export PATH="$TMP/bin:$PATH"
export SYSTEMCTL_LOG="$TMP/systemctl.log"

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
check "$(cat "$QH_STATE/concurrent.orig")" "4" "orig saved"
[ -f "$QH_DIR/applied" ] && echo "ok - applied written" || { echo "FAIL - applied written"; fail=1; }
check "$(cat "$SYSTEMCTL_LOG")" "kill --kill-whom=main --signal=HUP gitlab-runner" "hold sends SIGHUP to the runner's main process, not a reload job (#1103)"

# idempotent re-run while held
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "idempotent hold leaves concurrent=1"
check "$(cat "$QH_STATE/concurrent.orig")" "4" "idempotent leaves orig untouched"

# release restores the original value
rm -f "$QH_DIR/hold"
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "release restores concurrent=4"
[ -f "$QH_STATE/concurrent.orig" ] && { echo "FAIL - orig should be gone"; fail=1; } || echo "ok - orig removed"
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
rm -f "$QH_DIR/hold" "$QH_STATE/concurrent.orig" "$QH_DIR/applied"
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

# #1092: a reload that fails must not be read as a confirmed hold. The
# old script swallowed systemctl's exit status with `|| true` and wrote
# APPLIED unconditionally; this stub makes systemctl fail so the fix's
# exit-status check is the thing under test, not the stub's plumbing.
rm -f "$QH_DIR/hold" "$QH_STATE/concurrent.orig" "$QH_DIR/applied" "$QH_DIR/failed"
write_config
write_flag "$(($(date +%s) + 300))"
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
chmod +x "$TMP/bin/systemctl"
if "$SCRIPT" >/dev/null 2>"$TMP/err"; then
  echo "FAIL - a failed reload should not exit 0"; fail=1
else
  echo "ok - a failed reload exits nonzero"
fi
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "config is still set to 1 even though the reload failed"
[ -f "$QH_DIR/failed" ] && echo "ok - a failed marker was written" || { echo "FAIL - a failed marker was written"; fail=1; }
[ -f "$QH_DIR/applied" ] && { echo "FAIL - applied must not be written when the reload failed"; fail=1; } || echo "ok - applied was not written"
grep -q 'systemctl kill -s HUP gitlab-runner exited 1' "$QH_DIR/failed" \
  && echo "ok - the failed marker names the reason" \
  || { echo "FAIL - the failed marker names the reason"; fail=1; }

# A later run, once systemctl works again, retries rather than trusting
# the earlier failure: apply_hold's ORIG guard makes it safe to call
# again without re-saving the original value.
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$TMP/bin/systemctl"
"$SCRIPT" >/dev/null
[ -f "$QH_DIR/applied" ] && echo "ok - a retry after the fix writes applied" || { echo "FAIL - a retry after the fix writes applied"; fail=1; }
[ -f "$QH_DIR/failed" ] && { echo "FAIL - the failed marker should be cleared after a successful retry"; fail=1; } || echo "ok - the failed marker was cleared after a successful retry"
check "$(cat "$QH_STATE/concurrent.orig")" "4" "the retry did not clobber the saved original value"

# Releasing (the flag going away) clears a failed marker too, so a stale
# failure from an earlier hold cannot leak into the next one. Release the
# still-applied hold from above first, so ORIG is gone and the next hold
# below goes through a genuine apply_hold reload attempt rather than the
# ORIG-already-exists re-mark branch.
rm -f "$QH_DIR/hold"
"$SCRIPT" >/dev/null
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "released back to concurrent=4 before the next scenario"

write_flag "$(($(date +%s) + 300))"
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 1
STUB
chmod +x "$TMP/bin/systemctl"
"$SCRIPT" >/dev/null 2>/dev/null || true
[ -f "$QH_DIR/failed" ] && echo "ok - failed marker present before release" || { echo "FAIL - failed marker present before release"; fail=1; }
rm -f "$QH_DIR/hold"
"$SCRIPT" >/dev/null
[ -f "$QH_DIR/failed" ] && { echo "FAIL - release should clear the failed marker"; fail=1; } || echo "ok - release cleared the failed marker"

# #1094: a hold file can claim any expires= far in the future; the host
# must cap the hold at MAX_HOLD_S regardless of what the file says. This
# test can't fake the system clock, so it simulates elapsed time through
# the file's own started= field: a started= far enough in the past pushes
# started+MAX_HOLD_S below "now" even though expires claims 10 days out.
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$TMP/bin/systemctl"
rm -f "$QH_DIR/hold" "$QH_STATE/concurrent.orig" "$QH_DIR/applied" "$QH_DIR/failed"
write_config
long_started=$(( $(date +%s) - 4000 ))
far_expires=$(( $(date +%s) + 864000 ))
printf 'job=888\nurl=https://example.invalid/jobs/888\nstarted=%s\nexpires=%s\n' \
  "$long_started" "$far_expires" >"$QH_DIR/hold"
out=$("$SCRIPT")
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "a hold expiry beyond MAX_HOLD_S past started is treated as expired"
[ -f "$QH_DIR/hold" ] && { echo "FAIL - capped-expired hold should be deleted"; fail=1; } || echo "ok - capped-expired hold deleted"
echo "$out" | grep -q 'capped' && echo "ok - capped expiry logged" || { echo "FAIL - capped expiry not logged: $out"; fail=1; }

exit $fail
