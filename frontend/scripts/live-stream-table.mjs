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

import { session, feedSyslog, check, done } from './live-browser.mjs'

feedSyslog(20, 'live-stream-table')
const { page, consoleErrors } = await session({ waitForEvents: 20 })

// --- The header set is exactly the fifteen, in order --------------------
const headerLabels = await page.$$eval('.grid .header-cell .label-text', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  JSON.stringify(headerLabels) ===
    JSON.stringify([
      'Time', 'Device', 'Action', 'Chain', 'Source', 'Address', 'Src port', 'MAC',
      'Destination', 'Address', 'Proto', 'Interfaces', 'Port', 'NAT', 'Rule',
    ]),
  `the stream table shows exactly the fifteen columns, in order -- got ${JSON.stringify(headerLabels)}`,
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
// issue retires, and live-router-lookup.mjs/live-nat-popup.mjs cover it
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

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
