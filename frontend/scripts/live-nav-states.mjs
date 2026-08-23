// SPDX-License-Identifier: AGPL-3.0-only
//
// #545: the rail's three persistent states and the handle. What needs a
// real browser rather than a unit test:
//
// - The states are widths and mounted/unmounted DOM, not a store value.
//   Asserting railPref.effective === 'docked' would pass while the rail
//   still occupied 216px, which is the actual regression worth catching.
// - "The handle never writes the preference" is only observable across a
//   real page load: restore, reload, and the rail must come back docked.
//   Nothing short of a reload distinguishes it from a permanent undock.
// - Focus moves between two components that are never mounted at the same
//   time (rail -> handle on dock, handle -> rail on restore). A mocked
//   store hides exactly that wiring.
// - The tooltip is required on focus as well as hover, and a `title`
//   attribute silently satisfies neither in a way a test can see.

import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

feedSyslog(120, 'nav-states')
const { page, consoleErrors } = await session({ waitForEvents: 100 })

// Explicit rather than inherited: the default full/icons split is at
// 1280px and Playwright's default viewport is exactly 1280 wide, so the
// baseline would sit on the knife edge of its own boundary condition.
// Reloaded after resizing, not merely resized: the default is worked out
// once at module load and then never revisited ("never changed by the app
// on its own"), so without the reload this would still be asserting the
// default that Playwright's 1280px viewport produced -- exactly the
// boundary value the rule turns on.
await page.setViewportSize({ width: 1440, height: 900 })
await page.reload()
await page.waitForSelector('.rail .item', { timeout: 10000 })

const railWidth = async () => {
  const box = await page.$eval('.rail', (el) => el.getBoundingClientRect().width).catch(() => null)
  return box === null ? null : Math.round(box)
}
const DENSITY = '.state-btn[aria-label^="Show icons"]'
const DOCK = '.state-btn[aria-label^="Dock the navigation"]'

// --- Default: full, because the viewport is over 1280 --------------------
check((await railWidth()) === 216, `full density by default above 1280px -- got ${await railWidth()}px`)
check(
  await page.$eval('.rail .item .label', (el) => el.getBoundingClientRect().width > 0),
  'labels are visible at full density',
)

// The record asks the control to name its destination, not its current
// state -- "Show icons only" while full, the reverse once switched.
check(
  (await page.getAttribute(DENSITY, 'aria-label')) === 'Show icons only',
  `the density control names where it goes -- got "${await page.getAttribute(DENSITY, 'aria-label')}"`,
)

// --- Full -> icons -------------------------------------------------------
await page.click(DENSITY)
await page.waitForFunction(() => Math.round(document.querySelector('.rail').getBoundingClientRect().width) === 54, null, {
  timeout: 5000,
})
check((await railWidth()) === 54, `the density control switches to 54px -- got ${await railWidth()}px`)
check(
  (await page.getAttribute(DENSITY, 'aria-label')) === 'Show icons and text',
  'and the control now offers the way back',
)
check(
  await page.$eval('.rail .item .label', (el) => el.getBoundingClientRect().width === 0),
  'labels are not rendered at icons density',
)
// "Icons density keeps full labels ... label+count in aria-labels."
check(
  (await page.getAttribute('.rail .item[aria-current="page"]', 'aria-label')) === 'Stream',
  'the current item still carries its label in aria at icons density',
)

// --- The tooltip answers focus, not only hover ---------------------------
// A `title` attribute would look correct in the markup and never appear
// for a keyboard user, which is the failure this checks for.
await page.focus('.rail .item[aria-current="page"]')
const tipText = await page
  .waitForSelector('.tip', { timeout: 2000 })
  .then((el) => el.textContent())
  .catch(() => null)
check(tipText === 'Stream', `focusing a rail item shows its tooltip, not hover-only -- got ${JSON.stringify(tipText)}`)

// It must also not be clipped by the 54px rail it hangs off, which is
// what an absolutely-positioned tooltip inside a scrolling rail would be.
const tipClear = await page.$eval('.tip', (el) => el.getBoundingClientRect().left >= 54)
check(tipClear, 'the tooltip clears the rail rather than being clipped inside it')

// --- Dock ----------------------------------------------------------------
await page.click(DOCK)
await page.waitForSelector('.handle', { timeout: 5000 })
check((await page.$$('.rail')).length === 0, 'docking unmounts the rail rather than leaving a 0px one in the tab order')
check((await page.$$('.handle')).length === 1, 'the handle takes its place')

// "Docking returns focus to the handle."
check(
  await page.evaluate(() => document.activeElement?.classList.contains('handle')),
  'docking moves focus to the handle',
)

// The handle is centred on the viewport, always -- not on the document,
// and not on the content column's height.
const centred = await page.evaluate(() => {
  const r = document.querySelector('.handle').getBoundingClientRect()
  return Math.abs(r.top + r.height / 2 - window.innerHeight / 2) < 2
})
check(centred, 'the handle is vertically centred on the viewport')

check(
  (await page.textContent('[role="status"].sr-only'))?.includes('docked'),
  'docking is announced',
)

// --- Restore, and the density it comes back at ---------------------------
await page.click('.handle')
await page.waitForSelector('.rail', { timeout: 5000 })
check((await railWidth()) === 54, `restoring returns the same density it was docked from -- got ${await railWidth()}px`)
check((await page.$$('.handle')).length === 0, 'and the handle goes away with it')

// "Restoring lands focus on the current page."
check(
  await page.evaluate(() => document.activeElement?.getAttribute('aria-current') === 'page'),
  'restoring lands focus on the current page item',
)

// --- The handle never writes the preference ------------------------------
// The whole point of the distinction: restoring is not a state change, so
// a reload must come back docked. If this fails, the handle has quietly
// become a permanent undock and the footer is no longer the only place a
// state is selected.
await page.reload()
await page.waitForSelector('.handle', { timeout: 10000 })
check((await page.$$('.rail')).length === 0, 'a reload after restoring comes back docked -- the handle never wrote the preference')

// --- Keyboard-only: skip-link, then the handle ---------------------------
// The record puts the handle first in tab order after the skip-link. A
// docked rail is unreachable otherwise, so this is the one path that has
// to work without a pointer.
await page.keyboard.press('Tab')
check(
  await page.evaluate(() => document.activeElement?.className?.includes('skip-link')),
  'first Tab still lands on the skip-link',
)
await page.keyboard.press('Tab')
check(
  await page.evaluate(() => document.activeElement?.classList.contains('handle')),
  'the handle is next after it',
)
await page.keyboard.press('Enter')
await page.waitForSelector('.rail', { timeout: 5000 })
check((await railWidth()) === 54, 'Enter on the handle restores the rail')

// --- A selected state does persist ---------------------------------------
// The mirror of the check above: the footer writes, so this survives.
await page.click(DENSITY)
await page.waitForFunction(() => Math.round(document.querySelector('.rail').getBoundingClientRect().width) === 216, null, {
  timeout: 5000,
})
await page.reload()
await page.waitForSelector('.rail', { timeout: 10000 })
check((await railWidth()) === 216, `choosing a density in the footer persists across a reload -- got ${await railWidth()}px`)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
