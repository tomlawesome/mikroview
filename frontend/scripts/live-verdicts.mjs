// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #780 (rounds 34-35): verdicts, stamps and exclusions mounted on
// the flags tab. Supersedes live-flag-verdicts.mjs outright (deleted by
// this same change, not merely edited) -- that scenario drove the old
// card-grid's `.verdict-row`/`.verdict-btn-real`/`.verdict-badge`
// buttons, which round 29's plain-table rewrite had already retired
// before this issue existed; #780 goes on to retire the drawer-only
// buttons that replaced them, too. There is nothing left in that
// scenario worth carrying forward -- every one of its selectors targets
// markup this build no longer has anywhere.
//
// Real flags, real clicks: three independent port_scan sources so the
// noise/undo, real, and never-again/exclusions paths each run against
// their own flag rather than three assertions racing one shared row.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// Unused by every other scenario in this directory (checked against
// every 198.51.100.* literal already in use here before picking these).
const NOISE_IP = '198.51.100.104'
const REAL_IP = '198.51.100.105'
const NEVER_IP = '198.51.100.106'
const RELOAD_IP = '198.51.100.107'

feedPortScan(20, NOISE_IP)
feedPortScan(20, REAL_IP)
feedPortScan(20, NEVER_IP)
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
for (const ip of [NOISE_IP, REAL_IP, NEVER_IP, RELOAD_IP]) {
  const r = await waitForFlag(page, ip)
  check(r.ok, r.message)
  raised.push(r)
}

if (raised.every((r) => r.ok)) {
  await openFlags()

  // --- Call noise, with the drawer closed (item 1/2) -----------------

  const noiseRow = rowFor(NOISE_IP)
  await noiseRow.waitFor({ timeout: 15000 })
  check(
    !(await noiseRow.evaluate((el) => el.classList.contains('open'))),
    "the noise row's drawer starts closed, never opened for this call",
  )
  check(await noiseRow.locator('button.v.noise').isVisible(), 'the noise chip is reachable straight off the row')

  const beforeNoise = await badgeCount()
  await noiseRow.locator('button.v.noise').click()
  await noiseRow.locator('.stamp.noise').waitFor({ timeout: 5000 })

  check(
    (await noiseRow.locator('.stamp.noise').textContent())?.trim() === 'noise',
    'calling noise stamps the row in its own CALL IT cell',
  )
  check(
    await noiseRow.locator('.olink', { hasText: 'undo' }).isVisible(),
    'undo is offered beside the stamp',
  )
  check(
    !(await noiseRow.evaluate((el) => el.classList.contains('open'))),
    'the click never toggled the drawer open',
  )
  check((await drawerFor(NOISE_IP).count()) === 0, 'no drawer row exists for the call at all')

  await page.waitForFunction(
    (before) => {
      const el = document.querySelector('.card[aria-hidden="false"] .scene-bar .fmk')
      if (!el) return false
      const n = Number((el.textContent ?? '').replace(/[^\d]/g, ''))
      return !Number.isNaN(n) && n === before - 1
    },
    beforeNoise,
    { timeout: 10000 },
  )
  check(true, `the chrome's ⚑ counted down once the noise call landed (from ${beforeNoise})`)

  // --- Undo puts it back exactly --------------------------------------

  await noiseRow.locator('.olink', { hasText: 'undo' }).click()
  await noiseRow.locator('button.v.noise').waitFor({ timeout: 5000 })
  check((await noiseRow.locator('.stamp').count()) === 0, 'undo removes the stamp')
  check(await noiseRow.locator('button.v.expected').isVisible(), 'undo restores the whole trio, not just noise')

  await page.waitForFunction(
    (before) => {
      const el = document.querySelector('.card[aria-hidden="false"] .scene-bar .fmk')
      if (!el) return false
      const n = Number((el.textContent ?? '').replace(/[^\d]/g, ''))
      return !Number.isNaN(n) && n === before
    },
    beforeNoise,
    { timeout: 10000 },
  )
  check(true, "undo puts the flag back on the chrome's ⚑ count too")

  // --- Call real: stamp in CALL IT, row stays, caret and undo remain -

  const realRow = rowFor(REAL_IP)
  await realRow.locator('button.v.real').click()
  await realRow.locator('.stamp.real').waitFor({ timeout: 5000 })

  check(
    (await realRow.locator('.stamp.real').textContent())?.trim() === 'real',
    'calling real stamps REAL in the same CALL IT column',
  )
  check(await realRow.isVisible(), 'the row stays -- a real verdict never clears the flag')
  check(await realRow.locator('.openc').isVisible(), 'the caret is still there for a real row')
  check(
    await realRow.locator('.olink', { hasText: 'undo' }).isVisible(),
    'undo is offered for a real verdict too, with no window on it',
  )

  await realRow.click()
  await page.waitForSelector(`tr.frow:has-text("${REAL_IP}") + tr.drawer`, { timeout: 5000 })
  check(
    ((await drawerFor(REAL_IP).locator('.story .called').textContent()) ?? '').startsWith('Called real at'),
    'the drawer story leads with the "Called real" line',
  )

  // --- Never again: arm, confirm, listed in the exclusions body ------

  const neverRow = rowFor(NEVER_IP)
  await neverRow.locator('.openc').click()
  const neverDrawer = drawerFor(NEVER_IP)
  await neverDrawer.waitFor({ timeout: 5000 })

  const neverBtn = neverDrawer.locator('button.never')
  await neverBtn.click()
  check(
    ((await neverBtn.textContent()) ?? '').trim().startsWith('confirm — Port scan never fires again for'),
    'a first click arms never again with a confirm label, rather than acting immediately',
  )

  await neverBtn.click()
  await neverRow.locator('.stamp', { hasText: 'never again' }).waitFor({ timeout: 15000 })
  check(true, 'a second click calls clear-permanent and stamps NEVER AGAIN on the row')
  check((await neverRow.locator('.olink', { hasText: 'undo' }).count()) === 0, 'no undo on a never-again row')

  const showThem = page.locator('button', { hasText: 'show them' })
  await showThem.waitFor({ timeout: 10000 })
  await showThem.click()
  const excludedRow = page.locator(`tr.frow.fx:has-text("${NEVER_IP}")`)
  await excludedRow.waitFor({ timeout: 5000 })
  check(await excludedRow.isVisible(), 'the pair shows up in the exclusions body once shown')

  // --- Let it fire again: removed from the body, and can raise anew --

  await excludedRow.locator('.openc').click()
  await page.locator('button', { hasText: 'let it fire again' }).click()
  await page.waitForSelector(`tr.frow.fx:has-text("${NEVER_IP}")`, { state: 'detached', timeout: 10000 })
  check(true, 'let it fire again removes the pair from the exclusions body')

  const exclusionsAfter = await page.request
    .get(`${process.env.MV_URL}/api/flags/exclusions`)
    .then((r) => r.json())
    .then((b) => b.exclusions ?? [])
  check(
    !exclusionsAfter.some((e) => e.target === NEVER_IP),
    'the exclusion is gone server-side too, not just optimistically hidden',
  )

  // A fresh scan at the same source proves detection is actually
  // re-armed, not just that the list entry disappeared.
  feedPortScan(20, NEVER_IP)
  const refired = await waitForFlag(page, NEVER_IP)
  check(refired.ok, `letting it fire again actually lets it fire again: ${refired.message}`)

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

  await openFlags()
  const reloadRow = rowFor(RELOAD_IP)
  await reloadRow.waitFor({ timeout: 15000 })
  await reloadRow.locator('button.v.expected').click()
  await page.waitForTimeout(200)

  await page.reload({ waitUntil: 'networkidle' })

  const reloadResult = await waitForApiVerdict(RELOAD_IP, (f) => f.cleared && f.verdict === 'expected')
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
  check(true, 'skipped -- the verdict/exclusion flow cannot run without its four port-scan flags')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
