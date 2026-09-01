#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""scripts/seed-demo.py -- gives a running mikroview instance a full
story, not just syslog. Issue #687: every UI review this project has run
was hampered by a demo that only sent syslog, so empty surfaces read as
UI defects when they were data gaps -- no pushed rule/NAT/address
tables, no named entities, no watchlist, a flat metrics hourline, no
admin mutations, one account. This script is the seeder that stops that
being rebuilt from memory in /tmp every session.

One estate, reused everywhere: the same router/zone/host/rule/port names
appear in the pushed tables, the syslog traffic, the named entities and
the watchlist, so a name seen on the stream is the same thing seen on
the topography and in Entities.

Every host below keeps ONE stable MAC for the life of the run. An
earlier /tmp feeder generated a fresh random MAC on most lines, which
mikroview correctly read as a fresh device every time -- measured at
4,025 "new device" flags from about 8,000 events in under half an hour,
and the owner-reported cause of a laggy UI. A MAC is part of a host's
identity alongside its name and address; this feeder treats it that way.

Usage (against an already-running instance):
  export MV_URL=https://192.168.11.30:19893
  export MV_USER=... MV_PASS=...              # never echoed by this script
  export MV_SYSLOG_HOST=127.0.0.1             # host part of the syslog TLS target
  export MV_SYSLOG_PORT=16956
  scripts/seed-demo.py push        # rule/NAT/address tables (needs ingest tokens)
  scripts/seed-demo.py entities    # named hosts/rules/ports
  scripts/seed-demo.py accounts    # a user-tier and a viewer-tier account
  scripts/seed-demo.py watchlist   # 4 entries: 2 healthy, 1 held, 1 broken ring
  scripts/seed-demo.py all         # the four above, in order
  scripts/seed-demo.py feed        # runs forever: the syslog traffic generator
  scripts/seed-demo.py mutate      # a cleared flag+note, a rename, a definition edit
                                    # (run this once the feed has produced real flags)

`feed` is the long-running piece -- start it (nohup, in the background)
and leave it running; the metrics hourline, the register and the fall's
memory only stop being flat once real time has actually passed under it.
Everything else is one-shot and safe to re-run, except `push`, which
mints a fresh ingest token per router device on every run (harmless --
old tokens can be revoked from Settings if the clutter matters) because
a token's raw value is only ever returned once and there is no
supported way to recover an earlier one.
"""
import argparse
import collections
import json
import os
import random
import socket
import ssl
import sys
import time

import requests
import urllib3

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

CSRF_HEADER = "X-Requested-With"
CSRF_VALUE = "mikroview"

# ---------------------------------------------------------------------------
# The estate. One small, plausible network: a border router with four
# internal zones, an office router, a lab router -- and a fourth,
# declared-but-silent router (see cfg's `devices:` block) so Entities'
# "quiet is a fact, not a fault" card has something real behind it.
# ---------------------------------------------------------------------------

ROUTERS = {
    "border-rb5009": {
        "src": "127.0.0.1",
        "wan": "ether1",
        "wan_addr": "203.0.113.5/29",
        "wan_net": "203.0.113.0",
        "zones": [
            ("bridge1", "192.168.11", "core"),
            ("vlan10", "192.168.10", "staff"),
            ("vlan20", "192.168.20", "iot"),
            ("vlan30", "192.168.30", "guest"),
        ],
    },
    "office-hex": {
        "src": "127.0.0.2",
        "wan": "ether1",
        "wan_addr": "203.0.113.13/29",
        "wan_net": "203.0.113.8",
        "zones": [
            ("vlan40", "192.168.40", "office"),
            ("vlan50", "192.168.50", "voip"),
            ("wlan1", "192.168.60", "wifi"),
        ],
    },
    "lab-crs": {
        "src": "127.0.0.3",
        "wan": "sfp-sfpplus1",
        "wan_addr": "203.0.113.21/29",
        "wan_net": "203.0.113.16",
        "zones": [
            ("vlan99", "172.16.5", "mgmt"),
            ("bridge-srv", "10.10.0", "servers"),
        ],
    },
    # guest-ap (127.0.0.4) is declared in cfg.yaml and deliberately never
    # touched here -- that silence is the point (#687).
}

# (router, zone, last-octet, mac, name-or-None-for-unnamed, introduce-after-seconds)
# The last two entries are deliberate newcomers: absent for the opening
# stretch of the run, so `new_device` fires for them once, on schedule,
# rather than never -- see the module docstring on stable identity.
HOSTS = [
    ("border-rb5009", "core", 10, "aa:bb:cc:01:01:01", "core-switch", 0),
    ("border-rb5009", "core", 21, "aa:bb:cc:01:01:02", "home-nas", 0),
    ("border-rb5009", "staff", 15, "aa:bb:cc:01:02:01", "tom-laptop", 0),
    ("border-rb5009", "staff", 22, "aa:bb:cc:01:02:02", "printer-office", 0),
    ("border-rb5009", "iot", 31, "aa:bb:cc:01:03:01", "kitchen-cam", 0),
    ("border-rb5009", "iot", 42, "aa:bb:cc:01:03:02", "thermostat", 0),
    ("border-rb5009", "guest", 50, "aa:bb:cc:01:04:01", "guest-phone-1", 0),
    ("office-hex", "office", 12, "aa:bb:cc:02:01:01", "reception-pc", 0),
    ("office-hex", "office", 18, "aa:bb:cc:02:01:02", "office-nas", 0),
    ("office-hex", "voip", 20, "aa:bb:cc:02:02:01", "front-desk-phone", 0),
    ("office-hex", "wifi", 30, "aa:bb:cc:02:03:01", "staff-phone-1", 0),
    ("lab-crs", "mgmt", 5, "aa:bb:cc:03:01:01", "lab-switch-mgmt", 0),
    ("lab-crs", "servers", 10, "aa:bb:cc:03:02:01", "build-server", 0),
    ("lab-crs", "servers", 11, "aa:bb:cc:03:02:02", "test-server", 0),
    ("border-rb5009", "staff", 77, "aa:bb:cc:01:02:99", None, 480),
    ("office-hex", "wifi", 66, "aa:bb:cc:02:03:99", None, 1200),
]

# Real, geolocatable public addresses (so the country column has data)
# plus a documentation-range WAN, per the brief: real ones where it
# matters for country, documentation ranges everywhere else.
PUBLIC = ["1.1.1.1", "8.8.8.8", "9.9.9.9", "208.67.222.222", "193.17.47.1",
          "195.201.10.10", "51.15.1.1", "202.12.27.33", "200.7.4.1",
          "41.79.72.1", "103.21.244.1", "185.199.108.153", "5.255.255.70",
          "202.108.22.5", "13.107.42.14", "151.101.1.140"]
SCANNER_RECURRING = "45.155.205.233"   # fires repeatedly, gaps between -- "intermittent"
SCANNER_ONE_OFF = "89.248.165.100"     # fires once, never again -- "stopped"

# Filter rules (kind=filter-rule). `fires` marks whether the feed below
# ever emits a matching line -- False gives Entities' rules tab a rule
# that is pushed but has never fired, per the brief. Every logging rule
# below carries an explicit dstPort so watchlist coverage (internal/
# engine/coverage.go) means something: a rule with no port restriction
# covers everything trivially, which would make a genuine "broken ring"
# entry impossible to construct.
# Every rule below carries the inInterface/outInterface pair the fall's
# boundary bands (#616) group by -- see frontend/src/lib/fall.svelte.ts's
# boundaryKeyOf, which keys a pushed rule and a live event on the exact
# same (chain, inInterface, outInterface) triple. An input-chain rule
# omits outInterface entirely (terminates at the router, no far side);
# every other chain carries both, using the estate's own interface names
# from ROUTERS above -- never invented ones.
FILTER_RULES = {
    "border-rb5009": [
        dict(ordinal=0, comment="LAN to WAN egress", chain="forward", action="accept",
             logPrefix="lan-to-wan", log=True, dstPort="80,443", protocol="tcp", fires=True,
             inInterface="vlan10", outInterface="ether1"),
        dict(ordinal=1, comment="Drop unsolicited WAN inbound", chain="input", action="drop",
             logPrefix="wan-in-drop", log=True, dstPort="22,23,3389,445", protocol="tcp", fires=True,
             inInterface="ether1"),
        dict(ordinal=2, comment="IoT to LAN quarantine", chain="forward", action="drop",
             logPrefix="iot-quarantine", log=True, dstPort=445, protocol="tcp", fires=True,
             inInterface="vlan20", outInterface="vlan10"),
        dict(ordinal=3, comment="Guest VLAN isolation", chain="forward", action="drop",
             logPrefix="guest-isolate", log=True, dstPort=445, protocol="tcp", fires=True,
             inInterface="vlan30", outInterface="bridge1"),
        dict(ordinal=4, comment="Allow outbound ICMP", chain="forward", action="accept",
             logPrefix="icmp-out", log=False, protocol="icmp", fires=True,
             inInterface="bridge1", outInterface="ether1"),
        dict(ordinal=5, comment="Reject invalid state", chain="forward", action="reject",
             logPrefix="invalid-drop", log=False, connectionState=["invalid"], fires=True,
             inInterface="bridge1", outInterface="ether1"),
        dict(ordinal=6, comment="Legacy PPTP VPN allow (pending removal)", chain="input", action="accept",
             logPrefix="legacy-vpn-allow", log=True, dstPort=1723, protocol="tcp", fires=False,
             inInterface="ether1"),
        dict(ordinal=7, comment="Old DMZ segment drop (decommissioned)", chain="forward", action="drop",
             logPrefix="old-dmz-rule", log=True, dstPort=139, protocol="tcp",
             dstAddress="192.168.99.0/24", fires=False,
             inInterface="ether1", outInterface="bridge1"),
    ],
    "office-hex": [
        dict(ordinal=0, comment="Office LAN egress", chain="forward", action="accept",
             logPrefix="office-out", log=True, dstPort="80,443", protocol="tcp", fires=True,
             inInterface="vlan40", outInterface="ether1"),
        dict(ordinal=1, comment="VoIP priority egress", chain="forward", action="accept",
             logPrefix="voip-priority", log=True, dstPort=5060, protocol="udp", fires=True,
             inInterface="vlan50", outInterface="ether1"),
        dict(ordinal=2, comment="Guest wifi to LAN drop", chain="forward", action="drop",
             logPrefix="wifi-guest-drop", log=True, dstPort=445, protocol="tcp", fires=True,
             inInterface="wlan1", outInterface="vlan40"),
        dict(ordinal=3, comment="Block SMB off-net", chain="forward", action="drop",
             logPrefix="smb-block", log=True, dstPort=445, protocol="tcp", fires=True,
             inInterface="vlan40", outInterface="ether1"),
        dict(ordinal=4, comment="Drop unsolicited WAN inbound", chain="input", action="drop",
             logPrefix="wan-in-drop", log=True, dstPort="22,3389", protocol="tcp", fires=True,
             inInterface="ether1"),
        dict(ordinal=5, comment="Stale ICMP diagnostic allow", chain="input", action="accept",
             logPrefix="stale-icmp-allow", log=False, protocol="icmp", fires=False,
             inInterface="ether1"),
    ],
    "lab-crs": [
        dict(ordinal=0, comment="Mgmt VLAN egress", chain="forward", action="accept",
             logPrefix="mgmt-only", log=True, dstPort="22,443", protocol="tcp", fires=True,
             inInterface="vlan99", outInterface="sfp-sfpplus1"),
        dict(ordinal=1, comment="Servers egress allow", chain="forward", action="accept",
             logPrefix="srv-allow", log=True, dstPort="80,443,3000", protocol="tcp", fires=True,
             inInterface="bridge-srv", outInterface="sfp-sfpplus1"),
        dict(ordinal=2, comment="Lab general drop", chain="forward", action="drop",
             logPrefix="lab-drop", log=True, dstPort=445, protocol="tcp", fires=True,
             inInterface="bridge-srv", outInterface="vlan99"),
        dict(ordinal=3, comment="Mgmt SSH inbound", chain="input", action="accept",
             logPrefix="mgmt-ssh-in", log=True, dstPort=22, protocol="tcp", fires=True,
             inInterface="sfp-sfpplus1"),
        dict(ordinal=4, comment="Legacy SNMP monitoring allow (unused)", chain="input", action="accept",
             logPrefix="old-snmp-rule", log=True, dstPort=161, protocol="udp", fires=False,
             inInterface="sfp-sfpplus1"),
    ],
}

NAT_RULES = {
    "border-rb5009": [
        dict(ordinal=0, comment="Masquerade WAN egress", chain="srcnat", action="masquerade",
             logPrefix="masq-wan", inInterface="bridge1", outInterface="ether1"),
        dict(ordinal=1, comment="Port-forward web", chain="dstnat", action="dst-nat",
             logPrefix="port-fwd-web", dstPort=8080, toAddresses="192.168.11.10", toPorts=8080,
             protocol="tcp", inInterface="ether1", outInterface="bridge1"),
    ],
    "office-hex": [
        dict(ordinal=0, comment="Masquerade office WAN egress", chain="srcnat", action="masquerade",
             logPrefix="masq-office", inInterface="vlan40", outInterface="ether1"),
    ],
    "lab-crs": [
        dict(ordinal=0, comment="Masquerade lab WAN egress", chain="srcnat", action="masquerade",
             logPrefix="masq-lab", inInterface="bridge-srv", outInterface="sfp-sfpplus1"),
    ],
}

IP_ADDRESSES = {
    "border-rb5009": [
        dict(address="192.168.11.1/24", network="192.168.11.0", interface="bridge1", comment="core"),
        dict(address="192.168.10.1/24", network="192.168.10.0", interface="vlan10", comment="staff"),
        dict(address="192.168.20.1/24", network="192.168.20.0", interface="vlan20", comment="iot"),
        dict(address="192.168.30.1/24", network="192.168.30.0", interface="vlan30", comment="guest"),
        dict(address="203.0.113.5/29", network="203.0.113.0", interface="ether1", comment="wan"),
    ],
    "office-hex": [
        dict(address="192.168.40.1/24", network="192.168.40.0", interface="vlan40", comment="office"),
        dict(address="192.168.50.1/24", network="192.168.50.0", interface="vlan50", comment="voip"),
        dict(address="192.168.60.1/24", network="192.168.60.0", interface="wlan1", comment="wifi"),
        dict(address="203.0.113.13/29", network="203.0.113.8", interface="ether1", comment="wan"),
    ],
    "lab-crs": [
        dict(address="172.16.5.1/24", network="172.16.5.0", interface="vlan99", comment="mgmt"),
        dict(address="10.10.0.1/24", network="10.10.0.0", interface="bridge-srv", comment="servers"),
        dict(address="203.0.113.21/29", network="203.0.113.16", interface="sfp-sfpplus1", comment="wan"),
    ],
}

# Named entities: hosts (from HOSTS' own names above), a handful of
# rules and a handful of ports -- so the stream shows the name-over-
# address pairing rather than the unnamed fallback on every row, without
# naming literally everything (the two newcomers and a couple of one-off
# public IPs stay unnamed, which is the realistic case too).
RULE_ENTITIES = {
    "lan-to-wan": "LAN Egress",
    "iot-quarantine": "IoT Quarantine",
    "wan-in-drop": "WAN Inbound Drop",
    "voip-priority": "VoIP Priority",
    "mgmt-ssh-in": "Mgmt SSH Inbound",
}
PORT_ENTITIES = {
    "443": "HTTPS", "80": "HTTP", "22": "SSH", "5060": "SIP/VoIP", "445": "SMB",
}

# Watchlist targets: two healthy (covered by a logging rule's port),
# one that will be paused ("held"), one whose port no pushed rule logs
# at all ("broken ring" -- internal/engine/coverage.go's CoverageOutOfScope).
WATCHLIST_ENTRIES = [
    dict(name="kitchen-cam-web-only", ip="192.168.20.31", ports=[80, 443], pause=False, note=None),
    dict(name="guest-phone-smb-watch", ip="192.168.30.50", ports=[445], pause=True, note=None),
    dict(name="front-desk-phone-voip", ip="192.168.50.20", ports=[5060], pause=False, note=None),
    dict(name="build-server-db-exposure", ip="10.10.0.11", ports=[3306], pause=False,
         note="no pushed filter rule logs port 3306 anywhere -- ring broken by design"),
]


def host_ip(h):
    _router, zone, octet, _mac, _name, _intro = h
    return octet, zone


def zone_subnet(router, zone):
    for _iface, subnet, name in ROUTERS[router]["zones"]:
        if name == zone:
            return subnet
    raise KeyError(zone)


def zone_iface(router, zone):
    for iface, _subnet, name in ROUTERS[router]["zones"]:
        if name == zone:
            return iface
    raise KeyError(zone)


def full_ip(h):
    router, zone, octet, *_ = h
    return f"{zone_subnet(router, zone)}.{octet}"


# ---------------------------------------------------------------------------
# Syslog TLS delivery -- same shape as scripts/live-env.sh's send_tls and
# the old /tmp feeder: raw RouterOS-style lines, one write per message,
# paced every 25 lines (the listener drops on a full channel otherwise).
# ---------------------------------------------------------------------------

def send_tls(host, port, src_ip, lines):
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    with socket.create_connection((host, port), timeout=10, source_address=(src_ip, 0)) as sock:
        sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        with ctx.wrap_socket(sock, server_hostname=host) as tls:
            for i, line in enumerate(lines):
                tls.sendall(line.encode() + b"\n")
                if i % 25 == 24:
                    time.sleep(0.01)
            time.sleep(0.05)
            try:
                tls.unwrap()
            except OSError:
                pass


def active_hosts(router, elapsed):
    return [h for h in HOSTS if h[0] == router and elapsed >= h[5]]


def lines_for_router(router, elapsed, tick):
    """One tick of coherent, moderate-volume traffic for one router --
    every src-mac comes from a host's own stable identity in HOSTS."""
    out = []
    cfg = ROUTERS[router]
    wan = cfg["wan"]
    rules = {r["logPrefix"]: r for r in FILTER_RULES[router] if r["fires"]}
    hosts = active_hosts(router, elapsed)
    if not hosts:
        return out

    def pick_host():
        return random.choice(hosts)

    # Ordinary egress on the router's LAN-egress rule, one of its own
    # covered ports (matching FILTER_RULES' dstPort exactly, so a rule
    # pushed as "covers 22,443" never shows a live hit on 80), carrying
    # the host's own MAC. Restricted to the one zone the rule's own
    # inInterface names -- FILTER_RULES and the fall's boundary bands
    # (#616, frontend/src/lib/fall.svelte.ts) key on the exact (chain,
    # inInterface, outInterface) triple, so egress from a different zone
    # would light a boundary this rule never declared.
    egress = "lan-to-wan" if router == "border-rb5009" else (
        "office-out" if router == "office-hex" else "mgmt-only")
    egress_zone = {"border-rb5009": "staff", "office-hex": "office", "lab-crs": "mgmt"}[router]
    egress_ports = {"lan-to-wan": [80, 443], "office-out": [80, 443], "mgmt-only": [22, 443]}[egress]
    egress_hosts = [h for h in hosts if h[1] == egress_zone]
    if egress in rules and egress_hosts:
        for _ in range(random.randint(2, 4)):
            h = random.choice(egress_hosts)
            ip = full_ip(h)
            pub = random.choice(PUBLIC)
            port = random.choice(egress_ports)
            out.append(f"firewall,info A|{egress}| forward: in:{zone_iface(router, egress_zone)} out:{wan}, "
                        f"connection-state:new src-mac {h[3]}, proto TCP (SYN), "
                        f"{ip}:{random.randint(1024, 65000)}->{pub}:{port}, len 60")

    # A router-specific cross-zone drop rule, continuously, so the fall
    # has a band per boundary that never goes quiet -- one of #687's own
    # "still arriving" episode shapes. Each pair is fixed (not a random
    # zone every tick) so the live in/out always matches the one
    # boundary the pushed rule itself declares.
    drop_pairs = {
        "border-rb5009": ("iot-quarantine", "iot", "staff"),
        "office-hex": ("wifi-guest-drop", "wifi", "office"),
        "lab-crs": ("lab-drop", "servers", "mgmt"),
    }
    drop, drop_src_zone, drop_dst_zone = drop_pairs[router]
    drop_hosts = [h for h in hosts if h[1] == drop_src_zone]
    if drop in rules and drop_hosts:
        for _ in range(random.randint(1, 3)):
            h = random.choice(drop_hosts)
            ip = full_ip(h)
            victim = f"{zone_subnet(router, drop_dst_zone)}.{random.randint(60, 99)}"
            out.append(f"firewall,info D|{drop}| forward: in:{zone_iface(router, drop_src_zone)} "
                        f"out:{zone_iface(router, drop_dst_zone)}, connection-state:new src-mac {h[3]}, "
                        f"proto TCP (SYN), {ip}:{random.randint(1024, 65000)}->{victim}:445, len 60")

    # WAN-inbound drop -- unsolicited traffic aimed at the router, no
    # local src-mac (matches real RouterOS: input chain from the WAN
    # side carries no LAN MAC). No "out:" token at all -- one of the two
    # real shapes RouterOS emits for input-chain lines (the other is
    # "out:(unknown 0)"; see internal/routeros/parser_test.go) -- so the
    # parsed OutInterface stays "", matching wan-in-drop's own pushed
    # outInterface (omitted, since an input rule has no far side) rather
    # than mismatching into the fall's unmatched "other traffic" band.
    if "wan-in-drop" in rules and random.random() < 0.6:
        h = pick_host()
        ip = full_ip(h)
        wan_ports = {"border-rb5009": [22, 23, 3389, 445], "office-hex": [22, 3389]}[router]
        out.append(f"firewall,info D|wan-in-drop| input: in:{wan}, "
                    f"connection-state:new, proto TCP (SYN), "
                    f"{random.choice(PUBLIC)}:{random.randint(1024, 65000)}->{ip}:{random.choice(wan_ports)}, len 60")

    # Masquerade -- one line per router per tick, so the NAT chip and
    # the pushed NAT table tell the same story. Restricted to the one
    # zone the pushed masq rule's inInterface names, for the same
    # boundary-matching reason as egress above.
    masq = {"border-rb5009": "masq-wan", "office-hex": "masq-office", "lab-crs": "masq-lab"}[router]
    masq_zone = {"border-rb5009": "core", "office-hex": "office", "lab-crs": "servers"}[router]
    masq_hosts = [h for h in hosts if h[1] == masq_zone]
    if masq_hosts:
        h = random.choice(masq_hosts)
        ip = full_ip(h)
        pub = random.choice(PUBLIC)
        out.append(f"firewall,info A|{masq}| srcnat: in:{zone_iface(router, masq_zone)} out:{wan}, proto TCP, "
                    f"{ip}:{random.randint(1024, 65000)}->{pub}:443, NAT ({pub}:12345->{pub}:443), len 73")

    # Router-specific extras.
    if router == "border-rb5009" and random.random() < 0.3:
        # port-fwd-web's toAddresses is fixed to 192.168.11.10 (core-
        # switch's own address, HOSTS octet 10) -- picking any host here
        # would DNAT-translate to an address the pushed NAT rule never
        # names, so this is core-switch specifically, not pick_host().
        core_switch = next((x for x in hosts if x[1] == "core" and x[2] == 10), None)
        if core_switch:
            out.append(f"firewall,info A|port-fwd-web| dstnat: in:{wan} out:{zone_iface(router, 'core')}, "
                        f"proto TCP (SYN), {random.choice(PUBLIC)}:{random.randint(1024, 65000)}"
                        f"->192.168.11.1:8080, NAT ->({full_ip(core_switch)}:8080), len 60")
    if router == "border-rb5009" and "guest-isolate" in rules and random.random() < 0.4:
        h = next((x for x in hosts if x[1] == "guest"), None)
        if h:
            victim = f"{zone_subnet(router, 'core')}.{random.randint(60, 99)}"
            out.append(f"firewall,info D|guest-isolate| forward: in:{zone_iface(router, 'guest')} "
                        f"out:{zone_iface(router, 'core')}, connection-state:new src-mac {h[3]}, "
                        f"proto TCP (SYN), {full_ip(h)}:{random.randint(1024, 65000)}->{victim}:445, len 60")
    if router == "lab-crs" and "srv-allow" in rules and random.random() < 0.4:
        h = next((x for x in hosts if x[1] == "servers"), None)
        if h:
            out.append(f"firewall,info A|srv-allow| forward: in:{zone_iface(router, 'servers')} out:{wan}, "
                        f"connection-state:new src-mac {h[3]}, proto TCP (SYN), "
                        f"{full_ip(h)}:{random.randint(1024, 65000)}->{random.choice(PUBLIC)}:"
                        f"{random.choice([80, 443, 3000])}, len 60")
    if router == "office-hex" and "voip-priority" in rules and random.random() < 0.5:
        h = next((x for x in hosts if x[1] == "voip"), None)
        if h:
            out.append(f"firewall,info A|voip-priority| forward: in:{zone_iface(router, 'voip')} out:{wan}, "
                        f"connection-state:new src-mac {h[3]}, proto UDP, "
                        f"{full_ip(h)}:{random.randint(1024, 65000)}->{random.choice(PUBLIC)}:5060, len 200")
    if router == "lab-crs" and "mgmt-ssh-in" in rules and random.random() < 0.2:
        h = next((x for x in hosts if x[1] == "mgmt"), None)
        if h:
            out.append(f"firewall,info A|mgmt-ssh-in| input: in:{wan}, "
                        f"connection-state:new, proto TCP (SYN), "
                        f"{random.choice(PUBLIC)}:{random.randint(1024, 65000)}->{full_ip(h)}:22, len 60")

    # The one-off port scan: fires exactly once, on the very first
    # tick, from a fixed source, on office-hex -- a "stopped" episode,
    # never repeated again for the life of the run.
    if router == "office-hex" and tick == 0:
        victim = full_ip(next(x for x in hosts if x[1] == "office"))
        for p in random.sample(range(20, 9000), 22):
            out.append(f"firewall,info D|wan-in-drop| input: in:{wan}, "
                        f"connection-state:new, proto TCP (SYN), "
                        f"{SCANNER_ONE_OFF}:{random.randint(40000, 60000)}->{victim}:{p}, len 60")

    # The recurring port scan: a small chance each tick, on
    # border-rb5009, from the same rotating source -- an "intermittent"
    # episode, gaps between re-fires.
    if router == "border-rb5009" and random.random() < 0.04:
        victim = full_ip(next(x for x in hosts if x[1] == "core"))
        for p in random.sample(range(20, 9000), random.randint(16, 24)):
            out.append(f"firewall,info D|wan-in-drop| input: in:{wan}, "
                        f"connection-state:new, proto TCP (SYN), "
                        f"{SCANNER_RECURRING}:{random.randint(40000, 60000)}->{victim}:{p}, len 60")

    return out


def cmd_feed(args):
    print(f"seed-demo feed: {len(HOSTS)} stable-identity hosts across {len(ROUTERS)} routers "
          f"-> {args.syslog_host}:{args.syslog_port}", file=sys.stderr)
    start = time.time()
    tick = 0
    while True:
        elapsed = time.time() - start
        for router, cfg in ROUTERS.items():
            lines = lines_for_router(router, elapsed, tick)
            if not lines:
                continue
            try:
                send_tls(args.syslog_host, args.syslog_port, cfg["src"], lines)
            except Exception as e:  # a demo feeder never dies
                print(f"{router}: {e}", file=sys.stderr)
        tick += 1
        if args.once:
            break
        time.sleep(random.uniform(3, 6))


# ---------------------------------------------------------------------------
# API-side seeding.
# ---------------------------------------------------------------------------

class API:
    def __init__(self, base_url, user, password):
        self.base = base_url.rstrip("/")
        self.s = requests.Session()
        self.s.verify = False
        r = self.s.post(f"{self.base}/api/auth/login",
                         json={"username": user, "password": password},
                         headers={CSRF_HEADER: CSRF_VALUE})
        r.raise_for_status()

    def get(self, path, **kw):
        r = self.s.get(f"{self.base}{path}", **kw)
        r.raise_for_status()
        return r

    def _unsafe(self, method, path, **kw):
        headers = kw.pop("headers", {})
        headers[CSRF_HEADER] = CSRF_VALUE
        r = self.s.request(method, f"{self.base}{path}", headers=headers, **kw)
        return r

    def post(self, path, ok=(200, 201), **kw):
        r = self._unsafe("POST", path, **kw)
        if r.status_code not in ok:
            raise RuntimeError(f"POST {path}: {r.status_code} {r.text[:300]}")
        return r

    def put(self, path, ok=(200,), **kw):
        r = self._unsafe("PUT", path, **kw)
        if r.status_code not in ok:
            raise RuntimeError(f"PUT {path}: {r.status_code} {r.text[:300]}")
        return r

    def delete(self, path, ok=(200, 204), **kw):
        r = self._unsafe("DELETE", path, **kw)
        if r.status_code not in ok:
            raise RuntimeError(f"DELETE {path}: {r.status_code} {r.text[:300]}")
        return r


def ingest_push(base_url, token, kind, records, page=1, pages=1):
    r = requests.post(f"{base_url.rstrip('/')}/api/ingest/routeros",
                       headers={"Authorization": f"Bearer {token}"},
                       json={"kind": kind, "page": page, "pages": pages, "records": records},
                       verify=False)
    if r.status_code != 200:
        raise RuntimeError(f"ingest {kind}: {r.status_code} {r.text[:300]}")
    return r.json()


def cmd_push(args):
    api = API(args.url, args.user, args.password)
    for router in FILTER_RULES:
        tok = api.post("/api/tokens", json={"name": f"{router}-ingest-seed", "kind": "ingest",
                                             "device": router}).json()["value"]
        filt = [{k: v for k, v in r.items() if k != "fires"} for r in FILTER_RULES[router]]
        ack = ingest_push(args.url, tok, "filter-rule", filt)
        print(f"{router}: pushed {ack['records']} filter rules")
        ack = ingest_push(args.url, tok, "nat-rule", NAT_RULES[router])
        print(f"{router}: pushed {ack['records']} NAT rules")
        ack = ingest_push(args.url, tok, "ip-address", IP_ADDRESSES[router])
        print(f"{router}: pushed {ack['records']} address table entries")


def cmd_entities(args):
    api = API(args.url, args.user, args.password)
    n = 0
    for h in HOSTS:
        if h[4] is None:
            continue
        api.post("/api/entities", json={"type": "host", "key": full_ip(h), "label": h[4], "tags": [h[1]]})
        n += 1
    for prefix, label in RULE_ENTITIES.items():
        api.post("/api/entities", json={"type": "rule", "key": prefix, "label": label, "tags": []})
        n += 1
    for port, label in PORT_ENTITIES.items():
        api.post("/api/entities", json={"type": "port", "key": port, "label": label, "tags": []})
        n += 1
    print(f"seeded {n} named entities")


DEMO_USER_PASSWORD = "atlas-review-user-2026"
DEMO_VIEWER_PASSWORD = "atlas-review-viewer-2026"


def cmd_accounts(args):
    api = API(args.url, args.user, args.password)
    for username, password, role in [
        ("reviewer", DEMO_USER_PASSWORD, "user"),
        ("observer", DEMO_VIEWER_PASSWORD, "viewer"),
    ]:
        r = api._unsafe("POST", "/api/auth/users",
                         json={"username": username, "password": password, "role": role})
        if r.status_code == 201:
            print(f"created {role} account {username!r}")
        elif r.status_code == 409:
            print(f"{role} account {username!r} already exists, skipping")
        else:
            raise RuntimeError(f"create {username}: {r.status_code} {r.text[:300]}")
    # Never printed: written once to a local, uncommitted reference file
    # next to the admin's own credentials.txt, same convention.
    ref = "/tmp/mikroview-atlas-demo/seeded-accounts.txt"
    try:
        with open(ref, "w") as f:
            f.write(f"reviewer(user)={DEMO_USER_PASSWORD}\nobserver(viewer)={DEMO_VIEWER_PASSWORD}\n")
        print(f"credentials written to {ref} (not printed, not committed)")
    except OSError as e:
        print(f"could not write {ref}: {e}", file=sys.stderr)


def cmd_watchlist(args):
    api = API(args.url, args.user, args.password)
    for entry in WATCHLIST_ENTRIES:
        body = {"name": entry["name"], "expectation": {
            "source": {"mac": "", "ip": entry["ip"]},
            "sourceList": {"device": "", "list": ""},
            "destIp": "", "ports": entry["ports"], "invert": False,
            "includeStructuralNoise": False,
        }}
        r = api.post("/api/definitions", json=body, ok=(200, 201, 409))
        if r.status_code == 409:
            print(f"watchlist entry {entry['name']!r} already exists, skipping")
            continue
        wid = r.json()["id"]
        if entry["pause"]:
            api.put(f"/api/definitions/{wid}", json={"enabled": False})
            print(f"created and paused (held) watchlist entry {entry['name']!r}")
        else:
            print(f"created watchlist entry {entry['name']!r}"
                  + (" (broken ring by design)" if entry["note"] else ""))


def cmd_mutate(args):
    api = API(args.url, args.user, args.password)

    # 1) A cleared flag with a note.
    flags = api.get("/api/flags").json()["flags"]
    target = next((f for f in flags if not f.get("cleared")), None)
    if target:
        api.post(f"/api/flags/{target['id']}/clear",
                  json={"note": "reviewed -- expected traffic for this host, clearing with context"})
        print(f"cleared flag {target['id']} ({target['type']}) with a note")
    else:
        print("no active flag found to clear yet -- run this again once `feed` has produced one",
              file=sys.stderr)

    # 2) A rename: the NAS host entity, re-upserted under a new label.
    nas = next(h for h in HOSTS if h[4] == "home-nas")
    api.post("/api/entities", json={"type": "host", "key": full_ip(nas), "label": "home-nas-01", "tags": ["core"]})
    print("renamed entity home-nas -> home-nas-01")

    # 3) A definition change: nudge port_scan's own threshold.
    defs = api.get("/api/definitions").json()
    items = defs.get("definitions", defs if isinstance(defs, list) else [])
    port_scan = next((d for d in items if d.get("id") == "port_scan"), None)
    if port_scan:
        params = dict(port_scan.get("params") or {})
        if "threshold" in params:
            params["threshold"] = params["threshold"] + 1
        api.put("/api/definitions/port_scan", json={"params": params})
        print(f"edited port_scan definition params -> {params}")
    else:
        print("could not find the port_scan definition to edit", file=sys.stderr)


def cmd_all(args):
    cmd_push(args)
    cmd_entities(args)
    cmd_accounts(args)
    cmd_watchlist(args)


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--url", default=os.environ.get("MV_URL", "https://127.0.0.1:19893"))
    p.add_argument("--user", default=os.environ.get("MV_USER"))
    p.add_argument("--password", default=os.environ.get("MV_PASS"))
    p.add_argument("--syslog-host", default=os.environ.get("MV_SYSLOG_HOST", "127.0.0.1"))
    p.add_argument("--syslog-port", type=int, default=int(os.environ.get("MV_SYSLOG_PORT", "16956")))
    p.add_argument("--once", action="store_true", help="feed: send one tick per router and exit")
    sub = p.add_subparsers(dest="cmd", required=True)
    for name, fn in [("push", cmd_push), ("entities", cmd_entities), ("accounts", cmd_accounts),
                      ("watchlist", cmd_watchlist), ("feed", cmd_feed), ("mutate", cmd_mutate),
                      ("all", cmd_all)]:
        sp = sub.add_parser(name)
        sp.set_defaults(func=fn)
    args = p.parse_args()
    if args.cmd != "feed" and (not args.user or not args.password):
        p.error("MV_USER/MV_PASS (or --user/--password) are required for this command")
    args.func(args)


if __name__ == "__main__":
    main()
