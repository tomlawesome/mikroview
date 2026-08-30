// SPDX-License-Identifier: AGPL-3.0-only
//
// #644's "columns squared" restyle (round-29 scene 4): the live table's
// header set shrank from twelve columns to nine, the per-cell ⓘ
// (Ip/PortInvestigateButton) triggers are gone, and every row -- not
// just the mobile card -- now opens EventDetailSheet for the fields the
// dropped columns used to carry (device, chain, interfaces, src port,
// NAT, MAC).
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

// --- The header set is exactly the ratified nine, in order --------------
const headerLabels = await page.$$eval('.grid .header-cell .label-text', (els) =>
  els.map((e) => e.textContent.trim()),
)
check(
  JSON.stringify(headerLabels) ===
    JSON.stringify(['Time', 'Action', 'Source', 'Address', 'Destination', 'Address', 'Proto', 'Port', 'Rule']),
  `the stream table shows exactly the ratified nine columns, in order -- got ${JSON.stringify(headerLabels)}`,
)
check(!headerLabels.includes('NAT'), 'NAT is not its own column (it rides the action badge instead)')
check(!headerLabels.includes('Device'), 'Device is gone from the row -- it lives in the detail sheet now')
check(!headerLabels.includes('Chain'), 'Chain is gone from the row -- it lives in the detail sheet now')

// --- Every row lays out with exactly nine cells, matching the header ----
const firstRowCells = await page.$$eval(
  '.grid .row',
  (els) => els[0]?.querySelectorAll('.cell').length ?? 0,
)
check(
  firstRowCells === headerLabels.length,
  `a row's own cell count matches the header's (${firstRowCells} vs ${headerLabels.length})`,
)

// --- The action badge is present, and NAT never gets its own cell -------
check(await page.isVisible('.grid .row .cell.action .badge'), 'the action cell renders the shared badge')
check(
  (await page.$$eval('.grid .row', (els) => els.some((e) => e.querySelector('.cell.nat')))) === false,
  'no row carries a leftover .cell.nat',
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

// --- Clicking a row opens the detail sheet, carrying the moved fields ---
const firstRow = page.locator('.grid .row').first()
await firstRow.locator('.time-btn').click()
const sheet = page.locator('.sheet[role="dialog"]')
await sheet.waitFor({ state: 'visible', timeout: 5000 })
check(await sheet.isVisible(), 'clicking a row\'s time cell opens the detail sheet')
check((await sheet.textContent())?.includes('Chain') ?? false, 'the sheet carries Chain, dropped from the row')
await page.keyboard.press('Escape')
await sheet.waitFor({ state: 'hidden', timeout: 5000 })

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
