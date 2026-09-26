#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/upgrade-fixture-session.py (#1358) against a
# throwaway HTTPS+TCP stand-in for a released image -- no docker, no
# network, no real MikroView -- rather than record-upgrade-fixture.
# test.sh's stubbed `docker`, which never actually runs session.py (its
# `docker run` stub just echoes a fixed manifest). That leaves the one
# thing #1358 changed -- the second-factor enrolment dance -- with no
# regression coverage at all, which this closes.
#
# Two runs, same session.py, two fake servers:
#   - "new" enforces the forced-enrolment door #1253 added in v0.6.1,
#     the same way internal/api/auth.go's requireAuth does: every route
#     but the totp enrol/confirm pair 403s "this account has no second
#     factor..." until a code is confirmed against the secret POST
#     /api/auth/totp/enrol just handed out. It independently computes
#     the RFC 6238 code from that secret and only accepts a submission
#     that matches -- an honest counterpart, not a rubber stamp -- so a
#     pass here proves the code session.py's totp_code_now computed was
#     the one the server actually expected, not merely that some string
#     was submitted.
#   - "old" has no /api/auth/totp/* routes at all (a 404), matching
#     every pre-v0.6.1 image, and never gates anything.
# Both must let the recorded session run to completion, and the
# manifest session.py prints on stdout must say which happened
# (secondFactorEnrolled), so record-upgrade-fixture.sh's own
# post-processing keeps seeing the field it expects.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"

TMP="$(mktemp -d)"
SERVER_PID=""
cleanup() {
  [ -z "$SERVER_PID" ] || kill "$SERVER_PID" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

# A throwaway self-signed cert -- session.py's ssl_ctx sets
# check_hostname=False and verify_mode=CERT_NONE (it talks to a
# container on 127.0.0.1 sharing its own network namespace, never a
# name a real CA would sign for), so nothing here needs a matching
# name or a real chain. Same reasoning as record-upgrade-fixture.sh's
# own throwaway Postgres certificate.
openssl req -new -x509 -days 1 -nodes -subj "/CN=upgrade-fixture-session-test" \
  -keyout "$TMP/key.pem" -out "$TMP/cert.pem" >/dev/null 2>&1

# fake_server.py <https-port> <syslog-port> <mode: new|old> <log-file>
# <cert> <key> -- stands in for the recorded container's own API and
# syslog listener for exactly the calls generation "A" makes (no
# watchlist/coverage -- those are separate, already-untouched code
# paths; see this script's own header for why generation A is enough
# to cover the change at hand). Logs one line per request path so the
# test can tell which routes session.py actually reached.
cat > "$TMP/fake_server.py" <<'PYEOF'
import base64
import hashlib
import hmac
import http.server
import json
import socket
import ssl
import struct
import sys
import threading
import time

https_port, syslog_port, mode, log_path, cert, key = sys.argv[1:7]
https_port = int(https_port)
syslog_port = int(syslog_port)

logf = open(log_path, "a")


def log(line):
    logf.write(line + "\n")
    logf.flush()


def totp_code(secret_bytes, counter, digits=6):
    digest = hmac.new(secret_bytes, struct.pack(">Q", counter), hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    code = int.from_bytes(digest[offset:offset + 4], "big") & 0x7FFFFFFF
    return str(code % (10 ** digits)).zfill(digits)


state = {"confirmed": mode != "new", "secret": None}


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass  # quiet -- fake_server's own log() above is what the test reads

    def _body(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        raw = self.rfile.read(length) if length else b""
        return json.loads(raw) if raw else {}

    def _reply(self, status, body=None, cookie=None):
        self.send_response(status)
        if cookie:
            self.send_header("Set-Cookie", cookie)
        payload = json.dumps(body).encode() if body is not None else b""
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        if payload:
            self.wfile.write(payload)

    def handle_one(self, method):
        path = self.path
        log(f"{method} {path}")

        if path == "/api/healthz":
            return self._reply(200, {})

        if path == "/api/auth/register":
            self._body()
            return self._reply(201, {"username": "upgrade-fixture-admin", "role": "admin"}, cookie="sid=1")

        if path == "/api/auth/totp/enrol":
            if mode == "old":
                return self._reply(404, {})
            secret = b"upgrade-fixture-session-test-2fa"[:20]
            state["secret"] = secret
            return self._reply(200, {"uri": "otpauth://totp/x", "secret": base64.b32encode(secret).decode().rstrip("=")})

        if path == "/api/auth/totp/confirm":
            if mode == "old":
                return self._reply(404, {})
            code = self._body().get("code", "")
            now = int(time.time() // 30)
            valid = {totp_code(state["secret"], now + d) for d in (-1, 0, 1)}
            if code not in valid:
                return self._reply(400, {})
            state["confirmed"] = True
            return self._reply(200, {"enabled": True, "recoveryCodes": [f"code-{i}" for i in range(10)]}, cookie="sid=2")

        # Everything else -- /api/auth/users, /api/tokens, /api/entities,
        # PUT coverage/host-mark -- is what requireAuth's forced-
        # enrolment door (#1253) gates on the real server.
        if mode == "new" and not state["confirmed"]:
            return self._reply(403, None)
        self._body()
        return self._reply(200, {})

    def do_GET(self):
        self.handle_one("GET")

    def do_POST(self):
        self.handle_one("POST")

    def do_PUT(self):
        self.handle_one("PUT")


def serve_syslog():
    # Plain TCP, matching this test's own "plain" syslog_mode argv --
    # only the pre-TLS-listener v0.1.0 path uses this, but it is the
    # simpler listener to stand up here and session.py's own behaviour
    # (open, send one line, wait) does not depend on which mode reads
    # it on the other end.
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", syslog_port))
    srv.listen(1)
    conn, _ = srv.accept()
    conn.recv(4096)
    log("SYSLOG line received")
    conn.close()
    srv.close()


threading.Thread(target=serve_syslog, daemon=True).start()

httpd = http.server.ThreadingHTTPServer(("127.0.0.1", https_port), Handler)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(cert, key)
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
httpd.serve_forever()
PYEOF

wait_for_port() { # wait_for_port <port>
  for _ in $(seq 1 50); do
    python3 -c "
import socket, sys
s = socket.socket()
s.settimeout(0.2)
try:
    s.connect(('127.0.0.1', $1))
except OSError:
    sys.exit(1)
" && return 0
    sleep 0.1
  done
  return 1
}

# run_case <mode> <https-port> <syslog-port> -- starts a fresh fake
# server for <mode>, drives the real session.py against it exactly as
# record-upgrade-fixture.sh's `docker run` line would (same argv
# shape), and leaves $OUT/$RC/$LOG set for the caller's checks.
run_case() {
  local mode="$1" https_port="$2" syslog_port="$3"
  LOG="$TMP/log-$mode"
  : > "$LOG"
  python3 "$TMP/fake_server.py" "$https_port" "$syslog_port" "$mode" "$LOG" "$TMP/cert.pem" "$TMP/key.pem" &
  SERVER_PID=$!
  wait_for_port "$https_port" || { echo "fake server for $mode never opened $https_port"; kill "$SERVER_PID" 2>/dev/null || true; exit 1; }

  set +e
  OUT="$(python3 "$ROOT/scripts/upgrade-fixture-session.py" \
    "https://127.0.0.1:$https_port" 127.0.0.1 "$syslog_port" A "v-test-$mode" plain no)"
  RC=$?
  set -e
  kill "$SERVER_PID" >/dev/null 2>&1 || true
  wait "$SERVER_PID" 2>/dev/null || true
  SERVER_PID=""
}

# --- v0.6.1-shaped server: the forced-enrolment door is up ----------
run_case new 18443 11514
check "$([ "$RC" -eq 0 ] && echo true || echo false)" "session.py completes against a server enforcing the second-factor door (rc=$RC)"
if [ "$RC" -ne 0 ]; then
  printf '    %s\n' "${OUT//$'\n'/$'\n    '}"
fi

enrolled="$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['secondFactorEnrolled'])" "$OUT" 2>/dev/null || echo ERROR)"
check "$([ "$enrolled" = "True" ] && echo true || echo false)" "manifest reports secondFactorEnrolled=true when the door was up"
check "$(grep -q 'POST /api/auth/totp/enrol' "$LOG" && echo true || echo false)" "session.py asked to enrol a factor"
check "$(grep -q 'POST /api/auth/totp/confirm' "$LOG" && echo true || echo false)" "session.py confirmed it with a code the fake server's own RFC 6238 check accepted"

# --- pre-v0.6.1-shaped server: no totp routes exist, nothing gated --
run_case old 18444 11515
check "$([ "$RC" -eq 0 ] && echo true || echo false)" "session.py completes unmodified against a server with no second-factor door (rc=$RC)"
if [ "$RC" -ne 0 ]; then
  printf '    %s\n' "${OUT//$'\n'/$'\n    '}"
fi

enrolled_old="$(python3 -c "import json,sys; print(json.loads(sys.argv[1])['secondFactorEnrolled'])" "$OUT" 2>/dev/null || echo ERROR)"
check "$([ "$enrolled_old" = "False" ] && echo true || echo false)" "manifest reports secondFactorEnrolled=false when the server has no such route"
check "$(grep -q 'POST /api/auth/totp/enrol' "$LOG" && echo true || echo false)" "session.py still tried the enrol route once (that 404 is the detection, not a version check)"
check "$(! grep -q 'POST /api/auth/totp/confirm' "$LOG" && echo true || echo false)" "session.py never called confirm once enrol itself 404'd"

echo
if [ "$fails" -ne 0 ]; then
  echo "upgrade-fixture-session.test.sh: $fails check(s) failed"
  exit 1
fi
echo "upgrade-fixture-session.test.sh: all checks passed"
