# First run with a setup code — the ruling (#1415)

Owner ruling, 2026-10-01: the first admin can only be created with a
one-time setup code the server prints once in its log (gauntlet
ADR-0003, after orbit ADR-0022). This file is the design of what that
does to the first-run screen, `frontend/src/components/AuthSetup.svelte`
and the shell it renders, `AuthScreen.svelte`. There are no rounds
behind it and no mock-up: the change is one field, one note and three
lines of copy on a screen whose classes already exist, so the notes
below are exact. Build it from these, not from an impression.

## What stays

The door is unchanged: the gate (Enter), then **Create the admin
account** in the same column — centred title, `.subtitle`, underlined
placeholder-labelled fields, the outlined `.submit-btn` — then the
confirmation gate with Continue. SSO is already absent from every step
of setup: AuthSetup never passes `ssoAvailable` to AuthScreen and the
gate renders no link, pinned by "never offers SSO as a way into the
first run" in `AuthSetup.svelte.test.ts`. Nothing to hide; keep that
test. The hand-off to the provider *after* creation (`startSSOLink`)
also stays: the account exists by then, so the server's "no SSO during
setup" refusal does not reach it.

## What changes

The form's order, top to bottom:

1. `h1` — **Create the admin account** (unchanged).
2. `.subtitle` — one clause inserted in each variant:
   - no OIDC: *No account exists yet. Whoever completes this form,
     with the setup code from MikroView's log, becomes the admin.*
   - OIDC configured: *No account exists yet. Whoever completes this
     form, with the setup code from MikroView's log, becomes the admin
     — then you sign in with SSO to connect it, and this password stays
     as your way in if your provider is ever unreachable.*
3. `.subtitle-note` — new on this screen, the class #1250 added for
   left-aligned prose above a control. Exact text:

   > It's in MikroView's log from when it last started
   > (docker compose logs mikroview), on the line that says “create the
   > first admin with setup code”. Lost it? Restart MikroView and it
   > prints a new one.

   Plain text, no code styling (AuthScreen has none). The note comes
   before the field because the operator looks for the thing before
   they type it.
4. **setup code** — the new field, first, above account: proof of host
   access is the key to this door, and the server checks it before
   anything else (ADR-0003 §3). Same markup as the account field:

   ```html
   <label>
     <span class="sr-only">setup code</span>
     <input type="text" autocomplete="off" autocapitalize="off"
            spellcheck="false" placeholder="setup code" bind:value={code} required />
   </label>
   ```

   Not `inputmode="numeric"` and not `one-time-code`: the code is
   letters and digits (`xxxx-xxxx-xxxx-xxxx`), and the browser's
   one-time-code autofill looks for SMS codes. No masking or
   reformatting as they type — the server takes it with or without
   dashes, in either case, so the screen says nothing about either.
5. account, password, confirm password (unchanged).
6. `.error`, then **Create account** (unchanged).

Nothing in `<style>` changes: `.subtitle-note`, `label`, `input` and
`.error` already draw this.

## States and copy

| State | Where it shows | Exact text |
|---|---|---|
| Code empty on submit | `.error`, checked first, before the account check | Enter the setup code from MikroView's log. |
| Wrong code (server `401`, plain body) | `.error`, shown as `register()` returns it; the typed code stays in the field so a typo can be fixed | that setup code didn't match -- the current one is in MikroView's log; restart MikroView for a new one |
| Rate-limited (server `429`) | `.error`, as returned | too many attempts, try again later |
| Store no longer empty (`409`) | `.error`, as today | unchanged — `writeAuthError`'s text; a reload lands on the login door |

The 401 and 429 bodies are server copy: `register()` shows `res.text()`
unchanged, as the login door does. The 429 is login's own line,
verbatim, because the ADR gives the two the same budget — one message
in one place. Lowercase and ` -- ` follow the server's other refusals
("that code didn't match -- check your authenticator app's clock…").

The log line the note points at (the server side of this issue, so the
docs and the screen name the same words; gauntlet's text, so #1202 is a
swap): one `WARN` from the `auth` component,

```
no account exists yet -- create the first admin with setup code xxxx-xxxx-xxxx-xxxx (valid until an account exists or this process restarts)
```

## Build notes

- `AuthScreen.svelte`: a `setupCode = false` prop renders items 3–4
  above the account field and runs the empty check; the submit calls
  a distinct `onSubmitSetup?: (setupCode: string, username: string,
  password: string) => Promise<string | null>` rather than smuggling the
  code through `onsubmit`'s two slots (the component's own rule: a
  positional argument that no longer means what it says is a trap).
  Reuse the existing `code` state; `factorOnly` and `setupCode` are
  never both set.
- `AuthSetup.svelte`: pass `setupCode` and `onSubmitSetup={createAdmin}`;
  `createAdmin(setupCode, username, password)` calls
  `register(username, password, setupCode)`. The two `createSubtitle`
  strings above replace today's. Everything after `register()` —
  the SSO hand-off, `linkError`, the confirmation — is unchanged.
- `lib/api.ts`, `register`: posts `{ username, password, setupCode }`;
  the error path is unchanged (`res.text()`).
- Tests, `AuthSetup.svelte.test.ts`: the form after Enter shows a
  "setup code" textbox before "account" and the note; submitting with
  the code empty shows the empty line and never calls `register`;
  `register` receives the code; a string from `register` shows in
  `.error` with the code still in the field; the existing SSO test
  stays as it is.
- Docs: `docs/configuration.md` "Authentication" and the operator's
  first-run steps say where the line is — the same words as the note —
  and that an unexpectedly empty store (deleted file, wrong path, bad
  restore) prints a new code rather than letting the first visitor in.

Written by Fable 5.1, 2026-10-02.
