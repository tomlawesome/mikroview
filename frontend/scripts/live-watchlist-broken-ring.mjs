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
//   own poll (see its #546 comment). This scenario never navigates off
//   Stream, so nothing short of the real poll landing can make it pass.
// - "hiding the label tightens the ring to the icon" is a layout claim:
//   the outline is drawn outside the item's border box via
//   outline-offset, inside a rail whose own overflow-y:auto forces
//   overflow-x to clip too -- the same trap that clipped #545's tooltip
//   and #546's own count badge. Only a real layout engine can say whether
//   it survives.
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
 * App.svelte's 5s poll landing mid-check cannot make a correct ring look
 * wrong. Mirrors live-nav-badge.mjs's settledCount for the same reason.
 */
async function settledRing(timeoutMs = 15000) {
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
