# Sample data for mock-ups

Mock-ups use realistic sample data: real-looking lengths and variety show
how a screen reads in a real home, where placeholders like `test-user-a`
would hide layout problems. Every value below is made up. Use only these, so
no one has to ask whether a name or address belongs to a real person.

## People

alex, anna, kai, mira, rowan, meredith

Use them in device names (`laptop-anna`, `phone-kai`) and as account names.
To add a name, add it here first, in the same change.

## Networks

Only the documentation ranges (RFC 5737, RFC 3849). One network always gets
the same block, so rounds agree:

| Network | Block |
|---|---|
| LAN | 198.51.100.0/27 |
| Servers | 198.51.100.32/27 |
| IoT | 198.51.100.64/27 |
| Guest | 198.51.100.96/27 |
| wg0 | 198.51.100.128/27 |
| Workshop | 198.51.100.160/27 |
| Cams | 198.51.100.192/27 |
| Garage | 198.51.100.224/27 |
| Router WANs (a /29 each), the design host (.30) | 192.0.2.0/27 |
| Staff | 192.0.2.32/27 |
| Office | 192.0.2.64/27 |
| VoIP | 192.0.2.96/27 |
| WiFi | 192.0.2.128/27 |
| Mgmt | 192.0.2.160/27 |
| Core | 192.0.2.192/27 |
| L2TP tunnel (a /28 per tunnel) | 192.0.2.224/28 |
| DMZ | 192.0.2.240/29 |
| Old DMZ | 192.0.2.248/29 |
| Outside peers (scanners, internet hosts) | 203.0.113.0/24 |
| IPv6 | 2001:db8::/32 |

192.0.2.0/24 is the mixed block: the router WANs and the design host are
outside, every other row in it is inside. Inside networks outside RFC 1918
read as outside unless the config lists them under `networks.home`
(#1481); the demo's `cfg.yaml` lists every inside row above.

Write a network as its network address (`198.51.100.32/27`), never as a host
with a prefix. One device keeps one address across a page, its README and its
build script.

## Domains

Only reserved names (RFC 2606): `example.com`, `example.net`, `example.org`,
or anything under `.example` or `.test`.
