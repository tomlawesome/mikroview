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
// #app is height: 100vh; overflow: hidden, so anything that declares no
// scroll container of its own has its overflow clipped and unreachable.
// The wizard page was the only view missing the flex/min-height/
// overflow-y trio, which made the guided setup -- the first-run
// experience specifically -- impossible to read past the fold.
//
// #487 replaced that page with a modal, and the defect can recur in the
// same shape: a modal taller than the viewport, or a body that does not
// scroll, hides the bottom of a step just as effectively. So this now
// measures the modal, on the step whose body is genuinely long (step 4
// carries the whole push script).
//
// The modal caps itself at 92vh, so at 720px its body still fits the
// longest step and there would be nothing to scroll -- an assertion
// that cannot fail is worse than none. A shorter window is not a
// contrived condition either: it is a laptop with browser chrome, or a
// window that is not full height, and it is exactly where a clipped
// step body would bite. Restored to 1280x720 before the #384 half
// below, which is the viewport that defect was reported at.
await page.setViewportSize({ width: 1280, height: 460 })

await page.click('.rail .item:has-text("Run setup…")')
const wizard = page.locator('.setup-wizard')
await wizard.waitFor({ state: 'visible' })

await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').click()
if (await page.locator('.setup-wizard .mint select').count()) {
  const devices = await page.request.get(`${process.env.MV_URL}/api/devices`).then((r) => r.json())
  const withEvents = devices.find((d) => d.eventCount > 0) ?? devices[0]
  if (withEvents) {
    await page.selectOption('.setup-wizard .mint select', withEvents.id)
    await page.click('.setup-wizard .mint button.primary')
  }
}
await page.locator('.setup-wizard pre.script').waitFor({ state: 'visible' })

const modalBox = await page.$eval('.setup-wizard', (el) => {
  const r = el.getBoundingClientRect()
  return { top: r.top, bottom: r.bottom, viewportHeight: window.innerHeight }
})
check(
  modalBox.top >= -1 && modalBox.bottom <= modalBox.viewportHeight + 1,
  `the modal fits the viewport rather than running off it (${Math.round(modalBox.top)}px..${Math.round(modalBox.bottom)}px in ${modalBox.viewportHeight}px)`,
)

const body = await page.$eval('.setup-wizard .body', (el) => ({
  scrollHeight: el.scrollHeight,
  clientHeight: el.clientHeight,
  overflowY: getComputedStyle(el).overflowY,
}))
check(
  body.scrollHeight > body.clientHeight,
  `the step body overflows this viewport, so scrolling it is a real question (content ${body.scrollHeight}px in ${body.clientHeight}px)`,
)
check(
  body.overflowY === 'auto' || body.overflowY === 'scroll',
  `the step body declares its own scroll container (overflow-y: ${body.overflowY})`,
)

await page.$eval('.setup-wizard .body', (el) => el.scrollTo(0, el.scrollHeight))
const reached = await page.$eval('.setup-wizard .body', (el) => ({
  scrollTop: el.scrollTop,
  atBottom: el.scrollTop >= el.scrollHeight - el.clientHeight - 2,
}))
// Both halves, because either alone passes while the defect is present:
// with overflow clipped, scrollHeight === clientHeight, so "at the
// bottom" is vacuously true at scrollTop 0.
check(
  reached.atBottom && reached.scrollTop > 0,
  `the step body scrolls, and reaches its bottom (scrollTop ${reached.scrollTop})`,
)

// The assertion #383 actually asked for: not "a scrollbar exists" but
// "the last element of the longest step is reachable".
//
// Measured against the browser viewport, never against the body's own
// box. When the overflow is clipped, that box is its full unclipped
// height, so a rect comparison against it says the last element is
// "inside" while the operator cannot see or reach it -- the assertion
// would pass on exactly the build it exists to catch. window.innerHeight
// is what the operator actually has.
const lastVisible = await page.evaluate(() => {
  const children = document.querySelectorAll('.setup-wizard .body > *')
  const last = children[children.length - 1]
  if (!last) return null
  const r = last.getBoundingClientRect()
  return { top: r.top, bottom: r.bottom, viewportHeight: window.innerHeight }
})
check(lastVisible !== null, 'the wizard renders a step body')
check(
  lastVisible.bottom <= lastVisible.viewportHeight + 2 && lastVisible.bottom > 0,
  `the bottom of the step is on screen once scrolled to it -- not clipped past the fold (bottom ${Math.round(lastVisible.bottom)}px, viewport ${lastVisible.viewportHeight}px)`,
)

// Explicit close, so the rest of this scenario is not driving the page
// through a focus trap.
await page.keyboard.press('Escape')
await wizard.waitFor({ state: 'detached' })
await page.setViewportSize({ width: 1280, height: 720 })

// --- #384: naming an entity leaves the operator where they were ---------
// The workflow the defect punished is the one the view exists for:
// working down a long discovered list naming things one after another.
await page.click('.rail .item:has-text("Entities")')
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
