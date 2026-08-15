// SPDX-License-Identifier: AGPL-3.0-only
//
// A typed /api URL must reach the server, service worker or not. The
// worker's navigateFallback answers navigations from the cached app
// shell; before the denylist, visiting /api/stats in the address bar
// got the frontend instead of JSON -- reported by the owner against a
// real deployment, 2026-08-15. The UI's own fetch() calls never hit
// this path, which is exactly why no other scenario could catch it.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session()

// Wait for the worker to activate, then reload so it controls the page.
// If it never activates, that is its own finding -- fail loudly rather
// than passing vacuously on an uncontrolled page.
const activated = await page.evaluate(async () => {
  if (!('serviceWorker' in navigator)) return 'unsupported'
  const reg = await Promise.race([
    navigator.serviceWorker.ready,
    new Promise((r) => setTimeout(() => r(null), 15000)),
  ])
  return reg ? 'active' : 'timeout'
})
check(activated === 'active', `service worker activated (${activated})`)

await page.reload({ waitUntil: 'load' })
const controlled = await page.evaluate(() => navigator.serviceWorker.controller !== null)
check(controlled, 'the worker controls the page after reload')

// The real assertion: a NAVIGATION to /api/healthz -- the address-bar
// case -- must return the server's JSON, not the cached shell.
await page.goto(new URL('/api/healthz', page.url()).href, { waitUntil: 'load' })
const body = await page.evaluate(() => document.body.innerText)
let parsed = null
try {
  parsed = JSON.parse(body)
} catch {
  /* falls through to the check below with parsed null */
}
check(
  parsed !== null && parsed.status === 'ok',
  `navigating to /api/healthz returns the API's JSON (got: ${body.slice(0, 80).replace(/\n/g, ' ')})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.slice(0, 2).join(' | ') || 'none'})`)

done()
