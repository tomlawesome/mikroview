// SPDX-License-Identifier: AGPL-3.0-only
//
// Watchlist entry CRUD (#243 slice 4) against a real running mikroview.
// The unit and HTTP-level tests already cover the logic; the thing worth
// checking here is that a real session, real cookies, and the real CSRF
// header actually reach the mux at these new routes with the right
// admin gate -- the same reasoning live-ingest-token.mjs gives for its
// own equivalent scenario.

import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// Create -- non-inverted, generalising today's Control Ports shape.
const created = await api('POST', '/api/watchlist/entries', { name: 'live SSH watch', ports: [22] })
check(created.status === 201, `an entry is created (${created.status})`)
check(!!created.body?.id, 'the created entry has a server-generated id')
check(created.body?.name === 'live SSH watch', 'the created entry has the requested name')

const id = created.body?.id

// List -- the created entry is actually there.
const listed = await api('GET', '/api/watchlist/entries')
check(listed.status === 200, `entries list (${listed.status})`)
check((listed.body?.entries ?? []).some((e) => e.id === id), 'the created entry appears in the list')

// Update -- renaming, and widening the port set.
const updated = await api('PUT', `/api/watchlist/entries/${id}`, { name: 'live SSH watch (renamed)', ports: [22, 2222] })
check(updated.status === 200, `an entry is updated (${updated.status})`)
check(updated.body?.name === 'live SSH watch (renamed)', 'the rename applied')
check((updated.body?.ports ?? []).length === 2, 'the widened port set applied')

// Reject -- no ports on a non-inverted entry is a real validation error,
// not silently accepted.
const invalid = await api('POST', '/api/watchlist/entries', { name: 'broken' })
check(invalid.status === 400, `an entry with no ports is refused (${invalid.status})`)

// Invert -- a new inverted entry starts Observing automatically.
const inverted = await api('POST', '/api/watchlist/entries', {
  name: 'live camera',
  invert: true,
  source: { mac: 'aa:bb:cc:dd:ee:ff' },
})
check(inverted.status === 201, `an inverted entry is created (${inverted.status})`)
check(inverted.body?.observing === true, 'a new inverted entry starts observing')

// Delete -- both the entry and the inverted one, confirmed gone from a
// fresh list.
await api('DELETE', `/api/watchlist/entries/${id}`)
await api('DELETE', `/api/watchlist/entries/${inverted.body?.id}`)
const afterDelete = await api('GET', '/api/watchlist/entries')
check(
  !(afterDelete.body?.entries ?? []).some((e) => e.id === id || e.id === inverted.body?.id),
  'both deleted entries are gone from the list',
)

// The load-bearing assertion: no session, no cookies -- exactly what an
// unauthenticated request looks like. Plain fetch outside the browser's
// authenticated context.
const anonRes = await fetch(`${URL_BASE}/api/watchlist/entries`)
check(anonRes.status === 401, `an unauthenticated request is refused (${anonRes.status})`)

done()
