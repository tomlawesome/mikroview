// SPDX-License-Identifier: AGPL-3.0-only
//
// #546: the one count on the rail. What needs a real browser rather than a
// unit test:
//
// - The count has to agree with the server, not with a fixture. A unit
//   test asserting the badge renders `flagsState.activeCount` passes
//   whatever that number is, including wrong -- the regression worth
//   catching is the badge and /api/flags disagreeing.
// - At icons density the badge is absolutely positioned on the icon's
//   corner. Whether it is still inside the 54px rail is a layout fact, and
//   a rail that scrolls on one axis clips the other regardless of what
//   overflow-x says -- the same trap that clipped #545's tooltip.
// - "Docking navigation never docks the alarm": the count moves to a
//   different component, mounted only once the rail is gone. Nothing short
//   of really docking exercises that handover.
//
// Everything here asserts badge-agrees-with-server rather than a literal
// number. The feed raises whatever the detectors decide it raises, so a
// hardcoded expectation would be asserting the detectors' behaviour by
// accident, and would go red for a reason that has nothing to do with the
// badge.

import { session, feedSyslog, feedPortScan, check, waitForFlag, done } from './live-browser.mjs'

feedSyslog(40, 'nav-badge')
const { page, consoleErrors } = await session({ waitForEvents: 20 })

// Above the 1280px default split, and reloaded after resizing, for the
// reason live-nav-states.mjs gives: the density default is worked out once
// at module load, and Playwright's own viewport sits exactly on the
// boundary the rule turns on.
await page.setViewportSize({ width: 1440, height: 900 })
await page.reload()
await page.waitForSelector('.rail .item', { timeout: 10000 })

// The label span directly contains the word, which is what :text-is()
// requires -- it does not match an ancestor that merely wraps the text.
const FLAGS_ITEM = '.rail .item:has(.label:text-is("Flags"))'

/**
 * Polls the badge and /api/flags together until they agree, so the feed
 * still landing mid-check cannot make a correct badge look wrong. Returns
 * the agreed count, or null if they never converged.
 */
async function settledCount(timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  let last = null
  while (Date.now() < deadline) {
    last = await page.evaluate(async () => {
      const res = await fetch('/api/flags', { cache: 'no-store' })
      if (!res.ok) return { open: -1, badge: null }
      const body = await res.json()
      const list = Array.isArray(body) ? body : (body.flags ?? [])
      const open = list.filter((f) => !f.cleared).length
      const el = document.querySelector('.rail .count')
      return { open, badge: el ? el.textContent.trim() : null }
    })
    // No badge is the correct rendering of zero: the record puts one
    // alarm-filled count in the chrome, and only when it has something to
    // say -- a permanent "0" on Flags is the failure, not the goal.
    const shown = last.badge === null ? 0 : Number(last.badge)
    if (last.open >= 0 && shown === last.open) return last.open
    await new Promise((r) => setTimeout(r, 250))
  }
  check(false, `badge never agreed with the server -- last saw ${JSON.stringify(last)}`)
  return null
}

// --- Before anything is raised -------------------------------------------
const initial = await settledCount()
check(initial !== null, `the badge agrees with the server before any scan (${initial} open)`)

// --- Raise one, and it has to follow -------------------------------------
const SCANNER = '198.51.100.77'
feedPortScan(20, SCANNER)
const raised = await waitForFlag(page, SCANNER, { timeoutMs: 30000 })
check(raised.ok, `${raised.message} (a miss here is usually #450's known race, not this badge)`)

if (raised.ok) {
  const open = await settledCount()
  check(open !== null && open > 0, `the badge follows the server once a flag is raised (${open} open)`)

  if (open) {
    // "label+count in aria-labels", worded as the ratified mockup words it.
    const spoken = await page.getAttribute(FLAGS_ITEM, 'aria-label')
    check(
      spoken === `Flags — ${open} open`,
      `the row speaks its count rather than leaving a bare number -- got ${JSON.stringify(spoken)}`,
    )

    // The badge itself must not then be read a second time as a naked digit.
    check(
      (await page.getAttribute(`${FLAGS_ITEM} .count`, 'aria-hidden')) === 'true',
      'the badge is hidden from screen readers, since the label already carries the count',
    )

    // Flags is the only alarm-filled count in the chrome.
    check(
      (await page.$$('.rail .count')).length === 1,
      'exactly one count on the rail, and it is Flags',
    )

    // --- Icons density: still there, still inside the rail ---------------
    await page.click('.state-btn[aria-label^="Show icons"]')
    await page.waitForFunction(
      () => Math.round(document.querySelector('.rail').getBoundingClientRect().width) === 54,
      null,
      { timeout: 5000 },
    )
    const iconsCount = await page.$eval('.rail .count', (el) => el.textContent.trim()).catch(() => null)
    check(iconsCount === String(open), `the count survives the switch to icons density -- got ${iconsCount}`)

    const withinRail = await page.$eval('.rail .count', (el) => {
      const r = el.getBoundingClientRect()
      return r.width > 0 && r.right <= 55
    })
    check(withinRail, 'the badge is drawn inside the 54px rail rather than clipped or overflowing it')

    // --- Docked: the alarm follows the handle ----------------------------
    await page.click('.state-btn[aria-label^="Dock the navigation"]')
    await page.waitForSelector('.handle', { timeout: 5000 })
    const handleCount = await page.$eval('.handle .count', (el) => el.textContent.trim()).catch(() => null)
    check(handleCount === String(open), `docking navigation does not dock the alarm -- got ${handleCount}`)

    const handleLabel = await page.getAttribute('.handle', 'aria-label')
    check(
      handleLabel === `Restore navigation — ${open} open flags`,
      `the handle says what it holds as well as what it does -- got ${JSON.stringify(handleLabel)}`,
    )
  }
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
