// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283: preferences (saved filter presets among them) move to a
// per-user record on the server -- what a unit test cannot show is the
// actual point of the change: sign out, sign in as someone else on the
// *same* machine, and see nothing of the previous account. This drives
// a real sign-out/sign-in cycle in one real tab (mirrors live-door.mjs's
// own real-sign-out plumbing) between two real accounts (mirrors
// live-viewer-surfaces.mjs's createAccount, from the people door) and
// proves the round trip both ways: user A's preset is invisible to user
// B, and still there when A signs back in.
//
// NOT RUN as part of writing this: the backend half of #1283
// (GET/PUT /api/me/preferences) does not exist in this worktree, so
// nothing here has executed against a real server. node --check only.
// The integrator should run this once the backend lands.

import { session, check, done, goTo, openAccountMenu, completeSecondFactor, enrolFactorAndSignIn } from './live-browser.mjs'

const ADMIN_USER = process.env.MV_USER
const ADMIN_PASS = process.env.MV_PASS

const CARD = '.card[aria-hidden="false"]'
const BOX = `${CARD} .filterline .fbox`
const PEOPLE = '#people'

const PEER_USER = 'live-preferences-roam-1283'
const PEER_PASS = 'live-preferences-roam-1283-password'
const PRESET_NAME = 'mv1283 roam'
const RULE_FILTER = 'mv1283-scan'

const { page, consoleErrors } = await session()

// --- A second real account, created the way an admin actually would ----
// (mirrors live-viewer-surfaces.mjs's own createAccount).

await goTo(page, 'Settings')
await page.click(`${PEOPLE} .ogfoot .olink`)
await page.waitForSelector(`${PEOPLE} .pform`)
await page.fill(`${PEOPLE} .pform input[aria-label="username"]`, PEER_USER)
await page.fill(`${PEOPLE} .pform input[aria-label="password"]`, PEER_PASS)
await page.click(`${PEOPLE} .pform button:has-text("let them in")`)
await page.waitForSelector(`${PEOPLE} .prow:has-text("${PEER_USER}")`)
check(true, `a second account "${PEER_USER}" is created from the people door`)

// --- User A (the admin session() signed in as) saves a preset ----------

await goTo(page, 'Stream')
page.on('dialog', (d) => d.accept(PRESET_NAME))
await page.fill('input.rule', RULE_FILTER)

await page.click(`${BOX} .fsaved`)
// Waits out whatever reactivity delay there is between the filter
// landing and appState.hasActiveFilters flipping true, which is what
// gates "save this filter as…" rendering at all (FilterPresetsMenu's
// own {#if}) -- a fixed sleep would either race it or overshoot.
await page.waitForSelector(`${BOX} .fpmenu .fpsave`, { timeout: 5000 })
await page.click(`${BOX} .fpmenu .fpsave`)
await page.waitForSelector(`${BOX} .fpmenu`, { state: 'detached' })
check(true, `user A saves a preset named "${PRESET_NAME}"`)

async function signOut(p) {
  await openAccountMenu(p)
  // The reload only fires once logout()'s whole async chain has
  // resolved -- preferencesState.flush() (the still-pending debounced
  // write), then the server call, then the local reset (auth.svelte.ts)
  // -- so waiting for it is what actually proves the preset reached the
  // server before this tab forgets who it was signed in as, with no
  // separate sleep needed for the debounce.
  const reloaded = p.waitForEvent('load', { timeout: 10000 })
  await p.click('.account .menu button.row:text-is("Sign out")')
  await reloaded
  await p.waitForSelector('.screen', { timeout: 10000 })
}

// enrol: true for the peer account, created moments ago through the
// people door and holding no factor at all (#1335 pattern A) -- false
// (the default) for the admin session() already signed in once, whose
// factor from `scripts/live-env.sh up` just needs completing again
// (pattern B).
async function signInHere(p, username, password, { enrol = false } = {}) {
  await p.fill('input[autocomplete="username"]', username)
  await p.fill('input[autocomplete="current-password"]', password)
  await p.click('button[type="submit"]')
  if (enrol) {
    await enrolFactorAndSignIn(p)
  } else {
    await completeSecondFactor(p)
    await p.waitForSelector('#main-content', { timeout: 15000 })
  }
}

async function savedPresetNames(p) {
  await goTo(p, 'Stream')
  await p.click(`${BOX} .fsaved`)
  await p.waitForSelector(`${BOX} .fpmenu`, { state: 'visible', timeout: 5000 })
  const names = await p.$$eval(`${BOX} .fpmenu .fpname`, (els) => els.map((e) => e.textContent.trim()))
  await p.keyboard.press('Escape')
  await p.waitForSelector(`${BOX} .fpmenu`, { state: 'detached', timeout: 5000 })
  return names
}

// --- Sign out, sign in as user B on the same tab: no preset -----------

await signOut(page)
check(true, 'user A signs out')

await signInHere(page, PEER_USER, PEER_PASS, { enrol: true })
check(true, 'user B signs in, on the same tab user A just used')

const peerPresets = await savedPresetNames(page)
check(
  !peerPresets.includes(PRESET_NAME),
  `user B's saved filters carry nothing of user A's -- got ${JSON.stringify(peerPresets)}`,
)

// --- Sign back in as user A: the preset followed the account ----------

await signOut(page)
check(true, 'user B signs out')

await signInHere(page, ADMIN_USER, ADMIN_PASS)
check(true, 'user A signs back in, on the same tab')

const aPresetsAgain = await savedPresetNames(page)
check(
  aPresetsAgain.includes(PRESET_NAME),
  `user A's own preset is exactly where it was left -- got ${JSON.stringify(aPresetsAgain)}`,
)

// --- Clean up: forget the preset, remove the peer account --------------

await page.click(`${BOX} .fsaved`)
await page.waitForSelector(`${BOX} .fpmenu`, { state: 'visible', timeout: 5000 })
await page.click(`${BOX} .fpmenu .fprow:has-text("${PRESET_NAME}") .fpx`)
await page.keyboard.press('Escape')

await goTo(page, 'Settings')
const remove = page.locator(`${PEOPLE} .prow:has-text("${PEER_USER}") .remove`)
await remove.click()
await remove.click()
await page.waitForSelector(`${PEOPLE} .prow:has-text("${PEER_USER}")`, { state: 'detached' })
check(true, `the peer account "${PEER_USER}" is removed again`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
