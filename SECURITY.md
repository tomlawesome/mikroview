# Security policy

MikroView was originally built for one specific deployment shape: a
single instance, on a trusted home/office LAN, with no authentication,
over plain HTTP. Two things have since changed that: local accounts
(on first load, mikroview presents a one-time choice -- create the
admin account, or explicitly skip auth for this deployment; see
"Authentication" below), and TLS being on by default -- an app serving
real login credentials and session cookies has no business doing so over
cleartext, LAN or not (see "TLS" below). This document makes all of
that explicit, since none of it is the kind of thing that should be left
implicit for anyone deciding whether/how to deploy this.

## Threat model

**Before a decision is made** (the brief window between first load and
completing the choice screen), mikroview restricts every endpoint except
the handful needed to render and complete that screen -- there is no
window where live event data is readable before someone has actually
decided how this deployment should behave.

**If auth is skipped**, mikroview has no authentication and assumes
network-level trust instead, exactly like older versions of mikroview
did unconditionally. Anyone who can reach its HTTP port can view all
retained firewall events (including source/destination IPs, ports, and
the "investigate this IP" reputation lookup) and, indirectly, everyone's
router configuration as reflected in `config.yaml`'s device list. Anyone
who can reach its syslog port can inject fabricated events — the syslog
listeners accept any well-formed line from any source and never
authenticate the sender as a real RouterOS device.

This is fine on a LAN you already trust every device on. **It is not
safe to expose mikroview's HTTP or syslog ports to the internet, to an
untrusted network, or to a network segment shared with devices you don't
control** -- and this matters more than it used to: mikroview now
accumulates real network-activity insight (behavioral flags, per-host
baselines, friendly host/rule names) that an unauthenticated visitor
could use to scout a network without needing to prove anything, which is
the reason authentication exists at all. Creating an account rather than
skipping is strongly recommended for any deployment reachable by more
than you.

**Once an account exists**, every request except `/api/healthz` requires
a valid session -- see "Authentication" below for the full model. The
syslog listeners themselves remain unauthenticated regardless (RouterOS's
syslog protocol has no credential to check), so the "don't expose the
syslog port beyond a trusted network" guidance above still applies
either way.

## Authentication

- **Local accounts (username/password, Argon2id-hashed, `internal/auth`),
  plus optional OIDC/SSO.** SSO is strictly additive -- local login keeps
  working unmodified whether or not it's configured. See
  [docs/configuration.md](docs/configuration.md#single-sign-on-oidcsso)
  for setup; the security-relevant properties are below.
  - **Identity is `(issuer, subject)` only, never email/username.** A
    provider-side email or username reassignment can never silently
    inherit an existing mikroview account -- the display name shown
    for a JIT-provisioned account is a hint only, and falls back to a
    generated one on any collision with an existing account.
  - **Only asymmetric-signed ID tokens are accepted** (RS256/ES256/PS256)
    -- HS256 and `none` are rejected outright by an explicit allowlist,
    not by trusting whatever the provider's discovery document claims to
    support, closing the classic "resign with the public key as an HMAC
    secret" algorithm-confusion attack.
  - **Authorization Code + PKCE always**, with CSRF `state` and a
    replay-resistant `nonce`, both checked with constant-time comparison.
    The in-flight login's PKCE verifier/state/nonce are held in an
    AES-256-GCM-sealed, `HttpOnly` cookie -- confidential and
    tamper-evident, cleared after a single use regardless of outcome.
  - **`redirect_uri` is built only from `oidc.publicBaseUrl`**, never
    from a request's `Host`/`X-Forwarded-Host` header -- deriving it
    from client-influenced input is a known vulnerability class.
  - **A misconfigured or unreachable provider degrades to "SSO
    unavailable"** (logged once at startup) and never affects local
    login.
  - Verified end-to-end against a real, freshly bootstrapped Authentik
    instance (not just unit tests): the full redirect → real login form
    → PKCE exchange → RS256 token verification → account provisioning →
    session flow, including a repeat login correctly reusing the same
    account.
- **The first-run choice is made via the web UI**, not a CLI command --
  whoever loads mikroview first sees a one-time screen offering "create
  the admin account" or "continue without an account." Don't leave
  mikroview reachable by an untrusted network before this is resolved:
  whoever gets there first makes the choice, and if they create an
  account, they claim the admin role.
- **Skipping is a permanent, deliberate decision, not a default you fall
  into.** Once skipped, mikroview stays fully open indefinitely -- there
  is no way to re-enable auth from the web UI or any API call. Reversing
  it requires `mikroview -enable-auth-setup` (container/host access),
  which only re-arms the choice screen; it does not create an account by
  itself. This is intentional: it means nobody who can merely reach the
  running app -- as opposed to the host it runs on -- can unilaterally
  impose or remove authentication for everyone else.
- **Sessions are opaque, server-side, and in-memory** (not JWTs) -- easy
  to revoke, no signing-key management, but lost on a server restart
  (re-login is required; this does not affect account survival, which is
  persisted separately -- see "Data handling").
- **The session cookie's `Secure` flag is on by default**
  (`auth.secureCookie` / `MIKROVIEW_AUTH_SECURE_COOKIE`), matching TLS
  being on by default -- see "TLS" below. Only turn this off if you've
  also set `tls.enabled: false`, or sessions won't work at all (a
  `Secure` cookie is never sent back over a plain connection).
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
- **API tokens (`internal/auth.Token`, issue #101) are read-only and
  scoped by construction, not by convention.** A valid
  `Authorization: Bearer <token>` request is routed to a completely
  separate, minimal internal router carrying only `GET /api/events`,
  `/flags`, `/stats`, and `/devices` -- there is no handler for anything
  else registered on it, so no code path exists from a bearer token to a
  write, clear, or config endpoint, regardless of the method or path
  requested. Tokens are admin-created/revoked only (`POST`/`GET`/`DELETE
  /api/tokens`); the raw value is shown exactly once, at creation, and
  only its SHA-256 hash is ever persisted (Argon2id is deliberately not
  used here -- that cost exists to slow down guessing a low-entropy,
  human-chosen password, and a token's value is already a 128-bit random
  string). There is no expiry; a token is valid until explicitly
  revoked, same as a session or account.

## TLS

- **On by default, on mikroview's main listener** -- the application
  itself is never served over plain HTTP. A reverse proxy in front
  doesn't close the underlying problem on its own: mikroview's own
  listener stays fully reachable and functional over plain HTTP
  regardless of whether an RP exists upstream, so anyone who reaches it
  directly (by IP, by habit, by not knowing the RP's hostname) gets the
  same authenticated app in cleartext. TLS at mikroview's own listener
  closes that regardless of how it's reached.
- **A second, redirect-only listener** (`listen.httpRedirect`, off by
  setting it to `""`) exists purely so a client that guesses plain HTTP
  gets bounced to HTTPS instead of a connection reset -- it serves
  nothing but a 308 redirect to the HTTPS listener, never the
  application itself, so it doesn't reopen the cleartext-exposure gap
  the point above closes.
- **Zero-config default: a self-generated local CA + certificate**
  (`internal/servertls`), persisted across restarts if `tls.storePath`
  is configured (optional, same contract as `flags.storePath`) so the
  trust step is a one-time cost. This CA is trust-on-first-use, not a
  globally trusted root -- fine for an admin interface on infrastructure
  you already control, not a substitute for a real cert if you have one
  (`tls.certFile`/`tls.keyFile` take priority over generation).
- **The CA is served at `/ca.crt`, unauthenticated** (only when
  mikroview generated one -- never for a supplied cert), specifically so
  a browser or reverse proxy can fetch it to establish trust. Its
  fingerprint is also logged at startup for out-of-band verification
  instead of blind trust-on-first-use, if you want it.
- **One documented exception**: `tls.enabled: false` keeps mikroview's
  listener on plain HTTP, same as before this feature existed. This is
  **only** safe when mikroview's listener is provably unreachable except
  from your own reverse proxy over an isolated docker network -- never
  published to a LAN or the internet. In that specific topology the RP
  already owns TLS termination for real clients, and there's no bypass
  surface for mikroview to additionally protect on that internal hop.
  Logged clearly at startup whenever set, so it's never a silent state.
  See [docs/configuration.md](docs/configuration.md#tls) for the
  reverse-proxy backend-TLS pattern this is an alternative to (pointing
  your RP's upstream at `https://mikroview:PORT` instead, which needs no
  new port and works for every other topology).

## Data handling

- **No persistence for events.** Events live in an in-memory ring buffer
  only — there is no database. Restarting, redeploying, or crashing the
  process discards all retained history. MikroView is a live/recent-
  history view, not a log archive; if you need durable logs, forward
  RouterOS's syslog output to a second, dedicated logging destination as
  well.
- **Three deliberate exceptions: behavioral flags, accounts, and API
  tokens.** Raised flags (port scans, activity spikes, critical-port
  attempts, volume spikes -- see
  [docs/configuration.md](docs/configuration.md)), accounts (plus the
  create/skip decision), and API tokens are each persisted to small JSON
  files under `/var/lib/mikroview` by default (`flags.storePath` /
  `auth.storePath` / `auth.tokensStorePath`), which the container
  creates and owns -- no configuration needed for any of them to survive
  a process restart. The flags file contains the IP addresses that
  triggered a flag and a short human-readable description; the accounts
  file contains usernames and Argon2id password hashes (never plaintext
  passwords); the tokens file contains token names and SHA-256 hashes
  (never the raw bearer values). Treat all three with the same care as
  `config.yaml` (see "Recommended deployment hardening" below). None
  survive *container recreation* (as opposed to a simple restart) unless
  you mount a volume over `/var/lib/mikroview` -- for accounts
  specifically, that means the create/skip decision itself reverts to
  undecided on recreation without a volume, which re-shows the first-run
  choice screen rather than silently reopening or silently re-gating the
  deployment.
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
| HTTP (`api.Server` + static UI) | Session cookie once an account exists (or an API bearer token, read-only, for four `GET` routes only — see "API tokens" above); restricted to the choice-screen endpoints while undecided; fully open once skipped | On by default (self-generated or supplied) | See "TLS" above for the zero-config default and the one supported reason (`tls.enabled: false`) to disable it. `/api/healthz` always stays open. |
| Syslog UDP/TCP | None | None | Accepts and parses any line from any source as if it were a real RouterOS device -- unaffected by auth state. TLS doesn't apply here; RouterOS's syslog protocol has no TLS mode. |
| WebSocket (`/api/ws`) | Session cookie + same-origin check, once an account exists; blocked entirely while undecided (not in the choice-screen exemption list); open, no origin check, once skipped | Follows the HTTP listener (`wss://` when TLS is on) | `CheckOrigin` is permissive whenever `Auth.Count() == 0` (undecided or skipped) — moot for "undecided", since `requireAuth` never lets the request reach this handler in that state. See `internal/api/ws.go`. |

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
  VPN (e.g. WireGuard/Tailscale) rather than port-forwarding it onto the
  open internet -- creating an account rather than skipping (see
  "Authentication" above), plus TLS being on by default (see "TLS"
  above), meaningfully raise the bar versus mikroview's original
  no-auth/no-TLS posture, but neither is a substitute for not exposing
  an admin interface to the open internet at all in the first place.
- Keep `config.yaml` itself off of any shared/multi-tenant filesystem —
  besides router names/IPs, it may also hold your AbuseIPDB API key
  (`reputation.abuseIPDBKey`) if you've configured one; prefer the
  `MIKROVIEW_ABUSEIPDB_KEY` env var over the YAML field if the file
  itself might be more widely readable than your environment.
- The same applies to `flags.storePath`'s default file under
  `/var/lib/mikroview` — it holds real IP addresses and short
  descriptions of what they triggered, so keep it off a shared
  filesystem the same way.
- **Create an account before exposing mikroview beyond a fully trusted
  network** — see "Authentication" above; leaving it open on anything
  wider than a trusted LAN means anyone who reaches it first claims the
  admin account. Keep `auth.storePath`'s file off a shared filesystem
  too, same reasoning as the flags file.
- TLS is already on by default (see "TLS" above) -- there's nothing to
  turn on. If you've deliberately set `tls.enabled: false`, double-check
  it against that section's exact safe-use precondition (an isolated
  reverse-proxy network, never a directly reachable port) before relying
  on it.

## Reporting a vulnerability

This is a small, personally-maintained project without a formal
disclosure program. If you find a security issue, please open a GitHub
issue describing it — for anything you'd rather not post publicly first,
open a minimal issue asking for a private contact channel instead of
including details in it.
