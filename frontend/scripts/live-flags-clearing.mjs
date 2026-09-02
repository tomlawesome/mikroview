// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #198's "Clear all", with the click-again confirm as its only
// safeguard -- the last bulk control on the flags tab.
//
// This scenario used to drive three things: a split Clear button, its
// "Permanently clear" menu item, and Clear all. #640 removed the first
// two outright (nothing is dismissed without a judgement any more, and
// an expectation is what "never flag this again" became -- see
// live-flags-expectations.mjs and live-verdicts.mjs for those), so what
// is left here is the one control that survived, driven against the
// ratified table (`tr.frow`) rather than the card grid those steps still
// named.
//
// It leaves nothing behind: every flag it raises is plain-cleared, and
// Store.add revives a cleared flag on the very next matching event, so
// a later run at the same targets just works.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

// 20, not 15: the port_scan detector's threshold IS 15 distinct ports,
// so feeding exactly that leaves no margin -- a single event lost
// anywhere on the path means no flag, and the scenario then fails for a
// reason that has nothing to do with Clear all (#354).
const TARGETS = ['198.51.100.79', '198.51.100.80', '198.51.100.81']
for (const ip of TARGETS) feedPortScan(20, ip)

const { page } = await session()

async function openFlags() {
  await goTo(page, 'Flags')
  await page.waitForSelector('table.ftable', { timeout: 15000 })
}

function rowFor(ip) {
  return page.locator(`tr.frow:has-text("${ip}")`)
}

async function activeCount() {
  return page.locator('section[aria-label^="Active flags"] tr.frow').count()
}

// Server-side first (#354): a locator timeout here cannot say whether
// the flag was never raised or merely had not been rendered yet.
const raised = []
for (const ip of TARGETS) {
  const r = await waitForFlag(page, ip)
  check(r.ok, r.message)
  raised.push(r)
}

if (raised.every((r) => r.ok)) {
  await openFlags()

  // Wait for each flag rather than for "a row exists". Three scans were
  // just fed and the detector raises them independently, so waiting on
  // the first row and then asserting all three are present is a race --
  // it caught the third one missing on a local run.
  for (const ip of TARGETS) {
    await rowFor(ip)
      .waitFor({ timeout: 20000 })
      .catch(() => {})
    check(await rowFor(ip).isVisible(), `flag for ${ip} is active before Clear all`)
  }

  // Clear all is the docket tab row's outlined bubble (rounds 28-29,
  // owner-ratified): one click arms it alarm-red "confirm", a second
  // click clears, and a click anywhere else disarms.
  check(!(await page.isVisible('.docket .bubble.armed')), 'the bubble starts unarmed')
  await page.click('.docket .bubble:has-text("clear all")')
  await page.waitForTimeout(150)
  check(
    await page.isVisible('.docket .bubble.armed:has-text("confirm")'),
    'one click arms it -- alarm-red, and relabelled confirm',
  )

  const armedCount = await activeCount()

  // A click anywhere else disarms it without clearing, so an armed
  // bubble cannot ambush a later stray click.
  await page.click('table.ftable thead')
  await page.waitForTimeout(150)
  check(!(await page.isVisible('.docket .bubble.armed')), 'a click anywhere else disarms it without a second click')
  check((await activeCount()) === armedCount, 'nothing was cleared by an arm that was never confirmed')

  // The real thing: arm, then confirm.
  await page.click('.docket .bubble:has-text("clear all")')
  await page.waitForTimeout(150)
  await page.click('.docket .bubble.armed:has-text("confirm")')
  await page.waitForTimeout(600)

  check((await activeCount()) === 0, 'the second click clears every active flag, including any late rule_spike')

  // A bulk clear records no judgement, so it must record no expectation
  // either -- the invariant #198 stated and #640 restated in its own
  // vocabulary. Read off the flags themselves: a cleared flag with no
  // verdict is exactly what a plain clear leaves behind.
  const afterClear = await page.request
    .get(`${process.env.MV_URL}/api/flags`)
    .then((r) => r.json())
    .then((b) => b.flags ?? [])
  const judged = afterClear.filter((f) => TARGETS.includes(f.target) && f.verdict)
  check(judged.length === 0, `Clear all recorded no verdict on anything it cleared (${JSON.stringify(judged)})`)

  // Reload to confirm the clears persisted server-side, not just in the
  // optimistic client state.
  await page.reload({ waitUntil: 'networkidle' })
  await goTo(page, 'Flags')
  await page.waitForTimeout(500)
  check(
    await page.isVisible('text=Nothing open.'),
    'the cleared state survived a reload -- Clear all reached the server, not just the local optimistic update',
  )

  // And detection really is re-armed: a fresh scan at a cleared target
  // raises it again, which is what makes this scenario repeatable.
  feedPortScan(20, TARGETS[0])
  const back = await waitForFlag(page, TARGETS[0])
  check(back.ok, `a plain clear leaves the pair able to fire again: ${back.message}`)
} else {
  const reasons = raised
    .filter((r) => !r.ok)
    .map((r) => r.message)
    .join('; ')
  check(true, `skipped -- the Clear all flow cannot run without all three scan flags (${reasons})`)
}

done()
