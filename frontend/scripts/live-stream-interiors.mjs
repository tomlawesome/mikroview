// SPDX-License-Identifier: AGPL-3.0-only
//
// #644: the whisper (a quiet rate curve above the live table that can
// seek and fence the stream) and the filter bar's folded strip.
// Companion to live-smoke.mjs, which already covers the live view's
// plain substring filter and its row count -- this covers what #644
// added on top.
//
// Both sections were rewritten under #963 to follow the current UI:
//
// - The whisper (#717, "the elegant-fence redraw"): the old per-minute
//   `.wtick` marks and the "arm with a `.wfence` button, then two
//   clicks" gesture are gone. The curve is one polyline now
//   (Whisper.svelte's `.wline`, one vertex per bucket); a plain click on
//   it seeks, and a real drag sets the fence to the range dragged. See
//   that component's own top-of-file comment.
// - The filter row: round 30 (#697, owner 2026-08-31) retired the
//   standalone "Filters ▸" trigger this scenario used to click --
//   FilterBar.svelte gates it on `FILTERS_TRIGGER_ENABLED = false`, so
//   `button.fold-trigger` renders nowhere any more. The always-on-screen
//   `.fbox` (FilterBar.svelte's own disclosure) is the way in now: a
//   click anywhere inside it opens the same strip (`#filterbar-strip`,
//   still `.bar.thin` at desktop width) this scenario already asserts
//   the fields of.
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

// lib/whisperStats.ts's own constant: Stats.TimeSeries always carries 60
// one-minute buckets, and the whisper shows the last 15 of them
// (recentBuckets), so the curve always draws exactly this many vertices
// regardless of how many are actually populated with traffic.
const WHISPER_WINDOW_MINUTES = 15

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

/** The whisper's own last-15 slice of the server's 60-bucket series (whisperStats.ts's recentBuckets, mirrored here since the scenario has no access to the component's reactive state). */
async function visibleBuckets(page) {
  const res = await page.request.get(apiUrl(page, '/api/stats'))
  const body = await res.json()
  const series = body.timeSeries ?? []
  return series.slice(Math.max(0, series.length - WHISPER_WINDOW_MINUTES))
}

feedSyslog(120, 'stream-interiors')

// unfoldFilter: false -- this scenario owns the fold. session() opens the
// stream's filter for every other scenario, which would leave the two
// checks below asserting against a box already unfolded (#667).
const { page, consoleErrors } = await session({ waitForEvents: 60, unfoldFilter: false })

// Rounds 36-38: following is the `following` pill on the whisper's own
// line, not a button on the scene bar -- the whisper commands the stream
// and its seek is what stops the lines following, so the verb sits with
// its cause. Same state, same assertions, new home.
const followBtn = page.locator(`${CARD} .whisper .hand-btn.follow`)
const isFollowing = () => followBtn.evaluate((el) => el.classList.contains('on'))

// Defensive: an earlier scenario should have left this on, but a
// scenario that starts from an assumed answer instead of a checked fact
// is exactly the trap this file's own header warns about.
if (!(await isFollowing())) await followBtn.click()

// ============================================================
// The whisper
// ============================================================

const wstat = page.locator(`${CARD} .whisper .wstat`)
const wsvg = page.locator(`${CARD} .whisper .wsvg`)
const wline = page.locator(`${CARD} .whisper .wline`)
const wband = page.locator(`${CARD} .whisper .wband`)

// Kept a hair inside both edges: page.mouse's raw x,y (unlike a
// locator's own .click()) does no actionability check, so a coordinate
// landing exactly on (or a rounding error past) the svg's own boundary
// silently clicks whatever is next to it instead -- observed missing
// the seek entirely at frac=1 (the rightmost bucket) before this clamp.
function svgX(box, frac) {
  const clamped = Math.min(0.99, Math.max(0.01, frac))
  return box.x + clamped * box.width
}

/** Clicks (no drag) the curve at bucket `index` of `total` -- a plain seek. */
async function clickBucket(index, total) {
  const box = await wsvg.boundingBox()
  const frac = total > 1 ? index / (total - 1) : 0.5
  await page.mouse.click(svgX(box, frac), box.y + box.height / 2)
}

/** Drags a short distance around bucket `index`, closing a fence over exactly that one minute -- both ends resolve to the same bucket since the offset stays well inside one bucket's own pixel span. */
async function dragBucket(index, total) {
  const box = await wsvg.boundingBox()
  const frac = total > 1 ? index / (total - 1) : 0.5
  const x = svgX(box, frac)
  const y = box.y + box.height / 2
  const bucketPx = total > 1 ? box.width / (total - 1) : box.width
  const offset = Math.min(10, bucketPx / 3)
  await page.mouse.move(x - offset, y)
  await page.mouse.down()
  await page.mouse.move(x + offset, y, { steps: 4 })
  await page.mouse.up()
}

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

// #717 draws one continuous curve rather than a discrete tick per
// minute -- the per-minute fact now is one polyline vertex per bucket.
await wline.waitFor({ state: 'attached', timeout: 10000 })
const pointsAttr = ((await wline.getAttribute('points')) ?? '').trim()
const vertexCount = pointsAttr ? pointsAttr.split(/\s+/).length : 0
check(
  vertexCount === WHISPER_WINDOW_MINUTES,
  `the whisper draws one point per minute of its ${WHISPER_WINDOW_MINUTES}-minute window -- got ${vertexCount}`,
)

// --- Clicking the curve seeks -----------------------------------------------
await clickBucket(vertexCount - 1, vertexCount) // the rightmost point -- "now"
check(!(await isFollowing()), 'seeking the whisper stops the stream following (the pill on its own line is the one source of truth for it)')
check(
  (await followBtn.textContent())?.trim() === 'follow',
  'and the pill reads "follow" -- the way back, which #749 had none of',
)
const seekStat = (await wstat.textContent()).trim()
check(seekStat !== rollingStat, `the stat line changes once seeked -- got "${seekStat}"`)
check(!/now\b/.test(seekStat), `the stat line no longer reads "now" once seeked -- got "${seekStat}"`)

// Restored before the fence test below touches the table too -- a fenced
// view with following already off would compound two holds into one
// failure if either half of this scenario went wrong.
await followBtn.click()
check(await isFollowing(), 'following turns back on')

// --- A real drag dims, and clearing restores -------------------------------
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

// The target minute's position within the whisper's own visible window
// (its last WHISPER_WINDOW_MINUTES buckets), not within the server's
// full 60-bucket series -- mirrors whisperStats.ts's recentBuckets.
const visible = await visibleBuckets(page)
const targetLabel = hmLabel(populated[0])
const targetIdx = visible.findIndex((b) => b.time === populated[0])
check(targetIdx >= 0, `the whisper's own window includes the populated minute ${targetLabel}`)

if (targetIdx >= 0) {
  await dragBucket(targetIdx, visible.length)
}
check(await wband.count() > 0, 'a drag draws the fence band')

await page
  .waitForFunction((sel) => document.querySelectorAll(`${sel} .row.dimmed`).length > 0, CARD, { timeout: 8000 })
  .catch(() => {})
const dimmedCount = await page.locator(`${CARD} .row.dimmed`).count()
check(dimmedCount > 0, `the fence dims rows outside its range -- ${dimmedCount} dimmed`)

const fenceStat = (await wstat.textContent()).trim()
check(/fenced/.test(fenceStat), `the stat line reports the fenced range -- got "${fenceStat}"`)

// Escape clears the fence (Whisper.svelte's onKeyDown) -- the curve still
// holds keyboard focus from the drag's own pointerdown, same as a real
// user tabbing back to it. This is also this scenario's own cleanup for
// the fence, not just an assertion.
await page.keyboard.press('Escape')
check((await wband.count()) === 0, 'clearing the fence removes the band')
await page
  .waitForFunction((sel) => document.querySelectorAll(`${sel} .row.dimmed`).length === 0, CARD, { timeout: 8000 })
  .catch(() => {})
const dimmedAfter = await page.locator(`${CARD} .row.dimmed`).count()
check(dimmedAfter === 0, `clearing the fence restores every row -- ${dimmedAfter} still dimmed`)

// #968: clearFence() itself turns following back on -- the drag's own
// setFenceRange stopped it because the fence is a window on the past,
// and clearing that window removes the reason to hold the stream, so
// no extra click on the pill should be needed here (unlike the seek
// case above, which is a different gesture with its own way back).
check(await isFollowing(), 'clearing the fence turns following back on, unfreezing the table for what comes next')

// ============================================================
// The filter box's own folding strip
// ============================================================

// #697 retired the standalone "Filters ▸" trigger (FilterBar.svelte's
// FILTERS_TRIGGER_ENABLED = false); the always-on-screen `.fbox` is the
// disclosure now -- a click anywhere inside it opens the same strip.
const fbox = page.locator(`${CARD} .filterline .fbox`)
await fbox.waitFor({ state: 'visible', timeout: 10000 })
check(
  (await fbox.getAttribute('class'))?.includes('empty'),
  'the always-on-screen filter box carries no filter yet, by default',
)
check((await page.locator(`${CARD} .bar.thin`).count()) === 0, 'the filter row starts folded')

await fbox.click()
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
check((await fbox.count()) > 0, 'the row folds back into the filter box')
check((await page.locator(`${CARD} .filterline .fbox .chip`).count()) === 0, 'no filter chip remains once cleared')

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
