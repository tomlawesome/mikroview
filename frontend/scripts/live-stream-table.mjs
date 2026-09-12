// SPDX-License-Identifier: AGPL-3.0-only
//
// #644's "columns squared" restyle (round-29 scene 4) shrank the live
// table's header set from twelve columns to nine and moved six facts
// into EventDetailSheet: device, chain, interfaces, src port, NAT, MAC.
// The owner reversed that half in #717 -- "Lost from the original... I
// knew we could add back the missing columns... It's now later" -- so
// the six are columns again and the set is fifteen. What #644 did keep,
// and this script still guards, is the removal of the per-cell ⓘ
// (Ip/PortInvestigateButton) triggers and every row opening the sheet.
//
// LiveTable.svelte.test.ts's jsdom suite already proves the per-field
// rendering rules (named vs bare/geo, em dashes, ms timestamps) against
// hand-built fixtures. What it cannot show is that a real browser lays
// the *header row* out with exactly this label set, and that clicking
// into a real row's time cell actually opens the sheet -- the same gap
// live-group-mode.mjs's own comment describes for its own layout check.

import { session, feedSyslog, check, done, waitForStreamRows, goTo, DESKTOP_VIEWPORT } from './live-browser.mjs'

// #717's fifteen are the *desktop* set, and since #1150 that has a width:
// below 1500px a reader who has never opened the picker starts with MAC
// and Interfaces off, because at 1366 the fifteen measured 1762px into a
// 1308px box and the right-hand end of the table ran off the edge.
// Playwright's own default is 1280, so the width this half is about has
// to be asked for; the narrow start is asserted on its own terms at the
// foot of this file.
const { page, consoleErrors } = await session({ viewport: DESKTOP_VIEWPORT })
feedSyslog(20, 'live-stream-table')
await waitForStreamRows(page, 20)

// --- The header set is exactly the fifteen, in order --------------------
const headerLabels = await page.$$eval('.grid .header-cell .label-text', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  JSON.stringify(headerLabels) ===
    JSON.stringify([
      'Time', 'Device', 'Action', 'Chain', 'Source', 'Src address', 'Src port', 'MAC',
      'Destination', 'Dst address', 'Proto', 'Interfaces', 'Dst port', 'NAT', 'Rule',
    ]),
  `at desktop width the stream table shows exactly the fifteen columns, in order -- got ${JSON.stringify(headerLabels)}`,
)
// The six restored by #717, each named so a regression says which went.
for (const label of ['Device', 'Chain', 'Src port', 'MAC', 'Interfaces', 'NAT']) {
  check(headerLabels.includes(label), `${label} is back on the row (#717), not only in the detail sheet`)
}

// --- Every row lays out with one cell per header column -----------------
const firstRowCells = await page.$$eval(
  '.grid .row',
  (els) => els[0]?.querySelectorAll('.cell').length ?? 0,
)
check(
  firstRowCells === headerLabels.length,
  `a row's own cell count matches the header's (${firstRowCells} vs ${headerLabels.length})`,
)

// --- The action badge is present, and NAT has its own cell again --------
check(await page.isVisible('.grid .row .cell.action .badge'), 'the action cell renders the shared badge')
check(
  await page.$$eval('.grid .row', (els) => els.every((e) => e.querySelector('.cell.nat') !== null)),
  'every row carries its own .cell.nat again (#717 restored the column)',
)

// --- The per-cell ⓘ investigate triggers are gone ------------------------
// RouterRuleButton (the rule cell's pushed-table lookup, #186/#445) keeps
// its own "i" glyph -- that trigger was never one of the ⓘ buttons this
// issue retires, and live-before-router-lookup.mjs/live-nat-popup.mjs cover it
// staying put. What must be gone is IpInvestigateButton/
// PortInvestigateButton, both labelled "Investigate ..." -- distinct
// from RouterRuleButton's "Look up ..." labels, so this can tell them
// apart without depending on class names either script already owns.
const investigateGlyphs = await page.$$eval('.grid .row', (els) =>
  els.flatMap((e) => [...e.querySelectorAll('[aria-label]')].map((b) => b.getAttribute('aria-label') ?? '')),
)
check(
  !investigateGlyphs.some((l) => l.startsWith('Investigate ')),
  `no row carries an IP/port investigate trigger any more -- got ${JSON.stringify(investigateGlyphs.filter((l) => l.startsWith('Investigate ')))}`,
)

// --- Clicking a row opens the detail sheet ------------------------------
const firstRow = page.locator('.grid .row').first()
await firstRow.locator('.time-btn').click()
const sheet = page.locator('.sheet[role="dialog"]')
await sheet.waitFor({ state: 'visible', timeout: 5000 })
check(await sheet.isVisible(), 'clicking a row\'s time cell opens the detail sheet')
// Chain is on the row again, and the sheet still carries it: the sheet
// is the row's full record, not a home for whatever the row lost.
check((await sheet.textContent())?.includes('Chain') ?? false, 'the sheet still carries Chain alongside the row')
await page.keyboard.press('Escape')
await sheet.waitFor({ state: 'hidden', timeout: 5000 })

// --- Below 1500px it starts with thirteen, and says so (#1150) ----------
//
// Not a second table and not a second mechanism: the width only decides
// where a reader who has never opened `columns ▸` starts, and it is read
// once at load -- hence a reload here rather than a bare resize, which
// would arrive after the decision was made.
await page.setViewportSize({ width: 1366, height: 900 })
await page.reload({ waitUntil: 'networkidle' })
await goTo(page, 'Stream')
await waitForStreamRows(page, 1)

const narrowLabels = await page.$$eval('.grid .header-cell .label-text', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  JSON.stringify(narrowLabels) === JSON.stringify(headerLabels.filter((l) => l !== 'MAC' && l !== 'Interfaces')),
  `at 1366 the table starts with the desktop set less MAC and Interfaces, in the same order -- got ${JSON.stringify(narrowLabels)}`,
)

// Nothing silent about it: the picker draws both unticked, because every
// checkbox in it reads the same isColumnVisible the table does.
await page.click('button.tf-columns')
const macBox = page.locator('.col-panel input[aria-label="Source MAC column"]')
const ifaceBox = page.locator('.col-panel input[aria-label="Interfaces column"]')
await macBox.waitFor({ timeout: 5000 })
check(
  !(await macBox.isChecked()) && !(await ifaceBox.isChecked()),
  'the columns ▸ picker draws MAC and Interfaces unticked -- off, not missing',
)

// And ticking one back on is the ordinary path, not a narrow-screen one.
await macBox.check()
await page
  .waitForFunction(
    () => [...document.querySelectorAll('.grid .header-cell .label-text')].some((e) => e.textContent.trim() === 'MAC'),
    undefined,
    { timeout: 5000 },
  )
  .catch(() => {})
const afterTick = await page.$$eval('.grid .header-cell .label-text', (els) => els.map((e) => e.textContent.trim()))
check(afterTick.includes('MAC'), `ticking MAC puts its column back on the row -- got ${JSON.stringify(afterTick)}`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
