// SPDX-License-Identifier: AGPL-3.0-only
//
// #549's connection loss and recovery, on #616's chrome: the banner
// still tops the content column, and the live indicator now sits in
// every scene bar. What needs a real browser rather than a unit test:
//
// - "Pushes, never overlays" is a layout claim -- what could regress is
//   App.svelte's shell putting the banner back inside an absolutely
//   positioned wrapper, which only a real layout engine can catch.
// - The scene bar's indicator and the banner are driven by the same
//   appState.connState, but only a real dropped WebSocket exercises the
//   actual onclose -> 'closed' path end to end.
// - "Nav stays operable throughout" means clicking a roll-rail name
//   while genuinely disconnected still rolls the deck -- a claim about
//   the rail's onclick not being gated on connection state, which only
//   matters while a real disconnect is in effect.
//
// Loss is simulated by routing /api/ws through page.routeWebSocket
// (Playwright's own WebSocket interception) rather than
// context.setOffline: setOffline was tried first and does not reliably
// sever an already-established loopback WebSocket in Chromium. See this
// file's history for the long version.

import { session, feedSyslog, check, responsive, goTo, done } from './live-browser.mjs'

feedSyslog(40, 'connection-states')
const { page, consoleErrors } = await session({ waitForEvents: 20 })

// --- Wire the WS interception, transparent by default ----------------------
// `blocked` gates whether a *new* connection attempt is allowed through;
// `current` is the most recently intercepted route, so dropConnection()
// can force-close whatever is open right now.
let blocked = false
let current = null

await page.routeWebSocket('**/api/ws', (ws) => {
  if (blocked) {
    ws.close()
    return
  }
  current = ws
  const server = ws.connectToServer()
  ws.onMessage((message) => server.send(message))
  server.onMessage((message) => ws.send(message))
  ws.onClose(() => server.close())
  server.onClose(() => ws.close())
})

async function dropConnection() {
  blocked = true
  current?.close()
}

function restoreConnection() {
  blocked = false
}

// A fresh connection through the now-installed route -- reload rather
// than reconnect in place. The reload lands back on the fall (#616's
// landing), so return to Stream explicitly.
await page.reload()
await page.waitForSelector('.roll-rail .rail-name', { timeout: 10000 })
await goTo(page, 'Stream')

// The active card's own indicator -- the deck mounts the neighbouring
// cards too, each scene bar with a .conn of its own.
const CONN = '.card[aria-hidden="false"] .conn'
await page.waitForSelector(`${CONN}.conn-open`, { timeout: 15000 })

// --- Baseline: connected, quiet indicator, no banner ----------------------
check((await page.$('.banner-closed, .banner-connecting')) === null, 'no banner while connected')
check(
  // #683 (round 29) ratified 'LIVE' (uppercase), with a rate suffix everywhere except the Stream card's own bar --
  // SceneBar.svelte:107 passes `showRate={view !== 'live'}`, so on Stream (what goTo('Stream') above landed on)
  // ConnectionIndicator.svelte renders bare 'LIVE' with no ` · N/s` (ConnectionIndicator.svelte:19,30-32).
  (await page.$eval(CONN, (el) => el.textContent.trim())) === 'LIVE',
  'the scene bar says LIVE while connected',
)

const mainTopConnected = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)

// --- Connection lost -------------------------------------------------------
await dropConnection()

const bannerHandle = await page.waitForSelector('.banner-closed', { timeout: 15000 })
check(true, 'the banner appears once the connection is actually lost')

// Measure from the handle waitForSelector just returned, and take the
// content top right alongside it -- both in the same step as the wait, so
// a reconnect landing later in the scenario can't unmount the banner out
// from under a separate, later $eval('.banner-closed', ...).
const bannerBox = await bannerHandle.evaluate((el) => el.getBoundingClientRect())
const mainTopDisconnected = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)

await page.waitForSelector(`${CONN}.conn-closed`, { timeout: 15000 })
check(
  (await page.$eval(CONN, (el) => el.textContent.trim())) === 'Disconnected',
  'the scene bar indicator turns Disconnected with it',
)

// "Tops the content column and pushes content -- never overlays." An
// overlay would leave #main-content's top exactly where it was; pushing
// moves it down by roughly the banner's own height.
check(
  mainTopDisconnected > mainTopConnected + 10,
  `content is pushed down, not overlaid -- top went from ${mainTopConnected} to ${mainTopDisconnected}`,
)
check(
  bannerBox.bottom <= mainTopDisconnected + 1,
  `the banner sits above the content it pushed, not over it -- banner bottom ${bannerBox.bottom}, content top ${mainTopDisconnected}`,
)

// --- Nav stays operable while disconnected ---------------------------------
await goTo(page, 'Metrics')
check(true, 'clicking a roll-rail name still rolls the deck while disconnected')
check(
  (await page.$$(`${CONN}.conn-closed`)).length === 1,
  "and the Metrics card's own bar carries the same disconnected state -- no scene is blind to it",
)
await goTo(page, 'Stream')
await page.waitForSelector('input.rule', { timeout: 5000 })
check(true, 'and switching back works too')

// The roll rail itself is still fully rendered, not degraded.
check((await page.$$('.roll-rail .rail-name')).length > 0, 'the roll rail is still fully rendered')

// --- Recovery ----------------------------------------------------------------
restoreConnection()
await page.waitForSelector('.banner-closed', { state: 'detached', timeout: 15000 })
check(true, 'the banner clears once the connection actually recovers')
await page.waitForSelector(`${CONN}.conn-open`, { timeout: 15000 })
check(true, 'the scene bar indicator clears with it')

const mainTopRecovered = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)
check(
  Math.abs(mainTopRecovered - mainTopConnected) < 2,
  `content returns to its pre-loss position once the banner clears -- got ${mainTopRecovered}, expected ~${mainTopConnected}`,
)

check(await responsive(page), 'main thread responsive throughout')

// The WS route intercept above logs no console error of its own -- this
// is left in place (not filtered) so a genuine one still fails the run.
check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
