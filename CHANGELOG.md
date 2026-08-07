# Changelog

Notable changes to mikroview. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Removals before 1.0 are wholesale — no compatibility aliases or
stub commands are left behind (see `AGENTS.md`, "Removals are
wholesale"). This file is where they are communicated, so read it before
upgrading.

## [Unreleased]

### Added

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
