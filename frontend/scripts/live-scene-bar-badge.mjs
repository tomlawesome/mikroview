// SPDX-License-Identifier: AGPL-3.0-only
//
// #546's one alarm count, on #616's chrome: the open-flag badge lives on
// every scene bar now the rail is retired. What needs a real browser
// rather than a unit test:
//
// - The count has to agree with the server, not with a fixture. A unit
//   test asserting the badge renders `flagsState.activeCount` passes
//   whatever that number is, including wrong -- the regression worth
//   catching is the badge and /api/flags disagreeing.
// - Zero renders as no badge at all, not a "0": the badge is conditional
//   DOM, and only a real server whose flag count actually moves can
//   drive it through both states.
// - Clicking the badge is a deep link into the deck -- it has to roll
//   the Flags card to centre from wherever the operator is, which is
//   App/Deck wiring no component test sees.
//
// Everything here asserts badge-agrees-with-server rather than a literal
// number: the feed raises whatever the detectors decide it raises.

import { session, feedSyslog, feedPortScan, check, waitForFlag, done } from './live-browser.mjs'

feedSyslog(40, 'scene-badge')
const { page, consoleErrors } = await session({ waitForEvents: 20 })

// The active card's own scene bar -- the deck also mounts the
// neighbouring cards, each with a bar of its own.
const BADGE = '.card[aria-hidden="false"] .scene-bar .flag-badge'

/**
 * Polls the badge and /api/flags together until they agree, so the feed
 * still landing mid-check cannot make a correct badge look wrong. Returns
 * the agreed count, or null if they never converged.
 */
async function settledCount(timeoutMs = 15000) {
  const deadline = Date.now() + timeoutMs
  let last = null
  while (Date.now() < deadline) {
    last = await page.evaluate(async (sel) => {
      const res = await fetch('/api/flags', { cache: 'no-store' })
      if (!res.ok) return { open: -1, badge: null }
      const body = await res.json()
      const list = Array.isArray(body) ? body : (body.flags ?? [])
      const open = list.filter((f) => !f.cleared).length
      const el = document.querySelector(sel)
      return { open, badge: el ? el.textContent.trim() : null }
    }, BADGE)
    // No badge is the correct rendering of zero: the bar carries the
    // count only when it has something to say -- a permanent "0" is the
    // failure, not the goal.
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
    // One badge on the visible bar, not one per signal.
    check(
      (await page.$$(BADGE)).length === 1,
      'exactly one flag badge on the active scene bar',
    )

    // It travels with the chrome: the neighbouring cards' bars carry the
    // same count, so no scene is blind to the alarm.
    const metricsBadge = await page
      .$eval('.card[data-view="metrics"] .scene-bar .flag-badge', (el) => el.textContent.trim())
      .catch(() => null)
    check(
      metricsBadge === String(open),
      `the Metrics card's bar carries the same count -- got ${JSON.stringify(metricsBadge)}`,
    )

    // --- Clicking the badge lands on the Flags card ----------------------
    await page.click(BADGE)
    await page.waitForFunction(
      () => {
        const deck = document.querySelector('.deck')
        const el = deck?.querySelector('.card[data-view="flags"]')
        if (!el) return false
        return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
      },
      null,
      { timeout: 10000 },
    )
    check(true, 'clicking the badge rolls the Flags card to centre')
    check(
      (await page.$eval('.roll-rail .rail-name.on', (el) => el.textContent.trim())) === 'Flags',
      'and the roll rail agrees Flags is where we are',
    )
    await page.waitForSelector('.flags-page', { timeout: 10000 })
    check(true, 'with the Flags scene actually mounted under it')
  }
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
