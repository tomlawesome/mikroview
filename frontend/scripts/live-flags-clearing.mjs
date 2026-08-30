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
//
// #539: the "Permanently clear" step below creates a real, server-side
// exclusion for 198.51.100.78. That used to be the one piece of state
// this scenario left behind on exit. Every *other* target it touches
// only ever gets plain-cleared, and a plain clear is cheap to be
// independent of: Store.add (internal/flags/store.go) revives a cleared
// flag on the very next matching event, so feeding a fresh scan at a
// previously-cleared target just works, on this run or any later one.
// An exclusion does not revive -- Store.add's excluded check is a
// silent, permanent no-op by design (the whole point of "never flag
// this again") -- so a second run against the same server fed a fresh
// scan at 198.51.100.78, waited the full waitForFlag timeout for a flag
// that could now never arrive, and failed with a message that read as
// detector-timing flakiness while the server's own flag list showed
// exactly what happened: the target present and already `(cleared)`.
// (That shape is the tell for this bug specifically -- #450 is a
// different failure, where the awaited target is absent from the list
// entirely because the detector never raised it at all.)
//
// The fix is not a new address per run -- the split-button/Clear-all
// flows below are about *these specific* interactions, not about
// picking IPs the server has never seen, and burning a fresh exclusion
// on every single run would leave real permanent state accumulating on
// a shared server forever. Instead this scenario resets the one piece
// of state it is responsible for, at both ends: on the way in, in case
// an earlier run of this same scenario left the exclusion in place
// (crashed before its own cleanup, or predates this fix); and on the
// way out, once the assertions that need the exclusion to exist have
// run. Either half alone is enough to make repeat runs pass; both
// together also mean the exclusion is not just sitting there,
// unnoticed, on the assumption that a future run will clear it.
// resetExclusion is a no-op when there is nothing to remove --
// DELETE on an unknown exclusion ID is documented as such
// (internal/api/flags.go's handleExclusionRemove) -- so calling it
// unconditionally, whether or not this run actually created one, is
// safe.
//
// The startup reset runs over its own short-lived API request context
// (Playwright's request.newContext, no browser page attached) rather
// than through session()'s page, and deliberately before feedPortScan
// rather than after session(): App.svelte's flagsState.refresh() fires
// once on login and then every STATS_REFRESH_MS (5s) after that, on its
// own timer, unrelated to which view is on screen -- it is not
// re-triggered by navigating into the Flags view. Logging the real
// browser in first and feeding the scans after it (which an earlier
// version of this fix did, to reuse that page for the reset) starts
// that 5s clock before this run's own flags exist, so a fast
// waitForFlag -- which polls the server directly and can return in well
// under a second -- can land back in this scenario before App.svelte's
// own next refresh has caught up, and the Flags view then renders from
// a stale pre-scan snapshot that still shows both targets cleared. That
// produced exactly this bug's symptom (a card that should be active
// reading as cleared) for a different reason than #539 itself, on every
// run after the first. Feeding first and logging the real session in
// afterward, as originally written, keeps that timer's first tick safely
// after this run's events already exist.

import { fileURLToPath } from 'url'
import { request } from 'playwright'
import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

const ACTIVE = 'section[aria-labelledby="active-heading"] .card'
const PERMANENT_EXCLUSION_ID = 'port_scan:198.51.100.78'

/**
 * resetExclusion clears any lingering permanent exclusion for the one
 * target this scenario permanently clears (see #539 note above), through
 * anything with Playwright's request-context shape (a page's own
 * `.request`, or a standalone `request.newContext()`). Safe to call at
 * any time, whether or not an exclusion currently exists.
 */
async function resetExclusion(requester) {
  return requester.delete(`${process.env.MV_URL}/api/flags/exclusions/${PERMANENT_EXCLUSION_ID}`, {
    headers: { 'X-Requested-With': 'mikroview' },
  })
}

function cardFor(page, text) {
  return page.locator(ACTIVE, { hasText: text })
}

async function activeCount(page) {
  return page.locator(ACTIVE).count()
}

// A throwaway login (its own session, no browser page) purely to clear
// a leftover exclusion before anything else touches the server -- see
// the #539 note above for why this has to happen before feedPortScan,
// not after the real session() login below.
{
  const api = await request.newContext({ ignoreHTTPSErrors: true })
  const loginResp = await api.post(`${process.env.MV_URL}/api/auth/login`, {
    data: { username: process.env.MV_USER, password: process.env.MV_PASS },
    headers: { 'X-Requested-With': 'mikroview' },
  })
  const resetResp = loginResp.ok() ? await resetExclusion(api) : loginResp
  check(resetResp.ok(), `startup: any exclusion left over from an earlier run was cleared (status ${resetResp.status()})`)
  await api.dispose()
}

feedPortScan(20, '198.51.100.77')
feedPortScan(20, '198.51.100.78')

const { page } = await session()

async function openMenuView(label) {
  await goTo(page, label)
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
  await target.locator('.split-arrow').focus()
  await page.keyboard.press('Enter')
  await page.waitForTimeout(200)
  await page.keyboard.press('Tab')
  check(
    await page.evaluate(() => document.activeElement?.classList.contains('split-menu-item')),
    'Tab from the open arrow segment reaches the menu item',
  )
  await page.keyboard.press('Enter')

  // The *card* must go, not the global count drop by exactly one: on the
  // shared suite instance the 20-port scans' own late rule_spike flag
  // can land in this same window and hold the count level, which failed
  // this leg for a reason that had nothing to do with the keyboard path.
  // The Exclusions-tab check below still proves the clear was permanent.
  await target.waitFor({ state: 'hidden', timeout: 10000 }).catch(() => {})
  check(
    !(await target.isVisible()),
    'the permanent-clear menu item still clears the flag, keyboard-driven',
  )
  // #547: the standalone Exclusions page (and its "Permanently-excluded"
  // pointer here) is gone -- exclusions are now Flags' own Exclusions
  // tab, carrying a quiet, outlined count instead of a pointer sentence.
  check(
    await page.isVisible('[role="tab"]:has-text("Exclusions") .count'),
    'the Exclusions tab carries a count once an exclusion exists',
  )

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

// Leave the server as this scenario found it, not just as the next run
// of this scenario needs it (#539): the startup reset above is what
// actually guarantees repeatability, but there is no reason to leave a
// real permanent exclusion sitting on a shared server for however long
// it is before this scenario runs again. Unconditional and safe either
// way -- see resetExclusion's doc comment.
await resetExclusion(page.request)

done()
