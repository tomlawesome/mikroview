#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Verifies the log-throttling contract from #322 against a running
# binary: hammering the syslog TLS port past the per-source connection
# limit must produce O(1) rejection log lines per throttle window, not
# one per attempt -- and the line that is written must carry a running
# count, so the suppressed attempts are visible rather than just gone.
#
# The unit tests cover the Limiter in isolation. What they cannot show
# is that the listener's rejection paths actually route through it --
# which is the defect being guarded against: the port is unauthenticated
# by design, so an un-gated rejection line hands anyone who can reach it
# the ability to write log lines at connection-attempt rate.
#
# Usage: scripts/live-logspam-check.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
. "$REPO/scripts/live-stores.sh"
. "$REPO/scripts/live-slot.sh"

DIR="$(mktemp -d)"
cleanup() {
  [ -n "${PID:-}" ] && kill "$PID" 2>/dev/null || true
  rm -rf "$DIR"
}
trap cleanup EXIT

# Ports come from the shared standalone allocator (live-slot.sh), not a
# hardcoded value: those used to sit inside live-env.sh's per-checkout
# band and collide with it (#660). SYSLOG_PORT is this script's name for
# what the allocator calls the syslog-TLS port.
HTTP_PORT=$MV_STANDALONE_HTTP_PORT
SYSLOG_PORT=$MV_STANDALONE_SYSLOG_TLS_PORT
mv_require_free_port "$HTTP_PORT" "the log-throttling check's server"
mv_require_free_port "$SYSLOG_PORT" "the log-throttling check's syslog-TLS listener"

cat > "$DIR/cfg.yaml" <<EOF
listen: {http: "127.0.0.1:$HTTP_PORT", httpRedirect: "", syslogTls: "127.0.0.1:$SYSLOG_PORT"}
tls: {enabled: true, storePath: $DIR/tls}
$(mv_store_block "$DIR" true)
EOF

# The embedded frontend only needs to exist for the build; rebuild it
# only when it's genuinely missing, since this check is about the
# listener, not the UI.
if [ ! -f web/dist/index.html ]; then
  ( cd frontend && npm run build >/dev/null 2>&1 )
  rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/
fi
# -buildvcs=false: throwaway binary, nothing reads its VCS stamp, and
# stamping fails outright in a linked git worktree (#357).
go build -buildvcs=false -o "$DIR/mikroview" .

MIKROVIEW_CONFIG="$DIR/cfg.yaml" "$DIR/mikroview" > "$DIR/server.log" 2>&1 &
PID=$!

up=0
for _ in $(seq 1 40); do
  if curl -fsS -k "https://127.0.0.1:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then up=1; break; fi
  sleep 0.25
done
if [ "$up" -ne 1 ]; then
  echo "FAIL: server never came up"
  tail -20 "$DIR/server.log"
  exit 1
fi

# 20 sockets from one source against a per-source limit of 8: the first
# 8 hold slots (no TLS handshake needed -- slots are counted at accept),
# the remaining 12 are rejected. All 12 land inside one 30s throttle
# window.
ATTEMPTS=20
python3 - "$SYSLOG_PORT" "$ATTEMPTS" <<'PY'
import socket, sys, time
port, n = int(sys.argv[1]), int(sys.argv[2])
held = []
for _ in range(n):
    s = socket.socket()
    s.settimeout(2)
    try:
        s.connect(("127.0.0.1", port))
        held.append(s)
    except OSError:
        pass
    time.sleep(0.05)
time.sleep(1)  # let the server finish logging before we read the log
for s in held:
    s.close()
PY

fail=0
say() { printf '  %-5s %s\n' "$1" "$2"; }

lines=$(grep -c "per-source connection limit" "$DIR/server.log" || true)
rejected=$((ATTEMPTS - 8))
if [ "$lines" -ge 1 ] && [ "$lines" -le 2 ]; then
  say ok "$rejected rejections produced $lines log line(s), not $rejected"
else
  say FAIL "expected 1-2 throttled rejection lines, got $lines"
  fail=1
fi

if grep -q "such rejections since start" "$DIR/server.log"; then
  say ok "the written line carries the running count"
else
  say FAIL "no running count in the rejection line"
  fail=1
fi

if kill -0 "$PID" 2>/dev/null; then
  say ok "the server is still running"
else
  say FAIL "the server exited"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "--- rejection lines ---"
  grep "connection limit" "$DIR/server.log" | tail -5 || true
  exit 1
fi
echo
echo "PASS: rejection logging is throttled and counted."
