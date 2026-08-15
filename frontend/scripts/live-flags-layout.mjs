// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #199: selectable 1/2/3-column card grid on the Flags page.
//
// Real flags (not synthesized DOM state), a real localStorage round
// trip via reload, and the responsive floor exercised by actually
// resizing the viewport rather than asserting the media query exists.

import { fileURLToPath } from 'url'
import { session, check, done, feedPortScan } from './live-browser.mjs'



// Two independent flags -- enough to see a real grid (1 column would
// show them stacked regardless, so the layout has to actually reflow to
// tell 1 from 2/3 apart).
feedPortScan(20, '198.51.100.90')
feedPortScan(20, '198.51.100.91')

const { page } = await session()

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

await openMenuView('Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })

check(await page.isVisible('.layout-select'), 'the column selector is present')
check(
  await page.evaluate(() => document.querySelector('.layout-option.active')?.textContent?.trim()) === '1',
  'defaults to 1 column with nothing stored',
)

const gridColumnCount = () =>
  page.evaluate(() => {
    const grid = document.querySelector('section[aria-labelledby="active-heading"] .card-grid')
    if (!grid) return null
    return getComputedStyle(grid).gridTemplateColumns.split(' ').length
  })

check((await gridColumnCount()) === 1, 'the grid itself renders 1 column at the default')

// --- Switch to 2, then 3, and check both the control and the actual grid ---

await page.click('.layout-option:has-text("2")')
await page.waitForTimeout(200)
check(await page.evaluate(() => document.querySelector('.layout-option.active')?.textContent?.trim()) === '2', 'picking 2 updates the selected control')
check((await gridColumnCount()) === 2, 'the grid actually renders 2 columns, not just the control changing')
check(await page.isVisible('.card.compact'), 'cards switch to the compact variant at 2 columns')

await page.click('.layout-option:has-text("3")')
await page.waitForTimeout(200)
check((await gridColumnCount()) === 3, 'the grid renders 3 columns')

// The split button must still work at the narrowest density -- this is
// the #198/#199 coordination the issue calls out by name.
await page.click('.split-arrow >> nth=0')
check(await page.isVisible('.split-menu'), 'the split-button dropdown still opens at 3 columns')
await page.keyboard.press('Escape')

// --- Persistence: reload and the choice survives ---

await page.reload({ waitUntil: 'networkidle' })
await openMenuView('Flags')
await page.waitForSelector('.card .type', { timeout: 15000 })
check(
  await page.evaluate(() => document.querySelector('.layout-option.active')?.textContent?.trim()) === '3',
  'the choice persisted across a reload (localStorage)',
)
check((await gridColumnCount()) === 3, 'the grid still renders 3 columns after reload, not just the control label')

// --- Responsive floor: a real resize, not an assumption ---

await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(400)
check(
  (await gridColumnCount()) === 1,
  'below the mobile breakpoint the grid collapses to 1 column regardless of the stored setting',
)
check(
  !(await page.isVisible('.card.compact')),
  'card content reverts to full (non-compact) detail at the floor, not just the grid narrowing with compact styling still active',
)
check(
  await page.evaluate(() => document.querySelector('.layout-option.active')?.textContent?.trim()) === '3',
  'the stored preference itself is untouched by the floor -- still shows 3 selected',
)

await page.setViewportSize({ width: 1280, height: 900 })
await page.waitForTimeout(400)
check((await gridColumnCount()) === 3, 'widening back out re-applies the stored 3-column preference without re-selecting it')

done()
