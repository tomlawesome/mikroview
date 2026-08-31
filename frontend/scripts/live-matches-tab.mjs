// SPDX-License-Identifier: AGPL-3.0-only
//
// The Matches tab of Watchlist (#584) against a real running mikroview:
// real syslog lines through the real ingest pipeline, matched by real
// watchlist entries, read back through the real merged query
// (GET /api/matches?entries=all, #586) and rendered in a real browser.
//
// What is worth driving here rather than unit-testing:
//
//  - The merged list is the first surface that shows an "any source"
//    entry's matches at all. Those records were always written, but no
//    per-device query could find them without already guessing the
//    device -- so "it appears in the tab" is an end-to-end claim about
//    ingest, matching, the new query mode and the UI together.
//  - `n×` is matchlog's own collapsing, not a UI count. Two identical
//    lines have to become one row saying 2×, which only a real Append
//    against a real store can demonstrate.
//  - The admin split is that a viewer never reaches this tab at all --
//    Watchlist is admin-only end to end (navGroups.ts's `admin: true`),
//    so there is no read-only variant to check inside the page. Same
//    shape as live-suggestions.mjs's own check for #547.
//
// Two Playwright traps this file deliberately avoids, both of which
// have crashed or falsely passed a scenario in this project:
//
//  - page.isVisible() does not wait. Every visibility assertion below
//    goes through a locator's waitFor(), wrapped so a timeout is a
//    recorded failure rather than an exception that abandons the rest
//    of the run (check() exists precisely so one run reports
//    everything).
//  - Visibility comes from getBoundingClientRect(), so an element with
//    no geometry reads as hidden however real it is. The expanded block
//    of an unscoped entry is empty, and therefore zero-height, so the
//    "did the entry open" assertion below counts elements and reads
//    text instead of asking whether a box is visible.

import { session, feedRaw, check, done, goTo, launchBrowser } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

/** visible waits for a locator, returning false instead of throwing. */
async function visible(locator, timeout = 20000) {
  try {
    await locator.waitFor({ state: 'visible', timeout })
    return true
  } catch (e) {
    // The reason, not just the verdict: a bare "never became visible"
    // is the least useful half of the answer (live-browser.mjs's
    // waitForFlag makes the same argument). This is what told apart "the
    // row is missing" from "the row is there under a different name".
    console.log(`    (waited in vain: ${String(e).split('\n')[0]})`)
    return false
  }
}

/** hidden is visible's opposite, with the same no-throw contract. */
async function hidden(locator, timeout = 10000) {
  try {
    await locator.waitFor({ state: 'hidden', timeout })
    return true
  } catch {
    return false
  }
}

// Ports and addresses no other scenario uses: this instance is shared
// across a whole live-check run, so a row has to be identifiable as
// this scenario's own.
const WATCHED_PORT = 2223
const CAMERA_MAC = 'aa:bb:cc:dd:ee:77'
const VISITOR_MAC = 'aa:bb:cc:dd:ee:88'
const EGRESS_DEST = '198.51.100.33'
const PORT_DEST = '203.0.113.21'
const PORT_ENTRY = 'live matches port watch'
const CAMERA_ENTRY = 'live matches camera'

// --- The empty state, but only when the log really is empty --------------
// Every scenario shares one instance, and earlier ones record matches
// that cannot be deleted afterwards (a match outlives the entry that
// recorded it, by design). So this is asserted only when the log is
// genuinely empty, rather than made conditional on run order -- the
// three empty-state branches themselves are covered by
// Watchlist.svelte.test.ts.

const before = await api('GET', '/api/matches?entries=all')
check(before.status === 200, `the merged query answers (${before.status})`)

// Deliberately makes no claim about which tab was selected on the way
// in: the page stays mounted between visits, so arriving from the rail
// lands on whichever tab was last open.
async function openMatchesTab() {
  // Matching the label, not the row: NavRail moves each row's text into
  // a <span class="label">, and Playwright's text engine only matches an
  // element directly containing the text (see live-nav-rail.mjs).
  await goTo(page, 'Watchlist')
  const tab = page.locator('[role="tab"]:has-text("Matches")')
  if (!(await visible(tab))) {
    check(false, 'the Watchlist page carries a Matches tab')
    return
  }
  await tab.click()
}

if ((before.body?.matches ?? []).length === 0) {
  await openMatchesTab()
  check(await visible(page.locator('#panel-matches .empty-state')), 'an empty match log shows the empty state, not a blank list')
} else {
  console.log(`  --   empty-state assertion skipped: the shared instance already holds ${before.body.matches.length} matches`)
}

// --- Two entries, two kinds of sentence ----------------------------------
// One non-inverted with no source at all ("any source"), which is the
// case #586 made reachable, and one inverted, which is the other mode
// the row has to distinguish.

const portEntry = await api('POST', '/api/definitions', {
  name: PORT_ENTRY,
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [WATCHED_PORT] },
})
check(portEntry.status === 201, `an unscoped watched-port entry is created (${portEntry.status})`)
// An unscoped entry comes back with `source: {}`, not with the key
// absent -- so this reads the scope's own fields. Asserting the key was
// missing tested the wire format, not the property that matters, and
// passed nothing but a truthy empty object.
const portScope = portEntry.body?.expectation?.source
check(
  !portScope?.mac && !portScope?.ip,
  `that entry really is unscoped -- no match of it is reachable by mac or ip (scope ${JSON.stringify(portScope)})`,
)

const cameraEntry = await api('POST', '/api/definitions', {
  name: CAMERA_ENTRY,
  intent: 'expectation',
  kind: 'declarative',
  expectation: { invert: true, source: { mac: CAMERA_MAC } },
})
check(cameraEntry.status === 201, `an inverted (egress policy) entry is created (${cameraEntry.status})`)

const enforcing = await api('POST', `/api/definitions/${cameraEntry.body?.id}/observing`, { observing: false })
check(enforcing.status === 200 && !enforcing.body?.expectation?.observing, `the inverted entry leaves observe mode (${enforcing.status})`)

// --- Real traffic ---------------------------------------------------------

const portLine =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${VISITOR_MAC}, ` +
  `proto TCP (SYN), 192.168.7.11:51000->${PORT_DEST}:${WATCHED_PORT}, len 60`
const egressLine =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${CAMERA_MAC}, ` +
  `proto TCP (SYN), 192.168.7.12:51001->${EGRESS_DEST}:8443, len 60`

// The same watched-port line twice: matchlog collapses an identical
// repeat onto the existing record, which is what the row's `n×` reports.
feedRaw(portLine)
feedRaw(portLine)
feedRaw(egressLine)

let merged = []
for (let i = 0; i < 60; i++) {
  const got = await api('GET', '/api/matches?entries=all')
  merged = got.body?.matches ?? []
  const portMatch = merged.find((m) => m.entryId === portEntry.body?.id)
  if (portMatch?.count >= 2 && merged.some((m) => m.entryId === cameraEntry.body?.id)) break
  await page.waitForTimeout(250)
}

const portMatch = merged.find((m) => m.entryId === portEntry.body?.id)
const cameraMatch = merged.find((m) => m.entryId === cameraEntry.body?.id)
check(!!portMatch, 'the unscoped entry records a match, reachable only through entries=all')
check(
  portMatch?.tuple?.source?.mac?.toLowerCase() === VISITOR_MAC,
  `that match carries the event's own identity, not the entry's empty scope (got ${JSON.stringify(portMatch?.tuple?.source)})`,
)
check(portMatch?.count >= 2, `two identical lines collapse into one record with a count (got ${portMatch?.count})`)
check(!!cameraMatch, 'the inverted entry records a violation of its own')

// A match of the unscoped entry is genuinely unreachable the old way:
// nobody would know to ask for this device.
const byNothing = await api('GET', '/api/matches')
check(byNothing.status === 400, `a query with no identity and no entries=all is still refused (${byNothing.status})`)
const bothWays = await api('GET', `/api/matches?entries=all&mac=${VISITOR_MAC}`)
check(bothWays.status === 400, `entries=all combined with a mac is refused rather than resolved (${bothWays.status})`)

// --- The tab itself -------------------------------------------------------

await openMatchesTab()
const panel = page.locator('#panel-matches')
check(await visible(panel), 'the Matches tab opens')
// The panel that is not selected must actually be gone, not merely
// carrying a hidden attribute a class rule overrides.
check(await hidden(page.locator('#panel-watchlist')), 'selecting Matches hides the Watchlist panel, rather than stacking both')

const portRow = panel.locator('.match', { hasText: PORT_ENTRY }).first()
const cameraRow = panel.locator('.match', { hasText: CAMERA_ENTRY }).first()
check(await visible(portRow), 'the unscoped entry\'s match appears in the merged list')
check(await visible(cameraRow), 'the inverted entry\'s match appears in the same list, not a separate one')

check(
  (await portRow.locator('.badge.mode').textContent())?.trim() === 'watched port',
  'the watched-port row says which sentence to read',
)
check(
  (await cameraRow.locator('.badge.mode').textContent())?.trim() === 'egress policy',
  'the egress-policy row says the other one',
)
check((await portRow.locator('.count').textContent())?.trim() === '2×', 'the collapsed repeat is shown as 2×')
check(
  (await portRow.textContent())?.includes(`${PORT_DEST}:${WATCHED_PORT}`),
  'the row carries source -> destination:port',
)

// Newest first, across entries. Asserted against the server's own
// ordering rather than against which line was fed last: delivery is over
// a socket and the two records' timestamps can land either way round,
// but the list must always render whatever order the query returned.
const apiOrder = merged
  .filter((m) => m.entryId === portEntry.body?.id || m.entryId === cameraEntry.body?.id)
  .map((m) => (m.entryId === portEntry.body?.id ? PORT_ENTRY : CAMERA_ENTRY))
const domOrder = (await panel.locator('.match .entry-link').evaluateAll((els) => els.map((e) => (e.textContent ?? '').trim())))
  .filter((n) => n === PORT_ENTRY || n === CAMERA_ENTRY)
check(
  JSON.stringify(domOrder) === JSON.stringify(apiOrder),
  `the list renders the query's newest-first order (server ${JSON.stringify(apiOrder)}, page ${JSON.stringify(domOrder)})`,
)

const tabNames = await page.locator('[role="tablist"][aria-label="Watchlist views"] [role="tab"]').evaluateAll((els) =>
  els.map((e) => (e.textContent ?? '').trim()),
)
check(
  JSON.stringify(tabNames) === JSON.stringify(['Watchlist', 'Matches', 'Suggestions']),
  `Matches is a tab of Watchlist on the house tablist (got ${JSON.stringify(tabNames)})`,
)

// --- The entry name goes back to the entry --------------------------------

await cameraRow.locator('.entry-link').click()
check(await visible(page.locator('#panel-watchlist')), 'following the entry name returns to the Watchlist tab')
// Counted and read rather than asked "is it visible": an entry's
// expanded block can be legitimately zero-height, which Playwright
// reports as hidden.
const expandedText = await page
  .locator(`#entry-${cameraEntry.body?.id} .expanded`)
  .evaluateAll((els) => els.map((e) => e.textContent ?? '').join(''))
check(expandedText.length > 0, 'the entry it names is expanded, not merely scrolled to')
check(expandedText.includes('Permitted'), 'the expanded entry shows its own detail')

// --- A viewer never gets here at all --------------------------------------

const VIEWER_USER = 'live-viewer-584-matches'
const VIEWER_PASS = 'live-viewer-584-matches-password'

const createRes = await api('POST', '/api/auth/users', { username: VIEWER_USER, password: VIEWER_PASS, role: 'viewer' })
check(createRes.status === 201, `a viewer account is created (${createRes.status})`)

const browser = await launchBrowser()
const viewerCtx = await browser.newContext({ ignoreHTTPSErrors: true })
const viewerPage = await viewerCtx.newPage()
await viewerPage.goto(URL_BASE, { waitUntil: 'networkidle' })
await viewerPage.fill('input[autocomplete="username"]', VIEWER_USER)
await viewerPage.fill('input[autocomplete="current-password"]', VIEWER_PASS)
await viewerPage.click('button[type="submit"]')
await viewerPage.waitForSelector('.roll-rail .rail-name', { timeout: 15000 })

const viewerLabels = await viewerPage.$$eval('.roll-rail .rail-name', (els) => els.map((e) => e.textContent.trim()))
check(
  !viewerLabels.includes('Watchlist'),
  `Watchlist -- and the Matches tab inside it -- is absent from a viewer's roll rail, not disabled -- the rail shows ${JSON.stringify(viewerLabels)}`,
)
check(
  (await viewerPage.locator('#panel-matches').count()) === 0,
  'no Matches panel exists anywhere in a viewer session',
)

await browser.close()

// --- Cleanup: entries and the account, not the matches -------------------
// The match records deliberately survive their entries (that is what
// "(entry removed)" in the list is for), so only the entries go.

await api('DELETE', `/api/definitions/${portEntry.body?.id}`)
await api('DELETE', `/api/definitions/${cameraEntry.body?.id}`)

const users = await api('GET', '/api/auth/users')
const viewerAccount = (Array.isArray(users.body) ? users.body : []).find((u) => u.username === VIEWER_USER)
if (viewerAccount) {
  const del = await api('DELETE', `/api/auth/users/${encodeURIComponent(viewerAccount.id)}`)
  check(del.status < 300, `the viewer account "${VIEWER_USER}" is removed again (${del.status})`)
} else {
  check(false, `could not find the viewer account "${VIEWER_USER}" to clean it up`)
}

const anonMerged = await fetch(`${URL_BASE}/api/matches?entries=all`)
check(anonMerged.status === 401, `an unauthenticated merged query is refused (${anonMerged.status})`)

done()
