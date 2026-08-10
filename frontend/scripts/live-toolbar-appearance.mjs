// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #137: Appearance is a standalone toolbar control again, and
// Export moved into the menu on both breakpoints.
//
// Driven in a real browser at both widths because that is the whole of
// what this change is -- a UI reorganisation has no unit-testable truth
// beyond "the control renders where claimed and does what it did".

import { session, feedSyslog, check, done } from './live-browser.mjs'

// Events in the buffer, so Export has something to be enabled for.
feedSyslog(100)
const { page } = await session({ waitForEvents: 50 })

// --- Desktop ---------------------------------------------------------

check(await page.isVisible('.theme-menu .trigger'), 'the Theme control is standalone in the toolbar')

// One click to open, one to apply -- the regression #137 records was
// this taking two clicks through the menu.
await page.click('.theme-menu .trigger')
await page.click('.theme-menu button:has-text("Nebula")')
const colorway = await page.getAttribute('html', 'data-colorway')
check(colorway === 'nebula', `picking a colorway applies it (data-colorway=${colorway})`)

await page.click('.theme-menu .trigger')
await page.click('.theme-menu button:has-text("Light")')
const theme = await page.getAttribute('html', 'data-theme')
check(theme === 'light', `picking a mode applies it (data-theme=${theme})`)

// Export is out of the toolbar...
check(
  !(await page.isVisible('header.toolbar > .controls > button:has-text("Export")')),
  'no inline Export button on the desktop toolbar',
)

// ...and in the menu, where it must actually still export.
await page.click('.nav-menu .trigger')
check(
  await page.isVisible('.nav-menu button:has-text("Export to CSV")'),
  'Export to CSV is in the menu on desktop',
)
const [download] = await Promise.all([
  page.waitForEvent('download', { timeout: 10000 }),
  page.click('.nav-menu button:has-text("Export to CSV")'),
])
check(
  (download.suggestedFilename() ?? '').endsWith('.csv'),
  `the menu entry downloads a CSV (${download.suggestedFilename()})`,
)

// --- Mobile ----------------------------------------------------------

await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(400)

check(await page.isVisible('.theme-menu .trigger'), 'the Theme control survives the mobile breakpoint')

await page.click('.theme-menu .trigger')
check(
  await page.isVisible('.theme-menu .menu.mobile-sheet'),
  'at phone width the Theme menu is a bottom sheet, not a right-anchored dropdown',
)
await page.click('.theme-menu .mobile-sheet button:has-text("Signal")')
const mobileColorway = await page.getAttribute('html', 'data-colorway')
check(mobileColorway === 'signal', `a colorway applies from the sheet (data-colorway=${mobileColorway})`)

await page.click('.nav-menu .trigger')
check(
  await page.isVisible('.nav-menu button:has-text("Export to CSV")'),
  'Export to CSV is still in the menu on mobile',
)

done()
