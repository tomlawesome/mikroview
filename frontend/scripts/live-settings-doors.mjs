// SPDX-License-Identifier: AGPL-3.0-only
//
// #767 (round 32): keys and people, mounted directly in the Settings
// card -- docs/design/concepts/round-32/settings-doors.html's #keys
// (under ingest) and #people (under account), replacing the retired
// EngineRoomDoors.svelte and its USERS_DOOR_ENABLED/TOKENS_DOOR_ENABLED
// flags outright.
//
// Driven end to end in a real browser because none of this is visible
// from a unit test with a mocked store:
//
//  - the once-only reveal actually holds the server's real one-time
//    `value`, and the RouterOS lines it offers actually embed it;
//  - arm-then-confirm actually takes two real clicks, not a
//    window.confirm() dialog, and a stray click actually disarms it;
//  - a freshly let-in account can actually sign in and reach the app.

import { session, check, done, goTo, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

const KEYS = '#keys'
const PEOPLE = '#people'

await goTo(page, 'Settings')
check(await page.locator(`${KEYS} h3`).isVisible(), 'the keys group renders for an admin')
check(await page.locator(`${PEOPLE} h3`).isVisible(), 'the people group renders for an admin')

// --- keys: the list, from GET /api/tokens --------------------------------

const before = await page.request
  .get(`${URL_BASE}/api/tokens`)
  .then((r) => r.json())
  .then((b) => b.tokens ?? [])
const rowCount = await page.locator(`${KEYS} .prow`).count()
check(
  rowCount === before.length,
  `the keys list matches GET /api/tokens -- ui shows ${rowCount}, api holds ${before.length}`,
)

// --- keys: mint an ingest key, see the once-only reveal ------------------

const known = await page.request
  .get(`${URL_BASE}/api/devices`)
  .then((r) => r.json())
  .then((b) => (b.devices ?? []).map((d) => d.id))
check(known.length > 0, `the instance knows at least one device (${JSON.stringify(known)})`)
const DEVICE = known[0]
const KEY_NAME = 'live-settings-doors-key'

await page.click(`${KEYS} .ogfoot .olink`)
await page.waitForSelector(`${KEYS} .pform`)
await page.fill(`${KEYS} .pform input[aria-label="key name"]`, KEY_NAME)
await page.click(`${KEYS} .pform .seg[aria-label="Key kind"] button:has-text("ingest")`)
// By id, not text: the picker shows the device's name ("Live Router")
// where it has one, and the id is what the chip and the API carry.
await page.click(`${KEYS} .pform .seg[aria-label="which router"] button[data-device-id="${DEVICE}"]`)
await page.click(`${KEYS} .pform button:has-text("mint it")`)

await page.waitForSelector(`${KEYS} .reveal code`)
const mintedValue = (await page.textContent(`${KEYS} .reveal code`))?.trim() ?? ''
check(mintedValue.length > 0, 'the reveal shows the new key exactly once')
check(
  (await page.textContent(`${KEYS} .reveal`))?.includes('shown once') ?? false,
  'the reveal names its own once-only nature',
)
check(
  await page.locator(`${KEYS} .reveal button:has-text("copy for RouterOS")`).isVisible(),
  'an ingest key offers copy for RouterOS',
)

// The clipboard actually holds the raw value, and the RouterOS lines
// actually embed it -- not just that a button with the right label
// exists.
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: URL_BASE })
await page.click(`${KEYS} .reveal button:has-text("copy for RouterOS")`)
const routerScript = await page.evaluate(() => navigator.clipboard.readText())
check(
  routerScript.includes(mintedValue),
  'copy for RouterOS puts a script containing the freshly minted key on the clipboard',
)
check(
  routerScript.includes('/tool fetch') && routerScript.includes('/api/ingest/routeros'),
  'the copied script is a real RouterOS push block, pointed at this instance',
)

// done lets the ordinary row take the reveal's place.
await page.click(`${KEYS} .reveal button:has-text("done")`)
check(
  await page
    .locator(`${KEYS} .prow:has-text("${KEY_NAME}") .pr:has-text("ingest · speaks for ${DEVICE}")`)
    .waitFor({ timeout: 10000 })
    .then(() => true, () => false),
  'the minted key settles into an ordinary row once done is clicked',
)
check((await page.locator(`${KEYS} .reveal`).count()) === 0, 'the reveal is gone once done is clicked')

// --- keys: revoke arms, then confirms ------------------------------------

const revoke = page.locator(`${KEYS} .prow:has-text("${KEY_NAME}") .revoke`)
await revoke.click()
check(
  (await revoke.textContent())?.trim() === 'confirm — it stops speaking now',
  'a first click arms revoke rather than acting immediately',
)

// A click elsewhere disarms it -- the row must still be there.
await page.click(`${KEYS} h3`)
check(
  (await revoke.textContent())?.trim() === 'revoke',
  'clicking elsewhere disarms revoke without acting',
)
check(
  await page.locator(`${KEYS} .prow:has-text("${KEY_NAME}")`).isVisible(),
  'the key row survives a disarmed revoke',
)

await revoke.click()
await revoke.click()
await page.waitForSelector(`${KEYS} .prow:has-text("${KEY_NAME}")`, { state: 'detached' })
check(true, 'a second click on the armed button revokes the key')

const afterRevoke = await page.request.get(`${URL_BASE}/api/tokens`).then((r) => r.json())
check(
  !(afterRevoke.tokens ?? []).some((t) => t.name === KEY_NAME),
  'the revoked key is gone from the server, not just the UI',
)

// --- people: the list, from GET /api/auth/users --------------------------

const usersBefore = await page.request.get(`${URL_BASE}/api/auth/users`).then((r) => r.json())
const peopleRowCount = await page.locator(`${PEOPLE} .prow`).count()
check(
  peopleRowCount === usersBefore.length,
  `the people list matches GET /api/auth/users -- ui shows ${peopleRowCount}, api holds ${usersBefore.length}`,
)
const admin = usersBefore.find((u) => u.role === 'admin')
check(
  await page
    .locator(`${PEOPLE} .prow:has-text("${admin?.username ?? ''}") .olink.quiet.dim:has-text("console-only")`)
    .isVisible(),
  "the admin's own row ends console-only, not remove",
)

// --- people: let a viewer in ---------------------------------------------

const VIEWER_USER = 'live-settings-doors-viewer'
const VIEWER_PASS = 'live-settings-doors-viewer-password'

await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, VIEWER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, VIEWER_PASS)
await page.click(`${PEOPLE} .pform button:has-text("can only look")`)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`)
check(
  await page.isVisible(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .pr:has-text("can only look")`),
  'the new account is marked read-only in the people group',
)

// The account actually works, not just that a row appeared.
const browser = await launchBrowser()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
const signedIn = await viewerPage
  .waitForSelector('#main-content', { timeout: 15000 })
  .then(() => true, () => false)
check(signedIn, 'the freshly let-in viewer account can actually sign in')
await browser.close()

// --- people: remove arms, then confirms -----------------------------------

const remove = page.locator(`${PEOPLE} .prow:has-text("${VIEWER_USER}") .remove`)
await remove.click()
check(
  (await remove.textContent())?.trim() === 'confirm — signs them out, revokes their keys',
  'a first click arms remove rather than acting immediately',
)

await page.click(`${PEOPLE} h3`)
check(
  (await remove.textContent())?.trim() === 'remove',
  'clicking elsewhere disarms remove without acting',
)

await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${VIEWER_USER}")`, { state: 'detached' })
check(true, 'a second click on the armed button removes the account')

const usersAfter = await page.request.get(`${URL_BASE}/api/auth/users`).then((r) => r.json())
check(
  !usersAfter.some((u) => u.username === VIEWER_USER),
  'the removed account is gone from the server, not just the UI',
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
