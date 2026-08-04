# Security policy

MikroView is built for one specific deployment shape: a single instance,
on a trusted home/office LAN, with no authentication. That's a deliberate
scope decision, not an oversight — this document makes the resulting
threat model and its consequences explicit, since they aren't the kind of
thing that should be left implicit for anyone deciding whether/how to
deploy this.

## Threat model

**MikroView has no authentication and assumes network-level trust
instead.** Anyone who can reach its HTTP port can view all retained
firewall events (including source/destination IPs, ports, and the
"investigate this IP" reputation lookup) and, indirectly, everyone's
router configuration as reflected in `config.yaml`'s device list. Anyone
who can reach its syslog port can inject fabricated events — the syslog
listeners accept any well-formed line from any source and never
authenticate the sender as a real RouterOS device.

This is fine, and the intended use case, on a LAN you already trust every
device on. **It is not safe to expose MikroView's HTTP or syslog ports to
the internet, to an untrusted network, or to a network segment shared
with devices you don't control.**

## Data handling

- **No persistence.** Events live in an in-memory ring buffer only —
  there is no database and nothing is written to disk. Restarting,
  redeploying, or crashing the process discards all retained history.
  MikroView is a live/recent-history view, not a log archive; if you need
  durable logs, forward RouterOS's syslog output to a second, dedicated
  logging destination as well.
- **No secrets reach the browser.** The optional AbuseIPDB API key
  (`reputation.abuseIPDBKey` / `MIKROVIEW_ABUSEIPDB_KEY`) is read
  server-side only and used solely to call AbuseIPDB's API from the
  backend (`internal/reputation`) — it's never sent to, or readable by,
  the frontend.
- **No outbound calls except the on-demand IP lookup.** MikroView never
  calls out to the network on its own initiative. The only outbound
  requests it ever makes are to Shodan's InternetDB and (if configured)
  AbuseIPDB, and only when a user explicitly clicks "investigate" on one
  specific public IP — see the "IP reputation lookup" section of
  [docs/configuration.md](docs/configuration.md) for exactly what that
  does and doesn't do.

## Network exposure, by listener

| Listener | Auth | TLS | Notes |
|---|---|---|---|
| HTTP (`api.Server` + static UI) | None | None | Serves the UI and all `/api/*` routes to anyone who can reach it. |
| Syslog UDP/TCP | None | None | Accepts and parses any line from any source as if it were a real RouterOS device. |
| WebSocket (`/api/ws`) | None | None | `CheckOrigin` always allows, matching the no-auth trusted-LAN model — see `internal/api/ws.go`. |

## Hardening already in place

These don't change the no-auth threat model above, but they do bound the
damage a hostile or misbehaving LAN device can do:

- The syslog UDP listener never blocks on a slow downstream consumer —
  a full buffer drops the incoming datagram rather than letting it back
  up into the kernel receive buffer (`internal/syslog/udp_listener.go`).
- The syslog TCP listener caps concurrent connections and closes any
  connection that's gone idle too long, so it can't be used to exhaust
  goroutines/file descriptors by opening connections and never closing
  them (`internal/syslog/tcp_listener.go`).
- The WebSocket hub never blocks a slow browser tab from receiving
  events — a full per-client queue evicts its oldest event rather than
  applying backpressure to ingestion, and the drop count is surfaced to
  that client (`internal/hub/hub.go`).
- The main HTTP server sets read/write/idle timeouts, so a client that
  trickles in a request slowly (or never finishes one) can't tie up a
  connection indefinitely (`main.go`). These don't apply to `/api/ws`
  once a connection is upgraded — that path manages its own deadlines.
- The rule/raw-line search filter is matched with Go's RE2 engine, which
  has no catastrophic-backtracking behavior — an expensive or malicious
  regex in a filter can't peg a CPU core (`internal/store/query.go`).
- The container image is built on `distroless/static-debian12:nonroot`:
  no shell, no package manager, and it runs as a non-root user.

## Recommended deployment hardening

- Bind the HTTP and syslog ports to a loopback or trusted-LAN-only
  interface — set `MIKROVIEW_LISTEN_HTTP`/`listen.http` to a specific
  address rather than a bare port, and scope the host side of
  `deploy/docker-compose.yml`'s port mappings the same way, rather than
  publishing on every interface. See
  [docs/configuration.md](docs/configuration.md) for the full option
  reference.
- If you need to view MikroView from outside that LAN, put it behind a
  VPN (e.g. WireGuard/Tailscale) rather than port-forwarding or reverse-
  proxying it onto the open internet. There is no login screen to stop
  anyone who reaches it.
- Keep `config.yaml` itself off of any shared/multi-tenant filesystem —
  besides router names/IPs, it may also hold your AbuseIPDB API key
  (`reputation.abuseIPDBKey`) if you've configured one; prefer the
  `MIKROVIEW_ABUSEIPDB_KEY` env var over the YAML field if the file
  itself might be more widely readable than your environment.

## Reporting a vulnerability

This is a small, personally-maintained project without a formal
disclosure program. If you find a security issue, please open a GitHub
issue describing it — for anything you'd rather not post publicly first,
open a minimal issue asking for a private contact channel instead of
including details in it.
