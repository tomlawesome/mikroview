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

### Changed

- `internal/api`'s `Routes` gained an inner `mux`, so tests that
  exercise a handler rather than the authentication gate can mount the
  API directly. They previously got an ungated API by standing the
  fixture up with authentication disabled, which is no longer a state
  that exists.
