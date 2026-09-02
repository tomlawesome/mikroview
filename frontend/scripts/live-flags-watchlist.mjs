// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #641: a verdict writes to the watchlist. Both halves only exist
// end to end -- a real detector recording real evidence pairs, a real
// definitions store holding the entry, and the real form the draft opens
// in -- so both are driven here rather than asserted from unit tests:
//
//   1. Expected is automatic. Judging an internal_recon flag as expected
//      records the destinations it actually reached as permitted on the
//      device's inverted watchlist entry, creating that entry in its
//      observing state because the device had none. Nothing fires from
//      it: it observes.
//   2. Undo takes both back -- the permission, and the entry that
//      existed only to hold it.
//   3. Resolved offers. The undo line reads "resolved — undo · watch for
//      this"; taking it opens the Watchlist's own entry form prefilled,
//      and *discarding* returns the operator to the flags inbox with
//      nothing created. The flag stays resolved.
//   4. Taking it a second time and saving creates the watch, and returns
//      to the inbox the same way.
//
// The permitted destinations are compared against the flag's own
// evidence read off the API, not hardcoded: what the detector saw is
// what must be permitted, and asserting that relationship is the real
// claim. internal_recon is the detector under test because #641 is what
// turned evidence pairs on for it (with outbound_anomaly).
//
// **State this leaves behind.** Step 4 creates a real watch on
// 192.168.1.62, and steps 1-3 leave a real expectation on
// (internal_recon, 192.168.1.60) and (internal_recon, 192.168.1.61).
// `make live-check` starts from a wiped data directory, so a full gate
// run is unaffected. Run twice against one long-lived instance (the
// driving lane) and the first check fails by name rather than timing
// out: the expectation from the previous run absorbs the flag before it
// is ever raised. There is no API to prune an expectation until #640
// part C lands the ledger.

import { session, check, done, feedInternalRecon, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario here -- checked against the 192.168.1.*
// literals already in use (the router is .1, the port-scan target .10,
// and this sweep's destinations run .100 upward).
const EXPECT_IP = '192.168.1.60'
const RESOLVE_IP = '192.168.1.61'
const UNDO_IP = '192.168.1.62'

feedInternalRecon(12, EXPECT_IP, 445)
feedInternalRecon(12, RESOLVE_IP, 3389)
feedInternalRecon(12, UNDO_IP, 22)

const { page, consoleErrors } = await session()

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 10000 })
}

function rowFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}")`)
}

async function api(path) {
  const res = await page.request.get(`${process.env.MV_URL}${path}`)
  return res.json()
}

async function flagByTarget(target) {
  const body = await api('/api/flags')
  return (body.flags ?? []).find((f) => f.target === target)
}

// The watchlist's entries are expectation definitions (#407), which is
// what the page itself reads too -- see fetchWatchlistEntries.
async function entriesFor(ip) {
  const body = await api('/api/definitions')
  return (body.definitions ?? [])
    .filter((d) => d.intent === 'expectation' && d.expectation)
    .map((d) => d.expectation)
    .filter((e) => e.source?.ip === ip)
}

async function waitFor(fn, { timeoutMs = 8000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await fn()
    if (last) return last
    await page.waitForTimeout(300)
  }
  return last
}

const raised = await waitForFlag(page, EXPECT_IP)
check(
  raised.ok,
  `${raised.message}${raised.ok ? '' : " -- if this run is a repeat against a long-lived instance, an expectation from the previous run is absorbing it (see this file's header)"}`,
)
const resolveRaised = await waitForFlag(page, RESOLVE_IP)
check(resolveRaised.ok, resolveRaised.message)
const undoRaised = await waitForFlag(page, UNDO_IP)
check(undoRaised.ok, undoRaised.message)

if (raised.ok && resolveRaised.ok && undoRaised.ok) {
  await openFlags()

  // --- 1. Expected permits what the device was seen doing -------------

  const before = await flagByTarget(EXPECT_IP)
  const pairs = before?.evidence?.pairs ?? []
  check(
    pairs.length >= 10 && pairs.every((p) => p.port === 445),
    `the flag carries the destination/port pairs it saw (got ${JSON.stringify(pairs).slice(0, 200)})`,
  )

  const expectRow = rowFor(EXPECT_IP)
  await expectRow.waitFor({ timeout: 15000 })
  await expectRow.locator('button.v.expected').click()
  await expectRow.locator('.stamp.expected').waitFor({ timeout: 5000 })

  const permitted = await waitFor(async () => {
    const found = await entriesFor(EXPECT_IP)
    return found.length > 0 ? found[0] : null
  })
  check(Boolean(permitted), 'the expected verdict created a watchlist entry for the device')

  if (permitted) {
    check(permitted.invert === true, 'the entry it created is an inverted one -- a policy about where this device goes')
    check(
      permitted.observing === true,
      'and it is observing, so nothing fires from a step the operator never asked for',
    )
    const got = new Set((permitted.permitted ?? []).map((d) => `${d.destIp}:${d.port}`))
    const want = pairs.map((p) => `${p.host}:${p.port}`)
    check(
      want.length > 0 && want.every((k) => got.has(k)),
      `every pair the flag recorded is permitted (${want.length} wanted, ${got.size} present)`,
    )
  }

  // --- 2. Undo takes the permission and the entry back ----------------

  const undoRow = rowFor(UNDO_IP)
  await undoRow.waitFor({ timeout: 15000 })
  await undoRow.locator('button.v.expected').click()
  await undoRow.locator('.stamp.expected').waitFor({ timeout: 5000 })
  const created = await waitFor(async () => {
    const found = await entriesFor(UNDO_IP)
    return found.length > 0 ? found[0] : null
  })
  check(Boolean(created), 'setup: the expected verdict created an entry for the undo case too')

  await undoRow.locator('button.olink', { hasText: 'undo' }).first().click()
  const gone = await waitFor(async () => {
    const found = await entriesFor(UNDO_IP)
    return found.length === 0 ? 'gone' : null
  })
  check(
    gone === 'gone',
    'undoing the verdict removes the permission and the observing entry that existed only to hold it',
  )

  // --- 3. Resolved offers a watcher, and declining costs nothing ------

  const resolveRow = rowFor(RESOLVE_IP)
  await resolveRow.waitFor({ timeout: 15000 })
  await resolveRow.locator('button.v.investigate').click()
  await resolveRow.locator('button.v.resolved').waitFor({ timeout: 5000 })
  await resolveRow.locator('button.v.resolved').click()
  await resolveRow.locator('.stamp.resolved').waitFor({ timeout: 5000 })

  const offer = resolveRow.locator('button.olink', { hasText: 'watch for this' })
  check((await offer.count()) === 1, 'the resolved row offers "watch for this" beside its undo')

  await offer.click()
  const draft = page.locator('.wt-drawer.wt-draft')
  await draft.waitFor({ timeout: 10000 })
  check(true, 'taking the offer opens the watchlist entry form')

  const who = await draft.locator('input[aria-label="Who this watch scopes to"]').inputValue()
  const toward = await draft.locator('input[aria-label="Toward"]').inputValue()
  const provenance = (await draft.locator('.wf-prov').textContent())?.trim() ?? ''
  check(who === RESOLVE_IP, `the form is prefilled with the flag's own host (got "${who}")`)
  check(toward.includes(':3389'), `and with the ports it was seen reaching (got "${toward}")`)
  check(
    provenance.startsWith('From the last firing window') && /bound/.test(provenance),
    `the form states where those values came from and how the host is identified: "${provenance}"`,
  )

  await draft.locator('button.act.quiet', { hasText: 'discard' }).click()
  await page.waitForSelector('table.ftable', { state: 'visible', timeout: 10000 })
  check((await draft.count()) === 0, 'discarding closes the draft')
  check(
    (await rowFor(RESOLVE_IP).isVisible()) === true,
    'and returns the operator to the flags inbox -- declining costs no manual switch back',
  )
  check(
    (await entriesFor(RESOLVE_IP)).length === 0,
    'nothing was created by declining, and the flag stays resolved',
  )
  const stillResolved = await flagByTarget(RESOLVE_IP)
  check(
    stillResolved?.verdict === 'resolved' && stillResolved?.cleared === true,
    `the flag is still resolved after declining (got: ${JSON.stringify(stillResolved?.verdict)})`,
  )

  // --- 4. Taking it creates the watch, and comes back the same way ----

  await rowFor(RESOLVE_IP).locator('button.olink', { hasText: 'watch for this' }).click()
  await draft.waitFor({ timeout: 10000 })
  await draft.locator('button.act', { hasText: 'start watching' }).click()
  await page.waitForSelector('table.ftable', { state: 'visible', timeout: 10000 })

  const watcher = await waitFor(async () => {
    const found = await entriesFor(RESOLVE_IP)
    return found.length > 0 ? found[0] : null
  })
  check(Boolean(watcher), 'saving the draft creates the watch')
  if (watcher) {
    check(!watcher.invert, 'the watcher is a plain watch, not a fence')
    check((watcher.ports ?? []).includes(3389), `it watches the port the flag saw (got ${JSON.stringify(watcher.ports)})`)
  }
  check(
    (await rowFor(RESOLVE_IP).isVisible()) === true,
    'and saving returns the operator to the flags inbox too',
  )
} else {
  check(true, 'skipped -- the watchlist verdict flow cannot run without all three internal_recon flags')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
