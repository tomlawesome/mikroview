// SPDX-License-Identifier: AGPL-3.0-only
//
// Watchlist entry CRUD, observe/promote, and the match query (#243
// slice 4) against a real running mikroview. The unit and HTTP-level
// tests already cover the logic; the thing worth checking here is that
// a real session, real cookies, and the real CSRF header actually reach
// the mux at these new routes with the right admin gate -- the same
// reasoning live-ingest-token.mjs gives for its own equivalent
// scenario -- and, for the match query, that a real syslog line sent
// through the real ingest pipeline actually reaches internal/matchlog
// end to end (ingest -> Evaluator -> matchlog.Append -> the query API),
// not just that each layer works in isolation under a unit test.
//
// #407 folded the old /api/watchlist/entries surface into
// /api/definitions -- a watchlist entry is now an expectation
// definition, and everything below rides under a definition envelope
// rather than being the top-level response. A create/update/get still
// returns the entry, but at `.expectation` on the definition view, not
// as the whole body; a list is `body.definitions` filtered to
// `intent === 'expectation'`, not `body.entries`. The old routes are
// gone outright (404, no alias), so the unauthenticated-401 probes
// below point at the new surfaces too.

import { session, feedRaw, check, done } from './live-browser.mjs'

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
const created = await api('POST', '/api/definitions', {
  name: 'live SSH watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [22] },
})
check(created.status === 201, `an entry is created (${created.status})`)
check(!!created.body?.id, 'the created entry has a server-generated id')
check(created.body?.name === 'live SSH watch', 'the created entry has the requested name')

const id = created.body?.id

// List -- the created entry is actually there. The list is every
// definition this deployment holds (shipped detectors included), so
// this filters to intent === 'expectation' the way the frontend does
// (see api.ts's fetchWatchlistEntries) rather than assuming the whole
// list is watchlist entries.
const listed = await api('GET', '/api/definitions')
check(listed.status === 200, `entries list (${listed.status})`)
check(
  (listed.body?.definitions ?? []).some((d) => d.intent === 'expectation' && d.id === id),
  'the created entry appears in the list',
)

// Update -- renaming, and widening the port set. The expectation block
// is a full replace of the entry's operator-settable fields (server-
// side: expectationRequest.applyTo overwrites source/destIp/ports/
// invert/includeStructuralNoise together), not a per-field patch, so
// this only omits fields the entry never had set in the first place.
const updated = await api('PUT', `/api/definitions/${id}`, {
  name: 'live SSH watch (renamed)',
  expectation: { ports: [22, 2222] },
})
check(updated.status === 200, `an entry is updated (${updated.status})`)
check(updated.body?.name === 'live SSH watch (renamed)', 'the rename applied')
check((updated.body?.expectation?.ports ?? []).length === 2, 'the widened port set applied')

// Reject -- no ports on a non-inverted entry is a real validation error,
// not silently accepted.
const invalid = await api('POST', '/api/definitions', {
  name: 'broken',
  intent: 'expectation',
  kind: 'declarative',
  expectation: {},
})
check(invalid.status === 400, `an entry with no ports is refused (${invalid.status})`)

// Invert -- a new inverted entry starts Observing automatically.
const inverted = await api('POST', '/api/definitions', {
  name: 'live camera',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { invert: true, source: { mac: 'aa:bb:cc:dd:ee:ff' } },
})
check(inverted.status === 201, `an inverted entry is created (${inverted.status})`)
check(inverted.body?.expectation?.observing === true, 'a new inverted entry starts observing')

// Observe/promote: a real syslog line, matching the inverted entry's
// device, sent through the real ingest pipeline -- not synthesised
// directly into the store.
const line =
  'A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, ' +
  'proto TCP (SYN), 192.168.1.50:51234->203.0.113.9:443, len 60'
feedRaw(line)

let observed
for (let i = 0; i < 40 && !observed; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const got = await api('GET', '/api/definitions')
  const d = (got.body?.definitions ?? []).find((d) => d.id === inverted.body?.id)
  if (d?.expectation?.observed?.length) observed = d.expectation.observed[0]
}
check(!!observed, 'a real ingested event, matching an inverted entry still observing, becomes an Observed candidate')
check(observed?.destIp === '203.0.113.9' && observed?.port === 443, `the observed candidate is the right destination (got ${JSON.stringify(observed)})`)

// Promoting it must move it into Permitted -- and the SAME traffic must
// then produce nothing further (no violation, since it is now expected).
const promoted = await api('POST', `/api/definitions/${inverted.body?.id}/promote`, {
  destinations: [{ destIp: observed?.destIp, port: observed?.port }],
})
check(promoted.status === 200, `promote (${promoted.status})`)
check((promoted.body?.expectation?.permitted ?? []).length === 1, 'the destination moved into Permitted')
check((promoted.body?.expectation?.observed ?? []).length === 0, 'the destination left the Observed review list')

// Leave observe mode -- from here, anything NOT permitted should fire as
// a real matchlog record.
const stoppedObserving = await api('POST', `/api/definitions/${inverted.body?.id}/observing`, { observing: false })
// Entry.Observing has `omitempty` -- when false the key is absent from
// the JSON entirely, not present-and-false, so this checks falsy rather
// than strict equality (the violation check below is the real proof
// this took effect server-side: it can only fire once Observing is
// genuinely false).
check(stoppedObserving.status === 200 && !stoppedObserving.body?.expectation?.observing, `observe mode was turned off (${stoppedObserving.status})`)

const violatingLine =
  'A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new src-mac aa:bb:cc:dd:ee:ff, ' +
  'proto TCP (SYN), 192.168.1.50:51235->198.51.100.7:9999, len 60'
feedRaw(violatingLine)

let matches
for (let i = 0; i < 40 && !matches?.length; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const got = await api('GET', '/api/matches?mac=aa:bb:cc:dd:ee:ff')
  if (got.status === 200) matches = got.body?.matches
}
check(!!matches?.length, 'a violation (not observing, not permitted) is recorded to the match log')
check(
  matches?.some((m) => m.tuple?.destIp === '198.51.100.7' && m.tuple?.port === 9999),
  `the recorded match is the right violation (got ${JSON.stringify(matches)})`,
)

// Delete -- both entries, confirmed gone from a fresh list.
await api('DELETE', `/api/definitions/${id}`)
await api('DELETE', `/api/definitions/${inverted.body?.id}`)
const afterDelete = await api('GET', '/api/definitions')
check(
  !(afterDelete.body?.definitions ?? []).some((d) => d.id === id || d.id === inverted.body?.id),
  'both deleted entries are gone from the list',
)

// The load-bearing assertion: no session, no cookies -- exactly what an
// unauthenticated request looks like. Plain fetch outside the browser's
// authenticated context.
const anonRes = await fetch(`${URL_BASE}/api/definitions`)
check(anonRes.status === 401, `an unauthenticated request is refused (${anonRes.status})`)

const anonMatches = await fetch(`${URL_BASE}/api/matches?mac=aa:bb:cc:dd:ee:ff`)
check(anonMatches.status === 401, `an unauthenticated match query is refused (${anonMatches.status})`)

done()
