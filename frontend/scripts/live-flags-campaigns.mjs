// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #988 (round 47): the flags one source raises inside one
// 30-minute window fold into a campaign row, and the by-type strip above
// the table counts what the table holds. Driven end to end -- two real
// detectors on one LAN source, the real store, the real page -- because
// the fold is only true if the two flags actually carry the same source
// IP in `target` once they have been through the engine, which the unit
// tests take on trust.
//
//   1. A port scan and an internal sweep from one LAN host raise flags
//      of at least two types, and the table shows them as one
//      `⁂ CAMPAIGN · N flags` row naming that host, not as N rows.
//      N comes from the API, not the script: the sweep is on one port,
//      so repeated_drops fires for the same source too, and a third
//      flag in the fold is the fold working, not a fault.
//   2. The row opens on a click to its N members, one step in, under
//      the rule line.
//   3. The strip has a cell per open type, and the campaign's types are
//      among them.
//
// No verdict is given, so nothing is left behind for a repeat run to
// trip over; the flags themselves are ordinary open flags.

import { session, check, done, feedPortScan, feedInternalRecon, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario in this directory -- checked against
// every 192.168.1.* literal already in use here before picking it. A
// LAN address, because internal_recon only fires for a LAN source
// sweeping LAN destinations; port_scan does not care where its source
// is.
const SRC = '192.168.1.63'

const { page, consoleErrors } = await session()

feedPortScan(20, SRC)
feedInternalRecon(12, SRC, 445)

// Server-side first (#354): a locator timeout cannot say whether a
// detector raised nothing or the row merely had not rendered yet. The
// flags share a source, not a target: repeated_drops writes
// `<ip> -> port N`, so match the leading IP the way extractSourceIp in
// lib/flags.svelte.ts does, and wait for both types by hand.
async function flagsFor(ip) {
  const res = await page.request.get(`${process.env.MV_URL}/api/flags`)
  const body = await res.json()
  return (body.flags ?? []).filter((f) => (f.target === ip || f.target.startsWith(`${ip} `)) && !f.cleared)
}

const first = await waitForFlag(page, SRC)
check(first.ok, first.message)

let both = []
const deadline = Date.now() + 20000
while (Date.now() < deadline) {
  both = await flagsFor(SRC)
  if (new Set(both.map((f) => f.type)).size >= 2) break
  await page.waitForTimeout(250)
}
const types = [...new Set(both.map((f) => f.type))].sort()
check(
  types.includes('port_scan') && types.includes('internal_recon'),
  `both detectors raised a flag on ${SRC} (got ${JSON.stringify(types)})`,
)

if (types.length >= 2) {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 10000 })

  // --- 1. One campaign row, not N flag rows --------------------------

  const camp = page.locator(`tr.frow.camp:has-text("${SRC}")`)
  await camp.waitFor({ timeout: 15000 })
  check((await camp.count()) === 1, `one campaign row names ${SRC}`)

  // The detectors may still be raising (the drops flag lands after the
  // sweep), so the count is settled when the row and the API agree, not
  // after a fixed sleep. What the page shows is checked against what the
  // server holds, never against itself.
  let n = 0
  let flagCell = ''
  const settle = Date.now() + 15000
  while (Date.now() < settle) {
    n = (await flagsFor(SRC)).length
    flagCell = (await camp.locator('td.fmark').textContent())?.replace(/\s+/g, ' ').trim() ?? ''
    if (n >= 2 && new RegExp(`^⁂ campaign\\s*${n} flags$`, 'i').test(flagCell)) break
    await page.waitForTimeout(250)
  }
  const word = ['zero', 'one', 'two', 'three', 'four', 'five'][n] ?? String(n)
  check(
    new RegExp(`^⁂ campaign\\s*${n} flags$`, 'i').test(flagCell),
    `its FLAG cell counts the ${word} flags the API holds for ${SRC} (got "${flagCell}")`,
  )
  check(
    (await page.locator(`tr.frow:not(.camp):not(.mem):has-text("${SRC}")`).count()) === 0,
    'and neither flag has a row of its own outside it',
  )
  check((await page.locator('tr.frow.mem').count()) === 0, 'its members are not shown while it is closed')

  const evidence = (await camp.locator('td.ev').textContent())?.replace(/\s+/g, ' ').trim() ?? ''
  check(
    /Port scan/.test(evidence) && /Internal reconnaissance/.test(evidence) && /→ still arriving/.test(evidence),
    `EVIDENCE names each type inside and the span (got "${evidence}")`,
  )

  // --- 2. Opens to its members under the rule line -------------------

  // WHERE and EVIDENCE must not move when the members step in: the FLAG
  // column is pinned (round 47's own check). Measured from the row's own
  // FLAG cell, not the viewport: the members add height, the page grows
  // a scrollbar, and the whole table shifts a scrollbar's width -- which
  // is not the column giving way. Before the pin the column itself
  // widened by 92px here.
  const whereAt = async () => {
    const [flag, where] = await Promise.all([camp.locator('td.fmark').boundingBox(), camp.locator('td.k').boundingBox()])
    return Math.round((where?.x ?? 0) - (flag?.x ?? 0))
  }
  const whereBefore = await whereAt()
  await camp.click()
  const members = page.locator('tr.frow.mem')
  await members.first().waitFor({ timeout: 5000 })
  check((await members.count()) === n, `the campaign opens to its ${word} members (got ${await members.count()})`)
  const rule = (await page.locator('tr.crule td').textContent())?.replace(/\s+/g, ' ').trim() ?? ''
  check(
    rule.startsWith(`one source, ${word} flags, each inside 30 minutes of the last — one campaign.`),
    `the rule line says why they are one (got "${rule}")`,
  )
  const whereAfter = await whereAt()
  check(whereBefore === whereAfter, `opening the campaign leaves WHERE where it was (${whereBefore}px → ${whereAfter}px from FLAG)`)

  // A member opens its own drawer, the round 29 drawer, one step in.
  await members.first().click()
  const drawer = page.locator('tr.drawer.inc')
  await drawer.waitFor({ timeout: 5000 })
  check((await drawer.count()) === 1, "a member's drawer opens beneath it, carrying the member's step")

  // --- 3. The strip counts what the table holds ----------------------

  const strip = page.locator('.bytype')
  check((await strip.count()) === 1, 'the by-type strip sits above the table')
  const cells = await page.locator('.btc .btn').allTextContents()
  const names = cells.map((c) => c.replace(/\s+/g, ' ').trim())
  check(
    names.some((n) => /Port scan/.test(n)) && names.some((n) => /Internal reconnaissance/.test(n)),
    `the strip has a cell for each of the campaign's types (got ${JSON.stringify(names)})`,
  )

  // Click the recon cell: the table narrows to that type, and the
  // campaign is left opened to its one recon member. The strip counts
  // every open flag on the page (byType is built from `active`, not
  // this campaign alone), so a filter that matches recon force-opens
  // *any* campaign holding a recon member -- including one from another
  // source the gate's earlier scripts left open. Scoping the wait and
  // the member lookup to SRC's own campaign is what "one recon member"
  // actually means here; counting every `tr.frow.mem` on the page is
  // only right in an empty table, which a shared gate host never is.
  await page.locator('.btc:has-text("Internal reconnaissance")').click()
  await page.waitForFunction(
    (src) => [...document.querySelectorAll('tr.frow.mem')].filter((r) => r.textContent?.includes(src)).length === 1,
    SRC,
    { timeout: 5000 },
  )
  const filterValue = await page.locator('input[aria-label="Filter by flag type"]').inputValue()
  check(filterValue === 'Internal reconnaissance', `the FLAG filter reads the picked type (got "${filterValue}")`)
  const srcMembers = page.locator('tr.frow.mem', { hasText: SRC })
  check((await srcMembers.count()) === 1, `only the recon member is left inside ${SRC}'s campaign (got ${await srcMembers.count()})`)
  const memberType = (await srcMembers.first().locator('td.fmark').textContent())?.trim()
  check(/Internal reconnaissance/.test(memberType ?? ''), `only the recon member is left inside the campaign (got "${memberType}")`)

  await page.locator('.btc:has-text("Internal reconnaissance")').click()
  await page.waitForFunction(() => document.querySelector('input[aria-label="Filter by flag type"]').value === '', null, { timeout: 5000 })
  check(true, 'clicking the cell again clears the filter')
}

check(consoleErrors.length === 0, `no console errors (got ${consoleErrors.length})`)
done()
