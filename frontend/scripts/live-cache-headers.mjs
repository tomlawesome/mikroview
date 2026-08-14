// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #347: what the real server actually sends on the static files,
// checked against the running binary rather than the middleware alone.
//
// The unit test in main_test.go proves staticCacheHeaders returns the
// right header for a given path. It cannot prove the wrapper is still
// wired into the handler that serves the UI -- and "correct code, not
// reached" is exactly the failure this issue is about: an upgraded
// server that keeps handing out the previous release's app shell while
// every server-side signal reports the new version.
//
// So this asks the running instance, over HTTP, for one file of each
// shape and reads the header off the response.

import { session, check, done } from './live-browser.mjs'

const { page, consoleErrors } = await session()

// Fetched through the page so the request goes to the real listener the
// browser is already talking to, TLS and all, rather than a second
// client with its own trust settings.
const headerFor = (path) =>
  page.evaluate(async (p) => {
    const res = await fetch(p, { cache: 'no-store' })
    return { status: res.status, cacheControl: res.headers.get('cache-control') }
  }, path)

// --- index.html: the file a stale copy of shows the wrong UI ----------
const index = await headerFor('/')
check(index.status === 200, `the app shell is served (${index.status})`)
check(
  index.cacheControl === 'no-cache',
  `index.html is revalidated, not heuristically cached (${index.cacheControl})`,
)

// --- sw.js: the file that decides whether an upgrade is noticed -------
const sw = await headerFor('/sw.js')
check(sw.status === 200, `the service worker is served (${sw.status})`)
check(
  sw.cacheControl === 'no-cache',
  `sw.js is revalidated -- without this a browser may reuse it for 24h and never see the new build (${sw.cacheControl})`,
)

// --- A hashed asset: safe to keep, and named from the real build ------
// Read out of index.html rather than hardcoded: the hash changes with
// every content change, so a literal here would rot immediately and
// then pass against a 404.
const assetPath = await page.evaluate(async () => {
  const html = await (await fetch('/', { cache: 'no-store' })).text()
  return html.match(/\/assets\/[^"']+\.js/)?.[0] ?? null
})
check(assetPath !== null, `index.html references a hashed asset (${assetPath})`)

if (assetPath) {
  const asset = await headerFor(assetPath)
  check(asset.status === 200, `the hashed asset is served (${asset.status})`)
  check(
    asset.cacheControl === 'public, max-age=31536000, immutable',
    `a content-hashed asset may be kept indefinitely (${asset.cacheControl})`,
  )
}

// --- The API must not have been swept in ------------------------------
// staticCacheHeaders wraps the file server only. If it ever ends up
// wrapping the mux, live data starts being served with a year-long
// immutable header, which is a far worse bug than the one being fixed.
const api = await headerFor('/api/healthz')
check(api.status === 200, `the API answers (${api.status})`)
check(
  api.cacheControl === null || !api.cacheControl.includes('immutable'),
  `/api/* is not marked cacheable (${api.cacheControl})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
