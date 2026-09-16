#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# scripts/record-upgrade-fixture.sh <version> -- boots the *released*
# image ghcr.io/tomlawesome/mikroview:<version> on an empty data
# directory, drives a small scripted session against its own API (an
# admin, a viewer, an API token, a named entity, a watchlist entry, a
# flag, a coverage declaration, a host mark -- each only where that
# version's API has it), stops it, and packs what it wrote as
# .upgrade-fixtures/upgrade-fixture-<version>.tar.gz. It also writes
# testdata/upgrade/<version>/manifest.json (what was created -- no
# hashes, tokens, keys or passwords, see docs/upgrades.md) and uploads
# the tarball to the project's generic package registry.
#
# This is the ONE time an old image runs (docs/decisions/
# upgrade-framework.md, "Proof is recordings, not booted images"). The
# routine gate never boots one; it only opens what this script recorded,
# via scripts/fetch-upgrade-fixtures.sh and internal/persist/
# upgrade_test.go.
#
# Usage:
#   scripts/record-upgrade-fixture.sh v0.3.0
#
# Requires docker and, for the upload step, `glab` already configured
# against the GitLab host that hosts this project (see
# ~/.config/agents/skills/github-credentials -- this script does not set
# GLAB_CONFIG_DIR/GITLAB_HOST itself, it uses whatever the caller's shell
# already has). Set MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 to record and
# pack locally without uploading (used by the "tiny file" upload test and
# by anyone who wants to inspect a recording before it goes anywhere).
set -euo pipefail

VERSION="${1:?usage: scripts/record-upgrade-fixture.sh <version>, e.g. v0.3.0}"
VERSION_BARE="${VERSION#v}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="ghcr.io/tomlawesome/mikroview:${VERSION}"
CONTAINER_NAME="upgrade-fixture-${VERSION}"
VOLUME_NAME="upgrade-fixture-data-${VERSION}"
GITLAB_PROJECT_ID="${MIKROVIEW_GITLAB_PROJECT_ID:-53}"

FIXTURE_DIR="$ROOT/.upgrade-fixtures"
MANIFEST_DIR="$ROOT/testdata/upgrade/${VERSION}"
TARBALL="$FIXTURE_DIR/upgrade-fixture-${VERSION}.tar.gz"

log() { echo "record-upgrade-fixture[$VERSION]: $*" >&2; }

WORKDIR=""
cleanup() {
  docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  docker volume rm -f "$VOLUME_NAME" >/dev/null 2>&1 || true
  [ -n "$WORKDIR" ] && rm -rf "$WORKDIR"
}
trap cleanup EXIT

# version_ge A B -- true if version A is >= B, both without the leading v.
version_ge() {
  [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]
}

# Feature generations, from reading each tag's internal/api/server.go
# (#1239): POST /api/definitions (the watchlist) landed at v0.3.0; GET/PUT
# /api/coverage/declarations and /api/hosts/{key}/mark landed at v0.5.0.
# The same v0.5.0 boundary also turned on #853 (state-store encryption
# under history.keyFile) -- without a key mounted, flags/entities/
# watchlist/coverage/hosts are memory-only on those two versions and
# nothing would be left to pack.
HAS_WATCHLIST=false
HAS_COVERAGE=false
NEEDS_HISTORY_KEY=false
version_ge "$VERSION_BARE" "0.3.0" && HAS_WATCHLIST=true
version_ge "$VERSION_BARE" "0.5.0" && { HAS_COVERAGE=true; NEEDS_HISTORY_KEY=true; }
GENERATION="A"
$HAS_WATCHLIST && GENERATION="B"
$HAS_COVERAGE && GENERATION="C"

# v0.1.0 is syslog UDP/TCP on :1514, plain -- issue #188 (TLS on :6514)
# landed at v0.2.0. Every later tag speaks RouterOS's remote-protocol=tls
# on :6514, which is what every other generation here assumes.
SYSLOG_CONTAINER_PORT=6514
SYSLOG_MODE="tls"
if [ "$VERSION_BARE" = "0.1.0" ]; then
  SYSLOG_CONTAINER_PORT=1514
  SYSLOG_MODE="plain"
fi

log "generation $GENERATION (watchlist=$HAS_WATCHLIST coverage/hosts=$HAS_COVERAGE history-key=$NEEDS_HISTORY_KEY syslog=$SYSLOG_MODE)"

next_free_port() {
  local p="$1"
  while docker ps --format '{{.Ports}}' 2>/dev/null | grep -q ":$p->" ; do
    p=$((p + 1))
  done
  echo "$p"
}

WORKDIR="$(mktemp -d)"
HTTP_PORT="$(next_free_port 31000)"
SYSLOG_PORT="$(next_free_port $((HTTP_PORT + 1)))"
log "using host ports $HTTP_PORT (https), $SYSLOG_PORT (syslog, $SYSLOG_MODE)"

docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker volume rm -f "$VOLUME_NAME" >/dev/null 2>&1 || true

RUN_ARGS=(-d --name "$CONTAINER_NAME"
  -v "$VOLUME_NAME:/var/lib/mikroview"
  -p "127.0.0.1:${HTTP_PORT}:8080"
  -p "127.0.0.1:${SYSLOG_PORT}:${SYSLOG_CONTAINER_PORT}/tcp")

if $NEEDS_HISTORY_KEY; then
  # >= 32 bytes, per docs/configuration.md's history.keyFile section.
  # Not a real secret: it exists only to make this one throwaway
  # recording's fake accounts/flags/watchlist legible to the fixture
  # tarball's own key file, packed alongside the data directory below
  # (never committed -- see the .gitignore entry this script's own
  # header points at).
  head -c 32 /dev/urandom | base64 > "$WORKDIR/history.key"
  chmod 644 "$WORKDIR/history.key"
  RUN_ARGS+=(-v "$WORKDIR/history.key:/etc/mikroview/history.key:ro"
    -e MIKROVIEW_HISTORY_KEY_FILE=/etc/mikroview/history.key)
fi

log "starting $IMAGE"
docker run "${RUN_ARGS[@]}" "$IMAGE" >/dev/null

BASE_URL="https://127.0.0.1:${HTTP_PORT}"
for _ in $(seq 1 60); do
  if curl -fsSk "$BASE_URL/api/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
if ! curl -fsSk "$BASE_URL/api/healthz" >/dev/null 2>&1; then
  log "container never became healthy; last 40 log lines:"
  docker logs "$CONTAINER_NAME" 2>&1 | tail -40 >&2
  exit 1
fi
log "container healthy"

log "driving the scripted session"
set +e
python3 - "$BASE_URL" "127.0.0.1" "$SYSLOG_PORT" "$GENERATION" "$VERSION" "$SYSLOG_MODE" \
  > "$WORKDIR/session.json" <<'PY'
import json
import socket
import ssl
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import quote

base_url, syslog_host, syslog_port, generation, version, syslog_mode = sys.argv[1:7]
syslog_port = int(syslog_port)

CSRF_HEADER = "X-Requested-With"
CSRF_VALUE = "mikroview"

# Every credential and name this records is an obvious placeholder
# (owner ruling, docs/upgrades.md): never a real one, and identical
# across every recording so a reviewer only has to learn it once.
FIXTURE_ADMIN = "upgrade-fixture-admin"
FIXTURE_VIEWER = "upgrade-fixture-viewer"
FIXTURE_PASSWORD = "fixture-only-not-real"

# HOST_IP has to be a private (non-public) address for the v0.5.0+ host
# register to keep it at all (internal/hosts.isPublicIP) -- 172.16/12 is
# picked deliberately over 192.168.0.0/16 or 10.0.0.0/8, which is what
# this script's own real-address check greps for (the owner's own
# network is 192.168.x, per docs/decisions/upgrade-framework.md).
# DEST_IP is a plain RFC 5737 documentation address, never dereferenced
# as a host.
HOST_IP = "172.20.30.40"
HOST_MAC = "aa:bb:cc:00:ff:10"
IFACE = "ether1"
DEST_IP = "198.51.100.10"

ssl_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
ssl_ctx.check_hostname = False
ssl_ctx.verify_mode = ssl.CERT_NONE


class API:
    def __init__(self, base):
        self.base = base
        self.cookie = None

    def _req(self, method, path, body=None):
        url = self.base + path
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Content-Type": "application/json"}
        if method != "GET":
            headers[CSRF_HEADER] = CSRF_VALUE
        if self.cookie:
            headers["Cookie"] = self.cookie
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            resp = urllib.request.urlopen(req, context=ssl_ctx, timeout=15)
        except urllib.error.HTTPError as e:
            return e.code, e.read(), e.headers
        set_cookie = resp.headers.get("Set-Cookie")
        if set_cookie:
            self.cookie = set_cookie.split(";")[0]
        return resp.getcode(), resp.read(), resp.headers

    def post(self, path, body, ok=(200, 201)):
        sc, b, _ = self._req("POST", path, body)
        if sc not in ok:
            raise RuntimeError(f"POST {path}: {sc} {b[:300]!r}")
        return json.loads(b) if b else {}

    def put(self, path, body, ok=(200,)):
        sc, b, _ = self._req("PUT", path, body)
        if sc not in ok:
            raise RuntimeError(f"PUT {path}: {sc} {b[:300]!r}")
        return json.loads(b) if b else {}


api = API(base_url)
manifest = {"version": version, "recordedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}

# 1. The permanent admin, created by the same bootstrap endpoint the
# setup wizard's first screen calls -- this also opens the session
# every later call in this script uses.
api.post("/api/auth/register", {"username": FIXTURE_ADMIN, "password": FIXTURE_PASSWORD})
manifest["adminUsername"] = FIXTURE_ADMIN

# 2. A second, viewer-tier account.
api.post("/api/auth/users", {"username": FIXTURE_VIEWER, "password": FIXTURE_PASSWORD, "role": "viewer"})
manifest["viewerUsername"] = FIXTURE_VIEWER

# 3. A read-only API token. Its raw value is returned exactly once and
# is never written to the manifest -- only that one was created.
api.post("/api/tokens", {"name": "upgrade-fixture-token", "kind": "api"})
manifest["apiTokenName"] = "upgrade-fixture-token"

# 4. A named entity.
api.post("/api/entities", {"type": "host", "key": HOST_IP, "label": "upgrade-fixture-host", "tags": ["fixture"]})
manifest["entity"] = {"type": "host", "key": HOST_IP, "label": "upgrade-fixture-host"}

# 5. A watchlist entry (POST /api/definitions), from v0.3.0 on.
if generation in ("B", "C"):
    body = {
        "name": "upgrade-fixture-watch",
        "expectation": {
            "source": {"mac": "", "ip": HOST_IP},
            "sourceList": {"device": "", "list": ""},
            "destIp": "",
            "ports": [443],
            "invert": False,
            "includeStructuralNoise": False,
        },
    }
    api.post("/api/definitions", body)
    manifest["watchlist"] = {"name": "upgrade-fixture-watch", "ports": [443]}

# 6. One syslog line, which raises a deterministic `new_device` flag
# (first-ever traffic from HOST_MAC -- internal/flags.TypeNewDevice has
# worked this way since v0.1.0) and, from v0.5.0 on, also registers the
# host in the presence register a mark needs to exist first.
line = (
    f"firewall,info A|upgrade-fixture| forward: in:{IFACE} out:bridge1, "
    f"connection-state:new src-mac {HOST_MAC}, proto TCP (SYN), "
    f"{HOST_IP}:40000->{DEST_IP}:443, len 60"
)
if syslog_mode == "tls":
    with socket.create_connection((syslog_host, syslog_port), timeout=10) as sock:
        sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        with ssl_ctx.wrap_socket(sock, server_hostname=syslog_host) as tls:
            tls.sendall(line.encode() + b"\n")
            time.sleep(0.5)
            try:
                tls.unwrap()
            except OSError:
                pass
else:
    # v0.1.0 only: plain syslog TCP on :1514, no TLS (issue #188 added
    # the TLS listener at v0.2.0).
    with socket.create_connection((syslog_host, syslog_port), timeout=10) as sock:
        sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        sock.sendall(line.encode() + b"\n")
        time.sleep(0.5)
time.sleep(1.5)
manifest["flag"] = {"type": "new_device", "target": HOST_MAC}

# 7. A coverage declaration and a host mark, from v0.5.0 on.
if generation == "C":
    coverage_key = "upgrade-fixture-boundary"
    api.put(f"/api/coverage/declarations/{quote(coverage_key, safe='')}", {"reason": "upgrade-fixture-coverage-reason"})
    manifest["coverage"] = {"key": coverage_key, "reason": "upgrade-fixture-coverage-reason"}

    host_key = f"{IFACE}|{HOST_IP}"
    api.put(f"/api/hosts/{quote(host_key, safe='')}/mark", {"kind": "intended", "reason": "upgrade-fixture-host-reason"})
    manifest["hostMark"] = {"key": host_key, "kind": "intended", "reason": "upgrade-fixture-host-reason"}

print(json.dumps(manifest))
PY

SESSION_OK=$?
set -e
if [ "$SESSION_OK" -ne 0 ] || [ ! -s "$WORKDIR/session.json" ]; then
  log "scripted session failed; last 40 container log lines:"
  docker logs "$CONTAINER_NAME" 2>&1 | tail -40 >&2
  exit 1
fi
log "session recorded: $(cat "$WORKDIR/session.json")"

log "stopping container (graceful, so write-behind stores flush)"
docker stop --time 20 "$CONTAINER_NAME" >/dev/null

log "packing the data directory"
STAGE="$WORKDIR/stage"
mkdir -p "$STAGE/data"
# The volume's files are owned by uid 65532 (the image's own runtime
# user -- see Dockerfile's USER line at every one of these tags), which
# `cp -a` carries over as-is. Rootless docker maps container uid 0 to
# this host's own invoking user, so chowning to 0:0 before copying out
# is what makes the copy readable (and later removable) outside the
# container -- without it every later step here fails with "permission
# denied" reading its own temp directory.
docker run --rm -v "${VOLUME_NAME}:/data:ro" -v "$STAGE/data:/out" alpine \
  sh -c 'cp -a /data/. /out/ && chown -R 0:0 /out'
if $NEEDS_HISTORY_KEY; then
  cp "$WORKDIR/history.key" "$STAGE/history.key"
fi

# Never a real address or the owner's own hostname -- see the
# generation-A/B/C comment above for the documentation ranges this
# script uses instead.
if grep -rEq '192\.168\.|(^|[^0-9])10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|tomlawson' "$STAGE"; then
  log "refusing to pack: a real-looking address or hostname pattern was found under $STAGE"
  grep -rEn '192\.168\.|(^|[^0-9])10\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}|tomlawson' "$STAGE" >&2 || true
  exit 1
fi

mkdir -p "$FIXTURE_DIR"
tar -C "$STAGE" -czf "$TARBALL" .
log "packed $TARBALL ($(du -h "$TARBALL" | cut -f1))"

mkdir -p "$MANIFEST_DIR"
python3 -c '
import json, sys
manifest = json.loads(sys.argv[1])
manifest["historyKeyEncrypted"] = sys.argv[2] == "true"
print(json.dumps(manifest, indent=2, sort_keys=True))
' "$(cat "$WORKDIR/session.json")" "$NEEDS_HISTORY_KEY" > "$MANIFEST_DIR/manifest.json"
log "wrote $MANIFEST_DIR/manifest.json"

if [ "${MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD:-}" = "1" ]; then
  log "MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 set -- not uploading"
  exit 0
fi

log "uploading to the generic package registry (project $GITLAB_PROJECT_ID, package upgrade-fixtures/$VERSION)"
glab api -X PUT \
  "projects/${GITLAB_PROJECT_ID}/packages/generic/upgrade-fixtures/${VERSION}/upgrade-fixture-${VERSION}.tar.gz" \
  --input "$TARBALL"
log "done"
