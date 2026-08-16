// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #198: "Clear all" with click-again confirm, and a split Clear
// button with "Permanently clear".
//
// Two real port_scan flags from two different sources -- one drives the
// split button and its dropdown, the other survives to be swept up by
// Clear all -- so both actions run against real server state rather than
// a mocked click handler.
//
// Assertions are scoped to specific cards (by their target IP) and to
// count *deltas*, not fixed totals: the synthetic burst this needs to
// trigger a real port_scan also trips a real rule_spike on the shared
// log-prefix's own hit rate (confirmed by hand -- every request logged
// as `scan-src`, and ~40 of them inside its window is exactly what
// rule_spike watches for). That is the detector working correctly on
// synthetic traffic shaped like a real one, not a defect to work around
// by asserting an exact card count that real detector timing can't
// actually promise.

import { fileURLToPath } from 'url'
import { session, check, done, feedPortScan, waitForFlag } from './live-browser.mjs'

const ACTIVE = 'section[aria-labelledby="active-heading"] .card'


function cardFor(page, text) {
  return page.locator(ACTIVE, { hasText: text })
}

async function activeCount(page) {
  return page.locator(ACTIVE).count()
}

feedPortScan(20, '198.51.100.77')
feedPortScan(20, '198.51.100.78')

const { page } = await session()

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

// Server-side first (#354): a locator timeout here cannot say whether
// the flag was never raised or merely had not been rendered yet.
const firstRaised = []
for (const ip of ['198.51.100.77', '198.51.100.78']) {
  const raised = await waitForFlag(page, ip)
  check(raised.ok, raised.message)
  firstRaised.push(raised)
}

// The split-button flow below needs both cards to actually exist, and
// the Clear all section further down needs the split-button flow to have
// run (it asserts against the one exclusion that flow creates). Running
// either against a flag that never reached the server used to crash the
// scenario on the first Playwright locator timeout instead of reporting
// the real, upstream reason (#450).
if (firstRaised.every((r) => r.ok)) {
  await openMenuView('Flags')
  await page.waitForSelector('.card .type', { timeout: 15000 })

  check(await cardFor(page, '198.51.100.77').isVisible(), 'the first port scan raised its own flag')
  check(await cardFor(page, '198.51.100.78').isVisible(), 'the second port scan raised its own flag')

  // --- Split button: main segment behaves exactly like the old Clear ---

  check(
    !(await page.isVisible('button:has-text("Clear, never flag again")')),
    'the old two-button row is gone',
  )
  check(await page.isVisible('.split-arrow'), 'the split-button arrow segment is present for an admin')

  const before = await activeCount(page)
  await cardFor(page, '198.51.100.77').locator('.split-main').click()
  await page.waitForTimeout(400)
  check(
    (await activeCount(page)) === before - 1 && !(await cardFor(page, '198.51.100.77').isVisible()),
    'clicking the main Clear segment clears just that one flag, same as before',
  )

  // --- Split dropdown: keyboard accessibility, on the other scan's card ---

  const target = cardFor(page, '198.51.100.78')
  await target.locator('.split-arrow').focus()
  check(
    await page.evaluate(() => document.activeElement?.classList.contains('split-arrow')),
    'the arrow segment is reachable by keyboard focus',
  )
  await page.keyboard.press('Enter')
  await page.waitForTimeout(200)
  check(await page.isVisible('.split-menu'), 'Enter on the focused arrow segment opens the dropdown')
  check(
    await page.isVisible('.split-menu-item:has-text("Permanently clear")'),
    'the dropdown item is renamed to "Permanently clear"',
  )

  await page.keyboard.press('Escape')
  await page.waitForTimeout(200)
  check(!(await page.isVisible('.split-menu')), 'Escape closes the dropdown')

  // Outside click also closes it.
  await target.locator('.split-arrow').click()
  await page.waitForTimeout(200)
  check(await page.isVisible('.split-menu'), 'the dropdown reopens on a click')
  await page.click('h2:has-text("Active")')
  await page.waitForTimeout(200)
  check(!(await page.isVisible('.split-menu')), 'a click outside the split button closes the dropdown')

  // Permanently clear it via the menu item, keyboard-driven end to end:
  // focus the arrow, open with Enter, reach the item with Tab, activate
  // with Enter.
  const beforePermanent = await activeCount(page)
  await target.locator('.split-arrow').focus()
  await page.keyboard.press('Enter')
  await page.waitForTimeout(200)
  await page.keyboard.press('Tab')
  check(
    await page.evaluate(() => document.activeElement?.classList.contains('split-menu-item')),
    'Tab from the open arrow segment reaches the menu item',
  )
  await page.keyboard.press('Enter')
  await page.waitForTimeout(500)

  check(
    (await activeCount(page)) === beforePermanent - 1 && !(await target.isVisible()),
    'the permanent-clear menu item still clears the flag, keyboard-driven',
  )
  check(await page.isVisible('text=Permanently-excluded'), 'the exclusions pointer appears once an exclusion exists')

  // --- Clear all: click-again confirm ---

  // 20, not 15: the port_scan detector's threshold IS 15 distinct ports,
  // so feeding exactly that left no margin -- a single event lost anywhere
  // on the path means no flag, and the scenario fails for a reason that
  // has nothing to do with Clear all (#354).
  feedPortScan(20, '198.51.100.79')
  feedPortScan(20, '198.51.100.80')
  feedPortScan(20, '198.51.100.81')

  const secondRaised = []
  for (const ip of ['198.51.100.79', '198.51.100.80', '198.51.100.81']) {
    const raised = await waitForFlag(page, ip)
    check(raised.ok, raised.message)
    secondRaised.push(raised)
  }

  // Clear all needs all three cards actually present -- same reasoning
  // as the outer guard above (#450).
  if (secondRaised.every((r) => r.ok)) {
    await page.reload({ waitUntil: 'networkidle' })
    await openMenuView('Flags')
    await page.waitForSelector('.card .type', { timeout: 15000 })

    // Wait for each flag rather than for "a card exists". Three scans were
    // just fed and the detector raises them independently, so waiting on the
    // first card to appear and then asserting all three are present is a
    // race -- it caught the third one missing on a local run. Waiting for
    // each makes the assertion about Clear all, which is what this section
    // is for.
    for (const ip of ['198.51.100.79', '198.51.100.80', '198.51.100.81']) {
      await cardFor(page, ip)
        .waitFor({ timeout: 20000 })
        .catch(() => {})
      check(await cardFor(page, ip).isVisible(), `flag for ${ip} is active before Clear all`)
    }

    check(!(await page.isVisible('button:has-text("Confirm")')), 'Clear all starts unarmed')
    await page.click('button:has-text("Clear all")')
    await page.waitForTimeout(150)
    check(await page.isVisible('button.clear-all.armed:has-text("Confirm")'), 'one click arms it -- red, and relabelled Confirm')

    const armedCount = await activeCount(page)

    // Moving the pointer away disarms it without a second click.
    await page.hover('h2:has-text("Active")')
    await page.waitForTimeout(300)
    check(!(await page.isVisible('button.clear-all.armed')), 'the pointer leaving the button disarms it')

    // Re-arm and let the timeout do the disarming.
    await page.click('button:has-text("Clear all")')
    check(await page.isVisible('button.clear-all.armed'), 're-armed for the timeout check')
    await page.waitForTimeout(4500)
    check(!(await page.isVisible('button.clear-all.armed')), 'it disarms itself after the ~4s timeout with no second click')
    check((await activeCount(page)) === armedCount, 'nothing was cleared by an arm that was never confirmed')

    // The real thing: click, then click again while still hovering.
    await page.click('button:has-text("Clear all")')
    await page.click('button:has-text("Confirm")')
    await page.waitForTimeout(600)

    check((await activeCount(page)) === 0, 'the second click actually clears every active flag, including the extra rule_spike')

    // Regular clears only -- Clear all must never create an exclusion.
    const excludedResp = await page.request.get(`${process.env.MV_URL}/api/flags/exclusions`)
    const excludedBody = await excludedResp.json()
    check(
      (excludedBody.exclusions ?? []).length === 1,
      `Clear all created no new exclusions -- still just the one from the split-button test (${(excludedBody.exclusions ?? []).length})`,
    )

    // Reload to confirm the clears persisted server-side, not just in the
    // optimistic client state.
    await page.reload({ waitUntil: 'networkidle' })
    await openMenuView('Flags')
    await page.waitForTimeout(500)
    check(
      await page.isVisible('text=Nothing flagged right now'),
      'the cleared state survived a reload -- Clear all reached the server, not just the local optimistic update',
    )
  } else {
    const reasons = secondRaised.filter((r) => !r.ok).map((r) => r.message).join('; ')
    check(true, `skipped -- the Clear all flow cannot run without all three scan flags (${reasons})`)
  }
} else {
  const reasons = firstRaised.filter((r) => !r.ok).map((r) => r.message).join('; ')
  check(true, `skipped -- the split-button and Clear all flows cannot run without both scan flags (${reasons})`)
}

done()
