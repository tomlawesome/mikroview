# Configuration

## Precedence

`defaults < YAML file < environment variables < CLI flags` — each layer
overrides the one before it. The list of monitored devices only comes
from the YAML file (see below for why).

## config.yaml

Copy `deploy/config.example.yaml` to `deploy/config.yaml` and edit it —
`docker-compose.yml` mounts that path into the container at
`/etc/mikroview/config.yaml`.

```yaml
listen:
  syslogTls: ":6514"    # RouterOS remote-protocol=tls -- mikroview's only syslog listener
  http: ":8080"
  httpRedirect: ":8081"
  # Only set these if a reverse proxy fronts mikroview -- see
  # "Running behind a reverse proxy" below.
  # trustedProxies: ["private"]
  # clientIpHeader: "X-Forwarded-For"  # this is the default

store:
  retention: 24h
  maxMemory: 120MiB

devices:
  - id: core-router
    name: "Core Router"
    sourceIp: 192.168.1.1
```

- `store.retention` — how far back `/api/events` and the UI will show,
  as a Go duration string (`24h`, `12h30m`, ...). A ceiling on what a
  query returns, not a promise that history exists — see
  [How events are stored](#how-events-are-stored) for why.
- `store.maxMemory` — the memory budget for mikroview's event buffer, a
  Go duration-string-style size such as `120MiB` or `500MB` rather than
  an event count. Same section as above explains why a count would not
  mean anything portable between deployments.
- `devices` — maps a syslog source IP to a friendly name. Routers
  sending logs from an IP *not* listed here still appear in the UI and
  `/api/devices`, labelled by their raw IP with `configured: false`, so
  you can identify and add them rather than silently losing their events.

### How events are stored

There is no database. Every event mikroview has seen lives in one
fixed-size block of memory (`internal/store/ring.go`) that holds the most
recent events and overwrites the oldest once it fills — `store.maxMemory`
is how big that block is, and `store.retention` is a ceiling applied when
a query runs, not a guarantee that events that old still exist. If the
buffer has already overwritten what a query asks for, you get whatever
is left, not an error — `windowStart` in the response says what the
actual lower bound turned out to be, so you can tell the difference
between "nothing happened" and "it isn't there any more."

**How long the buffer actually covers is set by your own traffic, and it
varies enormously.** MikroTik firewall rules do not log by default — `log`
is `no` unless you explicitly turn it on for a rule — so the rate
mikroview sees is entirely a product of which rules you have logging
enabled for, not of your link speed or how busy your network is. A router
logging only dropped traffic and one logging every accepted connection
can differ by four or more orders of magnitude. There is no default that
serves both, and no table of "typical" rates in this document would mean
anything without knowing which of those you have.

So: measure your own. `GET /api/stats` reports `capacity`, `count` and
`eventsPerSecond` for your own running instance — once `count` reaches
`capacity` the buffer is overwriting, and `capacity / eventsPerSecond` is
roughly how many seconds of history remain. The live view's toolbar shows
this directly (`n% of buffer used`, or `holding last Xm Ys` once full),
so you do not need to query the API by hand to find out.

**A typical retained event costs about 624 bytes**, so the default
`maxMemory: 120MiB` holds roughly 200,000 events. The stored copy of
the raw log line is capped at 2KiB, which is about five times the
longest line a real RouterOS device produces — so the figure above is a
ceiling rather than a typical case that a pathological line can exceed.
A row whose line was cut says so on hover, and in a CSV export. That figure is a budget
for the buffer itself, not what mikroview's process occupies on the host
— expect *resident* memory (what `docker stats` or `top` reports) to run
about 1.47x higher once the Go runtime and process overhead are counted,
so provisioning for the 120MiB default really means having roughly 175MiB
of RAM to spare. That figure is not a round number for a reason: it is
the measured ring-to-resident overhead (see #244), and it is the same
1.47 the CFG-0012 warning uses, so what you read here and what MikroView
prints at startup agree. The whole budget is reserved **immediately at startup**
(`store.New` allocates it all up front), not filled up gradually — a
value too large for the machine fails right away rather than degrading
over hours, which is why mikroview warns above 1GiB (see
[CFG-0012](#cfg-0012)) without silently shrinking it back down: a large
budget on a machine that genuinely has the memory is a legitimate choice,
and the warning only makes sure you're making it with the real cost in
front of you.

### Running behind a reverse proxy

Mikroview rate-limits failed logins per source address as well as per
username. Behind a reverse proxy, every request arrives carrying the
*proxy's* address, so without configuration all your users share a
single rate-limit bucket and one attacker's failed attempts lock
everybody out.

`listen.trustedProxies` fixes that by telling mikroview which peers it
may believe a forwarding header from:

```yaml
listen:
  trustedProxies: ["private"] # or ["10.0.0.0/8", "172.20.0.5", ...]
  clientIpHeader: "X-Forwarded-For" # the default; set for proxies that use another
```

- **`trustedProxies`** — bare IPs, CIDRs, or the shorthand `"private"`
  (loopback, RFC1918, CGNAT, link-local and IPv6 ULA — which is where a
  proxy sharing your LAN or a Docker network lands). **Empty by
  default, and empty means forwarding headers are ignored entirely.**
  That default is not an oversight: a header is just text the client
  sent, so honouring one unconditionally would let anyone mint a fresh,
  empty rate-limit bucket for every request simply by varying it, which
  doesn't weaken the limiter so much as delete it. List only proxies you
  actually operate.
- **`clientIpHeader`** — which header to read, honoured only for peers
  matching `trustedProxies`. Defaults to `X-Forwarded-For`, which nginx,
  Caddy, Traefik, HAProxy, Cloudflare and the cloud load balancers all
  set without being asked. Point it at `X-Real-IP`, `CF-Connecting-IP`
  or anything else for a proxy that doesn't; single-value headers work
  unchanged.

The forwarded chain is walked **right to left**, skipping entries that
are themselves trusted proxies, and the first untrusted address wins.
That direction matters: entries are appended hop by hop, so the
rightmost was written by your own proxy while anything further left
could have been forged by the client before the request ever arrived.
If the chain is malformed, or every entry is a trusted proxy, mikroview
falls back to the directly-observed peer address rather than to
something the client chose.

A misconfigured entry here fails startup rather than being skipped —
silently ignoring it would leave you believing forwarded addresses were
being honoured when they weren't.

### Checking your config before you deploy

```
mikroview -validate-config
```

In Docker, use `docker run`, **not** `docker exec`. A configuration bad
enough to stop mikroview starting also means there is no running
container to exec into — `docker exec` answers "container is not
running", which looks like a second, unrelated problem:

```
docker run --rm \
  -e MIKROVIEW_CONFIG=/etc/mikroview/config.yaml \
  -v /path/to/config.yaml:/etc/mikroview/config.yaml:ro \
  ghcr.io/tomlawesome/mikroview:latest -validate-config
```

It reports anything wrong and exits `0` if all is well, `1` if it found
problems, and `2` if it couldn't read the config file at all — so you can
use it in a deployment script or CI.

It never touches the network. Nothing is dialled, nothing is written, and
no directories are created, so it's safe to run anywhere.

### What happens when a setting is wrong

Mikroview treats two kinds of mistake differently.

**Some settings stop it starting.** These are ones where carrying on
would be unsafe or would mean mikroview isn't doing its job — an
unreadable listen address, a session that never expires, or session
cookies without the `Secure` flag while TLS is on. The error names the
setting so you know what to fix.

**Everything else starts anyway, using a sensible default.** A negative
retention or a zero event limit would mean nothing is kept at all, so
mikroview substitutes the default rather than refusing — losing all your
monitoring over a typo would be worse. But it won't do that quietly: the
substitution appears in the log **and** as a banner across the top of the
web interface, naming the setting and the value actually in use.

That banner is only shown to admins, since the messages name file paths
and hostnames.

**File permissions**: the container runs as a fixed non-root user (uid
65532, distroless `nonroot`) that is unrelated to any user on the Docker
host. If `config.yaml` isn't world-readable (or owned by a matching
uid/gid), the container will fail to start with a permission error —
`chmod 644 deploy/config.yaml` after editing it.

### Problem codes

Every problem mikroview reports carries a code. The message already
includes the fix and a snippet; this is the same information in one
place, for when you would rather read than re-run.

#### CFG-0001

A listen address is empty. Mikroview would have nothing to bind.

```yaml
listen:
  http: ":8080"
```

#### CFG-0002

A listen address is not `host:port`. Use `:port` for every interface, or
`host:port` for one. `httpRedirect` is the exception — `""` disables it,
which is a supported choice rather than a mistake.

```yaml
listen:
  http: ":8080"                # every interface, port 8080
  # http: "192.168.1.10:8080"  # one interface only
  httpRedirect: ""             # "" disables the redirect listener
```

#### CFG-0003

`trustedProxies` has an entry that is not an IP, a CIDR, or the shorthand
`private`. Leaving it empty is fine and means forwarding headers are
ignored entirely.

```yaml
listen:
  trustedProxies: ["192.168.1.5", "10.0.0.0/8"]
  # trustedProxies: ["private"]   # a proxy on your LAN or docker network
```

#### CFG-0010

`store.retention` is zero or negative, which would keep nothing.
Mikroview starts anyway on the default rather than leaving you with no
monitoring, and says so.

```yaml
store:
  retention: 24h
```

#### CFG-0011

`store.maxMemory` is zero, negative, or too small to be a usable memory
budget, which would keep nothing. Same treatment as CFG-0010.

```yaml
store:
  maxMemory: 120MiB
```

#### CFG-0012

`store.maxMemory` is set above 1GiB. Unlike CFG-0010/CFG-0011, this is
only a warning that leaves your configured value alone — a large budget
on a machine that genuinely has the memory to spare is a legitimate
choice, not a mistake to correct. It exists because the whole budget is
reserved immediately at startup (see
[How events are stored](#how-events-are-stored)), so a value that turns
out to be too large fails right away rather than degrading gradually, and
the warning states the real memory cost — including the 1.47x resident
overhead on top of the ring itself — so you have it in front of you
before that happens.

```yaml
store:
  maxMemory: 120MiB  # lower this, or confirm the machine has the larger amount to spare
```

#### CFG-0020

`auth.sessionTTL` is zero or negative, so sessions would never expire — a
credential with no end.

```yaml
auth:
  sessionTTL: 24h
```

#### CFG-0021

`auth.secureCookie` is false while `tls.enabled` is true. Session cookies
would go out without the `Secure` flag, so they could be sent over a
plain connection this deployment has otherwise ruled out.

```yaml
# Serving TLS yourself -- the usual case:
tls:
  enabled: true
auth:
  secureCookie: true
```

Only set both to false if a reverse proxy terminates TLS **and**
mikroview's own listener is never reachable from the LAN:

```yaml
tls:
  enabled: false
auth:
  secureCookie: false
```

#### CFG-0030

A `devices[].sourceIp` is not an IP address. It should be the address the
router sends syslog from.

```yaml
devices:
  - sourceIp: "192.168.1.1"
    name: "edge-router"
    # id is optional -- it defaults to sourceIp. Set it to a stable name
    # if you want one, but see CFG-0032: it is the device's identity for
    # pushed router state and ingest tokens, not just a label.
```

#### CFG-0031

Two devices share a `sourceIp`. One would silently shadow the other:
events land under whichever entry wins and the other never appears, which
looks like a quiet router rather than a config mistake.

```yaml
devices:
  - sourceIp: "192.168.1.1"
    name: "edge-router"
  - sourceIp: "192.168.2.1"   # must differ from every other sourceIp
    name: "branch-router"
```

#### CFG-0032

Two devices share an `id`. A device's `id` is its identity everywhere:
the `deviceId` on its events, the key its pushed router state is stored
under, and the scope of an ingest token. Two routers under one id is two
routers wearing one identity -- either can then supply host names for
the other's traffic.

An entry with no `id` takes its `sourceIp` as its id, so a collision is
always an explicit one.

```yaml
devices:
  - sourceIp: "192.168.1.1"
    id: "edge-router"
    name: "Edge"
  - sourceIp: "192.168.2.1"
    id: "branch-router"   # must differ from every other id
    name: "Branch"
```

#### CFG-0033

A device's `id` is an IP address that is not its own `sourceIp`. A
router discovered from that address takes it as its own id, so the two
would merge into one identity -- the same collision as CFG-0032 by a
longer route.

```yaml
devices:
  - sourceIp: "192.168.1.1"
    id: "edge-router"     # a name, or this device's own sourceIp
    name: "Edge"
```

#### CFG-0040

`watchlist.matchLogPath` is empty. Unlike every other `storePath` in this
file, the match log has no in-memory-only mode -- durability is the
entire reason it exists -- so an empty value is treated the same as an
unusable one rather than as a deliberate opt-out.

```yaml
watchlist:
  matchLogPath: /var/lib/mikroview/matchlog.jsonl
```

#### CFG-0041

`watchlist.matchLogCapacity` is zero or negative, which would keep
nothing. Same treatment as CFG-0011. File backend only -- see
[Watchlist](#watchlist-optional).

```yaml
watchlist:
  matchLogCapacity: 200000
```

#### CFG-0042

`watchlist.matchLogRetention` is zero or negative. Postgres backend
only -- see [Watchlist](#watchlist-optional) -- validated regardless of
which backend is active so a config that later adopts Postgres doesn't
discover a bad value for the first time at that point.

```yaml
watchlist:
  matchLogRetention: 168h  # 7 days
```

#### CFG-0050

`notify.webhook.url` is a plain `http://` URL and
`notify.webhook.headers` is set, so whatever credential those headers
carry -- and every flag's contents with it -- crosses the network in
cleartext. See [Notifications](#notifications-optional).

Mikroview sends anyway: the receiver may well be on a network you
control end to end, and refusing would be MikroView deciding that for
you.

```yaml
notify:
  webhook:
    url: "https://ntfy.example.com/mikroview"
```

#### CFG-0051

`notify.webhook.url` is a plain `http://` URL. No credential is at risk
(no headers are set), but every flag's contents -- source addresses,
rule labels, detector detail -- still cross the network in cleartext.
Same "sends anyway" reasoning as CFG-0050 above.

```yaml
notify:
  webhook:
    url: "https://ntfy.example.com/mikroview"
```

#### CFG-0060

`oidc.issuerUrl` is set but `oidc.publicBaseUrl` is not, so MikroView
cannot build the redirect URI the provider has to send users back to.
See [Single sign-on](#single-sign-on-oidcsso).

SSO login is unavailable; local login is unaffected. That is deliberate
-- an optional integration being half-configured should not take a
working deployment down -- but `-validate-config` still exits non-zero,
so a pipeline is told.

```yaml
oidc:
  issuerUrl: "https://id.example.com"
  publicBaseUrl: "https://mikroview.example.com"
```

#### CFG-0061

`oidc.issuerUrl` is set but `oidc.clientId` and/or `oidc.clientSecret`
are empty. Same outcome as CFG-0060: SSO off, local login unaffected.

Remove `oidc.issuerUrl` if you meant to turn SSO off -- that is the
deliberate way to say so, and it silences this.

```yaml
oidc:
  clientId: "mikroview"
  clientSecret: "<from your provider>"
```

#### CFG-0062

`oidc.issuerUrl` names a multi-tenant provider. MikroView only supports
self-hosted identity providers (Authentik, Keycloak, Zitadel) or a
single-tenant Entra issuer URL -- ones where the issuer itself restricts
who can sign in. See [Single sign-on](#single-sign-on-oidcsso) for the
full reasoning; it is a refusal, not a warning you can configure away.

```yaml
oidc:
  # a self-hosted provider, not a multi-tenant one
  issuerUrl: "https://id.example.com"
```

## Logging

Mikroview's own server output (not event data -- see `store.retention`
above) is leveled and colorized, one line per entry:

```
18:43:44 INFO  auth        │ no decision made yet -- showing the first-run choice screen
18:43:45 WARN  flags       │ permission denied opening flags.json -- continuing in-memory-only
18:43:46 ERROR syslog-tls  │ listen tcp :6514: bind: address already in use
```

```yaml
log:
  level: info
```

**On boot**, before the leveled log lines start, mikroview prints its
ASCII wordmark once (plain text, not a log line -- always shown,
regardless of `level` or whether stdout is a terminal), followed by a
`version` line identifying which build is running -- the short commit
SHA it was built from (see `.github/workflows/docker.yml`), the
practical stand-in for a registry digest since a binary can't know its
own digest at build time. If the persisted version marker
(`/var/lib/mikroview/version`) doesn't match the running build, that
line reads `upgraded from <old> to <new>` instead -- confirming a
`docker compose pull`/image update actually took effect, versus a
routine restart on the same build. This boot sequence doesn't apply to
the CLI recovery commands (`-healthcheck`, `-transfer-admin`, etc.) --
only the real server-start path.

- **`level`** — one of `debug`/`info`/`warn`/`error` (case-insensitive).
  Defaults to `info`. Anything unrecognized (a typo, an empty value)
  falls back to `info` silently, the same way every other malformed
  value in this app degrades rather than failing startup over a log
  setting.
- **Color** follows the [NO_COLOR](https://no-color.org) convention and
  auto-disables when stdout isn't a terminal -- piping to a file,
  `docker logs | grep`, or a log collector all see plain text, not raw
  ANSI escapes.
- The component column (`auth`, `tls`, `flags`, `syslog-tls`, `http`,
  ...) identifies which part of mikroview logged the line -- the same
  names used throughout this doc and SECURITY.md for the pieces they
  refer to.
- This does **not** apply to the CLI recovery commands' own output
  (`-transfer-admin`'s account list, `-recover-admin-account`'s password prompts and
  success message, etc.) -- those print directly to stdout/stderr for
  scripting/piping, not through this leveled path.

## GeoIP country flags (optional)

mikroview can show a country flag next to public source/destination
addresses, using a MaxMind GeoLite2 (or paid GeoIP2) **Country** or
**City** database. This is entirely opt-in: mikroview doesn't bundle a
database or call out to MaxMind at runtime, since their license requires
you to create your own free account to obtain one.

1. Sign up for a free [MaxMind GeoLite2 account](https://www.maxmind.com/en/geolite2/signup)
   and download `GeoLite2-Country.mmdb` (or generate a license key and use
   their `geoipupdate` tool to keep it current).
2. Mount the `.mmdb` file into the container and point mikroview at it
   with `MIKROVIEW_GEOIP_DB_PATH` (or `geoip.dbPath` in `config.yaml`, or
   `-geoip-db` for local development).

If the path is unset, empty, or the file can't be opened/parsed, mikroview
logs a note at startup and simply shows no flags — this is never a fatal
error.

## IP reputation lookup (optional)

Clicking the "investigate" affordance next to a public source/destination
IP queries a threat-intel source and shows the result in a popover (open
ports, hostnames, known CVEs, abuse score). This proxies through the
backend so no key ever reaches the browser, and caches each IP briefly
to conserve free-tier quota.

It shows up next to a public IP in the live view, and (issue #213) on a
flag card next to any flag whose target is a real IP — a fresh check,
independent of the reputation snapshot frozen at the moment the flag
first raised (see [Behavioral flags](#behavioral-flags-optional-on-by-default)).
Raw events aren't persisted, so an old or cleared flag often has nothing
left in the live view to click into; this is the way to check what an IP
looks like now without leaving the Flags page.

- **Shodan InternetDB** — free, keyless, always used, no configuration
  needed.
- **AbuseIPDB** — optional, needs an API key (free tier: 1000 lookups/
  day). Set `reputation.abuseIPDBKey` in `config.yaml` or the
  `MIKROVIEW_ABUSEIPDB_KEY` env var. Adds abuse score, report count,
  country, and ISP to the result.

  GreyNoise was evaluated as a second live source (issue #113 Part A)
  and removed again: its Community API's free tier is 50 lookups/*week*,
  shared with the GreyNoise Visualizer web UI, not a usable quota for a
  live monitoring tool — no code from that integration remains.

Every behavioral flag's confidence floor is also informed by whichever
sources are configured (see "Behavioral flags" below) — an abuse score
or a Tor-exit/hosting-provider classification against the flag's source
IP raises (never lowers) that flag's confidence, the same way local
blocklist matches do (see "Local IP/CIDR blocklist matching" below).

Unconfigured, the feature still works with Shodan-only results; private/
loopback/link-local addresses are rejected server-side regardless of
configuration.

## Local IP/CIDR blocklist matching (optional, on by default)

Independent of the live reputation lookups above (which run on demand,
against public IPs a human clicks "investigate" on), mikroview also
maintains a small local cache of known-malicious CIDR ranges from a
vetted menu of free threat-intel feeds, and checks every ingested
event's source IP against it — raising a `known_bad_ip` flag directly on
a match, regardless of any behavioral threshold. This isn't a
"detector" in the `flags.detectors`/scope sense above (see
"Per-detector toggles"): there's no threshold to tune and no scope
narrower than "does this exact IP fall in a range on the list," so it
has no matching entry there, the same precedent `new_device`/
`stale_rule` already set.

```yaml
blocklist:
  sources:
    - spamhaus_drop
```

- **`sources`** — which feeds from the vetted menu to enable. This is
  deliberately *not* an arbitrary URL field: both the trust story (an
  operator ticking "Spamhaus DROP" is trusting mikroview's own vetting
  of that source, not whatever URL they happen to type) and the
  performance ceiling (every enabled list is consulted on the hot
  per-event ingest path — see below) depend on the menu staying small,
  fixed, and enumerable. An unrecognized entry is logged and skipped at
  startup, not fatal. Set to an empty list (`sources: []`) to disable
  local blocklist matching entirely.

  The menu today:
  - **`spamhaus_drop`** (on by default) —
    [Spamhaus's DROP list](https://www.spamhaus.org/drop/):
    small (documented at roughly 1-2k CIDR ranges combined), free, no
    registration, and deliberately conservative — Spamhaus only lists
    netblocks they're confident are entirely malicious-controlled
    (hijacked/stolen allocations, bulletproof hosting), which fits
    "safe to flag on sight, no behavioral corroboration needed" far
    better than a larger, noisier aggregated list would.
  - **`emerging_threats_compromised`** (opt-in, not part of the
    default) — [Emerging Threats' compromised-IPs
    list](https://rules.emergingthreats.net/blockrules/compromised-ips.txt):
    also free and requiring no registration, but a much larger,
    faster-changing list of individual compromised hosts rather than
    curated netblocks.

**Refresh** is a fixed daily cycle, deliberately not configurable — an
explicit decision to avoid over-polling Spamhaus/Emerging Threats' free
infrastructure, not an oversight. The first fetch happens in the
background at startup (never blocking it); if a refresh fails for a
given feed (network issue, upstream outage), that feed keeps serving
whatever it last successfully fetched rather than going blind, and logs
a warning — see the `blocklist` component in server logs.

**Performance.** Matching is a per-feed binary search over that feed's
own sorted, non-overlapping address ranges — O(log n) per feed, never a
linear scan, regardless of list size, since this runs on every single
ingested event. Combined entries across every enabled feed are capped at
100,000 (`internal/blocklist`'s `maxTotalEntries`) — measured, not
estimated: today's real combined feed size (Spamhaus DROP +
Emerging Threats) is ~2.2k entries, and benchmarking this package's
actual lookup and daily-rebuild paths shows neither is remotely close to
a real ceiling until well past 100,000 (rebuild takes ~33ms and ~7.5MB
at that size; per-event lookup cost barely changes even at 5 million
entries). 100,000 is real headroom over current usage, not a number
chosen to avoid a performance cliff that's actually much further out. A
feed that would push the combined total over this cap has its excess
entries truncated (a partially-loaded list still catches most of what
it would have) rather than being rejected outright; a truncation is
logged. Multiple feeds may be enabled simultaneously — each is matched
independently, so enabling more than one only ever adds coverage, never
conflicts.

**Firing behavior.** A match raises `known_bad_ip` directly against the
source IP, with a fixed high confidence (Spamhaus's own curation policy
— only netblocks they're confident are entirely malicious-controlled —
makes this about as strong a signal as this app has). It also raises the
confidence floor (see `RaiseConfidenceFloor`) of any other currently
active, source-IP-keyed flag for that same address (`port_scan`,
`activity_spike`, `critical_port`, `outbound_anomaly`, `internal_recon`,
`low_slow_scan`, `off_hours_activity`) — the same reinforcement role the
live reputation lookups above already play for those flags, just
resolved synchronously (a local lookup needs no network round-trip)
instead of asynchronously.

## Network attribution (optional, on by default)

When you click "investigate" on an IP, mikroview also labels it with the
kind of network it belongs to — a Tor exit, a commercial VPN, cloud or
datacenter space, or a privacy relay (Apple iCloud Private Relay,
Cloudflare WARP). It shows up as a **Network** row in the lookup popover,
alongside the reputation data.

**Attribution itself is display-only** for every category, always. A
network-class match by itself never raises a flag. The reason is
measured, not cautious: the broad datacenter/cloud lists cover more than
one in ten routable IPv4 addresses — Google Public DNS, Akamai edge, and
every Apple Private Relay user included — so treating a datacenter match
as suspicion would fire constantly on ordinary traffic.

**Two categories additionally reinforce an already-raised flag's
confidence: Tor and commercial VPN** — the two high-precision categories,
covering well under 1% of IPv4 combined. This only happens when a source
address in one of those categories is reaching *into* your network (a
LAN destination); a device on your own network reaching *out* to a VPN
or Tor address (Private Relay, WARP, ordinary browsing through a VPN you
run yourself) never contributes anything, in either direction of the
check. A match on its own, with no behavioral detection already
triggered, never creates a flag — the same "absence of evidence is not
evidence, but a mild match by itself is not evidence of anything either"
floor-only contract every other reputation signal in mikroview follows.
Datacenter and privacy-relay matches never affect a score, only the
display. To suppress attribution/reinforcement for a specific address
you trust (your own VPN, a VPS you run), use the existing flag exclusion
or entity-tagging tools — there's no separate allow-list for this.

```yaml
netClass:
  sources:
    - tor
    - apple_private_relay
    - x4b_vpn
```

The default is deliberately the **high-precision lists**, not the broad
ones, so the feature is quiet the day you enable it.

| Source | What it covers | Notes |
|---|---|---|
| `tor` | Tor exit nodes | The Tor Project's own list — tiny, first-party, highest precision |
| `apple_private_relay` | Apple iCloud Private Relay egress ranges | Official `egress-ip-ranges.csv`, fetched from Apple directly. On by default alongside `x4b_vpn` — not a separate opt-in — because `x4b_vpn`'s own upstream data includes these same ranges, so disabling this one would leave ordinary iPhone/iPad/Mac traffic misclassified as a VPN exit |
| `x4b_vpn` | Commercial VPN exits | [X4BNet](https://github.com/X4BNet/lists_vpn) (MIT); ~0.08% of IPv4, precise |
| `x4b_datacenter` | Cloud / hosting / datacenter | Broad — ~10% of IPv4. Useful as a label, noisy as a signal. Opt-in |
| `aws` | AWS ranges, with region | Official `ip-ranges.json`. Opt-in |
| `gcp` | Google Cloud ranges | Official `cloud.json`. Opt-in |

Set `sources` to an empty list to turn attribution (and the Tor/VPN
confidence reinforcement) off entirely.

**No range data ships in mikroview.** Every list is fetched at runtime,
from the operator's own device, on a fixed daily cycle with a small
random offset per install (so thousands of self-hosted instances don't
refresh in lockstep). A release therefore can never ship stale security
data. Until the first fetch completes, the popover simply shows no
Network row. Azure is deliberately not on the menu: it publishes no
stable range URL (the file is date-stamped and deleted within a fortnight),
and its space is already covered by `x4b_datacenter`.

Refresh cadence is not configurable, for the same over-polling reason as
the blocklist.

## Port lookup

Clicking the "i" affordance next to a source/destination port shows what
that port is commonly used for (`frontend/src/lib/commonPorts.ts`) --
standard/well-known services, common databases, common self-hosted apps,
remote access, VPN, messaging, and a few ports historically associated
with malware/backdoors. Unlike the IP lookup above, this is pure local
static data with no network call: only shown for ports with a known
entry, and there's no way to configure or disable it.

It's a curated reference, not an exhaustive IANA registry dump -- if a
port you care about is missing, add an entry to `commonPorts.ts`.

**`tools/docker-ports/list-exposed-ports.sh`** is a separate, standalone
companion script (not part of the running app) for self-hosters: run it
directly on a Docker host to list every running container's published
ports, flagging which are bound to a public interface (`0.0.0.0`/`::`)
versus loopback-only (`127.0.0.1`/`::1`) -- useful for cross-referencing
"what does mikroview say this port usually is" against "what's actually
listening on it, on this host."

## Friendly names (optional)

RouterOS auto-generates a rule identifier (e.g. `r13`) in the log line
when a firewall rule has no `comment=` set, and every host only ever
shows up as a raw IP. `config.yaml`'s `ruleNames`/`hostNames` maps let
you give either a friendly display name, shown in place of the raw value
everywhere it appears (live table, CSV export) -- the raw value (rule
label, IP) is always still what filtering, grouping, and the hover
tooltip use, this is display-only.

```yaml
ruleNames:
  r13: "Block known scanners"
hostNames:
  192.168.1.50: "Living room NAS"
```

Both are YAML-only (like `devices`) rather than env-var-configurable --
a map doesn't translate cleanly to an env var without an awkward
numbered-key scheme, the same reasoning `devices` already follows. A
rule or host not listed still displays exactly as it does today (the
raw label/IP), so this is purely additive.

Setting a `comment=` on the rule in RouterOS itself is the more durable
fix for rule names specifically, if you're able to -- `ruleNames` is for
when you can't or don't want to edit RouterOS config directly, or want a
different name in mikroview than the RouterOS comment.

## Entities: UI-managed host/rule/port labels and tags (optional)

`ruleNames`/`hostNames` above are YAML-only: a label survives a restart,
but changing one means editing `config.yaml` and restarting the
container. **Entities** are the same idea (a label attached to a rule
label, host IP, or -- issue #109 -- a port number), plus open-ended
tags, managed live from the UI (**Menu → Entities**, admin-only) with no
restart needed -- the shared foundation two features build on (a
mail-sender allowlist, and this IP/port/rule aliasing UI), so the record
shape is deliberately generic (`type`, `key`, `label`, `tags`) rather
than shaped around either one specifically. `type` is a free-form
string, not a closed set -- `host`, `rule`, and `port` are just the
values mikroview's own display sites (the live table, CSV export,
`internal/naming.Resolver`) know to look up, not a validation allowlist.

```yaml
entities:
  # Where entity records are persisted, as a small JSON file. Same
  # optional-persistence contract as flags.storePath: left unset,
  # entities still work, they just don't survive a restart. If you set
  # this in the container, mount a volume for its parent directory --
  # see deploy/docker-compose.yml.
  storePath: "/var/lib/mikroview/entities.json"
```

**One-time migration**: the very first time mikroview boots against a
given entities store, if `ruleNames`/`hostNames` are non-empty it
imports each entry as an entity (`type: rule`/`type: host`, `key` = the
map key, `label` = the map value) so an existing deployment's aliases
become UI-editable instead of disappearing on upgrade. This is tracked
with a marker persisted alongside the entities themselves, not by
"is the store currently empty" -- so it really is one-time: deleting
every entity later (one at a time, from the UI) does not cause them to
reappear on the next restart. `ruleNames`/`hostNames` stay supported
afterward as a YAML-only fallback for a rule/host with no matching
entity. Ports have no `config.yaml`-level equivalent -- entities are the
only way a port ever gets a friendly name.

Entities are managed via `GET`/`POST`/`DELETE /api/entities`
(admin-gated the same way `POST /api/auth/users` is -- see
[API reference](#api-reference)), or the **Entities** panel in the menu.
That panel is also where you'd go to name something *without* already
knowing its raw IP/rule label/port number: a **Discovered** section
lists hosts, rules, and ports seen in live traffic that don't have a
label yet (mirroring the auto-discovered-device pattern the **Fleet**
view already uses for RouterOS sources), each with a one-click "Name it"
action. Discovered rules come from mikroview's own unbounded-time
per-rule usage record (`GET /api/rules`); discovered hosts/ports are
derived from the events currently loaded in your browser tab, so that
list is only as complete as what's been seen there so far -- the entity
itself, once named, is fully persisted regardless.

## Audit log: admin action accountability (optional)

Every admin-privileged mutation -- creating a user, changing a detector's
enabled/scope settings, upserting or deleting an entity, creating or
revoking an API token, or removing a permanent flag exclusion -- is
recorded to a persisted, admin-only audit log (issue #112), so there's a
record of *who* made each of those changes and *when*. This is
deliberately narrower than a full request/access log: only mutations are
recorded, never reads (viewing a page, listing users) -- matching what
"audit log" conventionally means and what's actually useful for
accountability.

```yaml
audit:
  # Where audit log entries are persisted, as a small JSON file. Same
  # optional-persistence contract as entities.storePath: left unset, the
  # log still works, entries just don't survive a restart. If you set
  # this in the container, mount a volume for its parent directory --
  # see deploy/docker-compose.yml.
  storePath: "/var/lib/mikroview/audit.json"
```

Two deliberate scoping decisions, both from issue #112's own explicit
open question about which flag actions belong here:

- **A plain flag clear** (`POST /api/flags/{id}/clear`) is **not**
  logged -- that endpoint isn't admin-gated at all (any signed-in user
  can clear a flag), and this is an audit log of *admin* actions.
- **A permanent flag exclusion** (`POST /api/flags/{id}/clear-permanent`)
  **is** logged, and is admin-only. It was previously open to any
  signed-in user and unlogged; that was tightened because an exclusion
  permanently suppresses detection for that (detector, target) until
  someone notices and undoes it, which is not something a non-admin --
  or a single compromised low-privilege credential -- should be able to
  do silently. Removing an exclusion
  (`DELETE /api/flags/exclusions/{id}`) is admin-gated and logged too.

Reviewed from **Menu → Audit log** (admin-only, matching Entities' own
gate). Backed by `GET /api/audit`, a windowed query over the
persisted log (see [API reference](#api-reference)) -- the same
`since`/`until`/`limit` convention `GET /api/events` already uses, minus
that endpoint's event-specific filters.

## Watchlist (optional)

Issue #243 grew the old Control Ports tab into a user-tuned watchlist:
instead of one flat list of "interesting" ports shared by everyone, an
operator defines entries scoped by source device, destination and port
set. Matches are persisted so they survive both the in-memory event ring
wrapping and a mikroview restart -- unlike Control Ports before it, which
only ever saw whatever was still in the browser's own capped, volatile
event buffer.

Two kinds of entry, chosen per entry, not globally:

- **Record** (non-inverted) -- "watch attempts against these ports,"
  optionally scoped to a source device and/or destination. This is the
  direct generalisation of what Control Ports did: a device or the
  whole network reaching for SSH, RDP, or whatever ports you name, with
  every match recorded rather than only ever visible in the live view
  while it's on screen.
- **Invert** -- "this device should only ever reach these destinations,"
  the other direction: instead of naming ports to watch for, you name a
  device and let mikroview tell you what it actually does. A new
  inverted entry starts **observing**: nothing fires while observing --
  every distinct destination the device touches gets recorded as a
  candidate for review, not treated as a violation. You look at what it
  actually reached, **promote** the destinations that are expected
  (a known NTP server, a vendor's telemetry endpoint), then turn
  observing off. From that point, anything the device reaches that
  wasn't promoted is a real match: either it's genuinely unexpected, or
  you missed promoting something and should add it. Broadcast,
  multicast and link-local traffic is exempt from this by default
  (`includeStructuralNoise` opts back in) since it's rarely what anyone
  means by "did this device misbehave."

Managed from **Menu → Watchlist** (admin-only, matching Entities/Audit's
gate). Add,
edit and remove entries there; for an inverted entry, the same page
shows what's been promoted, what's waiting for review, and a toggle to
resume or stop observing. An entry with a scoped source can also show
its own recent matches inline, pulled from the match log below.

```yaml
watchlist:
  # Where watchlist entries themselves are persisted, as a small JSON
  # file. Same optional-persistence contract as entities.storePath: left
  # unset, entries still work, they just don't survive a restart.
  storePath: "/var/lib/mikroview/watchlist.json"

  # Where matches are recorded, append-only. Unlike storePath above,
  # this has NO in-memory-only mode: durability is the entire reason
  # this store exists (a match must survive a restart), so an empty
  # value is treated as unusable rather than as an opt-out (CFG-0040).
  matchLogPath: "/var/lib/mikroview/matchlog.jsonl"

  # The match log's hard ceiling on distinct records -- once reached, a
  # genuinely new match is refused rather than silently overwriting the
  # oldest, unlike the in-memory event ring. A repeat of an
  # already-recorded match still collapses into it at no cost even once
  # full. 100k-500k is the realistic range for the file backend; 200,000
  # is the default (CFG-0041 warns below zero). File backend only.
  matchLogCapacity: 200000

  # How long a match is kept, on the Postgres backend only, once its
  # last activity ages past it -- Postgres has no record-count ceiling
  # (matchLogCapacity above doesn't apply there), so this is what bounds
  # it instead. Purged hourly, not instantly on expiry. 7 days is the
  # default (CFG-0042 warns below zero).
  matchLogRetention: 168h

  # Where suggested watchlist entries (below) are persisted. Same
  # optional-persistence contract as storePath above: left unset,
  # suggestions still work, they just regenerate from scratch on
  # restart rather than remembering what you already accepted or hid.
  suggestionsStorePath: "/var/lib/mikroview/suggestions.json"
```

The match log is a third store, distinct from the in-memory event ring
(all events, volatile, capacity-bound by `store.maxMemory`) and the
behavioral-flags store below (aggregate judgements with deliberately
capped evidence): a match is a discrete fact that matters individually,
so it's kept at full fidelity -- the whole matched event, not a summary.
A repeated identical match collapses into a count with first-seen/
last-seen timestamps rather than being stored again, so a noisy entry
cannot consume the capacity a genuinely novel match needs. Queried via
`GET /api/watchlist/matches` (see [API reference](#api-reference)) --
open to any signed-in user and reachable via a read-only API token, the
same tier as `/api/events`/`/api/flags`/`/api/stats`/`/api/devices`,
since correlating a device against its recorded matches from an external
tool is exactly what that log is for.

**On Postgres** (see [Postgres](#postgres-optional) below), the match
log is a dedicated, indexed table rather than a row in the shared
document table everything else there uses -- it's the one store whose
data doesn't fit that shape, needing a real range query rather than a
whole-document load (see `docs/decisions/postgres-backend.md` §1a for
why that's a deliberate, scoped exception, not a reopened decision).
There's no record-count ceiling on Postgres -- `matchLogCapacity` is a
file-backend-only concept -- so it's bounded by age instead:
`matchLogRetention` (7 days by default), enforced by a background purge
that runs hourly.

Watchlist coverage is bounded by the same thing every detector in this
app is bounded by: mikroview only ever sees what RouterOS actually
logs. An entry watching a port the router's own rules don't log traffic
for, or a device whose traffic never crosses a logged rule, will never
produce a match -- not because the entry is wrong, but because there's
nothing here for mikroview to observe. Tuning entries, like tuning
detector thresholds below, is an expected, ongoing part of running this
against a real network, not a one-time setup step.

### Suggested watchlist entries (issue #243)

Building a watchlist from a blank page means already knowing what to
watch. mikroview instead suggests entries from data your router has
already pushed (see [RouterOS setup](routeros-setup.md)) -- named
devices from your DHCP leases, and ports an existing firewall rule
already drops or rejects -- so you have something to react to rather
than something to invent.

Managed from **Menu → Suggestions** (admin-only, same gate as the
watchlist itself). Every suggestion is one of three states, never a
plain accept/reject:

- **Undecided** -- generated, not yet acted on. Every new suggestion
  starts here, and this is the default view.
- **Accepted** -- you accepted it; a real watchlist entry now exists
  for it, editable from the Watchlist page like any other entry.
- **Hidden** -- you declined it. Reversible, but only by deliberately
  switching to the Hidden view and undoing it -- a hidden suggestion
  never reappears on its own, no matter how much time passes.

New router data is checked for automatically in the background every
few minutes; there is no manual refresh button, since a periodic check
already does everything one would. Checking finds new suggestions and
refreshes existing ones' display details -- it never changes a
suggestion's state: an already-accepted or hidden suggestion stays
exactly where you left it. If an accepted suggestion's original reason
disappears (the firewall rule it came from was changed or removed, or
the device's lease expired), it's flagged **stale** with an
unmissable highlight rather than being silently un-accepted -- the
watchlist entry itself keeps working either way, this is only a
prompt to go take a look.

Accepting a device suggestion creates an inverted watchlist entry that
starts observing with nothing pre-approved, the same safe default as
creating one by hand -- see the section above. A device suggestion
watches every port a device touches, not one in particular.

**Reset everything** wipes the entire watchlist -- not just suggestion
state, every entry you've hand-tuned too -- and immediately regenerates
a fresh set of undecided suggestions from your router's current data.
It cannot be undone, and requires confirming that explicitly; reach for
it only if you genuinely want a clean slate.

Suggestions are only as complete as what RouterOS pushes: a rule with
no destination port set (most default-deny "drop everything" rules) is
too broad to suggest a specific port from, and a device with no DHCP
lease name never gets a device suggestion since there'd be nothing
meaningful to call it.

## Behavioral flags (optional, on by default)

mikroview watches the ingested event stream for a small set of patterns
worth a human's attention, and raises a **flag** (visible in the UI, in
`GET /api/flags`) for each one -- never an automatic action. This is an
"interrogation helper," not an intrusion-prevention system: nothing here
blocks, drops, or reports traffic anywhere. If you're running a proper
IPS alongside mikroview (e.g. CrowdSec), this is meant to complement it,
not duplicate it -- see the detector descriptions below for what each one
is actually good at spotting.

Detection itself is always on and needs no configuration; every
threshold below has a sensible default and is only worth changing for an
unusually quiet or unusually busy network.

The port-scan, activity-spike, critical-port, distributed-brute-force,
internal-recon, outbound-anomaly, low-and-slow-port-scan, and
off-hours-activity detectors only count events whose
RouterOS-reported connection state is `new` (or absent, for setups that
don't log connection state at all) -- if your ruleset logs both
directions of an established connection on the same rule, a busy host's
ordinary return traffic would otherwise look identical to new connection
attempts and trigger false positives purely from being legitimately
busy.

```yaml
flags:
  storePath: "/var/lib/mikroview/flags.json"
  portScanThreshold: 15
  portScanWindow: 60s
  activitySpikeThreshold: 200
  activitySpikeWindow: 60s
  criticalPorts: [21, 22, 23, 445, 3389, 5900, 8291, 8728, 8729]
  criticalPortThreshold: 5
  criticalPortWindow: 5m
  globalSpikeMultiplier: 4
  globalSpikeMinEPS: 5
  globalSpikeWarmupSamples: 20
  distributedBruteForceThreshold: 10
  distributedBruteForceWindow: 5m
  outboundAnomalyThreshold: 25
  outboundAnomalyWindow: 5m
  internalReconThreshold: 10
  internalReconWindow: 60s
  ruleSpikeMultiplier: 5
  ruleSpikeMinRate: 0.2
  ruleSpikeWindow: 60s
  ruleSpikeWarmupSamples: 20
  repeatedDropsThreshold: 10
  repeatedDropsWindow: 15m
  hostActivityMultiplier: 3
  hostActivityWarmupSamples: 20
  lowSlowScanWindow: 3h
  lowSlowScanPortThreshold: 8
  lowSlowScanHostThreshold: 5
  lowSlowScanMinObservation: 45m
  lowSlowScanDropRatio: 0.8
  lowSlowScanBaselineMultiplier: 3
  offHoursStartHour: 23
  offHoursEndHour: 6
  offHoursMinSampleDays: 14
  offHoursMinCount: 5
  deviceStaleAfter: 15m
  ruleUsageStorePath: "/var/lib/mikroview/rule-usage.json"
  staleRuleDays: 30
  staleRuleCheckInterval: 1h
  vpnInterfaces: []
  vpnConfidenceMultiplier: 1.5
```

- **`storePath`** — where raised/cleared flags are persisted, as a small
  JSON file. This is the one deliberate exception to mikroview's
  otherwise in-memory-only design (see [SECURITY.md](../SECURITY.md)): a
  flag is meant to stay visible until a human clears it, so unlike
  everything else it survives a restart. Left empty (the default),
  flags still work, they just reset like everything else does. If you
  set this in the container, mount a volume for its parent directory —
  see `deploy/docker-compose.yml`.
- **Port scan** — one source touching `portScanThreshold`+ distinct
  destination ports within `portScanWindow`. Applies to any source,
  internal or external.
- **Activity spike** — one source's own event rate vs. a slow-moving
  baseline *of that specific host* (same EMA technique the global-spike
  and rule-spike detectors use, just scoped per source), at
  `hostActivityMultiplier`× or more, and still gated by an absolute
  floor of `activitySpikeThreshold`+ events within `activitySpikeWindow`
  so a nearly-idle host doesn't "spike" from one extra event. A host
  that's always busy is judged against its own normal rather than one
  number applied to every host equally — fixes the false-positive
  pattern where a legitimately busy server (e.g. a database with many
  clients) got flagged just for being itself. `hostActivityWarmupSamples`
  is how many observations a host needs before a flag can reach full
  confidence (see below) — a brand-new source with almost no history
  can't produce a high-confidence flag no matter how extreme its first
  few readings look.
- **Critical-port attempts** — `criticalPortThreshold`+ attempts against
  one of `criticalPorts` within `criticalPortWindow`, from an *external*
  source only (a LAN device reaching your own router's Winbox port is
  normal; the same from the internet usually isn't). The default port
  list covers SSH/Telnet/FTP/SMB/RDP/VNC and RouterOS's own
  Winbox/API ports (8291/8728/8729) — worth watching precisely because
  they're MikroTik-specific and a common target once a scanner has
  fingerprinted a device as RouterOS.
- **Global volume spike** — current events/sec vs. a slow-moving
  baseline of itself (an exponential moving average, not a fixed
  number), so it adapts to your network's real traffic level over time
  rather than needing to be hand-tuned. `globalSpikeMinEPS` is a floor
  below which a "spike" isn't worth flagging (e.g. 2 events/s against a
  0.5 events/s baseline is technically 4x, but not meaningfully busy).
  Flags carry a confidence score (same z-score-against-baseline approach
  as activity spike) rather than firing on the multiplier alone;
  `globalSpikeWarmupSamples` is how many `Check()` cycles the baseline
  needs before a flag can reach full confidence -- this only affects the
  confidence number, never whether or when a flag fires.
- **Distributed brute-force** — `distributedBruteForceThreshold`+
  *distinct* external source IPs hitting the *same* critical port within
  `distributedBruteForceWindow`. The inverse of critical-port attempts
  (which is one source hitting a port repeatedly): this is many
  different sources hammering one port, the signature of a
  botnet/credential-stuffing campaign rather than a single noisy
  scanner.
- **Outbound anomaly** — a LAN source contacting
  `outboundAnomalyThreshold`+ distinct *external* destinations within
  `outboundAnomalyWindow`. One of the strongest signals of a
  compromised/malware-infected device (C2 beaconing, botnet
  participation) available without a separate security product —
  nothing else notices "this device just started talking to 30 IPs it's
  never touched before."
- **Internal reconnaissance** — a LAN source contacting
  `internalReconThreshold`+ distinct *internal* destinations within
  `internalReconWindow`. A network sweep: the classic lateral-movement
  signature of an attacker who already has a foothold on the LAN,
  probing for what else is reachable.
- **Rule hit-rate spike** — a firewall rule's own hit rate vs. a
  slow-moving baseline of *that rule specifically* (same EMA technique
  as the global spike, just scoped per rule), at `ruleSpikeMultiplier`×
  or more. Catches a normally-quiet rule suddenly lighting up even when
  it's nowhere near large enough to move the network-wide total —
  `ruleSpikeMinRate` (events/sec) is the same kind of noise floor as
  `globalSpikeMinEPS`. Flags carry a confidence score the same way
  global volume spike does; `ruleSpikeWarmupSamples` plays the same role
  as `globalSpikeWarmupSamples` -- how many observations this specific
  rule's baseline needs before a flag can reach full confidence, not a
  factor in whether or when it fires.
- **Repeated drops on a port** — the same (source, destination port)
  pair getting dropped/rejected `repeatedDropsThreshold`+ times within
  `repeatedDropsWindow`, against a *locally-hosted* service (unlike
  critical-port attempts, not restricted to a curated port list or to
  external sources). Aimed at self-hosters: this is very often a
  misconfigured port-forward or firewall rule — the real client keeps
  retrying a port that isn't actually open the way you think — rather
  than necessarily an attack, so treat it as "worth a look," not
  "critical."
- **Low-and-slow port scan** — a scan deliberately paced to stay under
  the fast port-scan detector's short `portScanWindow`. Judged over the
  much longer `lowSlowScanWindow` (hours, not seconds), and deliberately
  *not* a single "distinct ports per hour" threshold — that alone is
  exactly the kind of thing container orchestration, health checks, and
  browsers legitimately trip. Instead, all of the following must hold at
  once before anything fires:
  - **Destination breadth on both axes** — `lowSlowScanPortThreshold`+
    distinct destination ports *and* `lowSlowScanHostThreshold`+ distinct
    destination hosts within the window. A real scan spans many
    host:port pairs, not many ports probed against one already-known
    host (that's the fast port-scan detector's job) or one port probed
    against many known hosts.
  - **A source's own destination-breadth rate vs. its own baseline** —
    same per-source EMA technique as activity-spike, at
    `lowSlowScanBaselineMultiplier`× or more, so a source that always
    talks to many destinations isn't flagged just for continuing to do
    what it always does.
  - **A high drop/reject ratio** — at least `lowSlowScanDropRatio` of the
    source's tracked attempts within the window must have been refused.
    Paced scan traffic against a target mostly gets dropped/rejected;
    legitimate low-rate access to real services mostly gets accepted.
  - **A minimum observation floor** — a source must have been under
    observation for at least `lowSlowScanMinObservation` before it's
    eligible to fire at all, so a source only seen briefly can't produce
    a flag no matter how its first few readings look.

  Confidence is the *minimum* of four sub-scores (port overshoot, host
  overshoot, drop-ratio strength, baseline deviation) — the
  weakest-clearing signal bounds the overall score, consistent with
  requiring several independent signals rather than trusting the
  strongest one alone.
- **Device silence (issue #98)** — a *configured* device (`devices` in
  this file, `Configured: true` in `GET /api/devices`) whose `lastSeen`
  hasn't advanced in `deviceStaleAfter`. Unlike every other detector
  above, this isn't a per-event pattern — it's the *absence* of events —
  so it's checked periodically (same ticker-based shape as global volume
  spike) rather than as events arrive. Only devices that have sent at
  least one event are eligible: a freshly configured device that hasn't
  said anything yet is "never seen," a distinct state `GET /api/devices`
  also reports, not "gone silent." Auto-discovered sources (seen on the
  wire but not listed in `devices`) are never eligible either — there's
  no expected cadence to fall silent from. Set `deviceStaleAfter: 0` to
  disable this detector entirely; the default (15m) sits well above
  RouterOS's normal bursty syslog gaps so an ordinarily quiet stretch
  never false-positives. The flag's target is the device's configured
  `id`, not an IP.

- **Stale rule (issue #102)** — a firewall rule that hasn't fired in
  `staleRuleDays`+ days, either dead weight or, worse, an unnecessary
  hole — flagging it for review closes attack surface at essentially no
  cost. Unlike every other detector above, this one doesn't watch the
  live event stream: it reads a separate, long-lived per-rule
  `firstSeen`/`lastSeen`/`count` record (persisted to
  `ruleUsageStorePath`, same optional-persistence contract as
  `flags.storePath`) that's updated on every ingested event alongside
  `internal/store`'s own in-memory rule counters, then periodically
  swept every `staleRuleCheckInterval` for anything past the threshold.
  That separate record exists specifically because `internal/store`'s
  counters are windowed to `store.retention` (24h by default) — nowhere
  near long enough to notice "hasn't fired in a month." **Accepted
  trade-off:** mikroview only sees a rule when it fires in syslog, with
  no visibility into the router's actual configured rule set (it's
  passive-syslog-only) — so a rule you've already removed will keep
  surfacing as stale until you manually clear the flag. Harmless: the
  implied suggestion ("consider removing this") is a no-op if it's
  already gone, and the alternative failure mode — a genuinely
  forgotten, still-open rule going unflagged — is worse. Unlike every
  other detector, stale-rule doesn't currently support the live
  enable/scope toggle described in [Per-detector
  toggles](#per-detector-toggles-and-scope-restrictions-optional), and
  doesn't attach a confidence score (see below) — set `staleRuleDays`
  high enough (or clear flags as they come up) if it's too noisy for
  your ruleset.

- **Unexpected mail sender (issue #108)** — a LAN source originating an
  outbound connection to an external destination on an SMTP port (25,
  465, or 587) that isn't tagged `trusted-mail-sender` on its host entity
  (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)
  above). Deterministic, like new-device and stale-rule detection — no
  threshold or window to tune, it fires the first time a given untagged
  source does this at all. Distinct from **Outbound anomaly** above,
  which only fires on distinct-*destination-count* spread over a window
  — a single new SMTP connection to one destination wouldn't trip that.
  If you self-host your own outbound mail server, tag its host entity
  `trusted-mail-sender` once (**Menu → Entities**, admin-only, or `POST
  /api/entities`) and mikroview never flags it for this again. Like
  stale-rule, this doesn't currently support the live enable/scope
  toggle described in [Per-detector
  toggles](#per-detector-toggles-and-scope-restrictions-optional), and
  doesn't attach a confidence score (see below).

- **Off-hours activity** — a source active during a fixed clock window
  (`offHoursStartHour`-`offHoursEndHour`, wrapping past midnight, 23:00-
  06:00 by default) it has no established history of being active in.
  Extends the same per-host EMA baseline technique activity-spike uses,
  but tracked 24 times over — one independent baseline per hour of the
  day — rather than once per host, so "usual for this host at 3am" and
  "usual for this host at 3pm" are judged separately. Deliberately
  *not* "any activity inside the configured window": that alone fires
  on trivial noise (a phone syncing, a scheduled job, a clock-skewed
  IoT device) with nothing to judge it against. Two independent floors
  gate every flag, both required:
  - **`offHoursMinSampleDays`** — that specific hour must have been
    observed on at least this many distinct *prior days* before a flag
    can fire for it at all, no matter how extreme the count. A single
    busy night isn't a baseline; this is what makes firing on one
    impossible rather than just unlikely.
  - **`offHoursMinCount`** — an absolute floor on top of the z-score
    check. A host never before seen at some hour has a near-zero
    baseline for it, so even a handful of events there would look like
    a huge deviation by z-score alone — this is the same role
    `activitySpikeThreshold` plays alongside `hostActivityMultiplier`
    for activity-spike.

  Every hour's baseline is tracked continuously regardless of the
  configured window — only whether a flag can *fire* is restricted to
  hours inside it, so widening or narrowing the window later doesn't
  discard history already being collected. "Off-hours" here is a fixed,
  operator-set window rather than a per-host-learned quiet period — an
  explicit design choice (the issue that scoped this feature left it
  open): a learned window has its own bootstrapping problem (deciding
  which hours are "quiet" needs the same history this detector is
  already gated on) and is harder for a human reviewing a flag to
  sanity-check than a plain clock range. Revisit if real-world use shows
  a fixed window doesn't fit typical deployments well.

**VPN interface confidence boost (issue #105, optional).** `vpnInterfaces`
identifies which of RouterOS's `InInterface` values correspond to a
configured VPN tunnel (e.g. a WireGuard interface such as `wireguard1`)
-- each entry is matched with glob syntax (`*`, `?`, `[...]`), so an
exact name (`wireguard1`) or a prefix pattern (`wireguard*`, for a
router with several tunnel interfaces) both work through the same
mechanism. RouterOS firewall log lines already see a WireGuard tunnel's
*inner*, already-decrypted traffic -- the peer's tunnel IP as `SrcIP`,
arriving on whatever interface name RouterOS assigns the WireGuard
interface -- which is exactly what `InInterface` captures, no RouterOS
API access required.

When **activity-spike**, **outbound-anomaly**, or **internal-recon**
raises a flag whose triggering event arrived via a matching interface,
its confidence score is boosted by `vpnConfidenceMultiplier` (a flat
multiplier on the already-computed score, clamped to 100 -- never a
change to whether or when the detector fires, only how urgently an
already-firing flag reads). The reasoning: a remote peer that already
had to pass WireGuard's own key-based authentication to reach the
network at all suddenly beaconing out, scanning the LAN, or generating
unusual volume is a stronger signal than an ordinary LAN device doing
the same. Left empty (the default), `vpnInterfaces` matches nothing, so
every existing deployment's confidence scoring is completely unchanged
until this is explicitly configured -- a safe, backward-compatible
default. A `vpnConfidenceMultiplier` of `0` or less is treated as `1`
(no boost) rather than zeroing or inverting a score, and the boost is
never allowed to end up *lower* than the unboosted value either -- a
misconfigured multiplier should never make a VPN-sourced anomaly read
as less alarming than an identical LAN-sourced one.

**Deliberately out of scope**, for now: tracking the WireGuard peer's
*outer* UDP endpoint (their real internet source IP) or handshake state.
Firewall logs never see that -- it only exists in
`/interface/wireguard/peers` on the router itself, which mikroview has
no access to today (passive syslog only, no RouterOS API client). That
data is what would let mikroview tell "this peer roamed to a new IP"
(normal for a mobile client) apart from "this peer's private key was
stolen and is now being used from somewhere else" (a real compromise
signal) -- arguably the more interesting half of "VPN peer anomaly,"
but it's blocked on [issue #21](https://github.com/tomlawesome/mikroview/issues/21)
deciding whether/how mikroview talks to the RouterOS API at all.

**Confidence score.** Every detector except global-volume-spike and
rule-hit-rate-spike attaches a `confidence` percentage (0-100) to each
flag it raises, shown in the UI as e.g. "73% confidence" — but not all
of them mean the same thing by it, and it's worth knowing which kind
you're looking at:

- **Statistical (activity-spike, off-hours activity).** These two make a
  real statistical judgment call rather than a deterministic threshold
  crossing: each combines (1) how much history backs the relevant
  baseline and (2) how far the current reading deviates from it (a
  z-score). Off-hours activity applies this per hour-of-day rather than
  once per host — "how much history" is that specific hour's distinct
  prior days (`offHoursMinSampleDays`), not overall event count. Both
  flags' own detail text spells out the actual baseline value, observed
  value, and sample count behind the score.
- **Overshoot-based (port-scan, critical-port, distributed
  brute-force, outbound anomaly, internal reconnaissance, repeated
  drops, device silence).** These seven are plain threshold crossings
  with no history or baseline behind them, so their confidence instead
  measures *how far past the threshold* the observed count is: just
  crossed reads low, three times the threshold or more reads 100%. For
  device silence specifically, the "count" is how long the device has
  gone quiet relative to `deviceStaleAfter`. This is a materially
  weaker claim than activity-spike's statistical score — it says
  nothing about whether the pattern is unusual for that specific
  source, only how large it is relative to the configured line. Treat a
  95% overshoot-based flag as "well past the configured threshold," not
  "95% likely to be malicious."
- **Composite (low-and-slow port scan only).** The minimum of four
  sub-scores — port overshoot, host overshoot, drop-ratio strength, and
  baseline deviation (the last computed the same way as activity-spike's
  statistical score). Reflects "how convincingly did the *weakest*
  required signal clear," not an average or the strongest signal alone —
  deliberately conservative, since this detector already requires
  several independent things to be true before it fires at all.
- **Not yet scored (global volume spike, rule hit-rate spike).** Both
  already track a slow-moving EMA baseline internally (same technique
  activity-spike uses), just without a confidence score attached yet —
  a known gap, filed separately.
- **Not scored (stale rule, unexpected mail sender).** Both are
  deterministic "has this happened at all" checks — stale rule's "has
  this rule's `lastSeen` crossed `staleRuleDays`," unexpected mail
  sender's "has this untagged source ever done this before" — with no
  baseline or overshoot concept behind either one, so there's nothing to
  score.

`confidence` is `null` in `GET /api/flags` for a detector that doesn't
score at all, never an implied 100%.

**Reputation-informed floor (optional, requires an AbuseIPDB key).**
When `reputation.abuseIPDBKey` is configured (see
[IP reputation lookup](#ip-reputation-lookup-optional)), `critical_port`,
`port_scan`, `activity_spike`, `repeated_drops`, `low_slow_scan`, and
`off_hours_activity`
additionally get an
async, best-effort AbuseIPDB lookup against the flag's source IP the
first time it's raised (not on every re-fire). If the IP has a known
abuse score, that score becomes a *floor* on the flag's confidence —
`finalConfidence = max(existingConfidence, abuseScore)` — never a
replacement and never a reduction: a clean or unavailable reputation
result is absence of evidence, not evidence of innocence, so it's never
allowed to pull an already-higher score down. `distributed_brute_force`
and `outbound_anomaly` get the same treatment, but against a *sampled
group* rather than one IP (they're keyed by many distinct source IPs or
destinations, not a single one) — up to 10 members are sampled, and at
least 3 of them must actually return reputation data before the
aggregate (their mean score, discounted by how much of the sample was
actually filled) is trusted enough to apply. `internal_recon` is
excluded because its destinations are internal LAN IPs and reputation
data doesn't exist for private ranges at all; `rule_spike` is excluded
because its target is a rule label, not an IP. This reuses the same
`internal/reputation.Client` and 15-minute cache the on-demand IP-lookup
popover already uses, so a manual lookup you've just done and an async
detector check for the same IP share results rather than double-querying
AbuseIPDB.

The same lookup also captures a **reputation snapshot** on the flag
(abuse score, report count, ISP, Shodan ports/hostnames/vulns where
available) *as of when it resolved* -- not fetched live later, since the
point is what the target looked like when it fired, not what it looks
like now. It's stored regardless of whether AbuseIPDB is configured
(the keyless Shodan source alone is still worth capturing) -- only the
confidence floor specifically needs an AbuseIPDB score. Only ever set
for the six single-IP detectors above; the sampled-group detectors
have no single coherent snapshot to attach. Expandable in the UI
alongside the target's country (from the same GeoIP lookup already
applied to the underlying event) and, for `port_scan`,
`distributed_brute_force`, `outbound_anomaly`, `internal_recon`, and
`low_slow_scan`, the specific ports/hosts actually involved -- and for any detector, NAT
translation info when the triggering event had one. `GET /api/flags`
exposes all of this as `reputation`, `country`, and `evidence` fields.

**Tor/hosting-provider signal (issue #58).** AbuseIPDB's response
already includes an `isTor` flag and a `usageType` classification (e.g.
"Data Center/Web Hosting/Transit") that mikroview now captures alongside
the abuse score. Either one also contributes to the confidence floor,
using the same floor-raise-only reasoning as the abuse score itself,
with the *strongest* of the two independent signals applied: a Tor exit
node raises the floor to 60, a hosting/data-center address to 30 --
deliberately smaller than a real abuse score, since neither is proof of
malice on its own (Tor use isn't illegal, and plenty of legitimate
scanners/CDNs/bots run from hosting providers too). Both are starting
points, not calibrated values. Shown in the expanded reputation detail
alongside ISP/country whenever present.

A flag is raised once per (detector, source) pair and updated in place
on re-firing (count/last-seen bumped, not duplicated) until a human
clears it via the UI or `POST /api/flags/{id}/clear`. Clearing an
already-active-again source re-raises it as a fresh entry rather than
silently resurrecting the old one.

A **Clear all** button above the active list (issue #198) clears every
active flag in one request (`POST /api/flags/clear-all`) -- a click-again
red "Confirm" is the safeguard against an accidental single click, not a
modal. It performs regular clears only and never creates a permanent
exclusion; there is no bulk variant of the action below.

Each flag's Clear button is a split control: the main segment is the
plain Clear above, and its arrow segment opens "Permanently clear"
(`POST /api/flags/{id}/clear-permanent`, **admin-only** once an account
exists, and recorded in the audit log) -- for a non-admin the arrow
segment is hidden entirely, leaving a plain Clear button rather than a
disabled one that would just advertise an action they can't take.
"Permanently clear" clears the flag *and* permanently
excludes that exact (detector, target) pair -- from then on it never
raises again, silently, until the exclusion is removed. This is
deliberately permanent rather than a timed snooze: a time-limited mute
either re-fires once it expires (nothing was solved) or it doesn't
(permanent exclusion was what was wanted all along), so there's no
in-between "snooze" option. Because "permanent" shouldn't mean
"unrecoverable by mistake," every current exclusion is listed (and can
be removed, re-enabling that pair) on its own **Exclusions** page,
reachable from the menu -- admin-only, same as every other admin-gated
endpoint (see [Authentication](#authentication)). It was split out of
the bottom of the Flags page (issue #207) because reviewing exclusions
underneath a list of hundreds of active flags was a pain.

## New-device detection (optional, on by default)

Raises a `new_device` flag the first time mikroview ever sees a given
LAN client MAC address (`store.Event.SrcMAC`). Unlike every detector
above, this isn't a threshold-crossing or a statistical judgment -- it's
a deterministic "have I ever seen this MAC before," so there's nothing
to tune: it fires exactly once per genuinely-new MAC, the moment it's
first observed, and never again for that same MAC.

```yaml
deviceMac:
  storePath: "/var/lib/mikroview/mac-registry.json"
```

- **`storePath`** — where the registry of every MAC address mikroview
  has ever seen (just a MAC plus first/last-seen timestamps) is
  persisted, as a small JSON file. This needs its own store, separate
  from `flags.storePath` above: it must survive a restart for "new" to
  mean anything at all -- the 24h event-retention window alone is
  nowhere near long enough. Left empty (not the default), detection
  still runs, it just starts with an empty registry -- and so treats
  every MAC as new again -- on every restart. Same optional-persistence
  contract as `flags.storePath`; if you set this in the container, mount
  a volume for its parent directory -- see `deploy/docker-compose.yml`.

**Coverage.** `SrcMAC` is only present on RouterOS log lines from
LAN-side/bridge-aware firewall rules -- by the time traffic reaches a
WAN-side rule, its Layer 2 source-MAC information is already gone
(that's how routing works, not a mikroview limitation). If your
firewall ruleset only logs WAN-side traffic, this detector simply never
fires; that's a data-availability gap, not a bug.

**No confidence score.** "Is this MAC new" is a deterministic yes/no,
but a brand-new device on your network is very often entirely benign (a
phone joining the Wi-Fi) -- there's no meaningful numeric judgment call
to attach on top of the deterministic fact. `confidence` is always
`null` for `new_device` flags in `GET /api/flags`, same as the two "not
yet scored" detectors above.

## Notifications (optional)

Flags are only visible if someone has the mikroview UI open. `notify`
sends an alert through one or more external channels whenever a new
flag *episode* is raised (a first-ever raise, or a revival after a human
clears an already-cleared flag -- never a plain re-fire of an
already-active one, so a noisy detector doesn't re-alert on every
event). Each channel below is independently enabled by its own
identifying field being set (`smtp.host`, `pushover.token`,
`webhook.url`) -- configure any combination of zero, one, two, or all
three; every enabled channel shares one `batchWindow`.

```yaml
notify:
  # batchWindow: how often pending flags are flushed to every enabled
  # channel -- a fixed interval, not a quiet-period debounce, so a
  # sustained flood of flags during a real incident still gets a
  # bounded max delay before alerting rather than the window
  # continuously resetting.
  batchWindow: 60s
  smtp:
    host: "smtp.example.com"
    port: 587
    # username left empty means no AUTH is attempted, for an open local
    # relay (e.g. a Postfix instance on the same host/network).
    username: "alerts@example.com"
    password: "changeme"
    # "" (plaintext, local relay only), "starttls" (upgrade after
    # connecting, typically port 587), or "implicit" (TLS from the
    # first byte, typically port 465).
    tlsMode: "starttls"
    from: "mikroview@example.com"
    to: ["ops@example.com", "oncall@example.com"]
  pushover:
    # From your Pushover application (https://pushover.net/apps) and
    # account/group, respectively -- no VAPID keys, no service worker,
    # no per-browser subscription management, just this pair.
    token: "your-application-token"
    user: "your-user-or-group-key"
  webhook:
    # Any HTTPS endpoint that accepts a JSON POST -- ntfy, Discord
    # (via a webhook URL's own payload adapter or a receiving proxy),
    # Slack (same), Home Assistant's webhook trigger, n8n, or a
    # bespoke receiver of your own. No bespoke per-service integration
    # here on purpose -- this is the "everything else" channel.
    url: "https://ntfy.example.com/mikroview-alerts"
    # headers: arbitrary extra headers sent with every POST -- most
    # commonly auth, since ntfy/Home Assistant/n8n-style receivers each
    # expect it in a different header rather than agreeing on one
    # convention. Optional; omit entirely for a receiver that needs no
    # auth (e.g. an unauthenticated local ntfy topic).
    headers:
      Authorization: "Bearer changeme"
      # X-Custom-Header: "some-other-receiver-specific-value"
```

Left with an empty `smtp.host`/`pushover.token`/`webhook.url` (the
default), that channel is simply never dispatched to -- no relay, app,
or endpoint is assumed. `smtp.password`/`pushover.token`/`pushover.user`/
`webhook.url` can also be set via env vars instead of the config file
(see the table below), same secret-via-env precedent as
`MIKROVIEW_ABUSEIPDB_KEY`; `webhook.headers` is YAML-only (a map doesn't
map cleanly onto one env var, same reasoning `flags.detectors` and the
device/naming maps already give).

One notification covers every flag raised within a `batchWindow`: title/
subject `mikroview: N new flag(s)`, one line per flag (type, target,
detail, confidence, first-seen) -- Pushover's message additionally caps
at 10 lines with a "...and N more" trailer, since its message field is
much smaller than an email body. The webhook channel instead POSTs a
JSON body shaped `{"title": "...", "count": N, "flags": [...]}`, where
each entry in `flags` is the same flag record the UI itself renders
(type, target, detail, count, first/last seen, confidence) -- a generic
consumer gets the full structured batch to template off of, rather than
a pre-rendered string. Built around a shared
`internal/notify.Notifier`/`Dispatcher` so every channel reuses the same
batching rather than each implementing their own; true device push (web
push API, VAPID keys, service worker) is a separate, not-yet-built
target scoped alongside PWA feasibility, since a lot of that plumbing
overlaps with making the frontend a PWA at all.

## Per-detector toggles and scope restrictions (optional)

Every detector above defaults to enabled and unscoped -- this section
lets you turn one off entirely, or narrow where it applies, without
touching its thresholds. Settings live in two places that layer
together:

- **`config.yaml`** sets the *starting point* on boot (`flags.detectors`
  below).
- **A live, admin-only UI** ("Detectors" in the toolbar, visible once
  signed in as an admin) can override that starting point without a
  restart -- takes effect on the very next ingested event. A live change
  persists to `detectorSettingsStorePath` (same optional-persistence
  contract as `flags.storePath` above: left unset, a live toggle still
  works, it just resets to `config.yaml`'s values on restart) and, once
  persisted, is what future restarts seed from -- `config.yaml`'s values
  are only ever consulted the *first* time that file doesn't exist yet.

```yaml
flags:
  detectorSettingsStorePath: "/var/lib/mikroview/detector-settings.json"
  detectors:
    critical_port:
      enabled: true
      scope:
        hosts: ["203.0.113.0/24"]
        hostsMode: deny
        ports: [22, 3389]
        portsMode: allow
        classification: external
    rule_spike:
      enabled: false
```

Each detector under `detectors` is optional -- an omitted detector keeps
today's default (enabled, unscoped). Within one detector's `scope`, every
field is also optional and independently combines with the others by
**AND**: an event only counts toward that detector if it satisfies every
restriction you've actually set. Within one field (`hosts`, `ports`, or
`rules`), `*Mode` is either `allow` (only listed entries are admitted) or
`deny` (listed entries are excluded) -- never both directions on the same
field at once. `hosts` entries accept a bare IP or a CIDR.

Not every field means something to every detector -- a detector only
consults the axes relevant to how it's keyed:

| Detector | `hosts` + `classification` restrict | `ports` restrict | `rules` restrict |
|---|---|---|---|
| `port_scan` | which source IPs are tracked at all | which distinct ports *count* toward the scan total (not which events are tracked) | -- |
| `activity_spike` | which source IPs are tracked at all | -- | -- |
| `critical_port` | source IP | the effective subset of `criticalPorts` this instance reacts to | -- |
| `distributed_brute_force` | which source IPs count toward a port's distinct-source total | the effective subset of `criticalPorts` | -- |
| `outbound_anomaly` | which source (LAN) IPs are watched | -- | -- |
| `internal_recon` | which source (LAN) IPs are watched | -- | -- |
| `rule_spike` | -- | -- | which rule labels this detector reacts to |
| `repeated_drops` | source IP | destination port | -- |
| `global_spike` | -- (network-wide, not keyed by anything per-source) | -- | -- |
| `low_slow_scan` | which source IPs are tracked at all | which distinct ports *count* toward its own breadth total | -- |
| `off_hours_activity` | which source IPs are tracked at all | -- | -- |
| `device_silence` | -- (a per-configured-device sweep, not keyed by host/port/rule) | -- | -- |

`global_spike` and `device_silence` only ever consult `enabled` -- the
former is a single network-wide aggregate, the latter a periodic sweep
over the whole configured device list, neither tied to any particular
host, port, or rule, so scoping either wouldn't mean anything.

## Authentication

The first time mikroview loads with no accounts and no prior decision,
it shows a one-time choice screen instead of the live view: **create
the admin account**. That is the only option -- running without
authentication was removed, and creating an account is the floor. See
[SECURITY.md](../SECURITY.md) for the full threat-model writeup; this
section is the configuration reference.

```yaml
auth:
  storePath: "/var/lib/mikroview/users.json"
  secureCookie: true
  sessionTTL: 24h
  tokensStorePath: "/var/lib/mikroview/tokens.json"
```

- **`storePath`** — where accounts are persisted, as a small JSON file (usernames + Argon2id password hashes,
  never plaintext). Defaults to `/var/lib/mikroview/users.json`, which
  the Dockerfile creates and owns -- no configuration needed for the
  zero-config case. Mount a volume over `/var/lib/mikroview` if you want
  the decision (and any accounts) to survive container recreation, not
  just process restarts -- see `deploy/docker-compose.yml`.
- **`secureCookie`** — sets the session cookie's `Secure` flag. On by
  default, matching [TLS](#tls) being on by default -- there's no other
  kind of connection to have a session on. Only turn this off if you've
  also set `tls.enabled: false`, or sessions won't work at all.

  Setting it to `false` while `tls.enabled` is `true` stops mikroview
  starting, with [CFG-0021](#cfg-0021) — the cookie would be sendable
  over a plain connection the deployment has otherwise ruled out. The
  env var is `MIKROVIEW_AUTH_SECURE_COOKIE`.
- **`sessionTTL`** — the idle timeout: a session's expiry slides forward
  on each authenticated request, so this is "how long you can go without
  activity before needing to log in again," not a fixed session
  lifetime.
- **`tokensStorePath`** — where [API tokens](#api-tokens-read-only) are
  persisted, as a small JSON file (names + SHA-256 hashes, never the raw
  bearer values). Defaults to `/var/lib/mikroview/tokens.json`. Unlike
  `storePath`, this really is optional: a deployment that never creates
  a token doesn't need it.

**Once you create the account**, every request except `GET /api/healthz`
and the login/session endpoints requires a valid session, permanently,
from then on. Whoever completes the form becomes the admin.

**Until then, mikroview serves nothing else.** There is no "run it
without a login" option. An earlier version had one, and it was removed:
an open mikroview shows anyone who can reach it which of your hosts are
being scanned, which rules are firing, which ports are under pressure,
and which accounts exist. That is a map of your network, and "it's only
for five minutes while I try it out" is exactly how a deployment ends up
open for a year. Creating an account is one form.

> Upgrading from a version that let you run without a login? See
> [CHANGELOG.md](../CHANGELOG.md) for what to do.

### Backing up and restoring

```sh
mikroview -backup /secure/place/mikroview-backup.json.gz
mikroview -restore /secure/place/mikroview-backup.json.gz
```

One gzipped file holding every store: accounts, API tokens, recovery-key
digests, flags, rule usage, detector settings, entities, the MAC
registry, the audit log, the watchlist, watchlist suggestions, and the
watchlist match log.

Three things are deliberately left out, and always have been:

- **The TLS certificate and key material** (`tls.storePath`) — a
  directory of generated key material, not a single document like every
  store above, and restoring it onto a different host is more likely to
  be wrong than right (different hostname/IP SANs, a CA nothing there
  has trusted yet). It regenerates on its own the next time mikroview
  starts without it.
- **The recovery pepper** (`auth.recoveryPepperPath`) — the secret mixed
  into every recovery-key digest. Keeping it out means a stolen backup
  carries the digests and nothing able to verify them against.
- **The GeoIP database** (`geoip.dbPath`) — a file you downloaded from
  MaxMind yourself, not something mikroview wrote. A fresh download
  replaces it exactly.

**It contains your credentials.** That is deliberate — a backup that
leaves them out cannot restore a working system, and you would find that
out during a disaster. So treat the file exactly as you would the data
directory. MikroView writes it `0600`, refuses to overwrite an existing
file, and refuses to write into a world-readable directory unless you
pass `--force`.

**Restoring never half-happens.** The file is fully parsed and checked
before anything on disk is touched, and each store is written to a temp
file and renamed into place. A corrupt or truncated backup leaves your
existing state exactly as it was — which matters most for the accounts
file, where the alternative to "unchanged" is "locked out".

`-restore` refuses to overwrite stores that already exist unless you pass
`--force`.

> **Using Postgres?** These commands refuse, on purpose. Back up the
> database with `pg_dump` or your provider's snapshots — see
> [CHANGELOG.md](../CHANGELOG.md) and the migration section above.

### Adding and removing people

Open **Menu → Users**. Only the admin sees it.

![The Users panel, showing the admin account and one ordinary user](screenshots/users-panel.png)

Type a username and password, press **Add**, and the account appears in
the list. Everyone added here can see everything MikroView shows, but
can't change settings, manage accounts, or create API tokens.

**Delete** removes an account. That person is signed out straight away,
on any device, and any API tokens they created stop working at the same
moment.

The admin account has no Delete button. There is exactly one admin, and
moving that role is a command-line step (see below) — so nobody who gets
hold of an admin's browser session can take ownership of your
deployment or lock you out of it.

### Connecting your account to SSO

If your deployment has SSO set up, you can switch your own account over
to it: **Menu → Connect SSO**. You'll be sent to your identity provider
to sign in, and when you come back the account uses SSO from then on.

**This deletes your MikroView password, and can't be undone from
MikroView.** After connecting:

- You sign in through your identity provider only.
- If you lose access to that provider, MikroView can't recover the
  account for you — that includes the admin account, so read
  "Recovering the admin account" above before connecting the admin.
- You stay signed in on the browser you did it from. Anywhere else
  you're signed in gets signed out.

There's deliberately no halfway state where both your old password and
SSO work. Keeping the password alive would leave the weaker way in
open on an account that was supposed to have moved past it.

If that SSO identity is already connected to someone else's account,
the attempt is refused and nothing changes.

### Handing admin to someone else

Only from the command line, and only with a recovery key:

```sh
mikroview -transfer-admin
```

It tells you who currently holds admin and asks for a recovery key.
Then it lists the other accounts, numbered, and you pick one:

```
Admin is currently "alice".
Recovery key: ····

Transfer admin to:

   1) bob
   2) carol   (signs in via SSO -- mikroview cannot recover this account)

Choose 1-2, or anything else to cancel:
```

You don't need to know the exact username in advance, which matters
because the usual reason to run this is that you can't sign in to look
it up.

**Backing out here costs nothing.** Your recovery key isn't spent until
the transfer actually happens, so cancelling at the list — or at the
SSO warning — leaves your existing keys working. That's deliberate: a
key that got used up by a cancelled command, or by a mistyped one, would
mean three false starts locks you out for good.

It's safe to leave the key valid because typing it doesn't expose it.
It isn't echoed as you type, isn't accepted as a command-line argument,
and is never written to the log — so a key you presented and then didn't
use hasn't been left lying anywhere.

If you already know the username, pass it and skip the list:
`mikroview -transfer-admin bob`.

Once it's done you get a replacement set of keys, handed over the same
way as `-generate-recovery-keys` below. The previous admin becomes an
ordinary user — their account isn't deleted.

If the person you're handing it to signs in through SSO, you're warned
before the key is asked for: MikroView won't be able to recover that
account itself if they ever lose access, because it holds no password
for them.

### Recovery keys

Recovery keys are the second thing — besides access to the machine
itself — needed to recover or transfer the admin account. They're
created once, handed over once, and stored hashed:

```sh
mikroview -generate-recovery-keys
```

You get three. Any one of them works, and using one replaces all three,
so treat them as one key with two spares — they're there in case a
printout smudges or a paste goes wrong, not as three separate uses.
Keep them somewhere safe, such as a password manager.

**Run this with `docker compose exec`, not `docker compose run`:**

```sh
docker compose exec mikroview /mikroview -generate-recovery-keys
```

The reason is that in a container, whatever the main process prints goes
into the container log — kept on disk, and usually shipped off to a
central log system. A recovery key sitting there is an admin takeover
for anyone who can read your logs. `docker exec` runs inside the
container that's already going, and its output isn't logged; it goes to
your terminal and nowhere else.

MikroView checks which one you used and refuses to print if it's the
wrong one, so you can't get this wrong by accident. The same applies to
`-recover-admin-account` and `-transfer-admin`, which also show you a
fresh set when they finish.

Not using containers? Then your terminal is just a terminal and the
command works as-is.

The keys are never written to a file. You are shown them once, and that
is the only copy — get them into a password manager before you type
`saved`.

You can't regenerate them while a set exists — that would let anyone
with access to the machine mint themselves a fresh key and walk straight
through the gate. They rotate automatically after each use instead.

### Recovering the admin account

If you can't sign in as the admin, you get back in from the command
line, not from the web interface -- so being locked out of mikroview
doesn't stop you fixing it. You need two things: access to the machine
(or container) mikroview runs on, and one of your recovery keys.

```sh
mikroview -recover-admin-account   # asks for a recovery key, then a new password
```

It asks for the recovery key first. If the key is wrong, nothing
changes and no key is used up. If it's right, you're asked for a new
password twice (it isn't shown as you type, and it's never passed as a
command-line argument, so it stays out of your shell history).

Once the password is set, you get a **new set of recovery keys**, shown
the same way as above. The old
ones stop working as soon as you confirm you've saved the new ones -- so
save them before typing `saved`. If anything goes wrong at that point,
your original keys stay valid.

Changing the password signs out that account everywhere immediately,
including on an already-running server. You don't need to restart
mikroview.

Two things this command deliberately won't do:

- **It only recovers the admin account.** Other people's accounts are
  managed by the admin from the web interface.
- **It can't help if the admin signs in through SSO only.** There's no
  password for mikroview to reset -- reset it at your identity
  provider, or use `-transfer-admin` to move the admin role to an
  account that does have a mikroview password.

If you have no recovery keys yet (your deployment predates them), run
`mikroview -generate-recovery-keys` once, on a terminal, and store what
it prints.

**A corrupt or unreadable accounts file refuses to boot, rather than
silently reopening.** If `auth.storePath` points at a file that exists
but can't be loaded, mikroview exits immediately with an error rather
than falling back to an empty, zero-account state -- the same state a
genuine fresh install starts from, which would otherwise mean a lost or
corrupted accounts file silently presents the first-run setup screen to
whoever loads mikroview next, indistinguishable from a real fresh
install in both behavior and the logs. A missing file (no persistence
configured, or a genuine first-ever boot) is unaffected and boots
normally either way -- only a file that exists but won't parse triggers
this. The startup error names the exact path; restore it from a backup
if you have one, or move/delete it and restart to consciously re-arm
the first-run setup screen -- container/host access is the trust
anchor here, the same as the recovery commands above, so there's no
separate CLI mode for what `mv`/`rm` already does.

## API tokens (read-only)

For a separate service to pull mikroview's data over the network with
no browser involved -- e.g. a companion OpenCanary-dashboard project
cross-referencing incidents against mikroview's event/flag history -- a
session cookie doesn't work: there's no login flow to hold one. API
tokens are a long-lived bearer credential for exactly that case,
admin-created from the "API tokens" panel in the menu's Account section
(or directly via the API below).

**Scope is deliberately narrow: read-only, five endpoints, nothing
else.** A valid token grants `Authorization: Bearer <token>` access to:

- `GET /api/events`
- `GET /api/flags`
- `GET /api/stats`
- `GET /api/devices`
- `GET /api/watchlist/matches`

and *nothing* else -- no clearing a flag, no changing a detector, no
managing users or other tokens, regardless of the method or path
requested. This isn't a per-request permission check that a future
handler could accidentally bypass: a bearer-authenticated request is
routed to a completely separate, minimal internal router that simply
has no other handler registered on it, so there is no code path from a
valid token to anything else, structurally rather than by convention.

A token's raw value is shown exactly once, at creation, and is not
recoverable afterward -- only the SHA-256 hash of it is ever persisted
(not Argon2id: that cost exists to slow down guessing a low-entropy,
human-chosen password, and a token's value is already a 128-bit random
string, well outside brute-forceable range). Losing the value means
issuing a new token; there's no way to view an existing one again.

There is no expiry -- like sessions and accounts, a token stays valid
until explicitly revoked from the same panel (or `DELETE
/api/tokens/{id}`).

```sh
curl -H "Authorization: Bearer <token>" https://mikroview.example.com/api/events
```

### Ingest tokens (RouterOS push)

`POST /api/tokens` also accepts `"kind": "ingest"` alongside `"device":
"<name>"` -- a second, entirely separate credential for the RouterOS
push-ingest integration (issue #186), scoped to exactly one router. It
grants the opposite access to a read-only API token above: nothing can
be read with it, and it can only be used at `POST
/api/ingest/routeros`, which an API token in turn cannot reach -- the
same structural, separate-router guarantee, just pointed the other way.
One token per router is deliberate: any RouterOS user holding the
`read` policy can print a script's source, ingest token included, so a
leaked one must only ever be able to speak for the single router it was
issued for.

What a push feeds, today: host names (a DNS static entry, DHCP lease
hostname, or WireGuard peer comment pushed by the router names that
address everywhere mikroview shows one -- and **RouterOS always wins**
over a label set in mikroview for the same address, so manage
router-known hosts in RouterOS; labels for anything the router doesn't
name are untouched), and the pushed firewall rule and NAT tables,
served read-only at `GET /api/routeros/{device}/rules` and `.../nat` in
RouterOS's own display order. Pushed state is held in memory only --
never written to disk, never in a backup -- and re-arrives with the
router's next scheduled push, so a mikroview restart costs at most one
push interval of naming/table enrichment and nothing else. Pushed data
never raises, lowers, clears or suppresses a detection: that boundary
is a build-failing test, not a convention.

See [routeros-setup.md](routeros-setup.md#4-push-router-state-for-names-and-rule-lookups-optional)
for the router-side walkthrough -- minting the token, importing
mikroview's certificate, and the script itself, explained line by line.

## Single sign-on (OIDC/SSO)

Optional, additive on top of [local authentication](#authentication) above
-- local login keeps working unmodified whether or not this is
configured. Tested against [Authentik](https://goauthentik.io/), and
works with any standard **self-hosted** OIDC provider (generic
discovery, no Authentik-specific behavior). Multi-tenant public
providers are not supported -- see [Supported providers](#supported-providers).

```yaml
oidc:
  issuerUrl: "https://authentik.example.com/application/o/mikroview/"
  clientId: "mikroview"
  clientSecret: "changeme"
  publicBaseUrl: "https://mikroview.example.com"
  scopes: ["openid", "profile", "email"] # this is the default if omitted

  # Which accounts in your directory may sign in. All optional, all
  # default to empty (anyone the issuer vouches for). Every field you
  # set adds a condition, and all set conditions must hold. See
  # "Restricting which accounts in your directory can sign in" below.
  #
  # allowedGroups: ["mikroview-admins", "netops"]
  # groupsClaim: "groups"          # default; Azure often needs "roles"
  # allowedEmails: ["you@example.com"]
  # allowedEmailDomains: ["example.com"]
  # requiredClaims:                # the general form the above are sugar over
  #   some_claim: ["expected-value"]
```

- **`issuerUrl`** — the provider's issuer URL (Authentik: your
  application's **OpenID Configuration Issuer**, found on the
  provider's overview page -- it ends in `/application/o/<slug>/`, not
  just your Authentik host). Empty (the default) means SSO is not
  configured at all -- no separate `enabled` flag, since there's no
  scenario where a fully-populated block should be silently inert.
- **`clientId`/`clientSecret`** — from the provider's confidential
  OAuth2/OIDC client registration (see the Authentik walkthrough
  below). `clientSecret` can also be set via
  `MIKROVIEW_OIDC_CLIENT_SECRET` instead of `config.yaml`, the same
  secret-via-env precedent `notify.smtp.password`/`reputation.abuseIPDBKey`
  already have.
- **`publicBaseUrl`** — mikroview's own externally-reachable origin,
  used to build the `redirect_uri` registered at the provider
  (`publicBaseUrl` + `/api/auth/oidc/callback`). Required whenever
  `issuerUrl` is set. Deliberately never inferred from a request's
  `Host` header -- doing so is a known `redirect_uri`-confusion
  vulnerability class. Covers either deployment mode this app
  supports: mikroview's own self-signed TLS on a LAN IP/hostname
  (`https://192.168.1.10:8443`), or fronted by a reverse proxy
  terminating a real domain (`https://mikroview.example.com`).
- **`scopes`** — defaults to `openid`, `profile`, `email` if omitted.
  `openid` is always required regardless of what's listed.

### Supported providers

**Mikroview supports self-hosted identity providers only** — Authentik,
Keycloak, Zitadel, or a Microsoft Entra **single-tenant** issuer URL
(`https://login.microsoftonline.com/<tenant-guid>/v2.0`).

Multi-tenant providers are **refused at startup**: Google
(`accounts.google.com`), Apple, and Microsoft's shared `/common`,
`/organizations` and `/consumers` endpoints. SSO stays disabled and the
reason is logged; local login is unaffected.

The reason is that mikroview's OIDC support rests on the issuer URL
*being* the access control. Every ID token is verified against that
issuer's own signing keys and your client ID, so pointing `issuerUrl` at
a directory you run means only accounts in that directory can sign in.
That isn't true of a public provider — every Google account on earth
produces a valid token against `accounts.google.com` — and because the
first account to register becomes an admin, such a deployment would hand
admin to whoever reached the login page first.

A safe configuration for a public provider is possible (pin a claim
identifying the organisation), and mikroview deliberately does not offer
it: it would make every deployment's safety depend on an operator
getting an extra restriction exactly right, where the failure is silent
and indistinguishable from working correctly. See
[docs/decisions/multi-tenant-oidc.md](decisions/multi-tenant-oidc.md)
for the full reasoning and the exact change to reverse it.

### Restricting which accounts in your directory can sign in

Configuring `issuerUrl` already restricts login to your directory. These
optional fields narrow it further, which is worth doing if your IdP also
serves other people or other applications — an Authentik account created
for a housemate to reach Jellyfin has no business reading firewall logs.

All of them default to empty, which permits anyone the issuer vouches
for. Each one you set adds a condition, and **all** set conditions must
hold.

- **`allowedGroups`** — permit an account carrying at least one of these
  in its groups claim. Your provider must actually be configured to
  release that claim; if it isn't, **every login is refused**, not
  permitted. That direction is deliberate — an allowlist that opens up
  when a claim goes missing isn't one.
- **`groupsClaim`** — which claim holds the groups. Defaults to
  `groups` (Authentik, Keycloak, most Okta setups); Azure commonly uses
  `roles`.
- **`allowedEmails`** — exact addresses, compared case-insensitively.
- **`allowedEmailDomains`** — permit any address at these domains.
  Compared as whole domains, not string suffixes, so listing
  `example.com` does not admit `attacker@notexample.com`. Subdomains are
  not implied — list `mail.example.com` separately if you want it.
- **`requiredClaims`** — the general mechanism the three fields above are
  conveniences over: a map of claim name to permitted values, where the
  account must carry at least one permitted value for every claim named.

Both email fields additionally require the provider to have marked the
address `email_verified`. At a provider that lets users set their own
unverified address, an email allowlist without that check is decorative.

Restrictions are re-evaluated on **every** sign-in, before any account is
created, so removing someone from a group at your IdP locks them out at
their next login rather than whenever their session happens to expire,
and a refused account never gets provisioned as a side effect of being
refused. A refused user is told plainly that they aren't permitted, but
never *which* condition they failed — that goes to the server log, since
the specifics would map out your allowlist for an outsider.

```yaml
# Scope mikroview to one Authentik group
oidc:
  issuerUrl: "https://authentik.example.com/application/o/mikroview/"
  clientId: "mikroview"
  clientSecret: "changeme"
  publicBaseUrl: "https://mikroview.example.com"
  allowedGroups: ["mikroview-admins"]
```

**Identity**: an account is matched by the immutable `(issuer, subject)`
pair from the verified ID token, never by email or username -- an
identity provider reassigning someone's email must never silently
inherit a different mikroview account. A first-ever login via SSO
just-in-time creates a local account (no pre-registration step), using
the ID token's `preferred_username`/`email` claim as a display name
only if it's free; otherwise a stable synthetic username is generated.
The very first account overall (local or SSO, whichever happens first)
becomes admin; every account after that is a regular user.

**Security**: only asymmetric-signed ID tokens are ever accepted
(RS256/ES256/PS256) -- HS256 and `none` are rejected outright,
regardless of what the provider's discovery document claims to
support. The Authorization Code flow always uses PKCE. A misconfigured
or unreachable provider degrades to "SSO unavailable" (logged, once,
at startup) rather than affecting local login in any way.

**Authentik setup walkthrough**:

1. **Applications → Providers → Create**, type **OAuth2/OpenID
   Provider**.
   - **Client type**: Confidential.
   - **Redirect URIs**: strict match, `https://<publicBaseUrl>/api/auth/oidc/callback`.
   - **Signing Key**: pick a certificate (Authentik ships a self-signed
     one out of the box) -- **do not leave this unset**. Authentik's
     `id_token_signing_alg_values_supported` in its discovery document
     depends entirely on this being assigned; without it, token signing
     may not use an algorithm mikroview's allowlist accepts.
   - Note the generated **Client ID**/**Client Secret**.
2. **Applications → Applications → Create**, bind it to the provider
   above. Its slug is what appears in the issuer URL
   (`/application/o/<slug>/`).
3. Under the provider's **Property mappings**, make sure the standard
   `openid`, `profile`, and `email` scope mappings are attached (they
   usually are by default) -- these are what put `email`/
   `preferred_username` in the ID token for mikroview's username hint.
4. Set mikroview's `oidc.issuerUrl` to
   `https://<your-authentik-host>/application/o/<slug>/` (the exact
   value shown as **OpenID Configuration Issuer** on the provider's
   overview page -- confirm by fetching
   `<issuerUrl>/.well-known/openid-configuration` and checking
   `id_token_signing_alg_values_supported` includes `RS256`).

Verified end-to-end against a real, freshly bootstrapped Authentik
instance (provider + application configured via Authentik's own API):
the full redirect → real Authentik login form → PKCE code exchange →
RS256 ID token verification → JIT account provisioning → mikroview
session flow, including a second login correctly reusing the same
account rather than creating a duplicate.

## TLS

Mikroview serves TLS by default on its main listener -- the
application itself is never served over plain HTTP. See
[SECURITY.md](../SECURITY.md#tls) for the full reasoning; this section
is the configuration reference.

**Typing the address into a browser works.** A browser given
`mikroview-host:8080` tries `http://` first, which arrives as plaintext
on the HTTPS listener. Mikroview answers that with a redirect to
`https://` on the same host and port, rather than the bare error a TLS
server would normally return -- so one published port is all a
deployment needs (issue #325). The redirect target is validated against
`tls.hosts` the same way the port-80 listener below is, so an arbitrary
`Host` header is never echoed back.

A second listener, `listen.httpRedirect` (default `:8081`, mapped to
host port 80 by `deploy/docker-compose.yml`), covers the other case: a
bare hostname with no port, which a browser sends to port 80 where
nothing above can see it. It never serves the application itself.
It's only started while `tls.enabled` is true (nothing to redirect to
otherwise), and only started at all if non-empty -- set it to `""` to
disable it, e.g. if your own reverse proxy already handles the
HTTP->HTTPS redirect. The redirect target is built by stripping any
port off the request's `Host` header and assuming HTTPS is reachable on
the browser-default 443, which holds for the default compose port
mapping (host `443` -> this listener); if you've remapped the HTTPS
port to something else externally, either disable this listener and
redirect at your reverse proxy instead, or accept that the `Location`
header will still point at `:443`. If `tls.hosts` is set, the `Host`
header is validated against it (falling back to the first configured
host on a mismatch) rather than echoed unconditionally -- only
relevant if something other than a real browser navigation reaches
this listener directly.

A third listener, `listen.syslogTls` (default `:6514`, RFC 5425's
syslog-over-TLS port), accepts RouterOS's `remote-protocol=tls` logging
action -- mikroview's only syslog listener. Confidentiality for log
traffic on the wire, and mikroview authenticating itself to the router
with a certificate: the same one the main HTTPS listener presents when
`tls.enabled` is true (the router already imports mikroview's
generated CA to verify HTTPS ingest, so this is that same trust step,
not a second one), or a self-generated one on its own if `tls.enabled`
is false -- unlike `httpRedirect`, this listener is started whenever it's
non-empty regardless of `tls.enabled`, since the router connects to it
directly and needs a certificate to trust either way. Set it to `""`
to disable syslog ingest entirely.

This listener does **not** authenticate the sender: RouterOS's logging
action has no client-certificate option (only `check-certificate`,
verifying the router trusts mikroview, not the reverse), so anything
able to reach the port can still connect and inject log lines. Point
RouterOS at it with:

```
/system logging action set 0 target=remote remote=<mikroview-host> remote-port=6514 remote-protocol=tls
```

and import mikroview's CA (`GET /ca.crt`) under
`/certificate import` first, or the router will refuse the connection
with `SSL: ssl: no trusted CA certificate found`.

```yaml
tls:
  enabled: true
  # Your own cert (a real domain + ACME, a corporate CA, etc) -- takes
  # priority over the self-generated one below if both are set.
  certFile: ""
  keyFile: ""
  # Hostnames/IPs a generated certificate should cover (SANs) --
  # whatever you'll actually use to reach mikroview. Defaults to
  # localhost/127.0.0.1 if empty -- still fully encrypted either way,
  # just only strictly verifiable under those names unless you add your
  # own.
  hosts: []
  # Persists the self-generated CA + certificate across restarts, so
  # the trust step only happens once. Optional, same contract as
  # flags.storePath.
  storePath: "/var/lib/mikroview/tls"
```

- **`enabled`** — on by default. The one supported reason to set this
  `false` is a deployment where mikroview's listener is *provably* only
  reachable from your own reverse proxy over an isolated docker network
  -- never published to a LAN or the internet at all. In that specific
  topology the RP already owns TLS termination for real clients, and
  there's no bypass surface for mikroview to additionally protect on
  that internal hop. Never set this `false` if mikroview's port is
  reachable from a LAN or the internet in any other way -- doing so
  serves the app, credentials included, in cleartext. Logged clearly at
  startup whenever it's off, so it's never a silent state. Syslog
  ingest is unaffected either way: `listen.syslogTls` loads its own
  certificate independently of this setting, since RouterOS connects to
  it directly rather than through your reverse proxy.
- **`certFile`/`keyFile`** — your own certificate. Skips local-CA
  generation entirely when both are set. See "Renewing your own
  certificate" below if something renews it for you.
- **`hosts`** — SANs for a self-generated certificate. Left empty, the
  generated cert only covers `localhost`/`127.0.0.1` -- connections from
  any other name/IP are still fully encrypted, just not strictly
  verifiable against that name without adding it here.
- **`storePath`** — where a generated CA + certificate persist across
  restarts. Left unset, TLS still works, it just regenerates (and needs
  re-trusting) every restart -- the same optional-persistence contract
  `flags.storePath` has.

### Renewing your own certificate

Send MikroView `SIGHUP` after the new files are in place and it picks
them up — on the HTTPS listener **and** the syslog listener — without a
restart:

```sh
docker kill --signal=HUP mikroview
```

Certbot and cert-manager both have a hook for exactly this, so nothing
needs scheduling separately:

```sh
certbot renew --deploy-hook 'docker kill --signal=HUP mikroview'
```

Without it MikroView keeps serving the certificate it loaded at startup.
Renewal is automatic and a restart is not, so the failure turns up
silently, weeks later, as an expired certificate — and a router
configured with `check-certificate=yes` stops sending its logs at that
point, which is the outage you would least want to discover late.

MikroView does not watch the files and renew on its own, deliberately:
it cannot tell a finished renewal from one still being written, and half
a certificate is worse than an old one. The signal is you (or your
renewal tool) saying the new files are complete.

**A failed reload changes nothing.** If the files are unreadable or
half-written, MikroView logs an error and carries on with the
certificate it already has, rather than dropping to none — you sent the
signal expecting an improvement, and an outage is not one.

**Zero-config default**: with no `certFile`/`keyFile`, mikroview
generates its own local certificate authority and a leaf certificate on
first start (`internal/servertls`). This CA is trust-on-first-use, not a
globally trusted root -- your browser will show an untrusted-certificate
warning on first visit until you import it, the same one-time step
Proxmox/TrueNAS/pfSense's own self-signed admin UIs already ask for. The
CA is served at `GET /ca.crt`, unauthenticated (only when one was
actually generated), specifically so a browser or reverse proxy can
fetch it to establish trust; its fingerprint is also logged at startup
if you'd rather verify it out-of-band than trust-on-first-use blindly.

**One thing the click-through does not cover.** Clicking past the
warning gets you a fully working MikroView, with one exception:
installing it as an app, and its offline behaviour, both need a service
worker, and browsers refuse to register one over a certificate outside
their trust store — click-through or not. So until you import the CA
above, "Install app" either does not appear or does nothing, and the
browser console shows `Failed to register a ServiceWorker ... An SSL
certificate error occurred`. Nothing else is affected, and importing the
CA (or supplying your own certificate via `tls.certFile`/`tls.keyFile`)
resolves it. Found by the containerised end-to-end harness (#273), which
runs against TLS as shipped rather than plain HTTP on loopback.

**Reverse proxy in front, with your own single ingress**: point your
RP's *upstream/backend* target at `https://mikroview:PORT` instead of
`http://mikroview:PORT` -- same host, same port, no new port opened
anywhere. Your RP's client-facing side is completely untouched. Every
mainstream reverse proxy supports backend/upstream TLS (Caddy's
`reverse_proxy https://...`, Traefik's backend TLS transport, nginx's
`proxy_pass https://...`), typically via either skipping strict
verification for that specific upstream (reasonable here, since you
configured that upstream address yourself) or trusting mikroview's
local CA explicitly (more correct, and what `/ca.crt` is for).

## Environment variables

Override individual scalar settings without a mounted file:

| Variable | Overrides |
|---|---|
| `MIKROVIEW_CONFIG` | path to the YAML config file to load |
| `MIKROVIEW_LISTEN_SYSLOG_TLS` | `listen.syslogTls` (see [TLS](#tls)) |
| `MIKROVIEW_LISTEN_HTTP` | `listen.http` |
| `MIKROVIEW_LISTEN_HTTP_REDIRECT` | `listen.httpRedirect` |
| `MIKROVIEW_TRUSTED_PROXIES` | `listen.trustedProxies` (comma-separated; see [Running behind a reverse proxy](#running-behind-a-reverse-proxy)) |
| `MIKROVIEW_CLIENT_IP_HEADER` | `listen.clientIpHeader` |
| `MIKROVIEW_STORE_RETENTION` | `store.retention` |
| `MIKROVIEW_STORE_MAX_MEMORY` | `store.maxMemory` |
| `MIKROVIEW_LOG_LEVEL` | `log.level` (see [Logging](#logging)) |
| `MIKROVIEW_GEOIP_DB_PATH` | `geoip.dbPath` (see [GeoIP country flags](#geoip-country-flags-optional)) |
| `MIKROVIEW_ABUSEIPDB_KEY` | `reputation.abuseIPDBKey` (see [IP reputation lookup](#ip-reputation-lookup-optional)) |
| `MIKROVIEW_FLAGS_STORE_PATH` | `flags.storePath` |
| `MIKROVIEW_FLAGS_PORT_SCAN_THRESHOLD` | `flags.portScanThreshold` |
| `MIKROVIEW_FLAGS_PORT_SCAN_WINDOW` | `flags.portScanWindow` |
| `MIKROVIEW_FLAGS_ACTIVITY_SPIKE_THRESHOLD` | `flags.activitySpikeThreshold` |
| `MIKROVIEW_FLAGS_ACTIVITY_SPIKE_WINDOW` | `flags.activitySpikeWindow` |
| `MIKROVIEW_FLAGS_CRITICAL_PORTS` | `flags.criticalPorts` (comma-separated, e.g. `22,3389,8291`) |
| `MIKROVIEW_FLAGS_CRITICAL_PORT_THRESHOLD` | `flags.criticalPortThreshold` |
| `MIKROVIEW_FLAGS_CRITICAL_PORT_WINDOW` | `flags.criticalPortWindow` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_MULTIPLIER` | `flags.globalSpikeMultiplier` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_MIN_EPS` | `flags.globalSpikeMinEPS` |
| `MIKROVIEW_FLAGS_GLOBAL_SPIKE_WARMUP_SAMPLES` | `flags.globalSpikeWarmupSamples` |
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_THRESHOLD` | `flags.distributedBruteForceThreshold` |
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_WINDOW` | `flags.distributedBruteForceWindow` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_THRESHOLD` | `flags.outboundAnomalyThreshold` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_WINDOW` | `flags.outboundAnomalyWindow` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_THRESHOLD` | `flags.internalReconThreshold` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_WINDOW` | `flags.internalReconWindow` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MULTIPLIER` | `flags.ruleSpikeMultiplier` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MIN_RATE` | `flags.ruleSpikeMinRate` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_WINDOW` | `flags.ruleSpikeWindow` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_WARMUP_SAMPLES` | `flags.ruleSpikeWarmupSamples` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_THRESHOLD` | `flags.repeatedDropsThreshold` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_WINDOW` | `flags.repeatedDropsWindow` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_MULTIPLIER` | `flags.hostActivityMultiplier` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_WARMUP_SAMPLES` | `flags.hostActivityWarmupSamples` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_WINDOW` | `flags.lowSlowScanWindow` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_PORT_THRESHOLD` | `flags.lowSlowScanPortThreshold` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_HOST_THRESHOLD` | `flags.lowSlowScanHostThreshold` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_MIN_OBSERVATION` | `flags.lowSlowScanMinObservation` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_DROP_RATIO` | `flags.lowSlowScanDropRatio` |
| `MIKROVIEW_FLAGS_LOW_SLOW_SCAN_BASELINE_MULTIPLIER` | `flags.lowSlowScanBaselineMultiplier` |
| `MIKROVIEW_FLAGS_OFF_HOURS_START_HOUR` | `flags.offHoursStartHour` |
| `MIKROVIEW_FLAGS_OFF_HOURS_END_HOUR` | `flags.offHoursEndHour` |
| `MIKROVIEW_FLAGS_OFF_HOURS_MIN_SAMPLE_DAYS` | `flags.offHoursMinSampleDays` |
| `MIKROVIEW_FLAGS_OFF_HOURS_MIN_COUNT` | `flags.offHoursMinCount` |
| `MIKROVIEW_FLAGS_DEVICE_STALE_AFTER` | `flags.deviceStaleAfter` |
| `MIKROVIEW_FLAGS_RULE_USAGE_STORE_PATH` | `flags.ruleUsageStorePath` |
| `MIKROVIEW_FLAGS_STALE_RULE_DAYS` | `flags.staleRuleDays` |
| `MIKROVIEW_FLAGS_STALE_RULE_CHECK_INTERVAL` | `flags.staleRuleCheckInterval` |
| `MIKROVIEW_FLAGS_VPN_INTERFACES` | `flags.vpnInterfaces` (comma-separated, e.g. `wireguard1,wireguard2`) |
| `MIKROVIEW_FLAGS_VPN_CONFIDENCE_MULTIPLIER` | `flags.vpnConfidenceMultiplier` |
| `MIKROVIEW_FLAGS_DETECTOR_SETTINGS_STORE_PATH` | `flags.detectorSettingsStorePath` (see [Per-detector toggles](#per-detector-toggles-and-scope-restrictions-optional)) |
| `MIKROVIEW_AUTH_STORE_PATH` | `auth.storePath` (see [Authentication](#authentication)) |
| `MIKROVIEW_AUTH_SECURE_COOKIE` | `auth.secureCookie` |
| `MIKROVIEW_AUTH_SESSION_TTL` | `auth.sessionTTL` |
| `MIKROVIEW_ENTITIES_STORE_PATH` | `entities.storePath` (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)) |
| `MIKROVIEW_AUDIT_STORE_PATH` | `audit.storePath` (see [Audit log](#audit-log-admin-action-accountability-optional)) |
| `MIKROVIEW_WATCHLIST_STORE_PATH` | `watchlist.storePath` (see [Watchlist](#watchlist-optional)) |
| `MIKROVIEW_WATCHLIST_MATCH_LOG_PATH` | `watchlist.matchLogPath` |
| `MIKROVIEW_WATCHLIST_MATCH_LOG_CAPACITY` | `watchlist.matchLogCapacity` |
| `MIKROVIEW_WATCHLIST_MATCH_LOG_RETENTION` | `watchlist.matchLogRetention` |
| `MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH` | `watchlist.suggestionsStorePath` (see [Suggested watchlist entries](#suggested-watchlist-entries-issue-243)) |
| `MIKROVIEW_AUTH_TOKENS_STORE_PATH` | `auth.tokensStorePath` (see [API tokens](#api-tokens-read-only)) |
| `MIKROVIEW_TLS_ENABLED` | `tls.enabled` (see [TLS](#tls)) |
| `MIKROVIEW_TLS_CERT_FILE` | `tls.certFile` |
| `MIKROVIEW_TLS_KEY_FILE` | `tls.keyFile` |
| `MIKROVIEW_TLS_HOSTS` | `tls.hosts` (comma-separated) |
| `MIKROVIEW_TLS_STORE_PATH` | `tls.storePath` |
| `MIKROVIEW_OIDC_ISSUER_URL` | `oidc.issuerUrl` (see [Single sign-on](#single-sign-on-oidcsso)) |
| `MIKROVIEW_OIDC_CLIENT_ID` | `oidc.clientId` |
| `MIKROVIEW_OIDC_CLIENT_SECRET` | `oidc.clientSecret` |
| `MIKROVIEW_OIDC_PUBLIC_BASE_URL` | `oidc.publicBaseUrl` |
| `MIKROVIEW_OIDC_SCOPES` | `oidc.scopes` (comma-separated) |
| `MIKROVIEW_AUTH_SESSION_MAX_LIFETIME` | `auth.sessionMaxLifetime` |
| `MIKROVIEW_OIDC_ALLOWED_GROUPS` | `oidc.allowedGroups` (comma-separated) |
| `MIKROVIEW_OIDC_GROUPS_CLAIM` | `oidc.groupsClaim` |
| `MIKROVIEW_OIDC_ALLOWED_EMAILS` | `oidc.allowedEmails` (comma-separated) |
| `MIKROVIEW_OIDC_ALLOWED_EMAIL_DOMAINS` | `oidc.allowedEmailDomains` (comma-separated) |
| `MIKROVIEW_RECOVERY_PEPPER_FILE` | `auth.recoveryPepperPath` -- for keeping the pepper off the data volume entirely, so a stolen copy of that volume does not carry the one value the recovery-key hashes are useless without |
| `MIKROVIEW_NOTIFY_BATCH_WINDOW` | `notify.batchWindow` (see [Notifications](#notifications-optional)) |
| `MIKROVIEW_NOTIFY_SMTP_HOST` | `notify.smtp.host` |
| `MIKROVIEW_NOTIFY_SMTP_PORT` | `notify.smtp.port` |
| `MIKROVIEW_NOTIFY_SMTP_USERNAME` | `notify.smtp.username` |
| `MIKROVIEW_NOTIFY_SMTP_PASSWORD` | `notify.smtp.password` |
| `MIKROVIEW_NOTIFY_SMTP_TLS_MODE` | `notify.smtp.tlsMode` |
| `MIKROVIEW_NOTIFY_SMTP_FROM` | `notify.smtp.from` |
| `MIKROVIEW_NOTIFY_SMTP_TO` | `notify.smtp.to` (comma-separated) |
| `MIKROVIEW_NOTIFY_PUSHOVER_TOKEN` | `notify.pushover.token` |
| `MIKROVIEW_NOTIFY_PUSHOVER_USER` | `notify.pushover.user` |
| `MIKROVIEW_DEVICE_MAC_STORE_PATH` | `deviceMac.storePath` (see [New-device detection](#new-device-detection-optional-on-by-default)) |
| `MIKROVIEW_NOTIFY_WEBHOOK_URL` | `notify.webhook.url` |
| `MIKROVIEW_BLOCKLIST_SOURCES` | `blocklist.sources` (comma-separated, see [Local IP/CIDR blocklist matching](#local-ipcidr-blocklist-matching-optional-on-by-default)) -- note an empty env var value is treated as unset, same as every other list env var here, so *disabling* the feature (`sources: []`) needs the YAML file, not this variable |
| `MIKROVIEW_ENGINE_STORE_PATH` | `engine.storePath` -- where `internal/engine`'s persisted per-definition baseline state lives. Nothing registers a definition against it yet, so this only matters once one does |

## Checking your version

What you see depends on which image you are running, because there are
two kinds (see `.github/workflows/docker.yml`; pushing to `dev`
deliberately publishes no image at all):

- **A release build** -- `latest`, or an explicit `v0.1.0`-style tag --
  reports that semantic version, e.g. `v0.1.0`. The version comes from
  the `VERSION` file in the repository, and merging to `main` with a
  value that has no tag yet is what cuts the release.
- **A preview build** -- `preview`, or `preview-<sha>` -- reports the
  short git commit SHA it was built from.

Either way it is checkable three ways, all showing the same value:

- `docker exec <container> mikroview -version` -- prints the bare SHA
  and nothing else, easy to capture in a script.
- `GET /api/healthz`'s `version` field -- reachable with no auth and no
  session regardless of deployment state, same as the rest of that
  endpoint.
- The account menu in the UI (bottom, below Sign out/appearance) --
  small, muted text, always visible regardless of auth state.

A plain local `go run .`/`go build .` (no `-ldflags`) shows `dev`
instead of a commit SHA.

## Postgres (optional)

By default MikroView keeps its accounts, flags and settings in JSON
files next to itself. You can move that state into Postgres instead.

**There is exactly one reason to do this: getting your data off the
machine MikroView runs on.** If someone compromises that host, the
accounts file goes with it. A database on a *separate* machine, reachable
only over a restricted network path, means they'd also have to reach and
break into a second system.

> **A Postgres on the same host — including a container next to
> MikroView — gives you none of that.** The connection password sits
> right there beside the data it protects, inside the same compromise.
> It's worse than the JSON files: same exposure, more to go wrong. This
> is why `deploy/docker-compose.yml` ships no Postgres service, not even
> commented out.

### Setting it up

Put the connection string in its own file. Not in `config.yaml`, and
there's no command-line flag for it — it contains a password, and
passwords in config files end up in backups, while passwords in
command-line arguments are visible to every process on the box and to
`docker inspect`.

```sh
umask 077
echo 'postgres://mikroview:PASSWORD@db.internal:5432/mikroview?sslmode=verify-full' \
  > /etc/mikroview/postgres-dsn
```

Then point MikroView at it:

```yaml
postgres:
  # Path to the file above. Empty (the default) means "use JSON files".
  dsnFile: /etc/mikroview/postgres-dsn
```

or `MIKROVIEW_POSTGRES_DSN_FILE=/etc/mikroview/postgres-dsn`.

**The file has to be readable by the container's user.** MikroView runs
as uid `65532` (distroless `nonroot`), which is not the user that created
the file on your host — so a `0600` file, which is the natural mode for
something holding a password, is unreadable inside the container and
MikroView refuses to start:

```
ERROR storage │ postgres: reading DSN file /etc/mikroview/postgres-dsn: permission denied
```

Keep the tight mode and hand it to that user:

```sh
chown 65532:65532 postgres-dsn
chmod 600 postgres-dsn
```

(Found by the containerised end-to-end harness in #273 — following these
instructions exactly, with the obvious `chmod 600`, produced a MikroView
that would not start.)

The database user needs permission to create tables in its own database
the first time it starts; MikroView creates what it needs and records
what it has done, so restarts are fine.

### The connection must be encrypted

`sslmode` has to be `require`, `verify-ca` or `verify-full`. Anything
weaker is **refused at startup** rather than quietly upgraded — if the
setting doesn't do what you asked, you should hear about it.

Use `verify-full` if you can. `require` encrypts the connection but
doesn't check *who* answered, so it doesn't stop someone who can
intercept traffic between MikroView and the database.

### Which schema the tables go in

`public`, unless you say otherwise. To keep MikroView's tables in a
schema of their own, create it and name it in the connection string:

```
postgres://mikroview:PASSWORD@db.internal:5432/mikroview?sslmode=verify-full&search_path=mikroview
```

MikroView always sets this on its connections rather than inheriting
whatever the role or the database defaults to. That is deliberate:
MikroView names its tables without a schema prefix, so if the default
search path were used, anyone able to create a table in a schema listed
ahead of yours could put their own `store_blob` or `match_log` there,
and MikroView would read and write theirs while reporting success.

### Your existing data moves automatically

On the first start with `dsnFile` set, MikroView copies each existing
JSON file into the database and says so:

```
storage │ migrated /var/lib/mikroview/users.json (362 bytes) into
          postgres db.internal/mikroview store "auth" -- that file is no
          longer read, and can be deleted once you are satisfied the move
          worked
```

Three things worth knowing:

- **This is a one-way door.** Once MikroView has started with Postgres
  configured, it records that, and removing `dsnFile` later will *not*
  bring you back on the JSON files — it refuses to start, and tells you
  why and where the marker is.

  That refusal is deliberate. Your JSON files stopped being current the
  moment you migrated, so quietly falling back to them would serve stale
  accounts — possibly a stale admin, possibly a password you had already
  changed — and nothing would look wrong. Decide before you migrate: use
  Postgres, or don't. Staying on the JSON files is always the reversible
  choice.

- **The old files are left alone.** Nothing is deleted or renamed — they
  simply stop being read. Delete them once you're satisfied the move
  worked.

- **Back up the database from then on.** The JSON files are frozen at
  the moment of migration, so backing those up protects nothing. This is
  the expectation that comes with choosing Postgres.

- **The JSON-file account commands stop working.** They would be editing
  files nothing reads. They refuse, and say so.
- **It only ever copies into an empty store.** Once the database holds
  data, the old file is ignored permanently. A stale file left on disk
  can't roll live data back on a later restart.
- **If the database is configured but unreachable, MikroView refuses to
  start.** It does not silently fall back to the JSON files — that would
  quietly run your deployment on stale local accounts, possibly with a
  different admin, and nothing would look wrong.
- **One exception: the watchlist match log is not migrated.** Every
  other store's JSON document round-trips byte-identically into
  Postgres; the match log's append-only line format doesn't fit that
  path, and it starts empty on Postgres instead — said in the startup
  log, not left to be discovered as missing history. The old
  `matchlog.jsonl` is untouched and still readable if you revert
  `postgres.dsnFile`. See [Watchlist](#watchlist-optional).

### Watching for schema changes on upgrade

A new MikroView image can bring a database schema change with it. That
happens automatically on start, and it is always announced:

```
schema │ database schema is up to date at version 3 (postgres db.internal/mikroview)
```

When an upgrade does change something, each step is named before it runs
and again once it finishes, so a migration that stalls or crashes is
identifiable from the last line written:

```
schema │ database schema is at version 3, 1 migration(s) to apply -- updating postgres db.internal/mikroview
schema │ applying 0004_add_widget.sql (version 4)
schema │ applied 0004_add_widget.sql in 11ms
schema │ database schema updated from version 3 to version 4 -- an older mikroview image may
         no longer read this database correctly
```

That last line is a warning for a reason: **rolling back to an older
image after a schema change is not guaranteed to work.** Take a database
backup before upgrading if that matters to you.

If a migration fails, MikroView refuses to start and the database is
left exactly as it was — each migration runs in its own transaction, so
there is no half-applied state to clean up:

```
schema  │ migration 0004_add_widget.sql (version 4) failed and was rolled back --
          the schema is unchanged, still at version 3
storage │ postgres: applying schema: ... ERROR: syntax error at or near "..."
```

### What isn't stored there

The live event stream stays in memory and is never persisted, on any
backend. TLS certificates also stay on disk — MikroView needs them
before it could reach a database at all.

## CLI flags (local development)

`-version`, `-syslog-tls`, `-http`,
`-http-redirect`, `-retention`, `-max-memory`, `-geoip-db` — see
`go run . -h`. Devices,
rule/host names, and auth config can only be set via YAML/env, not
flags.

`-healthcheck`, `-recover-admin-account`, `-transfer-admin <username>`
and `-generate-recovery-keys` are standalone modes -- each does its one job and
exits, rather than starting the server. See
[Authentication](#authentication) for all but the first.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/healthz` | liveness/uptime/version check |
| `GET /ca.crt` | mikroview's self-generated CA certificate, unauthenticated -- only present when TLS is on and mikroview generated its own CA (never for a supplied cert or `tls.enabled: false`); see [TLS](#tls) |
| `GET /api/events` | filtered, windowed historical query (see below) |
| `GET /api/devices` | known devices (configured + auto-discovered), each with a `status` of `live`/`stale`/`never_seen` (issue #98, see [Behavioral flags](#behavioral-flags-optional-on-by-default)'s "Device silence" entry) -- feeds the Fleet view |
| `GET /api/rules` | every rule label mikroview has ever seen fire, with first/last-seen time and count (`internal/rules.Store`) -- the "discovered but unnamed rules" source for the Entities panel (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)), open to any signed-in user, not admin-gated |
| `GET /api/stats` | totals, per-action counts, rolling events/sec |
| `GET /api/ws` | live-tail WebSocket feed |
| `GET /api/lookup/ip/{ip}` | on-demand reputation/threat-intel lookup for one public IP (see [IP reputation lookup](#ip-reputation-lookup-optional)) |
| `GET /api/flags` | active + cleared behavioral flags, plus the last hour of newly-raised-episode counts by type at 1-minute resolution (issue #100, feeds the dashboard's flags-over-time chart) (see [Behavioral flags](#behavioral-flags-optional-on-by-default)) |
| `POST /api/flags/{id}/clear` | mark one flag as cleared |
| `POST /api/flags/clear-all` | clear every currently-active flag in one request -- regular clears only, never creates an exclusion. Audit-logged once per call |
| `POST /api/flags/{id}/clear-permanent` | admin-only: clear one flag *and* permanently exclude its (detector, target) pair going forward. Audit-logged |
| `GET /api/flags/exclusions` | admin-only: every currently-excluded (detector, target) pair |
| `DELETE /api/flags/exclusions/{id}` | admin-only: remove one exclusion, letting that pair raise again |
| `GET /api/detectors` | admin-only: every detector's live enabled+scope (see [Per-detector toggles](#per-detector-toggles-and-scope-restrictions-optional)) |
| `PUT /api/detectors/{name}` | admin-only: replace one detector's enabled+scope wholesale |
| `GET /api/entities` | admin-only (see [Entities](#entities-ui-managed-hostruleport-labels-and-tags-optional)): every persisted entity |
| `POST /api/entities` | admin-only: create or replace (upsert) one entity, identified by `(type, key)` in the JSON body |
| `DELETE /api/entities` | admin-only: remove the entity identified by `(type, key)` in the JSON body |
| `GET /api/audit` | admin-only: a windowed slice of the admin action audit log (see [Audit log](#audit-log-admin-action-accountability-optional)), newest activity last, accepting `since`/`until`/`limit` query params like `GET /api/events` |
| `GET /api/watchlist/entries` | admin-only: every watchlist entry (see [Watchlist](#watchlist-optional)) |
| `POST /api/watchlist/entries` | admin-only: create one entry |
| `PUT /api/watchlist/entries/{id}` | admin-only: replace one entry's name/source/destination/ports/invert/includeStructuralNoise -- never its Permitted/Observed state, which only the two endpoints below can change |
| `DELETE /api/watchlist/entries/{id}` | admin-only: remove one entry |
| `POST /api/watchlist/entries/{id}/promote` | admin-only: move one or more observed destinations into that entry's Permitted set |
| `POST /api/watchlist/entries/{id}/observing` | admin-only: turn an inverted entry's observe mode on or off |
| `GET /api/watchlist/matches` | a windowed query over the persisted match log, by `mac`/`ip`/`since`/`until`/`limit` -- open to any signed-in user and reachable via a read-only API token, same tier as `/api/events`/`/api/flags`/`/api/stats`/`/api/devices` |
| `GET /api/suggestions` | admin-only: every suggested watchlist entry (see [Suggested watchlist entries](#suggested-watchlist-entries-issue-243)), optionally filtered with `?status=off\|on\|hide` |
| `POST /api/suggestions/{id}/accept` | admin-only: accept an undecided suggestion, creating a real watchlist entry |
| `POST /api/suggestions/{id}/hide` | admin-only: decline an undecided suggestion |
| `POST /api/suggestions/{id}/unhide` | admin-only: return a hidden suggestion to undecided |
| `POST /api/suggestions/reset` | admin-only, destructive: wipes the entire watchlist and regenerates suggestions from scratch -- requires `{"confirm": true}` in the request body |
| `GET /api/auth/session` | current auth state (setup-required / authenticated / not) -- always 200, never gated |
| `POST /api/auth/register` | create the first (admin) account -- only while zero accounts exist |
| `POST /api/auth/login` | sign in, sets the session cookie |
| `POST /api/auth/logout` | sign out, clears the session cookie |
| `GET /api/auth/users` | admin-only: list accounts |
| `POST /api/auth/users` | admin-only: create an additional account |
| `DELETE /api/auth/users/{id}` | admin-only: remove an account |
| `POST /api/tokens` | admin-only: create a read-only API token (see [API tokens](#api-tokens-read-only)) -- returns the raw value once |
| `GET /api/tokens` | admin-only: list tokens (name/created/last-used, never the value or hash) |
| `DELETE /api/tokens/{id}` | admin-only: revoke a token |
| `GET /api/auth/oidc/login` | start the SSO flow -- a top-level browser redirect to the configured provider, only present when [OIDC](#single-sign-on-oidcsso) is configured |
| `GET /api/auth/oidc/callback` | the provider's redirect target completing the SSO flow -- see [Single sign-on](#single-sign-on-oidcsso) |
| `POST /api/auth/oidc/link` | connect the signed-in account to an SSO identity, so the same person can sign in either way -- see [Connecting your account to SSO](#single-sign-on-oidcsso) |
| `GET /api/config/problems` | admin-only: the same configuration warnings `-validate-config` reports, as the UI shows them -- see [Problem codes](#problem-codes) |

Every route above `/api/auth/session`/`/register`/`/login`/`/logout` and
`/api/healthz` requires a valid session once an account exists -- see
[Authentication](#authentication). `GET /api/events`, `/flags`, `/stats`,
and `/devices` additionally accept a valid `Authorization: Bearer
<token>` header instead of a session (see [API tokens](#api-tokens-read-only));
no other route accepts one.
Every mutating (`POST`/`PUT`/`DELETE`) request also requires an
`X-Requested-With: mikroview` header once an account exists (a CSRF
mitigation, see SECURITY.md).

`/api/events` query parameters: `device`, `action` (`accept`/`drop`/
`reject`/`log`/`unknown`), `protocol`, `chain`, `interface`, `ip` (exact
or CIDR, matches source or destination), `port` (matches source or
destination), `srcScope`/`dstScope` (`internal` or `external`, restricts
that side of the connection to a private/LAN or public address
respectively -- an address that can't be parsed satisfies neither),
`rule` (substring match), `since` (RFC3339), `until` (RFC3339, an
optional upper bound -- paired with `since` this gives a bounded
before/after window instead of an open-ended tail to now), `sinceId`
(cursor), `limit` (default 500, max 5000).

**Bounded before/after lookback (issue #29).** `around` (RFC3339) +
`window` (a duration, e.g. `5m`, default `5m`) is sugar for `since`/
`until` centered on a timestamp -- overrides them if both forms are
given. Meant for pulling "what was this IP doing right before/after X"
from an external signal (a honeypot hit, a manual investigation
trigger), combined with `ip`:

```
GET /api/events?ip=203.0.113.9&around=2026-01-15T14:32:00Z&window=10m
```

"Before" context is inherently capped by the in-memory retention window
(`store.retention`) -- older history simply doesn't exist. Rather than
silently truncating, the response's `windowStart` field always reports
the *actual* applied lower bound, so a caller comparing it against the
`since` (or `around`-`window`) it requested can tell whether it got
truncated by retention.

## Live updates: server vs. client filtering

The initial page load and any "load older" request go through
`GET /api/events`, filtered **server-side** against the full retained
buffer (up to `store.maxMemory` / `store.retention`) — this keeps the
browser from ever having to download more than it needs.

The WebSocket at `/api/ws`, by contrast, pushes **every** new event to
every connected client, unfiltered, batched into one frame every ~50ms.
The frontend applies the active filters client-side against its own
capped in-memory buffer. This is deliberate: it means changing a filter
in the UI is instant, with no round-trip to the server, and it keeps the
server-side WebSocket handling simple (one fan-out list, no per-connection
filter state to maintain).
