#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Verifies that SIGHUP swaps the served certificate on both listeners
# without a restart (#294 item 5) -- against a running mikroview, with a
# real TLS handshake reading the certificate off the wire.
#
# The unit tests cover the reloader in isolation. What they cannot show
# is the part that actually broke: the certificate a *listener* hands out
# came from a fixed tls.Config built once at startup, so a reloader that
# works perfectly changes nothing until both listeners read through it.
# That is only observable by connecting.
#
# Usage: scripts/live-cert-reload.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

DIR="$(mktemp -d)"
cleanup() {
  [ -n "${PID:-}" ] && kill "$PID" 2>/dev/null || true
  rm -rf "$DIR"
}
trap cleanup EXIT

HTTP_PORT=19811
SYSLOG_PORT=19812

# An operator-supplied certificate, which is the case that matters:
# mikroview's own generated one has no external renewal to pick up.
mkcert() {
  local cn="$1" out="$2"
  openssl req -new -x509 -days 2 -nodes -subj "/CN=$cn" \
    -addext "subjectAltName=IP:127.0.0.1" \
    -keyout "$out.key" -out "$out.crt" >/dev/null 2>&1
}

mkcert "before-renewal" "$DIR/first"
mkcert "after-renewal" "$DIR/second"
cp "$DIR/first.crt" "$DIR/live.crt"
cp "$DIR/first.key" "$DIR/live.key"

cat > "$DIR/cfg.yaml" <<EOF
listen: {http: "127.0.0.1:$HTTP_PORT", httpRedirect: "", syslogTls: "127.0.0.1:$SYSLOG_PORT"}
tls:
  enabled: true
  certFile: $DIR/live.crt
  keyFile: $DIR/live.key
auth: {storePath: $DIR/users.json, tokensStorePath: $DIR/tokens.json, secureCookie: true}
flags: {storePath: $DIR/flags.json}
watchlist: {storePath: $DIR/watchlist.json, matchLogPath: $DIR/matchlog.jsonl}
EOF

( cd frontend && npm run build >/dev/null 2>&1 )
rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/
# -buildvcs=false: throwaway binary, nothing reads its VCS stamp, and
# stamping fails outright in a linked git worktree (#357).
go build -buildvcs=false -o "$DIR/mikroview" .

MIKROVIEW_CONFIG="$DIR/cfg.yaml" "$DIR/mikroview" > "$DIR/server.log" 2>&1 &
PID=$!

for _ in $(seq 1 40); do
  curl -fsS -k "https://127.0.0.1:$HTTP_PORT/api/healthz" >/dev/null 2>&1 && break
  sleep 0.25
done

# The subject the listener actually serves, read off a real handshake.
served() {
  local port="$1"
  echo | openssl s_client -connect "127.0.0.1:$port" 2>/dev/null | openssl x509 -noout -subject 2>/dev/null
}

fail=0
say() { printf '  %-5s %s\n' "$1" "$2"; }

https_before="$(served "$HTTP_PORT")"
syslog_before="$(served "$SYSLOG_PORT")"
case "$https_before" in *before-renewal*) say ok "https serves the original certificate";; *) say FAIL "https before: $https_before"; fail=1;; esac
case "$syslog_before" in *before-renewal*) say ok "syslog tls serves the original certificate";; *) say FAIL "syslog before: $syslog_before"; fail=1;; esac

# The renewal: new files in place, no restart.
cp "$DIR/second.crt" "$DIR/live.crt"
cp "$DIR/second.key" "$DIR/live.key"

# Nothing should change until the signal, since mikroview cannot tell a
# finished renewal from a half-written one.
unchanged="$(served "$HTTP_PORT")"
case "$unchanged" in *before-renewal*) say ok "replacing the files alone changes nothing -- the swap waits to be told";; *) say FAIL "changed without a signal: $unchanged"; fail=1;; esac

kill -HUP "$PID"
sleep 1

https_after="$(served "$HTTP_PORT")"
syslog_after="$(served "$SYSLOG_PORT")"
case "$https_after" in *after-renewal*) say ok "https serves the renewed certificate after SIGHUP";; *) say FAIL "https after: $https_after"; fail=1;; esac
case "$syslog_after" in *after-renewal*) say ok "syslog tls serves it too -- the listener routers actually connect to";; *) say FAIL "syslog after: $syslog_after"; fail=1;; esac

# A broken renewal must not take the listener down.
printf -- '-----BEGIN CERTIFICATE-----\ntruncated' > "$DIR/live.crt"
kill -HUP "$PID"
sleep 1
after_bad="$(served "$HTTP_PORT")"
case "$after_bad" in *after-renewal*) say ok "a broken reload keeps the certificate already loaded, rather than serving none";; *) say FAIL "after a broken reload: $after_bad"; fail=1;; esac

if kill -0 "$PID" 2>/dev/null; then
  say ok "the server is still running"
else
  say FAIL "the server exited"
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo
  echo "--- server log ---"
  tail -20 "$DIR/server.log"
  exit 1
fi
echo
echo "PASS: SIGHUP swaps the certificate on both listeners, and a bad reload is survivable."
