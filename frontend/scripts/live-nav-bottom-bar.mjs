// SPDX-License-Identifier: AGPL-3.0-only
//
// #550: the small-screen counterpart to live-nav-rail.mjs. Three things
// here cannot be proved by a jsdom unit test.
//
// The breakpoint itself: viewportState (lib/viewport.svelte.ts) reads a
// real matchMedia listener, which jsdom does not implement -- no unit
// test in this repo can tell a small viewport from a desktop one (see
// LiveTable's own tests, which stub it permanently false).
//
// The focus trap: BottomBar.svelte.test.ts proves the sheet renders the
// right pages and closes the right way, but "Tab never lands outside the
// sheet" is a claim about the browser's own tab order across real
// rendered elements, not about the component's props.
//
// The browser Back button: closing the sheet on Back is the one behaviour
// in lib/focusTrap.ts's neighbour (BottomBar's own history.pushState/
// popstate handling) that only a real session-history stack can exercise
// -- jsdom's back()/forward() are documented no-ops, which is why
// BottomBar.svelte.test.ts stubs them out rather than asserting on them.

import { session, feedSyslog, feedPortScan, check, waitForFlag, responsive, done } from './live-browser.mjs'

feedSyslog(60, 'nav-bottom-bar')
const { page, consoleErrors } = await session({ waitForEvents: 30 })

// --- Resize down to a small viewport --------------------------------------
// viewportState's matchMedia listener is live, so no reload is needed --
// unlike railPref's density default, which live-nav-badge.mjs's comment
// explains is worked out once at module load.
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForSelector('.bottom-bar', { timeout: 5000 })

// --- The rail is gone; the bar is the whole of navigation -----------------
check((await page.$$('.rail')).length === 0, 'the pointer-width rail does not render at a small viewport')
check((await page.$$('.handle')).length === 0, 'the docked handle does not render either -- the bar replaces both')

const groupNames = await page.$$eval('.bottom-bar .group-btn .label', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(groupNames) === JSON.stringify(['Live', 'Investigate', 'Detect', 'Expect', 'Admin']),
  `bottom bar carries the five groups in the ratified order -- got ${JSON.stringify(groupNames)}`,
)

// --- Dock and density are pointer-width-only, per the record --------------
check((await page.$$('.state-btn')).length === 0, 'no dock/density control renders at small width')
check(
  (await page.$$('[aria-label^="Dock the navigation"], [aria-label^="Show icons"]')).length === 0,
  'no dock/density affordance under any label either, anywhere in the document',
)

// --- Single-page group: straight to the page, no sheet --------------------
// Expect holds only Watchlist -- tapping it must land there directly.
await page.click('.bottom-bar .group-btn .label:text-is("Expect")')
await page.waitForFunction(
  () => document.querySelector('.bottom-bar .group-btn[aria-current="page"] .label')?.textContent.trim() === 'Expect',
  null,
  { timeout: 5000 },
)
check((await page.$$('[role="dialog"]')).length === 0, 'a single-page group navigates directly -- no sheet raised')

// --- Multi-page group: raises the half-sheet -------------------------------
await page.click('.bottom-bar .group-btn .label:text-is("Investigate")')
await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
const sheetItems = await page.$$eval('.sheet .sheet-item .label', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(sheetItems) === JSON.stringify(['Metrics', 'Audit log']),
  `the half-sheet lists Investigate's pages -- got ${JSON.stringify(sheetItems)}`,
)

// --- Focus trap -------------------------------------------------------------
const startsInside = await page.evaluate(() => document.activeElement?.closest('.sheet') != null)
check(startsInside, 'opening the sheet moves focus inside it')

const focusableCount = await page.$$eval('.sheet a[href], .sheet button', (els) => els.length)
for (let i = 0; i < focusableCount + 2; i++) await page.keyboard.press('Tab')
check(
  await page.evaluate(() => document.activeElement?.closest('.sheet') != null),
  'Tab cycles within the sheet rather than escaping it, however many times it is pressed',
)

for (let i = 0; i < focusableCount + 2; i++) await page.keyboard.press('Shift+Tab')
check(
  await page.evaluate(() => document.activeElement?.closest('.sheet') != null),
  'Shift+Tab also cycles within the sheet rather than escaping it',
)

// --- Esc closes it -----------------------------------------------------------
await page.keyboard.press('Escape')
await page.waitForFunction(() => document.querySelector('[role="dialog"]') === null, null, { timeout: 5000 })
check(true, 'Esc closes the half-sheet')
check(await page.isVisible('.bottom-bar'), 'closing via Esc leaves the bar itself in place')

// --- The browser Back button also closes it -----------------------------
await page.click('.bottom-bar .group-btn .label:text-is("Investigate")')
await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
await page.goBack()
await page.waitForFunction(() => document.querySelector('[role="dialog"]') === null, null, { timeout: 5000 })
check(true, 'the browser back button also closes the half-sheet')
check(await page.isVisible('.bottom-bar'), 'closing via back leaves the app in place, not a real navigation away')

// --- Selecting a page in the sheet navigates and closes it -----------------
await page.click('.bottom-bar .group-btn .label:text-is("Detect")')
await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
await page.click('.sheet .sheet-item .label:text-is("Flags")')
await page.waitForFunction(() => document.querySelector('[role="dialog"]') === null, null, { timeout: 5000 })
await page.waitForSelector('.flags', { timeout: 5000 })
const currentGroup = await page
  .$eval('.bottom-bar .group-btn.current .label', (e) => e.textContent.trim())
  .catch(() => null)
check(currentGroup === 'Detect', `selecting Flags moves the bar's current-group marker to Detect -- got ${currentGroup}`)

// --- The flag badge still shows -------------------------------------------
const SCANNER = '198.51.100.66'
feedPortScan(20, SCANNER)
const raised = await waitForFlag(page, SCANNER, { timeoutMs: 30000 })
check(raised.ok, `${raised.message} (a miss here is usually #450's known race, not this badge)`)

if (raised.ok) {
  await page.waitForFunction(
    () => (document.querySelector('.bottom-bar .group-btn .count')?.textContent ?? '').trim().length > 0,
    null,
    { timeout: 15000 },
  )
  const badgeText = await page.$eval('.bottom-bar .group-btn .count', (e) => e.textContent.trim())
  const spoken = await page.getAttribute('.bottom-bar .group-btn:has(.count)', 'aria-label')
  check(Number(badgeText) > 0, `the bar shows the open-flag count -- got "${badgeText}"`)
  check(
    spoken === `Detect — ${badgeText} open`,
    `the badge is spoken in words on the group button, per the record -- got ${JSON.stringify(spoken)}`,
  )
}

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)
done()
