// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and `make
// live-check`) never picks it up: it produces images, not pass/fail
// checks, and doesn't fit the check()/done() contract the other scripts
// share. Same shape as capture-release-screenshots.mjs and
// capture-engine-room-screenshots.mjs, which it borrows its login
// mechanics from.
//
// Produces three of docs/authenticator-app.md's own `> Screenshot: ...`
// placeholders (#1249's page); see the update note below for why the
// fourth, the account-menu "nothing set up yet" shot, is no longer one
// of them:
//
//  - docs/screenshots/authenticator-enrol-dark.png: the enrolment
//    screen -- QR code, the text secret beside it, the code box beneath.
//  - docs/screenshots/authenticator-recovery-codes-dark.png: the ten
//    recovery codes, Copy all, I have saved these -- since 3e08bb76 this
//    is AuthEnrolFactor.svelte's 'codes' stage at the forced-enrolment
//    door, not AuthenticatorOverlay's own (see the update note below),
//    so it's captured from a separate, freshly created no-factor account
//    driven straight through that door, not from this script's admin.
//  - docs/screenshots/authenticator-login-code-dark.png: "Enter your
//    code" at a second sign-in, with the "Use a recovery code instead"
//    link.
//
// There is no way to get a screenshot of any step past the first
// without actually driving a real enrolment -- the QR code, the secret
// and the recovery codes are all minted fresh, server-side, per
// enrolment, and cannot be faked from the client. Getting past
// "Confirm" therefore needs a real 6-digit TOTP code, not a made-up
// one: the server checks it exactly as an authenticator app's own clock
// would. totpCode() below is a direct port of internal/auth/totp.go's
// GenerateTOTPCode -- RFC 6238's time-step counter (30s) over RFC
// 4226's HOTP (HMAC-SHA1 + dynamic truncation), base32-decoding the
// same secret the enrol response hands back in its otpauth:// URI. Not
// a call into the Go binary, and not a separate throwaway program left
// lying around uncommitted -- the algorithm is short and standard
// enough (both RFCs, no library, matching the Go file's own comment
// that a third-party TOTP library was never on the table) to write
// straight here instead.
//
// The account this drives is live-env.sh's own admin -- the one and
// only account a fresh instance has, minted by `up` and gone entirely
// once `scripts/live-env.sh down` deletes $MV_DIR. Unlike
// capture-engine-room-screenshots.mjs's "jenny", there is no second,
// purpose-made account created and torn down here: the secret and the
// ten recovery codes captured on screen are real numbers, but they
// belong to an account that stops existing the moment this instance
// goes away, which is what makes shipping them in a doc fine.
//
// Usage:
//   eval "$(scripts/live-env.sh up)"
//   scripts/live-env.sh syslog 50   # a little background traffic, not required
//   cd frontend && node scripts/capture-authenticator-app-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)
//
// Since #1253/m19 (second factors, every local account), live-env.sh's
// own `up` step now enrols a factor for this admin before handing back
// -- requireAuth's forced-enrolment door refuses an account without one
// everything but the four enrolment routes, and without that this
// script's own plain-password login would never reach #main-content.
// completeSecondFactor (live-browser.mjs) finishes that login step with
// MV_TOTP_SECRET, the same helper every live-*.mjs scenario's session()
// already uses.
//
// That earlier factor also means the account-menu screenshot's own
// "no '· on' tag yet" state -- true the moment nothing is enrolled --
// is no longer the account's starting point: it already holds one.
// There is no way back to a browsable, un-enrolled local account either
// (the door refuses that combination outright, and the CLI's own
// `-clear-second-factor` lands the same account back in the door, not
// in the ordinary app -- see sessionResponse.MustEnrolSecondFactor's own
// comment in internal/api/auth.go).
//
// Update, 3e08bb76 (owner ruling: removing an account's last second
// factor is now allowed, and answers truthfully -- every session on the
// account, this one included, is revoked at once instead of the client
// quietly trusting a stale "still signed in" belief). That closed the
// gap the previous version of this comment described: turning off the
// admin's only factor no longer leaves a browsable, un-enrolled session
// to screenshot at all -- the very next render is the forced-enrolment
// door (AuthEnrolFactor.svelte), a different component from the one
// docs/authenticator-app.md's account-menu flow depicts. So:
//
//  - The account-menu "no factor yet" screenshot is no longer reachable
//    from a signed-in session and is not recaptured here (it also isn't
//    stale -- AccountMenu.svelte hasn't changed).
//  - A throwaway passkey is registered first (below) purely so the
//    admin's TOTP factor is never the *last* one when it's turned off --
//    ClearTOTP then keeps the account signed in with the ordinary
//    'off-done' screen (internal/auth/passkeys_test.go's
//    TestClearTOTPKeepsRecoveryCodesWhileAPasskeyRemains pins exactly
//    this), which is what lets the rest of this script reach
//    AuthenticatorOverlay's modal at all, same as before 3e08bb76.
//  - That same test's sibling, TestClearTOTPWithNoPasskeysStillClearsRecoveryCodes,
//    is the other edge: recovery codes are only wiped -- and so only
//    freshly re-minted on the next enrolment -- when TOTP really was the
//    last factor. With a passkey standing in, the codes survive, so
//    re-confirming TOTP here lands on 'on-done' ("already issued"), not
//    'codes'. The recovery-codes screenshot's own fresh mint is only
//    ever shown to an account's first-ever factor, and that is now
//    always enrolled through the door, never through this modal -- so
//    docs/screenshots/authenticator-recovery-codes-dark.png is instead
//    captured from a separate, freshly created no-factor account driven
//    straight through the door (see "The recovery codes, at the
//    forced-enrolment door" below), before this admin's own TOTP factor
//    is touched at all.
//  - The passkey is removed again (via the API, self-service, this
//    account's own password) before the final sign-out/sign-in capture,
//    so the login screen still shows a plain code box rather than
//    offering "Use your passkey" first.

import { chromium } from 'playwright'
import { fileURLToPath } from 'url'
import path from 'path'
import crypto from 'crypto'
import { completeSecondFactor } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}
// The throwaway passkey below (see the update note above) is bound to
// the origin the relying party was built from -- publicUrl's exact
// origin, not MV_URL's 127.0.0.1 literal, which WebAuthn refuses as an
// RP ID outright. live-passkeys.mjs hits the same requirement and
// swaps to it the same way.
const NAV_URL = process.env.MV_PUBLIC_URL || URL_BASE

const outDir = path.join(REPO, 'docs', 'screenshots')

// The deck keeps every nearby card mounted at once (Deck.svelte's
// IntersectionObserver-driven `mounted()`, a 25% lookahead margin so
// scrolling to the next card doesn't pop it in late) rather than only
// ever having the visible one in the DOM, and both SceneBar.svelte and
// Fall.svelte draw their own <AccountMenu />, each with its own
// <AuthenticatorOverlay /> child. `open` (the menu) is local component
// state, so only the card actually clicked opens its menu -- but
// `authState.showAuthenticator` is shared, so opening the dialog from
// any one card's menu makes *every* mounted card's overlay render at
// once, each a full-viewport `position:fixed` backdrop, each visually
// identical since they start from the same step. Whichever is last in
// the DOM (the next card queued to mount, e.g. topography, sitting
// after fall) paints on top and is the one that actually receives
// clicks -- confirmed by hand: a click scoped to the *visible* card's
// own copy landed on the hidden one's instead, silently driving a
// second, separate copy of the step machine (`step`/`secret`/`code` are
// per-instance $state too) while the one this script was reading from
// never left 'status'. So past the account-menu click, this targets
// whichever `.modal` is topmost -- `.locator('.modal').last()` -- not
// the visible card's own, because that is the one every subsequent
// click and read actually has to agree with.
const ACTIVE = '.card[aria-hidden="false"]'
const modal = () => page.locator('.modal').last()

// --- TOTP, ported from internal/auth/totp.go's algorithm -----------------

// base32Decode reverses RFC 4648's standard alphabet with no padding --
// the form EncodeTOTPSecret (and the otpauth:// URI it feeds) produces.
function base32Decode(input) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const ch of input.toUpperCase().replace(/=+$/, '')) {
    const val = alphabet.indexOf(ch)
    if (val === -1) throw new Error(`totp: invalid base32 character ${JSON.stringify(ch)}`)
    bits += val.toString(2).padStart(5, '0')
  }
  const bytes = []
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(bytes)
}

// totpCode mirrors GenerateTOTPCode(secret, totpCounter(now, 30s)): the
// standard 30-second step, 6 digits, HMAC-SHA1 with RFC 4226's dynamic
// truncation. atMs defaults to "now" so every call site gets a code
// valid for the moment it actually submits, not one computed earlier in
// the script and gone stale by the time it's used.
function totpCode(base32Secret, atMs = Date.now()) {
  const counter = Math.floor(atMs / 1000 / 30)
  const counterBuf = Buffer.alloc(8)
  counterBuf.writeBigUInt64BE(BigInt(counter))
  const hmac = crypto.createHmac('sha1', base32Decode(base32Secret)).update(counterBuf).digest()
  const offset = hmac[hmac.length - 1] & 0x0f
  const code = ((hmac[offset] & 0x7f) << 24) | (hmac[offset + 1] << 16) | (hmac[offset + 2] << 8) | hmac[offset + 3]
  return String(code % 1_000_000).padStart(6, '0')
}

const browser = await chromium.launch()
const context = await browser.newContext({
  viewport: { width: 1600, height: 900 },
  colorScheme: 'dark',
  ignoreHTTPSErrors: true,
})
const page = await context.newPage()

await page.goto(NAV_URL, { waitUntil: 'networkidle' })
await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
await page.click('button[type="submit"]')
// live-env.sh's admin already holds a factor by the time this runs (see
// the header comment) -- plain password submit lands on the code box,
// not #main-content, until this finishes the second step with
// MV_TOTP_SECRET.
await completeSecondFactor(page)
await page.waitForSelector('#main-content', { timeout: 15000 })
await page.waitForTimeout(1000)

// --- The recovery codes, at the forced-enrolment door -----------------------
//
// Since 3e08bb76 (every local account needs a second factor, no
// exceptions), the only place an account's first-ever recovery codes are
// ever shown is AuthEnrolFactor.svelte's 'codes' stage, reached by
// signing in with *no* factor enrolled yet -- never AuthenticatorOverlay's
// own 'codes' step below (see step 2's own comment), which this admin
// account (already holding a factor by the time live-env.sh hands it
// back) can no longer reach. So this needs its own account with nothing
// enrolled, created fresh here (POST /api/auth/users, the same admin-only
// route Settings -> people uses) and driven through the door in its own
// browser context, entirely apart from the admin flow the rest of this
// script drives. Done before the throwaway-passkey work below so the
// two never share a context or a `page`.
{
  const doorUser = `door-capture-${crypto.randomBytes(4).toString('hex')}`
  const doorPass = crypto.randomBytes(12).toString('base64url')
  const createRes = await page.request.fetch(`${NAV_URL}/api/auth/users`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: { username: doorUser, password: doorPass, role: 'user' },
  })
  if (!createRes.ok()) {
    console.error(`POST /api/auth/users answered ${createRes.status()} -- cannot create the door-capture account`)
    process.exit(1)
  }

  const doorContext = await browser.newContext({
    viewport: { width: 1600, height: 900 },
    colorScheme: 'dark',
    ignoreHTTPSErrors: true,
  })
  const door = await doorContext.newPage()
  await door.goto(NAV_URL, { waitUntil: 'networkidle' })
  await door.fill('input[autocomplete="username"]', doorUser)
  await door.fill('input[autocomplete="current-password"]', doorPass)
  await door.click('button[type="submit"]')
  // main.door is AuthEnrolFactor's own top-level element -- the only
  // screen an account with no factor can land on (requireAuth's
  // secondFactorEnrolPaths refuses everything else).
  await door.waitForSelector('main.door', { timeout: 15000 })
  await door.click('button.key:has-text("Authenticator app")')
  await door.waitForSelector('[data-testid="totp-secret"]', { timeout: 10000 })
  // groupSecret() (AuthEnrolFactor.svelte) renders the secret in space-
  // separated groups of four, for reading while typing it in by hand --
  // stripped back out before decoding.
  const doorSecretText = (await door.locator('[data-testid="totp-secret"]').textContent())?.trim() ?? ''
  const doorSecret = doorSecretText.replace(/\s+/g, '')
  if (!doorSecret) {
    console.error('could not read the TOTP secret off the door’s enrolment stage')
    process.exit(1)
  }
  await door.fill('#enrol-code', totpCode(doorSecret))
  await door.click('form[aria-label="Confirm the authenticator app"] button.enter')
  await door.waitForSelector('[data-testid="recovery-codes"]', { timeout: 10000 })
  await door.waitForTimeout(300)
  await door.screenshot({ path: path.join(outDir, 'authenticator-recovery-codes-dark.png') })
  console.log('captured authenticator-recovery-codes-dark.png')
  await door.click('button.enter:text-is("I have saved these")')
  await door.waitForSelector('#main-content', { timeout: 15000 })
  await doorContext.close()
}

// --- Register a throwaway passkey -----------------------------------------
//
// See the update note above: turning off the admin's only factor now
// signs the session out instead of leaving it browsable. Adding a
// second factor first means the ClearTOTP below is never clearing the
// account's *last* one, so the session survives it the way the next
// step depends on. CDP's WebAuthn domain (Chromium only) stands in for
// a real platform authenticator, the same virtual-authenticator pattern
// live-passkeys.mjs uses for its own live-check scenario.

{
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })
  await page.click(`${ACTIVE} .account button.chip`)
  await page.waitForSelector(`${ACTIVE} .account .menu`, { timeout: 5000 })
  await page.click(`${ACTIVE} .account .menu button.row:text-is("Passkeys")`)
  await modal().locator('button:has-text("Add passkey")').waitFor({ timeout: 10000 })
  await modal().locator('button:has-text("Add passkey")').click()
  await modal().locator('input[placeholder="this laptop"]').fill('capture-script throwaway')
  await modal().locator('button:has-text("Continue")').click()
  // The admin's TOTP factor (live-env.sh's own) already minted recovery
  // codes, so registering a second factor always takes the "already
  // issued" branch -- nothing here is one of the four screenshots.
  await modal().locator('text=Your recovery codes were already issued').waitFor({ timeout: 10000 })
  await modal().locator('button:has-text("Close")').click()
  await page.keyboard.press('Escape')
  await page.waitForSelector('.modal', { state: 'detached', timeout: 5000 })
}

// --- 0. Turn off the factor live-env.sh enrolled -------------------------
//
// With the throwaway passkey standing in, this is no longer the
// account's last factor, so it reaches the ordinary 'off-done' screen
// and stays signed in -- the update note above covers why that stopped
// being true of the bare account post-3e08bb76.

await page.click(`${ACTIVE} .account button.chip`)
await page.waitForSelector(`${ACTIVE} .account .menu`, { timeout: 5000 })
await page.click(`${ACTIVE} .account .menu button.row:text-is("Authenticator app")`)
await modal().locator('.actions button.danger:text-is("Turn off")').click()
await modal().locator('input[autocomplete="current-password"]').fill(PASS)
await modal().locator('.actions button.danger:text-is("Turn off authenticator app")').click()
await modal().locator('.actions button.confirm:text-is("Close")').click()
await page.waitForSelector('.modal', { state: 'detached', timeout: 5000 })

// --- 1. The enrolment screen: QR code, text secret, code box -------------
//
// The account-menu "nothing set up yet" screenshot (previously captured
// here) is skipped -- see the update note above: with the throwaway
// passkey present the menu now reads "Passkeys · 1", not the bare row
// the docs depict, and that image isn't stale (AccountMenu.svelte
// hasn't changed) so nothing needs it recaptured.

await page.click(`${ACTIVE} .account button.chip`)
await page.waitForSelector(`${ACTIVE} .account .menu`, { timeout: 5000 })
await page.click(`${ACTIVE} .account .menu button.row:text-is("Authenticator app")`)
await modal().locator('.actions button.confirm:text-is("Set up authenticator app")').click()
await modal().locator('[data-testid="totp-secret"]').waitFor({ timeout: 10000 })
// QRCode.toCanvas (lib/qrcode.ts) draws asynchronously -- give it a
// moment so the screenshot doesn't catch a blank canvas.
await page.waitForTimeout(500)
await page.screenshot({ path: path.join(outDir, 'authenticator-enrol-dark.png') })
console.log('captured authenticator-enrol-dark.png')

const secret = (await modal().locator('[data-testid="totp-secret"]').textContent())?.trim()
if (!secret) {
  console.error('could not read the TOTP secret off the rendered enrolment screen')
  process.exit(1)
}

// --- 2. Confirm the admin's own TOTP again -- codes already captured -----
//
// See the update note above: the throwaway passkey that keeps this
// session alive also means the account's recovery codes were never
// wiped, so confirming below always lands on 'on-done' ("already
// issued"), never 'codes') -- the actual recovery-codes screenshot was
// already taken from the door, above, by an account for which this is
// genuinely the first factor. Confirmed anyway (rather than skipped),
// since step 3's sign-in needs the factor actually turned back on; the
// 'codes' branch is kept as a fallback in case that assumption ever
// stops holding.

await modal().locator('input[autocomplete="one-time-code"]').fill(totpCode(secret))
await modal().locator('.actions button.confirm:text-is("Confirm")').click()
await Promise.race([
  modal().locator('[data-testid="recovery-codes"]').waitFor({ timeout: 10000 }),
  modal().locator('text=Authenticator app turned on').waitFor({ timeout: 10000 }),
  modal().locator('.error').waitFor({ timeout: 10000 }),
])
if ((await modal().locator('.error').count()) > 0) {
  console.error(`confirming the TOTP code failed: ${await modal().locator('.error').textContent()}`)
  process.exit(1)
}
if ((await modal().locator('[data-testid="recovery-codes"]').count()) > 0) {
  await page.waitForTimeout(300)
  await page.screenshot({ path: path.join(outDir, 'authenticator-recovery-codes-dark.png') })
  console.log('captured authenticator-recovery-codes-dark.png')
  await modal().locator('.actions button.confirm:text-is("I have saved these")').click()
} else {
  console.log(
    "authenticator-recovery-codes-dark.png already captured from the door above -- 'codes' step unreachable from this modal post-3e08bb76, see this file's header comment",
  )
  await modal().locator('.actions button.confirm:text-is("Close")').click()
}
await page.waitForSelector('.modal', { state: 'detached', timeout: 5000 })

// --- Remove the throwaway passkey ------------------------------------------
//
// So the sign-in below shows the plain code box the docs depict, not
// "Use your passkey" (AuthLogin offers whichever the address can use
// first, since 390098af) -- self-service, this account's own password,
// the same DELETE live-passkeys.mjs's own housekeeping block uses.

{
  const list = await page.request.fetch(`${NAV_URL}/api/auth/passkeys`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
  const passkeys = list.ok() ? await list.json() : []
  for (const pk of passkeys ?? []) {
    await page.request.fetch(`${NAV_URL}/api/auth/passkeys/${encodeURIComponent(pk.id)}`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
      data: { password: PASS },
    })
  }
}

// --- 3. Signing back in: "Enter your code" --------------------------------

await page.click(`${ACTIVE} .account button.chip`)
await page.waitForSelector(`${ACTIVE} .account .menu`, { timeout: 5000 })
await page.click(`${ACTIVE} .account .menu button.row:text-is("Sign out")`)
await page.waitForSelector('input[autocomplete="username"]', { timeout: 10000 })
await page.waitForTimeout(1000)

await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
await page.click('button[type="submit"]')
await page.waitForSelector('input[autocomplete="one-time-code"]', { timeout: 10000 })
await page.waitForTimeout(500)
await page.screenshot({ path: path.join(outDir, 'authenticator-login-code-dark.png') })
console.log('captured authenticator-login-code-dark.png')

// Left signed out here, deliberately not finishing this second sign-in:
// the server's replay guard (VerifyTOTP, internal/auth/totp.go) refuses
// a code already spent, and the one just confirmed a few steps up is
// still within its own 30s counter often enough that computing "the
// current code" again would just hand back the same, now-spent one.
// Waiting out a fresh window to avoid that would only be for this
// script's own tidiness -- `scripts/live-env.sh down` destroys the
// whole instance, account included, regardless of where this leaves
// the browser.

await context.close()
await browser.close()
