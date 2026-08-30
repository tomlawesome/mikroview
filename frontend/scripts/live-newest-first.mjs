// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #363 item 1: newest event at the top of the live view, autoscroll
// follows it there, and -- the hard part -- a scrolled-back reader's rows
// must not move under them once Autoscroll is off.
//
// live-autoscroll.mjs already proves the freeze holds (no new rows
// appear, count unchanged, survives a view switch). This scenario proves
// the two things #363 actually changed and that one didn't cover:
// display order, and that the freeze holds under *real* scroll pixels,
// not just row presence/absence.
//
// jsdom can't be trusted for the pixel half at all: LiveTable.svelte.test.ts
// pins the array order (newest first in the DOM), but jsdom reports
// scrollHeight/clientHeight as 0 regardless of what the component does,
// so a "scrollTop stayed at 0" assertion there would pass whether or not
// the code ever set it -- the exact vacuous-test shape this project has
// been bitten by before (a scrolled-to-bottom check that was trivially
// true because jsdom clips overflow to clientHeight). This scenario
// checks scrollHeight against window.innerHeight first, specifically to
// rule that out here.

import { session, feedSyslog, check, done } from './live-browser.mjs'

// Comfortably past MAX_RENDERED_ROWS (800), same reasoning as
// live-autoscroll.mjs: below that threshold nothing ever gets evicted
// regardless of order, so the interesting behaviour (a long, genuinely
// scrollable table) doesn't exist yet.
feedSyslog(450, 'order-batch-a')
const { page } = await session({ waitForEvents: 400 })
feedSyslog(450, 'order-batch-b')
await page.waitForFunction(() => document.querySelectorAll('.row').length >= 800, null, { timeout: 30000 })

const bodySel = '.body.scrollbar'

// --- Not vacuous: there really is more content than fits on screen -------
const overflow = await page.evaluate((sel) => {
  const el = document.querySelector(sel)
  return { scrollHeight: el.scrollHeight, clientHeight: el.clientHeight, innerHeight: window.innerHeight }
}, bodySel)
check(
  overflow.scrollHeight > overflow.clientHeight + 50 && overflow.scrollHeight > overflow.innerHeight,
  `the table body genuinely overflows -- scrollHeight ${overflow.scrollHeight} vs clientHeight ${overflow.clientHeight} vs window.innerHeight ${overflow.innerHeight}`,
)

// --- Newest at the top, autoscroll follows it there -----------------------
feedSyslog(20, 'order-newest')
await page.waitForFunction(() => document.querySelector('.row[title*="order-newest"]') !== null, {
  timeout: 5000,
})
await page.waitForTimeout(300)

const firstRowTitle = await page.getAttribute('.grid .row', 'title')
check(
  (firstRowTitle ?? '').includes('order-newest'),
  `the first rendered row is the newest event, not the oldest (title: ${firstRowTitle})`,
)

const scrollTopFollowing = await page.$eval(bodySel, (el) => el.scrollTop)
check(
  scrollTopFollowing === 0,
  `autoscroll holds the view at the top (newest end), not the bottom (scrollTop=${scrollTopFollowing})`,
)

// --- The hard part: a scrolled-back reader's rows don't move ------------
await page.click('.scene-bar button:has-text("Autoscroll")')
await page.waitForTimeout(200)

// Scroll down and away from the top -- the reader is now looking at
// older rows, exactly the scenario the frozen pool exists for.
const scrollTarget = 400
await page.$eval(
  bodySel,
  (el, y) => {
    el.scrollTop = y
  },
  scrollTarget,
)
await page.waitForTimeout(200)

const scrollBefore = await page.$eval(bodySel, (el) => el.scrollTop)
check(scrollBefore > 0, `the reader is genuinely scrolled away from the top (scrollTop=${scrollBefore})`)

// Snapshot exactly what's rendered at several fixed positions, not just
// "a row exists somewhere" -- this is what "does not move under them"
// actually means: the Nth row in the DOM is the same event before and
// after, not merely that the total count is unchanged.
const snapshotBefore = await page.$$eval('.grid .row', (els) => els.slice(0, 10).map((e) => e.getAttribute('title')))

// New events arrive while frozen -- none of them may appear, and nothing
// already on screen may shift.
feedSyslog(60, 'order-after-freeze')
await page.waitForTimeout(1500)

const scrollAfter = await page.$eval(bodySel, (el) => el.scrollTop)
check(
  scrollAfter === scrollBefore,
  `scroll position is pixel-identical after new events arrive while frozen (${scrollBefore} -> ${scrollAfter})`,
)

const snapshotAfter = await page.$$eval('.grid .row', (els) => els.slice(0, 10).map((e) => e.getAttribute('title')))
check(
  JSON.stringify(snapshotAfter) === JSON.stringify(snapshotBefore),
  `the same 10 rows, in the same order, are still there after new events arrived (before: ${JSON.stringify(snapshotBefore)}, after: ${JSON.stringify(snapshotAfter)})`,
)

check(
  (await page.locator('.row[title*="order-after-freeze"]').count()) === 0,
  'none of the events fed while frozen appear anywhere in the table',
)

// --- Releasing the freeze resumes newest-at-top --------------------------
await page.click('.scene-bar button:has-text("Autoscroll")')
await page.waitForTimeout(300)

const scrollReleased = await page.$eval(bodySel, (el) => el.scrollTop)
check(scrollReleased === 0, `turning Autoscroll back on returns to the top (scrollTop=${scrollReleased})`)

const firstRowAfterResume = await page.getAttribute('.grid .row', 'title')
check(
  (firstRowAfterResume ?? '').includes('order-after-freeze'),
  `the events that arrived while frozen are now on top, as the newest (title: ${firstRowAfterResume})`,
)

done()
