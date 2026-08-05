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
  syslogUdp: ":1514"
  syslogTcp: ":1514"
  http: ":8080"

store:
  retention: 24h
  maxEvents: 200000

devices:
  - id: core-router
    name: "Core Router"
    sourceIp: 192.168.1.1
```

- `store.retention` — how far back `/api/events` and the UI will show,
  as a Go duration string (`24h`, `12h30m`, ...).
- `store.maxEvents` — the hard cap on events held in memory at once (a
  fixed-size ring buffer); this bounds memory use regardless of traffic
  volume. Once full, the oldest events are overwritten first.
- `devices` — maps a syslog source IP to a friendly name. Routers
  sending logs from an IP *not* listed here still appear in the UI and
  `/api/devices`, labelled by their raw IP with `configured: false`, so
  you can identify and add them rather than silently losing their events.

**File permissions**: the container runs as a fixed non-root user (uid
65532, distroless `nonroot`) that is unrelated to any user on the Docker
host. If `config.yaml` isn't world-readable (or owned by a matching
uid/gid), the container will fail to start with a permission error —
`chmod 644 deploy/config.yaml` after editing it.

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
IP in the live view queries a threat-intel source and shows the result in
a popover (open ports, hostnames, known CVEs, abuse score). This proxies
through the backend so no key ever reaches the browser, and caches each
IP briefly to conserve free-tier quota.

- **Shodan InternetDB** — free, keyless, always used, no configuration
  needed.
- **AbuseIPDB** — optional, needs an API key (free tier: 1000 lookups/
  day). Set `reputation.abuseIPDBKey` in `config.yaml` or the
  `MIKROVIEW_ABUSEIPDB_KEY` env var. Adds abuse score, report count,
  country, and ISP to the result.

Unconfigured, the feature still works with Shodan-only results; private/
loopback/link-local addresses are rejected server-side regardless of
configuration.

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
internal-recon, and outbound-anomaly detectors only count events whose
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
  distributedBruteForceThreshold: 10
  distributedBruteForceWindow: 5m
  outboundAnomalyThreshold: 25
  outboundAnomalyWindow: 5m
  internalReconThreshold: 10
  internalReconWindow: 60s
  ruleSpikeMultiplier: 5
  ruleSpikeMinRate: 0.2
  ruleSpikeWindow: 60s
  repeatedDropsThreshold: 10
  repeatedDropsWindow: 15m
  hostActivityMultiplier: 3
  hostActivityWarmupSamples: 20
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
  `globalSpikeMinEPS`.
- **Repeated drops on a port** — the same (source, destination port)
  pair getting dropped/rejected `repeatedDropsThreshold`+ times within
  `repeatedDropsWindow`, against a *locally-hosted* service (unlike
  critical-port attempts, not restricted to a curated port list or to
  external sources). Aimed at self-hosters: this is very often a
  misconfigured port-forward or firewall rule — the real client keeps
  retrying a port that isn't actually open the way you think — rather
  than necessarily an attack, so treat it as "worth a look," not
  "critical."

**Confidence score.** The activity-spike detector (currently the only
one making a statistical judgment call rather than a deterministic
threshold crossing) attaches a `confidence` percentage to each flag it
raises, shown in the UI as e.g. "73% confidence". It's deliberately not
a black-box number: it combines (1) how much history backs the host's
baseline and (2) how far the current reading deviates from it, and the
flag's own detail text spells out the actual baseline value, observed
value, and sample count behind the score — mikroview's job is to put
that information in front of you, not to make the call for you. Flags
from every other detector have no `confidence` field at all (`null` in
`GET /api/flags`) rather than an implied 100% — a plain threshold
crossing is exactly as trustworthy as the count it reports, so a
percentage there would be noise.

A flag is raised once per (detector, source) pair and updated in place
on re-firing (count/last-seen bumped, not duplicated) until a human
clears it via the UI or `POST /api/flags/{id}/clear`. Clearing an
already-active-again source re-raises it as a fresh entry rather than
silently resurrecting the old one.

## Environment variables

Override individual scalar settings without a mounted file:

| Variable | Overrides |
|---|---|
| `MIKROVIEW_CONFIG` | path to the YAML config file to load |
| `MIKROVIEW_LISTEN_SYSLOG_UDP` | `listen.syslogUdp` |
| `MIKROVIEW_LISTEN_SYSLOG_TCP` | `listen.syslogTcp` |
| `MIKROVIEW_LISTEN_HTTP` | `listen.http` |
| `MIKROVIEW_STORE_RETENTION` | `store.retention` |
| `MIKROVIEW_STORE_MAX_EVENTS` | `store.maxEvents` |
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
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_THRESHOLD` | `flags.distributedBruteForceThreshold` |
| `MIKROVIEW_FLAGS_DISTRIBUTED_BRUTE_FORCE_WINDOW` | `flags.distributedBruteForceWindow` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_THRESHOLD` | `flags.outboundAnomalyThreshold` |
| `MIKROVIEW_FLAGS_OUTBOUND_ANOMALY_WINDOW` | `flags.outboundAnomalyWindow` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_THRESHOLD` | `flags.internalReconThreshold` |
| `MIKROVIEW_FLAGS_INTERNAL_RECON_WINDOW` | `flags.internalReconWindow` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MULTIPLIER` | `flags.ruleSpikeMultiplier` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_MIN_RATE` | `flags.ruleSpikeMinRate` |
| `MIKROVIEW_FLAGS_RULE_SPIKE_WINDOW` | `flags.ruleSpikeWindow` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_THRESHOLD` | `flags.repeatedDropsThreshold` |
| `MIKROVIEW_FLAGS_REPEATED_DROPS_WINDOW` | `flags.repeatedDropsWindow` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_MULTIPLIER` | `flags.hostActivityMultiplier` |
| `MIKROVIEW_FLAGS_HOST_ACTIVITY_WARMUP_SAMPLES` | `flags.hostActivityWarmupSamples` |

## CLI flags (local development)

`-syslog-udp`, `-syslog-tcp`, `-http`, `-retention`, `-max-events`,
`-geoip-db` — see `go run . -h`. Devices can only be configured via YAML,
not flags.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/healthz` | liveness/uptime check |
| `GET /api/events` | filtered, windowed historical query (see below) |
| `GET /api/devices` | known devices (configured + auto-discovered) |
| `GET /api/stats` | totals, per-action counts, rolling events/sec |
| `GET /api/ws` | live-tail WebSocket feed |
| `GET /api/lookup/ip/{ip}` | on-demand reputation/threat-intel lookup for one public IP (see [IP reputation lookup](#ip-reputation-lookup-optional)) |
| `GET /api/flags` | active + cleared behavioral flags (see [Behavioral flags](#behavioral-flags-optional-on-by-default)) |
| `POST /api/flags/{id}/clear` | mark one flag as cleared |

`/api/events` query parameters: `device`, `action` (`accept`/`drop`/
`reject`/`log`/`unknown`), `protocol`, `chain`, `interface`, `ip` (exact
or CIDR, matches source or destination), `port` (matches source or
destination), `srcScope`/`dstScope` (`internal` or `external`, restricts
that side of the connection to a private/LAN or public address
respectively -- an address that can't be parsed satisfies neither),
`rule` (substring match), `since` (RFC3339), `sinceId`
(cursor), `limit` (default 500, max 5000).

## Live updates: server vs. client filtering

The initial page load and any "load older" request go through
`GET /api/events`, filtered **server-side** against the full retained
buffer (up to `store.maxEvents` / `store.retention`) — this keeps the
browser from ever having to download more than it needs.

The WebSocket at `/api/ws`, by contrast, pushes **every** new event to
every connected client, unfiltered, batched into one frame every ~50ms.
The frontend applies the active filters client-side against its own
capped in-memory buffer. This is deliberate: it means changing a filter
in the UI is instant, with no round-trip to the server, and it keeps the
server-side WebSocket handling simple (one fan-out list, no per-connection
filter state to maintain).
