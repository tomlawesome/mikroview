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

# #1094 follow-up: started= is in the same unprivileged file as expires=,
# so a started= in the future would have pushed the ceiling out with it.
# The cap must come from the host's own clock: a future started= is
# clamped to now, so the capped expiry can never exceed now + MAX_HOLD_S.
rm -f "$QH_DIR/hold" "$QH_STATE/concurrent.orig" "$QH_STATE/hold.since" "$QH_DIR/applied" "$QH_DIR/failed"
write_config
t0=$(date +%s)
printf 'job=777\nurl=https://example.invalid/jobs/777\nstarted=%s\nexpires=%s\n' \
  "$((t0 + 864000))" "$((t0 + 864000))" >"$QH_DIR/hold"
out=$("$SCRIPT")
capped=$(echo "$out" | sed -n 's/.*capping to \([0-9]*\).*/\1/p')
if [ -n "$capped" ] && [ "$capped" -le $(( $(date +%s) + 3600 )) ]; then
  echo "ok - a future started= is clamped: capped expiry $capped is within an hour of now"
else
  echo "FAIL - a future started= is clamped: got [$out]"; fail=1
fi
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "the clamped hold is still applied for its hour"
[ -f "$QH_STATE/hold.since" ] && echo "ok - the host recorded when the hold began" || { echo "FAIL - the host recorded when the hold began"; fail=1; }

# The file can be rewritten while a hold is active. The ceiling counts
# from when the host first applied the hold (hold.since, root-owned),
# not from whatever the file says now: a hold begun over an hour ago is
# expired however fresh its started= looks.
printf '%s\n' "$((t0 - 4000))" >"$QH_STATE/hold.since"
printf 'job=777\nurl=https://example.invalid/jobs/777\nstarted=%s\nexpires=%s\n' \
  "$t0" "$((t0 + 300))" >"$QH_DIR/hold"
out=$("$SCRIPT")
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 4" "a rewritten hold file cannot extend a hold past an hour from when the host applied it"
[ -f "$QH_DIR/hold" ] && { echo "FAIL - the over-age hold should be deleted"; fail=1; } || echo "ok - the over-age hold was deleted"
[ -f "$QH_STATE/hold.since" ] && { echo "FAIL - release should clear hold.since"; fail=1; } || echo "ok - release cleared hold.since"

# ai/agent-infra #24: the hold logs how many other jobs are running on other
# lanes, read from the runner's Prometheus endpoint. These tests are written
# from the specification, before the code. A curl stub serves the file named
# by $CURL_BODY, exits ${CURL_RC:-0}, and records its arguments.
cat >"$TMP/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
chmod +x "$TMP/bin/systemctl"
cat >"$TMP/bin/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${CURL_LOG:?}"
cat "${CURL_BODY:?}"
exit "${CURL_RC:-0}"
STUB
chmod +x "$TMP/bin/curl"
export CURL_LOG="$TMP/curl.log"
export QH_METRICS="http://127.0.0.1:59252/metrics-test"

sample() { # runner runner_name state stage value ("-" for no runner_name)
  local name=""
  [ "$2" = "-" ] || name=",runner_name=\"$2\""
  printf 'gitlab_runner_jobs{runner="%s"%s,system_id="s_x",state="%s",stage="%s",executor_stage="docker_run"} %s\n' \
    "$1" "$name" "$3" "$4" "$5"
}

fresh_hold() {
  rm -f "$QH_DIR/hold" "$QH_STATE/concurrent.orig" "$QH_STATE/hold.since" "$QH_DIR/applied" "$QH_DIR/failed"
  write_config
  write_flag "$(($(date +%s) + 300))"
}

leak_line() { printf '%s\n' "$1" | grep '^leak: ' || true; }

# 1: two runners, running samples summed per runner, other states ignored
{
  echo '# HELP gitlab_runner_jobs The current number of running jobs.'
  echo '# TYPE gitlab_runner_jobs gauge'
  echo 'gitlab_runner_concurrent 4'
  sample abc123 big running build_script 1
  sample def456 light running build_script 1
  sample def456 light running after_script 1
  sample def456 light success build_script 5
  sample def456 light success after_script 3
  echo 'gitlab_runner_limit 0'
} >"$TMP/body-two"
fresh_hold
: >"$CURL_LOG"
out=$(CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 2 job(s) on other lanes (big=1 light=2) for job=999" "leak: two runners, running samples summed, other states ignored"
applied_n=$(printf '%s\n' "$out" | grep -n 'HOLD applied' | head -n1 | cut -d: -f1 || true)
leak_n=$(printf '%s\n' "$out" | grep -n '^leak: ' | head -n1 | cut -d: -f1 || true)
if [ -n "$applied_n" ] && [ -n "$leak_n" ] && [ "$applied_n" -lt "$leak_n" ]; then
  echo "ok - leak: the leak line comes after the HOLD applied line"
else
  echo "FAIL - leak: the leak line comes after the HOLD applied line: applied at [$applied_n], leak at [$leak_n]"; fail=1
fi

# 10: how curl is called
curl_args=$(cat "$CURL_LOG")
case "$curl_args" in
  *"--max-time 3"*) echo "ok - leak: curl is called with --max-time 3" ;;
  *) echo "FAIL - leak: curl is called with --max-time 3: got [$curl_args]"; fail=1 ;;
esac
case "$curl_args" in
  *"$QH_METRICS"*) echo "ok - leak: curl is called with the QH_METRICS url" ;;
  *) echo "FAIL - leak: curl is called with the QH_METRICS url: got [$curl_args]"; fail=1 ;;
esac

# 2: only the holding job running
sample abc123 big running build_script 1 >"$TMP/body-self"
fresh_hold
out=$(CURL_BODY="$TMP/body-self" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 0 job(s) on other lanes (big=1) for job=999" "leak: only the holding job running gives 0"

# 3: runner metrics but no running samples
{
  echo 'gitlab_runner_concurrent 4'
  sample abc123 big success build_script 7
  sample def456 light success build_script 2
} >"$TMP/body-norunning"
fresh_hold
out=$(CURL_BODY="$TMP/body-norunning" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 0 job(s) on other lanes (none) for job=999" "leak: no running samples gives 0 and none"

# 4: idle runner exports no gitlab_runner_jobs samples at all
printf 'gitlab_runner_concurrent 1\ngitlab_runner_limit 0\n' >"$TMP/body-idle"
fresh_hold
out=$(CURL_BODY="$TMP/body-idle" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 0 job(s) on other lanes (none) for job=999" "leak: an idle runner is a true 0, not a failure"

# 5: not a metrics body
printf '<!doctype html>\n<html><head><title>Not found</title></head><body><h1>404</h1></body></html>\n' >"$TMP/body-html"
fresh_hold
out=$(CURL_BODY="$TMP/body-html" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: unknown (metrics unavailable: no runner metrics in the body)" "leak: an HTML body is reported as no runner metrics"

# 6: no runner_name label falls back to the runner token
sample abc123 - running build_script 1 >"$TMP/body-noname"
fresh_hold
out=$(CURL_BODY="$TMP/body-noname" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 0 job(s) on other lanes (abc123=1) for job=999" "leak: a runner with no runner_name is shown by its token"

# 7: curl fails; the hold is unaffected
fresh_hold
rc=0
out=$(CURL_RC=7 CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null) || rc=$?
check "$(leak_line "$out")" "leak: unknown (metrics unavailable: curl exited 7)" "leak: a curl failure is reported with its exit code"
check "$rc" "0" "leak: a curl failure leaves the exit status 0"
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "leak: a curl failure still applies the hold"
[ -f "$QH_DIR/applied" ] && echo "ok - leak: a curl failure still writes applied" || { echo "FAIL - leak: a curl failure still writes applied"; fail=1; }

# 8: curl not installed. PATH holds only the systemctl stub and symlinks to
# the tools the script may need, each resolved to its real file; curl is
# deliberately not among them.
mkdir -p "$TMP/bin-nocurl"
cp "$TMP/bin/systemctl" "$TMP/bin-nocurl/systemctl"
for tool in bash sh env sed grep cut cat date mkdir printf rm awk sort head tail tr dirname basename mv cp wc mktemp ln chmod touch stat readlink sleep tee id uniq xargs; do
  p=$(command -v "$tool" || true)
  case "$p" in
    /*) ln -sf "$p" "$TMP/bin-nocurl/$tool" ;;
  esac
done
fresh_hold
rc=0
out=$(PATH="$TMP/bin-nocurl" CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null) || rc=$?
check "$(leak_line "$out")" "leak: unknown (metrics unavailable: curl not installed)" "leak: missing curl is reported as not installed"
check "$rc" "0" "leak: missing curl leaves the exit status 0"
check "$(grep '^concurrent = ' "$CONFIG")" "concurrent = 1" "leak: missing curl still applies the hold"
[ -f "$QH_DIR/applied" ] && echo "ok - leak: missing curl still writes applied" || { echo "FAIL - leak: missing curl still writes applied"; fail=1; }

# 9: later runs while held also print the line; release and expiry do not
fresh_hold
CURL_BODY="$TMP/body-two" "$SCRIPT" >/dev/null 2>&1
out=$(CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 2 job(s) on other lanes (big=1 light=2) for job=999" "leak: an idempotent re-run while held prints the line"
rm -f "$QH_DIR/applied"
out=$(CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "leak: 2 job(s) on other lanes (big=1 light=2) for job=999" "leak: a re-marked hold prints the line"
rm -f "$QH_DIR/hold"
out=$(CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "" "leak: a release run prints no leak line"
fresh_hold
CURL_BODY="$TMP/body-two" "$SCRIPT" >/dev/null 2>&1
write_flag "$(($(date +%s) - 10))"
out=$(CURL_BODY="$TMP/body-two" "$SCRIPT" 2>/dev/null)
check "$(leak_line "$out")" "" "leak: an expiry run prints no leak line"

exit $fail
