// SPDX-License-Identifier: AGPL-3.0-only
//
// #616: the stream's controls moved from the retired toolbar onto the
// Stream card's own scene bar, unchanged in behaviour, and only there --
// Metrics and the other scenes carry a bare bar. A UI reorganisation has
// no unit-testable truth beyond "the control renders where claimed and
// does what it did", so this drives the real bar on the real deck.

import { session, feedSyslog, check, goTo, done } from './live-browser.mjs'

// Events in the buffer, so the controls have something to act on and
// Export has something to be enabled for.
feedSyslog(100)
const { page, consoleErrors } = await session({ waitForEvents: 50 })

// The active card's bar -- the deck mounts the neighbouring cards too,
// each with a scene bar of its own.
const BAR = '.card[aria-hidden="false"] .scene-bar'
const btn = (label) => `${BAR} .controls button:text-is("${label}")`

// --- The Stream card carries the stream's controls -------------------------
for (const label of ['Autoscroll', 'Pause', 'Group', 'Clear']) {
  check(await page.isVisible(btn(label)), `${label} is on the Stream card's scene bar`)
}
check(
  await page.isVisible(`${BAR} .controls select[aria-label="Display duration"]`),
  'the display-duration select is there too',
)
check(await page.isVisible(`${BAR} .controls .account button.chip`), 'alongside the account chip')

// --- And they still do what the toolbar's did ------------------------------
// Autoscroll defaults on; one click turns it off, one turns it back.
check(
  await page.$eval(btn('Autoscroll'), (el) => el.classList.contains('active')),
  'Autoscroll starts active',
)
await page.click(btn('Autoscroll'))
check(
  !(await page.$eval(btn('Autoscroll'), (el) => el.classList.contains('active'))),
  'clicking Autoscroll turns it off',
)
await page.click(btn('Autoscroll'))
check(
  await page.$eval(btn('Autoscroll'), (el) => el.classList.contains('active')),
  'and clicking again turns it back on',
)

// Pause renames itself to the way back, then resumes.
await page.click(btn('Pause'))
check(
  await page.isVisible(`${BAR} .controls button:has-text("Resume")`),
  'Pause becomes Resume while paused',
)
await page.click(`${BAR} .controls button:has-text("Resume")`)
check(await page.isVisible(btn('Pause')), 'and Resume goes back to Pause')

// --- Export stayed in the live view's filter bar (#137) --------------------
check(
  await page.isVisible('.card[aria-hidden="false"] .bar button:has-text("Export to CSV")'),
  'Export to CSV is in the filter bar, not the scene bar',
)
const [download] = await Promise.all([
  page.waitForEvent('download', { timeout: 10000 }),
  page.click('.card[aria-hidden="false"] .bar button:has-text("Export to CSV")'),
])
check(
  (download.suggestedFilename() ?? '').endsWith('.csv'),
  `the filter bar entry downloads a CSV (${download.suggestedFilename()})`,
)

// --- Metrics carries a bare bar: no stream controls anywhere on it ---------
await goTo(page, 'Metrics')
for (const label of ['Autoscroll', 'Pause', 'Group', 'Clear']) {
  check(
    (await page.$$(btn(label))).length === 0,
    `${label} is absent from the Metrics card's scene bar`,
  )
}
check(
  (await page.$$(`${BAR} .controls select`)).length === 0,
  'no display-duration select on Metrics either',
)
check(
  await page.isVisible(`${BAR} .controls .account button.chip`),
  'while the account chip is still there -- the chrome travels, the controls do not',
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)
done()
