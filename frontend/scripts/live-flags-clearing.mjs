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

import { execFileSync } from 'child_process'
import { fileURLToPath } from 'url'
import path from 'path'
import { session, check, done } from './live-browser.mjs'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const ACTIVE = 'section[aria-labelledby="active-heading"] .card'

function portscan(n, sourceIp) {
  execFileSync(path.join(REPO, 'scripts/live-env.sh'), ['portscan', String(n), sourceIp], {
    stdio: 'ignore',
    cwd: REPO,
  })
}

function cardFor(page, text) {
  return page.locator(ACTIVE, { hasText: text })
}

async function activeCount(page) {
  return page.locator(ACTIVE).count()
}

portscan(20, '198.51.100.77')
portscan(20, '198.51.100.78')

const { page } = await session()

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

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

portscan(15, '198.51.100.79')
portscan(15, '198.51.100.80')
portscan(15, '198.51.100.81')
await page.reload({ waitUntil: 'networkidle' })
await openMenuView('Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })

for (const ip of ['198.51.100.79', '198.51.100.80', '198.51.100.81']) {
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

done()
