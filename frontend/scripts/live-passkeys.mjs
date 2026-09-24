// SPDX-License-Identifier: AGPL-3.0-only
//
// #1250 slice G: passkeys as a second factor, driven end to end in a
// real browser -- register one from the account menu while signed in,
// sign out, then sign back in with the password and complete the
// second step with that same passkey. See
// docs/plans/passkeys-second-factor.md's Tests section, "Live-check
// (G): Playwright's CDP virtual authenticator registers a passkey and
// completes a login against publicUrl set to the harness's own
// localhost origin -- the one test that exercises the real browser
// JSON path" (parseCreationOptionsFromJSON / parseRequestOptionsFromJSON
// / credential.toJSON(), none of which jsdom or the Go layer can reach).
//
// Chromium only. A real passkey needs a virtual authenticator, which is
// Chrome DevTools Protocol's WebAuthn domain reached through
// Playwright's context.newCDPSession(page) -- Firefox and WebKit expose
// nothing equivalent. #1306 covers running the suite under both; this
// scenario skips there rather than failing a run it cannot pass.
if ((process.env.MV_BROWSER || 'chromium') !== 'chromium') {
  console.log(
    `live-passkeys: skipping -- a virtual authenticator needs CDP's WebAuthn domain, which only Chromium exposes (MV_BROWSER=${process.env.MV_BROWSER})`,
  )
  process.exit(0)
}

// A passkey is bound to the origin the relying party was built from,
// which is publicUrl's exact origin -- here, MV_PUBLIC_URL
// (http://localhost:19846), not MV_URL (http://127.0.0.1:19846) the
// rest of the harness talks to the same instance over.
// live-browser.mjs reads MV_URL into a module-level const at import
// time, so the reassignment has to land before that import happens --
// hence the dynamic import below rather than a static one.
process.env.MV_URL = process.env.MV_PUBLIC_URL
const { session, check, done, openAccountMenu } = await import('./live-browser.mjs')

const USER = process.env.MV_USER
const PASS = process.env.MV_PASS

const { page } = await session()

// page.isVisible() answers from the DOM as it stands right now and does
// no waiting at all. For anything that appears in response to a click
// that is a race, and it is one this scenario lost twice against a UI
// that was working correctly: the check reported FAIL and the very next
// line clicked the same element successfully, because click() auto-waits
// and isVisible() does not. Every visibility assertion here goes through
// this instead.
async function visible(selector, timeoutMs = 10000) {
  try {
    await page.locator(selector).first().waitFor({ state: 'visible', timeout: timeoutMs })
    return true
  } catch {
    return false
  }
}

async function api(method, path, body) {
  const res = await page.request.fetch(`${process.env.MV_URL}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
}

// Housekeeping, not the thing under test. live-browser.mjs's own
// resetInstance() deliberately leaves accounts (and everything on them)
// alone, so a crash midway through an earlier run of this very scenario
// leaves a passkey sitting on this shared admin account -- which would
// change what the registration below actually exercises (a second
// factor rather than the account's first, so no recovery codes would be
// minted) and leave "Passkeys · 1" untrue before this run has done
// anything. Clear any survivors through the API first so registration
// always starts from the same "no factor yet" account. This is setup,
// not verification -- the registration and login themselves are driven
// through the UI below.
{
  const existing = await api('GET', '/api/auth/passkeys')
  for (const pk of existing.body ?? []) {
    await api('DELETE', `/api/auth/passkeys/${encodeURIComponent(pk.id)}`, { password: PASS })
  }
}

// --- The virtual authenticator --------------------------------------------
// CDP's WebAuthn domain: automaticPresenceSimulation answers the
// "touch the key" step with no further input, and hasResidentKey +
// hasUserVerification + isUserVerified stand in for a real platform
// authenticator (fingerprint/face/PIN) offering a discoverable
// credential -- the passkey case the design targets, not a roaming
// security key.
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

// --- Register a passkey from the account menu -----------------------------

await openAccountMenu(page)
check(
  await visible('.account .menu button.row:text-is("Passkeys")'),
  'the account menu offers Passkeys, with no count tag before anything is added',
)
await page.click('.account .menu button.row:text-is("Passkeys")')
check(await visible('[aria-label="Passkeys"]'), 'the Passkeys dialog opens')

// Fail here with a diagnosis rather than an uncaught timeout if this
// deployment never reached PasskeyStatusReady -- the dialog's
// 'unavailable' state (its own four-way copy block) offers no "Add
// passkey" button at all, and a bare click().catch would either hang
// for Playwright's full default timeout with no explanation or -- worse
// -- swallow the failure outright. Surfacing the dialog's own body text
// is what tells "this deployment genuinely can't do passkeys" apart
// from "the selector is wrong", the same ambiguity goTo()'s own timeout
// handler in live-browser.mjs exists to resolve.
const addButton = page.locator('button:has-text("Add passkey")')
const becameReady = await addButton
  .waitFor({ state: 'visible', timeout: 10000 })
  .then(() => true)
  .catch(() => false)
if (!becameReady) {
  const shown = await page.locator('.modal .body').innerText().catch(() => '(could not read the dialog body)')
  check(false, `the dialog never offered "Add passkey" -- publicUrl is presumably not reaching PasskeyStatusReady here: ${JSON.stringify(shown)}`)
  done()
}
await page.click('button:has-text("Add passkey")')
check(
  await visible('input[placeholder="this laptop"]'),
  'adding asks for a name before the browser prompt fires',
)
await page.fill('input[placeholder="this laptop"]', 'live-check authenticator')
await page.click('button:has-text("Continue")')

// The ceremony (navigator.credentials.create(), begin -> browser prompt
// -> finish) round-trips through the virtual authenticator with no
// further input needed.
//
// What the dialog shows next depends on whether this passkey is the
// account's first factor: the first one mints ten shared recovery codes
// and shows them once, a later one says the existing codes still cover
// it (docs/plans/passkeys-second-factor.md, "Recovery codes are
// shared": "Mint when a factor activation finds RecoveryCodes empty").
//
// In practice it is the later one, because since #1253 every local
// account must hold a factor and scripts/live-env.sh enrols an
// authenticator app for this admin when it stands the instance up --
// so the codes were minted there. Both paths are asserted anyway
// rather than only the expected one, so this scenario keeps its meaning
// if the harness ever stops doing that.
if (await visible('[data-testid="recovery-codes"]', 5000)) {
  const codeCount = await page.locator('[data-testid="recovery-codes"] .rc').count()
  check(codeCount === 10, `ten recovery codes are minted for the account's first factor (saw ${codeCount})`)
  await page.click('button:has-text("I have saved these")')
} else {
  check(
    await visible('text=Your recovery codes were already issued'),
    'a passkey added beside an existing factor reuses the recovery codes already minted',
  )
  await page.click('button:has-text("Close")')
  await page.keyboard.press('Escape')
}
check(!(await visible('[aria-label="Passkeys"]', 2000)), 'the dialog is closed once the passkey is added')

await openAccountMenu(page)
check(
  await visible('.account .menu button.row:has-text("Passkeys · 1")'),
  'the account menu shows the new passkey immediately, with no reload',
)
await page.keyboard.press('Escape')

// --- Sign out, then back in with the password and the passkey ------------

await openAccountMenu(page)
await page.click('.account .menu button.row:text-is("Sign out")')
await page.waitForSelector('input[autocomplete="username"]', { timeout: 15000 })

await page.fill('input[autocomplete="username"]', USER)
await page.fill('input[autocomplete="current-password"]', PASS)
await page.click('button[type="submit"]')

// A right password on an account holding a factor does not sign the
// caller in on its own (docs/plans/passkeys-second-factor.md,
// "handleAuthLogin's gate widens ... to HasSecondFactor()") -- the door
// asks for the second step instead. This account holds both kinds since
// #1253 (scripts/live-env.sh enrols an authenticator app at startup),
// and the passkey is offered first here because this scenario drives
// MV_PUBLIC_URL, where the credential is usable.
check(
  await visible('button:has-text("Use your passkey")'),
  'the second step offers "Use your passkey" to an account that holds one',
)
await page.click('button:has-text("Use your passkey")')

// Success re-checks the session client-side (authState.loginWithPasskey
// -> check()) rather than reloading, so the shell itself is the
// signal -- the same marker session() itself waits on after signing in.
await page.waitForSelector('#main-content', { timeout: 15000 })
check(await visible(`.account button.chip:has-text("${USER}")`), 'the passkey completes sign-in as the same account')

await openAccountMenu(page)
check(
  await visible('.account .menu button.row:has-text("Passkeys · 1")'),
  'the account menu still reflects the passkey after signing back in with it',
)

// --- Clean up: leave the account exactly as this run found it ------------
// Removing the only passkey also clears the recovery codes it minted
// (DeletePasskey: "if no factor of either kind remains afterwards,
// clear RecoveryCodes too"), so this restores the "no factor" account
// the run started from -- the same reasoning live-change-password
// restores the password it changed.

await page.click('.account .menu button.row:has-text("Passkeys · 1")')
check(await visible('[aria-label="Passkeys"]'), 'the Passkeys dialog reopens to remove it')
await page.click('.pk-remove')
check(await visible('input[type="password"]'), 'removing asks for the password')
await page.fill('input[type="password"]', PASS)
await page.click('button:has-text("Remove passkey")')
check(await visible('text=Add a passkey'), 'the list is empty again after removing it')
await page.keyboard.press('Escape')

await openAccountMenu(page)
check(
  await visible('.account .menu button.row:text-is("Passkeys")'),
  'the account menu drops the count tag once the passkey is gone',
)
await page.keyboard.press('Escape')

done()
