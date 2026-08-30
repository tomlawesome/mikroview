// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #638: the three-button verdict row (Expected / Noise / Real) on
// a flag card, Clear demoted to secondary, and the judge-with-undo flow
// for Expected/Noise.
//
// Every verdict posts to the server immediately, on the one press --
// Undo (offered for ~5s afterward) is a real DELETE that reverses an
// already-recorded verdict, not a cancelled request. An earlier version
// of this deferred the POST itself behind the undo window instead, and
// this scenario's own reload-inside-the-window case is what caught the
// defect that replaced it: the PWA's service worker re-issues every
// fetch through itself, which strips the keepalive guarantee a
// page-teardown request depended on, so a verdict judged just before a
// reload reached the server 0 times out of 6. Real flags, real clicks,
// and a real reload rather than trusting the client state -- that gap
// only ever showed up against the real thing.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// Four independent scan sources, unused by every other scenario in this
// directory (checked before choosing them -- #590 is about exactly this
// collision).
const REAL_IP = '198.51.100.100'
const EXPECTED_IP = '198.51.100.101'
const NOISE_IP = '198.51.100.102'
const RELOAD_IP = '198.51.100.103'

// How long Undo stays offered (flags.svelte.ts's VERDICT_UNDO_MS) plus a
// margin -- long enough that a slow CI runner's setTimeout firing a
// little late still lands inside this scenario's own wait.
const UNDO_WAIT_MS = 5800

feedPortScan(20, REAL_IP)
feedPortScan(20, EXPECTED_IP)
feedPortScan(20, NOISE_IP)
feedPortScan(20, RELOAD_IP)

const { page } = await session()

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('#panel-flags', { timeout: 10000 })
}

function cardFor(ip) {
  return page.locator('section[aria-labelledby="active-heading"] .card', { hasText: ip })
}

function clearedCardFor(ip) {
  return page.locator('section[aria-labelledby="cleared-heading"] .card', { hasText: ip })
}

async function flagByTarget(target) {
  const res = await page.request.get(`${URL_BASE}/api/flags`)
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

// Server-side first (#354): a locator timeout here can't say whether the
// scan never raised a flag at all or just hasn't rendered yet.
const raised = []
for (const ip of [REAL_IP, EXPECTED_IP, NOISE_IP, RELOAD_IP]) {
  const r = await waitForFlag(page, ip)
  check(r.ok, r.message)
  raised.push(r)
}

if (raised.every((r) => r.ok)) {
  await openFlags()
  await page.waitForSelector('.card .type', { timeout: 15000 })

  // --- The row itself: bare labels, nothing else, Clear demoted ---

  const realCard = cardFor(REAL_IP)
  await realCard.waitFor({ timeout: 15000 })

  const labels = (await realCard.locator('.verdict-row button').allTextContents()).map((t) => t.trim())
  check(
    labels.length === 3 && labels.join(', ') === 'Expected, Noise, Real',
    `the row shows exactly the three bare labels, in order, no second line (got: ${labels.join(' | ')})`,
  )
  check(
    (await realCard.locator('.split-clear.secondary, .clear.secondary').count()) > 0,
    'Clear is still present on the card, demoted to secondary rather than removed',
  )

  // --- Real: judges without clearing, badge replaces the row ---

  await realCard.locator('.verdict-btn-real').click()
  await page.waitForTimeout(600)

  check(await cardFor(REAL_IP).isVisible(), 'Real does not clear the flag -- the card stays in Active')
  const realBadge = cardFor(REAL_IP).locator('.verdict-badge.verdict-real')
  check(
    (await realBadge.isVisible()) && (await realBadge.textContent())?.trim() === 'Real',
    'a Real verdict shows a plain "Real" badge',
  )
  check(
    (await cardFor(REAL_IP).locator('.verdict-row').count()) === 0,
    'the button row is gone once judged -- a judged flag is never re-presented as an open question',
  )
  const judgedByText = (await cardFor(REAL_IP).locator('.verdict-judged-by').textContent())?.trim() ?? ''
  check(
    judgedByText.length > 0 && judgedByText.includes(process.env.MV_USER ?? ''),
    `the badge names who judged it and when (got: "${judgedByText}")`,
  )

  // Reload to confirm this reached the server, not just optimistic
  // local state -- same reasoning live-flags-layout.mjs's persistence
  // check gives for its own column setting.
  await page.reload({ waitUntil: 'networkidle' })
  await openFlags()
  await page.waitForSelector('.card .type', { timeout: 15000 })
  check(
    await cardFor(REAL_IP).locator('.verdict-badge.verdict-real').isVisible(),
    'the Real verdict survived a reload',
  )

  // --- Expected: one press posts and clears at once; Undo is a real
  // DELETE, not a cancelled request ---

  const expectedCard = cardFor(EXPECTED_IP)
  await expectedCard.waitFor({ timeout: 15000 })

  await expectedCard.locator('.verdict-btn-expected').click()
  await page.waitForTimeout(200)
  check(
    !(await cardFor(EXPECTED_IP).isVisible()),
    'Expected clears the card immediately -- it reads as gone right away',
  )
  check(
    await page.isVisible('.verdict-undo:has-text("Cleared as Expected")'),
    'an interruptible undo affordance appears, naming the verdict just applied',
  )

  // The verdict is already on the server well before any undo window
  // would lapse -- it was posted on the press itself, not deferred.
  const expectedPosted = await waitForApiVerdict(EXPECTED_IP, (f) => f.cleared && f.verdict === 'expected')
  check(
    expectedPosted.ok,
    `Expected reaches the server at once, not deferred behind the undo window (got: ${JSON.stringify(expectedPosted.flag)})`,
  )
  check(
    !!expectedPosted.flag?.verdictBy && !!expectedPosted.flag?.verdictAt,
    'verdictBy/verdictAt are both recorded',
  )

  // Undo: now a real DELETE that reverses an already-recorded verdict,
  // not a cancelled timer -- the server must actually hear about this
  // too, and the flag must come all the way back to open and unjudged.
  await page.click('.verdict-undo-btn')
  await page.waitForTimeout(200)
  check(await cardFor(EXPECTED_IP).isVisible(), 'Undo restores the card to Active')
  check(!(await page.isVisible('.verdict-undo')), 'the undo affordance is gone once used')

  const undone = await waitForApiVerdict(EXPECTED_IP, (f) => !f.cleared && !f.verdict)
  check(
    undone.ok,
    `Undo reaches the server too -- the flag ends up open and unjudged, not just locally reopened (got: ${JSON.stringify(undone.flag)})`,
  )

  // Judge it again, for real this time: no undo. The verdict is already
  // confirmed on the server (checked above, and this press is the same
  // code path) -- what's left to prove is the operator-visible timing:
  // Undo stops being offered on its own after the window, without that
  // being what makes the verdict stick.
  await cardFor(EXPECTED_IP).locator('.verdict-btn-expected').click()
  await page.waitForTimeout(200)
  check(!(await cardFor(EXPECTED_IP).isVisible()), 'the second Expected press clears the card again')
  await page.waitForTimeout(UNDO_WAIT_MS)
  check(!(await page.isVisible('.verdict-undo')), 'the undo affordance disappears on its own after ~5s')

  const expectedFinal = await flagByTarget(EXPECTED_IP)
  check(
    !!expectedFinal?.cleared && expectedFinal?.verdict === 'expected',
    `the flag stays judged after the undo window lapses (got: ${JSON.stringify(expectedFinal)})`,
  )

  await page.reload({ waitUntil: 'networkidle' })
  await openFlags()
  await page.waitForSelector('.card .type', { timeout: 15000 })
  check(
    await clearedCardFor(EXPECTED_IP).locator('.verdict-badge.verdict-expected').isVisible(),
    'the cleared card shows its verdict badge instead of the plain "cleared HH:MM" line',
  )

  // --- Noise: same one-press clear, its own distinct verdict ---

  const noiseCard = cardFor(NOISE_IP)
  await noiseCard.waitFor({ timeout: 15000 })

  await noiseCard.locator('.verdict-btn-noise').click()
  await page.waitForTimeout(200)
  check(!(await cardFor(NOISE_IP).isVisible()), 'Noise also clears on one press')

  const noisePosted = await waitForApiVerdict(NOISE_IP, (f) => f.cleared && f.verdict === 'noise')
  check(
    noisePosted.ok,
    `noise clears and records its own verdict at once, distinct from expected (got: ${JSON.stringify(noisePosted.flag)})`,
  )

  // --- Reload inside the undo window: the case that found the defect
  // this scenario now guards against ---
  //
  // A verdict judged and then immediately reloaded away from must still
  // be on the server afterward -- the opposite of what an earlier,
  // deferred version of this asserted (undo meant "the server never
  // heard about it"; here, *not* undoing means it did, whether or not
  // the page stuck around to find out).

  const reloadCard = cardFor(RELOAD_IP)
  await reloadCard.waitFor({ timeout: 15000 })

  await reloadCard.locator('.verdict-btn-expected').click()
  await page.waitForTimeout(200)
  check(!(await cardFor(RELOAD_IP).isVisible()), 'judging Expected clears the card immediately, same as elsewhere')

  // Reload right away, well inside the undo window -- there is nothing
  // left in flight for the reload to lose, because the request already
  // went out (and, per the check above pattern, already landed) before
  // this line runs.
  await page.reload({ waitUntil: 'networkidle' })

  const reloadResult = await waitForApiVerdict(RELOAD_IP, (f) => f.cleared && f.verdict === 'expected')
  check(
    reloadResult.ok,
    `a verdict judged just before an immediate reload is on the server afterward -- posted at once, nothing for the reload to lose (got: ${JSON.stringify(reloadResult.flag)})`,
  )

  await openFlags()
  await page.waitForSelector('.card .type', { timeout: 15000 })
  check(
    await clearedCardFor(RELOAD_IP).locator('.verdict-badge.verdict-expected').isVisible(),
    'the reloaded UI shows the verdict too',
  )
} else {
  const reasons = raised.filter((r) => !r.ok).map((r) => r.message).join('; ')
  check(true, `skipped -- the verdict row cannot be exercised without all four scan flags (${reasons})`)
}

done()
