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
// All three tabs are genuine tables now, so all three carry the heads
// and filter row the mockup shows directly, and none of them carries the
// standalone `.sortbar`/`.filterbar` toolbar this scenario used to drive
// -- that pair no longer exists anywhere in frontend/src. Flags was
// rebuilt from a card grid into the ratified table by 68fd460 (#688,
// Flags.svelte:585), Watchlist by #676/#761 (Watchlist.svelte:784), and
// the audit log was always one (AuditLog.svelte:238).

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

// --- Flags: the head group of the ratified active table ---

// The active section is named by aria-label rather than a heading id:
// round 30 drew the panel as a bare table with no heading over it
// (Flags.svelte:552-556), so the name survives only on the section
// itself, and it carries the live count -- "Active flags (2)" -- so this
// matches on the stem.
const ACTIVE = 'section[aria-label^="Active flags"]'

await goTo(page, 'Flags')
await page.waitForSelector(`${ACTIVE} tr.frow td.fmark`, { timeout: 15000 })
// The where lives in the row's `td.k`, as a `button.wl` when it can be
// opened in the topography and as a plain `span.wl-plain` when it cannot
// (Flags.svelte:703-718); waiting on the cell covers both.
await page.waitForSelector(`${ACTIVE} tr.frow td.k:has-text("${LOW_IP}")`, { timeout: 15000 })
await page.waitForSelector(`${ACTIVE} tr.frow td.k:has-text("${HIGH_IP}")`, { timeout: 15000 })

check(await page.isVisible(`${ACTIVE} thead .sorth`), 'the Flags active table sorts from its column heads')
check(
  await page.isVisible(`${ACTIVE} thead tr.filters`),
  'the Flags active table carries a filter row beneath its heads',
)

function activeTargets() {
  // `tr.frow` only: an open row's drawer (Flags.svelte:744) and the "no
  // flags match these filters" row (:626) are siblings in the same
  // tbody, and neither is a flag.
  return page.$$eval(`${ACTIVE} tbody tr.frow td.k`, (els) => els.map((el) => el.textContent?.trim()))
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

// --- Watchlist: the same head group, over the watches table ---

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

// #panel-watchlist itself is unchanged (Watchlist.svelte:758) -- what
// moved is inside it: the entries card list became the ratified watches
// table (#676/#761, Watchlist.svelte:784), one `tr.wt-row` per watch
// with the name in `td.k`, and its heads carry `.th-sort` rather than
// Flags' `.sorth`.
const WATCHES = '#panel-watchlist .watch-table'

await goTo(page, 'Watchlist')
await page.waitForSelector(`${WATCHES} tr.wt-row td.k`, { timeout: 15000 })

check(await page.isVisible(`${WATCHES} thead .th-sort`), 'the Watchlist watches table sorts from its column heads')
check(
  await page.isVisible(`${WATCHES} thead tr.filters`),
  'the Watchlist watches table carries a filter row beneath its heads',
)

function entryNames() {
  // Not `.wt-draft`: an unsaved draft watch renders as a row of this
  // same table (Watchlist.svelte:833) but is not an entry yet.
  return page.$$eval(`${WATCHES} tbody tr.wt-row:not(.wt-draft) td.k`, (els) =>
    els.map((el) => el.textContent?.trim()),
  )
}

const namesAsc = await entryNames()
check(
  namesAsc.indexOf('live sort watch A') < namesAsc.indexOf('live sort watch B'),
  'defaults to alphabetical by watch name',
)

await page.click(`${WATCHES} .th-sort:has-text("watch")`)
await page.waitForTimeout(150)
const namesDesc = await entryNames()
check(
  namesDesc.indexOf('live sort watch B') < namesDesc.indexOf('live sort watch A'),
  'clicking the watch head reverses the order',
)

await page.fill('input[aria-label="Filter watches by watch name"]', 'watch A')
await page.waitForTimeout(150)
const filteredNames = await entryNames()
check(
  filteredNames.length === 1 && filteredNames[0] === 'live sort watch A',
  `filtering "watch" to "watch A" narrows the list to just that entry (got ${JSON.stringify(filteredNames)})`,
)
await page.fill('input[aria-label="Filter watches by watch name"]', '')

// --- Audit log: a genuine <table>, heads and filter row match the
// mockup directly -- the two definition creates above already produced
// two real entries to sort/filter over. ---

await goTo(page, 'Audit log')
await page.waitForSelector('table thead th', { timeout: 15000 })
// Audit.Record's own signature (internal/audit/store.go) is
// (actor, action, target, detail) -- definitions.go's create call passes
// the entry's ID as target and its name as detail. Round 30 collapsed
// this table to three columns, When · Who · What (AuditLog.svelte:239-250),
// so target and detail no longer have heads of their own: describeEntry
// composes them into one What sentence, lead + key + tail, and `td.what`
// holds exactly that sentence (:266). Filtering and sorting "what" run
// over the same composed string (:154-157, :191), so the name this
// scenario seeded is still what it narrows and orders on.
await page.waitForSelector('tbody tr td.what:has-text("live sort watch")', { timeout: 15000 })

check(await page.isVisible('tr.filters'), 'the audit log table carries a dashed filter row beneath its heads')

function auditDetails() {
  return page.$$eval('tbody tr td.what', (els) => els.map((el) => el.textContent?.trim()))
}

await page.fill('input[aria-label="Filter by what"]', 'live sort watch')
await page.waitForTimeout(150)
const auditFiltered = await auditDetails()
check(
  auditFiltered.length >= 2 && auditFiltered.every((t) => t?.includes('live sort watch')),
  `filtering the audit log's what column to "live sort watch" narrows to just those entries (got ${JSON.stringify(auditFiltered)})`,
)

await page.click('th:has-text("What")')
await page.waitForTimeout(150)
const detailsAsc = await auditDetails()
const sortedAsc = [...detailsAsc].sort((a, b) => (a || '').localeCompare(b || ''))
check(
  JSON.stringify(detailsAsc) === JSON.stringify(sortedAsc),
  `clicking the What head sorts the filtered rows by it (got ${JSON.stringify(detailsAsc)})`,
)

await page.click('th:has-text("What")')
await page.waitForTimeout(150)
const detailsDesc = await auditDetails()
const sortedDesc = [...sortedAsc].reverse()
check(JSON.stringify(detailsDesc) === JSON.stringify(sortedDesc), 'clicking it again reverses the order')

await page.fill('input[aria-label="Filter by what"]', '')

// --- Cleanup ---

await api(page, 'DELETE', `/api/definitions/${watchB.body?.id}`)
await api(page, 'DELETE', `/api/definitions/${watchA.body?.id}`)

done()
