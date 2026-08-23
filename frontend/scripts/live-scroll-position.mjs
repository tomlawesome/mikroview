// SPDX-License-Identifier: AGPL-3.0-only
//
// Two scroll defects the v0.2.0 preview pass turned up, pinned against a
// real browser at a real viewport (#383, #384).
//
// Neither was visible from the code, and one of them is actively
// misleading there: the Entities jump reads like a re-render problem,
// and the CSS and the keyed {#each} blocks both look correct. It is a
// focus() call on a row that should never have existed. So both
// assertions here measure the thing the operator actually experiences --
// can I reach the bottom of the page, and am I still where I was -- and
// not the mechanism, which is free to change.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 60 })

// The viewport both defects were reported at. Fixed rather than
// inherited: a scroll assertion that runs at whatever size the harness
// defaults to is a scroll assertion that can silently stop overflowing.
await page.setViewportSize({ width: 1280, height: 720 })

// --- #383: every wizard step is reachable -------------------------------
// #app is height: 100vh; overflow: hidden, so a view that declares no
// scroll container of its own has its overflow clipped and unreachable.
// Setup.svelte was the only view missing the flex/min-height/overflow-y
// trio the other eight carry, which made the guided setup -- the
// first-run experience specifically -- impossible to read past the fold.
await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("Connect a router")')
await page.waitForSelector('.setup')

const setup = await page.$eval('.setup', (el) => ({
  scrollHeight: el.scrollHeight,
  clientHeight: el.clientHeight,
  overflowY: getComputedStyle(el).overflowY,
}))
check(
  setup.scrollHeight > setup.clientHeight,
  `the wizard overflows this viewport, so scrolling it is a real question (content ${setup.scrollHeight}px in ${setup.clientHeight}px)`,
)
check(
  setup.overflowY === 'auto' || setup.overflowY === 'scroll',
  `the wizard declares its own scroll container (overflow-y: ${setup.overflowY})`,
)

await page.$eval('.setup', (el) => el.scrollTo(0, el.scrollHeight))
const reached = await page.$eval('.setup', (el) => ({
  scrollTop: el.scrollTop,
  atBottom: el.scrollTop >= el.scrollHeight - el.clientHeight - 2,
}))
// Both halves, because either alone passes while the defect is present:
// with overflow clipped, scrollHeight === clientHeight, so "at the
// bottom" is vacuously true at scrollTop 0.
check(
  reached.atBottom && reached.scrollTop > 0,
  `the wizard scrolls, and reaches its bottom (scrollTop ${reached.scrollTop})`,
)

// The assertion #383 actually asked for: not "a scrollbar exists" but
// "the last element of the longest step is reachable".
//
// Measured against the browser viewport, never against .setup's own
// box. When the overflow is clipped, .setup's box is its full unclipped
// height, so a rect comparison against it says the last step is
// "inside" while the operator cannot see or reach it -- the assertion
// would pass on exactly the build it exists to catch. window.innerHeight
// is what the operator actually has.
const lastVisible = await page.evaluate(() => {
  const sections = document.querySelectorAll('.setup section')
  const last = sections[sections.length - 1]
  if (!last) return null
  const r = last.getBoundingClientRect()
  return { top: r.top, bottom: r.bottom, viewportHeight: window.innerHeight }
})
check(lastVisible !== null, 'the wizard renders at least one step section')
check(
  lastVisible.bottom <= lastVisible.viewportHeight + 2 && lastVisible.bottom > 0,
  `the last step is on screen once scrolled to the bottom -- not clipped past the fold (bottom ${Math.round(lastVisible.bottom)}px, viewport ${lastVisible.viewportHeight}px)`,
)

// --- #384: naming an entity leaves the operator where they were ---------
// The workflow the defect punished is the one the view exists for:
// working down a long discovered list naming things one after another.
await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("Entities")')
await page.waitForSelector('.page .row.discovered')

const entities = await page.$eval('.page', (el) => ({
  scrollHeight: el.scrollHeight,
  clientHeight: el.clientHeight,
}))
check(
  entities.scrollHeight > entities.clientHeight * 3,
  `the discovered list is long enough for losing your place to matter (${entities.scrollHeight}px in ${entities.clientHeight}px)`,
)

// Partway down, not at the top -- at the top the defect is invisible.
await page.$eval('.page', (el) => el.scrollTo(0, Math.floor(el.scrollHeight * 0.6)))
await page.waitForTimeout(300)
const before = await page.$eval('.page', (el) => el.scrollTop)
check(before > entities.clientHeight * 2, `the view is scrolled well down before the add (scrollTop ${before})`)

// Pick a row that is actually on screen at this position, so the click
// itself cannot be what moves the viewport.
const target = await page.evaluate(() => {
  const pageEl = document.querySelector('.page')
  const pr = pageEl.getBoundingClientRect()
  for (const row of document.querySelectorAll('.row.discovered')) {
    const r = row.getBoundingClientRect()
    if (r.top > pr.top + 60 && r.bottom < pr.bottom - 60) return row.querySelector('.key')?.textContent ?? null
  }
  return null
})
check(target !== null, 'a discovered row is on screen at this scroll position to name')

await page.click(`.row.discovered:has(.key:text-is("${target}")) button.name-it`)
await page.fill('.row.discovered .inline-input', 'live-scroll-check')

// Saved with Enter, not by clicking Save: Playwright scrolls a click
// target into view first, which would hide exactly the defect under
// test if the button ever sat off screen.
await page.focus('.row.discovered .inline-input')
await page.keyboard.press('Enter')
await page.waitForFunction(
  (k) => !document.querySelector(`.row.discovered .key[data-probe="${k}"]`) &&
    Array.from(document.querySelectorAll('.section')).some((s) =>
      /Named entities/.test(s.querySelector('h3')?.textContent ?? '') &&
      /live-scroll-check/.test(s.textContent ?? '')),
  target,
  { timeout: 15000 },
)

const after = await page.$eval('.page', (el) => el.scrollTop)
// One row's worth of tolerance, and no more. A row is inserted into
// "Named entities" above the viewport and one leaves "Discovered"
// below, so the compensated position legitimately shifts by about a row
// height -- the defect moved it by seventeen thousand pixels.
const drift = Math.abs(after - before)
check(
  drift <= 120,
  `naming an entity leaves the operator where they were (scrollTop ${before} -> ${after}, drift ${drift}px)`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
