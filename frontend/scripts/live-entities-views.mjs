// SPDX-License-Identifier: AGPL-3.0-only
//
// Entities as views, not tabs (#804, rounds 37-38).
//
// What needs a real browser rather than a unit test:
//
// - The switch is real furniture on a real page. #681's tab strip was
//   built, drawn nowhere, and unmounted behind a flag; the views that
//   replace it have to actually render under the router cards and
//   actually swap the table, against real seeded entities rather than a
//   mocked store.
// - The counts come from three different sources (the entity store, the
//   routers' pushed rule tables, ports seen in traffic). A unit test
//   mocks all three into agreement; only a running instance shows the
//   count beside a name matching the rows under it.
// - The descriptor lines are gone (round 38, owner's word). An absence
//   is exactly the thing a component test can pass while the built page
//   still shows it -- CSS, a stale bundle, a sibling component.
// - The views must be reachable from the keyboard. The drawing uses
//   bare spans; this build uses buttons for that reason, and a real
//   browser is the only place tabbing to one proves anything.

import { session, check, responsive, goTo, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 60 })

await goTo(page, 'Entities')
const card = '.card[aria-hidden="false"]'
await page.waitForSelector(`${card} #eviews`, { timeout: 15000 })

// --- Three views, named, counted, one underlined --------------------------
const views = await page.$$eval(`${card} #eviews [data-v]`, (els) =>
  els.map((e) => ({
    v: e.getAttribute('data-v'),
    tag: e.tagName,
    text: e.textContent.replace(/\s+/g, ' ').trim(),
    on: e.classList.contains('on'),
  })),
)
check(
  views.map((x) => x.v).join(',') === 'hosts,rules,ports',
  `the three views are hosts, rules, ports in that order -- got ${JSON.stringify(views.map((x) => x.v))}`,
)
check(
  views.every((x) => /^(hosts|rules|ports) \d+$/.test(x.text)),
  `each view carries its own count -- got ${JSON.stringify(views.map((x) => x.text))}`,
)
check(
  views.filter((x) => x.on).map((x) => x.v).join(',') === 'hosts',
  `hosts is the one underlined view on arrival -- got ${JSON.stringify(views.filter((x) => x.on).map((x) => x.v))}`,
)
check(
  views.every((x) => x.tag === 'BUTTON'),
  `every view is a button, so the keyboard reaches it -- got ${JSON.stringify(views.map((x) => x.tag))}`,
)

// Not tabs: no tablist, no tab role, none of #681's tab furniture.
check(
  (await page.locator(`${card} [role="tablist"]`).count()) === 0,
  'the entities card draws no tablist -- these are views, not tabs',
)
check((await page.locator(`${card} .tab`).count()) === 0, "none of #681's tab furniture is left on the page")

// --- No descriptor line under any view ------------------------------------
for (const v of ['hosts', 'rules', 'ports']) {
  await page.click(`${card} #eviews [data-v="${v}"]`)
  await page.waitForSelector(`${card} #eviews [data-v="${v}"].on`, { timeout: 5000 })
  const text = (await page.locator(`${card}`).textContent()) ?? ''
  check(!/a name is yours to give/.test(text), `the ${v} view prints no descriptor line (round 38)`)
  check(
    (await page.locator(`${card} .table-hint`).count()) === 0,
    `the ${v} view carries no hint line element`,
  )
}

// --- Switching swaps the table, and keeps one table -----------------------
const headersOf = () => page.$$eval(`${card} .etable th`, (els) => els.map((e) => e.textContent.trim()))

await page.click(`${card} #eviews [data-v="ports"]`)
await page.waitForSelector(`${card} #eviews [data-v="ports"].on`, { timeout: 5000 })
check(
  (await headersOf()).join(',') === 'name,port,last seen',
  `the ports view draws the ports table -- got ${JSON.stringify(await headersOf())}`,
)
check((await page.locator(`${card} .etable`).count()) === 1, 'one table under the views, not three stacked')

await page.click(`${card} #eviews [data-v="rules"]`)
await page.waitForSelector(`${card} #eviews [data-v="rules"].on`, { timeout: 5000 })
check(
  (await headersOf()).join(',') === 'name,chain,action,last fired',
  `the rules view draws the rules table -- got ${JSON.stringify(await headersOf())}`,
)

await page.click(`${card} #eviews [data-v="hosts"]`)
await page.waitForSelector(`${card} #eviews [data-v="hosts"].on`, { timeout: 5000 })
check(
  (await headersOf()).join(',') === 'name,lane,address,mac,first seen,last seen,marks',
  `the hosts view draws the ratified hosts table unchanged -- got ${JSON.stringify(await headersOf())}`,
)

// --- The count beside a name matches the rows under it --------------------
// The hosts view is the one whose rows this harness reliably produces
// (the feed above names hosts by talking); rules and ports depend on a
// pushed table this instance may not have, so their counts are only
// checked for internal consistency, never asserted non-zero.
for (const v of ['hosts', 'rules', 'ports']) {
  await page.click(`${card} #eviews [data-v="${v}"]`)
  await page.waitForSelector(`${card} #eviews [data-v="${v}"].on`, { timeout: 5000 })
  const shown = Number(
    ((await page.locator(`${card} #eviews [data-v="${v}"]`).textContent()) ?? '').replace(/\D+/g, ''),
  )
  const bodyText = (await page.locator(`${card} .etable tbody`).textContent()) ?? ''
  const rows = await page.locator(`${card} .etable tbody tr`).count()
  // An empty view draws one "nothing here yet" row, which is a message,
  // not an entity -- so it reads as zero.
  const isEmptyMessage = /Nothing seen yet|No router has pushed|No port has shown up/.test(bodyText)
  const counted = isEmptyMessage ? 0 : rows
  check(
    shown === counted,
    `the ${v} count matches the rows under it (count says ${shown}, table has ${counted})`,
  )
}

// --- Keyboard reaches a view ----------------------------------------------
await page.focus(`${card} #eviews [data-v="hosts"]`)
await page.keyboard.press('Tab')
await page.keyboard.press('Enter')
check(
  (await page.locator(`${card} #eviews [data-v="rules"].on`).count()) === 1,
  'tabbing from hosts to rules and pressing Enter switches the view',
)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
