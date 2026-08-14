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
SYSLOG_TLS_PORT="${MV_SYSLOG_TLS_PORT:-16803}"
MV_USER="live-admin"
MV_PASS="live-password-123"

if [ "$MV_BIND" = "127.0.0.1" ]; then
  MV_SCHEME=http
  TLS_BLOCK='tls: {enabled: false}'
  SECURE_COOKIE=false
  CURL_TLS=()
  # Syslog TLS runs even with tls.enabled=false, unlike httpRedirect:
  # mikroview loads a certificate when *either* the HTTPS listener or
  # syslogTls needs one, and starts this listener on syslogTls being
  # non-empty alone. It has to run here -- since #189 removed the
  # plaintext listeners this is mikroview's only syslog ingest, and
  # leaving it empty gave every browser scenario zero events.
  SYSLOG_TLS_ADDR="127.0.0.1:$SYSLOG_TLS_PORT"
  # A declared device, loopback mode only. Every feeder connects from
  # 127.0.0.1, so this stays the single device -- now with a stable id
  # -- which is what the token dialog's device pick-list (#326) and
  # every devices[0]-reading scenario get to rely on. NOT set in
  # $MV_BIND mode: the CHR's traffic arrives from a different address
  # there, and a second device would make devices[0] nondeterministic
  # (internal/device.Registry.List is map-ordered).
  DEVICES_BLOCK='devices: [{id: live-router, name: Live Router, sourceIp: 127.0.0.1}]'
else
  MV_SCHEME=https
  TLS_BLOCK="tls: {enabled: true, hosts: [\"$MV_BIND\", \"127.0.0.1\"], storePath: $MV_DIR/data/tls}"
  SECURE_COOKIE=true
  # The generated CA is trusted by nothing yet -- that is what
  # live-routeros.sh's `trust` step is for on the router side. Here the
  # harness is talking to a certificate it just watched get created.
  CURL_TLS=(-k)
  # On $MV_BIND, not loopback: the router fixture (#188) needs to reach
  # this listener the same way it reaches the HTTPS one above.
  SYSLOG_TLS_ADDR="$MV_BIND:$SYSLOG_TLS_PORT"
  DEVICES_BLOCK=''
fi

# The host half of SYSLOG_TLS_ADDR, for the feeders below to dial.
SYSLOG_TLS_HOST="${SYSLOG_TLS_ADDR%:*}"

# send_tls -- read complete syslog lines on stdin and deliver them over
# the TLS listener, mikroview's only syslog ingest since #189. Lines are
# newline-delimited: the listener splits a read on newlines when they are
# present and takes it whole when they are not, so this shape and
# RouterOS's unterminated one both land as one event per message.
send_tls() {
  python3 -c '
import socket, ssl, sys, time
host, port = sys.argv[1], int(sys.argv[2])
lines = [l for l in sys.stdin.buffer.read().split(b"\n") if l]
# The certificate is self-signed and was generated seconds ago by the
# server under test. There is no chain to verify against, and verifying
# one is not what these scenarios exercise -- live-routeros.sh trust is
# where the real trust step is covered.
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
with socket.create_connection((host, port), timeout=10) as sock:
    sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
    with ctx.wrap_socket(sock, server_hostname=host) as tls:
        # One message per write, paced. The listener hands each parsed
        # line to a buffered channel with a non-blocking send and drops
        # on a full channel, so a single bulk write outruns the consumer
        # and silently loses events -- measured at 575 of 900 before this
        # pacing, 900 of 900 after. One write per message is also what a
        # real RouterOS router does, so this matches the shape the
        # listener was measured against.
        for i, line in enumerate(lines):
            tls.sendall(line + b"\n")
            if i % 25 == 24:
                time.sleep(0.01)
' "$SYSLOG_TLS_HOST" "$SYSLOG_TLS_PORT"
}

build() {
  ( cd frontend && npm run build >/dev/null 2>&1 )
  # touch .gitkeep for the same reason the Makefile's frontend target
  # does: rm -rf takes the only tracked file in here with it, and a live
  # check should not leave the tree dirty (#353).
  rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/ && touch web/dist/.gitkeep
  # -buildvcs=false: this binary is a throwaway built into a temp dir,
  # run by the scenarios and deleted, so nothing ever reads its VCS
  # stamp. Stamping it also fails outright in a linked git worktree --
  # "error obtaining VCS status: exit status 128" -- which took the whole
  # live check down for anyone not working in a plain clone (#348).
  go build -buildvcs=false -o "$MV_DIR/mikroview" .
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
listen: {syslogTls: "$SYSLOG_TLS_ADDR", http: "$MV_BIND:$HTTP_PORT", httpRedirect: ""}
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
watchlist: {storePath: $MV_DIR/data/watchlist.json, matchLogPath: $MV_DIR/data/matchlog.jsonl}
$DEVICES_BLOCK
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
  # No MV_SYSLOG_PORT: there is no plaintext listener to point it at any
  # more, and exporting a dead port invites scenarios to hand-roll a UDP
  # feed that silently delivers nothing (which is exactly what happened).
  echo "export MV_SYSLOG_TLS_PORT=$SYSLOG_TLS_PORT"
}

# syslog N [label] -- N synthetic RouterOS firewall lines over syslog TLS.
syslog() {
  python3 - "${1:-100}" "${2:-live-test-rule}" <<'PY' | send_tls
import sys
n, label = int(sys.argv[1]), sys.argv[2]
for i in range(n):
    print(f"firewall,info D|{label}| forward: in:ether1 out:bridge1, "
          f"connection-state:new, proto TCP (SYN), "
          f"203.0.113.{i%250}:{5000+i%1000}->192.168.1.10:443, len 60")
PY
  echo "sent ${1:-100} events labelled ${2:-live-test-rule}" >&2
}

# raw LINE -- deliver one exact syslog line, for scenarios needing a
# specific shape (a control-port hit, say) rather than the bulk
# generators. Scenarios must use this rather than opening their own
# socket: there is no plaintext listener left for them to talk to.
raw() {
  printf '%s\n' "$1" | send_tls
}

# portscan N [source-ip] -- N distinct destination ports from one source
# IP, inside the default port-scan window, so a real port_scan flag gets
# raised rather than synthesized -- for scenarios (live-exclusions.mjs,
# live-flags-clearing.mjs) that need an actual flag to clear/exclude, not
# just events in the table. source-ip defaults to 198.51.100.77;
# pass a different one to raise a second, independent flag.
portscan() {
  python3 - "${1:-20}" "${2:-198.51.100.77}" <<'PY' | send_tls
import sys
n, src = int(sys.argv[1]), sys.argv[2]
for i in range(n):
    print(f"firewall,info D|scan-src| forward: in:ether1 out:bridge1, "
          f"connection-state:new, proto TCP (SYN), "
          f"{src}:{40000+i}->192.168.1.10:{1000+i}, len 60")
PY
  echo "sent a ${1:-20}-port scan from ${2:-198.51.100.77}" >&2
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
  raw) shift; raw "$@" ;;
  portscan) shift; portscan "$@" ;;
  down) down ;;
  *) echo "usage: $0 {up|syslog N [label]|raw LINE|portscan N [src-ip]|down}" >&2; exit 2 ;;
esac
