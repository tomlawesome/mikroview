# The catalogue — which lists the builder offers, and why (#1360, round 2)

Researched 2026-09-30 against samples fetched the same day (entry
counts below are from those samples) and each source's own terms
page. The catalogue is MikroView's: names, URLs, the parse recipe and
the guidance. The lists are never MikroView's: the router fetches
each one from its source, and MikroView never serves or copies list
data (AGENTS.md, "List and lookup data"; the dependencies-and-data
skill). Attribution terms still bind data the router fetches, so each
entry records them.

## The test a list has to pass

1. **Terms that let anyone use it, stated by the source.** Free of
   charge and free of a non-commercial clause — MikroView cannot
   know whether the operator is a business, so a list with a
   non-commercial licence cannot be offered without a caveat the
   operator has to judge. A list whose site states no terms at all
   fails too: "free to download" is not a grant.
2. **Fits a router.** Address-list entries the router adds one at a
   time from a script; a file the router can hold in storage. Under
   about 20,000 entries and a few hundred KB.
3. **Safe to drop blind.** Nothing that would drop the operator's
   own traffic (private space, bogons on the LAN side), nothing that
   lists whole /24s of shared cloud space, nothing that is really
   policy (Tor) rather than known-bad.
4. **Adds something.** Not a re-packaging of a list already offered.
5. **Alive.** Updated, and with a maintainer who has been around for
   years.

## Offered (three)

| List | Entries (2026-09-30) | Size | Updates | Terms | Default |
|---|---|---|---|---|---|
| **Spamhaus DROP** (+ DROPv6) | 1,692 ranges (widest /12) + 91 IPv6 ranges | 47 KB text · about 100 KB as JSON | re-evaluated daily; fetch at least 1 h apart | free, attribution required | **on**, from them, IPv6 too |
| **Emerging Threats compromised IPs** | 633 hosts | 9 KB | once a day on weekdays | BSD-3-Clause | **on**, both ways (owner, 17a) |
| **CINS Army** (`ci-badguys.txt`) | 15,000 hosts (capped there) | 213 KB | hourly | free, "use in any way you see fit" | off — offered for routers that expose a service |

**Spamhaus DROP.** Netblocks Spamhaus says carry no legitimate traffic
— hijacked space and criminal hosting. Conservative by design, so
false positives are rare; MikroView already flags from it, so the
router's drops and MikroView's flags agree. Terms, from
[spamhaus.org/drop](https://www.spamhaus.org/drop/): "credit must be
given to Spamhaus Project, and the date and © text should remain with
the file and data"; "Please DO NOT auto-fetch the DROP list more than
once per hour!"; once a day is enough. The generated script keeps the
credit in the address-list comment and the schedule never goes under
six hours. **Format:** Spamhaus now recommends the JSON files
(`drop_v4.json`, `drop_v6.json`: one object per line,
`{"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}`) and says
of the text files "in time, these will be deprecated; users will be
notified with ample notice". The builder writes for JSON from the
start (`:deserialize from=json`, RouterOS 7.13 and later, so every
supported router). Widest range today is a /12; the script refuses
anything wider than /8 as a corrupted line. Contains no private
space (checked). *Finding for MikroView's own fetcher:*
`internal/blocklist` still reads `drop.txt`; it should move to the
JSON files before Spamhaus retires the text.

**Emerging Threats compromised IPs.** Single hosts seen attacking in
the last few days; rebuilt daily on weekdays. Churns, and a cleaned-up
host stays listed a while, so an occasional drop is an innocent
server — which is why the owner chose both directions with the drops
logged (17a): a LAN device talking *to* one of these is the finding,
and MikroView shows every block. Licence: the ET Open distribution's
`rules/LICENSE` places `compromised-ips.txt` under BSD-3-Clause
(checked 2026-09-26 for `docs/configuration.md`; the same file is
served from the same host). ET's own download instructions ask for
one fetch a day. Overlap with DROP: 63 of 633 hosts sit inside a DROP
range — harmless, the raw rule hits whichever list is first.

**CINS Army.** Sentinel IPS / Nomic Networks' free list, ten years
running: hosts with a very poor "rogue packet" score or that tripped
trusted alerts across their sensors, deliberately **restricted to
addresses not already on other public lists** — measured: 0 of 15,000
inside DROP, 5 of 15,000 on ET compromised. Terms, from
[cinsscore.com](https://cinsscore.com/#list): provided free "as a
simple text file, with which you can parse and use in any way you see
fit"; no non-commercial clause. Hourly updates. Off by default because
of its size: 15,000 single hosts is a minute or more of address-list
adds on a small router, and for a home router with no exposed service
the inbound scanners it lists are dropped by the default firewall
anyway. Offered for routers that expose SSH, a VPN or a web service.

## Considered and left out

| List | Why not |
|---|---|
| **Spamhaus DROPv6** | not left out — folded into the DROP entry as "IPv6 too", 91 ranges, same terms |
| **Spamhaus ASN-DROP** | lists autonomous systems, not addresses; RouterOS cannot act on an ASN |
| **Emerging Threats Block-IPs** (`emerging-Block-IPs.txt`, 1,716 entries) | an aggregate: 1,692 of its entries are Spamhaus DROP, 17 are DShield's /24s, 5 are Feodo. Would double-drop DROP and import DShield's non-commercial data |
| **abuse.ch Feodo Tracker** | CC0 and commercial-friendly on paper, but the datasets are empty — "thanks to various successful takedowns … Emotet in 2021 and Operation Endgame in 2024" (its FAQ); 5 entries in the sample. Nothing to drop |
| **abuse.ch SSL Blacklist** | deprecated 2025-01-03 (its own header) |
| **abuse.ch ThreatFox, URLhaus** | the exports need an Auth-Key, and the current [abuse.ch terms](https://abuse.ch/terms-of-use/) restrict the platforms to authenticated users "for not-for-profit purposes"; commercial use "may require a paid subscription". A router cannot hold a personal key sensibly, and the terms fail test 1. ThreatFox's host file is also mostly domains (44,548 entries), and URLhaus's is URLs — neither is an address list |
| **FireHOL level 1** (4,664 entries) | an aggregate of DROP + DShield + Feodo + Team Cymru fullbogons. It contains `10.0.0.0/8`, `192.168.0.0/16`, `172.16.0.0/12`, `127.0.0.0/8`, `0.0.0.0/8`, `224.0.0.0/3` and more; its README warns "if you apply such a blocklist on your DMZ or LAN side, you will be blocked out of your firewall". In raw prerouting, placed first, it would drop the operator's own LAN. DROP is already offered on its own |
| **Team Cymru fullbogons** (3,050 ranges, widest /4) | routing hygiene, not a threat list; includes private and reserved space, so it can only ever be applied on the WAN interface, and Cymru warns "these are not simple filters, and can have adverse impacts if improperly applied". A possible later "policy" section, never a known-bad list |
| **DShield top 20** (20 × /24) | CC BY-NC-SA 2.5 — a non-commercial clause MikroView cannot judge for the operator; whole /24s (one in the sample is Google Cloud); rolling three-day window. Round 1 offered it off; round 2 drops it under test 1 |
| **blocklist.de** (`all.txt` 29,160 hosts; `strongips.txt` 386) | fail2ban reports from the last 48 hours, refreshed every 30 minutes — useful, and `strongips` is small and high-confidence, but the site states no licence at all ("provided as they are … at your own risk" is a warranty disclaimer, not a grant). Also 29,000 entries is over the router budget; `strongips` would fit. Worth the owner asking blocklist.de for terms — MikroView cannot ask |
| **GreenSnow** (4,886 hosts) | no licence stated; the site says "Reproduction or republication strictly prohibited"; a single company. 2,373 of its hosts are also on blocklist.de |
| **Binary Defense banlist** (1,134 hosts) | header: "may not be used for commercial resale or in products that are charging fees" — a non-commercial clause |
| **ipsum** (level 3: 17,754 hosts; level 4: 9,137) | The Unlicense, daily, a well-known maintainer — but it is a re-publication of 30+ other lists under a licence their compiler cannot grant (its sources include lists with non-commercial terms, and the README itself says "a few of these lists may have special licences"). Test 1 fails one level down. It also lags a day, and 4,691 of CINS's hosts are on it already |
| **Tor exit list** (1,405 hosts, hourly, CC0) | policy, not known-bad: dropping inbound from exits blocks nothing a home firewall does not already block, and dropping outbound breaks Tor for the operator's own devices. If a policy section is ever wanted, this is its first entry |

## What the measurements say about overlap

- ET Block-IPs is 98.6% Spamhaus DROP (1,692 of 1,716) — the reason
  it is not offered.
- CINS and the two default lists barely touch (0 and 5 of 15,000) —
  the reason it is.
- FireHOL level 1 is 65% bogons (3,031 of 4,664) and 33% DROP; the
  remaining 1% is DShield.
- Of ET compromised's 633 hosts, 317 are on ipsum level 3 and 169 on
  blocklist.de — the daily-rebuilt attacker lists agree with each
  other about half the time, which is the churn the guidance warns
  about.

## Router limits the catalogue is measured against

- A script variable holds at most 63 KiB, so every list is fetched
  to a file and read in 32 KiB chunks with `/file read` (RouterOS
  7.13 and later; MikroView's floor is 7.18, owner, question 18).
- Address-list entries are added one at a time from the script;
  15,000 is the largest list offered and is off by default.
- The router's push sends address lists entry by entry within a
  64 KB envelope, so the builder's lists (`mv-bl-*`) are sent as a
  count, never entry by entry — part 1 of the block updates the push
  for this and for the raw rules' counters (owner, 11a).

## Sources

- Spamhaus DROP: https://www.spamhaus.org/drop/ (terms, JSON
  recommendation, fetch frequency); files `drop_v4.json`,
  `drop_v6.json`, legacy `drop.txt`.
- Emerging Threats: https://rules.emergingthreats.net/blockrules/compromised-ips.txt;
  licence in `rules/LICENSE` of
  https://rules.emergingthreats.net/open/suricata-5.0.0/emerging.rules.tar.gz;
  fetch guidance at
  https://rules.emergingthreats.net/OPEN_download_instructions.html.
- CINS Army: https://cinsscore.com/ (terms, description);
  https://cinsscore.com/list/ci-badguys.txt.
- abuse.ch: https://abuse.ch/terms-of-use/;
  https://feodotracker.abuse.ch/blocklist/ and its FAQ;
  https://sslbl.abuse.ch/blacklist/ (deprecation header);
  https://threatfox.abuse.ch/faq/; https://urlhaus.abuse.ch/api/.
- FireHOL: https://github.com/firehol/blocklist-ipsets (README:
  composition of level1, the LAN-side warning, per-list licences).
- Team Cymru: https://www.team-cymru.com/bogon-networks and
  https://www.team-cymru.com/bogon-reference-http.
- DShield: https://www.dshield.org/block.txt (CC BY-NC-SA 2.5 header).
- blocklist.de: https://www.blocklist.de/en/export.html.
- GreenSnow: https://greensnow.co/.
- Binary Defense: header of
  https://www.binarydefense.com/banlist.txt.
- ipsum: https://github.com/stamparm/ipsum.
- Tor: https://check.torproject.org/torbulkexitlist;
  https://metrics.torproject.org/collector.html (CC0).
- RouterOS: `https://download.mikrotik.com/routeros/<version>/CHANGELOG`
  for 7.13–7.24 (`/file read` and `:deserialize` in 7.13;
  `http-max-redirect-count` in 7.18, default 2 from 7.22; `:break`,
  `:continue`, `:exit` in 7.22; scheduler `days` in 7.24).
