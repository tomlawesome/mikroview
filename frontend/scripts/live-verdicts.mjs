// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #780 (rounds 34-35) mounted the verdicts on the flag row itself;
// #640 replaced what they say. A fresh flag offers expected · checked ·
// investigate, an investigated one expected · resolved, and all four are
// user-tier -- the noise chip, the plain clear and the admin-only
// "never again" (with the exclusions body that reviewed it) are gone
// from this scene entirely, so nothing below reaches for them.
//
// The sized half of expected -- absorbing within tolerance, returning
// above it -- is live-flags-expectations.mjs. This scenario is about the
// row: which chips are offered, what a call stamps, and that undo puts
// it back.
//
// Real flags, real clicks: four independent port_scan sources so the
// checked/undo, investigate, and reload paths each run against their own
// row rather than several assertions racing one.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario in this directory (checked against
// every 198.51.100.* literal already in use here before picking these).
const CHECKED_IP = '198.51.100.104'
const INVESTIGATE_IP = '198.51.100.105'
const RESOLVED_IP = '198.51.100.106'
const RELOAD_IP = '198.51.100.107'

feedPortScan(20, CHECKED_IP)
feedPortScan(20, INVESTIGATE_IP)
feedPortScan(20, RESOLVED_IP)
feedPortScan(20, RELOAD_IP)

const { page, consoleErrors } = await session()

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 10000 })
}

function rowFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}")`)
}

function drawerFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}") + tr.drawer`)
}

const badge = page.locator('.card[aria-hidden="false"] .scene-bar .fmk')

async function badgeCount() {
  const text = (await badge.textContent())?.trim() ?? ''
  return Number(text.replace(/[^\d]/g, ''))
}

async function flagByTarget(target) {
  const res = await page.request.get(`${process.env.MV_URL}/api/flags`)
  const body = await res.json()
  return (body.flags ?? []).find((f) => f.target === target)
}

// Polls rather than checking once: even a same-process, awaited POST
// still has real network latency, and a single immediate check would
// occasionally read the instant before the response landed rather than
// proving anything about whether it landed at all.
async function waitForApiVerdict(target, predicate, { timeoutMs = 5000 } = {}) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await flagByTarget(target)
    if (last && predicate(last)) return { ok: true, flag: last }
    await page.waitForTimeout(300)
  }
  return { ok: false, flag: last }
}

// Server-side first (#354): a locator timeout here can't say whether a
// scan never raised a flag at all or just hasn't rendered yet.
const raised = []
for (const ip of [CHECKED_IP, INVESTIGATE_IP, RESOLVED_IP, RELOAD_IP]) {
  const r = await waitForFlag(page, ip)
  check(r.ok, r.message)
  raised.push(r)
}

if (raised.every((r) => r.ok)) {
  await openFlags()

  // --- A fresh flag offers exactly the three chips -------------------

  const checkedRow = rowFor(CHECKED_IP)
  await checkedRow.waitFor({ timeout: 15000 })
  check(
    !(await checkedRow.evaluate((el) => el.classList.contains('open'))),
    "the row's drawer starts closed, never opened for a call",
  )
  check(await checkedRow.locator('button.v.expected').isVisible(), 'expected is reachable straight off the row')
  check(await checkedRow.locator('button.v.checked').isVisible(), 'checked is too')
  check(await checkedRow.locator('button.v.investigate').isVisible(), 'and investigate')
  check(
    (await checkedRow.locator('button.v.resolved').count()) === 0,
    'resolved is not offered on a flag nobody is investigating yet',
  )

  // --- Call checked, with the drawer closed --------------------------

  const beforeChecked = await badgeCount()
  await checkedRow.locator('button.v.checked').click()
  await checkedRow.locator('.stamp.checked').waitFor({ timeout: 5000 })

  check(
    (await checkedRow.locator('.stamp.checked').textContent())?.trim() === 'checked',
    'calling checked stamps the row in its own CALL IT cell',
  )
  check(await checkedRow.locator('.olink', { hasText: 'undo' }).isVisible(), 'undo is offered beside the stamp')
  check(
    !(await checkedRow.evaluate((el) => el.classList.contains('open'))),
    'the click never toggled the drawer open',
  )
  check((await drawerFor(CHECKED_IP).count()) === 0, 'no drawer row exists for the call at all')

  await page.waitForFunction(
    (before) => {
      const el = document.querySelector('.card[aria-hidden="false"] .scene-bar .fmk')
      if (!el) return false
      const n = Number((el.textContent ?? '').replace(/[^\d]/g, ''))
      return !Number.isNaN(n) && n === before - 1
    },
    beforeChecked,
    { timeout: 10000 },
  )
  check(true, `the chrome's ⚑ counted down once the checked call landed (from ${beforeChecked})`)

  // --- Undo puts it back exactly --------------------------------------

  await checkedRow.locator('.olink', { hasText: 'undo' }).click()
  await checkedRow.locator('button.v.checked').waitFor({ timeout: 5000 })
  check((await checkedRow.locator('.stamp').count()) === 0, 'undo removes the stamp')
  check(
    await checkedRow.locator('button.v.expected').isVisible(),
    'undo restores the whole row, not just the chip that was clicked',
  )

  await page.waitForFunction(
    (before) => {
      const el = document.querySelector('.card[aria-hidden="false"] .scene-bar .fmk')
      if (!el) return false
      const n = Number((el.textContent ?? '').replace(/[^\d]/g, ''))
      return !Number.isNaN(n) && n === before
    },
    beforeChecked,
    { timeout: 10000 },
  )
  check(true, "undo puts the flag back on the chrome's ⚑ count too")

  // --- Investigate: the row stays, and its chips change --------------

  const investigateRow = rowFor(INVESTIGATE_IP)
  await investigateRow.locator('button.v.investigate').click()
  await investigateRow.locator('button.v.resolved').waitFor({ timeout: 5000 })

  check(await investigateRow.isVisible(), 'the row stays -- an investigate verdict never clears the flag')
  check(
    await investigateRow.locator('button.v.expected').isVisible(),
    'an investigated row offers expected · resolved',
  )
  check(
    (await investigateRow.locator('button.v.checked').count()) === 0,
    'checked is gone from it -- it has already been looked at',
  )
  check(await investigateRow.locator('.openc').isVisible(), 'the caret is still there for an investigated row')

  await investigateRow.click()
  await page.waitForSelector(`tr.frow:has-text("${INVESTIGATE_IP}") + tr.drawer`, { timeout: 5000 })
  check(
    ((await drawerFor(INVESTIGATE_IP).locator('.story .called').textContent()) ?? '').startsWith(
      'Being investigated since',
    ),
    'the drawer story leads with the "being investigated" line',
  )
  await investigateRow.locator('.openc').click()

  // --- Resolved: clears, and records no suppression ------------------

  const resolvedRow = rowFor(RESOLVED_IP)
  await resolvedRow.locator('button.v.investigate').click()
  await resolvedRow.locator('button.v.resolved').waitFor({ timeout: 5000 })
  await resolvedRow.locator('button.v.resolved').click()
  await resolvedRow.locator('.stamp.resolved').waitFor({ timeout: 5000 })
  check(true, 'calling resolved from an investigated row stamps RESOLVED and clears it')

  const resolvedFlag = await waitForApiVerdict(RESOLVED_IP, (f) => f.cleared && f.verdict === 'resolved')
  check(
    resolvedFlag.ok,
    `the resolved verdict reached the server and cleared the flag (got: ${JSON.stringify(resolvedFlag.flag)})`,
  )

  // --- Reload immediately after judging: the case that caught a real
  // defect, carried forward from the retired live-flag-verdicts.mjs ---
  //
  // An earlier version of judgeAndClear deferred the POST itself behind
  // the undo window instead of sending it at once. This exact case --
  // judge, then reload right away, well inside that window -- is what
  // caught the defect that replaced it: the PWA's own service worker
  // re-issues every fetch through itself (vite.config.ts's
  // registerType: 'autoUpdate' sets clientsClaim), which strips the
  // keepalive guarantee a page-teardown request depends on, so a
  // verdict judged just before a reload reached the server 0 times out
  // of 6 in testing -- silently, and only on a properly-certificated
  // deployment. Posting at once (see flags.svelte.ts's judgeAndClear
  // doc comment) leaves nothing in flight for the reload to lose, so
  // this must still show the verdict landed even though the page
  // navigated away before the request's own promise ever resolved.
  //
  // Checked, not expected: this leg is about the request surviving, and
  // an expected verdict would leave a real expectation on a shared
  // instance for the sake of testing something else.

  await openFlags()
  const reloadRow = rowFor(RELOAD_IP)
  await reloadRow.waitFor({ timeout: 15000 })
  await reloadRow.locator('button.v.checked').click()
  await page.waitForTimeout(200)

  await page.reload({ waitUntil: 'networkidle' })

  const reloadResult = await waitForApiVerdict(RELOAD_IP, (f) => f.cleared && f.verdict === 'checked')
  check(
    reloadResult.ok,
    `a verdict judged just before an immediate reload is on the server afterward -- posted at once, nothing for the reload to lose (got: ${JSON.stringify(reloadResult.flag)})`,
  )

  // The reloaded UI reflects it too -- not by a persistent badge (#780's
  // design has no permanent cleared-flags view; a called-and-cleared row
  // is pinned in place only for the visit that called it, per pin's own
  // doc comment), but by the row genuinely being gone from Active: a
  // fresh mount with no pin and a truly uncleared flag would still show
  // it, so its absence here is what proves the reload's fresh state
  // agrees with the server rather than the UI just not having noticed
  // yet.
  await openFlags()
  check((await rowFor(RELOAD_IP).count()) === 0, 'the reloaded, unpinned UI no longer shows the judged row as open')
} else {
  check(true, 'skipped -- the verdict row cannot be driven without its four port-scan flags')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
