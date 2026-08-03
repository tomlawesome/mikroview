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

`/api/events` query parameters: `device`, `action` (`accept`/`drop`/
`reject`/`log`/`unknown`), `protocol`, `chain`, `interface`, `ip` (exact
or CIDR, matches source or destination), `port` (matches source or
destination), `rule` (substring match), `since` (RFC3339), `sinceId`
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
