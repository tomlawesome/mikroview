// SPDX-License-Identifier: AGPL-3.0-only
//
// #761: the watchlist's own create, edit, remove, permit and fence, all
// in the docket's drawer -- round 31's ratified surface, built for real
// against the ratified table (#676/#680) rather than the retired
// "Manage entries" card list. What needs a real browser rather than
// vitest/jsdom is the cross-component handoff a unit test cannot see
// end to end: `+ watch` (Docket.svelte) and a flag's own `watch this
// pathway`/`watch this source` (Flags.svelte) both write into
// Watchlist.svelte's private draft state through the shared slot in
// topologyNav.svelte.ts, and only a real click through the real deck
// proves that wiring actually lands on the right tab with the right
// fields filled in.
//
// Drives: a flag's `watch this source` opens the draft filled in (item
// 3) -> `+ watch` writes a fresh one (item 1/2) -> start watching creates
// a real fenced (inverted) entry, observing by default (#243) -> real
// syslog traffic through the real ingest pipeline gives it something to
// learn (item 4) -> permit one place, then fence the rest -> edit its
// name (item 5) -> remove it (item 5, round 28's arm/confirm gesture).

import { session, feedRaw, feedPortScan, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

async function api(page, method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// A source unused by every other scenario in this directory (#590's
// collision reasoning), so a stray match from an unrelated flag can
// never land on this entry's observed list.
const MAC = 'aa:bb:cc:dd:ee:61'
const SCAN_IP = '198.51.100.161'

feedPortScan(6, SCAN_IP)

const { page, consoleErrors } = await session()

// --- Item 3: a flag writes the draft for you ------------------------
//
// Clicks land on a cell with no click handler of its own (the flag
// type's `td.fmark`, the watch's `td.k`) rather than on the row as a
// whole: several cells (the flag's own IP link, `open in stream`, the
// chevron) call stopPropagation so they can do their own thing instead
// of toggling the drawer, and a locator built from `hasText` has no way
// to avoid clicking through to one of those by accident.

await goTo(page, 'Flags')
const scanRow = page.locator('tr.frow', { hasText: SCAN_IP })
await scanRow.locator('td.fmark').waitFor({ timeout: 15000 })
await scanRow.locator('td.fmark').click()
const watchSourceBtn = page.locator('tr.drawer', { hasText: SCAN_IP }).getByRole('button', { name: 'watch this source' })
await watchSourceBtn.waitFor({ timeout: 5000 })
await watchSourceBtn.click()

await page.locator('.watchlist-page').waitFor({ timeout: 10000 })
check(await page.locator('.wt-draft').first().isVisible(), 'watch this source lands on the watchlist tab with the draft open')
const prefilledWho = await page.locator('#panel-watchlist input[aria-label="Who this watch scopes to"]').inputValue()
check(prefilledWho === SCAN_IP, `the draft is pre-filled with the flag's source (got ${JSON.stringify(prefilledWho)})`)
check(
  await page.locator('.wf-mode .mode.on', { hasText: 'fence it' }).isVisible(),
  'a source watch pre-fills as "fence it", not "expect it"',
)
await page.click('#panel-watchlist button:has-text("discard")')
await page.locator('.wt-draft').first().waitFor({ state: 'detached', timeout: 5000 })

// --- Items 1/2: `+ watch`, and start watching creates a real entry ---

await page.click('.bubble.watch')
const draftRow = page.locator('.wt-draft').first()
await draftRow.waitFor({ timeout: 5000 })

const NAME = 'live manage cam'
await page.fill('#panel-watchlist input[aria-label="Watch name"]', NAME)
await page.fill('#panel-watchlist input[aria-label="Who this watch scopes to"]', MAC)
await page.click('.wf-mode .mode:has-text("fence it")')
check(
  await page.locator('#panel-watchlist input[aria-label="Toward"]').isDisabled(),
  '"toward" greys out once fence it is chosen -- there is nowhere to name yet',
)
await page.click('button:has-text("start watching")')
await draftRow.waitFor({ state: 'detached', timeout: 10000 })

function rowFor(name) {
  return page.locator('.wt-row', { hasText: name })
}
function drawerFor(name) {
  return page.locator('.wt-drawer', { hasText: name })
}

await rowFor(NAME).waitFor({ timeout: 10000 })
check(true, 'start watching creates a real entry and the draft closes')

const created = await api(page, 'GET', '/api/definitions')
const entry = (created.body?.definitions ?? []).find((d) => d.expectation?.source?.mac?.toLowerCase() === MAC)
check(!!entry, 'the created definition is a real, fenced (inverted) expectation')
check(entry?.expectation?.observing === true, 'a new fenced watch starts observing, per #243 -- it learns before it fires')

// --- Item 4: learning, permit, and fence now -------------------------

const line1 =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${MAC}, ` +
  'proto TCP (SYN), 192.168.1.61:51234->203.0.113.61:443, len 60'
const line2 =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${MAC}, ` +
  'proto TCP (SYN), 192.168.1.61:51235->203.0.113.62:8443, len 60'
feedRaw(line1)
feedRaw(line2)

await rowFor(NAME).locator('td.k').click()
const seenItems = drawerFor(NAME).locator('.seen li')

// Watchlist.svelte's own poll cadence is a 60s admin-only interval
// (App.svelte's WATCHLIST_COVERAGE_REFRESH_MS) -- too slow for a
// scenario to sit through. This component's own onMount fires the same
// refresh every time it (re)mounts, and Docket.svelte fully unmounts it
// on every tab switch away (`{#if tab === 'watchlist'} <Watchlist />`),
// so switching away and back is a fast, real "arrival" rather than a
// synthetic poke at internal state.
let sawTwo = false
for (let i = 0; i < 40 && !sawTwo; i++) {
  await page.waitForTimeout(500)
  await goTo(page, 'Flags')
  await goTo(page, 'Watchlist')
  await rowFor(NAME).locator('td.k').click()
  if ((await seenItems.count()) >= 2) sawTwo = true
}
check(sawTwo, `real ingested traffic through the real pipeline populates "where it has reached" (saw ${await seenItems.count()} places)`)
check(await drawerFor(NAME).locator('.seen').isVisible(), 'the drawer shows "where it has reached" while learning')
check(
  await page.locator('.wt-row', { hasText: NAME }).locator('.wchip2.learn', { hasText: 'learning' }).isVisible(),
  'the chip reads learning while still observing',
)

await seenItems.first().getByRole('button', { name: 'permit' }).click()
await page.waitForTimeout(300)
check(
  (await drawerFor(NAME).locator('.seen .ok', { hasText: 'permitted' }).count()) === 1,
  'permitting one place marks it permitted in the list',
)

const permitAllBtn = drawerFor(NAME).getByRole('button', { name: /permit all/ })
if (await permitAllBtn.count()) await permitAllBtn.click()
await page.waitForTimeout(300)

await drawerFor(NAME).getByRole('button', { name: /fence now/ }).click()
await page.waitForTimeout(300)
check(
  await rowFor(NAME).locator('.wchip2', { hasText: 'fencing' }).isVisible(),
  'fence now turns the chip to fencing',
)
check(await drawerFor(NAME).getByRole('button', { name: 'learn again' }).isVisible(), 'the same button now reads learn again')

// --- Item 5: edit and remove ------------------------------------------

await drawerFor(NAME).getByRole('button', { name: 'edit' }).click()
const RENAMED = 'live manage cam (renamed)'
await page.fill('.wt-drawer input[aria-label="Watch name"]', RENAMED)
await page.click('.wt-drawer button:has-text("save")')
await rowFor(RENAMED).waitFor({ timeout: 10000 })
check(true, 'edit saves the rename onto the real entry')

await rowFor(RENAMED).locator('td.k').click()
const removeBtn = drawerFor(RENAMED).getByRole('button', { name: 'remove' })
await removeBtn.click()
check(
  await drawerFor(RENAMED).getByRole('button', { name: /confirm/ }).isVisible(),
  'the first click arms remove -- "confirm — its matches stay", nothing removed yet',
)
await drawerFor(RENAMED).getByRole('button', { name: /confirm/ }).click()
await rowFor(RENAMED).waitFor({ state: 'detached', timeout: 10000 })
check(true, 'the second click actually removes the watch')

const afterDelete = await api(page, 'GET', '/api/definitions')
check(
  !(afterDelete.body?.definitions ?? []).some((d) => d.id === entry?.id),
  'the entry is really gone server-side, not just hidden client-side',
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
