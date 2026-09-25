# M19 delivery plan: second factor on web login

Milestone **M19 — Second factor on web login**, shipping in v0.6.1. Design
record #1245; exit criteria on the milestone itself. This document is the
delivery order and who owns which files, so parallel work does not collide.

## The three issues are a strict chain

| Order | Issue | Why it is here |
| --- | --- | --- |
| 1 | #1249 authenticator app (TOTP) | builds the shared pieces: the pending-login cookie, `POST /api/auth/login/factor`, the ten recovery codes, and the admin + CLI clear paths |
| 2 | #1250 passkeys (WebAuthn) | reuses all four of those; starting it first would mean inventing and then rebuilding them |
| 3 | #1253 a factor is mandatory | the rule cannot be enforced until there is something to enrol |

There are **no parallel lanes at issue level**. Each stage consumes the
previous one's surfaces.

## Verified against the code, 2026-09-23

- `MustChangePassword` gate exists at `internal/api/auth.go:330`, so #1253's
  "same shape as #1251's gate" is a real thing to copy.
- `handleAuthLogin` is at `internal/api/auth.go:588`; #1249's body says 523,
  so the reference has drifted but the function is there.
- **`MV_PUBLIC_URL` does not exist** anywhere in the code. #1250 says "if a
  setting like it exists by then, reuse it" -- it does not, so #1250 must add
  it. Passkeys cannot work until MikroView knows its own hostname.
- No TOTP code exists yet; #1249 starts from nothing.
- #1253's comment thread raises two questions (who it is mandatory for, and
  what happens to existing accounts) that were **answered on 2026-09-18** and
  are in its body and the milestone's exit criteria. Do not re-ask them.

## Fan-out inside #1249

Split by file ownership. No two agents in the same wave touch the same file --
that is the rule, not a preference (shared files are where parallel work
collides).

| Wave | Slice | Files it owns |
| --- | --- | --- |
| 1 | TOTP codes | `internal/auth/totp.go`, `internal/auth/totp_test.go` (new) |
| 1 | Store fields and recovery codes | `internal/auth/store.go`, the backup envelope, recovery-code generation and hashing |
| 2 | HTTP routes | `internal/api/auth.go` and its tests: enrol, confirm, delete, the login factor step, the admin clear route |
| 2 | Frontend | `AccountMenu.svelte`, `AuthLogin.svelte`, `EngineRoom.svelte`, the `qrcode` dependency |
| 3 | CLI clear | `main.go`, `internal/auth/recovery.go` -- `-clear-second-factor` behind the recovery key |
| 3 | Docs | `SECURITY.md`, the operator page and its screenshots |

Wave 2 needs wave 1 merged: the routes call the code verifier and the store
fields. The frontend can start against the contract in #1249's body, which is
precise enough to build against, but cannot be proved end to end until the
routes land.

Wave 3's CLI slice touches `internal/auth/recovery.go` and `main.go`, which
wave 2 does not, but it needs the store fields from wave 1.

## Standing constraints for every slice

- RFC 6238 on the standard library. **No new Go dependency** for TOTP.
- A password-only login on an account holding a factor must never create a
  session. #1249 calls that out as a bug to test for; it is the single most
  important test in the milestone.
- A code's 30-second counter may be used once -- the replay guard is
  `TOTPLastCounter`, not an afterthought.
- Recovery codes are single-use and hashed the same way passwords are.
- SSO accounts are never offered a local factor; their provider owns identity.
- Every new `User` field joins the backup envelope and must round-trip. There
  is a test for that pattern already; follow it.

Written by Opus 5, 2026-09-23.
