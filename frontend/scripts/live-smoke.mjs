// SPDX-License-Identifier: AGPL-3.0-only
//
// The baseline scenario every change runs: sign in, see live data, filter
// it, and come away with no console errors. A change-specific scenario
// goes in its own file alongside this one.

import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

feedSyslog(200, 'smoke-rule')
const { page, consoleErrors } = await session({ waitForEvents: 100 })

// Scoped to the Stream card: the deck (#616) keeps the neighbouring
// cards mounted, and their scenes render .row elements of their own.
const streamRows = () => page.evaluate(() => document.querySelectorAll('.card[data-card="live"] .row').length)
const rows = await streamRows()
check(rows >= 100, `live view rendered ${rows} events`)

await page.fill('input.rule', 'smoke-rule')
await page.waitForTimeout(600)
const filtered = await streamRows()
check(filtered > 0, `substring filter kept ${filtered} rows`)

await page.fill('input.rule', 'definitely-not-a-rule-xyz')
await page.waitForTimeout(600)
const none = await streamRows()
check(none === 0, 'a non-matching filter empties the table')

await page.fill('input.rule', '')
await page.waitForTimeout(600)
check(await responsive(page), 'main thread responsive throughout')
check(consoleErrors.length === 0, `no console errors (${consoleErrors.slice(0, 2).join(' | ') || 'none'})`)

done()
