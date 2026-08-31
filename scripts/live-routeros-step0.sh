#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Regenerates every answer in docs/decisions/routeros-ingest-spike.md by
# asking a real router again.
#
# The transcript in that document is committed evidence, and evidence
# nobody can reproduce is just a claim with better formatting. Run this
# when a RouterOS release might have moved one of the answers -- the size
# limit and the minimum version are both the kind of thing that changes
# quietly and invalidates a schema decision downstream.
#
# Usage:
#   eval "$(MV_BIND=$(scripts/live-routeros.sh host-addr) scripts/live-env.sh up)"
#   eval "$(scripts/live-routeros.sh up)"
#   scripts/live-routeros-step0.sh
#   scripts/live-routeros.sh down && scripts/live-env.sh down
#
# CHR_VERSION=7.12 re-runs it against another release, which is how the
# 7.13 floor for :serialize was established rather than guessed.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MV_URL="${MV_URL:?run scripts/live-env.sh up first, with MV_BIND set to the host address}"
# These two used to default to 19899 and 19822. 19822 is slot 22's HTTP
# port, so this fixture claimed a port live-env.sh hands out -- the same
# defect #660 fixed in the four standalone checks, in a script that is
# opt-in and therefore was not what surfaced it. 19899 sits one below the
# allocator's standalone band and is a port rootless Docker has been seen
# publishing on this host, which would fail the same way.
#
# Both now come from live-slot.sh's own band for this fixture, derived
# from the checkout slot like everything else. An explicit
# MVCHR_SINK_PORT/MVCHR_SFTP_PORT still wins, since a RouterOS container
# reaching the host may need a port the operator chose.
. "$REPO/scripts/live-slot.sh"
SINK_PORT="${MVCHR_SINK_PORT:-$MV_ROUTEROS_SINK_PORT}"
SINK_LOG="${MVCHR_SINK_LOG:-/tmp/mikroview-chr/sink.log}"
SFTP_PORT="${MVCHR_SFTP_PORT:-$MV_ROUTEROS_SFTP_PORT}"
SFTP_CONTAINER="${MVCHR_SFTP_CONTAINER:-mv-sftp-probe}"
HOST_ADDR="$("$REPO/scripts/live-routeros.sh" host-addr)"
SINK_URL="http://$HOST_ADDR:$SINK_PORT"

ros() { "$REPO/scripts/live-routeros.sh" run "$@"; }
section() { printf '\n\n### %s\n\n' "$*"; }

# The sink exists because "the POST succeeded" is not the same claim as
# "the bytes arrived unaltered". mikroview itself has no ingest endpoint
# yet -- that is steps 1-3 -- so the only way to see what RouterOS
# actually puts on the wire is to catch it.
sink_start() {
  mkdir -p "$(dirname "$SINK_LOG")"
  : > "$SINK_LOG"
  python3 - "$SINK_PORT" "$SINK_LOG" <<'PY' &
import http.server, json, sys, threading
port, path = int(sys.argv[1]), sys.argv[2]
lock = threading.Lock()

class H(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def do_POST(self):
        n = int(self.headers.get('Content-Length') or 0)
        body = self.rfile.read(n) if n else b''
        with lock, open(path, 'a') as f:
            f.write(json.dumps({
                'path': self.path,
                'headers': dict(self.headers.items()),
                'body_len': len(body),
                'body': body[:2000].decode('utf-8', 'replace'),
            }) + '\n')
        payload = b'{"ok":true}'
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        # A payload far larger than /tool fetch will ever send, so the
        # router has something oversized to hold on disk.
        payload = json.dumps({'pad': '0123456789' * 30000}).encode()
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *a):
        pass

http.server.ThreadingHTTPServer(('0.0.0.0', port), H).serve_forever()
PY
  echo $! > /tmp/mikroview-chr/sink.pid
  sleep 1
}

sink_stop() {
  if [ -f /tmp/mikroview-chr/sink.pid ]; then
    kill "$(cat /tmp/mikroview-chr/sink.pid)" 2>/dev/null || true
  fi
  rm -f /tmp/mikroview-chr/sink.pid
}

sink_show() {
  python3 - "$SINK_LOG" "$1" <<'PY'
import json, sys
want = sys.argv[2]
for line in open(sys.argv[1]):
    rec = json.loads(line)
    if rec['path'] != want:
        continue
    print('received at %s:' % rec['path'])
    for k in ('User-Agent', 'Content-Type', 'Content-Length', 'Authorization', 'Accept-Encoding'):
        if k in rec['headers']:
            print('  %s: %s' % (k, rec['headers'][k]))
    print('  body (%d bytes): %s' % (rec['body_len'], rec['body'][:200]))
PY
}

# The SFTP server exists purely to let the router demonstrate what its
# sftp client does and does not check. mikroview ships no such thing.
sftp_start() {
  docker rm -f "$SFTP_CONTAINER" >/dev/null 2>&1 || true
  docker build --network=host -q \
    -f "$REPO/scripts/live-routeros-sftp.dockerfile" -t mv-sftp-probe:local "$REPO/scripts" >/dev/null
  docker run -d --name "$SFTP_CONTAINER" -p "$HOST_ADDR:$SFTP_PORT:22" mv-sftp-probe:local >/dev/null
  sleep 4
}

sftp_stop() {
  docker rm -f "$SFTP_CONTAINER" >/dev/null 2>&1 || true
}

cleanup() { sink_stop; sftp_stop; }
trap cleanup EXIT
sink_start

echo "# RouterOS Step 0 transcript"
echo
echo "Router: CHR ${CHR_VERSION:-7.23.3}, QEMU. mikroview: $MV_URL. Sink: $SINK_URL"

section "0. Version and licence tier"
ros ':put [/system resource get version]' ':put [/system license get level]'

section "1. Authenticated POST with a JSON body"
ros "/tool fetch url=\"$SINK_URL/q1\" http-method=post http-data=\"{\\\"hello\\\":\\\"router\\\"}\" http-header-field=\"Content-Type: application/json,Authorization: Bearer spike-token\" output=user as-value"
sink_show /q1

section "2. Payload size limit"
# 65,430 through and 65,440 refused bracket the limit. The refusal is
# client-side: nothing reaches the sink, so an oversized payload is a
# router that goes quiet rather than one that reports an error.
for n in 65430 65440; do
  echo "--- $n bytes"
  ros ":local d \"\"; :for i from=1 to=$((n / 10)) do={:set d (\$d . \"0123456789\")}; /tool fetch url=\"$SINK_URL/q2-$n\" http-method=post http-data=\$d output=user as-value" \
    | tail -3
  sink_show "/q2-$n"
done

section "2b. Whether a file on the router routes around the size limit"
# The obvious escape from a 64KiB body is to write the payload to a file
# and upload that instead. Over HTTP that is simply not on offer.
ros "/tool fetch url=\"$SINK_URL/big.json\" dst-path=big.json" | tail -4
ros '/file print where name~"big"'
ros "/tool fetch url=\"$SINK_URL/q2b\" upload=yes src-path=big.json http-method=post as-value" | tail -3
# Over SFTP it is, and it works -- so the question is not whether the
# router can, but what it costs. Two properties decide that, and both are
# measured here rather than assumed.
sftp_start
ros "/tool fetch url=\"sftp://$HOST_ADDR:$SFTP_PORT/drop/from-router.json\" user=mvingest password=ingest-pass-123 src-path=big.json upload=yes as-value" | tail -3
echo "uploaded, server side:"
docker exec "$SFTP_CONTAINER" ls -l /drop/from-router.json
docker exec "$SFTP_CONTAINER" md5sum /drop/from-router.json
echo "--- is there a key-auth parameter? (an sftp password is a reusable secret)"
ros '/tool fetch address=1.2.3.4 mode=sftp keyfile=x src-path=y upload=yes' | tail -2
echo "--- does it verify the server's host key? changing it should break the transfer"
docker exec "$SFTP_CONTAINER" sh -c 'rm -f /etc/ssh/ssh_host_* && ssh-keygen -A -q'
docker restart "$SFTP_CONTAINER" >/dev/null
sleep 4
docker exec "$SFTP_CONTAINER" ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
ros "/tool fetch url=\"sftp://$HOST_ADDR:$SFTP_PORT/drop/after-keychange.json\" user=mvingest password=ingest-pass-123 src-path=big.json upload=yes as-value" | tail -3
echo "landed despite the server identity changing?"
docker exec "$SFTP_CONTAINER" ls -l /drop/after-keychange.json 2>&1 || echo "  no -- the router refused"
sftp_stop
# And the limit is not an artifact of typing into a console: the same
# refusal comes back from a script, which is the path a scheduler uses.
ros '/system script remove [find name=mv-oversize]' >/dev/null 2>&1 || true
ros "/system script add name=mv-oversize policy=read,test source=\":local d \\\"\\\"; :for i from=1 to=6544 do={:set d (\\\$d . \\\"0123456789\\\")}; :put [:len \\\$d]; /tool fetch url=\\\"$SINK_URL/q2b-script\\\" http-method=post http-data=\\\$d output=none\"" >/dev/null
ros '/system script run mv-oversize' | tail -3

section "2c. What the budget buys, in records"
ros '/system script remove [find name=mv-rules]' >/dev/null 2>&1 || true
# shellcheck disable=SC2016  # \$i is RouterOS script syntax, not shell
# \$i has to reach RouterOS as a literal: inside a double-quoted source=
# the router expands $i at add time, which silently stores a script that
# adds nothing and reports no error.
ros '/system script add name=mv-rules policy=read,write source=":for i from=1 to=60 do={/ip/firewall/filter add chain=forward action=accept comment=(\"rule \" . \$i) src-address=(\"10.0.\" . \$i . \".0/24\")}"' >/dev/null
ros '/system script run mv-rules' >/dev/null
ros ':put ([:len [/ip/firewall/filter find]] . " rules serialise to " . [:len [:serialize to=json value=[/ip/firewall/filter print as-value]]] . " bytes")'
ros '/ip/firewall/filter remove [find comment~"rule "]' >/dev/null

section "3. Scheduler policy set, and whether it stores a credential"
ros '/system script remove [find name=mv-ingest]' \
    '/system scheduler remove [find name=mv-ingest]' >/dev/null
ros "/system script add name=mv-ingest policy=read,test source=\":local p [:serialize to=json value={ver=[/system resource get version];ifs=[/interface print as-value]}]; /tool fetch url=\\\"$SINK_URL/q3\\\" http-method=post http-data=\\\$p http-header-field=\\\"Content-Type: application/json,Authorization: Bearer sched-token\\\" output=none\"" >/dev/null
ros '/system scheduler add name=mv-ingest interval=30s policy=read,test on-event="/system script run mv-ingest"' >/dev/null
echo "waiting 70s for the scheduler to fire unattended..." >&2
sleep 70
ros '/system scheduler print detail where name=mv-ingest'
sink_show /q3

section "4. JSON assembly"
ros ':put [:serialize to=json value={a=1;b="two"}]' \
    ':put [:serialize to=json value={s="quote\"inside"}]' \
    ':put [:serialize to=json value={outer={inner={deep=1}};list={1;2;3}}]' \
    ':put [:serialize to=json value=[/ip/address print as-value]]'

section "5. Whether a read-only user can read the script source"
ros '/user remove [find name=mv-read]' >/dev/null 2>&1 || true
ros '/user add name=mv-read group=read password=readpass1' >/dev/null
"$REPO/scripts/live-routeros.sh" run-as mv-read readpass1 \
  ':put [/system script get [find name=mv-ingest] source]'
ros ':put "back as admin"' >/dev/null

section "6. TLS against mikroview's generated CA"
echo "--- with the CA removed, which is the state an operator starts in"
ros '/certificate remove [find where common-name="mikroview local CA"]' >/dev/null 2>&1 || true
# Printing the fetch's own status field rather than its body: a verified
# fetch that succeeds prints nothing at all, which reads the same as one
# that was never run.
ros ":put ([/tool fetch url=\"$MV_URL/api/healthz\" check-certificate=yes output=user as-value]->\"status\")" | tail -3
echo "--- after fetching and importing /ca.crt"
"$REPO/scripts/live-routeros.sh" trust "$MV_URL" | tail -12
# Printing the fetch's own status field rather than its body: a verified
# fetch that succeeds prints nothing at all, which reads the same as one
# that was never run.
ros ":put ([/tool fetch url=\"$MV_URL/api/healthz\" check-certificate=yes output=user as-value]->\"status\")" | tail -3
