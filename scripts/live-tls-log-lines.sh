#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Verifies #321 against a running mikroview: a client that refuses the
# certificate, and one that speaks plain HTTP at the HTTPS port, each
# produce a line an operator can act on -- once, not once per retry.
#
# The unit tests cover the translation table. What they cannot show is
# that these strings are what Go actually hands us: the format of
# net/http's own error line is not part of its API, and a mismatch there
# means every line silently falls through to the untranslated path.
# Only a real handshake settles it.
#
# Usage: scripts/live-tls-log-lines.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

DIR="$(mktemp -d)"
cleanup() {
  [ -n "${PID:-}" ] && kill "$PID" 2>/dev/null || true
  rm -rf "$DIR"
}
trap cleanup EXIT

PORT=19831

cat > "$DIR/cfg.yaml" <<EOF
listen: {http: "127.0.0.1:$PORT", httpRedirect: "", syslogTls: ""}
tls: {enabled: true, storePath: $DIR/tls}
auth: {storePath: $DIR/users.json, tokensStorePath: $DIR/tokens.json, secureCookie: true}
flags: {storePath: $DIR/flags.json}
watchlist: {storePath: $DIR/watchlist.json, matchLogPath: $DIR/matchlog.jsonl}
EOF

if [ ! -f web/dist/index.html ]; then
  ( cd frontend && npm run build >/dev/null 2>&1 )
  rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/
fi
# -buildvcs=false: throwaway binary, nothing reads its VCS stamp, and
# stamping fails outright in a linked git worktree (#357).
go build -buildvcs=false -o "$DIR/mikroview" .

MIKROVIEW_CONFIG="$DIR/cfg.yaml" "$DIR/mikroview" > "$DIR/server.log" 2>&1 &
PID=$!

for _ in $(seq 1 40); do
  curl -fsS -k "https://127.0.0.1:$PORT/api/healthz" >/dev/null 2>&1 && break
  sleep 0.25
done

fail=0
say() { printf '  %-5s %s\n' "$1" "$2"; }

# A client that verifies the certificate and rejects it -- what a
# browser with no exception, and a router with no CA imported, both do.
# Python's ssl rather than curl: curl aborts in a way that surfaces as
# "local error: tls: bad record MAC" on TLS 1.3, which is a different
# (and much rarer) case than the one operators actually hit.
python3 - "$PORT" <<'PY'
import socket, ssl, sys
port = int(sys.argv[1])
for _ in range(6):
    ctx = ssl.create_default_context()
    # TLS 1.2 on purpose: on 1.3 the client's rejection alert is
    # encrypted, and the server reports it as "local error: tls: bad
    # record MAC" rather than naming the cause. Both are translated;
    # this is the one operators actually reported (#321).
    ctx.maximum_version = ssl.TLSVersion.TLSv1_2
    try:
        with socket.create_connection(("127.0.0.1", port), timeout=3) as s:
            with ctx.wrap_socket(s, server_hostname="localhost") as ss:
                ss.recv(1)
    except Exception:
        pass  # rejecting is the point
PY

sleep 1

if grep -q "refused our certificate" "$DIR/server.log"; then
  say ok "a client refusing the certificate is explained in plain language"
else
  say FAIL "no plain-language line for a refused certificate"
  fail=1
fi

if grep -q "re-import /ca.crt" "$DIR/server.log"; then
  say ok "the line says what to actually do about it"
else
  say FAIL "the line offers no remedy"
  fail=1
fi

if grep -q "Detail: remote error" "$DIR/server.log"; then
  say ok "the original error is preserved for diagnosis"
else
  say FAIL "the raw error was discarded"
  fail=1
fi

# Six rejections, at most two written lines (a window boundary could
# split them); definitely not six.
n=$(grep -c "refused our certificate" "$DIR/server.log" || true)
if [ "$n" -le 2 ]; then
  say ok "6 rejections from one client produced $n line(s), not 6"
else
  say FAIL "6 rejections produced $n lines -- the repeat gate is not holding"
  fail=1
fi

# Plain HTTP at the HTTPS port deliberately has no assertion here: Go's
# server answers it directly with a 400 and never routes it through
# ErrorLog, so there is no line to translate. Making that request useful
# is issue #325, not this one -- checked by running it and reading the
# log, not assumed.

# The source port is per-retry noise; its absence is what stops a
# reconnect loop reading as a port scan.
if grep "refused our certificate" "$DIR/server.log" | grep -qE '127\.0\.0\.1:[0-9]+ refused'; then
  say FAIL "the ephemeral source port is still in the message"
  fail=1
else
  say ok "the ephemeral source port is not in the message"
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "--- server log ---"
  grep -E "WARN|ERROR" "$DIR/server.log" | tail -20
  exit 1
fi
echo
echo "PASS: handshake failures are explained, actionable, and not repeated."
