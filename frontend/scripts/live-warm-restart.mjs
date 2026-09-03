// SPDX-License-Identifier: AGPL-3.0-only
//
// #795, design round 41: what a surface says about its own provenance
// while the process is not yet fully live.
//
// The gate's instance is always freshly booted with no snapshot to load,
// so what this scenario can exercise end to end is the **cold** half:
// `counting since HH:MM — nothing before`, on the metrics hourline and
// in the docket's clear-all row, both naming the same minute. The warm
// half (`restored to HH:MM · live since HH:MM`) needs a restart with a
// snapshot on disk, which no scenario here can stand up; it is covered
// by lib/provenance.test.ts and by the two component tests instead, and
// the issue's own "done when" is checked by killing a real instance.
//
// What is only provable in a real browser, and is the reason this file
// exists:
//
//  - The two surfaces agree. They are separate components, mounted on
//    separate deck cards, and the whole point of putting the derivation
//    in lib/provenance.ts is that they cannot word it differently or
//    name different times. A unit test renders one component at a time
//    and can never see the two disagree; this navigates between them in
//    one session and compares the strings.
//  - The statement is real text on a real page, in the place the design
//    puts it -- last in the hourline's rate group, and in the clear-all
//    row rather than inside a tab's panel.
//  - The minute it names is the server's own `liveSince`, not something
//    the client made up. Checked against GET /api/stats directly.
//
// Note on waiting: Svelte 5 applies state to the DOM in a microtask, so
// every read below happens after its own wait, never in the same step as
// the navigation that caused it (the trap live-topography-edges.mjs
// records at length).

import { session, feedSyslog, check, responsive, done, goTo } from './live-browser.mjs'

// Enough traffic that the hourline has an hour to talk about at all --
// `.rate` renders regardless, but a page with no minutes on its axis is
// not the page the design was drawn against.
feedSyslog(60, 'warm-restart')

const { page, consoleErrors } = await session({ waitForEvents: 20 })

/** apiUrl resolves a path against the page's own origin, for page.request. */
function apiUrl(path) {
  return new URL(path, page.url()).toString()
}

// Mirrors lib/format.ts's formatHM, the same way live-metrics-views.mjs
// does: Node and the browser share one OS clock and timezone in this
// harness, which is what lets a stamp read off the API be compared
// against the HH:MM the page printed.
function hmLabel(iso) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

// --- What the server actually says about its own start -------------------
const statsRes = await page.request.get(apiUrl('/api/stats'))
check(statsRes.ok(), `GET /api/stats responds -- status ${statsRes.status()}`)
const stats = await statsRes.json()

check(typeof stats.liveSince === 'string', `the stats payload carries liveSince -- got ${JSON.stringify(stats.liveSince)}`)
// A cold start omits the key entirely rather than sending null, so its
// absence is the answer. A null here would mean the server had started
// special-casing "not restored", which is exactly what the wire shape
// was chosen to avoid.
check(
  !('restoredTo' in stats) || stats.restoredTo === undefined,
  `a freshly booted instance sends no restoredTo -- got ${JSON.stringify(stats.restoredTo)}`,
)

const bootMinute = hmLabel(stats.liveSince)
const COLD = `counting since ${bootMinute} — nothing before`
const ageMinutes = (Date.now() - new Date(stats.liveSince).getTime()) / 60000
// The statement clears sixty minutes after boot. The gate's instance is
// minutes old when the scenarios run, so this is a guard against reading
// a silent page as a passing one rather than a real risk.
check(ageMinutes < 55, `the instance is young enough to still be saying it -- up ${ageMinutes.toFixed(1)} minutes`)

// --- The metrics hourline: the last fact in the rate group ---------------
await goTo(page, 'Metrics')
const metricsStmt = page.locator('.metrics .hourline .rate .fact.stmt')
await metricsStmt.waitFor({ state: 'visible', timeout: 10000 })
const metricsText = (await metricsStmt.textContent()).replace(/\s+/g, ' ').trim()
check(metricsText === COLD, `the hourline states the cold start -- got "${metricsText}", expected "${COLD}"`)

// Last, not merely present: it is a fact about the other facts, so it
// reads after them. `.rate`'s last element child is the assertion,
// because "somewhere in the group" would pass with it drawn first.
const lastInRate = await page.$eval('.metrics .hourline .rate', (el) => ({
  tag: el.lastElementChild?.tagName.toLowerCase(),
  cls: el.lastElementChild?.className,
  buttons: el.querySelectorAll('button').length,
}))
check(
  (lastInRate.cls ?? '').split(/\s+/).includes('stmt'),
  `the statement is the last fact on the hourline -- got ${JSON.stringify(lastInRate)}`,
)
// A fact, not a control: nothing in the rate group is clickable.
check(
  lastInRate.tag === 'span' && lastInRate.buttons === 0,
  `the statement is a fact, not a control -- got ${JSON.stringify(lastInRate)}`,
)

// --- The docket: the same words, as a dim chip in the clear-all row ------
//
// Checked on all three tabs. The chip sits outside the tab gating in
// Docket.svelte on purpose -- every tab's contents came out of the same
// restarted process -- and "present on every tab" is a claim about
// navigation, so it is checked by navigating.
for (const tab of ['Flags', 'Watchlist', 'Audit log']) {
  await goTo(page, tab)
  const chip = page.locator('.docket .clear-row .att.dim')
  await chip.waitFor({ state: 'visible', timeout: 10000 })
  const text = (await chip.textContent()).replace(/\s+/g, ' ').trim()
  check(text === COLD, `the docket's ${tab} tab states the cold start -- got "${text}"`)

  const shape = await chip.evaluate((el) => ({
    tag: el.tagName.toLowerCase(),
    inButton: el.closest('button') !== null,
    marker: el.querySelector('i') !== null,
  }))
  check(
    shape.tag === 'span' && !shape.inButton && shape.marker,
    `the ${tab} chip is a statement, not a control -- got ${JSON.stringify(shape)}`,
  )
}

// --- The two surfaces agree, which is the whole point of one derivation --
//
// Re-read the hourline after the docket rather than trusting the string
// captured before the navigation: the round trip is what would expose a
// surface deriving its own answer from its own clock.
await goTo(page, 'Metrics')
await metricsStmt.waitFor({ state: 'visible', timeout: 10000 })
const metricsAgain = (await metricsStmt.textContent()).replace(/\s+/g, ' ').trim()
await goTo(page, 'Flags')
const docketChip = page.locator('.docket .clear-row .att.dim')
await docketChip.waitFor({ state: 'visible', timeout: 10000 })
const docketAgain = (await docketChip.textContent()).replace(/\s+/g, ' ').trim()
check(
  metricsAgain === docketAgain,
  `both surfaces say the same thing -- hourline "${metricsAgain}", docket "${docketAgain}"`,
)
check(
  metricsAgain.includes(bootMinute) && docketAgain.includes(bootMinute),
  `both name the server's own liveSince minute (${bootMinute}) -- hourline "${metricsAgain}", docket "${docketAgain}"`,
)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
