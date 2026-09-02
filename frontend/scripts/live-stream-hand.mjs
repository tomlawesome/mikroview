// SPDX-License-Identifier: AGPL-3.0-only
//
// Rounds 36-38 on the stream (#800): the hand on the whisper's line, the
// column boundary that draws nothing until the hand is over it, saved
// filters at the filter box's right end, and the foot band gone.
//
// Three of these cannot be checked anywhere but in a real browser.
// A CSS `:hover` state has no meaning in jsdom -- getComputedStyle there
// answers from the rules that matched at parse time, so a "nothing is
// drawn at rest" assertion would pass whether or not the rest state was
// ever styled, which is the vacuous-test shape live-newest-first.mjs's
// header warns about. Neither has a pseudo-element's own computed style,
// which is where the hairline actually lives. And a wipe emptying the
// screen while the server's ring keeps every line is a claim about two
// buffers, only one of which exists in a unit test.
//
// live-scene-bar-controls.mjs owns the hand's own toggling (following
// two-way, pause, csv). This owns what it does to the table.

import { session, feedSyslog, check, done } from './live-browser.mjs'

// The active card -- the deck mounts the neighbouring cards too, and the
// whisper and its hand belong to the Stream card.
const CARD_SEL = '.card[aria-hidden="false"]'

feedSyslog(120, 'hand-rule')
// unfoldFilter: false -- one of the checks below is that opening the
// saved list does NOT also unfold the filter strip, and session() opens
// that strip for every other scenario (#667), which would leave the
// check asserting against a strip that was already open.
const { page, consoleErrors } = await session({ waitForEvents: 60, unfoldFilter: false })

// The always-present type-ahead inside the box, which is the same
// appState.filters.rule the strip's own Rule field writes -- reachable
// with the strip folded, which is how this scenario runs.
const TERM = `${CARD_SEL} .filterline input.fbtype`

const CARD = CARD_SEL
const hand = (cls) => `${CARD} .whisper .hand-btn${cls}`
const pill = (label) => `${CARD} .whisper .wpill:text-is("${label}")`

// ============================================================
// The column boundary: nothing at rest, a hairline under the hand
// ============================================================

const headers = page.locator(`${CARD} .grid .header-cell`)
const headerCount = await headers.count()
check(headerCount > 1, `the stream has a header row to find a boundary in (${headerCount} cells)`)

// The second cell, not the first: the first is the sticky time column,
// and reading its edge would confound "sticky" with "hovered".
const target = headers.nth(1)

const boundaryStyle = (el) => {
  const s = getComputedStyle(el, '::after')
  return { color: s.borderRightColor, cursor: s.cursor, width: s.width }
}

// Move the pointer somewhere that is definitely not the header first --
// Playwright leaves the mouse wherever the last action put it, and a
// stale hover here would report the hover state as the resting one.
await page.mouse.move(0, 0)
await page.waitForTimeout(200)

const atRest = await target.evaluate(boundaryStyle)
check(
  atRest.color === 'rgba(0, 0, 0, 0)' || atRest.color === 'transparent',
  `nothing is drawn on the column boundary at rest (border-right-color: ${atRest.color})`,
)

await target.hover()
await page.waitForTimeout(300)

const hovered = await target.evaluate(boundaryStyle)
check(
  hovered.color !== atRest.color,
  `the boundary shows itself under the hand (${atRest.color} -> ${hovered.color})`,
)
check(
  hovered.color !== 'rgba(0, 0, 0, 0)' && hovered.color !== 'transparent',
  `and what it shows is a real hairline, not another transparent one (${hovered.color})`,
)
check(hovered.cursor === 'col-resize', `the cursor says what the boundary does (${hovered.cursor})`)

// The drag target itself exists and sits over a boundary -- the hairline
// above is only an affordance if something is actually draggable there.
const resizers = page.locator(`${CARD} .grid .resizer`)
check((await resizers.count()) === headerCount - 1, 'one drag target per boundary, and none past the last column')

// ============================================================
// The foot band is gone
// ============================================================

check(
  (await page.$$(`${CARD} .foot-legend`)).length === 0,
  'no foot band under the stream (owner, round 36: "I don\'t want that at all")',
)

// ============================================================
// Saved filters, at the filter box's right end
// ============================================================

const saved = page.locator(`${CARD} .filterline .fbox .fsaved`)
check(await saved.isVisible(), 'saved ▾ rides the filter box, not a bar of its own')
check((await saved.textContent())?.trim() === 'saved ▾', 'reading "saved ▾"')

await saved.click()
const menu = page.locator(`${CARD} .fpmenu`)
await menu.waitFor({ state: 'visible', timeout: 5000 })
check(true, 'clicking it opens the list')
check((await saved.getAttribute('aria-expanded')) === 'true', 'and says so')

// Opening the saved list must not also unfold the filter strip -- the
// box is a disclosure for that strip, and reaching past it for a saved
// filter is not reaching for the fields.
check((await page.$$(`${CARD} .bar.thin`)).length === 0, 'and does not unfold the filter strip under it')

// With no filter set there is nothing to save, so nothing offers to.
check(
  (await menu.locator('.fpsave').count()) === 0,
  'with no filter set, "save this filter as…" is absent rather than offered and refused',
)

await page.keyboard.press('Escape')
await menu.waitFor({ state: 'detached', timeout: 5000 })
check(true, 'Escape closes it')

// Set a filter, and the save entry appears. The name comes from a
// prompt, so answer it rather than letting Playwright dismiss it.
page.on('dialog', (d) => d.accept('hand-rule, everything'))
await page.fill(TERM, 'hand-rule')
await page.waitForTimeout(500)

await saved.click()
await menu.waitFor({ state: 'visible', timeout: 5000 })
const saveEntry = menu.locator('.fpsave')
check((await saveEntry.count()) === 1, 'with a filter set, "save this filter as…" sits at the foot')
await saveEntry.click()
await page.waitForTimeout(300)

await saved.click()
await menu.waitFor({ state: 'visible', timeout: 5000 })
const rows = menu.locator('.fprow')
check((await rows.count()) === 1, `the saved filter is in the list (${await rows.count()})`)
check(
  ((await rows.first().textContent()) ?? '').includes('hand-rule, everything'),
  'under the name it was given',
)
check((await rows.first().locator('.fpx').count()) === 1, 'with an × to forget it')

await rows.first().locator('.fpx').click()
await page.waitForTimeout(300)
check((await menu.locator('.fprow').count()) === 0, 'and the × forgets it')
check(await menu.isVisible(), 'without closing the list out from under the hand')

await page.keyboard.press('Escape')
await page.fill(TERM, '')
await page.waitForTimeout(500)

// ============================================================
// group folds repeats; wipe empties this screen and says so
// ============================================================

const rowCount = () => page.locator(`${CARD} .grid .row`).count()
const before = await rowCount()
check(before > 0, `the table has lines to act on (${before})`)

// live-group-mode.mjs owns what grouping does to the rows; what is
// checked here is only that the hand's own pill drives it. `<=` rather
// than `<` for the same reason that scenario uses it: whether the feed
// happens to contain repeats is not this check's business, and asserting
// it would make this fail on a feed that simply had none.
await page.click(hand(':text-is("group")'))
await page.waitForTimeout(500)
const grouped = await rowCount()
check(
  (await page.getAttribute(hand(':text-is("group")'), 'aria-pressed')) === 'true',
  'the group pill turns on',
)
check(grouped <= before, `and folds repeats of the same line into one (${before} -> ${grouped})`)
await page.click(hand(':text-is("group")'))
await page.waitForTimeout(500)
check((await rowCount()) === before, 'clicking it again unfolds them')

await page.click(pill('wipe'))
await page.waitForTimeout(500)

check((await rowCount()) === 0, 'wipe empties the lines held on this screen')
// `.body .empty`, not `.empty`: the filter box carries an `empty` class
// of its own when no term is set, and the bare selector picked that up
// instead and read back "saved ▾". The table's empty state lives inside
// its own scroll body.
const emptyText = ((await page.textContent(`${CARD} .body .empty`)) ?? '').trim()
check(
  /wiped here, by you/.test(emptyText),
  `the table says who emptied it, not "waiting for events" (${emptyText})`,
)
check(
  /server's ring still holds every line/.test(emptyText),
  `and that the server kept its own copy -- the half that is not on screen (${emptyText})`,
)
const wstat = ((await page.textContent(`${CARD} .whisper .wstat`)) ?? '').trim()
check(/wiped \d\d:\d\d:\d\d/.test(wstat), `the whisper states when it was wiped (${wstat})`)

// The wipe notice describes a silence, so it must end when the silence
// does -- a stale one would keep saying "nothing since" over a full table.
feedSyslog(30, 'after-wipe')
await page.waitForFunction(
  (sel) => document.querySelectorAll(`${sel} .grid .row`).length > 0,
  CARD,
  { timeout: 15000 },
)
const wstatAfter = ((await page.textContent(`${CARD} .whisper .wstat`)) ?? '').trim()
check(!/wiped/.test(wstatAfter), `and stops saying it once lines arrive again (${wstatAfter})`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)
done()
