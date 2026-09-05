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

Since #870 that estate has a second half: the round-40 city story
(`docs/design/screens/city/DESIGN.md`, ratified on #854) added as two
more routers -- `rb5009` and `hap-ax3` -- alongside the original three
rather than renamed over them, because #709's device block in
scripts/live-env.sh is keyed by router id and the original names are the
only thing that binds a pushed table to a device. Which block feeds
which part of the city story is written against each one, under the
ROUND-40 banner below.

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
  scripts/seed-demo.py watchlist   # 6 entries: 4 healthy, 1 held, 1 broken ring
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

# ---------------------------------------------------------------------------
# ROUND-40: the city's own data story (#870), added to the estate above
# rather than renamed over it. `docs/design/screens/city/DESIGN.md` is the
# ratified record; `docs/design/concepts/round-40/BRIEF.md` §data story is
# the same story in the mockup's words. Two more routers, so five in all.
#
# What each block below feeds, in the city's terms:
#
#   ROUND40 (routers)   the two boroughs. rb5009 is the primary; hap-ax3's
#                       own WAN address is 10.0.10.9 -- inside rb5009's LAN
#                       -- so the second borough is reached by a road from
#                       the LAN district rather than from the river.
#   ROUND40_HOSTS       the buildings, one per named device in the story,
#                       plus hap-ax3 itself as a building in the LAN
#                       district (the near end of that road).
#   FILTER_RULES        the walls and gates. A gate is a forward-chain
#                       accept on an (inInterface, outInterface) pair; a
#                       lamp on it is log=True. frontend/src/lib/
#                       policy.svelte.ts keys on exactly that pair, and
#                       Topography.svelte's zoneCaption reads a district's
#                       badge off the two pairs it makes with the WAN. So
#                       the DARK districts are DARK here by *omission of
#                       log*, never by omission of a rule: IoT's three
#                       rules toward ether1 all carry log=False (DARK
#                       TOWARD WAN), Guest's and Cams' carry log=False in
#                       both directions (DARK), and everything the story
#                       calls LOGGED has a logging rule each way.
#   NAT_RULES           hap-ax3 masquerades its two districts behind
#                       10.0.10.9, which is why workshop -> servers arrives
#                       at rb5009 as traffic from that one address.
#   IP_ADDRESSES        the district plaques: zones.svelte.ts takes a
#                       zone's name from an entry's `comment` and its CIDR
#                       from `address`, and draws a district from the push
#                       alone -- which is how Guest and Cams exist on the
#                       map at all while emitting no traffic.
#   WIREGUARD_*         the wg0 footbridge and its far-bank hamlet
#                       (phone-tom-away). Public keys only, and obviously
#                       fake ones: a real router never shows a private key
#                       to a read,test script either.
#   lines_for_round40   the roads. LAN <-> Servers heavy on :53 :123 :445
#                       :5001; workshop -> servers backups on :445; wg0 a
#                       trickle (QUIET); l2tp about 0.3/s; and the one
#                       alarm road -- UNPLANNED iot -> lan tcp/445, 14x,
#                       caught by the default drop.
#
# NOT here, because today's ingest schema has no field for it (#874):
# tunnel up/down state. `wireguard-peer` carries no lastHandshake and
# there is no ppp-active kind, so wg0's QUIET and l2tp's UP are told the
# only way that is honest today -- by the traffic on those interfaces and
# by the addresses the interfaces carry.
# ---------------------------------------------------------------------------

ROUND40 = ("rb5009", "hap-ax3")

ROUTERS.update({
    "rb5009": {
        "src": "127.0.0.5",
        "wan": "ether1",
        "wan_addr": "203.0.113.7/29",
        "wan_net": "203.0.113.0",
        "zones": [
            ("bridge-lan", "10.0.10", "lan"),
            ("vlan-srv", "10.0.20", "servers"),
            ("vlan-iot", "10.0.30", "iot"),
            ("vlan-guest", "10.0.40", "guest"),
        ],
    },
    # Its ether1 is not on the river: 10.0.10.9 is an address in rb5009's
    # own LAN, so this borough hangs off the LAN district's road.
    "hap-ax3": {
        "src": "127.0.0.6",
        "wan": "ether1",
        "wan_addr": "10.0.10.9/24",
        "wan_net": "10.0.10.0",
        "zones": [
            ("bridge-workshop", "10.0.50", "workshop"),
            ("vlan-cams", "10.0.60", "cams"),
        ],
    },
})

# Same tuple shape as HOSTS above, appended to it. The Guest and Cams
# hosts never emit a line -- their districts have no logging rule, and a
# district mikroview has never heard from is exactly what an unlit plate
# means. They are still named entities, because the operator knows they
# are there; the map's silence about them is the honest part.
HOSTS += [
    # rb5009 -- LAN. hap-ax3 is a building here as well as a borough.
    ("rb5009", "lan", 9, "aa:bb:cc:40:01:09", "hap-ax3", 0),
    ("rb5009", "lan", 20, "aa:bb:cc:40:01:20", "tom-desktop", 0),
    ("rb5009", "lan", 21, "aa:bb:cc:40:01:21", "phone-tom", 0),
    ("rb5009", "lan", 22, "aa:bb:cc:40:01:22", "laptop-anna", 0),
    ("rb5009", "lan", 23, "aa:bb:cc:40:01:23", "tv-lounge", 0),
    # rb5009 -- Servers.
    ("rb5009", "servers", 10, "aa:bb:cc:40:02:10", "nas", 0),
    ("rb5009", "servers", 11, "aa:bb:cc:40:02:11", "pihole", 0),
    ("rb5009", "servers", 12, "aa:bb:cc:40:02:12", "unifi", 0),
    # rb5009 -- IoT. cam-porch is the one that starts the alarm road.
    ("rb5009", "iot", 31, "aa:bb:cc:40:03:31", "cam-porch", 0),
    ("rb5009", "iot", 32, "aa:bb:cc:40:03:32", "hue-bridge", 0),
    # "thermostat-hall" rather than the story's bare "thermostat": the
    # original estate above already has a host by that name, and two
    # entities with one label would break this file's own promise that a
    # name on the stream is one thing.
    ("rb5009", "iot", 33, "aa:bb:cc:40:03:33", "thermostat-hall", 0),
    ("rb5009", "iot", 34, "aa:bb:cc:40:03:34", "doorbell", 0),
    ("rb5009", "iot", 35, "aa:bb:cc:40:03:35", "esp-weather", 0),
    ("rb5009", "iot", 36, "aa:bb:cc:40:03:36", "plug-kettle", 0),
    ("rb5009", "iot", 37, "aa:bb:cc:40:03:37", None, 0),
    ("rb5009", "iot", 38, "aa:bb:cc:40:03:38", None, 0),
    # rb5009 -- Guest: named, never heard from (no logging rule).
    ("rb5009", "guest", 50, "aa:bb:cc:40:04:50", "guest-e8b2", 0),
    ("rb5009", "guest", 51, "aa:bb:cc:40:04:51", None, 0),
    # A guest that appears once and never returns -- #738 item 3's "guest
    # device that appears once", distinct from a plain newcomer (which
    # joins once and then stays for the rest of the run). See
    # HOST_PRESENCE above: this mac is active for one bounded window only.
    ("rb5009", "guest", 53, "aa:bb:cc:40:04:53", None, 0),
    # hap-ax3 -- Workshop.
    ("hap-ax3", "workshop", 10, "aa:bb:cc:40:05:10", "cnc", 0),
    ("hap-ax3", "workshop", 11, "aa:bb:cc:40:05:11", "printer-3d", 0),
    ("hap-ax3", "workshop", 12, "aa:bb:cc:40:05:12", "pc-bench", 0),
    # hap-ax3 -- Cams: named, never heard from (no logging rule).
    ("hap-ax3", "cams", 20, "aa:bb:cc:40:06:20", "cam-yard", 0),
    ("hap-ax3", "cams", 21, "aa:bb:cc:40:06:21", "cam-gate", 0),
]

# rb5009's 41 rules, in the order a real /ip/firewall/filter print shows
# them: input chain first (protect the router), then forward. Ordinal is
# display order only -- logPrefix is what a log line resolves back to.
#
# Read the log= column as the city's lamps. Every logging rule carries an
# explicit dstPort for the same reason the original estate's do (see
# FILTER_RULES' own note above): a port-less logging rule makes every
# watchlist entry read "covered" in internal/engine/coverage.go.
FILTER_RULES["rb5009"] = [
    dict(ordinal=0, comment="Accept established and related", chain="input", action="accept",
         logPrefix="r40-in-established", log=False, connectionState=["established", "related"],
         fires=False, inInterface="ether1"),
    dict(ordinal=1, comment="Drop invalid", chain="input", action="drop",
         logPrefix="r40-in-invalid", log=False, connectionState=["invalid"],
         fires=False, inInterface="ether1"),
    dict(ordinal=2, comment="Allow ICMP from LAN", chain="input", action="accept",
         logPrefix="r40-in-icmp", log=False, protocol="icmp", fires=False,
         inInterface="bridge-lan"),
    dict(ordinal=3, comment="Winbox from LAN", chain="input", action="accept",
         logPrefix="r40-winbox-lan", log=True, dstPort=8291, protocol="tcp", fires=True,
         inInterface="bridge-lan"),
    dict(ordinal=4, comment="SSH from LAN", chain="input", action="accept",
         logPrefix="r40-ssh-lan", log=True, dstPort=22, protocol="tcp", fires=True,
         inInterface="bridge-lan"),
    dict(ordinal=5, comment="DNS from LAN to the router", chain="input", action="accept",
         logPrefix="r40-dns-lan", log=False, dstPort=53, protocol="udp", fires=False,
         inInterface="bridge-lan"),
    dict(ordinal=6, comment="DNS from IoT to the router", chain="input", action="accept",
         logPrefix="r40-dns-iot", log=False, dstPort=53, protocol="udp", fires=False,
         inInterface="vlan-iot"),
    # The two tunnel listeners. Unlogged on purpose: a handshake every few
    # minutes is not the story, the traffic inside the tunnel is.
    dict(ordinal=7, comment="WireGuard wg0 listener", chain="input", action="accept",
         logPrefix="r40-wg-listen", log=False, dstPort=51820, protocol="udp", fires=False,
         inInterface="ether1"),
    dict(ordinal=8, comment="L2TP server listener", chain="input", action="accept",
         logPrefix="r40-l2tp-listen", log=False, dstPort=1701, protocol="udp", fires=False,
         inInterface="ether1"),
    dict(ordinal=9, comment="Drop unsolicited WAN inbound", chain="input", action="drop",
         logPrefix="r40-wan-in-drop", log=True, dstPort="22,23,445,3389,8291", protocol="tcp",
         fires=True, inInterface="ether1"),

    # LAN's gates. LOGGED BOTH WAYS toward the WAN needs a logging rule on
    # bridge-lan|ether1 (below) and one on ether1|bridge-lan (ordinal 17).
    dict(ordinal=10, comment="LAN to WAN web", chain="forward", action="accept",
         logPrefix="r40-lan-wan-web", log=True, dstPort="80,443", protocol="tcp", fires=True,
         inInterface="bridge-lan", outInterface="ether1"),
    dict(ordinal=11, comment="LAN to WAN mail", chain="forward", action="accept",
         logPrefix="r40-lan-wan-mail", log=True, dstPort="465,587,993", protocol="tcp", fires=False,
         inInterface="bridge-lan", outInterface="ether1"),
    dict(ordinal=12, comment="LAN to Servers DNS", chain="forward", action="accept",
         logPrefix="r40-lan-srv-dns", log=True, dstPort=53, protocol="udp", fires=True,
         inInterface="bridge-lan", outInterface="vlan-srv"),
    dict(ordinal=13, comment="LAN to Servers NTP", chain="forward", action="accept",
         logPrefix="r40-lan-srv-ntp", log=True, dstPort=123, protocol="udp", fires=True,
         inInterface="bridge-lan", outInterface="vlan-srv"),
    dict(ordinal=14, comment="LAN to Servers SMB", chain="forward", action="accept",
         logPrefix="r40-lan-srv-smb", log=True, dstPort=445, protocol="tcp", fires=True,
         inInterface="bridge-lan", outInterface="vlan-srv"),
    dict(ordinal=15, comment="LAN to Servers NAS app", chain="forward", action="accept",
         logPrefix="r40-lan-srv-app", log=True, dstPort=5001, protocol="tcp", fires=True,
         inInterface="bridge-lan", outInterface="vlan-srv"),
    dict(ordinal=16, comment="LAN to IoT control", chain="forward", action="accept",
         logPrefix="r40-lan-iot-ctl", log=True, dstPort="80,1883", protocol="tcp", fires=True,
         inInterface="bridge-lan", outInterface="vlan-iot"),

    # Inbound from the river. Note ordinal 19: a logging rule on
    # ether1|vlan-iot is what makes IoT read "DARK TOWARD WAN" rather than
    # "DARK BOTH WAYS" -- inbound is watched, outbound is not.
    dict(ordinal=17, comment="Port-forward NAS app from WAN", chain="forward", action="accept",
         logPrefix="r40-wan-lan-app", log=True, dstPort=5001, protocol="tcp", fires=True,
         inInterface="ether1", outInterface="bridge-lan"),
    dict(ordinal=18, comment="Drop unsolicited WAN to Servers", chain="forward", action="drop",
         logPrefix="r40-wan-srv-drop", log=True, dstPort="445,3389", protocol="tcp", fires=True,
         inInterface="ether1", outInterface="vlan-srv"),
    dict(ordinal=19, comment="Drop unsolicited WAN to IoT", chain="forward", action="drop",
         logPrefix="r40-wan-iot-drop", log=True, dstPort="23,2323,445", protocol="tcp", fires=True,
         inInterface="ether1", outInterface="vlan-iot"),

    dict(ordinal=20, comment="Servers to WAN updates", chain="forward", action="accept",
         logPrefix="r40-srv-wan", log=True, dstPort="80,443", protocol="tcp", fires=True,
         inInterface="vlan-srv", outInterface="ether1"),
    dict(ordinal=21, comment="Servers to WAN NTP", chain="forward", action="accept",
         logPrefix="r40-srv-wan-ntp", log=False, dstPort=123, protocol="udp", fires=False,
         inInterface="vlan-srv", outInterface="ether1"),
    dict(ordinal=22, comment="Servers to LAN NAS app", chain="forward", action="accept",
         logPrefix="r40-srv-lan-app", log=True, dstPort=5001, protocol="tcp", fires=True,
         inInterface="vlan-srv", outInterface="bridge-lan"),
    dict(ordinal=23, comment="Servers to LAN SMB", chain="forward", action="accept",
         logPrefix="r40-srv-lan-smb", log=True, dstPort=445, protocol="tcp", fires=True,
         inInterface="vlan-srv", outInterface="bridge-lan"),
    dict(ordinal=24, comment="Servers to IoT polling", chain="forward", action="accept",
         logPrefix="r40-srv-iot-poll", log=True, dstPort=80, protocol="tcp", fires=False,
         inInterface="vlan-srv", outInterface="vlan-iot"),

    # IoT: three gates to the river, not one of them lamped. This is the
    # DARK TOWARD WAN badge, and it is a real configuration -- the traffic
    # is allowed, nobody asked the router to write it down.
    dict(ordinal=25, comment="IoT to WAN web", chain="forward", action="accept",
         logPrefix="r40-iot-wan-web", log=False, dstPort="80,443", protocol="tcp", fires=False,
         inInterface="vlan-iot", outInterface="ether1"),
    dict(ordinal=26, comment="IoT to WAN NTP", chain="forward", action="accept",
         logPrefix="r40-iot-wan-ntp", log=False, dstPort=123, protocol="udp", fires=False,
         inInterface="vlan-iot", outInterface="ether1"),
    dict(ordinal=27, comment="IoT to WAN MQTT", chain="forward", action="accept",
         logPrefix="r40-iot-wan-mqtt", log=False, dstPort=8883, protocol="tcp", fires=False,
         inInterface="vlan-iot", outInterface="ether1"),
    dict(ordinal=28, comment="IoT to Servers DNS", chain="forward", action="accept",
         logPrefix="r40-iot-srv-dns", log=True, dstPort=53, protocol="udp", fires=True,
         inInterface="vlan-iot", outInterface="vlan-srv"),
    dict(ordinal=29, comment="IoT to Servers NTP", chain="forward", action="accept",
         logPrefix="r40-iot-srv-ntp", log=True, dstPort=123, protocol="udp", fires=True,
         inInterface="vlan-iot", outInterface="vlan-srv"),
    dict(ordinal=30, comment="IoT camera recording to NAS", chain="forward", action="accept",
         logPrefix="r40-iot-srv-rec", log=True, dstPort=445, protocol="tcp", fires=False,
         inInterface="vlan-iot", outInterface="vlan-srv"),

    # Guest: gates in every direction, no lamp on any of them. Nothing
    # from this district ever reaches the stream, which is the point --
    # the plate is drawn from the pushed address table alone.
    dict(ordinal=31, comment="Guest to WAN web", chain="forward", action="accept",
         logPrefix="r40-guest-wan-web", log=False, dstPort="80,443", protocol="tcp", fires=False,
         inInterface="vlan-guest", outInterface="ether1"),
    dict(ordinal=32, comment="Guest to WAN DNS", chain="forward", action="accept",
         logPrefix="r40-guest-wan-dns", log=False, dstPort=53, protocol="udp", fires=False,
         inInterface="vlan-guest", outInterface="ether1"),
    dict(ordinal=33, comment="Guest isolation from LAN", chain="forward", action="drop",
         logPrefix="r40-guest-lan-drop", log=False, dstPort="139,445", protocol="tcp", fires=False,
         inInterface="vlan-guest", outInterface="bridge-lan"),
    dict(ordinal=34, comment="Guest isolation from Servers", chain="forward", action="drop",
         logPrefix="r40-guest-srv-drop", log=False, dstPort="139,445", protocol="tcp", fires=False,
         inInterface="vlan-guest", outInterface="vlan-srv"),

    # The two footbridges.
    dict(ordinal=35, comment="wg0 peers to LAN", chain="forward", action="accept",
         logPrefix="r40-wg-lan", log=True, dstPort="22,445,5001", protocol="tcp", fires=True,
         inInterface="wg0", outInterface="bridge-lan"),
    dict(ordinal=36, comment="LAN to wg0 peers", chain="forward", action="accept",
         logPrefix="r40-lan-wg", log=False, dstPort="445,5001", protocol="tcp", fires=False,
         inInterface="bridge-lan", outInterface="wg0"),
    dict(ordinal=37, comment="L2TP peer to Servers", chain="forward", action="accept",
         logPrefix="r40-l2tp-srv", log=True, dstPort="445,5001", protocol="tcp", fires=True,
         inInterface="l2tp-anna-remote", outInterface="vlan-srv"),
    dict(ordinal=38, comment="wg0 peers to Servers DNS", chain="forward", action="accept",
         logPrefix="r40-wg-srv-dns", log=False, dstPort=53, protocol="udp", fires=False,
         inInterface="wg0", outInterface="vlan-srv"),

    dict(ordinal=39, comment="Old DMZ segment drop (decommissioned)", chain="forward", action="drop",
         logPrefix="r40-old-dmz", log=True, dstPort=139, protocol="tcp",
         dstAddress="10.0.99.0/24", fires=False,
         inInterface="ether1", outInterface="bridge-lan"),
    # The last forward rule -- what the city's alarm road ends against.
    # A real default drop names no interface and no port; this one names
    # the boundary and the ports it actually catches, because the pair is
    # what frontend/src/lib/fall.svelte.ts's boundary bands key on and the
    # ports are what keeps watchlist coverage from reading "covered"
    # everywhere. The callout still says what happened: caught by the
    # default drop, no gate on this wall.
    dict(ordinal=40, comment="Default drop -- no gate on this boundary", chain="forward",
         action="drop", logPrefix="r40-default-drop", log=True, dstPort="139,445,3389,5900",
         protocol="tcp", fires=True, inInterface="vlan-iot", outInterface="bridge-lan"),
]

# hap-ax3's 12. Its ether1 faces rb5009's LAN, not the river, so its
# "WAN" rules are really uplink rules -- and Cams is dark for the same
# reason Guest is: gates, no lamps.
FILTER_RULES["hap-ax3"] = [
    dict(ordinal=0, comment="Accept established and related", chain="input", action="accept",
         logPrefix="ax3-in-established", log=False, connectionState=["established", "related"],
         fires=False, inInterface="ether1"),
    dict(ordinal=1, comment="Drop invalid", chain="input", action="drop",
         logPrefix="ax3-in-invalid", log=False, connectionState=["invalid"], fires=False,
         inInterface="ether1"),
    dict(ordinal=2, comment="Winbox from Workshop", chain="input", action="accept",
         logPrefix="ax3-winbox", log=True, dstPort=8291, protocol="tcp", fires=True,
         inInterface="bridge-workshop"),
    dict(ordinal=3, comment="Drop management from the uplink", chain="input", action="drop",
         logPrefix="ax3-uplink-drop", log=True, dstPort="22,8291,8728", protocol="tcp", fires=True,
         inInterface="ether1"),
    dict(ordinal=4, comment="Workshop to uplink web", chain="forward", action="accept",
         logPrefix="ax3-work-up-web", log=True, dstPort="80,443", protocol="tcp", fires=True,
         inInterface="bridge-workshop", outInterface="ether1"),
    dict(ordinal=5, comment="Workshop backups to the NAS", chain="forward", action="accept",
         logPrefix="ax3-work-backup", log=True, dstPort=445, protocol="tcp", fires=True,
         inInterface="bridge-workshop", outInterface="ether1"),
    dict(ordinal=6, comment="Workshop to NAS app", chain="forward", action="accept",
         logPrefix="ax3-work-app", log=True, dstPort=5001, protocol="tcp", fires=False,
         inInterface="bridge-workshop", outInterface="ether1"),
    dict(ordinal=7, comment="Workshop to Cams RTSP", chain="forward", action="accept",
         logPrefix="ax3-work-cams", log=False, dstPort="80,554", protocol="tcp", fires=False,
         inInterface="bridge-workshop", outInterface="vlan-cams"),
    dict(ordinal=8, comment="Cams recording to the NAS", chain="forward", action="accept",
         logPrefix="ax3-cams-nas", log=False, dstPort=445, protocol="tcp", fires=False,
         inInterface="vlan-cams", outInterface="ether1"),
    dict(ordinal=9, comment="Cams NTP", chain="forward", action="accept",
         logPrefix="ax3-cams-ntp", log=False, dstPort=123, protocol="udp", fires=False,
         inInterface="vlan-cams", outInterface="ether1"),
    dict(ordinal=10, comment="Cams isolation from Workshop", chain="forward", action="drop",
         logPrefix="ax3-cams-work-drop", log=False, dstPort="139,445", protocol="tcp", fires=False,
         inInterface="vlan-cams", outInterface="bridge-workshop"),
    dict(ordinal=11, comment="Default drop -- uplink to Workshop", chain="forward", action="drop",
         logPrefix="ax3-default-drop", log=True, dstPort="139,445,3389", protocol="tcp", fires=True,
         inInterface="ether1", outInterface="bridge-workshop"),
]

NAT_RULES["rb5009"] = [
    dict(ordinal=0, comment="Masquerade WAN egress", chain="srcnat", action="masquerade",
         logPrefix="r40-masq-wan", inInterface="bridge-lan", outInterface="ether1"),
    dict(ordinal=1, comment="Port-forward NAS app", chain="dstnat", action="dst-nat",
         logPrefix="r40-fwd-nas", dstPort=5001, toAddresses="10.0.20.10", toPorts=5001,
         protocol="tcp", inInterface="ether1", outInterface="bridge-lan"),
]
# This masquerade is the reason workshop -> servers arrives at rb5009 as
# traffic from 10.0.10.9 rather than from 10.0.50.x: the second borough
# hides behind its own uplink address, which is a building in the LAN.
NAT_RULES["hap-ax3"] = [
    dict(ordinal=0, comment="Masquerade behind 10.0.10.9", chain="srcnat", action="masquerade",
         logPrefix="ax3-masq-uplink", inInterface="bridge-workshop", outInterface="ether1"),
]

# The district plaques, plus the two tunnel interfaces' own addresses.
# wg0 carries the story's 10.99.0.0/24; the l2tp entry is what a RouterOS
# l2tp-server actually produces for a connected peer -- a dynamic /32 on
# the per-peer interface, local address with the peer's as the network.
IP_ADDRESSES["rb5009"] = [
    dict(address="10.0.10.1/24", network="10.0.10.0", interface="bridge-lan", comment="LAN"),
    dict(address="10.0.20.1/24", network="10.0.20.0", interface="vlan-srv", comment="Servers"),
    dict(address="10.0.30.1/24", network="10.0.30.0", interface="vlan-iot", comment="IoT"),
    dict(address="10.0.40.1/24", network="10.0.40.0", interface="vlan-guest", comment="Guest"),
    dict(address="203.0.113.7/29", network="203.0.113.0", interface="ether1", comment="wan"),
    dict(address="10.99.0.1/24", network="10.99.0.0", interface="wg0", comment="wg0"),
    dict(address="10.98.0.1/32", network="10.98.0.2", interface="l2tp-anna-remote",
         comment="l2tp anna-remote"),
]
IP_ADDRESSES["hap-ax3"] = [
    dict(address="10.0.50.1/24", network="10.0.50.0", interface="bridge-workshop",
         comment="Workshop"),
    dict(address="10.0.60.1/24", network="10.0.60.0", interface="vlan-cams", comment="Cams"),
    dict(address="10.0.10.9/24", network="10.0.10.0", interface="ether1",
         comment="uplink -- inside rb5009's LAN"),
]

# The footbridge and its far bank. Keys are obviously-fake placeholders:
# a WireGuard public key is not a secret, but a value that looks like a
# working one has no place in a tracked file.
WIREGUARD_INTERFACES = {
    "rb5009": [
        dict(name="wg0", comment="road warriors", listenPort=51820,
             publicKey="DEMO-FAKE-wg0-public-key-not-a-real-key"),
    ],
}
WIREGUARD_PEERS = {
    "rb5009": [
        dict(publicKey="DEMO-FAKE-phone-tom-away-public-key", allowedAddress=["10.99.0.2/32"],
             endpointAddress="", comment="phone-tom-away"),
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
    # Round-40: the four the city's callouts name by hand.
    "r40-lan-srv-smb": "LAN to Servers SMB",
    "r40-default-drop": "Default Drop",
    "r40-wg-lan": "WireGuard to LAN",
    "ax3-work-backup": "Workshop Backups",
}
PORT_ENTITIES = {
    "443": "HTTPS", "80": "HTTP", "22": "SSH", "5060": "SIP/VoIP", "445": "SMB",
    # Round-40's LAN <-> Servers ports, so the heavy roads carry names.
    "53": "DNS", "123": "NTP", "5001": "NAS App",
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
    # Round-40: the two the story says the operator is watching, so the
    # city's watch lens has buildings to ring (BRIEF.md's "watched"
    # importance reading -- cam-porch tall, nas mid).
    dict(name="cam-porch-smb-watch", ip="10.0.30.31", ports=[445], pause=False, note=None),
    dict(name="nas-shares-watch", ip="10.0.20.10", ports=[445, 5001], pause=False, note=None),
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
# #738 item 3: "hosts with characters" -- a laptop that comes and goes, a
# guest device that appears once. Every host not named here keeps the
# plain rule active_hosts always used: present from its own
# introduce-after-seconds (HOSTS' last field) onward, forever. Overriding
# by mac rather than growing the HOSTS tuple keeps the estate table above
# readable, and the two forms below are the only stories that table's
# plain "present since" field cannot already express:
#
#   ("cyclic", period, on_fraction, phase) -- on for on_fraction*period out
#       of every period seconds (a laptop being closed and opened again),
#       counted from its own intro time.
#   ("once", start, duration)              -- active only during
#       [start, start+duration), then gone for the rest of the run --
#       unlike a plain newcomer (HOSTS' intro-after-seconds), which joins
#       once and then stays.
# ---------------------------------------------------------------------------

HOST_PRESENCE = {
    "aa:bb:cc:01:02:01": ("cyclic", 1800, 0.4, 0),   # tom-laptop: ~12 min on, ~18 off
    "aa:bb:cc:40:04:53": ("once", 300, 600),         # a guest seen once, then never again
}


def host_active(h, elapsed):
    """Whether host tuple h (see HOSTS) is transmitting at elapsed seconds
    into the run."""
    mac, intro = h[3], h[5]
    spec = HOST_PRESENCE.get(mac)
    if spec is None:
        return elapsed >= intro
    kind = spec[0]
    if kind == "cyclic":
        _, period, on_fraction, phase = spec
        if elapsed < intro:
            return False
        return ((elapsed - intro + phase) % period) < period * on_fraction
    if kind == "once":
        _, start, duration = spec
        return start <= elapsed < start + duration
    raise ValueError(f"host {mac}: unknown presence kind {kind!r}")


# #738 item 3's other half: "uniform random talkers are what make the map
# read as noise". A handful of round-40's hosts talk more or less than
# their zone's baseline; everything not listed here gets the baseline
# weight of 1.0 -- most hosts, deliberately, since "a few characters and a
# quiet majority" is the shape the issue asks for, not a tuned number for
# every host.
HOST_WEIGHT = {
    "aa:bb:cc:40:01:20": 3.0,   # tom-desktop: the LAN's heaviest talker
    "aa:bb:cc:40:01:23": 0.3,   # tv-lounge: mostly idle
    "aa:bb:cc:40:02:10": 2.0,   # nas: busy as both source and destination
}


def weighted_pick(hosts):
    """Pick one host from hosts, favouring the ones HOST_WEIGHT marks as
    chattier. None for an empty list, same contract random.choice would
    refuse outright."""
    if not hosts:
        return None
    weights = [HOST_WEIGHT.get(h[3], 1.0) for h in hosts]
    return random.choices(hosts, weights=weights, k=1)[0]


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
    return [h for h in HOSTS if h[0] == router and host_active(h, elapsed)]


# #738 item 2: "traffic that follows a day". Keyed off the real wall-clock
# hour a demo is actually running in, not a fabricated simulated day: a
# review that runs across an afternoon sees real daytime volume, and one
# left running overnight actually goes quiet -- honestly, rather than
# pretending to have lived through a day it has not (see the module
# docstring and #738's own "not in scope: a network simulator").
HOURLY_FACTOR = [
    0.15, 0.15, 0.15, 0.15, 0.15, 0.20,   # 00-05: small hours
    0.35, 0.60, 0.85, 1.00, 1.00, 1.00,   # 06-11: ramp into the day
    0.95, 1.00, 1.00, 1.00, 1.00, 0.90,   # 12-17: the busy day
    0.75, 0.60, 0.45, 0.35, 0.25, 0.20,   # 18-23: evening wind-down
]


def diurnal_factor(ts=None):
    """0.15 (quiet, small hours) .. 1.0 (business day) activity multiplier
    for the local hour at ts (default: now)."""
    hour = time.localtime(ts if ts is not None else time.time()).tm_hour
    return HOURLY_FACTOR[hour]


def lines_for_router(router, elapsed, tick):
    """One tick of coherent, moderate-volume traffic for one router --
    every src-mac comes from a host's own stable identity in HOSTS."""
    # The round-40 boroughs (#870) have their own generator: the
    # per-router lookups below are exhaustive over the original three.
    if router in ROUND40:
        return lines_for_round40(router, elapsed, tick)
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


# ---------------------------------------------------------------------------
# ROUND-40 traffic: the city's roads (#870). Kept in its own function
# rather than folded into lines_for_router above, because that function's
# per-router lookups are exhaustive dicts over the original three routers
# and a fourth key would only be a KeyError waiting for someone.
#
# Every line here uses an interface pair that a pushed rule in
# FILTER_RULES declares with log=True, for the reason lines_for_router's
# own comments give: a live event whose (chain, in, out) triple no pushed
# rule carries lands in the fall's unmatched "other traffic" band, and on
# the city that is a road with no wall behind it.
# ---------------------------------------------------------------------------

# The UNPLANNED episode's bookkeeping. cam-porch has been asking
# tom-desktop for tcp/445 and the default drop has been refusing it: the
# story's "14x". Emitted as a wave of exactly fourteen every ten minutes,
# so the count the composer card quotes is real and the road stays lit
# for a demo that runs for hours rather than going "stopped" after one
# burst -- the two episode shapes #687 already draws elsewhere.
UNPLANNED_WAVE_SECONDS = 600
UNPLANNED_PER_WAVE = 14
# cam-porch's own DNS beacon -- #738 item 3's "camera that beacons on a
# schedule", fired on a fixed cadence rather than left to the same random
# per-tick draw every other IoT host gets.
CAM_BEACON_SECONDS = 90
_r40_state = {"last_unplanned_wave": -1, "last_cam_beacon": -1}


def _fw(action, prefix, chain, in_iface, out_iface, src, sport, dst, dport,
        proto="TCP (SYN)", mac=None, length=60):
    """One RouterOS-shaped firewall line. `out_iface=None` omits the out:
    token entirely, which is what a real input-chain line looks like."""
    where = f"in:{in_iface}" + (f" out:{out_iface}" if out_iface else "")
    # Exactly the two shapes lines_for_router already emits: with a
    # src-mac the comma follows the MAC, without one it follows the state.
    state = f"connection-state:new src-mac {mac}," if mac else "connection-state:new,"
    return (f"firewall,info {action}|{prefix}| {chain}: {where}, "
            f"{state} proto {proto}, "
            f"{src}:{sport}->{dst}:{dport}, len {length}")


def _eph():
    return random.randint(1024, 65000)


def lines_for_round40(router, elapsed, tick):
    out = []
    hosts = active_hosts(router, elapsed)
    if not hosts:
        return out
    by_zone = collections.defaultdict(list)
    for h in hosts:
        by_zone[h[1]].append(h)

    def one(zone):
        return weighted_pick(by_zone[zone])

    if router == "hap-ax3":
        # The second borough. Workshop is lamped both ways; Cams is not
        # lamped at all, so it never appears here -- that silence is the
        # DARK badge, and inventing a line for it would be the one thing
        # the city's honesty rule forbids.
        for _ in range(random.randint(1, 3)):
            h = one("workshop")
            if h:
                out.append(_fw("A", "ax3-work-up-web", "forward", "bridge-workshop", "ether1",
                               full_ip(h), _eph(), random.choice(PUBLIC),
                               random.choice([80, 443]), mac=h[3]))
        # The backups road: workshop -> the NAS across in rb5009's Servers.
        for _ in range(random.randint(1, 2)):
            h = one("workshop")
            if h:
                out.append(_fw("A", "ax3-work-backup", "forward", "bridge-workshop", "ether1",
                               full_ip(h), _eph(), "10.0.20.10", 445, mac=h[3]))
        if random.random() < 0.25:
            h = one("workshop")
            if h:
                out.append(_fw("A", "ax3-winbox", "input", "bridge-workshop", None,
                               full_ip(h), _eph(), "10.0.50.1", 8291, mac=h[3]))
        if random.random() < 0.2:
            out.append(_fw("D", "ax3-uplink-drop", "input", "ether1", None,
                           f"10.0.10.{random.randint(20, 23)}", _eph(), "10.0.10.9",
                           random.choice([22, 8291, 8728])))
        if random.random() < 0.15:
            h = one("workshop")
            if h:
                out.append(_fw("D", "ax3-default-drop", "forward", "ether1", "bridge-workshop",
                               f"10.0.10.{random.randint(20, 23)}", _eph(), full_ip(h),
                               random.choice([139, 445, 3389])))
        h = one("workshop")
        if h:
            pub = random.choice(PUBLIC)
            out.append(f"firewall,info A|ax3-masq-uplink| srcnat: in:bridge-workshop out:ether1, "
                       f"proto TCP, {full_ip(h)}:{_eph()}->{pub}:443, "
                       f"NAT (10.0.10.9:12345->{pub}:443), len 73")
        return out

    # --- rb5009, the primary borough ---------------------------------
    nas, pihole = "10.0.20.10", "10.0.20.11"

    # LAN -> WAN, the busiest road out.
    for _ in range(random.randint(3, 5)):
        h = one("lan")
        if h:
            out.append(_fw("A", "r40-lan-wan-web", "forward", "bridge-lan", "ether1",
                           full_ip(h), _eph(), random.choice(PUBLIC),
                           random.choice([80, 443]), mac=h[3]))

    # LAN <-> Servers, the heavy pair: :53 :123 :445 :5001, both ways.
    for _ in range(random.randint(4, 7)):
        h = one("lan")
        if not h:
            break
        prefix, port, proto, dst = random.choice([
            ("r40-lan-srv-dns", 53, "UDP", pihole),
            ("r40-lan-srv-dns", 53, "UDP", pihole),
            ("r40-lan-srv-ntp", 123, "UDP", pihole),
            ("r40-lan-srv-smb", 445, "TCP (SYN)", nas),
            ("r40-lan-srv-app", 5001, "TCP (SYN)", nas),
        ])
        out.append(_fw("A", prefix, "forward", "bridge-lan", "vlan-srv",
                       full_ip(h), _eph(), dst, port, proto=proto, mac=h[3]))
    for _ in range(random.randint(1, 3)):
        s, h = one("servers"), one("lan")
        if s and h:
            prefix, port = random.choice([("r40-srv-lan-app", 5001), ("r40-srv-lan-smb", 445)])
            out.append(_fw("A", prefix, "forward", "vlan-srv", "bridge-lan",
                           full_ip(s), _eph(), full_ip(h), port, mac=s[3]))

    # The second borough's backups, seen again on this side of the road:
    # hap-ax3 masquerades, so they arrive from 10.0.10.9 -- the building
    # in the LAN district that is also a borough.
    gw = next((x for x in hosts if x[4] == "hap-ax3"), None)
    if gw and random.random() < 0.5:
        out.append(_fw("A", "r40-lan-srv-smb", "forward", "bridge-lan", "vlan-srv",
                       full_ip(gw), _eph(), nas, 445, mac=gw[3]))

    # Servers out, and IoT's one lamped road (to its own resolver).
    if random.random() < 0.5:
        s = one("servers")
        if s:
            out.append(_fw("A", "r40-srv-wan", "forward", "vlan-srv", "ether1",
                           full_ip(s), _eph(), random.choice(PUBLIC),
                           random.choice([80, 443]), mac=s[3]))
    for _ in range(random.randint(1, 2)):
        h = one("iot")
        if h:
            prefix, port = random.choice([("r40-iot-srv-dns", 53), ("r40-iot-srv-ntp", 123)])
            out.append(_fw("A", prefix, "forward", "vlan-iot", "vlan-srv",
                           full_ip(h), _eph(), pihole, port, proto="UDP", mac=h[3]))

    # cam-porch's beacon: fixed cadence, not a coin flip -- see
    # CAM_BEACON_SECONDS' own comment above.
    beacon_tick = int(elapsed // CAM_BEACON_SECONDS)
    if beacon_tick != _r40_state["last_cam_beacon"]:
        _r40_state["last_cam_beacon"] = beacon_tick
        cam = next((x for x in hosts if x[4] == "cam-porch"), None)
        if cam:
            out.append(_fw("A", "r40-iot-srv-dns", "forward", "vlan-iot", "vlan-srv",
                           full_ip(cam), _eph(), pihole, 53, proto="UDP", mac=cam[3]))
    if random.random() < 0.4:
        h = one("lan")
        if h:
            out.append(_fw("A", "r40-lan-iot-ctl", "forward", "bridge-lan", "vlan-iot",
                           full_ip(h), _eph(), full_ip(one("iot")),
                           random.choice([80, 1883]), mac=h[3]))

    # The river's own boundary: unsolicited inbound, no local src-mac.
    if random.random() < 0.6:
        out.append(_fw("D", "r40-wan-in-drop", "input", "ether1", None,
                       random.choice(PUBLIC), _eph(), "203.0.113.7",
                       random.choice([22, 23, 445, 3389, 8291])))
    if random.random() < 0.3:
        out.append(_fw("D", "r40-wan-srv-drop", "forward", "ether1", "vlan-srv",
                       random.choice(PUBLIC), _eph(), nas, random.choice([445, 3389])))
    if random.random() < 0.3:
        h = one("iot")
        if h:
            out.append(_fw("D", "r40-wan-iot-drop", "forward", "ether1", "vlan-iot",
                           random.choice(PUBLIC), _eph(), full_ip(h),
                           random.choice([23, 2323, 445])))
    if random.random() < 0.25:
        out.append(_fw("A", "r40-wan-lan-app", "forward", "ether1", "bridge-lan",
                       random.choice(PUBLIC), _eph(), nas, 5001))
    if random.random() < 0.2:
        h = one("lan")
        if h:
            prefix, port = random.choice([("r40-winbox-lan", 8291), ("r40-ssh-lan", 22)])
            out.append(_fw("A", prefix, "input", "bridge-lan", None,
                           full_ip(h), _eph(), "10.0.10.1", port, mac=h[3]))

    h = one("lan")
    if h:
        pub = random.choice(PUBLIC)
        out.append(f"firewall,info A|r40-masq-wan| srcnat: in:bridge-lan out:ether1, "
                   f"proto TCP, {full_ip(h)}:{_eph()}->{pub}:443, "
                   f"NAT (203.0.113.7:12345->{pub}:443), len 73")

    # The two footbridges. wg0 is QUIET -- a trickle, not silence and not
    # a stream; l2tp carries anna-remote's session at roughly 0.3/s (the
    # tick below sleeps 3-6s, so one or two lines a tick). Neither line
    # carries a src-mac: traffic arriving on a tunnel interface has no
    # ethernet source, and inventing one would make the register believe
    # in a device that does not exist.
    if random.random() < 0.12:
        out.append(_fw("A", "r40-wg-lan", "forward", "wg0", "bridge-lan",
                       "10.99.0.2", _eph(), full_ip(one("lan")),
                       random.choice([22, 445, 5001])))
    for _ in range(random.randint(1, 2)):
        out.append(_fw("A", "r40-l2tp-srv", "forward", "l2tp-anna-remote", "vlan-srv",
                       "10.98.0.2", _eph(), nas, random.choice([445, 5001])))

    # The alarm road: UNPLANNED, iot -> lan, tcp/445, 14x, caught by the
    # default drop. cam-porch to tom-desktop, the same two buildings every
    # wave, so the composer card's "it's been asking" is about one pair.
    wave = int(elapsed // UNPLANNED_WAVE_SECONDS)
    if wave != _r40_state["last_unplanned_wave"]:
        _r40_state["last_unplanned_wave"] = wave
        cam = next((x for x in hosts if x[4] == "cam-porch"), None)
        desk = next((x for x in hosts if x[4] == "tom-desktop"), None)
        if cam and desk:
            for _ in range(UNPLANNED_PER_WAVE):
                out.append(_fw("D", "r40-default-drop", "forward", "vlan-iot", "bridge-lan",
                               full_ip(cam), _eph(), full_ip(desk), 445, mac=cam[3]))
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
        # #738 item 2: quieter hours get a longer gap between ticks
        # instead of a smaller one each -- the same per-tick composition
        # (busy pathways vs. quiet ones), just less of it overnight.
        time.sleep(random.uniform(3, 6) / diurnal_factor())


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
    # A demo seeder, pointed at a local instance serving the self-signed
    # certificate it generated at startup. There is no certificate here
    # worth checking and nothing this script does is deployed.
    # nosemgrep: python.requests.security.disabled-cert-validation.disabled-cert-validation
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
        # The tunnel tables, where the router has any (#870: rb5009 only).
        # There is no read-back endpoint for either kind yet -- the city's
        # bridges slice (#866) is what will grow one -- so a successful
        # ack is all this can prove today.
        if router in WIREGUARD_INTERFACES:
            ack = ingest_push(args.url, tok, "wireguard-interface", WIREGUARD_INTERFACES[router])
            print(f"{router}: pushed {ack['records']} wireguard interfaces")
        if router in WIREGUARD_PEERS:
            ack = ingest_push(args.url, tok, "wireguard-peer", WIREGUARD_PEERS[router])
            print(f"{router}: pushed {ack['records']} wireguard peers")


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
