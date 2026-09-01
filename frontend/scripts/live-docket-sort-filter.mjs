// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #649: "every column sorts and filters, across all three tabs."
// Round 18 (ratified; round-19 corrections applied) gave the docket a
// quiet dashed filter row beneath the column heads, and made every head
// clickable to sort (click again to reverse) -- see
// docs/design/concepts/round-18/the-docket-opened.html. The built docket
// had fixed sort orders and no filter row on Flags, Watchlist and Audit
// log; this scenario proves the fix landed on all three, against a real
// running instance rather than jsdom.
//
// Flags and Watchlist render as card lists, not tables -- the sortbar/
// filterbar toolbar above each list stands in for column heads there.
// Audit log is a genuine <table>, so its heads/filter row are the ones
// the mockup shows directly.

import { session, check, done, feedPortScan, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

async function api(page, method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// Two distinct sources, unused by every other scenario in this directory
// (#590's collision reasoning) -- different scan sizes so the flags also
// carry different `count`, which is what the count column sorts on.
const LOW_IP = '198.51.100.120'
const HIGH_IP = '198.51.100.121'
feedPortScan(6, LOW_IP)
feedPortScan(18, HIGH_IP)

const { page } = await session()

// --- Flags: the sortbar/filterbar toolbar above the Active card list ---

await goTo(page, 'Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })
await page.waitForSelector(`.card .target:has-text("${LOW_IP}")`, { timeout: 15000 })
await page.waitForSelector(`.card .target:has-text("${HIGH_IP}")`, { timeout: 15000 })

check(await page.isVisible('.sortbar'), 'the Flags Active list carries a sort toolbar')
check(await page.isVisible('.filterbar'), 'the Flags Active list carries a filter row')

function activeTargets() {
  return page.$$eval('section[aria-labelledby="active-heading"] .card .target', (els) =>
    els.map((el) => el.textContent?.trim()),
  )
}

const beforeSort = await activeTargets()
check(beforeSort.indexOf(HIGH_IP) < beforeSort.indexOf(LOW_IP), 'defaults to newest-first, the fixed order this replaces')

await page.click('.sorth:has-text("count")')
await page.waitForTimeout(150)
const ascCount = await activeTargets()
check(ascCount.indexOf(LOW_IP) < ascCount.indexOf(HIGH_IP), 'clicking the count head sorts ascending by it')

await page.click('.sorth:has-text("count")')
await page.waitForTimeout(150)
const descCount = await activeTargets()
check(descCount.indexOf(HIGH_IP) < descCount.indexOf(LOW_IP), 'clicking it again reverses the order')

await page.fill('input[aria-label="Filter by where"]', LOW_IP)
await page.waitForTimeout(150)
const filtered = await activeTargets()
check(
  filtered.length === 1 && filtered[0] === LOW_IP,
  `filtering "where" to ${LOW_IP} narrows the list to just that flag (got ${JSON.stringify(filtered)})`,
)
await page.fill('input[aria-label="Filter by where"]', '')

// --- Watchlist: same toolbar idiom, over the Entries card list ---

const watchB = await api(page, 'POST', '/api/definitions', {
  name: 'live sort watch B',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [22] },
})
const watchA = await api(page, 'POST', '/api/definitions', {
  name: 'live sort watch A',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [23] },
})
check(watchB.status === 201 && watchA.status === 201, 'two watchlist entries are created to sort/filter over')

await goTo(page, 'Watchlist')
await page.waitForSelector('.card .name', { timeout: 15000 })

check(await page.isVisible('.sortbar'), 'the Watchlist Entries list carries a sort toolbar')
check(await page.isVisible('.filterbar'), 'the Watchlist Entries list carries a filter row')

function entryNames() {
  return page.$$eval('#panel-watchlist .card .name', (els) => els.map((el) => el.textContent?.trim()))
}

const namesAsc = await entryNames()
check(
  namesAsc.indexOf('live sort watch A') < namesAsc.indexOf('live sort watch B'),
  'defaults to alphabetical by watch name',
)

await page.click('.sorth:has-text("watch")')
await page.waitForTimeout(150)
const namesDesc = await entryNames()
check(
  namesDesc.indexOf('live sort watch B') < namesDesc.indexOf('live sort watch A'),
  'clicking the watch head reverses the order',
)

await page.fill('input[aria-label="Filter by watch name"]', 'watch A')
await page.waitForTimeout(150)
const filteredNames = await entryNames()
check(
  filteredNames.length === 1 && filteredNames[0] === 'live sort watch A',
  `filtering "watch" to "watch A" narrows the list to just that entry (got ${JSON.stringify(filteredNames)})`,
)
await page.fill('input[aria-label="Filter by watch name"]', '')

// --- Audit log: a genuine <table>, heads and filter row match the
// mockup directly -- the two definition creates above already produced
// two real entries to sort/filter over. ---

await goTo(page, 'Audit log')
await page.waitForSelector('table thead th', { timeout: 15000 })
// Audit.Record's own signature (internal/audit/store.go) is
// (actor, action, target, detail) -- definitions.go's create call passes
// the entry's ID as target and its name as detail, so it's Detail this
// scenario filters/sorts on, not Target.
await page.waitForSelector('tbody tr .dim:has-text("live sort watch")', { timeout: 15000 })

check(await page.isVisible('tr.filters'), 'the audit log table carries a dashed filter row beneath its heads')

function auditDetails() {
  return page.$$eval('tbody tr .dim', (els) => els.map((el) => el.textContent?.trim()))
}

await page.fill('input[aria-label="Filter by detail"]', 'live sort watch')
await page.waitForTimeout(150)
const auditFiltered = await auditDetails()
check(
  auditFiltered.length >= 2 && auditFiltered.every((t) => t?.includes('live sort watch')),
  `filtering the audit log to "live sort watch" narrows to just those entries (got ${JSON.stringify(auditFiltered)})`,
)

await page.click('th:has-text("Detail")')
await page.waitForTimeout(150)
const detailsAsc = await auditDetails()
const sortedAsc = [...detailsAsc].sort((a, b) => (a || '').localeCompare(b || ''))
check(
  JSON.stringify(detailsAsc) === JSON.stringify(sortedAsc),
  `clicking the Detail head sorts the filtered rows by it (got ${JSON.stringify(detailsAsc)})`,
)

await page.click('th:has-text("Detail")')
await page.waitForTimeout(150)
const detailsDesc = await auditDetails()
const sortedDesc = [...sortedAsc].reverse()
check(JSON.stringify(detailsDesc) === JSON.stringify(sortedDesc), 'clicking it again reverses the order')

await page.fill('input[aria-label="Filter by detail"]', '')

// --- Cleanup ---

await api(page, 'DELETE', `/api/definitions/${watchB.body?.id}`)
await api(page, 'DELETE', `/api/definitions/${watchA.body?.id}`)

done()
