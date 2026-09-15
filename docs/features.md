# What MikroView does

## Features

- **Ingestion**: RouterOS forwards firewall log lines via
  `/system logging` over syslog-over-TLS (`remote-protocol=tls`). No
  polling, no RouterOS API access, no RouterOS credentials held by
  MikroView — push-based and cheap for the router. The optional config
  and backup pushes carry an ingest token MikroView mints per device.
- **Parsing**: a RouterOS-specific parser decodes chain, action, rule
  label, interfaces, protocol, addresses/ports, and length from each
  log line. See [docs/routeros-setup.md](routeros-setup.md) for the
  log-prefix convention that makes "accept vs. drop vs. reject" and the
  responsible rule visible at all.
- **Storage**: by default, events live in a fixed block of memory — a
  ring buffer that overwrites the oldest event once full, windowed to
  `store.retention` (default 24h) and gone on restart. An optional
  on-disk history (`history:` in config.yaml, off by default) writes
  the same events to one encrypted, compressed file per day and keeps
  `history.days` of them, so flag thresholds can be judged against
  weeks of real traffic rather than just what the ring still holds.
  See
  [docs/configuration.md](configuration.md#on-disk-event-history-optional-off-by-default).
  Behavioral flags (see below) can still persist to a small JSON file
  as before, since they're meant to stay visible until a human clears
  them.
- **Router backups**: an optional SFTP drop box (`backup:` in
  config.yaml, off by default) where the router's own nightly script
  pushes its binary `.backup` and plain-text `.rsc` export, so the
  copies are still to hand when the router itself is gone. See
  [docs/configuration.md](configuration.md#router-backups-over-sftp-optional-off-by-default).
- **Behavioral flags**: watches for port scans, per-source activity
  spikes, repeated attempts against critical ports (SSH, RDP, Winbox,
  ...) from external IPs, and network-wide volume spikes — each raises a
  flag for a human to review and clear, never an automatic action. See
  [docs/configuration.md](configuration.md) for the detectors and
  their thresholds.
- **Watchlist**: operator-defined entries, persisted and queryable, in
  two modes — **record** ("watch attempts against these ports") and
  **invert** ("this device should only ever reach these destinations",
  reviewed via an observe-then-promote workflow before anything is
  treated as a violation). Tuning entries against your own network's
  traffic is expected and ongoing, not a one-time setup step: MikroView
  presents what it saw and lets you decide what's expected, it never
  decides that for you. And like everything else here, it only ever
  sees what the router is actually configured to log — an entry with no
  matches can mean "nothing happened" or "nothing logs this," and
  telling those apart is on the operator, not the tool. See
  [docs/configuration.md](configuration.md)'s "Watchlist" section.
  Entries can also be **suggested** from data your router has already
  pushed (named DHCP leases, ports an existing rule already blocks), so
  there's something to react to rather than a blank page — reviewed
  Off/Accepted/Hidden, never applied automatically. See that section's
  "Suggested watchlist entries" subsection.
- **Live updates**: a WebSocket pushes new events to the browser in
  real time; historical/filtered queries go through a REST endpoint
  against the retained buffer. See
  [docs/configuration.md](configuration.md) for the API and the
  server/client filtering split.
- **UI**: Svelte, no component framework, dark professional theme,
  ~201KB of JavaScript over the wire (~667KB before compression). CI
  gates the bundle at 230KB gzipped — the measured reading plus ~15%,
  re-derived after the v0.4.0 interface reshape shipped (see
  [docs/decisions/ui-framework.md](decisions/ui-framework.md)).
- **Logging**: leveled (debug/info/warn/error) and colorized server
  output, auto-plain when piped or `NO_COLOR` is set. See
  [docs/configuration.md](configuration.md)'s "Logging" section.
- **Authentication**: required, always. On first load MikroView asks you
  to create the admin account, and serves nothing else until you do.
  After that it is required for everything except the health check
  (Argon2id-hashed passwords, opaque server-side sessions,
  self-registration for the first/super-admin account only). Local
  accounts and single sign-on via an external OIDC identity provider
  (e.g. Authentik, Keycloak, Entra ID) can both be enabled at once. See
  [docs/configuration.md](configuration.md) and
  [SECURITY.md](../SECURITY.md) for the threat model and setup.

## Security levels

Auth isn't one setting — the choice you make trades off convenience
against how much a compromise of the MikroView host itself can expose.
From least to most isolated:

1. **Local accounts (username/password).** The floor — MikroView will
   not serve anything until one exists. Simplest to set up, but account data — usernames and
   roles, including who's the admin — lives in a plaintext file on the
   same host MikroView runs on. There's no separation between "the app
   is compromised" and "the account data is readable": a leaked
   backup, a misconfigured mount, or anything that exposes that one
   file hands over who to target, and local login security then
   depends entirely on password strength.
2. **OIDC/SSO (recommended when available).** The real credential
   lives with your external identity provider (Authentik, Keycloak,
   Entra ID, ...), never on the MikroView host — compromising the host
   doesn't expose anything usable against an SSO-provisioned account.
   Local and SSO accounts can coexist.
3. **Off-box database backend (Postgres, optional).** Moving persisted
   state to a separately-secured, network-restricted database closes
   the "read one file, get everything" exposure that local accounts
   still have today. See
   [docs/configuration.md](configuration.md#postgres-optional) for
   setup — added in issue #131.

Pick the level that matches your network: local accounts are the floor,
and SSO is worth preferring wherever you have an identity provider
available. Full detail and the reasoning behind each is in
[SECURITY.md](../SECURITY.md#authentication).
