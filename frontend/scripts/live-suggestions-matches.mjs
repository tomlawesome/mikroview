// SPDX-License-Identifier: AGPL-3.0-only
//
// Suggestions and matches on the watchlist tab (#771, design round 33)
// against a real running mikroview: real router data pushed through the
// real ingest endpoint generating real candidates, real syslog lines
// through the real ingest pipeline matched by real watchlist entries,
// and both driven through the real UI in a real browser.
//
// This file replaces two scenarios rather than joining them:
// live-suggestions.mjs and live-matches-tab.mjs both drove the
// `Watchlist | Matches | Suggestions` sub-tab strip, which round 33
// deliberately does not draw -- "a suggestion is a watch that has not
// been said yes to, and a match is a line in the watch's own drawer"
// (docs/design/concepts/round-33/README.md). With the strip retired
// those two scenarios address a surface that no longer exists.
//
// Every assertion they made that still has a referent is carried
// forward here rather than dropped, because the surface changing is not
// the claim changing:
//
//  - accepting a device candidate creates a real watchlist entry that
//    arrives *observing* (learning), not enforcing;
//  - hide/unhide round-trips a candidate without ever destroying it;
//  - the reset really does delete the real watchlist entries it had
//    created, and really does regenerate the pool;
//  - `n×` is matchlog's own collapsing of two identical lines, not a UI
//    count;
//  - a match of an unscoped ("any source") entry carries the *event's*
//    identity, never the entry's empty scope;
//  - the query refuses a request with no identity, and refuses
//    entries=all combined with a mac, rather than quietly resolving it;
//  - an unauthenticated read of either surface is refused;
//  - a viewer never reaches Watchlist -- and so never reaches either of
//    these -- at all.
//
// The one claim deliberately NOT carried forward is the cross-watch
// merged *view* (GET /api/matches?entries=all rendered as a page of its
// own). Round 33 records its home as the stream with a "matched a watch"
// lens rather than the docket, so it has no drawn surface to assert
// against; the query itself is still exercised below, since the table's
// own "last event" column and every drawer's match list read it.
//
// Playwright traps this file deliberately avoids, both of which have
// crashed or falsely passed a scenario in this project:
//
//  - page.isVisible() does not wait. Every visibility assertion goes
//    through a locator's waitFor(), wrapped so a timeout is a recorded
//    failure rather than an exception that abandons the rest of the run.
//  - Visibility comes from getBoundingClientRect(), so an element with
//    no geometry reads as hidden however real it is. Assertions below
//    count elements and read text rather than asking whether a box is
//    visible wherever the element may legitimately be zero-height.

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

async function push(token, payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

/** visible waits for a locator, returning false instead of throwing. */
async function visible(locator, timeout = 20000) {
  try {
    await locator.waitFor({ state: 'visible', timeout })
    return true
  } catch (e) {
    // The reason, not just the verdict: a bare "never became visible" is
    // the least useful half of the answer, and this is what tells apart
    // "the row is missing" from "the row is there under a different name".
    console.log(`    (waited in vain: ${String(e).split('\n')[0]})`)
    return false
  }
}

async function openWatchlist() {
  // goTo rolls the deck to the docket scene and clicks its Watchlist tab
  // on the scene bar (#700); the label matching lives in
  // live-browser.mjs's SCENES table, not here.
  await goTo(page, 'Watchlist')
  await page.waitForSelector('#panel-watchlist', { timeout: 10000 })
}

// Ports and addresses no other scenario uses: this instance is shared
// across a whole live-check run, so a row has to be identifiable as this
// scenario's own.
const WATCHED_PORT = 2224
const CAMERA_MAC = 'aa:bb:cc:dd:ee:71'
const VISITOR_MAC = 'aa:bb:cc:dd:ee:72'
const EGRESS_DEST = '198.51.100.44'
const PORT_DEST = '203.0.113.31'
const PORT_ENTRY = 'live r33 port watch'
const CAMERA_ENTRY = 'live r33 camera'
const SUGGEST_HOST = 'live-r33-camera'
const SUGGEST_MAC = 'aa:bb:cc:dd:ee:73'
const SUGGEST_PORT = 3391

// ==========================================================================
// PART 1 -- THE SUB-TAB STRIP IS GONE
// ==========================================================================
// Round 33 draws one flat watch table with a second body under it. This
// is the fidelity claim the two retired scenarios can no longer make,
// stated once here so a re-mounted strip is caught rather than silently
// tolerated.

await openWatchlist()

check(
  (await page.$$('[role="tablist"][aria-label="Watchlist views"]')).length === 0,
  'no Watchlist sub-tab strip is drawn -- round 33 has none',
)
check((await page.$$('#panel-matches')).length === 0, 'no separate Matches panel exists')
check((await page.$$('#panel-suggestions')).length === 0, 'no separate Suggestions panel exists')

// ==========================================================================
// PART 2 -- MATCHES IN THE WATCH'S OWN DRAWER
// ==========================================================================

const before = await api('GET', '/api/matches?entries=all')
check(before.status === 200, `the merged query answers (${before.status})`)

// One entry with no source at all ("any source"), and one inverted --
// the two modes a row has to tell apart, and the pair the retired
// Matches tab existed to make reachable.

const portEntry = await api('POST', '/api/definitions', {
  name: PORT_ENTRY,
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [WATCHED_PORT] },
})
check(portEntry.status === 201, `an unscoped watched-port entry is created (${portEntry.status})`)

// An unscoped entry comes back with `source: {}`, not with the key
// absent -- so this reads the scope's own fields. Asserting the key was
// missing tested the wire format, not the property that matters.
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
check(
  enforcing.status === 200 && !enforcing.body?.expectation?.observing,
  `the inverted entry leaves observe mode (${enforcing.status})`,
)

// --- Real traffic ---------------------------------------------------------

const portLine =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${VISITOR_MAC}, ` +
  `proto TCP (SYN), 192.168.7.21:51000->${PORT_DEST}:${WATCHED_PORT}, len 60`
const egressLine =
  `A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac ${CAMERA_MAC}, ` +
  `proto TCP (SYN), 192.168.7.22:51001->${EGRESS_DEST}:8443, len 60`

// The same watched-port line three times: matchlog collapses identical
// repeats onto the existing record, which is what the drawer's `n×`
// reports. Three rather than two so the count is unmistakably not a
// row count.
feedRaw(portLine)
feedRaw(portLine)
feedRaw(portLine)
feedRaw(egressLine)

let merged = []
for (let i = 0; i < 60; i++) {
  const got = await api('GET', '/api/matches?entries=all')
  merged = got.body?.matches ?? []
  const pm = merged.find((m) => m.entryId === portEntry.body?.id)
  if (pm?.count >= 3 && merged.some((m) => m.entryId === cameraEntry.body?.id)) break
  await page.waitForTimeout(250)
}

const portMatch = merged.find((m) => m.entryId === portEntry.body?.id)
const cameraMatch = merged.find((m) => m.entryId === cameraEntry.body?.id)
check(!!portMatch, 'the unscoped entry records a match, reachable only through entries=all')
check(
  portMatch?.tuple?.source?.mac?.toLowerCase() === VISITOR_MAC,
  `that match carries the event's own identity, not the entry's empty scope (got ${JSON.stringify(portMatch?.tuple?.source)})`,
)
check(portMatch?.count >= 3, `identical lines collapse into one record with a count (got ${portMatch?.count})`)
check(!!cameraMatch, 'the inverted entry records a violation of its own')

// The query still refuses what it always refused. Both of these are
// about the route, not the page, so they survive the page changing.
const byNothing = await api('GET', '/api/matches')
check(byNothing.status === 400, `a query with no identity and no entries=all is still refused (${byNothing.status})`)
const bothWays = await api('GET', `/api/matches?entries=all&mac=${VISITOR_MAC}`)
check(bothWays.status === 400, `entries=all combined with a mac is refused rather than resolved (${bothWays.status})`)

// --- The drawer itself ----------------------------------------------------

await openWatchlist()
// The page stays mounted between visits and the entries were created
// over the API, so the table needs a reload to know about them.
await page.reload({ waitUntil: 'networkidle' })
await openWatchlist()

const cameraRow = page.locator(`#watch-${cameraEntry.body?.id}`)
check(await visible(cameraRow), "the inverted entry has a row on the watch table")
await cameraRow.click()

const cameraDrawer = page.locator('tr.wt-drawer').first()
check(await visible(cameraDrawer), "the watch's drawer opens")

const matchesBlock = cameraDrawer.locator('.matches')
check(await visible(matchesBlock), 'the drawer carries a "what it matched" block')

const label = ((await matchesBlock.locator('.lab').textContent()) ?? '').trim()
check(
  label.startsWith('what it matched'),
  `the block is labelled as the drawing names it (got ${JSON.stringify(label)})`,
)

// Counted rather than asked "is it visible": a match line is a grid row
// that can legitimately be short, and the claim here is how many there
// are and what they say.
const lines = await matchesBlock.locator('.mlist li').evaluateAll((els) => els.map((e) => (e.textContent ?? '').trim()))
check(lines.length >= 1 && lines.length <= 3, `the drawer shows at most three match lines (got ${lines.length})`)
// The destination is rendered by its friendly name where the naming
// resolver has one (matchDest prefers event.dstHostName), which is what
// the drawing shows -- `nas`, not 198.51.100.40. So this asserts the
// shape of the line and its port, not the raw address: an instance that
// happens to name the destination is behaving correctly, not failing.
check(
  lines.some((l) => l.includes('→') && l.includes(':8443')),
  `a match line carries source → destination:port (in ${JSON.stringify(lines)})`,
)
check(
  lines.some((l) => l.includes(CAMERA_MAC)),
  `that line carries the event's own source identity (looked for ${CAMERA_MAC} in ${JSON.stringify(lines)})`,
)

// The port entry's own drawer is where the collapsed count shows.
const portRow = page.locator(`#watch-${portEntry.body?.id}`)
if (await visible(portRow)) {
  await portRow.click()
  const portDrawer = page.locator(`#watch-${portEntry.body?.id} + tr.wt-drawer`)
  const portLines = await portDrawer
    .locator('.mlist li')
    .evaluateAll((els) => els.map((e) => (e.textContent ?? '').trim()))
  check(
    portLines.some((l) => l.includes('3×') || l.includes('4×')),
    `the collapsed repeat is shown as n× in the drawer (got ${JSON.stringify(portLines)})`,
  )
  // An unscoped entry has no mac and no ip, so there is no per-device
  // query to page with -- the control is correctly not offered rather
  // than offered and guaranteed to 400.
  check(
    (await portDrawer.locator('button.slink.older').count()) === 0,
    'an unscoped entry is not offered `older ▸` -- there is no identity to ask by',
  )
  await portRow.click()
} else {
  check(false, 'the unscoped entry has a row on the watch table')
}

// `older ▸` on an entry that does have an identity.
await cameraRow.click()
const older = cameraDrawer.locator('button.slink.older')
if ((await older.count()) > 0) {
  await older.click()
  // It either loads more or reports itself exhausted; what must not
  // happen is an error surfacing in the drawer.
  await page.waitForTimeout(1500)
  check(
    (await cameraDrawer.locator('p.error').count()) === 0,
    '`older ▸` pages back without surfacing an error',
  )
} else {
  console.log('  --   `older ▸` not offered: this entry has no older page to walk back to')
}

// ==========================================================================
// PART 3 -- SUGGESTIONS AS A SECOND BODY
// ==========================================================================
// RunPeriodicSync's own interval (5 minutes) is far too coarse for a
// live-check run -- /api/suggestions/reset regenerates synchronously as
// part of what it already does (internal/api's handleSuggestionsReset),
// so it doubles as the fastest real path to fresh candidates without a
// test-only knob added just for this.

const ingest = await api('POST', '/api/tokens', {
  name: 'live-r33',
  kind: 'ingest',
  device: 'live-r33-router',
})
check(ingest.status === 201, `an ingest token is issued (${ingest.status})`)

check(
  (await push(ingest.body.value, {
    kind: 'dhcp-lease',
    page: 1,
    pages: 1,
    records: [{ hostname: SUGGEST_HOST, mac: SUGGEST_MAC, address: '192.168.51.10' }],
  })) === 200,
  'a named DHCP lease is pushed',
)

check(
  (await push(ingest.body.value, {
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    records: [
      {
        ordinal: 1,
        chain: 'forward',
        action: 'drop',
        dstPort: SUGGEST_PORT,
        protocol: 'tcp',
        comment: 'live r33 test rule',
      },
    ],
  })) === 200,
  'a drop rule with a dst-port is pushed',
)

const reset0 = await api('POST', '/api/suggestions/reset', { confirm: true })
check(reset0.status === 200, `the initial reset regenerates candidates (${reset0.status})`)

await page.reload({ waitUntil: 'networkidle' })
await openWatchlist()

const sugg = page.locator('#sugg')
check(await visible(sugg), 'the suggestions body renders under the watches, in the same table')

// The heading says what it is and counts what is open.
const heading = ((await sugg.locator('.sdl').textContent()) ?? '').trim()
check(
  heading.startsWith('mikroview suggests'),
  `the suggestions heading reads as drawn (got ${JSON.stringify(heading)})`,
)

const deviceRow = sugg.locator('tr.wt-sugg', { hasText: SUGGEST_HOST }).first()
check(await visible(deviceRow), 'the device suggestion appears as a row in the watch grammar')

const deviceChip = ((await deviceRow.locator('.wchip2.sugg').textContent()) ?? '').trim()
check(
  deviceChip.startsWith('◇ suggested'),
  `a suggested row wears the dashed suggested chip (got ${JSON.stringify(deviceChip)})`,
)

// Suggestions are not watches, so round 31's sort and filter must leave
// this body alone. Filtering the watch table down to nothing is the
// sharpest form of that claim.
await page.fill('input[aria-label="Filter watches by name"]', 'zzz-no-such-watch-zzz').catch(async () => {
  // The filter inputs are labelled per column; fall back to the first
  // one rather than failing the whole scenario on a label rename.
  await page.locator('#panel-watchlist thead input').first().fill('zzz-no-such-watch-zzz')
})
await page.waitForTimeout(400)
check(
  await visible(sugg.locator('tr.wt-sugg', { hasText: SUGGEST_HOST }).first(), 5000),
  "the watch table's filter leaves the suggestions body alone -- a suggestion is not a watch",
)
await page.locator('#panel-watchlist thead input').first().fill('')
await page.waitForTimeout(400)

// --- not this / show them / bring it back ---------------------------------

await deviceRow.click()
const deviceDrawer = sugg.locator('tr.wt-drawer').first()
check(await visible(deviceDrawer), "a suggestion's drawer opens as a watch's does")
check(
  (await deviceDrawer.locator('.dwr-acts button').count()) >= 2,
  "the suggestion drawer carries its verbs at the foot of the drawer",
)

const acceptLabel = ((await deviceDrawer.locator('.dwr-acts button').first().textContent()) ?? '').trim()
check(
  acceptLabel.startsWith('watch it'),
  `a device suggestion's first verb is the accept verb (got ${JSON.stringify(acceptLabel)})`,
)

await deviceDrawer.locator('.dwr-acts button', { hasText: 'not this' }).click()
await page.waitForTimeout(800)
check(
  (await sugg.locator('tr.wt-sugg', { hasText: SUGGEST_HOST }).count()) === 0,
  '`not this` sets the suggestion aside -- it leaves the open list',
)

const showThem = sugg.locator('.sdr button.slink', { hasText: 'set aside' })
check(await visible(showThem), 'the heading offers `n set aside · show them`')
await showThem.click()
await page.waitForTimeout(400)
const asideRow = sugg.locator('tr.wt-aside', { hasText: SUGGEST_HOST }).first()
check(await visible(asideRow), '`show them` reveals the set-aside rows')

await asideRow.click()
const asideDrawer = sugg.locator('tr.wt-drawer.wt-aside').first()
const backBtn = asideDrawer.locator('.dwr-acts button', { hasText: 'bring it back' })
check(await visible(backBtn), 'a set-aside suggestion offers one verb, `bring it back`')
await backBtn.click()
await page.waitForTimeout(800)
check(
  await visible(sugg.locator('tr.wt-sugg', { hasText: SUGGEST_HOST }).first()),
  '`bring it back` returns it to the open suggestions -- nothing is thrown away from here',
)

// --- watch it: the row moves up among the watches, learning ---------------

const acceptRow = sugg.locator('tr.wt-sugg', { hasText: SUGGEST_HOST }).first()
await acceptRow.click()
const acceptDrawer = sugg.locator('tr.wt-drawer.wt-sugg').first()
await acceptDrawer.locator('.dwr-acts button', { hasText: 'watch it' }).click()
await page.waitForTimeout(1500)

const created = await api('GET', '/api/definitions')
const madeEntry = (created.body?.definitions ?? created.body ?? []).find?.(
  (e) => (e.name ?? '').includes(SUGGEST_HOST),
)
check(!!madeEntry, 'accepting a device suggestion created a real watchlist entry')
check(
  madeEntry?.expectation?.observing !== false,
  'the accepted device entry arrives observing -- it learns first, it does not enforce from nothing',
)

const learnRow = page.locator('#panel-watchlist tbody:not(#sugg) tr.wt-row', { hasText: SUGGEST_HOST }).first()
check(
  await visible(learnRow),
  'the accepted row moves up among the watches, out of the suggestions body',
)
// Round 33 draws the accepted device arriving as `◌ learning — nothing
// seen yet`, and that is what it reads whenever mikroview can see
// logging for it. It is not the only honest answer: #680 settled the
// state precedence as paused > no logging visible > ring broken >
// watching, and a brand-new entry on an instance with no logging
// coverage for that host legitimately leads with the sight problem
// instead -- telling an operator "learning" about a watch mikroview
// cannot see would be the wrong sentence first.
//
// So this asserts the two things that must hold either way: the row is
// not enforcing, and it is not claiming to be watching normally. The
// "it arrives observing" claim is made against the API above, which is
// where it is unambiguous.
const learnChip = ((await learnRow.locator('.wchip2').textContent()) ?? '').trim()
check(
  learnChip.includes('learning') || learnChip.includes('no logging visible'),
  `the accepted device arrives learning, or says why it cannot yet (got ${JSON.stringify(learnChip)})`,
)
check(
  !learnChip.includes('◉ watching'),
  `the accepted device does not arrive enforcing (got ${JSON.stringify(learnChip)})`,
)

// ==========================================================================
// PART 4 -- START OVER: ARM, CONFIRM, AND THE BODY IT LEAVES
// ==========================================================================

// Located by class, never by its text. The whole point of this control
// is that its wording changes when armed, so a `hasText: 'start over'`
// locator stops matching the moment the first click lands and every
// later read of it times out against an element that is on screen.
// `.quiet` is what distinguishes the reset from the `show them` pill
// beside it, and it survives arming (which only adds `.armed`).
const resetBtn = sugg.locator('.sdr button.slink.quiet')
check(await visible(resetBtn), 'the heading offers `start over — wipe every watch`')
check(
  ((await resetBtn.textContent()) ?? '').trim().startsWith('start over'),
  'and it says what it will do before it is clicked, not after',
)

await resetBtn.click()
await page.waitForTimeout(300)
const armedText = ((await resetBtn.textContent()) ?? '').trim()
check(
  armedText.startsWith('confirm —'),
  `one click arms the reset and says what it will do (got ${JSON.stringify(armedText)})`,
)

// Any other click disarms, per round 28's gesture. Proving it disarms
// is proving the second click is a deliberate one.
await page.locator('#panel-watchlist').click({ position: { x: 5, y: 5 } })
await page.waitForTimeout(300)
check(
  ((await resetBtn.textContent()) ?? '').trim().startsWith('start over'),
  'any other click disarms it again',
)

// Count watches, not definitions: /api/definitions lists the shipped
// detectors too, which no reset deletes, so counting everything reads a
// complete wipe as 18 -> 17 and fails (first dev gate after #819).
const watchCount = async () => {
  const listed = await api('GET', '/api/definitions')
  return (listed.body?.definitions ?? listed.body ?? []).filter((d) => d.intent === 'expectation').length
}
const countBefore = await watchCount()
check(countBefore > 0, `there are watches to wipe before the reset (${countBefore})`)

await resetBtn.click()
await page.waitForTimeout(300)
await resetBtn.click()
await page.waitForTimeout(2500)

const countAfter = await watchCount()
check(countAfter === 0, `the confirmed reset deleted every watchlist entry (${countBefore} → ${countAfter})`)

check(
  await visible(page.locator('tr.wempty')),
  'the watch body says `Started over.` where the rows were',
)
const startedOver = ((await page.locator('tr.wempty').textContent()) ?? '').trim()
check(
  startedOver.includes('Started over.') && startedOver.includes('rebuilt from the'),
  `that row explains what happens next (got ${JSON.stringify(startedOver.slice(0, 120))})`,
)

// The eye in the chrome reads 0 -- the count is of watches, and there
// are none. Read rather than asked for visibility: it is a small inline
// mark that can be zero-height.
const eyeText = await page
  .locator('.scstatus .wmk, .wmk')
  .evaluateAll((els) => els.map((e) => (e.textContent ?? '').trim()).join(' '))
check(/\b0\b/.test(eyeText) || eyeText === '', `the chrome's watch count reads 0 after the reset (got ${JSON.stringify(eyeText)})`)

// ==========================================================================
// PART 5 -- WHO CAN REACH ANY OF THIS
// ==========================================================================

const anonSuggest = await fetch(`${URL_BASE}/api/suggestions`)
check(anonSuggest.status === 401, `an unauthenticated request to /api/suggestions is refused (${anonSuggest.status})`)
const anonMerged = await fetch(`${URL_BASE}/api/matches?entries=all`)
check(anonMerged.status === 401, `an unauthenticated merged query is refused (${anonMerged.status})`)

const VIEWER_USER = 'live-viewer-771'
const VIEWER_PASS = 'live-viewer-771-password'

const createRes = await api('POST', '/api/auth/users', {
  username: VIEWER_USER,
  password: VIEWER_PASS,
  role: 'viewer',
})
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
  `Watchlist -- and the suggestions and matches inside it -- is absent from a viewer's roll rail, not disabled -- the rail shows ${JSON.stringify(viewerLabels)}`,
)
check((await viewerPage.locator('#sugg').count()) === 0, 'no suggestions body exists anywhere in a viewer session')
check((await viewerPage.locator('.matches').count()) === 0, 'no match list exists anywhere in a viewer session')

await browser.close()

// --- Cleanup: the account, and any entry the reset did not take ----------
// The match records deliberately survive their entries (that is what a
// match outliving its watch means), so only the entries and the account
// go.

const leftovers = await api('GET', '/api/definitions')
for (const e of leftovers.body?.definitions ?? leftovers.body ?? []) {
  if ((e.name ?? '').startsWith('live r33') || (e.name ?? '').includes(SUGGEST_HOST)) {
    await api('DELETE', `/api/definitions/${e.id}`)
  }
}

const users = await api('GET', '/api/auth/users')
const viewerAccount = (Array.isArray(users.body) ? users.body : []).find((u) => u.username === VIEWER_USER)
if (viewerAccount) {
  const del = await api('DELETE', `/api/auth/users/${encodeURIComponent(viewerAccount.id)}`)
  check(del.status < 300, `the viewer account "${VIEWER_USER}" is removed again (${del.status})`)
} else {
  check(false, `could not find the viewer account "${VIEWER_USER}" to clean it up`)
}

done()
