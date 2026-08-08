#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Stands up a real mikroview: real binary, real embedded UI, real syslog
# listeners, real admin account. Not a mock and not the test suite.
#
# This exists because nearly every defect worth finding this project has
# hit was found by running the thing, not by reading it or unit-testing
# it: recovery keys in the container log (a TTY check that a Docker pty
# satisfies), CLI commands writing files nothing read on a Postgres
# deployment, a rule pattern that became unevaluable only once matching
# events arrived. None were visible from the code or the suite.
#
# Usage:
#   eval "$(scripts/live-env.sh up)"   # exports MV_URL, MV_USER, MV_PASS, MV_DIR
#   scripts/live-env.sh syslog 200     # feed N synthetic firewall events
#   scripts/live-env.sh down
#
# MV_BIND=<addr> moves the listener off loopback, which the RouterOS
# fixture needs: a rootless container cannot reach the host's 127.0.0.1,
# but it can reach the host's LAN address. Off loopback the instance runs
# with TLS on, because config.TLS.Enabled's own doc comment says plain
# HTTP is only defensible while the listener is provably unreachable from
# the LAN -- which is the exact property MV_BIND gives up.
set -euo pipefail

MV_DIR="${MV_DIR:-/tmp/mikroview-live}"
MV_BIND="${MV_BIND:-127.0.0.1}"
HTTP_PORT="${MV_HTTP_PORT:-19801}"
SYSLOG_PORT="${MV_SYSLOG_PORT:-16801}"
MV_USER="live-admin"
MV_PASS="live-password-123"

if [ "$MV_BIND" = "127.0.0.1" ]; then
  MV_SCHEME=http
  TLS_BLOCK='tls: {enabled: false}'
  SECURE_COOKIE=false
  CURL_TLS=()
else
  MV_SCHEME=https
  TLS_BLOCK="tls: {enabled: true, hosts: [\"$MV_BIND\", \"127.0.0.1\"], storePath: $MV_DIR/data/tls}"
  SECURE_COOKIE=true
  # The generated CA is trusted by nothing yet -- that is what
  # live-routeros.sh's `trust` step is for on the router side. Here the
  # harness is talking to a certificate it just watched get created.
  CURL_TLS=(-k)
fi

build() {
  ( cd frontend && npm run build >/dev/null 2>&1 )
  rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/
  go build -o "$MV_DIR/mikroview" .
}

up() {
  # Always start from nothing. Without this an earlier instance keeps the
  # port, the new binary fails to bind, and the admin registration lands
  # on the *old* server -- which answers 409 and looks like a bug in the
  # thing under test rather than in the harness.
  down >/dev/null 2>&1 || true
  for _ in $(seq 1 20); do
    if ! curl -fsS "${CURL_TLS[@]+"${CURL_TLS[@]}"}" "$MV_SCHEME://$MV_BIND:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then break; fi
    sleep 0.25
  done
  rm -rf "$MV_DIR"; mkdir -p "$MV_DIR/data"
  build
  cat > "$MV_DIR/cfg.yaml" <<EOF
listen: {syslogUdp: "127.0.0.1:$SYSLOG_PORT", syslogTcp: "127.0.0.1:$((SYSLOG_PORT+1))", http: "$MV_BIND:$HTTP_PORT", httpRedirect: ""}
$TLS_BLOCK
auth:
  storePath: $MV_DIR/data/users.json
  recoveryKeysPath: $MV_DIR/data/recovery.json
  recoveryPepperPath: $MV_DIR/data/pepper
  tokensStorePath: $MV_DIR/data/tokens.json
  secureCookie: $SECURE_COOKIE
flags: {storePath: $MV_DIR/data/flags.json}
entities: {storePath: $MV_DIR/data/entities.json}
audit: {storePath: $MV_DIR/data/audit.json}
EOF
  MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" > "$MV_DIR/server.log" 2>&1 &
  echo $! > "$MV_DIR/pid"

  for _ in $(seq 1 40); do
    if curl -fsS "${CURL_TLS[@]+"${CURL_TLS[@]}"}" "$MV_SCHEME://$MV_BIND:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then break; fi
    sleep 0.25
  done

  curl -fsS "${CURL_TLS[@]+"${CURL_TLS[@]}"}" -X POST -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
    -d "{\"username\":\"$MV_USER\",\"password\":\"$MV_PASS\"}" \
    "$MV_SCHEME://$MV_BIND:$HTTP_PORT/api/auth/register" >/dev/null

  echo "export MV_URL=$MV_SCHEME://$MV_BIND:$HTTP_PORT"
  echo "export MV_USER=$MV_USER"
  echo "export MV_PASS=$MV_PASS"
  echo "export MV_DIR=$MV_DIR"
  echo "export MV_SYSLOG_PORT=$SYSLOG_PORT"
}

# syslog N [label] -- N synthetic RouterOS firewall lines over UDP.
syslog() {
  python3 - "$SYSLOG_PORT" "${1:-100}" "${2:-live-test-rule}" <<'PY'
import socket, sys
port, n, label = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
for i in range(n):
    s.sendto((f"firewall,info D|{label}| forward: in:ether1 out:bridge1, "
              f"connection-state:new, proto TCP (SYN), "
              f"203.0.113.{i%250}:{5000+i%1000}->192.168.1.10:443, len 60").encode(),
             ("127.0.0.1", port))
print(f"sent {n} events labelled {label}", file=sys.stderr)
PY
}

# portscan N [source-ip] -- N distinct destination ports from one source
# IP, inside the default port-scan window, so a real port_scan flag gets
# raised rather than synthesized -- for scenarios (live-exclusions.mjs,
# live-flags-clearing.mjs) that need an actual flag to clear/exclude, not
# just events in the table. source-ip defaults to 198.51.100.77;
# pass a different one to raise a second, independent flag.
portscan() {
  python3 - "$SYSLOG_PORT" "${1:-20}" "${2:-198.51.100.77}" <<'PY'
import socket, sys
port, n, src = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
for i in range(n):
    s.sendto((f"firewall,info D|scan-src| forward: in:ether1 out:bridge1, "
              f"connection-state:new, proto TCP (SYN), "
              f"{src}:{40000+i}->192.168.1.10:{1000+i}, len 60").encode(),
             ("127.0.0.1", port))
print(f"sent a {n}-port scan from {src}", file=sys.stderr)
PY
}

down() {
  if [ -f "$MV_DIR/pid" ]; then kill "$(cat "$MV_DIR/pid")" 2>/dev/null || true; fi
  # Belt and braces: a run killed mid-way leaves no pid file but may leave
  # the process, and the next `up` would then silently talk to it.
  pkill -f "$MV_DIR/mikroview" 2>/dev/null || true
  sleep 0.4
  rm -rf "$MV_DIR"
}

case "${1:-}" in
  up) up ;;
  syslog) shift; syslog "$@" ;;
  portscan) shift; portscan "$@" ;;
  down) down ;;
  *) echo "usage: $0 {up|syslog N [label]|portscan N|down}" >&2; exit 2 ;;
esac
