// SPDX-License-Identifier: AGPL-3.0-only
//
// #294 item 4: changing your own password from the interface, which had
// no route at all -- it meant `-recover-admin-account` and host access.
//
// Driven live rather than only at the HTTP layer because the property
// that matters is "the session you are using survives and every other
// one does not", and that spans the session store, the cookie, and
// PasswordChangedAt's cross-process invalidation. A unit test can assert
// each; only a real browser plus a second real client shows the
// combination behaving.
//
// Runs last against the shared instance on purpose: it changes the admin
// password, so anything after it that expects MV_PASS to work would
// fail. It changes it back at the end regardless, and asserts that it
// did.

import { chromium } from 'playwright'
import { session, check, done, openAtlas } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
const NEW_PASS = PASS + '-rotated'

const { page } = await session()

async function api(client, method, path_, body) {
  const res = await client.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
}

// A second signed-in browser, genuinely separate: its own context, its
// own cookie jar. This is the session that must not survive.
const browser = await chromium.launch()
const otherCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const other = await otherCtx.newPage()
await other.goto(URL_BASE, { waitUntil: 'networkidle' })
const otherLogin = await api(other.request, 'POST', '/api/auth/login', { username: USER, password: PASS })
check(otherLogin.status === 200, `a second browser signs in (${otherLogin.status})`)
// GET /api/definitions is used below purely as an "is this session still
// an authenticated admin" probe -- any admin-gated read would do, and
// this is the one every other scenario in this directory already
// exercises directly, so a session-store or cookie regression here would
// not be a novel failure mode. #407 retired the watchlist entries route
// this used to probe with; the admin gate it enforced carries over
// identically onto /api/definitions.
check(
  (await api(other.request, 'GET', '/api/definitions')).status === 200,
  'the second browser has a working session before the change',
)

// --- Refusals ------------------------------------------------------------

check(
  (await api(page.request, 'POST', '/api/auth/password', { currentPassword: 'wrong', newPassword: NEW_PASS })).status === 401,
  'a wrong current password is refused',
)
check(
  (await api(page.request, 'POST', '/api/auth/password', { currentPassword: PASS, newPassword: 'short' })).status === 400,
  'a too-short new password is refused',
)
check(
  (await api(page.request, 'POST', '/api/auth/password', { currentPassword: PASS, newPassword: PASS })).status === 400,
  'reusing the current password is refused',
)
// Still the old password after every refusal.
check(
  (await api(page.request, 'GET', '/api/definitions')).status === 200,
  'the caller is still signed in after the refusals',
)

// --- The real change -----------------------------------------------------

const changed = await api(page.request, 'POST', '/api/auth/password', {
  currentPassword: PASS,
  newPassword: NEW_PASS,
})
check(changed.status === 200, `the password is changed (${changed.status})`)

check(
  (await api(page.request, 'GET', '/api/definitions')).status === 200,
  'the browser that made the change is still signed in -- being signed out by your own action is what stops people doing it',
)
check(
  (await api(other.request, 'GET', '/api/definitions')).status !== 200,
  'the other browser is signed out -- this is the "sign out everywhere" a suspected theft needs',
)

// The new password is the one that works now.
const reLogin = await api(other.request, 'POST', '/api/auth/login', { username: USER, password: PASS })
check(reLogin.status === 401, 'the old password no longer signs in')
const newLogin = await api(other.request, 'POST', '/api/auth/login', { username: USER, password: NEW_PASS })
check(newLogin.status === 200, `the new password signs in (${newLogin.status})`)

// --- The atlas entry an operator actually uses --------------------------
// The account actions live in the atlas's account group since #633
// retired the rail (and its popover) wholesale.

await page.reload({ waitUntil: 'networkidle' })
await openAtlas(page)
check(
  await page.isVisible('.atlas button.port:has-text("Change password")'),
  'the atlas offers Change password',
)
await page.click('.atlas button.port:has-text("Change password")')
check(await page.isVisible('[aria-label="Change password"]'), 'the dialog opens')
check(
  await page.isVisible('text=signed out'),
  'the dialog says other sessions will be signed out before you do it, not after',
)
await page.keyboard.press('Escape')

// --- Put it back ---------------------------------------------------------

const restored = await api(page.request, 'POST', '/api/auth/password', {
  currentPassword: NEW_PASS,
  newPassword: PASS,
})
check(restored.status === 200, `the password is restored for the rest of the run (${restored.status})`)

await browser.close()
done()
