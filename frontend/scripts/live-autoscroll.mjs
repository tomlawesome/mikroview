// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #232: Autoscroll off must hold the visible window -- not just
// skip the jump-to-bottom -- including across navigating away to another
// view and back (the freeze lives on appState.frozenPool, not on
// LiveTable itself, which unmounts on every view switch -- see
// state.svelte.ts). Not unit-testable end to end: jsdom has no
// layout/scrolling, and the cross-view case only exists once a real
// second view is actually mounted against the same running app.

// Rounds 36-38 moved the control this drives. Autoscroll is no longer a
// button on the scene bar (round 30 retired the toolbar it came from and
// homed it nowhere, which is #749); it is the `following` pill on the
// whisper's own line, and it reads `follow` while off. The assertions
// below are unchanged in substance -- what the freeze must do is what it
// always had to do -- only the selector and the two tooltip readings
// follow the control to where it now lives.
import { session, feedSyslog, check, done, goTo, eventsTotal, waitForEventsTotal, waitForStreamRows } from './live-browser.mjs'

// Scoped to the centred card: the deck mounts the neighbouring cards
// too, and the whisper belongs to the Stream card.
const CARD = '.card[aria-hidden="false"]'
const FOLLOW = `${CARD} .whisper .hand-btn.follow`

// Two labelled batches, comfortably past MAX_RENDERED_ROWS (800) between
// them, so the freeze is exercised where the reported symptom actually
// lives -- below that threshold every event renders regardless of
// autoscroll, and the bug is invisible.
//
// The split is load-bearing, not stylistic: it is what gets the window
// past its 800-row cap. The client's initial GET /api/events asks for
// limit:500 (state.svelte.ts), so seeding before the page is up can
// never do it -- only events arriving over the WebSocket afterwards can,
// and both batches do, which is why they are fed in two goes with the
// page waiting on the first before the second lands.
const { page } = await session()

feedSyslog(450, 'batch-a')
await waitForStreamRows(page, 400)

feedSyslog(450, 'batch-b')
await page.waitForFunction(() => document.querySelectorAll('.row').length >= 800, null, { timeout: 30000 })

check(
  (await page.textContent(FOLLOW))?.trim() === 'following',
  'the pill reads "following" while the stream is following',
)
// "newest line", not "newest events": the drawn wording (round 36) is
// about the line arriving, which is the thing on screen. Same claim.
const tooltipBefore = await page.getAttribute(FOLLOW, 'title')
check(/newest line/i.test(tooltipBefore ?? ''), `the pill's tooltip describes following new lines (${tooltipBefore})`)

await page.click(FOLLOW)
await page.waitForFunction(
  (sel) => document.querySelector(sel)?.textContent?.trim() === 'follow',
  FOLLOW,
  { timeout: 5000 },
)

check((await page.textContent(FOLLOW))?.trim() === 'follow', 'and reads "follow" -- the way back -- once off')
const tooltipAfter = await page.getAttribute(FOLLOW, 'title')
check(
  /stays put|hold/i.test(tooltipAfter ?? ''),
  `the off tooltip says the table stays put, not just "no auto-jump" (${tooltipAfter})`,
)

const frozenCount = await page.locator('.row').count()
check(frozenCount > 0, `the frozen window has rows (${frozenCount})`)

// New events arrive while frozen -- none of them may appear. Wait for
// the server to have actually counted them (the freeze under test is a
// client-rendering concern, not an ingest one) rather than guessing how
// long ingest takes.
const beforeAfterFreeze = await eventsTotal(page)
feedSyslog(50, 'after-freeze')
await waitForEventsTotal(page, beforeAfterFreeze + 50)

check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'no row from after the freeze appears while autoscroll is off',
)
check((await page.locator('.row').count()) === frozenCount, `row count is unchanged while frozen (still ${frozenCount})`)

// Navigating away to another view and back must not disturb the freeze
// -- LiveTable unmounts on every view switch, so this only proves
// anything if appState.frozenPool genuinely outlives the component.
// goTo() itself already blocks until the destination card is centred,
// which is proof enough that the switch (and so the unmount) happened.
await goTo(page, 'Metrics')

// Back to the live view via its own rail item -- the rail has no
// re-click-to-return-to-live behaviour the old menu trigger had.
await goTo(page, 'Stream')
await page.waitForSelector(`${CARD} .row`, { timeout: 10000 })
check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'the live view is still frozen after visiting another view',
)

// A filter change while frozen must narrow the visible rows -- within
// what was already frozen, never pulling in "after-freeze" just because
// the filter changed.
await page.fill('input.rule', 'batch-b')
await page.waitForFunction(() => document.querySelectorAll('.row[title*="batch-a"]').length === 0, { timeout: 5000 })

const batchBCount = await page.locator('.row').count()
check(batchBCount > 0, `filtering to batch-b while frozen narrows the table (${batchBCount} rows)`)
check((await page.locator('.row[title*="batch-a"]').count()) === 0, 'batch-a rows are excluded by the filter')
check((await page.locator('.row[title*="after-freeze"]').count()) === 0, 'after-freeze rows stay excluded, filter or not')

// Clearing the filter re-widens within the same frozen pool -- batch-a
// comes back, after-freeze still does not.
await page.fill('input.rule', '')
await page.waitForFunction(() => document.querySelectorAll('.row[title*="batch-a"]').length > 0, { timeout: 5000 })

check((await page.locator('.row[title*="batch-a"]').count()) > 0, 'clearing the filter brings batch-a back')
check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'after-freeze still absent once the filter clears -- the frozen pool never grew',
)

// Following again releases the freeze.
await page.click(FOLLOW)
feedSyslog(20, 'resumed')
await page.waitForFunction(() => document.querySelector('.row[title*="resumed"]') !== null, {
  timeout: 5000,
}).then(
  () => check(true, 'following again resumes following new events'),
  () => check(false, 'following again resumes following new events'),
)

done()
