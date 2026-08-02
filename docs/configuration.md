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

## CLI flags (local development)

`-syslog-udp`, `-syslog-tcp`, `-http`, `-retention`, `-max-events` — see
`go run . -h`. Devices can only be configured via YAML, not flags.

## API reference

| Endpoint | Description |
|---|---|
| `GET /api/healthz` | liveness/uptime check |
| `GET /api/events` | filtered, windowed historical query (see below) |
| `GET /api/devices` | known devices (configured + auto-discovered) |
| `GET /api/stats` | totals, per-action counts, rolling events/sec |
| `GET /api/ws` | live-tail WebSocket feed |

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
