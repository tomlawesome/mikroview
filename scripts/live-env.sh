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
set -euo pipefail

MV_DIR="${MV_DIR:-/tmp/mikroview-live}"
HTTP_PORT="${MV_HTTP_PORT:-19801}"
SYSLOG_PORT="${MV_SYSLOG_PORT:-16801}"
MV_USER="live-admin"
MV_PASS="live-password-123"

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
    if ! curl -fsS "http://127.0.0.1:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then break; fi
    sleep 0.25
  done
  rm -rf "$MV_DIR"; mkdir -p "$MV_DIR/data"
  build
  cat > "$MV_DIR/cfg.yaml" <<EOF
listen: {syslogUdp: "127.0.0.1:$SYSLOG_PORT", syslogTcp: "127.0.0.1:$((SYSLOG_PORT+1))", http: "127.0.0.1:$HTTP_PORT"}
tls: {enabled: false}
auth:
  storePath: $MV_DIR/data/users.json
  recoveryKeysPath: $MV_DIR/data/recovery.json
  recoveryPepperPath: $MV_DIR/data/pepper
  tokensStorePath: $MV_DIR/data/tokens.json
  secureCookie: false
flags: {storePath: $MV_DIR/data/flags.json}
entities: {storePath: $MV_DIR/data/entities.json}
audit: {storePath: $MV_DIR/data/audit.json}
EOF
  MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" > "$MV_DIR/server.log" 2>&1 &
  echo $! > "$MV_DIR/pid"

  for _ in $(seq 1 40); do
    if curl -fsS "http://127.0.0.1:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then break; fi
    sleep 0.25
  done

  curl -fsS -X POST -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
    -d "{\"username\":\"$MV_USER\",\"password\":\"$MV_PASS\"}" \
    "http://127.0.0.1:$HTTP_PORT/api/auth/register" >/dev/null

  echo "export MV_URL=http://127.0.0.1:$HTTP_PORT"
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
  down) down ;;
  *) echo "usage: $0 {up|syslog N [label]|down}" >&2; exit 2 ;;
esac
