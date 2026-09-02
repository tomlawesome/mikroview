// SPDX-License-Identifier: AGPL-3.0-only
//
// Grouping in the live view (#341): the toggle, the count replacing
// the time, the flag marker, and the drawer.
//
// Unit tests cover the grouping itself. What they cannot show is the
// part that has broken twice in this project already -- that a component
// laid out in a CSS grid still lays out once another row type is added
// to it. EventRow uses `display: contents`, so its cells *are* the grid
// children; a wrapper element would silently destroy the column
// alignment while every test still passed.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session({ waitForEvents: 200 })

const rowCount = () => page.$$eval('.grid .row', (els) => els.length)
// Not `.row:first-of-type` -- the first div child of .grid is a header
// cell, so that selector matches nothing and any comparison built on it
// passes as 0 === 0 without testing anything.
const cellsInFirstRow = () =>
  page.$$eval('.grid .row', (els) => els[0]?.querySelectorAll('.cell').length ?? 0)

// --- Normal mode is unchanged -------------------------------------------
const normalRows = await rowCount()
const normalCells = await cellsInFirstRow()
check(normalRows > 0, `the live view renders rows (${normalRows})`)
check(await page.isVisible('.grid .cell.time'), 'rows show a time')

// --- Turning it on collapses repeats ------------------------------------
await page.click('.whisper .hand-btn:text-is("group")')
await page.waitForTimeout(500)

const groupedRows = await rowCount()
check(
  groupedRows > 0 && groupedRows <= normalRows,
  `grouping shows no more rows than normal (${groupedRows} <= ${normalRows})`,
)

// The layout assertion that unit tests cannot make: a collapsed row must
// still produce the same number of grid cells as a normal one, or the
// columns no longer line up with their headers.
const groupedCells = await cellsInFirstRow()
check(
  normalCells > 0 && groupedCells === normalCells,
  `a grouped row has the same cell count as a normal one (${groupedCells} vs ${normalCells})`,
)

// --- The count replaces the time on a collapsed row ---------------------
const counts = await page.$$eval('.grid .count', (els) =>
  els.map((e) => Number(e.textContent?.trim())),
)
if (counts.length > 0) {
  check(
    counts.every((n) => Number.isFinite(n) && n > 1),
    `every count shown is a real number above 1 (${counts.slice(0, 5)})`,
  )

  // Nothing may be lost: the counts plus the singletons must account for
  // every row normal mode would have shown.
  const singletons = groupedRows - counts.length
  const accounted = counts.reduce((a, b) => a + b, 0) + singletons
  check(
    accounted === normalRows,
    `the counts account for every event (${accounted} vs ${normalRows} rows)`,
  )

  // --- The drawer ------------------------------------------------------
  await page.click('.grid .count-cell')
  await page.waitForTimeout(300)
  const afterOpen = await rowCount()
  check(afterOpen > groupedRows, `opening a group reveals its events (${groupedRows} -> ${afterOpen})`)
  check(await page.isVisible('.grid .row.member'), 'the revealed events are marked as members')

  const revealed = afterOpen - groupedRows
  check(revealed <= 20, `the drawer renders at most 20 events (${revealed})`)

  await page.click('.grid .count-cell')
  await page.waitForTimeout(300)
  check((await rowCount()) === groupedRows, 'closing the group hides them again')
} else {
  // Honest rather than silently passing: with no repeats in the feed
  // there is nothing to collapse, and that is a real state.
  check(true, 'no repeated connections in this run, so nothing collapsed (not a failure)')
}

// --- Off again ----------------------------------------------------------
await page.click('.whisper .hand-btn:text-is("group")')
await page.waitForTimeout(500)
check((await rowCount()) === normalRows, 'turning it off restores every row')

// --- The control does not exist where it cannot work (#381) -------------
// Below 700px LiveTable renders EventCardMobile, which has no grouped
// form. The button used to survive into that layout, take its active
// styling, flip its title, and persist the preference -- while changing
// nothing on screen. A control that lies about what it did is worse than
// a missing one, so it is hidden rather than reimplemented.
//
// Asserted in a real browser because that is the only place the
// breakpoint exists: viewportState reads matchMedia, which jsdom does not
// implement (LiveTable's unit tests stub it to a permanent `false`), so
// no unit test in this repo can tell these two layouts apart.
await page.setViewportSize({ width: 480, height: 900 })
await page.waitForTimeout(400)

// button.card, not .card: the deck's own snap sections carry class
// "card" too (#616), and counting those would let this pass with no
// event cards at all.
const mobileCards = await page.$$eval('button.card', (els) => els.length)
check(mobileCards > 0, `phone width renders the card layout (${mobileCards} cards)`)
check(
  !(await page.isVisible('.whisper .hand-btn:text-is("group")')),
  'the group toggle is absent at phone width, where grouping has no effect',
)

// Back to desktop width: the control returns, and returns usable.
await page.setViewportSize({ width: 1280, height: 720 })
await page.waitForTimeout(400)
check(
  await page.isVisible('.whisper .hand-btn:text-is("group")'),
  'the group toggle comes back at desktop width',
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
