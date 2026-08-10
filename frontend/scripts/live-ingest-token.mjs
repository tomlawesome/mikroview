// SPDX-License-Identifier: AGPL-3.0-only
//
// Ingest tokens (#186 step 1) against a real running mikroview.
//
// The unit tests already assert kind separation, so the thing worth
// checking here is the part they cannot: that a token minted through the
// real HTTP API, carried in a real Authorization header, to the real
// mux, is refused. Every layer between "the store said no" and "the
// request came back 401" is exercised only by running it -- and this
// project's history is that those layers are where the defects live.

import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

// Mint both kinds through the API the operator would use, cookies and
// CSRF header included, rather than reaching into the store.
async function createToken(body) {
  const res = await page.request.post(`${URL_BASE}/api/tokens`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

const api = await createToken({ name: 'live-api', kind: 'api' })
check(api.status === 201, `a read-only API token is issued (${api.status})`)

const ingest = await createToken({ name: 'live-ingest', kind: 'ingest', device: 'live-router' })
check(ingest.status === 201, `an ingest token is issued when scoped to a device (${ingest.status})`)
check(ingest.body?.device === 'live-router', 'the issued ingest token reports its device scope')
check(ingest.body?.kind === 'ingest', 'the issued ingest token reports its kind')

const unscoped = await createToken({ name: 'live-unscoped', kind: 'ingest' })
check(unscoped.status === 400, `an unscoped ingest token is refused (${unscoped.status})`)

// The load-bearing assertion. Plain fetch, no cookies -- exactly what a
// router-side script sends.
async function getWithToken(path, token) {
  const res = await fetch(`${URL_BASE}${path}`, { headers: { Authorization: `Bearer ${token}` } })
  return res.status
}

// 404, not 401: an ingest token authenticates successfully (it's a
// valid token, just not this kind of route) and dispatches to its own
// disjoint mux (see internal/api/auth.go's ingestRoutes), which simply
// doesn't register these paths -- the same "valid token, wrong mux"
// shape a read-only token gets hitting a write route. Was 401 before
// #186 step 3 gave ingest tokens a mux of their own to dispatch to.
const readOnlyPaths = ['/api/events', '/api/flags', '/api/stats', '/api/devices']
for (const path of readOnlyPaths) {
  check(
    (await getWithToken(path, ingest.body.value)) === 404,
    `an ingest token is refused at ${path} (404, wrong mux)`,
  )
  // Asserted alongside, so a wholesale-broken bearer path cannot pass
  // this scenario by refusing everything.
  check(
    (await getWithToken(path, api.body.value)) === 200,
    `a read-only API token still works at ${path}`,
  )
}

// The reverse direction: a read-only API token must not reach the
// ingest endpoint either -- same disjoint-mux guarantee, pointed the
// other way. 404 again, not 401, for the identical reason.
check(
  (await getWithToken('/api/ingest/routeros', api.body.value)) === 404,
  'a read-only API token is refused at /api/ingest/routeros (404, wrong mux)',
)

// Revocation is what an operator reaches for when a router is
// compromised, so it has to work on the new kind too.
const revoked = await page.request.delete(`${URL_BASE}/api/tokens/${ingest.body.id}`, {
  headers: { 'X-Requested-With': 'mikroview' },
})
check(revoked.status() === 200, `an ingest token can be revoked (${revoked.status()})`)

done()
