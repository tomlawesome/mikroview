# Changelog

Notable changes to mikroview. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Removals before 1.0 are wholesale — no compatibility aliases or
stub commands are left behind (see `AGENTS.md`, "Removals are
wholesale"). This file is where they are communicated, so read it before
upgrading.

## [Unreleased]

### Fixed

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
