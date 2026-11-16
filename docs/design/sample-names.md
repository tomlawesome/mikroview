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
| Site tunnels, router WAN, the design host | 192.0.2.0/24 (a /28 per tunnel) |
| Outside peers (scanners, internet hosts) | 203.0.113.0/24 |
| IPv6 | 2001:db8::/32 |

Write a network as its network address (`198.51.100.32/27`), never as a host
with a prefix. One device keeps one address across a page, its README and its
build script.

## Domains

Only reserved names (RFC 2606): `example.com`, `example.net`, `example.org`,
or anything under `.example` or `.test`.
