// SPDX-License-Identifier: AGPL-3.0-only
//
// #375: an open /api/ws used to survive logout -- authenticated once at
// the HTTP upgrade and never re-checked again, so a socket kept streaming
// live firewall events to a browser whose session had just been revoked.
// The fix (internal/api/ws.go) re-validates the session on every
// wsPingInterval tick and sends a real close frame once it no longer
// checks out.
//
// A single signed-in tab is not the interesting case: App.svelte's own
// polling already notices a 401 within STATS_REFRESH_MS and voluntarily
// calls liveSocket.disconnect() (see ws.ts), so a normal tab looks fixed
// whether or not the server-side revalidation exists at all. What the
// server-side check actually defends is a socket nobody is voluntarily
// tearing down -- the stolen-cookie case from the issue. This scenario
// opens a *raw* WebSocket by hand (bypassing liveSocket.ts and every bit
// of App.svelte's auth-state cleanup entirely) to stand in for exactly
// that: a live-tail connection with nothing else watching the session,
// which only the server closing it can stop.
//
// Runs for close to a real wsPingInterval (30s): shrinking that for the
// live check would mean adding a config knob whose only job is making a
// test faster, which is out of scope for this fix. The wait here is the
// same bound an operator actually gets.

import { session, feedSyslog, check, done, openAccountMenu } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// No waitForEvents here -- this scenario cares about events delivered to
// the raw socket *after* it connects, not about a pre-existing snapshot.
const { page } = await session()

// A raw WebSocket, deliberately not liveSocket -- see the file doc
// comment above. Counts events as they arrive and records how/when it
// closes, all inside the page so it experiences exactly what a real
// browser socket would.
await page.evaluate(() => {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const ws = new WebSocket(`${proto}://${location.host}/api/ws`)
  window.__rawWs = ws
  window.__rawWsEvents = 0
  window.__rawWsClosed = false
  window.__rawWsCloseWasClean = null
  ws.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data)
      if (msg.type === 'events' && Array.isArray(msg.events)) {
        window.__rawWsEvents += msg.events.length
      }
    } catch {
      // ignore -- a ping/pong control frame never reaches onmessage
    }
  }
  ws.onclose = (ev) => {
    window.__rawWsClosed = true
    window.__rawWsCloseWasClean = ev.wasClean
  }
})

await page.waitForFunction(() => window.__rawWs.readyState === WebSocket.OPEN, null, { timeout: 5000 })

feedSyslog(20, 'ws-revocation-rule')
await page.waitForFunction(() => window.__rawWsEvents > 0, null, { timeout: 10000 })
const beforeLogout = await page.evaluate(() => window.__rawWsEvents)
check(beforeLogout > 0, `the raw socket received ${beforeLogout} events before logout`)

// Log out from "another tab" -- a separate browser context carrying the
// exact same session cookie the raw socket above is using (session()'s
// page owns a context that Playwright refuses a second page.newPage()
// on directly, so the cookie is copied across rather than sharing the
// context object itself; the server sees the identical session either
// way, which is what matters here). Its own liveSocket.disconnect() and
// the account chip's Sign out row are exercised too, since that is the
// interface an operator actually uses (mirroring live-change-password.mjs).
const cookies = await page.context().cookies()
const otherContext = await page.context().browser().newContext({ ignoreHTTPSErrors: true })
await otherContext.addCookies(cookies)
const other = await otherContext.newPage()
await other.goto(URL_BASE, { waitUntil: 'networkidle' })
await other.waitForSelector('#main-content', { timeout: 15000 })
await openAccountMenu(other)
check(await other.isVisible('.account .menu button.row:has-text("Sign out")'), 'the account menu offers Sign out')
await other.click('.account .menu button.row:has-text("Sign out")')
await other.waitForSelector('input[autocomplete="username"]', { timeout: 10000 })
await otherContext.close()

// The bound is one wsPingInterval (30s in production) plus the write --
// see the file doc comment for why this isn't shrunk for the check.
await page.waitForFunction(() => window.__rawWsClosed === true, null, { timeout: 40000 }).catch(() => {})
const closed = await page.evaluate(() => ({
  closed: window.__rawWsClosed,
  wasClean: window.__rawWsCloseWasClean,
  readyState: window.__rawWs.readyState,
}))
check(closed.closed, `the revoked session's raw socket closed (readyState=${closed.readyState})`)
check(closed.wasClean === true, 'the socket closed with a clean WebSocket close frame, not a dropped connection')

// Feeding more events after the close must not move the counter -- the
// socket is gone, not merely quiet.
const afterClose = await page.evaluate(() => window.__rawWsEvents)
feedSyslog(20, 'ws-revocation-rule')
await page.waitForTimeout(1000)
const stillAfterClose = await page.evaluate(() => window.__rawWsEvents)
check(stillAfterClose === afterClose, 'no further events reach the socket once it is closed')

// The original tab's own polling should have noticed the 401 by now too,
// landing back on the login screen rather than a zombie live view.
let landedOnLogin = true
try {
  await page.waitForSelector('input[autocomplete="username"]', { timeout: 15000 })
} catch {
  landedOnLogin = false
}
check(landedOnLogin, 'the original tab lands back on the login screen')

done()
