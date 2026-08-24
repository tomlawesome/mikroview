// SPDX-License-Identifier: AGPL-3.0-only
//
// #488: metrics ships three views of one data set -- Seismograph
// (default), Register, Table -- chosen in the page header and persisted
// as a per-user preference, with the cursor's minute surviving every
// switch (docs/design/screens/metrics/DESIGN.md).
//
// Three claims here cannot be proved anywhere but a real browser:
//
//  - The drawn views measure their own box and size their SVG in real
//    CSS pixels. jsdom reports every box as zero, so a unit test renders
//    a drum at its fallback width and would pass while the real page
//    drew nothing.
//  - "Persisted and applied before first paint" is a claim about a
//    reload: the preference has to be in localStorage *and* read
//    synchronously at module load. A store test proves the write; only a
//    reload proves the read beats the paint.
//  - "The cursor's selected minute survives every view switch" is a
//    claim across three components mounting and unmounting, which is
//    exactly the wiring a mocked store hides.
//
// Note on waiting: page.isVisible() returns immediately, so a check
// written with it races the render and passes or fails on timing. Every
// wait below goes through a locator's waitFor().

import { session, feedSyslog, feedPortScan, waitForFlag, check, responsive, done } from './live-browser.mjs'

// Enough traffic for several minutes of the hour to carry a rate, and a
// scan so at least one flag type has an episode to draw a tick for.
feedSyslog(240, 'metrics-views')
feedPortScan(20, '203.0.113.44')

const { page, consoleErrors } = await session({ waitForEvents: 100 })
await waitForFlag(page, '203.0.113.44')

const VIEW_BUTTON = (name) => `.views button:text-is("${name}")`
const SEISMOGRAPH = '.drum svg'
const REGISTER = '.register .paper svg'
const TABLE = '.table-view table'

await page.click('.rail .item .label:text-is("Metrics")')

// --- The default view, actually drawn -----------------------------------
await page.locator(SEISMOGRAPH).waitFor({ state: 'visible', timeout: 10000 })
check(true, 'Metrics opens on the seismograph and draws it')

const buttons = await page.$$eval('.views button', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(buttons) === JSON.stringify(['Seismograph', 'Register', 'Table']),
  `all three views are offered in the page header -- got ${JSON.stringify(buttons)}`,
)

const pressed = await page.$eval('.views button[aria-pressed="true"]', (e) => e.textContent.trim())
check(pressed === 'Seismograph', `the seismograph is the default -- got "${pressed}"`)

// The SVG is sized from the measured box, not stretched from a viewBox:
// a width attribute that matches the element's own client width is what
// makes one unit one CSS pixel, which the record's sharpness clause
// depends on.
const sizing = await page.$eval(SEISMOGRAPH, (svg) => ({
  attr: Number(svg.getAttribute('width')),
  box: Math.round(svg.getBoundingClientRect().width),
  viewBox: svg.getAttribute('viewBox'),
}))
check(sizing.attr > 400, `the drum is sized in real pixels -- width attribute ${sizing.attr}`)
check(
  Math.abs(sizing.attr - sizing.box) <= 2,
  `one SVG unit is one CSS pixel -- width attribute ${sizing.attr} vs box ${sizing.box}`,
)
check(
  (sizing.viewBox ?? '').startsWith(`0 0 ${sizing.attr} `),
  `the viewBox matches the pixel size rather than stretching it -- got "${sizing.viewBox}"`,
)

// Two chart inks only: no per-series hue cycling survived the rewrite.
const inks = await page.$$eval('.drum svg path', (els) =>
  [...new Set(els.map((e) => getComputedStyle(e).fill))].sort(),
)
check(inks.length > 0 && inks.length <= 2, `two chart inks only -- got ${JSON.stringify(inks)}`)

// --- The old surfaces are gone, not hidden -------------------------------
for (const gone of ['Event volume — last hour', 'Flags raised — last hour']) {
  const found = await page.locator(`text=${gone}`).count()
  check(found === 0, `the old overlay chart "${gone}" is gone`)
}

// --- The cursor, and its survival across a view switch -------------------
await page.click(VIEW_BUTTON('Table'))
await page.locator(TABLE).waitFor({ state: 'visible', timeout: 10000 })

const minuteButtons = page.locator('.table-view tbody th button.minute')
await minuteButtons.first().waitFor({ state: 'visible', timeout: 10000 })
const chosenMinute = (await minuteButtons.nth(1).textContent()).trim()
await minuteButtons.nth(1).click()

await page.locator('.table-view tbody tr.selected').waitFor({ state: 'visible', timeout: 5000 })
const selectedInTable = (await page.locator('.table-view tbody tr.selected th button.minute').textContent()).trim()
check(selectedInTable === chosenMinute, `the table highlights the minute clicked -- got "${selectedInTable}"`)

await page.click(VIEW_BUTTON('Seismograph'))
await page.locator(`${SEISMOGRAPH} line.cursor`).waitFor({ state: 'visible', timeout: 10000 })
const drumMinute = (await page.locator('.drum svg text.cursor-label').textContent()).trim()
check(drumMinute === chosenMinute, `the drum's cursor is on the same minute -- got "${drumMinute}" for "${chosenMinute}"`)

await page.click(VIEW_BUTTON('Register'))
await page.locator(`${REGISTER} line.cursor`).waitFor({ state: 'visible', timeout: 10000 })
const crossSection = (await page.locator('.cross-section h3').textContent()).trim()
check(
  crossSection === `The minute ${chosenMinute}`,
  `the register reads the same minute across the page -- got "${crossSection}"`,
)

// The cursor reads the whole minute, not one series: every traffic
// series plus the episode count is in the cross-section.
const crossRows = await page.$$eval('.cross-section .xs-row dt', (els) => els.map((e) => e.textContent.trim()))
check(
  crossRows.length === 8 && crossRows[crossRows.length - 1] === 'flag episodes',
  `the cursor reads every series at once -- got ${JSON.stringify(crossRows)}`,
)

// --- The preference is persisted, and applied before first paint ---------
const stored = await page.evaluate(() => localStorage.getItem('mikroview-metrics-view'))
check(stored === 'register', `the chosen view is persisted -- got ${JSON.stringify(stored)}`)

await page.reload({ waitUntil: 'networkidle' })
await page.click('.rail .item .label:text-is("Metrics")')
// No click on a view button between the reload and this wait: if the
// preference were applied after first paint, the seismograph would be
// on screen here instead.
await page.locator(REGISTER).waitFor({ state: 'visible', timeout: 10000 })
const pressedAfterReload = await page.$eval('.views button[aria-pressed="true"]', (e) => e.textContent.trim())
check(pressedAfterReload === 'Register', `the stored view survives a reload -- got "${pressedAfterReload}"`)
check(
  (await page.locator(SEISMOGRAPH).count()) === 0,
  'the default view is not mounted first and then replaced',
)

// The cursor is deliberately *not* persisted: a minute selected an hour
// ago has aged off the axis, so a reload starts with none.
check(
  (await page.locator(`${REGISTER} line.cursor`).count()) === 0,
  'the cursor starts clear after a reload rather than pointing at a stale minute',
)

// --- The keyboard contract ----------------------------------------------
const surface = page.locator('.surface[role="slider"]')
await surface.waitFor({ state: 'visible', timeout: 5000 })
await surface.focus()
await page.keyboard.press('End')
await page.locator(`${REGISTER} line.cursor`).waitFor({ state: 'visible', timeout: 5000 })
const atBrink = await surface.getAttribute('aria-valuetext')
check(/\d\d:\d\d — Accept /.test(atBrink ?? ''), `End reads the brink minute and its figures -- got "${atBrink}"`)

await page.keyboard.press('ArrowLeft')
const oneBack = await surface.getAttribute('aria-valuetext')
check(oneBack !== atBrink, 'an arrow moves the cursor one minute')

await page.keyboard.press('Escape')
check(
  (await page.locator(`${REGISTER} line.cursor`).count()) === 0,
  'Escape clears the cursor',
)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
