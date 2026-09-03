# Security policy

MikroView was originally built for one specific deployment shape: a
single instance, on a trusted home/office LAN, with no authentication,
over plain HTTP. Two things have since changed that: local accounts
(on first load, mikroview asks for an admin account and will not serve
anything until one exists; see "Authentication" below), and TLS being
on by default -- an app serving
real login credentials and session cookies has no business doing so over
cleartext, LAN or not (see "TLS" below). This document makes all of
that explicit, since none of it is the kind of thing that should be left
implicit for anyone deciding whether/how to deploy this.

## Threat model

**Before an admin account exists** (the brief window between first load
and someone completing the registration screen), mikroview restricts
every endpoint except the handful needed to render and complete that
screen -- there is no window where live event data is readable before
someone has claimed the deployment.

There is no way to run mikroview without authentication. An earlier
version offered "continue without an account" as a first-run choice; it
was removed, and the code path with it -- see "Authentication" below for
the full model and the reasoning. What follows describes the risks that
remain once an account exists, not an optional unauthenticated mode.

**The syslog port is unauthenticated, by design and unavoidably.**
Anyone who can reach it can inject fabricated events: the listeners
accept any well-formed line from any source and never authenticate the
sender as a real RouterOS device, because RouterOS's logging action has
no client-certificate option to authenticate with. TLS on that port
gets you confidentiality on the wire and the router verifying
mikroview, not the reverse.

**It is not safe to expose mikroview's HTTP or syslog ports to the
internet, to an untrusted network, or to a network segment shared with
devices you don't control** -- and this matters more than it used to:
mikroview accumulates real network-activity insight (behavioral flags,
per-host baselines, friendly host/rule names) which is precisely a
reconnaissance map of the network it is watching, and the syslog port
lets an attacker write into that picture as well as read the
consequences of it.

**Once an account exists**, every request except `/api/healthz` requires
a valid session -- see "Authentication" below for the full model. The
syslog listeners themselves remain unauthenticated regardless (RouterOS's
syslog protocol has no credential to check), so the "don't expose the
syslog port beyond a trusted network" guidance above still applies
either way.

## How features get built

New features are researched before they are designed, including an
explicit CVE search and a comparison against known secure and insecure
implementations of the same thing. Industry norms carry weight but are
verified rather than assumed — several widely-repeated ones turned out
to be stale or simply wrong when checked.

See [docs/security-by-design.md](docs/security-by-design.md).

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
  - **Only self-hosted identity providers are supported, and that is
    enforced, not advised.** Mikroview's OIDC support rests on the
    issuer URL *being* the access control: tokens are verified against
    the configured issuer's own keys and your client ID, so a
    self-hosted Authentik/Keycloak/Zitadel issuer already restricts
    login to a directory you run. That property is false for a public
    provider -- every Google account validates against
    `accounts.google.com` -- and combined with "first account becomes
    admin", such a deployment would hand admin to whoever reached the
    login page first. **Multi-tenant issuers (Google, Apple, and
    Microsoft's shared `/common`, `/organizations`, `/consumers`
    endpoints) are refused at startup and cannot be enabled by
    configuration.** SSO stays off, the reason is logged, and local
    login is unaffected. Entra's *single-tenant* issuer URL is
    correctly permitted, since it does scope logins to one
    organisation. A safe configuration for a public provider exists and
    was deliberately not shipped -- see
    [docs/decisions/multi-tenant-oidc.md](docs/decisions/multi-tenant-oidc.md).
  - **Access restrictions fail closed and are re-checked every login.**
    A missing, empty or unreadable claim is a refusal, never a pass --
    a group allowlist that opens up when the provider forgets to
    release the claim is not an allowlist. Domain restrictions match
    whole domains rather than string suffixes (so `example.com` never
    admits `attacker@notexample.com`) and require the provider's own
    `email_verified`. The check runs *before* account provisioning, so
    a refused identity is never created as a side effect of being
    refused, and revoking someone at the IdP locks them out at their
    next sign-in rather than whenever their session lapses. The refused
    user is told they aren't permitted but never which condition
    failed -- that detail goes to the operator's log, since it would
    otherwise map out the allowlist.
  - Verified end-to-end against a real, freshly bootstrapped Authentik
    instance (not just unit tests): the full redirect → real login form
    → PKCE exchange → RS256 token verification → account provisioning →
    session flow, including a repeat login correctly reusing the same
    account.
- **First-run registration happens in the web UI**, not via a CLI
  command -- whoever loads mikroview first sees a one-time screen asking
  them to create the admin account. There is no second option on that
  screen; see the "no way to run without authentication" point below.
  Don't leave mikroview reachable by an untrusted network before it is
  completed: whoever gets there first claims the admin role.
- **"Whoever gets there first" means exactly one winner, enforced
  atomically.** First-run registration is resolved under a single lock:
  concurrent attempts cannot all succeed. The precondition is re-checked
  with the write lock held rather than before taking it, which matters
  because password hashing (Argon2id, ~100ms by design) runs first and
  would otherwise leave a wide window. Without this, N simultaneous
  registrations would each create an admin. A regression test exercises
  the race directly (`internal/auth/store_test.go`).
- **There is no way to run mikroview without authentication.** An
  earlier version offered "no authentication" as a first-run choice.
  It was removed. An unauthenticated mikroview publishes which hosts are
  being scanned, which rules fire, which ports are under pressure, and
  which accounts exist -- a reconnaissance map of the network it is
  meant to be watching -- and "it's only for a few minutes" does not
  survive contact with a deployment nobody got round to changing.
  Creating a local account is one screen, and it is the floor.

  A deployment that took the old option upgrades into the ordinary
  "no account yet" state: it fails closed, serves nothing but the
  create-account screen, and says so at startup rather than leaving the
  operator to work out why a system that has been open since they
  installed it is suddenly asking for a login.
- **A corrupt or unreadable accounts file fails closed, not open.**
  mikroview refuses to start (rather than silently falling back to an
  empty, zero-account state) if the accounts file exists but can't be
  loaded -- a fresh install (no file at all) is unaffected and boots
  normally. Without this, a lost/corrupted accounts file would look
  identical, on the next restart, to a genuine fresh install: the
  first-run setup screen, reachable by whoever gets there first, on a
  deployment that previously had real accounts. Recovering is a
  container/host-access action, the same trust anchor as the other
  recovery commands below: restore the file from a backup, or move/
  delete it (the boot-failure log message names the exact path) and
  restart to consciously re-arm the first-run setup screen -- no
  dedicated CLI mode for this, since an operator who can already run
  `mikroview -recover-admin-account` can already `mv` or `rm` a file on
  the same host.
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
  UI/API entirely, and requires a recovery key on top of host access**:
  `mikroview -recover-admin-account` (prompts for a recovery key, then
  for a new password, both with echo suppressed -- never a CLI argument
  or env var, so neither touches shell history, process args, or
  `docker inspect` output). There is deliberately no standalone account-listing command: the
  one thing it was needed for -- finding the username to transfer admin to
  while locked out -- is now a numbered list inside `-transfer-admin`,
  behind the recovery key that command already requires. Gating a
  standalone list on a key was rejected as teaching the operator to type a
  recovery key for a routine read.
  Keeping it off the web UI/API means a locked-out admin isn't dependent
  on the very system they're locked out of. A password reset immediately
  invalidates every existing session for that account, including on an
  already-running server.
- **Recovery-key digests and the pepper are kept apart, and follow
  different storage.** A recovery key is never stored -- what is stored
  is an HMAC-SHA-256 digest of it, computed under a 256-bit server-side
  secret (the pepper). The pepper is not there to make guessing harder;
  the keys are 160 random bits, which is not brute-forceable either way.
  It is there so the digests are inert on their own: whoever holds only
  the digests cannot test a candidate key against them.

  That only pays off if the two halves can actually be stolen
  separately. On the JSON backend they are separate files on one host,
  which protects against a partial leak (one file in a backup, a stray
  copy, a wrong bind-mount) and nothing more. When Postgres is
  configured the digests follow the accounts into the database while the
  pepper stays a local file on the mikroview host -- so a database dump
  yields digests nothing can test, and compromising the mikroview host
  yields a pepper with no digests to apply it to. Neither prize is
  useful alone.

- **Recovery keys are never printed by the container's main process**,
  because in a container that stream is the container log: `docker run
  -t` allocates a pty, so a terminal check passes while the log driver
  still writes every byte to disk, and logs are the artefact most likely
  to be shipped off the host and retained for months. Measured, not
  assumed -- keys printed that way were recovered from
  `/var/lib/docker/containers/<id>/<id>-json.log` and used to take over
  the admin account.

  `-generate-recovery-keys`, `-recover-admin-account` and
  `-transfer-admin` refuse to run as PID 1 and name `docker compose exec`
  instead, whose output the log driver does not capture. The check runs
  before any of them does work, so a refusal never leaves a half-finished
  transfer. On a host install PID 1 is the system's init, so printing is
  allowed there, which is correct.

  The keys are not written to a file at any point. Handing them over via
  a file on the data volume was implemented and then removed: the
  operator has to read the file to use it, so the keys reach a terminal
  regardless, while the file adds plaintext keys on the data volume --
  captured by any backup taken during that window, and left behind
  entirely if the process dies before deleting it. It moved the exposure
  and charged a disk copy for it.

  It scopes to **the admin account only** and **refuses an SSO-only
  admin**. The command it replaced (`-reset-password <username>`) could
  rewrite any account's password from host access alone, which made
  every user account a route to a working login, and gave no protection
  at all against a lower-privileged local account or container exec that
  could run the binary. Recovery keys are the second factor -- see
  "Recovery keys" below.
- **Nothing mikroview runs is loaded from disk at runtime.** There is no
  `os/exec`, no `plugin.Open`, no shared-library loading, and no
  interpreter anywhere in the module. The database schema migrations are
  compiled into the binary with `go:embed` -- verified by running the
  binary from a directory containing no `migrations/` at all and
  confirming the schema still applied. **There is therefore no migration
  script on a deployed system to tamper with**, and the runtime image is
  distroless, so even a written-in script would have nothing to execute
  it.

  Every file mikroview *does* read at runtime is data, not code:
  `config.yaml`, the JSON stores, TLS material, the Postgres DSN. Editing
  those requires host access, which is already the declared trust anchor
  for the CLI recovery commands -- an attacker holding it does not need
  to smuggle in code.

- **Integrity is verified before the artefact runs, not by the artefact
  itself.** Published images are signed with keyless Sigstore/cosign and
  carry SLSA build provenance and an SBOM, all bound to the image
  *digest* rather than a tag (a tag is a mutable pointer; signing one
  would let it later point at different content and still verify):

  ```sh
  cosign verify ghcr.io/tomlawesome/mikroview@sha256:... \
    --certificate-identity-regexp '^https://github.com/tomlawesome/mikroview/' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  gh attestation verify oci://ghcr.io/tomlawesome/mikroview@sha256:... \
    --repo tomlawesome/mikroview
  ```

  **Releases are rebuilt, and re-earn what rebuilding costs.** A
  `v<x.y.z>` binary has to be built from the release, so `main` cannot
  simply retag preview's digest for it. Retagging existed to guarantee
  that the exact bytes smoke-tested are the bytes that ship, so the
  release job runs the container smoke test **against the image it just
  built, before `latest` moves to it**, and additionally asserts the
  binary reports the version being released. The guarantee is preserved,
  just re-earned on the release artefact instead of inherited. The
  release is tagged last, so a failure anywhere above leaves no tag
  claiming a release happened. See
  [docs/decisions/release-versioning.md](docs/decisions/release-versioning.md).

  **Self-verification was considered and deliberately rejected** -- an
  app checking its own checksums, against GitHub or anything else, does
  not work. The check lives inside the thing being verified, so tampered
  code simply skips it; it makes a network service a startup dependency
  and a new trust anchor for every deployment; and it tells a third party
  when and where mikroview is running. Signing moves the trust anchor
  outside the artefact, which is the only place it can usefully be.

- **Persisted state can live in Postgres instead of JSON files (issue
  #131), and the only security benefit is separation from this host.**
  The connection is required to be encrypted -- `sslmode` below
  `require` is refused at startup rather than silently upgraded, and
  `verify-full` is what actually authenticates the server rather than
  merely encrypting to it. The DSN is read from a file, never a config
  field or a flag, for the same reason `-recover-admin-account` prompts
  rather than taking an argument: a password in argv is visible to every
  process on the host.

  **A same-host database, including a container beside mikroview,
  provides none of that benefit** -- its credential sits inside the
  exact compromise it was meant to survive, making it strictly worse
  than the files it replaced. `deploy/docker-compose.yml` therefore
  ships no Postgres service, not even commented out, because the
  copy-pasteable example has to be the thing that is actually
  recommended.

  A configured-but-unreachable database is a hard startup failure, not a
  fallback to the local files: falling back would run the deployment on
  stale accounts with no outward sign anything was wrong.
- **Linking an account to SSO is a one-way, destructive conversion.**
  `POST /api/auth/oidc/link` attaches an OIDC identity to the *calling
  session's own* account and, in the same store operation, replaces its
  password with a fresh unmatchable hash and clears
  `HasLocalPassword`. There is deliberately no state where a local
  password and a linked identity both work — that would keep the weaker
  local-password surface alive on an account that has supposedly moved
  past it. The invariant lives inside `auth.Store.LinkOIDCIdentity`, not
  at the API layer, so a future caller cannot forget it.

  It is **POST, not a GET redirect**, specifically because it is
  destructive: a GET-initiated flow is triggerable cross-site, and with
  an identity provider that silently re-authenticates, a victim's
  password could be destroyed by a link they never requested. POST puts
  it behind the CSRF header. The target account comes from the session
  and is sealed into the flow state — never from the request — and the
  session is re-checked against that sealed value at the callback, so a
  sign-out-and-in mid-flow can't attach an identity to the wrong
  account.
- **Admin is a single, transferable role, and transfer is CLI-only**
  (`mikroview -transfer-admin <username>`, recovery-key gated). No
  authenticated session can grant or move admin. The reasoning is that
  identity-provider credentials are usually distinct from host access:
  if an admin's IdP account is compromised, the attacker can sign in,
  but cannot make themselves the durable owner of the deployment or
  demote the real admin out of it. A second admin tier was considered
  and rejected -- for the amount of administration mikroview actually
  has, it adds attack surface without adding capability.
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
- **What a warm restart saves, and what it does not.** So that a restart
  does not silently reset every counter to zero, mikroview writes a
  small snapshot of its *derived* state every few minutes and reads the
  newest one back on the next boot (`snapshot.dir`,
  `snapshots/` beside its other state by default, mode 0600 in a 0700
  directory, six generations kept). A snapshot holds counts, the
  per-minute stamps behind the hourline, rule and log-prefix labels,
  device ids and names with their first and last seen, and each
  detector's rolling window counts keyed by source address. It never
  holds event lines, packet payloads, or the rule/NAT/DHCP tables your
  routers push -- those three stay in memory for the life of the
  process, which is what the bullet above promises and this feature does
  not change. Snapshot files are **not encrypted at rest**: they are
  covered by the same open decision as the other state files, issue
  #853, and until that lands they need the same filesystem care as
  `config.yaml`. Losing the directory costs nothing but a cold start, so
  there is no reason to include it in a backup.
- **Deliberate exceptions: behavioral flags, accounts, API tokens, the
  stale-rule usage record, and the new-device MAC registry.** Raised
  flags (port scans, activity spikes, critical-port attempts, volume
  spikes -- see [docs/configuration.md](docs/configuration.md)),
  accounts (plus the create/skip decision), API tokens, the per-rule
  first/last-seen usage record backing the stale-rule detector, and the
  new-device detector's per-MAC first/last-seen history are each
  persisted to small JSON files under `/var/lib/mikroview` by default
  (`flags.storePath` / `auth.storePath` / `auth.tokensStorePath` /
  `flags.ruleUsageStorePath` / `deviceMac.storePath`), which the
  container creates and owns -- no configuration needed for any of them
  to survive a process restart. The flags file contains the IP addresses
  that triggered a flag and a short human-readable description; the
  accounts file contains usernames and Argon2id password hashes (never
  plaintext passwords); the tokens file contains token names and
  SHA-256 hashes (never the raw bearer values); the rule-usage file
  contains rule labels and timestamps only; the MAC registry file
  contains LAN client MAC addresses and timestamps only. Treat all of
  these with the same care as `config.yaml` (see "Recommended
  deployment hardening" below). None survive *container recreation* (as
  opposed to a simple restart) unless you mount a volume over
  `/var/lib/mikroview` -- for accounts specifically, that means the
  create/skip decision itself reverts to undecided on recreation
  without a volume, which re-shows the first-run choice screen rather
  than silently reopening or silently re-gating the deployment; for the
  MAC registry, it means every MAC looks "new" again after a recreation
  without a volume.
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
| Syslog TLS | None | Always (mikroview's only syslog listener) | Accepts and parses any line from any source as if it were a real RouterOS device -- unaffected by auth state. TLS buys confidentiality on the wire and mikroview authenticating itself to the router, but not the reverse: RouterOS's logging action has no client-certificate option, so anything able to reach the port can still connect and inject log lines. |
| WebSocket (`/api/ws`) | Session cookie + same-origin check, once an account exists; blocked entirely while undecided (not in the choice-screen exemption list); open, no origin check, once skipped | Follows the HTTP listener (`wss://` when TLS is on) | `CheckOrigin` is permissive whenever `Auth.Count() == 0` (undecided or skipped) — moot for "undecided", since `requireAuth` never lets the request reach this handler in that state. See `internal/api/ws.go`. |

## Hardening already in place

These don't change the no-auth threat model above, but they do bound the
damage a hostile or misbehaving LAN device can do:

- The syslog TLS listener caps concurrent connections and closes any
  connection that's gone idle too long, so it can't be used to exhaust
  goroutines/file descriptors by opening connections and never closing
  them (`internal/syslog/tcp_listener.go`'s `ServeTCP`, which
  `internal/syslog/tls_listener.go` wraps in TLS).
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
- **Source-address attribution behind a reverse proxy is opt-in, and
  ignoring forwarding headers is the default.** A forwarding header is
  just text the client sent; honouring one unconditionally would let
  anyone mint a fresh, empty rate-limit bucket per request by varying
  it, which deletes the per-source limiter rather than weakening it. So
  a header is read only when the connection came from an address the
  operator listed in `listen.trustedProxies`, and the chain is then
  walked right-to-left, skipping trusted hops, so a client's forged
  left-hand entries are never what gets used. With no proxies declared,
  only the transport-level peer address counts. The cost of leaving it
  unset behind a proxy is a shared bucket, not a bypass: all users key
  to the proxy's address, so one attacker's failures can lock the
  deployment out of *password* login (the per-username limiter is
  unaffected, as is SSO). See
  [docs/configuration.md](docs/configuration.md#running-behind-a-reverse-proxy).

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
  itself might be more widely readable than your environment. The same
  applies to any credentials configured under `notify` -- SMTP's
  `notify.smtp.password`, Pushover's `notify.pushover.token`, and any
  auth header set under `notify.webhook.headers` (e.g. a bearer token
  for ntfy/Home Assistant/n8n) -- `notify.smtp.password` and
  `notify.pushover.token`/`.user` have env var equivalents (see
  [docs/configuration.md](docs/configuration.md)) for the same reason;
  `notify.webhook.headers` is YAML-only, so if it holds a real secret,
  that's one more reason to keep `config.yaml` itself off a shared
  filesystem.
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
