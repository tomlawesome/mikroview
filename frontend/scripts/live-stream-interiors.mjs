// SPDX-License-Identifier: AGPL-3.0-only
//
// #644: the whisper (a quiet rate curve above the live table that can
// seek and fence the stream) and the filter bar's new folded thin row.
// Companion to live-smoke.mjs, which already covers the live view's
// plain substring filter and its row count -- this covers what #644
// added on top.
//
// Note on the filter row: #644 (round 8, "The whisper commands the
// stream, and the filter box folds to a thin bar") made the desktop
// filter row start folded behind a "Filters ▸" trigger rather than
// always on screen. Every field here is therefore scoped under
// `.bar.thin` rather than the bare `input.rule` selector live-smoke.mjs
// still uses -- that selector only resolves once this scenario's own
// first section (below) has unfolded the row.
//
// Both the whisper's click state (seek/fence) and the filter bar's own
// fields are plain in-memory Svelte state, not persisted -- each
// live-check scenario launches its own fresh browser (see
// live-browser.mjs's session()), so neither leaks into the next
// script's page load regardless of what this one leaves behind. The
// restore/clear/fold-back steps below exist anyway, both because a
// scenario that starts from an assumption instead of a fact is exactly
// this project's own recurring trap, and because "the fence dims, then
// clearing restores" is itself part of what #644 asked for -- the
// cleanup step is also the assertion.

import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

const CARD = '.card[data-card="live"]'

/** apiUrl resolves a path against the page's own origin, for page.request. */
function apiUrl(page, path) {
  return new URL(path, page.url()).toString()
}

// Mirrors lib/format.ts's formatHM exactly -- Node and the browser share
// one OS clock/timezone in this harness, so the two agree on what a
// minute's HH:MM label is.
function hmLabel(iso) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

/** populatedMinutes reads which of the server's own axis minutes actually carry traffic. */
async function populatedMinutes(page) {
  const res = await page.request.get(apiUrl(page, '/api/stats'))
  const body = await res.json()
  return (body.timeSeries ?? [])
    .filter((b) => Object.values(b.byAction ?? {}).some((v) => v > 0))
    .map((b) => b.time)
}

feedSyslog(120, 'stream-interiors')

// unfoldFilter: false -- this scenario owns the fold. session() opens the
// stream's filter for every other scenario, which would leave the two
// checks below asserting against a box already unfolded (#667).
const { page, consoleErrors } = await session({ waitForEvents: 60, unfoldFilter: false })

const autoscrollBtn = page.locator(`${CARD} .scene-bar button:text-is("Autoscroll")`)
const isAutoscrollOn = () => autoscrollBtn.evaluate((el) => el.classList.contains('active'))

// Defensive: an earlier scenario should have left this on, but a
// scenario that starts from an assumed answer instead of a checked fact
// is exactly the trap this file's own header warns about.
if (!(await isAutoscrollOn())) await autoscrollBtn.click()

// ============================================================
// The whisper
// ============================================================

const wstat = page.locator(`${CARD} .whisper .wstat`)
const wfence = page.locator(`${CARD} .wfence`)
const ticks = page.locator(`${CARD} .wbar .wtick`)

// --- Rendered above the live table, carrying a rate figure ----------------
const whisperAboveTable = await page.evaluate((sel) => {
  const whisper = document.querySelector(`${sel} .whisper`)
  const table = document.querySelector(`${sel} .table-wrap`)
  if (!whisper || !table) return false
  // DOCUMENT_POSITION_FOLLOWING on the *table* (asked from the whisper's
  // own node) means the table comes after the whisper in the document.
  return !!(whisper.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING)
}, CARD)
check(whisperAboveTable, 'the whisper sits above the live table in the document')

await wstat.waitFor({ state: 'visible', timeout: 10000 })
const rollingStat = (await wstat.textContent()).trim()
check(/\d[\d.]*\/s/.test(rollingStat), `the whisper carries a rate figure -- got "${rollingStat}"`)

const tickCount = await ticks.count()
check(tickCount > 0, `the whisper draws a tick per minute of its window -- got ${tickCount}`)

// --- Clicking the curve seeks -----------------------------------------------
await ticks.nth(tickCount - 1).click()
check(!(await isAutoscrollOn()), 'seeking the whisper turns Autoscroll off (the scene bar is the one source of truth for it)')
const seekStat = (await wstat.textContent()).trim()
check(seekStat !== rollingStat, `the stat line changes once seeked -- got "${seekStat}"`)
check(!/now\b/.test(seekStat), `the stat line no longer reads "now" once seeked -- got "${seekStat}"`)

// Restored before the fence test below touches the table too -- a fenced
// view under Autoscroll-off would compound two holds into one failure if
// either half of this scenario went wrong.
await autoscrollBtn.click()
check(await isAutoscrollOn(), 'Autoscroll turns back on')

// --- The fence toggle plus two clicks dims, and clearing restores ---------
//
// Fencing needs something outside its range to dim, which needs real
// traffic in at least two distinct wall-clock minutes -- not guaranteed
// by one feedSyslog call landing inside a single minute, so this checks
// first and only waits for the clock to turn over if it must.
let populated = await populatedMinutes(page)
if (populated.length < 2) {
  const beforeMinute = Math.floor(Date.now() / 60000)
  while (Math.floor(Date.now() / 60000) === beforeMinute) {
    await page.waitForTimeout(1000)
  }
  feedSyslog(20, 'stream-interiors-fence')
  await page.waitForTimeout(1500)
  populated = await populatedMinutes(page)
}
check(populated.length >= 2, `at least two minutes carry real traffic before fencing -- got ${populated.length}`)

const targetLabel = hmLabel(populated[0])
const tickLabels = await ticks.evaluateAll((els) => els.map((el) => el.getAttribute('aria-label') ?? ''))
const targetIdx = tickLabels.findIndex((l) => l.includes(targetLabel))
check(targetIdx >= 0, `the whisper has a tick for the populated minute ${targetLabel}`)

await wfence.click()
check((await wfence.getAttribute('aria-pressed')) === 'true', 'the fence toggle turns on')

if (targetIdx >= 0) {
  // Same tick twice: the first click opens the fence at that minute, the
  // second closes a one-minute-wide range there -- narrow enough that
  // real traffic in any other populated minute is guaranteed to fall
  // outside it.
  await ticks.nth(targetIdx).click()
  await ticks.nth(targetIdx).click()
}

await page
  .waitForFunction((sel) => document.querySelectorAll(`${sel} .row.dimmed`).length > 0, CARD, { timeout: 8000 })
  .catch(() => {})
const dimmedCount = await page.locator(`${CARD} .row.dimmed`).count()
check(dimmedCount > 0, `the fence dims rows outside its range -- ${dimmedCount} dimmed`)

const fenceStat = (await wstat.textContent()).trim()
check(/fenced/.test(fenceStat), `the stat line reports the fenced range -- got "${fenceStat}"`)

// Clearing (the fence toggle itself, per whisper.svelte.ts's toggleFence)
// restores every row -- this is also this scenario's own cleanup for the
// fence, not just an assertion.
await wfence.click()
check((await wfence.getAttribute('aria-pressed')) === 'false', 'the fence toggle turns off')
await page
  .waitForFunction((sel) => document.querySelectorAll(`${sel} .row.dimmed`).length === 0, CARD, { timeout: 8000 })
  .catch(() => {})
const dimmedAfter = await page.locator(`${CARD} .row.dimmed`).count()
check(dimmedAfter === 0, `clearing the fence restores every row -- ${dimmedAfter} still dimmed`)

// ============================================================
// The thin filter bar
// ============================================================

const foldTrigger = page.locator(`${CARD} button.fold-trigger`)
await foldTrigger.waitFor({ state: 'visible', timeout: 10000 })
check(
  (await foldTrigger.textContent()).trim().startsWith('Filters'),
  'the folded "Filters ▸" trigger is on screen by default on desktop',
)
check((await page.locator(`${CARD} .bar.thin`).count()) === 0, 'the filter row starts folded')

await foldTrigger.click()
const thinBar = page.locator(`${CARD} .bar.thin`)
await thinBar.waitFor({ state: 'visible', timeout: 5000 })
const microLabels = await page.$$eval(`${CARD} .bar.thin .fb-label`, (els) => els.map((e) => e.textContent.trim()))
check(
  microLabels.includes('Device') && microLabels.includes('Action') && microLabels.length >= 5,
  `unfolding shows the thin bar's own micro-labels -- got ${JSON.stringify(microLabels)}`,
)

// --- Setting one field writes the real filter, not a cosmetic one ---------
//
// A fresh, unique rule label rather than one of the feedSyslog batches
// above: the suite's shared instance carries arbitrary history by now,
// so a common label could already match rows this scenario never fed.
const uniqueLabel = `thinbar-${Date.now()}`
feedSyslog(15, uniqueLabel)
await page.waitForFunction((sel) => document.querySelectorAll(`${sel} .row`).length > 0, CARD, { timeout: 15000 })

const ruleInput = page.locator(`${CARD} .bar.thin .rule-group input.rule`)
await ruleInput.fill(uniqueLabel)
await page.waitForTimeout(600)

// appState.hasActiveFilters is the filter state the UI itself exposes --
// the thin bar's own "clear" control is gated on exactly that flag.
check(
  await page.locator(`${CARD} .bar.thin .tf-clear`).isVisible(),
  'the thin bar shows its own "clear" once a field is set, agreeing with the filter state it just wrote',
)

const rowCount = await page.locator(`${CARD} .row`).count()
check(rowCount > 0, `the typed rule filters the stream to at least one row -- got ${rowCount}`)
const ruleTexts = await page.$$eval(`${CARD} .cell.rule .rule-btn`, (els) => els.map((e) => e.textContent.trim()))
check(
  ruleTexts.length === rowCount && ruleTexts.every((t) => t === uniqueLabel),
  `every visible row's own rule label agrees with the field just set -- got ${JSON.stringify([...new Set(ruleTexts)])}`,
)

// --- Clear and fold back before done(): scenarios share the instance ------
await page.locator(`${CARD} .bar.thin .tf-clear`).click()
await page.waitForTimeout(300)
check(
  (await page.locator(`${CARD} .bar.thin .tf-clear`).count()) === 0,
  "clearing removes the thin bar's own \"clear\" control again",
)

await page.locator(`${CARD} .bar.thin button.tf-fold`).click()
await thinBar.waitFor({ state: 'detached', timeout: 5000 })
check((await foldTrigger.count()) > 0, 'the row folds back to the "Filters ▸" trigger')
check((await page.locator(`${CARD} .fold-trigger .dot`).count()) === 0, 'no active-filter dot remains once cleared')

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
