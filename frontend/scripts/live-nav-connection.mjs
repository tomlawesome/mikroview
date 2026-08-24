// SPDX-License-Identifier: AGPL-3.0-only
//
// #549: connection loss and recovery, the one piece of the chrome's own
// states the issue names explicitly for live-check. What needs a real
// browser rather than a unit test:
//
// - "Pushes, never overlays" is a layout claim -- ConnectionBanner.svelte
//   and ConnectionIndicator.svelte have no layout logic of their own to
//   unit test; what could regress is App.svelte's shell putting the
//   banner back inside an absolutely-positioned wrapper, which only a
//   real layout engine can catch.
// - The rail-head dot (NavRail.svelte's .rail-head-dot) and the banner
//   are driven by the same appState.connState, but only a real dropped
//   WebSocket exercises the actual onclose -> 'closed' path end to end --
//   a unit test setting appState.connState by hand would prove the
//   rendering, not the wiring.
// - "Nav stays operable throughout" means clicking a rail item while
//   genuinely disconnected still switches the view -- that is a claim
//   about the rail's onclick handlers not being gated on connection
//   state, which only matters while a real disconnect is in effect.
// - The docked case swaps which component carries the state entirely
//   (NavRail unmounts, NavHandle takes over) -- see NavHandle.svelte's
//   own comment on why connection state is deliberately not its job.
//
// Sorted alongside the other nav scenarios (badge < connection < rail <
// states), not first or last among them. It pushes no filter table, adds
// no watchlist/detector config, and its only effect on shared server
// state is the syslog batch every scenario here already feeds -- so
// nothing later in the alphabet inherits anything from it beyond that,
// and it does not need anything upstream of it either.
//
// Loss is simulated by routing /api/ws through page.routeWebSocket
// (Playwright's own WebSocket interception) rather than context.setOffline:
// setOffline was tried first and does not reliably sever an
// already-established loopback WebSocket in Chromium -- the connection
// just sat there "offline" and the banner never appeared, timing out
// after 20s with nothing else on screen to explain why. routeWebSocket
// intercepts at the transport the app itself uses, transparently proxying
// to the real server by default and refusing new connections on command,
// which is what makes the reconnect below the app's own real backoff loop
// (lib/ws.ts) running against a genuinely closed socket, not a scripted
// stand-in for one.
import { session, feedSyslog, check, responsive, done } from './live-browser.mjs'

feedSyslog(40, 'nav-connection')

// routeWebSocket has to be installed before the socket it will intercept
// is created, so this session is built by hand (session()'s own login
// flow already opens the WS by the time it returns) rather than through
// the shared helper.
const { page, consoleErrors } = await session({ waitForEvents: 20 })

// --- Wire the WS interception, transparent by default ----------------------
// `blocked` gates whether a *new* connection attempt is allowed through --
// what makes a reconnect attempt made while "offline" actually fail
// instead of silently succeeding through the proxy. `current` is the most
// recently intercepted route, so dropConnection() below can force-close
// whatever is open right now rather than only refusing future attempts.
let blocked = false
let current = null

await page.routeWebSocket('**/api/ws', (ws) => {
  if (blocked) {
    // Refused outright -- indistinguishable, from the app's side, from the
    // real listener being unreachable.
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
// than reconnect in place, matching live-nav-states.mjs's own pattern for
// getting a clean, intercepted socket. Also picks up the >1280px rail
// density this scenario needs (see its comment for why the default is
// otherwise worked out once, on Playwright's own boundary-width viewport).
await page.setViewportSize({ width: 1440, height: 900 })
await page.reload()
await page.waitForSelector('.rail .item', { timeout: 10000 })
await page.waitForSelector('.conn-open', { timeout: 15000 })

const STREAM_ITEM = '.rail .item:has(.label:text-is("Stream"))'
const METRICS_ITEM = '.rail .item:has(.label:text-is("Metrics"))'
const DOCK = '.state-btn[aria-label^="Dock the navigation"]'

// --- Baseline: connected, quiet dot, no banner ----------------------------
check((await page.$('.banner-closed, .banner-connecting')) === null, 'no banner while connected')
check(
  (await page.$eval('.rail-head-dot', (el) => el.classList.contains('alarm'))) === false,
  'the rail-head dot is quiet while connected',
)

const mainTopConnected = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)

// --- Connection lost -------------------------------------------------------
await dropConnection()

await page.waitForSelector('.banner-closed', { timeout: 15000 })
check(true, 'the banner appears once the connection is actually lost')

check(
  await page.$eval('.rail-head-dot', (el) => el.classList.contains('alarm')),
  'the rail-head dot turns alarm',
)

// "Tops the content column and pushes content -- never overlays." An
// overlay would leave #main-content's top exactly where it was; pushing
// moves it down by roughly the banner's own height.
const bannerBox = await page.$eval('.banner-closed', (el) => el.getBoundingClientRect())
const mainTopDisconnected = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)
check(
  mainTopDisconnected > mainTopConnected + 10,
  `content is pushed down, not overlaid -- top went from ${mainTopConnected} to ${mainTopDisconnected}`,
)
check(
  bannerBox.bottom <= mainTopDisconnected + 1,
  `the banner sits above the content it pushed, not over it -- banner bottom ${bannerBox.bottom}, content top ${mainTopDisconnected}`,
)
// Still inside the content column, not spanning the rail too -- the
// record is explicit the banner tops *the content column*.
const railBox = await page.$eval('.rail', (el) => el.getBoundingClientRect())
check(bannerBox.left >= railBox.right - 1, 'the banner starts after the rail, not over it')

// --- Nav stays operable while disconnected ---------------------------------
await page.click(METRICS_ITEM)
await page.waitForSelector('.dashboard', { timeout: 5000 })
check(true, 'clicking a rail item still switches the view while disconnected')
await page.click(STREAM_ITEM)
await page.waitForSelector('input.rule', { timeout: 5000 })
check(true, 'and switching back works too')

// The rail and its own controls are still there and still respond -- not
// just the one click above.
check((await page.$$('.rail .item')).length > 0, 'the rail itself is still fully rendered, not degraded')

// --- Recovery ----------------------------------------------------------------
restoreConnection()
await page.waitForSelector('.banner-closed', { state: 'detached', timeout: 15000 })
check(true, 'the banner clears once the connection actually recovers')
await page.waitForFunction(
  () => !document.querySelector('.rail-head-dot')?.classList.contains('alarm'),
  null,
  { timeout: 15000 },
)
check(true, 'the rail-head dot clears with it')

const mainTopRecovered = await page.$eval('#main-content', (el) => el.getBoundingClientRect().top)
check(
  Math.abs(mainTopRecovered - mainTopConnected) < 2,
  `content returns to its pre-loss position once the banner clears -- got ${mainTopRecovered}, expected ~${mainTopConnected}`,
)

// --- Docked: the banner alone carries it ------------------------------------
await page.click(DOCK)
await page.waitForSelector('.handle', { timeout: 5000 })
check((await page.$$('.rail')).length === 0, 'docked -- the rail (and its dot) are gone')

await dropConnection()
await page.waitForSelector('.banner-closed', { timeout: 15000 })
check(true, 'the banner still appears while docked -- it does not depend on the rail being mounted')
check(
  (await page.$('.rail-head-dot')) === null,
  'there is no rail-head dot to carry it docked -- nothing invented in its place',
)
// "Connection state is never the handle's job": the handle itself must
// not have grown an alarm styling of its own for this.
check(
  (await page.$eval('.handle', (el) => el.className)).indexOf('alarm') === -1,
  'the handle carries no connection styling of its own',
)

// The one control docked navigation has -- restore -- still works while
// disconnected, same claim as the full-rail case above, on the one
// interactive element that exists in this state.
await page.click('.handle')
await page.waitForSelector('.rail', { timeout: 5000 })
check(true, 'the handle still restores the rail while disconnected')

restoreConnection()
await page.waitForSelector('.banner-closed', { state: 'detached', timeout: 15000 })
check(true, 'and recovery still clears the banner from the docked path')

check(await responsive(page), 'main thread responsive throughout')

// The WS route intercept above logs no console error of its own -- this
// is left in place (not filtered) so a genuine one still fails the run.
check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
