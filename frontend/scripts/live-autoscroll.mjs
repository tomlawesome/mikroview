// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #232: Autoscroll off must hold the visible window -- not just
// skip the jump-to-bottom -- along with the ControlPorts scoping this
// branch adds (a global Autoscroll toggle must not freeze a table that
// has no Autoscroll control of its own). Neither is unit-testable end to
// end: jsdom has no layout/scrolling, and the cross-view leak only
// exists once both LiveTable instances are mounted against the same
// running app.

import { session, feedSyslog, feedRaw, check, done } from './live-browser.mjs'

/** feedControlPort sends one attempt against dst port 22 (SSH, on by default). */
function feedControlPort(label) {
  feedRaw(
    `firewall,info D|${label}| forward: in:ether1 out:bridge1, connection-state:new, ` +
      `proto TCP (SYN), 203.0.113.77:51000->192.168.1.10:22, len 60`,
  )
}

// Two labelled batches, comfortably past MAX_RENDERED_ROWS (800) between
// them, so the freeze is exercised where the reported symptom actually
// lives -- below that threshold every event renders regardless of
// autoscroll, and the bug is invisible.
//
// The split around session() is load-bearing, not stylistic: the client's
// initial GET /api/events asks for limit:500 (state.svelte.ts), so no
// amount of pre-seeding gets the page past 500 rows. Only events arriving
// over the WebSocket, after the page is up, can drive the window past the
// 800-row cap. Feeding both batches up front leaves it stuck at 500 and
// the wait below never satisfies.
feedSyslog(450, 'batch-a')

const { page } = await session({ waitForEvents: 400 })

feedSyslog(450, 'batch-b')
await page.waitForFunction(() => document.querySelectorAll('.row').length >= 800, null, { timeout: 30000 })

const tooltipBefore = await page.getAttribute('header.toolbar button:has-text("Autoscroll")', 'title')
check(/newest events/i.test(tooltipBefore ?? ''), `Autoscroll tooltip describes following new events (${tooltipBefore})`)

await page.click('header.toolbar button:has-text("Autoscroll")')
await page.waitForTimeout(200)

const tooltipAfter = await page.getAttribute('header.toolbar button:has-text("Autoscroll")', 'title')
check(
  /stays put|hold/i.test(tooltipAfter ?? ''),
  `Autoscroll-off tooltip says the table stays put, not just "no auto-jump" (${tooltipAfter})`,
)

const frozenCount = await page.locator('.row').count()
check(frozenCount > 0, `the frozen window has rows (${frozenCount})`)

// New events arrive while frozen -- none of them may appear.
feedSyslog(50, 'after-freeze')
await page.waitForTimeout(1500)

check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'no row from after the freeze appears while autoscroll is off',
)
check((await page.locator('.row').count()) === frozenCount, `row count is unchanged while frozen (still ${frozenCount})`)

// The Control Ports tab has no Autoscroll control of its own -- toggling
// the live view's must not freeze it (issue #232's fix leaking into
// ControlPorts.svelte, since both mount LiveTable against the same
// global appState.autoscroll).
feedControlPort('ctrl-before-nav')
await page.waitForTimeout(500)

await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("Control ports")')
await page.waitForSelector('.row[title*="ctrl-before-nav"]', { timeout: 5000 })

feedControlPort('ctrl-after-nav')
await page.waitForFunction(
  () => document.querySelector('.row[title*="ctrl-after-nav"]') !== null,
  { timeout: 5000 },
).then(
  () => check(true, 'Control Ports keeps updating while the live view is frozen'),
  () => check(false, 'Control Ports keeps updating while the live view is frozen'),
)

// Back on the live view, the freeze must still be holding -- visiting
// another tab must not have disturbed it.
// There is no "Live view" menu entry on this branch -- that arrives with
// #237. The way back here is re-clicking the view you are already on,
// which toggleView() maps to 'live'.
await page.click('.nav-menu .trigger')
await page.click('.nav-menu button:has-text("Control ports")')
await page.waitForTimeout(300)
check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'the live view is still frozen after visiting Control Ports',
)

// A filter change while frozen must narrow the visible rows -- within
// what was already frozen, never pulling in "after-freeze" just because
// the filter changed.
await page.fill('input.rule', 'batch-b')
await page.waitForTimeout(300)

const batchBCount = await page.locator('.row').count()
check(batchBCount > 0, `filtering to batch-b while frozen narrows the table (${batchBCount} rows)`)
check((await page.locator('.row[title*="batch-a"]').count()) === 0, 'batch-a rows are excluded by the filter')
check((await page.locator('.row[title*="after-freeze"]').count()) === 0, 'after-freeze rows stay excluded, filter or not')

// Clearing the filter re-widens within the same frozen pool -- batch-a
// comes back, after-freeze still does not.
await page.fill('input.rule', '')
await page.waitForTimeout(300)

check((await page.locator('.row[title*="batch-a"]').count()) > 0, 'clearing the filter brings batch-a back')
check(
  (await page.locator('.row[title*="after-freeze"]').count()) === 0,
  'after-freeze still absent once the filter clears -- the frozen pool never grew',
)

// Autoscroll back on releases the freeze.
await page.click('header.toolbar button:has-text("Autoscroll")')
feedSyslog(20, 'resumed')
await page.waitForFunction(() => document.querySelector('.row[title*="resumed"]') !== null, {
  timeout: 5000,
}).then(
  () => check(true, 'turning Autoscroll back on resumes following new events'),
  () => check(false, 'turning Autoscroll back on resumes following new events'),
)

done()
