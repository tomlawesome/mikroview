#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
#
# The scripted session scripts/record-upgrade-fixture.sh records: an
# admin, a viewer, an API token, a named entity, a watchlist entry, a
# flag, a coverage declaration, a host mark, and (v0.6.0 on) a declared
# router -- each only where the released version under test has it. It
# prints the manifest of what it created as one JSON object on stdout,
# and nothing else.
#
# It runs *inside a throwaway container sharing the recorded container's
# own network namespace*, not on the host, which is why every address
# here is 127.0.0.1 and a container port rather than a published one.
# That is not a preference: GitLab's runner hands jobs the runner host's
# Docker socket, so a `--publish`ed port lands on the runner host, where
# the job container cannot reach it (see .gitlab-ci.yml's test:container
# header). Sharing the namespace works the same way in both places.
#
# Its own file rather than a heredoc in the recording script because two
# modes now drive the identical session -- the file backend's tarball and
# the Postgres dump -- and a session that differed between them would
# make the two recordings of one release incomparable.
#
# Usage:
#   upgrade-fixture-session.py <base-url> <syslog-host> <syslog-port> \
#       <generation> <version> <syslog-mode> <declared-router>
#
# <declared-router> is "yes" when record-upgrade-fixture.sh wrote a
# config.yaml declaring DECLARED_ROUTER_ID/DECLARED_ROUTER_SOURCE_IP
# below under `devices:` -- every version from v0.6.0 on, because
# issue #1281 refuses a TLS syslog connection from an address nobody
# declared before the handshake even starts -- and "no" otherwise.

import json
import socket
import ssl
import sys
import time
import urllib.error
import urllib.request
from urllib.parse import quote

base_url, syslog_host, syslog_port, generation, version, syslog_mode, declared_router = sys.argv[1:8]
syslog_port = int(syslog_port)
declared_router = declared_router == "yes"

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

# Matches the devices: entry record-upgrade-fixture.sh writes to
# config.yaml when declared_router is true -- same id and sourceIp, so
# the manifest and the config agree about which router this was. The
# sourceIp really is loopback, not a placeholder: the syslog line below
# is sent from inside this container's own network namespace, shared
# with the recorded container, so that connection's source address is
# 127.0.0.1 regardless of version.
DECLARED_ROUTER_ID = "upgrade-fixture-router"
DECLARED_ROUTER_SOURCE_IP = "127.0.0.1"

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
            # nosemgrep: python.lang.security.audit.dynamic-urllib-use-detected.dynamic-urllib-use-detected -- base_url is the recording script's own https://127.0.0.1:<port>, not input from anyone else
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


def wait_for_health(api_base, attempts=120, delay=0.5):
    """Block until the recorded container answers /api/healthz.

    The recording script used to do this with curl on the host against a
    published port. Inside the container's own network namespace there is
    no curl and no published port, so the wait lives here, immediately
    before the first call that depends on it.
    """
    last = None
    for _ in range(attempts):
        try:
            req = urllib.request.Request(api_base + "/api/healthz", method="GET")
            # nosemgrep: python.lang.security.audit.dynamic-urllib-use-detected.dynamic-urllib-use-detected -- same fixed local address as API._req
            with urllib.request.urlopen(req, context=ssl_ctx, timeout=5) as resp:
                if resp.getcode() == 200:
                    return
        except Exception as exc:  # noqa: BLE001 -- any failure means "not yet"
            last = exc
        time.sleep(delay)
    raise SystemExit(f"container never answered {api_base}/api/healthz: {last}")


wait_for_health(base_url)
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

# 5a. Record the declared router before sending anything to it: it is
# config.yaml, not this script, that actually enrols the address (see
# record-upgrade-fixture.sh), but the syslog line below only gets
# through at all because of it, from v0.6.0 on.
if declared_router:
    manifest["declaredRouter"] = {"id": DECLARED_ROUTER_ID, "sourceIp": DECLARED_ROUTER_SOURCE_IP}

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
