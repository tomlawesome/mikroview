# Security policy

MikroView was originally built for one specific deployment shape: a
single instance, on a trusted home/office LAN, with no authentication.
As of the local-auth feature, that's now **conditional**: mikroview
stays fully open (the original behavior) until you create the first
account, at which point authentication is required for everything except
`/api/healthz`. This document makes both states' threat model explicit,
since neither is the kind of thing that should be left implicit for
anyone deciding whether/how to deploy this.

## Threat model

**Before an account exists**, mikroview has no authentication and
assumes network-level trust instead. Anyone who can reach its HTTP port
can view all retained firewall events (including source/destination IPs,
ports, and the "investigate this IP" reputation lookup) and, indirectly,
everyone's router configuration as reflected in `config.yaml`'s device
list. Anyone who can reach its syslog port can inject fabricated events —
the syslog listeners accept any well-formed line from any source and
never authenticate the sender as a real RouterOS device.

This is fine on a LAN you already trust every device on. **It is not
safe to expose mikroview's HTTP or syslog ports to the internet, to an
untrusted network, or to a network segment shared with devices you don't
control** -- and this matters more than it used to: mikroview now
accumulates real network-activity insight (behavioral flags, per-host
baselines, friendly host/rule names) that an unauthenticated visitor
could use to scout a network without needing to prove anything, which is
the reason authentication exists at all. Creating an account as early as
practical is strongly recommended for any deployment reachable by more
than you.

**Once an account exists**, every request except `/api/healthz` requires
a valid session -- see "Authentication" below for the full model. The
syslog listeners themselves remain unauthenticated regardless (RouterOS's
syslog protocol has no credential to check), so the "don't expose the
syslog port beyond a trusted network" guidance above still applies
either way.

## Authentication

- **Local accounts only, no SSO yet.** Username/password, Argon2id-hashed
  (`internal/auth`). OIDC/SSO is tracked separately and not required for
  this to be complete.
- **The first account is created via the web UI**, not a CLI command --
  whoever completes that one-time setup form becomes the admin. Don't
  leave mikroview reachable by an untrusted network before completing
  it: whoever gets there first claims the admin account.
- **Sessions are opaque, server-side, and in-memory** (not JWTs) -- easy
  to revoke, no signing-key management, but lost on a server restart
  (re-login is required; this does not affect account survival, which is
  persisted separately -- see "Data handling").
- **The session cookie's `Secure` flag is off by default**
  (`auth.secureCookie` / `MIKROVIEW_AUTH_SECURE_COOKIE`) because
  mikroview is very commonly run over plain HTTP on a trusted LAN, and
  forcing `Secure` would silently break login on any non-TLS deployment.
  If you run mikroview behind TLS (directly or via a reverse proxy), turn
  this on -- until you do, the session cookie (and your password, during
  login) crosses the network in cleartext.
- **A lightweight CSRF mitigation** requires a custom header
  (`X-Requested-With: mikroview`) on every mutating request once an
  account exists -- `SameSite=Lax` cookies already block a cross-site
  `<form>` POST from carrying the session cookie in modern browsers, so
  this is mostly defense-in-depth.
- **The WebSocket endpoint enforces same-origin** once an account
  exists, on top of the session-cookie check -- cookies are attached to
  cross-site requests regardless of `fetch`/CORS rules, so origin
  checking is what actually stops a malicious page from opening a live
  connection using a signed-in visitor's session.
- **Account recovery is a CLI command, deliberately outside the web
  UI/API entirely**: `mikroview -reset-password <username>` (prompts for
  a new password, no echo -- never a CLI argument or env var, so it
  never touches shell history, process args, or `docker inspect`
  output), and `mikroview -list-users` to see existing accounts.
  Container/host access (the ability to run these at all) is the trust
  anchor, so a locked-out admin isn't dependent on the very system
  they're locked out of. A password reset immediately invalidates every
  existing session for that account, including on an already-running
  server.

## Data handling

- **No persistence for events.** Events live in an in-memory ring buffer
  only — there is no database. Restarting, redeploying, or crashing the
  process discards all retained history. MikroView is a live/recent-
  history view, not a log archive; if you need durable logs, forward
  RouterOS's syslog output to a second, dedicated logging destination as
  well.
- **Two deliberate exceptions: behavioral flags, and accounts.** If
  `flags.storePath` / `MIKROVIEW_FLAGS_STORE_PATH` is configured, raised
  flags (port scans, activity spikes, critical-port attempts, volume
  spikes -- see [docs/configuration.md](docs/configuration.md)) are
  persisted to a small JSON file, since a flag is meant to stay visible
  until a human clears it. This file contains the IP addresses that
  triggered a flag and a short human-readable description -- treat it
  with the same care as `config.yaml` (see "Recommended deployment
  hardening" below). Left unconfigured (the default), flags behave like
  everything else: they work, they just reset on restart. **Accounts are
  not optional the same way**: once you create one, `auth.storePath` /
  `MIKROVIEW_AUTH_STORE_PATH` must be configured and writable, or the
  account would vanish on restart -- mikroview refuses to create one
  otherwise, rather than silently creating an account that either locks
  everyone out or (worse) silently reopens once it's gone. This file
  contains usernames and Argon2id password hashes (never plaintext
  passwords) -- treat it with at least the same care as the flags file.
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
| HTTP (`api.Server` + static UI) | Session cookie, once an account exists; open otherwise | None by default | Set `auth.secureCookie` once TLS is terminated in front of mikroview — see "Authentication" above. `/api/healthz` always stays open. |
| Syslog UDP/TCP | None | None | Accepts and parses any line from any source as if it were a real RouterOS device -- unaffected by whether an account exists. |
| WebSocket (`/api/ws`) | Session cookie + same-origin check, once an account exists; open otherwise | None by default | `CheckOrigin` is permissive only while no account exists — see `internal/api/ws.go`. |

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
- The behavioral detectors (`internal/detect`) bound their per-source
  tracking state the same way every other buffer in mikroview has an
  explicit ceiling — a scan using many spoofed or ephemeral source IPs
  can't grow that state without bound; the least-recently-active source
  is evicted first once the cap is reached.
- The container image is built on `distroless/static-debian12:nonroot`:
  no shell, no package manager, and it runs as a non-root user.
- Passwords are hashed with Argon2id (64 MiB memory, per-user random
  salt), never stored or logged in plaintext. Login failures are rate-
  limited per username and per source IP (`internal/auth.LoginLimiter`),
  and a failed login takes the same amount of time whether the username
  exists or not (a constant-time comparison against a dummy hash either
  way), so response timing can't be used to enumerate valid usernames.

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
- The same applies if you've enabled flag persistence (`flags.storePath`)
  — the resulting file holds real IP addresses and short descriptions of
  what they triggered, so keep it off a shared filesystem the same way.
- **Create an account before exposing mikroview beyond a fully trusted
  network** — see "Authentication" above; leaving it open on anything
  wider than a trusted LAN means anyone who reaches it first claims the
  admin account. Keep `auth.storePath`'s file off a shared filesystem
  too, same reasoning as the flags file.
- Turn on `auth.secureCookie` once mikroview sits behind TLS (directly or
  via a reverse proxy) — without it, the session cookie and login
  credentials cross the network in cleartext.

## Reporting a vulnerability

This is a small, personally-maintained project without a formal
disclosure program. If you find a security issue, please open a GitHub
issue describing it — for anything you'd rather not post publicly first,
open a minimal issue asking for a private contact channel instead of
including details in it.
