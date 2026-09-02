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

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/live-stores.sh"
# The slot and every port derived from it live in one place since #660.
# They used to be computed here and hardcoded, differently, in the four
# standalone scripts -- two allocators handing out overlapping ranges,
# which collided by construction rather than by luck.
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/live-slot.sh"

# Defaults are derived per checkout, not fixed, because two live checks
# running at once used to destroy each other rather than merely clash.
# `up` calls `down` and then `rm -rf "$MV_DIR"`, so a second run on the
# same defaults killed the first run's server and deleted its data
# directory mid-scenario -- surfacing as an unexplained scenario timeout
# in the run that got trampled, with nothing in its own log to explain
# it. Observed 2026-08-22 with several agents driving live-check from
# separate git worktrees at the same time.
#
# The slot is a hash of the checkout path, so each worktree gets its own
# directory and its own port block, stable across repeated runs in the
# same tree -- which keeps $MV_DIR/server.log and `down` predictable
# between invocations. An explicit MV_DIR or MV_*_PORT still wins.
#
# 64 slots is not a guarantee: two checkouts can hash to the same one.
# That is what the bind check in `up` is for -- it turns a residual
# collision into a named error instead of a silent trampling.
#
# MV_SLOT and the port bands come from live-slot.sh, sourced above.

MV_DIR="${MV_DIR:-/tmp/mikroview-live-$MV_SLOT}"
MV_BIND="${MV_BIND:-127.0.0.1}"
HTTP_PORT="${MV_HTTP_PORT:-$MV_SLOT_HTTP_PORT}"
SYSLOG_PORT="${MV_SYSLOG_PORT:-$MV_SLOT_SYSLOG_PORT}"
SYSLOG_TLS_PORT="${MV_SYSLOG_TLS_PORT:-$MV_SLOT_SYSLOG_TLS_PORT}"
MV_USER="live-admin"
MV_PASS="live-password-123"

if [ "$MV_BIND" = "127.0.0.1" ]; then
  MV_SCHEME=http
  # storePath even with enabled: false. The syslog-TLS listener loads a
  # certificate independently of the HTTPS one (main.go's
  # cfg.TLS.Enabled || cfg.Listen.SyslogTLS != ""), so a CA is still
  # generated here -- and without a path it lands on the
  # /var/lib/mikroview default that no developer machine can write.
  TLS_BLOCK="tls: {enabled: false, storePath: $MV_DIR/data/tls}"
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

# MV_DEMO_DEVICES=1 declares the estate scripts/seed-demo.py feeds, which
# is the only way its pushed tables can ever be read back (#709).
#
# A pushed rule/NAT/address table is keyed by *device id*. seed-demo.py
# mints its ingest tokens against the router names below and then streams
# syslog from one loopback address per router, so unless those addresses
# are declared here the registry invents a discovered device per source
# IP -- "127.0.0.1" and friends -- and the pushed tables sit under ids no
# device has. Both halves report success and never meet: the topography
# draws an unnamed waist card and boundary-derived zones, and the fall's
# bands read "not in a pushed rule table". seed-demo.py was written
# expecting this block to exist; its own comment calls guest-ap
# "declared in cfg.yaml", and nothing declared it.
#
# Opt-in, and set after both branches above deliberately: the gate's
# scenarios read devices[0], and internal/device.Registry.List is
# map-ordered, so declaring four devices unconditionally would make that
# nondeterministic. Unset, every existing caller behaves exactly as
# before.
#
# guest-ap is declared and never fed on purpose -- a router that has
# said nothing is part of the story the demo tells (#687).
if [ "${MV_DEMO_DEVICES:-}" = "1" ]; then
  DEVICES_BLOCK='devices: [{id: border-rb5009, name: border-rb5009, sourceIp: 127.0.0.1}, {id: office-hex, name: office-hex, sourceIp: 127.0.0.2}, {id: lab-crs, name: lab-crs, sourceIp: 127.0.0.3}, {id: guest-ap, name: guest-ap, sourceIp: 127.0.0.4}]'
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
        # Close cleanly, and only once the server has acknowledged the
        # shutdown. Without this the last write is followed straight by
        # the socket close and the tail of the burst is lost -- measured
        # at 9 of 20, 11 of 24 and 46 of 49, with the listener reporting
        # dropped=0 throughout, because the lines never reached it at
        # all. It was intermittent rather than constant, and invisible
        # for any burst that happened to be a multiple of 25: those got
        # a pause from the pacing above after their final line, and
        # delivered 100 percent every time. That is what made this look
        # like detector flakiness for so long (issue #450) -- scenarios
        # send a 20-port scan, port_scan needs 15 distinct ports, and
        # the tail going missing put it either side of the threshold
        # from one run to the next.
        #
        # unwrap() sends TLS close_notify and waits for the reply, so
        # the server has read to EOF before the socket goes away. The
        # sleep covers the parse the listener still has to do after
        # that read.
        time.sleep(0.05)
        try:
            tls.unwrap()
        except OSError:
            pass
' "$SYSLOG_TLS_HOST" "$SYSLOG_TLS_PORT"
}

build() {
  # No /dev/null here, and the exit code is checked explicitly. `set -e`
  # alone already aborted `up` here on a fresh checkout (no
  # frontend/node_modules) -- but silently, because npm's own error was
  # being thrown away with it, and because `up` is meant to be run as
  # `eval "$(scripts/live-env.sh up)"` (the Makefile's live-check target):
  # `eval "$(cmd)"` only ever sees cmd's captured *stdout*, so a cmd that
  # dies before printing any `export ...` line leaves eval nothing to run
  # but an empty string -- which eval treats as success, whatever cmd's
  # real exit code was. The Makefile's own live-routeros-container comment
  # names this same trap (#613); this was its second bite (#617). A
  # message on stderr, printed here before returning control to a caller
  # that can no longer see the exit code, is what survives it.
  #
  # 1>&2 on the subshell, not a bare call: npm's own build banner and
  # vite's asset listing print to stdout, and stdout is exactly the
  # stream `eval "$(scripts/live-env.sh up)"` captures and executes. Left
  # unredirected that output becomes shell input the moment a build
  # succeeds too -- "> vite build" read as a redirection into a command
  # named "build" is how this was actually caught, as "eval: build: not
  # found" once npm's own text stopped going to /dev/null with the error.
  # MV_DEMO_BUILD=1: ship a self-destroying service worker, so a browser
  # holding an earlier build's precached shell drops it instead of
  # serving it back (#713). Without this a fix can be in the tree, in the
  # bundle and served correctly, and still be invisible to whoever is
  # reviewing the demo.
  if ! ( cd frontend && MV_DEMO_BUILD=1 npm run build ) 1>&2; then
    echo "live-env: npm run build failed in frontend/ -- see the output above." >&2
    if [ ! -d frontend/node_modules ]; then
      echo "live-env: frontend/node_modules is missing -- run 'npm ci' in frontend/ first." >&2
    fi
    exit 1
  fi
  # touch .gitkeep for the same reason the Makefile's frontend target
  # does: rm -rf takes the only tracked file in here with it, and a live
  # check should not leave the tree dirty (#353).
  rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/ && touch web/dist/.gitkeep
  # -buildvcs=false: this binary is a throwaway built into a temp dir,
  # run by the scenarios and deleted, so nothing ever reads its VCS
  # stamp. Stamping it also fails outright in a linked git worktree --
  # "error obtaining VCS status: exit status 128" -- which took the whole
  # live check down for anyone not working in a plain clone (#348).
  # Stamp the binary so the running instance can say which build it is.
  # Without this every demo called itself "dev:local" (main.go's no-ldflags
  # fallback), so an instance built before a fix was indistinguishable in
  # the browser from one built after it -- which is how round 30 lost a
  # day to the owner reviewing a stale build and finding faults that were
  # already fixed in the tree. AGENTS.md carries the rule; this is what
  # makes it true. -buildvcs=false stays: it is what stops `go build`
  # dying in a linked worktree (#348), and the sha below is read from git
  # explicitly instead.
  mv_sha="$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"
  mv_dirty=""
  git diff --quiet HEAD 2>/dev/null || mv_dirty="-dirty"
  mv_stamp="$(cat VERSION 2>/dev/null || echo 0.0.0)+g${mv_sha}${mv_dirty}.$(date -u +%Y%m%dT%H%M%SZ)"
  go build -buildvcs=false -ldflags "-X main.version=$mv_stamp" -o "$MV_DIR/mikroview" .
  echo "live-env: built $mv_stamp" >&2
}

# True if anything is listening on the given TCP port on this host.
# Prefers ss; falls back to a bash /dev/tcp connect probe where it is
# absent (the container images used by live-container have no iproute2).
up() {
  # Always start from nothing. Without this an earlier instance keeps the
  # port, the new binary fails to bind, and the admin registration lands
  # on the *old* server -- which answers 409 and looks like a bug in the
  # thing under test rather than in the harness.
  down >/dev/null 2>&1 || true
  for _ in $(seq 1 20); do
    # --max-time, because this loop waits for our *own* previous instance
    # to stop answering. A foreign process on the port that accepts a
    # connection and never replies would otherwise hang each iteration on
    # curl's default (effectively unbounded) timeout, turning a 5-second
    # wait into minutes -- found while testing the bind check below.
    if ! curl -fsS --max-time 2 "${CURL_TLS[@]+"${CURL_TLS[@]}"}" "$MV_SCHEME://$MV_BIND:$HTTP_PORT/api/healthz" >/dev/null 2>&1; then break; fi
    sleep 0.25
  done
  # Refuse to proceed if something still holds a port after our own
  # teardown, rather than deleting $MV_DIR and racing it. Whatever is
  # listening is not ours -- `down` above already stopped anything this
  # slot started -- so it is another checkout that hashed to the same
  # slot, or an unrelated process. Both cases need a human choice, and
  # the destructive step is the very next line.
  for port in "$HTTP_PORT" "$SYSLOG_TLS_PORT"; do
    if mv_port_in_use "$port"; then
      echo "live-env: port $port is still in use after teardown." >&2
      echo "live-env: another live check is probably running from a different checkout." >&2
      echo "live-env: set MV_DIR and MV_HTTP_PORT/MV_SYSLOG_PORT/MV_SYSLOG_TLS_PORT to run alongside it." >&2
      exit 1
    fi
  done
  rm -rf "$MV_DIR"; mkdir -p "$MV_DIR/data"
  build
  # ADDING A PERSISTED STORE TO MIKROVIEW? IT NEEDS A LINE IN THE CONFIG
  # BELOW. Every store gets an explicit path under $MV_DIR/data, because
  # a store left at its /var/lib/mikroview default cannot be written by
  # this account -- and checkStoresUsable (storage_preflight.go) walks
  # backedUpStores and refuses to start on the first unusable path
  # (#536). So the moment a new store joins that list, this config has to
  # name it or nothing boots here.
  #
  # That failure is invisible to every unit test, and it does not look
  # like what it is. What you see is every scenario header followed by no
  # RESULT: line at all, each one printing "MV_URL unset -- run: eval
  # ..." -- because up() never got far enough to export MV_URL. #487's
  # setup store landed exactly that way. When up() times out, read
  # $MV_DIR/server.log first: the refusal names the store and the path.
  cat > "$MV_DIR/cfg.yaml" <<EOF
listen: {syslogTls: "$SYSLOG_TLS_ADDR", http: "$MV_BIND:$HTTP_PORT", httpRedirect: ""}
$TLS_BLOCK
$(mv_store_block "$MV_DIR/data" "$SECURE_COOKIE")
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
# raised rather than synthesized -- for scenarios (live-verdicts.mjs,
# live-flags-expectations.mjs) that need an actual flag to judge, not
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
