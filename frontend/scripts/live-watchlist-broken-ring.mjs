// SPDX-License-Identifier: AGPL-3.0-only
//
// #546: the broken ring. What needs a real browser rather than a unit test:
//
// - The ring has to agree with the *server's own* coverage answer, not a
//   fixture. #546's own issue history is the reason: coverage moved from
//   the watchlist endpoint to the definitions endpoint mid-project, and a
//   unit test asserting the ring follows whatever watchlistState.coverage
//   happens to hold would keep passing even if that wiring silently broke.
// - The ring is chrome, not page content -- it has to appear and clear
//   without the operator ever opening Watchlist, driven by App.svelte's
//   own poll (see its #546 comment). Both the appearing and the clearing
//   are checked from Stream, so nothing short of the real poll landing
//   can make either pass. (The #583 leg below does open Watchlist once,
//   deliberately and only after the ring has already appeared, to prove
//   the group ring's claim is resolved by the next tap.)
// - "hiding the label tightens the ring to the icon" is a layout claim:
//   the outline is drawn outside the item's border box via
//   outline-offset, inside a rail whose own overflow-y:auto forces
//   overflow-x to clip too -- the same trap that clipped #545's tooltip
//   and #546's own count badge. Only a real layout engine can say whether
//   it survives.
// - #583 put the same ring on the small-screen bottom bar, on the
//   *group*. The breakpoint is a live matchMedia listener jsdom does not
//   implement, so no unit test in this repo can tell a phone viewport
//   from a desktop one; and "the ring tightens to the icon alone rather
//   than icon + word" is a layout fact about a flex column, not a prop.
//   Driving that leg here rather than in live-nav-bottom-bar.mjs is
//   forced by ordering: it needs a pushed filter table to make coverage
//   'no-logging', and live-router-lookup.mjs -- which sorts before every
//   live-nav-* scenario -- asserts that no table has been pushed yet.
//
// Named and sorted alongside the other watchlist scenarios, not the nav
// ones, and deliberately: live-router-lookup.mjs (which sorts earlier)
// has its own "before any push, no table exists yet" check against the
// one shared device every scenario here pushes through, so nothing may
// push a filter-rule table before it runs. This scenario does not need
// to be first -- it establishes its own baseline with an explicit push
// rather than assuming a pristine one -- so it sorts after. It does keep
// live-watchlist-coverage.mjs's own downstream assumption intact: it
// ends by resetting the table to non-logging, the same "tables pushed by
// earlier scenarios have no logging rules" state that scenario documents
// needing.

import { session, feedSyslog, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

feedSyslog(3, 'broken-ring-probe')
const { page, consoleErrors } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

async function coverageFor(id) {
  const got = await api('GET', '/api/definitions')
  const d = (got.body?.definitions ?? []).find((d) => d.id === id)
  return d?.coverage
}

const device = (await api('GET', '/api/devices')).body?.devices?.[0]?.id
check(!!device, `the instance reports a device (${device})`)

const tokenRes = await api('POST', '/api/tokens', { name: 'broken-ring', kind: 'ingest', device })
check(tokenRes.status === 201, `an ingest token is issued (${tokenRes.status})`)
const token = tokenRes.body?.value

async function push(records) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'filter-rule', page: 1, pages: 1, records }),
  })
  return res.status
}

// A port nothing else in this shared instance's other scenarios watches,
// so an unrelated rule cannot accidentally cover or scope it.
const PORT = 19222

const entry = await api('POST', '/api/definitions', {
  name: 'broken ring watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [PORT] },
})
check(entry.status === 201, `an entry is created (${entry.status})`)
const id = entry.body?.id

const WATCHLIST_ITEM = '.rail .item:has(.label:text-is("Watchlist"))'
const watchlistItem = page.locator(WATCHLIST_ITEM)

/**
 * Polls the ring's DOM state and the server's coverage answer together
 * until they agree on whether this entry should be ringing, so
 * App.svelte's own coverage poll landing mid-check cannot make a correct
 * ring look wrong. Mirrors live-nav-badge.mjs's settledCount for the same
 * reason.
 *
 * The default timeout is well above App.svelte's
 * WATCHLIST_COVERAGE_REFRESH_MS (60s): unlike the flag count, coverage
 * only ever changes as fast as a pushed filter table can (RouterOS's
 * documented push scheduler is interval=20m), so the ring rides a much
 * slower poll than the 5s stats tick -- see that constant's own comment.
 * A short timeout here would just be testing this scenario's patience,
 * not a regression.
 */
async function settledRing(timeoutMs = 75000) {
  const deadline = Date.now() + timeoutMs
  let last = null
  while (Date.now() < deadline) {
    const coverage = await coverageFor(id)
    const broken = await watchlistItem.evaluate((el) => el.classList.contains('broken'))
    last = { coverage, broken }
    const expected = coverage === 'no-logging'
    if (broken === expected) return last
    await new Promise((r) => setTimeout(r, 250))
  }
  check(false, `the ring never agreed with the server's coverage answer -- last saw ${JSON.stringify(last)}`)
  return null
}

// --- Driven into no-logging: the ring appears and names the reason -------
// (Whether the ring correctly stays *off* for 'unknown'/'out-of-scope'/
// 'covered' is exercised at the unit level -- watchlist.svelte.test.ts and
// NavRail.svelte.test.ts -- against synthetic coverage, which is the
// right seat for it: this scenario's own job is agreement with a real
// server, not re-proving the predicate's branches against one.)
check(
  (await push([{ ordinal: 0, chain: 'forward', action: 'drop' }])) === 200,
  'a filter table with no logging rules is accepted',
)
let settled = await settledRing()
check(settled?.coverage === 'no-logging', `the server now says no-logging (got ${settled?.coverage})`)
check(settled?.broken === true, 'the ring follows the server into no-logging')

const spokenBroken = await watchlistItem.getAttribute('aria-label')
check(
  spokenBroken === "Watchlist — 1 watch can't be checked: the firewall rules it needs aren't being logged",
  `the ring names the count and the cause, singular, in plain operator language -- got ${JSON.stringify(spokenBroken)}`,
)
check(
  !/coverage|no-logging/i.test(spokenBroken ?? ''),
  'the label never leaks the internal coverage vocabulary the operator never chose',
)

const ringStyle = await watchlistItem.evaluate((el) => {
  const s = getComputedStyle(el)
  return { style: s.outlineStyle, width: s.outlineWidth, offset: s.outlineOffset }
})
check(
  ringStyle.style === 'solid' && ringStyle.width === '2px' && ringStyle.offset === '3px',
  `the ring is a 2px outline at 3px offset, per the record -- got ${JSON.stringify(ringStyle)}`,
)

// --- Icons density: the ring tightens to the icon, and stays inside the rail
const fullWidth = await watchlistItem.evaluate((el) => el.getBoundingClientRect().width)
await page.click('.state-btn[aria-label^="Show icons"]')
await page.waitForFunction(
  () => Math.round(document.querySelector('.rail').getBoundingClientRect().width) === 54,
  null,
  { timeout: 5000 },
)
const iconsWidth = await watchlistItem.evaluate((el) => el.getBoundingClientRect().width)
check(
  iconsWidth < fullWidth / 2,
  `the row itself narrows at icons density (no extra CSS -- .rail.icons .label{display:none} does this) -- ${fullWidth}px -> ${iconsWidth}px`,
)

const iconsRingStyle = await watchlistItem.evaluate((el) => {
  const s = getComputedStyle(el)
  return { style: s.outlineStyle, width: s.outlineWidth, offset: s.outlineOffset }
})
check(
  iconsRingStyle.style === 'solid' && iconsRingStyle.width === '2px' && iconsRingStyle.offset === '3px',
  `the ring survives the switch to icons density -- got ${JSON.stringify(iconsRingStyle)}`,
)

// The rail scrolls on one axis, which clips the other regardless of what
// overflow-x says -- exactly the trap that clipped #545's tooltip and the
// count badge. The ring's outline extends 5px (2px width + 3px offset)
// beyond the item's own border box, so this checks that extension lands
// inside the rail rather than being cut off.
const withinRail = await page.evaluate(() => {
  const rail = document.querySelector('.rail').getBoundingClientRect()
  const item = Array.from(document.querySelectorAll('.rail .item')).find(
    (el) => el.querySelector('.label')?.textContent?.trim() === 'Watchlist',
  )
  const r = item.getBoundingClientRect()
  const RING_EXTENT = 5
  return r.left - RING_EXTENT >= rail.left && r.right + RING_EXTENT <= rail.right
})
check(withinRail, 'the ring is drawn inside the 54px rail rather than clipped by its own scroll container')

const iconsLabel = await watchlistItem.getAttribute('aria-label')
check(
  iconsLabel === "Watchlist — 1 watch can't be checked: the firewall rules it needs aren't being logged",
  `the reason still speaks at icons density, where the visible label is hidden -- got ${JSON.stringify(iconsLabel)}`,
)

await page.click('.state-btn[aria-label^="Show icons"]')

// --- Small screens (#583): the same alarm, on the bar of groups ----------
// A phone-only operator is not a lesser operator: the ring's guarantee
// cannot be conditional on screen width. The bar shows groups rather than
// pages, so Expect wears it and makes the weaker claim -- "an answer
// behind this group cannot be trusted" -- which the record allows only
// because the next tap resolves it. That resolution is checked at the end
// of this leg.
//
// The record's second ring, on the page row inside the half-sheet, cannot
// be driven here and it is not an omission: Expect holds Watchlist alone,
// so tapping it goes straight to the page and no sheet is ever raised for
// the one group that can ring today. BottomBar.svelte.test.ts covers that
// row against a stand-in second page.
await page.setViewportSize({ width: 390, height: 844 })
const bar = page.locator('.bottom-bar')
await bar.waitFor({ timeout: 5000 })
check((await page.$$('.rail')).length === 0, 'at 390px the rail is gone and the bar is the whole of navigation')

const expectGroup = page.locator('.bottom-bar .group-btn:has(.label:text-is("Expect"))')
const ringedIcon = expectGroup.locator('.icon-slot.broken')
check(
  await ringedIcon.waitFor({ timeout: 5000 }).then(
    () => true,
    () => false,
  ),
  'the Expect group wears the ring on the bar, without the operator opening anything',
)

const barRing = await ringedIcon.evaluate((el) => {
  const s = getComputedStyle(el)
  return { style: s.outlineStyle, width: s.outlineWidth, offset: s.outlineOffset }
})
check(
  barRing.style === 'solid' && barRing.width === '2px' && barRing.offset === '3px',
  `the bar's ring is the same 2px outline at 3px offset as the rail's -- got ${JSON.stringify(barRing)}`,
)

// Tightened to the icon, per the record: five groups across a phone-width
// bar leave a 2px outline at 3px offset nowhere to go around a stacked
// icon-and-word. The outline extends 5px (2px width + 3px offset) past the
// icon's own box, so this also checks it does not spill into the
// neighbouring group's button.
const boxes = await expectGroup.evaluate((el) => {
  // Plain numbers rather than the DOMRects themselves: a DOMRect's
  // properties live on its prototype, so one handed back across the
  // evaluate boundary arrives as an empty object.
  const plain = (r) => ({ left: r.left, right: r.right, width: r.width })
  return {
    btn: plain(el.getBoundingClientRect()),
    icon: plain(el.querySelector('.icon-slot').getBoundingClientRect()),
    label: plain(el.querySelector('.label').getBoundingClientRect()),
  }
})
const RING_EXTENT = 5
check(
  boxes.icon.width + 2 * RING_EXTENT < boxes.btn.width,
  `the ring goes round the icon alone, not icon + label -- icon ${Math.round(boxes.icon.width)}px inside a ${Math.round(boxes.btn.width)}px button`,
)
check(
  boxes.icon.width < boxes.label.width,
  `the label is wider than what the ring wraps, so the ring is demonstrably not around both -- icon ${Math.round(boxes.icon.width)}px vs label ${Math.round(boxes.label.width)}px`,
)
check(
  boxes.icon.left - RING_EXTENT >= boxes.btn.left && boxes.icon.right + RING_EXTENT <= boxes.btn.right,
  'the ring stays inside its own group button rather than crowding its neighbours',
)

const spokenGroup = await expectGroup.getAttribute('aria-label')
check(
  spokenGroup === "Expect — 1 watch can't be checked: the firewall rules it needs aren't being logged",
  `the group speaks the same sentence about a bigger subject -- got ${JSON.stringify(spokenGroup)}`,
)
check(
  !/coverage|no-logging/i.test(spokenGroup ?? ''),
  'and it leaks no more internal vocabulary on the bar than it does on the rail',
)

const ringedGroups = await page.$$eval('.bottom-bar .group-btn:has(.icon-slot.broken) .label', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  JSON.stringify(ringedGroups) === JSON.stringify(['Expect']),
  `only the group the break is behind rings -- got ${JSON.stringify(ringedGroups)}`,
)

// The deferral is honest only because the next tap resolves it. Expect
// holds one page today, so the tap lands straight on Watchlist -- the
// third and narrowest reading of the same sentence, the entry itself.
await expectGroup.click()
check(
  await page
    .locator('.watchlist-page')
    .waitFor({ timeout: 5000 })
    .then(
      () => true,
      () => false,
    ),
  'tapping the ringed group resolves the claim: it lands on the page that carries the break',
)

// Back to Stream and back to a desk-width viewport, so the clearing below
// is still the rail reading App.svelte's own poll rather than a page
// refetch of its own.
await page.click('.bottom-bar .group-btn .label:text-is("Live")')
await page.waitForFunction(
  () => document.querySelector('.bottom-bar .group-btn[aria-current="page"] .label')?.textContent.trim() === 'Live',
  null,
  { timeout: 5000 },
)
await page.setViewportSize({ width: 1280, height: 720 })
await watchlistItem.waitFor({ timeout: 5000 })

// --- Driven back out: covered, and the ring clears with no acknowledge ---
check(
  (await push([{ ordinal: 0, chain: 'forward', action: 'drop', log: true, dstPort: String(PORT) }])) === 200,
  'a filter table with a logging rule covering the entry is accepted',
)
settled = await settledRing()
check(settled?.coverage === 'covered', `the server now says covered (got ${settled?.coverage})`)
check(settled?.broken === false, 'the ring clears the moment coverage recovers -- a live reading, not a record')
check(
  (await watchlistItem.getAttribute('aria-label')) === null,
  'and the aria-label override drops with it, back to the plain row',
)

// The bar clears with it: same store, same live reading, no per-surface
// acknowledge state to go stale on a phone.
await page.setViewportSize({ width: 390, height: 844 })
await bar.waitFor({ timeout: 5000 })
check(
  (await page.$$('.bottom-bar .icon-slot.broken')).length === 0,
  'the bar drops its ring the moment coverage recovers, just as the rail does',
)
check(
  (await expectGroup.getAttribute('aria-label')) === null,
  'and the group goes back to speaking its plain name',
)
await page.setViewportSize({ width: 1280, height: 720 })

// --- Cleanup: leave the shared instance the way live-watchlist-coverage.mjs
// expects to find it -- "tables pushed by earlier scenarios have no
// logging rules" is that scenario's own documented assumption.
check(
  (await push([{ ordinal: 0, chain: 'forward', action: 'drop' }])) === 200,
  'the table is reset to non-logging for whatever runs next',
)
await api('DELETE', `/api/definitions/${id}`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
