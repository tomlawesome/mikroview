# #1250 delivery design: passkeys (WebAuthn) as a second factor

Issue #1250 (passkeys), milestone M19, shipping in v0.6.1. #1249 is merged on
this branch; this design is written against the code it actually left behind,
not the issue text. Build agents implement what is written here; anything this
document does not decide is an escalation, not a guess.

**Library**: `github.com/go-webauthn/webauthn` **v0.18.2**, BSD-3-Clause,
licence and version checked 2026-09-23. Needs Go >= 1.26; the project is on
1.27.1. Settled — do not re-open. Passwordless sign-in is out of scope:
password first, passkey second, and the docs say so.

## The public URL setting

New **top-level** config key `publicUrl` (env override `MIKROVIEW_PUBLIC_URL`
in `config.go`'s existing env block — the issue wrote `MV_PUBLIC_URL`, but
every env var in this codebase is `MIKROVIEW_*`, so the issue's name loses to
the convention). The URL users reach MikroView on, e.g.
`https://mikroview.home.lan:8443`. Do **not** reuse or fall back to
`oidc.publicBaseUrl` — the two usually match but changing one must never
silently break the other; instead `validate.go` warns when `oidc.publicBaseUrl`
is set and `publicUrl` is not, suggesting they probably want the same value.

Validation is warn-and-degrade, never fatal — MikroView always boots; passkeys
fail into an explained "unavailable" state (below). New problem codes:

- `CFG-0100` `publicUrl` does not parse as an absolute URL → ignored.
- `CFG-0101` host is an IP literal → passkeys unavailable (status `ip`).
- `CFG-0102` scheme is `http` and host is not `localhost` → unavailable
  (status `insecure`); browsers only offer passkeys over https.
- `CFG-0103` path, query or fragment present → warn, strip, still usable.

`env_docs_test.go` will force the `docs/configuration.md` entry; it belongs to
the config slice, not the docs slice.

**RP construction** (`internal/api/webauthn.go`): `webauthn.New(&Config{RPID:
<hostname of publicUrl>, RPDisplayName: "MikroView", RPOrigins:
[<exact origin of publicUrl>]})`. Server-side availability is one of `ready`,
`unset`, `ip`, `insecure`, computed once at boot from the validated setting.

**When `publicUrl` is set but later changes**: each stored passkey records the
`RPID` it was registered under. A passkey whose stored RPID differs from the
current one is *stale*: excluded from login assertion options, shown in the
Account menu with why, still deletable. This is the whole answer to "registered
against a hostname that later changes" — nothing breaks silently, the UI names
the old and new address, and login falls back to the other ways in.

## Store (`internal/auth`)

`User` gains `Passkeys []Passkey` with `json:"passkeys,omitempty"` — it rides
the accounts file, so `-backup` carries it with no envelope work beyond a
round-trip test.

```go
type Passkey struct {
    ID          []byte                    // credential ID (JSON base64)
    PublicKey   []byte
    SignCount   uint32
    Transports  []protocol.AuthenticatorTransport
    Flags       webauthn.CredentialFlags  // BE/BS/UP/UV, needed to rebuild the library credential
    RPID        string                    // the RP ID it was registered under — the stale-passkey story
    Name        string
    CreatedAt   time.Time
    LastUsedAt  time.Time
}
```

`RPID` and `Flags` are additions to the issue's field list; both are needed for
correctness (stale detection; faithful `webauthn.Credential` reconstruction).

New file `internal/auth/passkeys.go`, methods following the TOTP methods'
shape exactly (Persisted guard, reloadIfStale, restore-on-persist-failure):

- `AddPasskey(userID, pk)` — cap **10 per account**, duplicate credential ID
  refused, `Name` trimmed, bounded at 64 chars, empty → `Passkey <n>`.
- `RenamePasskey(userID, credID, name)`.
- `DeletePasskey(userID, credID)` — in the same locked write, if no factor of
  either kind remains afterwards, clear `RecoveryCodes` too.
- `RecordPasskeyAssertion(userID, credID, signCount, now)` — advances
  `SignCount` and `LastUsedAt`; forward-only, same stance as
  `RecordTOTPCounter`.
- `ClearPasskeys(userID)` — admin clear; same conditional recovery-code rule.
- `ClearAllSecondFactors(userID)` — TOTP + passkeys + recovery codes in one
  write, for the CLI.
- `(u *User) HasSecondFactor() bool` — `HasActiveTOTP() || len(Passkeys) > 0`.

**Change to #1249's code**: `ClearTOTP` currently always deletes the recovery
codes. With passkeys sharing them that would strip a still-active passkey
factor of its fallback, so the code-clearing becomes conditional on no
passkeys remaining. `List()` blanks `Passkeys` the same way it blanks
`TOTPSecret` (the API reports a count instead).

## Recovery codes are shared

One set of ten covers both factors. The rules, exhaustively:

- **Mint** when a factor activation finds `RecoveryCodes` empty — TOTP confirm
  or passkey register-finish, whichever happens first. Shown once, that
  response only.
- **Never regenerate on a later activation.** #1249's `handleTOTPConfirm`
  mints unconditionally today; that becomes mint-if-absent (a passkey-first
  user enrolling TOTP must not have their existing codes silently replaced).
  Its response gains `recoveryCodes: null, alreadyIssued: true` for that case
  and `AuthenticatorOverlay` skips its `codes` step with a one-line note that
  the existing codes still stand.
- **Clear** only when the last factor of either kind goes, whichever route
  removes it (self-service delete, either admin clear, the CLI — the CLI
  clears everything by definition).
- Burning codes at login is untouched: `handleAuthLoginFactor`'s recovery
  branch already works for any factor mix.

## Routes (`internal/api`)

All session-gated, own account, registered in `server.go`'s table. Plural
`passkeys` throughout (the issue mixed singular and plural; one shape wins),
and a rename route the issue's frontend list implies but its route list omits:

- `GET /api/auth/passkeys` → `[{id (base64url), name, createdAt, lastUsedAt,
  transports, stale (bool), rpId}]` — feeds the overlay.
- `POST /api/auth/passkeys/register/begin` `{}` → the library's
  `protocol.CredentialCreation` JSON; the `webauthn.SessionData` sealed
  AEAD-style (the `pendingLoginCodec` pattern) into a 5-minute cookie
  `mikroview_passkey_register`, path `/api/auth/passkeys`. Refused with the
  availability reason when status is not `ready`. `excludeCredentials` lists
  the account's current-RPID passkeys so one authenticator cannot enrol twice.
- `POST /api/auth/passkeys/register/finish` `{credential, name}` →
  `FinishRegistration`, `AddPasskey`, clear the cookie (single-use: a replayed
  finish is refused because the cookie is gone). If this is the account's
  **first factor**: revoke all sessions and reissue this one (parity with TOTP
  confirm — a stolen session must not outlive the factor turning on), and
  mint recovery codes per the rules above. Response
  `{passkey, recoveryCodes: [...]|null}`.
- `PATCH /api/auth/passkeys/{id}` `{name}` — rename, no password.
- `DELETE /api/auth/passkeys/{id}` `{password}` — password rechecked through
  the existing `passwordRecheckLimiterKey` budget, same as TOTP delete.
- `POST /api/auth/login/factor/begin` — pending cookie required (its path
  `/api/auth/login` already covers this route); `BeginLogin` over the
  account's non-stale passkeys → assertion options; `SessionData` sealed into
  cookie `mikroview_passkey_assert`, path `/api/auth/login`, 5 minutes.
- `POST /api/auth/login/factor` — the existing route branches on body shape:
  `{code}` is #1249's path unchanged; `{assertion}` runs `FinishLogin` against
  the sealed `SessionData`. Success goes through `completeLoginFactor`
  untouched. Shares the same `LoginLimiter` reservations as the code path.
- `DELETE /api/auth/users/{id}/passkeys` — admin clear, mirrors
  `handleTOTPAdminClear` including refusing the caller's own account.

`handleAuthLogin`'s gate widens from `HasActiveTOTP()` to `HasSecondFactor()`,
and its response becomes `{"secondFactor": [...], "passkeyOrigin": "..."}`:
the list holds only *currently usable* kinds — `totp` if active, `passkey` if
at least one non-stale passkey exists and the RP is `ready` — passkey first.
`passkeyOrigin` is present iff `passkey` is listed. An account whose only
factor is stale passkeys gets an **empty list**: the password still never
creates a session, and the frontend shows the recovery-code screen with an
explanation. `handleAuthLoginFactor`'s deleted-mid-flight re-check widens the
same way.

**Sign count**: after `FinishLogin`, `credential.Authenticator.CloneWarning`
true (the library sets it on a regression when either counter is nonzero — a
platform authenticator that always reports zero never trips it, and must not)
→ refuse the login 401 "that passkey couldn't be verified — use another way
in", audit `account.passkey_clone_suspected`, and leave the stored count
unmoved. Otherwise `RecordPasskeyAssertion`.

**Session/user surfaces**: `GET /api/auth/session` gains
`passkeys: {count, status, origin}` (`origin` set only when `ready`);
the users list gains `passkeyCount` per user.

**Audit**: `account.passkey_added`, `account.passkey_removed`,
`account.passkey_clone_suspected`, `user.passkeys_cleared` (admin), following
the `account.totp_*` naming. Rename is cosmetic and is not audited.

## CLI

`-clear-second-factor` switches from `ClearTOTP` to `ClearAllSecondFactors`
and its output sentence says both: "authenticator app and passkeys removed;
recovery codes cleared". Still one command for "I lost everything"; no
passkey-only CLI variant.

## Frontend

**Capability check**, in one place (`lib/passkeys.svelte.ts`): the browser can
do passkeys here iff `window.isSecureContext`, `PublicKeyCredential` exists,
`PublicKeyCredential.parseCreationOptionsFromJSON` is a function, and
`location.origin === passkeys.origin` from the session. Use the native
`parseCreationOptionsFromJSON` / `parseRequestOptionsFromJSON` /
`credential.toJSON()` — no hand-rolled base64url walking; a browser old enough
to lack them is shown the same "use the authenticator app" path.

**Account menu — the composition call**: two rows, not a merged one. Directly
under "Authenticator app · on" sits "Passkeys · 2" (count tag only when > 0),
opening a new `PasskeysOverlay` cut to `AuthenticatorOverlay`'s pattern.
Merging both factors into one "second factor" overlay would be the tidier
end-state but means reworking a surface #1249 just shipped mid-milestone; the
two overlays instead each carry one cross-reference line ("Your ten recovery
codes cover your authenticator app and your passkeys alike"). The row is
always present — hiding it is exactly the silent failure the issue forbids.

`PasskeysOverlay` states: `list` (rows: name, added date, last used, rename
pencil, remove; stale rows tagged "made for <old host> — won't work here" with
only remove offered), `adding` (name field then the browser prompt), `codes`
(reuses the recovery-codes presentation), `removing` (password confirm, same
beat as TOTP's turn-off), and one `unavailable` state whose copy depends on
why:

- status `unset`: "Passkeys need MikroView to have a web address. A passkey is
  tied to a domain name — that is how WebAuthn works, so a bare IP address can
  never hold one. Set `publicUrl` in your config (or `MIKROVIEW_PUBLIC_URL`)
  to the https address you reach MikroView on and restart. Your authenticator
  app works everywhere, IP addresses included."
- status `ip`: same shape, opening instead with "`publicUrl` is set to an IP
  address, and browsers refuse to create a passkey for an IP."
- status `insecure`: "…is set to an http address. Browsers only offer passkeys
  over https."
- ready but wrong address (server fine, browser on the IP): "You're viewing
  MikroView at <current origin>, but passkeys live at <link to
  `passkeys.origin`>. Open MikroView there to add or use one. Your
  authenticator app works from either address."

**Login second step** (`AuthLogin` + `AuthScreen`): the factor screen is
driven by the `secondFactor` list. With `passkey` listed and the browser
capable: a "Use your passkey" button fires the prompt (deliberately a click,
never auto-fired on mount — Safari's gesture rules), with "Use your
authenticator app" (when `totp` listed) and "Use a recovery code" as links
below. Switching between the three is client-side state only — the pending
cookie keeps the login alive, nobody re-enters a password. `passkey` listed
but browser incapable: the button is replaced by "Your passkeys work at <link>
— open MikroView there (you'll enter your password again), or use another way
in below." Empty list: recovery-code field with "This account's passkeys were
made for a different web address. Enter one of your recovery codes to finish
signing in." Users group in `EngineRoom` shows the passkey count beside the
existing authenticator-app tag, with the admin clear button mirroring TOTP's.

## Waves and file ownership

No two agents in the same wave touch the same file. Wave 2 needs all of wave 1
merged; F needs only 1A; G and H need wave 2.

| Wave | Slice | Files it owns |
| --- | --- | --- |
| 1 | A store, recovery-code sharing | `internal/auth/passkeys.go` + test (new), `internal/auth/store.go` (User fields, `HasSecondFactor`, `ClearTOTP` condition, `List` blanking), `backup_passkeys_roundtrip_test.go` (new) |
| 1 | B public URL setting | `internal/config/config.go`, `validate.go`, their tests, the `docs/configuration.md` entry |
| 1 | C dependency, RP, cookie codecs, fake authenticator | `go.mod`, `go.sum`, `internal/api/webauthn.go` (new), `internal/api/webauthn_test.go` (new), `internal/api/webauthnfake_test.go` (new) |
| 2 | D routes | `internal/api/passkey.go` + test (new), `internal/api/auth.go`, `internal/api/server.go`, `authz_matrix_test.go` |
| 2 | E frontend | `lib/auth.svelte.ts`, `lib/passkeys.svelte.ts` (new), `PasskeysOverlay.svelte` (new), `AccountMenu.svelte`, `AuthenticatorOverlay.svelte` (alreadyIssued), `AuthScreen.svelte`, `AuthLogin.svelte`, `EngineRoom.svelte`, their tests |
| 2 | F CLI clear | `main.go` |
| 3 | G live-check | `scripts/live-passkeys.mjs` (new), `Makefile` |
| 3 | H docs | `SECURITY.md`, `docs/authenticator-app.md`, screenshots |

E builds against the contracts in this document and is proved end to end once
D lands, same arrangement as #1249's frontend slice.

**The fake authenticator (slice C)**: the issue said the library ships a
software authenticator for tests. It does not — its `testing/` directory holds
metadata mocks only. Its own e2e tests instead hand-build the ceremony from a
stdlib ECDSA P-256 key: authenticator data, client-data JSON, CBOR attestation
object, signed assertions (`webauthn/es256k_e2e_test.go` is the worked
example, using only the library's own `protocol` and `webauthncbor` packages).
Slice C ports that pattern into a small test-only helper — settable RP ID,
origin, sign count — rather than adding a third-party test dependency.

## Tests

Beyond the issue's list (register/login round-trip, wrong origin refused,
sign-count regression refused, last-passkey-with-TOTP keeps the requirement,
removing every factor clears it, backup round-trips):

- Password-only login on a passkey-only account: assert **no session cookie
  is set** and the body is the `secondFactor` shape — not merely "no error".
- Stale-RPID account: factor list omits `passkey`, password still never
  creates a session, recovery code still completes.
- Mint-once: passkey-first then TOTP-confirm leaves the original hashes
  untouched and returns `alreadyIssued`; and the reverse order likewise.
- Clear-conditional: `ClearTOTP` with a passkey present keeps the codes;
  deleting the last passkey with TOTP active keeps them; the CLI clears all.
- Register-finish replay (second finish on the same cookie) refused; assertion
  `SessionData` from register cannot finish a login and vice versa.
- Clone warning: stored count 5, presented 3 → 401, audit entry, stored count
  still 5. Zero-reporting authenticator (0 → 0) signs in fine.
- Cap and bounds: 11th passkey refused; 65-char name refused or truncated per
  the store rule; wrong password on delete refused and rate-limited.
- New routes join the authz matrix.
- Live-check (G): Playwright's CDP virtual authenticator registers a passkey
  and completes a login against `publicUrl` set to the harness's own
  `localhost` origin — the one test that exercises the real browser JSON path.

**Tests that would pass while proving nothing, named so nobody writes them**:
a wrong-origin test asserting only `err != nil` (a malformed body fails too —
pair it with the same body succeeding at the right origin); a sign-count test
built on 0 → 0 (the library deliberately never warns there); a clone-warning
test that checks the 401 but not that the stored count stayed put; a backup
test comparing structs without completing a login from the restored store; a
frontend test asserting only that a `navigator.credentials` mock was called.

## Departures from the issue body

- `MV_PUBLIC_URL` → yaml `publicUrl` + env `MIKROVIEW_PUBLIC_URL` (codebase
  convention; every env var is `MIKROVIEW_*`).
- Routes pluralised to `/api/auth/passkeys/...`; `GET` list and `PATCH` rename
  added (the issue's frontend requires both, its route list named neither).
- `Passkey` gains `RPID` and `Flags` (stale detection; faithful credential
  reconstruction).
- Two #1249 behaviours change: TOTP confirm mints recovery codes only when
  none exist; `ClearTOTP` keeps them while passkeys remain.
- "Sign-count going backwards → refuse" refined to the library's
  `CloneWarning` semantics, so zero-reporting platform authenticators (the
  common passkey case) are never falsely refused.
- The fake-authenticator claim corrected: hand-built test helper on the
  library's `protocol` package, not a shipped one.

Follow-up worth its own issue, not built here: a "regenerate recovery codes"
action — today the only path is remove-and-re-add a factor, which #1249 also
lives with, but with two factor kinds that workaround gets clumsier.

Written by Fable 5, 2026-09-23.
