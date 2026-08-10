# Changelog

Notable changes to mikroview. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Removals before 1.0 are wholesale — no compatibility aliases or
stub commands are left behind (see `AGENTS.md`, "Removals are
wholesale"). This file is where they are communicated, so read it before
upgrading.

## [Unreleased]

### Removed

- **The plaintext syslog listeners** (#189): `listen.syslogUdp`,
  `listen.syslogTcp`, `internal/syslog/udp_listener.go`, and the
  plaintext `ListenTCP` entry point in `tcp_listener.go`, along with
  their config keys, env vars (`MIKROVIEW_LISTEN_SYSLOG_UDP`,
  `MIKROVIEW_LISTEN_SYSLOG_TCP`) and CLI flags (`-syslog-udp`,
  `-syslog-tcp`). Wholesale, per `AGENTS.md` -- no alias, no listener
  kept alive only to log a warning. Syslog over TLS (#188) is now
  mikroview's only syslog listener, and was shown to work repeatably
  against a real router first: sustained real traffic over an hour-plus,
  survives a mikroview container restart, and survives a full router
  reboot, all without manual intervention.

  RouterOS's `remote-protocol=tls` requires 7.18 or later, which is now
  the effective minimum RouterOS version for mikroview's whole setup
  guide, not just this listener.

  The syslog TLS listener is also no longer gated behind `tls.enabled`:
  it now loads/generates its own certificate independently, so a
  deployment that disables mikroview's own HTTP TLS (its own reverse
  proxy terminates TLS for real clients) doesn't lose syslog ingest as
  a side effect -- RouterOS connects to the syslog port directly, never
  through that proxy, so it needs a certificate to trust either way.

- **`store.maxEvents`** (#244), replaced by `store.maxMemory`: a memory
  budget (`"120MiB"`, `"500MB"`) rather than a raw event count, along
  with its env var (`MIKROVIEW_STORE_MAX_EVENTS` → `MIKROVIEW_STORE_MAX_MEMORY`)
  and CLI flag (`-max-events` → `-max-memory`). Wholesale, per
  `AGENTS.md` -- no alias, no dual reading of the old key. An event count
  meant something different by four or more orders of magnitude between
  deployments (MikroTik firewall rules do not log by default, so the
  rate mikroview actually sees is set entirely by which rules an operator
  turned logging on for) -- measured on one real instance, the old
  200,000 default held under three minutes against a configured 24h
  retention, invisible anywhere until someone went looking. A memory
  budget is the thing an operator can actually reason about across that
  spread: it's what they set on a container, and mikroview derives the
  rest and reports it back (`GET /api/stats`, and now the live view's
  toolbar directly). See `docs/configuration.md`'s "How events are
  stored" and CFG-0011/CFG-0012 for the full reasoning.

- **The Control Ports tab and `GET /api/critical-ports`** (#243),
  replaced wholesale by the watchlist (see Added below). The
  `flags.criticalPorts` config key is unchanged and still feeds the
  unrelated `critical_port` behavioral detector -- only the HTTP
  endpoint that re-exposed that same list for the now-deleted tab, and
  the tab itself, are gone. No alias, no dual UI, per `AGENTS.md`.

### Added

- **The watchlist**, replacing the old Control Ports tab (#243): a
  persisted, admin-managed entry set (`internal/watchlist`) with two
  matching modes -- **record**, "watch attempts against these ports,"
  generalising Control Ports' client-side-only logic to run server-side
  against every ingested event; and **invert**, "this device should only
  ever reach these destinations," which starts a new entry in an
  observe state (nothing fires, candidate destinations are recorded for
  review), lets the operator promote what's expected, and only then
  starts treating anything else as a real match. Matches are persisted
  (`internal/matchlog`, append-only, fsync per write) so they survive
  both the in-memory event ring wrapping and a mikroview restart --
  unlike Control Ports, which only ever saw whatever was still in the
  browser's own capped, volatile event buffer. Matching runs on an async
  evaluation worker mirroring `internal/detect.Detector`'s own
  queue/drop/recover pattern, so a slow or backed-up match evaluation
  never delays store insertion or WebSocket broadcast on the single
  ingest goroutine -- the same failure mode issue #221 already
  demonstrated on the equivalent detection path.

  Managed end to end: an HTTP API (entry CRUD, promote, observe toggle,
  and a windowed match query reachable via a read-only API token) and an
  admin-only **Menu → Watchlist** page for creating entries, reviewing
  an inverted entry's observed candidates, promoting them, and viewing
  an entry's recent matches inline.

  New config: `watchlist.storePath`, `watchlist.matchLogPath`,
  `watchlist.matchLogCapacity` (CFG-0040/CFG-0041), see
  `docs/configuration.md`'s "Watchlist" section.

- **Watchlist entries suggested from pushed router data** (#243 slice
  5): named DHCP leases and ports an existing firewall rule already
  drops or rejects are generated into `internal/suggest`'s candidate
  pool automatically in the background, so there's something to react
  to rather than a blank page. Every candidate is one of three states,
  never a plain accept/reject -- **undecided** (the default),
  **accepted** (a real watchlist entry now exists for it), or
  **hidden** (declined, reversible only by deliberately undoing it from
  the Hidden view; it never reappears on its own). Accepting a device
  suggestion always starts observing with nothing pre-approved, the
  watchlist's own safe default. An accepted suggestion whose original
  reason later disappears (the rule changed, the lease expired) is
  flagged **stale** with a hard-to-miss highlight rather than silently
  reverted. A confirm-gated **"reset everything"** wipes the whole
  watchlist and regenerates suggestions from scratch.

  New admin-only **Menu → Suggestions** page and
  `GET/POST /api/suggestions/...` API. New config:
  `watchlist.suggestionsStorePath`, see `docs/configuration.md`'s
  "Suggested watchlist entries" section.

  `docs/routeros-setup.md` gained the two push-script blocks
  (`dhcp-lease`, `arp`) this feature's device suggestions actually
  depend on -- previously only documented as a reference table row,
  never given as code to paste in, so following the setup guide as
  written produced zero device suggestions no matter how the feature
  itself was configured. Verified against a real RouterOS 7.23.3
  router, including one real surprise: a lease with no client-reported
  hostname yet serializes `host-name` as JSON `null` rather than
  omitting or erroring on it, which MikroView already handles the same
  way it handles an unset `dst-port`.

- **Tor and VPN network-class matches now reinforce an already-raised
  flag's confidence** (#114), direction-aware: only a classified source
  reaching *into* your network counts, never your own outbound traffic
  through a VPN or Apple Private Relay. Datacenter and privacy-relay
  matches stay display-only, as measured in #114's research: the broad
  datacenter/cloud lists alone cover more than one in ten routable IPv4
  addresses, so scoring on them would fire constantly on ordinary
  traffic (Google Public DNS, Akamai edge, every Apple Private Relay
  user). A match never raises a flag by itself, only reinforces one a
  behavioral detector already raised — the same floor-only contract
  every other reputation signal in mikroview follows. Whitelisting a
  trusted VPN/VPS uses the existing flag-exclusion tools; no new
  suppression mechanism was added.

- **New `netClass` source: `apple_private_relay`**, Apple's official
  Private Relay egress range list, on by default alongside `x4b_vpn`.
  Not a cosmetic addition -- X4BNet's VPN feed pulls the same Apple
  ranges in via their own build pipeline, so without an authoritative
  source of the same ranges taking priority, an iPhone or Mac's ordinary
  Private Relay traffic would classify as a "known VPN exit," which
  #114's research called out as the single false positive that mattered
  most for a home-network product.

- **`POST /api/ingest/routeros`** (#186 step 3), the RouterOS push-ingest
  endpoint. Reachable only with an ingest token (#186 step 1) -- there is
  no session-based path to it at all, structurally the same "separate,
  minimal mux" guarantee the existing read-only API tokens have, just
  pointed the other way: an ingest token can reach nothing else, and
  nothing else can reach this. Payloads are strictly validated
  (`internal/ingest`, #186 step 2) and rejected whole on any problem,
  rate-limited per token (120 requests/15 minutes, sized around one
  multi-page, multi-kind push rather than a single request), and every
  accepted push is audited by device, recording only its shape (kind,
  page, record count) -- never the pushed content itself.

- **Pushed router state is applied** (#186 step 4): host names and the
  rule/NAT tables. A validated push lands in a new in-memory-only store
  (`internal/routerstate`), keyed by the ingest token's own device --
  never by anything the payload claims about itself.

  **Host names**: a DNS static entry, DHCP lease hostname, or WireGuard
  peer comment pushed by the router now names that address everywhere
  mikroview displays one. RouterOS always wins -- a router-pushed name
  out-ranks a UI-set entity label and the config map for the same
  address, so names in mikroview always match RouterOS with no drift and
  no reconciling. The mikroview-side label isn't destroyed, just
  shadowed: manage router-known hosts in RouterOS, and hand-made labels
  for anything the router doesn't name simply persist (and resurface if
  the router stops naming a host).

  **Rule and NAT tables**: `GET /api/routeros/{device}/rules` and
  `.../nat` serve the pushed tables in RouterOS's own display order,
  reading only mikroview's local store -- nothing ever contacts the
  router. Event-to-rule resolution goes through the operator's
  `log-prefix` only (the ordinal never appears in a log line), and a
  shared prefix honestly returns every matching rule rather than
  guessing at one.

  In the UI, event rows grow a lookup button beside the rule and NAT
  cells -- the same shape as the IP/port investigate buttons. The rule
  popover shows every pushed rule carrying that row's log-prefix, with
  its RouterOS ordinal ("go look at rule 7 in RouterOS"), comment,
  chain, action and src-address-list; the NAT popover shows the whole
  numbered NAT table, since a log line carries the translation result,
  never which rule performed it. Three states are kept honestly
  distinct: no table pushed by this device yet, a table with no rule
  carrying this prefix, and the matches themselves.

  **Structurally incapable of affecting detection**: the store is
  in-memory only (a restart costs one push interval of enrichment,
  nothing more -- and there is nothing to redact from a backup because
  it is never in one), and a build-failing test forbids any import edge
  between it and the flags/detect machinery, in both directions. Pushed
  data can name things and fill tables; it cannot raise, lower, clear
  or suppress a suspicion signal.

- **Syslog over TLS** (#188), `listen.syslogTls` (default `:6514`, RFC
  5425's port), accepting RouterOS's `remote-protocol=tls`. It presents
  the same certificate the HTTPS listener already uses -- the router
  already imports mikroview's generated CA to verify HTTPS ingest, so
  this is that same trust step, not a second one -- and is only started
  while `tls.enabled` is true, since there's no certificate to present
  otherwise. Implementation-wise it's a `tls.Listener` wrapped around the
  same `ServeTCP` the plaintext TCP listener already uses, so the
  connection cap, per-source cap, idle timeout, and RouterOS's
  no-newline framing (#202) all apply unchanged.

  This buys confidentiality for log traffic on the wire and mikroview
  authenticating itself to the router. It does **not** authenticate the
  sender: RouterOS's logging action has no client-certificate option
  (verified against a real router -- see
  `docs/decisions/routeros-ingest-spike.md`), so anything able to reach
  the port can still connect and inject log lines, exactly as with the
  plaintext `syslogUdp`/`syslogTcp` listeners, which stay on alongside
  this one (their removal is #189, deliberately deferred until this
  ships and is proven against a real router).

- **A live abuse-check button on flag cards** (#213). Raw events aren't
  persisted, so an old or cleared flag often has nothing left in the
  live view to click into — this reuses the existing
  `IpInvestigateButton`/`IpLookupPopover` path wholesale (no new
  backend, already cached and rate-limited) to answer "what does this
  IP look like now" without leaving the page. Additive only: the
  frozen reputation snapshot captured at raise time is unchanged, and
  still answers "what did it look like when it fired". Shown for every
  flag type with a real IP-shaped target; `device_silence` is
  explicitly excluded even though an auto-discovered device's ID can
  itself be IP-shaped, since that flag identifies the device that went
  quiet, not a source worth threat-checking.

### Changed

- **Selectable 1/2/3-column layout for the Flags page** (#199),
  persisted per browser. 2 and 3 columns switch to a compact card
  variant — a truncated one-line detail, the split Clear button flowing
  full-width below the content instead of floating in a fixed-width
  corner — rather than forking the card markup per density. Below the
  shared 700px mobile breakpoint the grid collapses to 1 column and
  cards revert to their full, non-compact detail regardless of the
  stored setting, which itself is left untouched by the floor.

- **"Clear all" and a split Clear button on the Flags page** (#198). A
  new Clear all button above the active list clears every active flag in
  one request — click-again red "Confirm" is the safeguard against an
  accidental single click, not a modal — via a new
  `POST /api/flags/clear-all` endpoint, one audit entry per call rather
  than one per flag. Regular clears only; it can never create a
  permanent exclusion. Each flag's own Clear button is now a split
  control: the main segment is unchanged, and its arrow segment (admin
  only) opens "Permanently clear" (renamed from "Clear, never flag
  again"), keyboard-accessible (focus, Enter to open, Tab to the item,
  Escape to close).

- **Permanent exclusions moved to their own page** (#207), reachable
  from the menu alongside Detectors/Entities/Audit log. Reaching and
  reviewing exclusions underneath a potentially large active-flags list
  was a pain; the new Exclusions page adds a count and a filter by
  detector type and target. A pointer stays where the section used to be
  on the Flags page. No backend changes -- it's a client-side filter
  over the existing `GET /api/flags/exclusions` /
  `DELETE /api/flags/exclusions/{id}` endpoints.

- **Appearance is a standalone toolbar control again, and Export moved
  into the menu** (#137). #73's inline-vs-menu split filed theme and
  colorway switching under "everything else" and buried it two clicks
  deep, while Export — an occasional, deliberate action — held an inline
  toolbar slot. The two are now where that split should have put them:
  Appearance one click away on every view at both breakpoints (a bottom
  sheet at phone widths, same as the menu), and Export in the menu on
  desktop as it already was on mobile.

### Fixed

- **Autoscroll off now holds the live view still** (#232), rather than
  only skipping the jump-to-bottom. The rendered window is a sliding
  slice of the most recent 800 events, so once the buffer passed that
  cap rows kept falling off the top as new ones arrived regardless of
  the toggle -- the page scrolled itself out from under you, which is
  what the toggle was supposed to prevent. Switching Autoscroll off now
  freezes the event pool, and filters still narrow and widen within what
  was frozen, so an event arriving after the freeze can never appear no
  matter what you do to the filter. Distinct from Pause, which also
  halts the age-based display cutoff and detection bookkeeping; this
  only stops what is on screen from moving.

  The freeze survives navigating to another view and back, and the
  Control Ports table -- which has no Autoscroll control of its own --
  is no longer frozen by the live view's toggle.

- **Network-class attribution silently favored the wrong source on an
  exact-prefix collision between two feeds** (found while implementing
  #114's remaining scope). `buildTable`'s own doc comment claimed
  iterating sources in priority order made the higher-priority source
  win a tie; `bart.Table.Insert` is actually last-write-wins on an exact
  prefix, so the *lower*-priority source (whichever was iterated last)
  silently overwrote the higher-priority one instead. Caught by a new
  test reproducing the concrete case this mattered for: with
  `apple_private_relay` and `x4b_vpn` both covering the same prefixes,
  attribution was resolving to "VPN" instead of the intended,
  higher-priority "Private Relay". Fixed by checking for an existing
  exact-prefix entry before inserting.

- **Syslog over TCP ingested nothing from a RouterOS router** (#202).
  RouterOS sends each message as a bare payload with no trailing newline
  and no octet count. The listener read with a `bufio.Scanner` on the
  default line split, so it waited for a delimiter that never arrived:
  the connection was accepted, held, and silently discarded. Measured
  against a real router, `remote-protocol=tcp` delivered **0** events
  where UDP delivered 3 of the same messages — and nothing was logged, so
  it read as "no traffic".

  Framing is now one read per message, or several if that read contains
  newlines, so a conventional syslog sender that does terminate its lines
  keeps working unchanged.

- **Duplicate events in the client buffer** (#183). The initial
  `GET /api/events` fetch and the WebSocket stream overlap, so an event
  arriving in both was appended twice. `LiveTable`'s keyed `{#each}` then
  had duplicate keys — Svelte logged `each_key_duplicate` and keyed-each
  behaviour is undefined from there, so the row rendered twice and any
  count taken off the buffer was inflated. All three insert paths (the
  initial fetch, the live flush, and the pause-resume splice) now dedupe.

### Added

- **API tokens now have a kind, and ingest tokens are scoped to one
  device** (#186). Alongside the existing read-only API token (#101)
  there is now an ingest token, which the RouterOS push integration will
  use. The two are not interchangeable in either direction: an ingest
  token is refused everywhere a read-only token is accepted, and vice
  versa, because `Authenticate` requires its caller to state the kind it
  expects rather than returning a token for the caller to inspect.

  This matters because an ingest token lives in a script on a router,
  where any RouterOS user holding the `read` policy can print it. Without
  the separation, that value would become a read-everything credential
  for every event, flag, stat and device mikroview holds.

  An ingest token must name the device it is issued for, and only an
  ingest token may. One token per router means a compromised router
  cannot report state for any other.

  `POST /api/tokens` takes optional `kind` (`api` or `ingest`) and
  `device`. Omitting `kind` still issues a read-only API token, so
  existing callers are unchanged and the less privileged option stays the
  default.

  A token whose kind this build does not recognise cannot authenticate at
  all, but stays listed and revocable — guessing that an unknown kind
  meant the read-everything one is the wrong direction to guess in.

### Changed

- **Rule-regex filtering runs in a Web Worker** (#157), with a hard
  timeout that terminates the Worker if a pattern overruns. The main
  thread no longer executes a user-supplied regular expression at all, so
  no pattern can hang the tab — including shapes nobody has anticipated,
  which a structural screen can never cover. It replaces
  `isSafeRulePattern`, which rejected the known catastrophic-backtracking
  shapes but, as its own comment said, could not prove an accepted
  pattern was fast.

  It is also less work: the old path ran the pattern against the rule
  label *and* the raw line for every event on every recomputation — up to
  10,000 regex executions across a 5,000-event buffer, repeated per
  top-talker widget. Filtering is now a set lookup.

  A pattern that is invalid, or refused for overrunning, leaves the rule
  filter inactive and says so on the regex toggle rather than looking
  like "no matches".

### Added

- **`-backup` and `-restore`** (#97), producing a single gzipped JSON
  document keyed by store name. Not tar: there are no filenames in the
  format, so path traversal is impossible by construction rather than
  defended against. It carries everything, including accounts and
  recovery-key digests — a backup missing credentials cannot restore a
  working system, and disaster recovery is the wrong moment to find that
  out. Protection is on the output instead: mode 0600, `O_EXCL` at
  creation, and a refusal to write into a world-readable directory
  without `--force`.

  JSON deployments only. On Postgres both commands refuse and point at
  your database's own tooling, which is the expectation that came with
  choosing Postgres.

- **Recovery-key digests now follow the accounts into Postgres**, while
  the pepper stays a local file on the mikroview host. Previously the
  digests stayed in a local JSON file *next to the pepper* even on a
  Postgres deployment — the single-host arrangement, on the one
  deployment that chose Postgres specifically to avoid it.

### Fixed

- **`-transfer-admin` and `-recover-admin-account` work on Postgres
  deployments.** They previously refused outright, which left a Postgres
  deployment with no way to transfer or recover admin at all: neither
  operation can go through the web UI (a compromised session must not be
  able to grant itself admin) nor through an identity provider (an IdP
  account is a login, not an authorisation to escalate). The CLI is the
  only route, so it has to work in every deployment shape.

### Removed

- **`callerIsAdminOrOpen`'s "no accounts yet" bypass**, which treated an
  anonymous caller as an admin on the detector-settings, flags-exclusion
  and config-problems endpoints. It dated from when mikroview could run
  with authentication switched off. Unreachable since that mode was
  removed — `requireAuth` refuses those paths before they route — but it
  read as "anonymous callers are admins under some condition" and would
  have gone live again the moment `requireAuth` was loosened. There is
  now one admin check, `callerIsAdmin`, with no bypass.

- **The option to run mikroview without authentication.** The first-run
  screen offered "No Authentication" alongside creating an admin
  account. An unauthenticated mikroview publishes which hosts are being
  scanned, which rules fire, which ports are under pressure, and which
  accounts exist — a reconnaissance map of the network it is meant to be
  watching. Creating a local account is one form, and it is now the
  floor.

  Gone with it: `POST /api/auth/skip`, the `authDisabled` field on
  `GET /api/auth/session`, and the frontend's skip flow.

  **If your deployment had authentication disabled:** it now starts in
  the create-account state and serves nothing else until you complete
  it. Nothing is lost — there were no accounts to lose. Open the web
  interface and create the admin account. The `"disabled": true` key in
  your accounts file is ignored and disappears the next time the file is
  written.

- **`-enable-auth-setup`.** It re-armed the first-run screen after a
  deployment had skipped authentication. There is no skipping, so there
  is nothing to re-arm.

- **`-reset-password`.** Superseded some time ago by
  `-recover-admin-account`, which is narrower (the admin account only)
  and requires a recovery key. It had been left as a stub that printed
  where to go instead; that stub is now gone too. Use
  `mikroview -recover-admin-account`.

- **Legacy on-disk format fallbacks.** `auth`, `entities` and `flags`
  each accepted a bare JSON array as well as their current object shape,
  for files written by builds that predate those shapes. `auth` also
  carried `migrateHasLocalPassword`, which filled in a missing
  `hasLocalPassword` by inferring it from whether the account had a
  linked SSO identity — the field was a `*bool` purely so that "absent"
  could be told from "false".

  All four are gone, and `User.HasLocalPassword` is a plain `bool` now
  that absence has no meaning. Files written by any current build are
  unaffected; a file in one of the old shapes will fail to load and
  mikroview will refuse to start, naming the path.

### Changed

- `internal/api`'s `Routes` gained an inner `mux`, so tests that
  exercise a handler rather than the authentication gate can mount the
  API directly. They previously got an ungated API by standing the
  fixture up with authentication disabled, which is no longer a state
  that exists.
