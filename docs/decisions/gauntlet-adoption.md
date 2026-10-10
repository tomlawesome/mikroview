# Authentication moves onto gauntlet: what is replaced, what carries over, what changes

**Status:** proposed design, 2026-10-10 (issue #1202, milestone v0.7.0;
owner opened the work the same day: "Gauntlet has reached v0.4.1 with no
issues left outstanding; it's ready to adopt"). The owner answered the
ten design questions on 2026-10-10, and the three points left open the
same day (rulings 11-13); the rulings are at the end and the design
below states them. The build follows this record.
**Applies to:** `internal/auth`, `internal/oidc`,
`internal/api/{auth,oidc,tokens,passkey,webauthn}.go`, `main.go`'s auth
wiring and CLI commands, `internal/persist` (one interface method),
`frontend/src/lib/{api,auth.svelte}.ts` and the sign-in components.
**Relates to:** gauntlet ADR-0001 (why the module exists), ADR-0002
(compatibility promise), ADR-0003 (setup code), ADR-0005 (encryption at
rest), ADR-0010 (several admins), ADR-0015 (every admin holds a
passkey); here `upgrade-framework.md`, `event-retention.md` (#853 rule
6 and the drop-list addendum, both ended: rulings 2 and 13),
`multi-tenant-oidc.md`,
`postgres-backend.md`.

## Context

Gauntlet (`github.com/tomlawesome/gauntlet`, Apache-2.0) began on
2026-09-26 as a port of this project's `internal/auth`, `internal/oidc`
and the three auth handler files, read against them at every slice so
that this move would be "a swap" (`internal/auth/store.go:35` already
names its constructor after gauntlet's). Since then it has grown past
what mikroview has: lockout and address bans, a sign-in history, unusual
sign-in handling, several admins, a passkey rule for admins, a common
password list, held first-factor enrolment, API token expiry, RFC 9457
error bodies and an OpenAPI description of every route.

Acceptance on #1202: behaviour identical for existing accounts, sessions
and OIDC logins, proven by a migration test against a copy of real
stored data. "Identical" cannot be literal -- gauntlet's own rules are
not switchable in several places -- so this record lists every
difference a person would notice, split into what gauntlet forces and
what mikroview chooses, with the owner's ruling for each choice.

Facts the design rests on (checked 2026-10-10 against mikroview `dev`
2b2089de and gauntlet v0.4.1):

- The stored shapes are the same. Mikroview's `User` is a strict subset
  of gauntlet's `User` with identical JSON names; `Passkey`,
  `PasskeyFlags`, `RecoveryCode` and `Token` match field for field; the
  password hash format (`argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>`),
  the TOTP secret encoding (base32, in the clear), the recovery code
  alphabet and Argon2id hashing, the reset code shape and the 32-hex ID
  format are all the same. Mikroview's accounts document `{"users":
  [...]}` has no version and gauntlet reads it as version 1; the bare
  token list reads as tokens version 1. Gauntlet writes accounts as
  version 10 and tokens as `{"version":3,"seq":n,"tokens":[...]}` on its
  first save.
- `persist.Backend` is the same four methods. Gauntlet decides whether a
  backend is encrypted through an optional interface,
  `persist.AtRest{ProtectedAtRest() bool}`; a backend without it counts
  as plaintext and `OpenStore` refuses it unless
  `Options.AllowPlaintextAtRest` is set. Mikroview never sets it
  (ruling 2).
- Mikroview's own encrypted file backend binds each file to its path and
  derives the key with the same construction gauntlet's `persist/seal.go`
  copied; the existing encrypted files open either way.
- The HTTP routes mikroview's frontend calls (29 routes) all exist in
  gauntlet's gate under the same paths and the same success bodies, with
  four exceptions listed under "HTTP contract" below. Gauntlet has 17
  further routes mikroview does not call.
- Mikroview's frontend error handling depends on two exact plain-text
  bodies (`incorrect password`, `sign in again`) and one header name
  (`X-Mikroview-Auth-Gate`). Gauntlet returns `application/problem+json`
  and `X-Auth-Gate`.

## Decisions

### 1. What gauntlet replaces, what stays

Replaced, and deleted from this repository (about 13,500 lines with
tests):

- `internal/auth` -- except `recovery.go`, the CLI recovery-key store
  (see decision 5) -- becomes `import "github.com/tomlawesome/gauntlet"`.
- `internal/oidc` becomes `gauntlet/oidc`. Mikroview's `Policy` fields
  (`AllowedGroups`, `GroupsClaim`, `AllowedEmails`, `AllowedEmailDomains`,
  `RequiredClaims`) exist there under the same names;
  `AllowIssuer`/`IsMultiTenantIssuer` too, so `multi-tenant-oidc.md`
  stands unchanged.
- `internal/api/auth.go`, `oidc.go`, `tokens.go`, `passkey.go`,
  `webauthn.go` become `gate.Routes()` and `gate.Protect()`, with
  `gauntlet/passkey` as the relying party. This is done in one step, not
  the two gauntlet's design §3 once sketched (handlers first over the
  new store, routes later): the store API has moved since the port
  (held enrolments, lockout reservations, step-up), so keeping the
  handlers means re-fitting 3,300 lines that are then deleted, and
  gauntlet ADR-0002 decision 5 planned exactly one frontend migration --
  onto Problem Details -- not two.

Stays mikroview's, by gauntlet's design (its README: storage, logging,
the clock) and by this project's needs:

- **Storage backends**: `internal/persist` (file, encrypted file,
  Postgres, write-behind, schema stamp and migrations). Nothing moves to
  `gauntlet/persist` except two imports: `gpersist.ErrConflict` becomes
  the value of mikroview's `persist.ErrConflict` (stores compare with
  `errors.Is`), and `gpersist.Encrypt` wraps the Postgres row (decision
  3). Mikroview's `EncryptedFileBackend` gains `ProtectedAtRest() bool`
  returning true; the write-behind wrapper forwards its inner backend's
  answer; the plain file and Postgres backends get none, so gauntlet
  sees them as plaintext, which they are. The auth stores are never
  opened on one of those bare: with no key mikroview does not start
  (decision 2, "Encryption at rest").
- **Logging**: `logging.New("auth")` is a `*slog.Logger`; every gauntlet
  constructor takes one.
- **Audit**: `audit.Store.Record(actor, action, target, detail)` fits
  `gate.Auditor` once its return value is dropped by a one-method
  adapter. Gauntlet's action names (`user.login`, `token.create`, ...)
  replace mikroview's; the audit page shows them as text, so nothing
  else changes.
- **Clock**: `gate.Config.Now` is `time.Now`; tests pass a fixed one.
- **Client address**: mikroview's trusted-proxy and `ClientIPHeader`
  logic becomes `gate.Config.ClientIP`.
- **Country**: `internal/geoip.Manager.Country(ip) (code, ok)` already
  has `gate.Config.Country`'s exact signature; wired when the operator
  has configured a country-data provider, nil otherwise.
- **HTTP wiring**: `internal/api.Server` keeps its mux and its own
  routes; `Routes()` becomes `g.Protect(mux)` with gauntlet's routes
  mounted on the same mux and `g.Handle(kind, subMux)` registered for
  the three bearer kinds (`api`, `ingest`, `droplist-pull`), which keeps
  the "separate minimal router per token kind" structure the docs
  promise. Handlers read the caller with `gate.UserFromContext(r)`.
  Mikroview's own password re-check routes (config editor, history
  settings, vault lock, router-backup switch, device enrolment) move to
  `LoginLimiter.ReserveRecheck/ReleaseRecheck` and
  `Store.PasswordMatches`, keeping their separate budget.
- **Setup-only mode** (`setuponly.go`) is unchanged: it mounts gate's
  routes and the config editor under `Protect` as before. The no-key
  refusal (decision 2) applies to it as to a normal start (ruling 12):
  the key is still found with a broken config file, through the app
  folder's `keys/history.key` or `MIKROVIEW_HISTORY_KEY_FILE`.
- **Frontend**: stays, and changes where the contract does (decision 4).
- **CLI**: `-recover-admin-account`, `-generate-recovery-keys`,
  `-transfer-admin`, `-clear-second-factor` keep their names and
  behaviour over `gauntlet.Store` (`SetPassword`, `TransferAdmin`,
  `ClearAllSecondFactors`, `GenerateRecoveryCodes` all exist). A CLI
  process opens the store with `OnSetupCode`/`OnUnlockCode` handlers
  that do nothing, so it announces no codes of its own (gauntlet
  ADR-0003 decision 6).

### 2. Carrying existing data over: read in place, no copy

**Pattern: forward-compatible read with a versioned write** -- the
document-store form of "expand on read, stamp on write": the new code
reads the old shape unchanged and writes the new shape on its first
save, atomically per document (temp-and-rename for files; a
compare-and-swap row update for Postgres). Gauntlet is built for exactly
this (its design.md §3: "a copy of real data loads without migration").
No mikroview code reads or writes the auth document format; gauntlet
ADR-0002 forbids it, and there is nothing to convert.

What happens to each kind of data:

| Data | Carry-over |
|---|---|
| Accounts, roles, password hashes | Read as-is; the hash format is identical and verified by the same code. |
| TOTP secrets and counters | Read as-is (`totpSecret`, `totpConfirmedAt`, `totpLastCounter`). A secret with `totpConfirmedAt` set is active; the new `totpPendingSince` field is read as zero, which gauntlet treats as an expired pending enrolment -- so an enrolment scanned but not confirmed at the moment of upgrade must be started again. |
| Passkeys | Read as-is, same `rpId` binding; stale (old public URL) handling is the same. |
| Recovery codes | Read as-is; an account that has codes has them live (held enrolment applies only to enrolments made after the move). |
| Reset codes | Read as-is; `resetCodeSpentHash` reads empty. |
| OIDC links | `oidcIssuer`/`oidcSubject` read as-is; the `(issuer, subject)` index is rebuilt on open. `hasLocalPassword` is already stored, so SSO-only accounts stay exempt from the second-factor door. |
| Sessions | In memory on both sides; the upgrade's restart signs everyone out exactly as every restart does. Gauntlet revokes a session issued before `max(passwordChangedAt, sessionsEndedAt)`; mikroview documents have no `sessionsEndedAt`, which reads as zero, so the rule is the existing one. |
| API tokens | The bare list reads as tokens version 1. Existing hashes are SHA-256 of the raw value, as gauntlet's; existing raw values (32 hex, no prefix) keep working because the value is hashed as presented. Existing tokens have no `expiresAt` and so never expire -- "what it was issued as" (gauntlet `token.go`). |
| Recovery keys (CLI) | Not gauntlet's; the store moves to `internal/recoverykeys`, same file and pepper, and is now sealed under the key like the rest (ruling 2); an existing plaintext file is sealed on first open. |
| Settings | Config keys keep their names; two gain a silent clamp (decision 6, ruling 3) and one is added (`auth.adminPasskey`, ruling 1). The retention key file (`history.keyFile`) is the key gauntlet's sealing uses, as #853 decided for every other store -- and from v0.7.0 it is required (below). |

**Encryption at rest: a key is required (ruling 2).** From v0.7.0
mikroview refuses to start when no `history.keyFile` is mounted.
Gauntlet's `OpenStore` refuses to hold account data (TOTP secrets
included) in the clear, and mikroview will not set
`AllowPlaintextAtRest`. This ends the #853 rule 6 exception for the
auth stores: accounts, tokens, recovery-key digests and sign-in history
are all sealed under the key, on both the file and Postgres backends.
The exception list in `storage.go` (`plaintextWithoutKeyStores`) is
deleted outright, with `plaintextStoreNames`, `plaintextWithoutKeyList`
and the no-key log line that names them (ruling 13). The drop list was
on it only so its entries survived a restart on a keyless install; no
install runs keyless now, so with a key it is sealed like every other
store. `handleDroplistPull`'s empty-feed guard stays: an operator can
still set `droplist.storePath` to `""`.

- **Refusal.** Start-up stops with its own CFG error code and a message
  that says how to mount a key (generate `mikroview/keys/history.key`,
  or point `history.keyFile` at one). The CLI commands that open the
  store (`-recover-admin-account`, `-transfer-admin`,
  `-clear-second-factor`, `-generate-recovery-keys`) give the same
  refusal.
- **Existing plaintext data is sealed in place on first open**
  (`MigratePlaintext`), so an install that has run keyless upgrades
  without a manual step once a key is mounted. Postgres: each auth
  store's row is wrapped in `persist.Encrypt(..., MigratePlaintext:
  true)` under its own label, the store's name (`auth`, `tokens`,
  `recovery_keys`, `signins`), so a row copied into another store's
  place fails to open rather than reading as that store (gauntlet §3,
  ADR-0005); the column stays `text` because the envelope is JSON text
  (`{"sealed": "<base64>"}`). This is the one exception to
  `event-retention.md`'s "Postgres is unaffected": the other stores stay
  plaintext on Postgres, as decided there. File backend (ruling 11):
  mikroview's own `EncryptedFileBackend` gains a seal-in-place option,
  set for the auth stores: a file without the sealed header that parses
  as JSON is read as plaintext, sealed under the key with the file's
  path bound as today, and saved back atomically under the version it
  was read at. Anything else -- a damaged file, a wrong key -- fails to
  open as today, never sealed and never read as an empty store. Files it
  already sealed open unchanged, byte for byte. Gauntlet's
  `persist.Encrypt` is not used for files: it binds its label rather
  than the path and writes the text envelope, so it cannot open files
  mikroview sealed before, and its own `EncryptedFileBackend` never
  migrates plaintext. Gauntlet is not changed.
- **Shipped files.** `deploy/docker-compose.yml` and the install docs
  ship with a key mounted (today the compose file leaves history
  memory-only until the operator adds one).
- **Docs.** The CHANGELOG lists this under breaking changes.
  `docs/configuration.md`'s rule 6 passages are rewritten (#1441,
  configuration.md rule 6 wording).

**Downgrade.** Pre-1.0, downgrade refuses, per `upgrade-framework.md`;
this move is the first change that makes the refusal load-bearing for
the accounts data, because an older build reads some of gauntlet's
output *wrongly* rather than not at all: it would parse the version-10
accounts document (unknown keys ignored) and save it back without the
new fields; it cannot parse the tokens object at all and would refuse to
boot; and it would read a sealed Postgres row as an accounts document
with no users -- an empty store, setup code and all (gauntlet ADR-0005
consequences). So the upgrade adds **schema migration 4, "accounts and
tokens on gauntlet"**, a stamp with no data change, on both backends:
the existing downgrade guard (`SchemaTooNewError`) then refuses a v0.6.x
build before it opens anything. Rolling back means restoring the copy
taken before the upgrade, as `docs/upgrades.md` already tells operators;
no automatic pre-upgrade copy is made (`upgrade-framework.md` dropped
that on purpose).

Deviation from the stricter pattern (an all-or-nothing import with a
start record and rollback), stated: there is no import step to record
or roll back, because the document is the same document; the atomic
unit is one save, the stamp is the start record, and the downgrade
guard is the rollback refusal. An import step would exist only to copy
a document into itself.

**Proof.** `TestUpgradeFixtures` and `TestUpgradeFixturesPostgres`
(`internal/persist/upgrade_*_test.go`), which open every recorded
release's real data directory, gain the gauntlet open path: accounts
through `gauntlet.OpenStore`, tokens through `gauntlet.OpenTokenStore`,
under a test key (a plaintext recording is sealed on that first open),
password checks for the recorded admin and viewer, the enrolled
authenticator app reported active on the versions that recorded one,
the recorded token present, a save and re-open proving the version-10
document round-trips, and the schema at 4. The recordings are this
project's "copy of real stored data" by `upgrade-framework.md`'s
decision: data written by released builds, with placeholder credentials,
never committed. A check against the owner's own install is a manual
step in the build notes, run on a copy under `~/projects/.backups/`.

### 3. Store wiring

- Accounts: `gauntlet.OpenStore(backend, gauntlet.Options{Log,
  ProductName: "MikroView", OnSetupCode, OnUnlockCode,
  PasswordBlocklist})` over a sealed backend (decision 2);
  `AllowPlaintextAtRest` is never set. `OnSetupCode`
  logs the setup code in mikroview's own wording -- the line
  `scripts/setup-code.sh`, `live-setup-code.mjs` and
  `docs/configuration.md` already match; gauntlet's default wording is
  the same text today, but the handler makes that mikroview's to keep.
  `BreachCheck` is nil: no outbound call to Have I Been Pwned (ruling 7).
- Password list (ruling 7): `PasswordBlocklist` is a
  `*blocklist.Refresher` (`blocklist.NewRefresher(blocklist.RefreshConfig{
  Dir, Log})`, `Run(ctx)` in a goroutine, stopped at shutdown), per
  gauntlet ADR-0007. It starts from the signed top-10,000 list built
  into the release, or the newer copy kept in `Dir` (a new directory
  under the data directory; the list is public, so it is not sealed).
  Once at start and then daily (plus or minus a tenth), it fetches the
  checksum from gauntlet's public GitHub release
  (`blocklist.DefaultURL`, `.sha256` first -- an unchanged list costs
  one small request), then the list (about 410 KB) and its signature,
  and adopts the list only if the checksum and the signature (against a
  key compiled into the release) both verify and it is newer. Any
  failure logs one Warn and leaves the list in use as it was, so an
  install with no outbound access keeps the embedded list. This is a
  new outbound fetch; `docs/configuration.md` documents what is fetched,
  from where and how often (build item, commit 7).
- Tokens: `gauntlet.OpenTokenStore(backend, gauntlet.TokenOptions{Log,
  Kinds: {TokenKindAPI, TokenKindIngest, TokenKind("droplist-pull")}})`.
  Without `droplist-pull` in `Kinds` the existing droplist tokens would
  be kept but never authenticate.
- Sessions: `gauntlet.NewSessionStore(idle, ceiling)` with the clamped
  config values: anything above gauntlet's 1h idle / 24h total is cut
  down to those limits, silently -- no start-up warning (ruling 3); the
  CHANGELOG records it. Documented defaults become 1h / 24h.
- Limiter: `gauntlet.NewLoginLimiter(5, 5*time.Minute)`, the existing
  constants; gauntlet adds lockout, disable and address-ban behaviour on
  top (decision 6).
- Sign-in history: `gauntlet.OpenSignInHistory` over a fourth store
  named `signins` (file `signins.json`), sealed under the key like the
  other auth stores, and passed as `Deps.SignIns`, closed at shutdown.
  Recorded from v0.7.0; the page is #1442 (sign-in history page), not
  in v0.7.0 (ruling 8).
- Gate: `gate.New(gate.Config{CookieName: "mikroview_session",
  SecureCookie, CSRFHeaderValue: "mikroview", ProductName: "MikroView",
  LoginPath: "/", ClientIP, Country (ruling 6), AdminPasskey (ruling 1),
  UnusualSignIns: {} (flag), Log, Audit, Now}, gate.Deps{Users,
  Sessions, Tokens, Limiter, OIDC, OIDCState, OIDCPolicy, Passkeys,
  SignIns})`. `Notices` stays nil: mikroview sends no mail. `Passkeys`
  is `passkey.New(passkey.Config{PublicURL: cfg.PublicURL, DisplayName:
  "MikroView"})` when `publicUrl` validates, nil otherwise; the existing
  start-up refusal when accounts hold passkeys but the relying party is
  not ready stays, computed from `Store.AnyPasskeysExist()`.
  `PasskeySignIn` is false (ruling 9, #1443).
- `g.SweepTokens(ctx, now)` runs once a day from `main.go` (ruling 5).

### 4. HTTP contract: what the frontend must change

Every route the frontend calls keeps its path, method and success body.
Four things change on the wire, and the frontend changes with them in
the same branch:

1. **Error bodies** are RFC 9457 Problem Details
   (`application/problem+json`: `type`, `title`, `status`, `detail`).
   `api.ts` reads `detail` for display where it showed the text body,
   and branches on the `type` anchor instead of body text:
   `#invalid-credentials` replaces the exact string `incorrect password`
   (TOTP enrol, passkey begin, and the other password re-checks) and
   `#step-expired` replaces `sign in again` (login/factor). Gauntlet's
   `docs/api/errors.md` says to key on `type`, never on `detail`.
   Mikroview's own routes keep their plain-text errors; setup-only
   mode's `{"error": ...}` is untouched.
2. **The door header** is `X-Auth-Gate` (was `X-Mikroview-Auth-Gate`),
   with a third value `must-enrol-passkey` beside `must-change-password`
   and `must-enrol-factor`. `GET /api/auth/session` gains
   `mustEnrolPasskey` and `adminPasskeyRequired`; `auth.svelte.ts` maps
   it to a new view state that reuses the enrol-factor screen with
   passkey-only wording and no authenticator-app option.
3. **`POST /api/tokens`** takes the admin's own `password` again (ASVS
   7.5.3: a token outlives the session that minted it) and an optional
   `expiresAt` (RFC 3339, or `"never"`; omitted means one year). The
   tokens door, the wizard's mint step and the blocklist key flow prompt
   for the password; ingest tokens send `"expiresAt": "never"` (a
   router's token must not stop its reporting on an anniversary --
   gauntlet's own comment); API tokens omit it, so they expire after one
   year (ruling 4). New raw values
   carry the `gnt_` prefix.
4. **Recovery codes are confirmed.** The first second factor an account
   adds is held, with its ten codes, until `POST
   /api/auth/recovery-codes/confirm` says the person saved them; the
   enrol response carries `pendingConfirmation: true`, and the factor is
   not live (and the must-enrol-factor door stays shut) until then; an
   unconfirmed enrolment expires after ten minutes. The codes screen
   gains a "I have saved these" step that calls confirm. The live-check
   helper `enrolFactorAndSignIn` and the fixture recorder's
   `enrol_second_factor_if_required` make the same call.

Smaller, also in the branch: the session cookie is `__Host-mikroview_session`
when `secureCookie` is on (the browser never sends it to another host
or port; the frontend does not read it); cookie `Max-Age` becomes the
session ceiling rather than 30 days; `WWW-Authenticate` says
`realm="gate"`; gauntlet's JSON decoding refuses unknown fields, so
requests send only the documented ones; `GET /api/auth/users` rows gain
`heldForPasskey`; `POST /api/auth/users` accepts `role: "admin"` with a
step-up, which the frontend does not offer in v0.7.0.

Nothing changes for bearer-token clients: routes, kinds and the
`Authorization: Bearer` check are the same, and existing token values
keep working.

### 5. What stays mikroview-specific around auth

- `internal/recoverykeys` (from `internal/auth/recovery.go`): the CLI
  recovery keys and pepper. Gauntlet's design §1.7 names this as
  mikroview's to keep.
- The bearer sub-routers (`readOnlyRoutes`, `ingestRoutes`,
  `droplistPullRoutes`) and the ingest limiter.
- `UIAllow`, trusted proxies, setup-only mode, the WebSocket's
  per-ping session re-check (it calls `Sessions.Validate` as before).
- The start-up warning "no MikroView admin has a local password" and
  the stale-passkey start-up refusal.

### 6. Every behaviour a person would notice

**(a) Forced by gauntlet** -- no switch exists:

- **Sessions end sooner.** Idle limit at most 1 hour and total lifetime
  at most 24 hours (`gate.New` refuses more; NIST SP 800-63B-4 AAL2's
  limits, adopted on gauntlet #51). Today's defaults are 24 hours idle
  and 7 days total; the new defaults are 1 hour and 24 hours, and a
  configured value above either limit is cut down to it without a
  start-up warning (ruling 3; the CHANGELOG says so). Everyone signs in
  at least daily. A session that
  timed out inside the 24 hours can be resumed with the password alone
  (`POST /api/auth/reauthenticate`, `resumable` on the session body) --
  the frontend does not use that in v0.7.0.
- **Lockout, not just rate limiting.** After 5 failures in 5 minutes the
  *account* is locked for 5 minutes, tripling each time up to an hour;
  50 consecutive failures disable sign-in for 24 hours (an admin unlocks
  it, or the lone admin uses a one-time unlock code written to the
  log); 100 failures from one address in 24 hours ban the address for
  24 hours; 5 wrong second-factor codes in a row force a password
  change. Lockouts are saved on the account and survive a restart.
- **New passwords are checked harder.** Refused if on the list
  of the 10,000 most common passwords (the copy built into the release,
  or the newer signed one the daily refresher has fetched), or if they are the username or
  "mikroview" with different case, punctuation or digits around them.
  Existing passwords are not re-checked.
- **An admin re-enters their password to mint a token**; new tokens
  show a `gnt_` prefix.
- **Recovery codes must be confirmed** before the first factor goes live
  (above).
- **Several admins are possible** through the API (`PUT
  /api/auth/users/{id}/role`, `POST /api/auth/users` with
  `role: "admin"`, each needing the caller's password and a current
  second factor). The frontend does not offer it in v0.7.0, so the
  only visible change is in the docs: "MikroView holds exactly one
  admin" becomes "the first account is the admin; an admin can make
  others". `-transfer-admin` keeps working.
- **Unusual sign-ins are flagged.** A sign-in from a browser or country
  the account has not used before is recorded as unusual on the session
  and in the audit log, and the browser is remembered for 45 days (a
  `gate_known_browser` cookie). Nothing is refused and nobody is asked
  for anything.
- **Usernames** refuse invisible formatting characters; the existing
  no-`@` rule is unchanged.
- Error bodies and the header rename (decision 4) -- invisible to
  people, visible to anyone scripting against the API.

**(b) Gauntlet features mikroview chooses**, as the owner ruled (see
"Owner rulings"): the admin passkey rule (1), a required key and every
auth store sealed (2), `sessionTTL` values above the new limits cut down
silently (3), API token expiry (4), the daily token sweep that removes
tokens unused for a year (5), the sign-in country from the existing
geoip data (6), the refreshed password list on and Have I Been Pwned off
(7), the sign-in history recorded (8), passkey-alone sign-in off (9),
SSO group-to-role mapping off (10). Off in v0.7.0 without a ruling: account notices
(mikroview has no mail), unusual-sign-in `confirm`/`prove`/`block` (the
first needs a delivery path mikroview lacks; `block` risks the lone
admin, which gauntlet ADR-0011 mitigates but does not remove).

### 7. Licence and the dependency pin

Gauntlet is Apache-2.0; mikroview is AGPL-3.0-only. Apache-2.0 is
compatible with GPLv3-family licences in this direction (FSF's licence
list; GPLv3 §7 and AGPLv3 §13 permit combining with Apache-2.0 code),
so importing it changes none of mikroview's terms -- which is why the
owner chose Apache-2.0 for the module (gauntlet ADR-0001). Mikroview
adds gauntlet to `THIRD-PARTY-NOTICES.md` and `supply-chain/
licence-policy.yml`, and gauntlet's own notice for the Have I Been
Pwned-derived list travels with the embedded list (gauntlet README:
"an application that embeds gauntlet does not have to repeat it").

The pin is exact: `github.com/tomlawesome/gauntlet v0.4.1`, never a
branch or pseudo-version, bumped by a merge request of its own each
time (gauntlet ADR-0002: "a version-number change and a green pipeline,
nothing else"). The module is fetched directly from GitHub, not through
the public Go module proxy: `GOPRIVATE=github.com/tomlawesome/gauntlet`
(which also sets `GONOSUMDB` and `GONOSUMCHECK` for it) in the
developer shell and in `.gitlab-ci.yml`, because a proxy lookup
publishes and caches the version irrevocably (owner, 2026-10-03). New
indirect modules in `go.sum` from gauntlet: `maxmind/mmdbwriter`,
`oschwald/maxminddb-golang/v2`, `tinylib/msgp`, `philhofer/fwd`,
`go4.org/netipx` -- all behind `gauntlet/geoip` and `gauntlet/blocklist`
build tooling; only the common-password list enters the binary. Go
1.27 on both sides.

### 8. Risks

- **Frontend contract drift** is the main one: four wire changes across
  about a dozen call sites, pinned by the vitest suites named in the
  build notes and by every live-check script's `session()` helper.
- **Held enrolment** silently breaks any script that enrols a factor
  without confirming (the fixture recorder, `live-browser.mjs`); both
  are updated in the branch, and `scripts/record-upgrade-fixture.sh
  --check` on the built image proves the recorder before the tag.
- **`__Host-` cookie** is refused by browsers over plain http on a
  non-localhost host; mikroview already refuses `secureCookie: true`
  without TLS (CFG-0021), so the combination cannot be configured.
- **Lone admin lockout**: 50 wrong passwords disable the only admin for
  24 hours. Gauntlet's answer is the unlock code in the server log, and
  mikroview's `-recover-admin-account` remains. Documented under
  "Recovering the admin account".
- **Sealing in place** is one-way for every auth store, file and
  Postgres alike: `MigratePlaintext` seals on first open; rollback needs
  the copy or `pg_dump` taken before. The schema stamp stops an older
  build from reading a sealed row as "no accounts".
- **Upgrade without a key stops the service.** Any install that has run
  keyless (the compose file's default today) refuses to start on
  v0.7.0 until a key is mounted. The refusal says how; the CHANGELOG
  lists it under breaking changes, `docs/upgrades.md` and the install
  docs lead with it, and the shipped compose file mounts one.
- **New outbound fetch** (the password-list refresher): a locked-down
  install sees a daily Warn and keeps the built-in list. Documented in
  `docs/configuration.md`.
- **go module fetch**: CI must carry the `GOPRIVATE` setting or the
  first pipeline publishes the module to the proxy. The build notes put
  it in the first commit.
- **Session shortening** will read as a regression to anyone used to
  staying signed in for a week. Over-limit settings are cut down with no
  start-up warning (ruling 3), so the CHANGELOG is the only place that
  says why.

### 9. Commits, one branch (`feat/1202-gauntlet`)

1. `persist`: `ErrConflict` aliased to gauntlet's, `ProtectedAtRest` on
   the encrypted and write-behind backends, `go.mod` gains the pin,
   `GOPRIVATE` in CI and `docs/development.md`. Builds; nothing else
   changes.
2. `recoverykeys`: move `internal/auth/recovery.go` and its tests to
   `internal/recoverykeys`; CLI call sites follow. Pure move.
3. `frontend`: accept both contracts (Problem Details or text, either
   door header), send the token password, call recovery-codes confirm
   when `pendingConfirmation` is returned, add the must-enrol-passkey
   state. Vitest suites updated. The old backend still passes.
4. `auth`: the swap. Delete `internal/auth` (minus the moved file),
   `internal/oidc`, the five handler files; wire gauntlet in `main.go`
   and `server.go`; port the CLI commands and the password re-check
   routes; the session-limit clamp (silent), `auth.adminPasskey`, the
   start-up refusal without a key, sealing of the auth stores, the
   password-list refresher, and a key mounted in
   `deploy/docker-compose.yml`. Go tests from the
   specification in the build notes land in this commit (written
   first, failing, as the order of work requires).
5. `upgrade`: schema migration 4 and the extended fixture tests;
   `scripts/upgrade-fixture-session.py` confirms recovery codes.
6. `live-check`: `live-browser.mjs` confirms recovery codes; any script
   that read the old error text or header.
7. `docs`: `configuration.md` (Authentication, sessions, tokens,
   recovering the admin, the new key), `SECURITY.md`,
   `THIRD-PARTY-NOTICES.md`, `CHANGELOG.md` (Breaking changes: a key is
   required, the session limits cut configured values down; Changed;
   Security; Removed), `docs/upgrades.md` (downgrade note, key
   required), the install docs (key mounted), `docs/configuration.md`'s
   rule 6 passages (#1441) and the password-list fetch (what, from
   where, how often), this record's status.
8. `frontend`: remove the old-contract tolerances from commit 3.

Merges into `dev` before the v0.7.0 audit (#1426), which then runs on the merged code (owner, 2026-10-10; recorded exception).

## Rejected

- **Keep mikroview's handlers over gauntlet's store** (gauntlet design
  §3's two-step plan). Rejected above: the store API moved, and the
  frontend migrates once either way.
- **Convert the documents in a mikroview migration step.** Rejected:
  gauntlet reads the old shape; mikroview code touching the format is
  what ADR-0002 forbids, and a copy of a document into itself has
  nothing to record or roll back.
- **Move the backends to `gauntlet/persist`.** Rejected for v0.7.0:
  mikroview's backends carry the schema stamp, write-behind, Postgres
  and the path-bound file encryption the fixtures depend on; gauntlet
  needs only the four methods and one optional one.
- **Refuse to start on `sessionTTL` above 1 hour.** The documented
  default is 24 hours, so every existing config would refuse. Over-limit
  values are cut down instead (ruling 3).
- **Accept plaintext accounts when no key is mounted**
  (`AllowPlaintextAtRest`, with a start-up warning), keeping #853
  rule 6 for the auth stores. Rejected by the owner (ruling 2): the
  stores hold TOTP secrets, and mikroview now refuses to start without
  a key.
- **Expose role changes, several admins, sessions list, sign-in
  history and passkey-alone sign-in in the frontend now.** Each is a
  UI design call and its own issue; the API is there when they come.

## Owner rulings (2026-10-10)

Rulings 1-10 were put to the owner as questions 4-13. Each is the
decision the design above states.

- **Ruling 1, admin passkey:** required over https on a domain name when
  passkeys are wired, optional otherwise; `auth.adminPasskey` overrides.
- **Ruling 2, accounts at rest:** a key is required. From v0.7.0 mikroview
  refuses to start when no `history.keyFile` is mounted, because
  gauntlet's `OpenStore` refuses unencrypted account data and
  mikroview will not set `AllowPlaintextAtRest`. Every auth store
  (accounts, tokens, recovery-key digests, sign-in history) is sealed
  under the key, on the file and Postgres backends; existing plaintext
  data is sealed in place on first open (`MigratePlaintext`). Ends the
  #853 rule 6 exception for these stores. Needs a start-up refusal
  with a clear error code and message on mounting a key; a key mounted
  in `deploy/docker-compose.yml` and the install docs; a CHANGELOG entry
  under breaking changes; the `docs/configuration.md` rule 6 passages
  rewritten (#1441).
- **Ruling 3, session limits:** configured values above gauntlet's 1h idle /
  24h total are cut down to those limits with no start-up warning; the
  CHANGELOG records it. Documented defaults become 1h / 24h.
- **Ruling 4, token expiry:** one year for API tokens; ingest tokens never
  expire.
- **Ruling 5, daily token sweep:** on.
- **Ruling 6, sign-in country:** on, from mikroview's existing country data.
- **Ruling 7, password list:** the list refresher is on (fetches gauntlet's
  signed list, gauntlet ADR-0007); Have I Been Pwned stays off. The new
  outbound fetch is documented in `docs/configuration.md` (what is
  fetched, from where, how often).
- **Ruling 8, sign-in history:** recorded from v0.7.0; the page is #1442.
- **Ruling 9, passkey-alone sign-in:** off; tracked in #1443.
- **Ruling 10, SSO group roles:** off; tracked in #1444.
- **Ruling 11, sealing plaintext auth files (2026-10-10):** mikroview's
  own `EncryptedFileBackend` gains the seal-in-place option rather than
  gauntlet gaining one; files already sealed open unchanged. On
  Postgres each auth store's row has its own label.
- **Ruling 12, setup-only mode:** the no-key refusal applies there too,
  with the same message.
- **Ruling 13, the plaintext exception list:** deleted outright, drop
  list included, since no install runs keyless. `event-retention.md`
  gains an addendum ending #853 rule 6 and the 2026-09-18 drop-list
  addendum, and noting the auth stores as the exception to "Postgres
  is unaffected".
