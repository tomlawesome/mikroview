// SPDX-License-Identifier: AGPL-3.0-only
//
// #274 item 1: telling an operator that a watchlist entry can never
// match, because nothing a router pushed can feed it.
//
// The property worth protecting here is *silence*, not the warning.
// #274 rejected an earlier sketch of this feature for guessing, on the
// grounds that a false "this can never fire" hides a working entry and a
// false "this looks fine" is worse than saying nothing. So most of this
// scenario checks that nothing is claimed -- which is also the state
// every deployment that never set up the optional router push is in.
//
// Runs against a real instance with real pushed tables, not a unit
// fixture: the coverage answer crosses the ingest decoder, the router
// state store and the API, and the unit tests cover none of that
// joinery.
//
// Every scenario in this directory shares one instance, and two that
// sort earlier (live-router-lookup, live-suggestions) push filter tables
// of their own. So there is no clean slate here, and the "nothing has
// been pushed" case cannot be tested from this seat -- it lives in
// TestCoverageSaysNothingWithoutPushedRules instead. Assuming otherwise
// is what the first version of this file did, and it failed for that
// reason rather than for a defect.
//
// #407 moved the entry surface onto /api/definitions, and the coverage
// answer moved with it: rather than a `body.coverage[id]` map returned
// alongside the entries, coverage now rides per-definition as
// `definition.coverage` on the definitions list. coverageFor below
// re-reads the list and picks the one definition out, rather than
// indexing a separate map.

import { session, check, done, feedSyslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

async function coverageFor(id) {
  const got = await api('GET', '/api/definitions')
  const d = (got.body?.definitions ?? []).find((d) => d.id === id)
  return d?.coverage
}

const entry = await api('POST', '/api/definitions', {
  name: 'coverage ssh',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [22] },
})
check(entry.status === 201, `an entry is created (${entry.status})`)
const id = entry.body?.id

// The tables the earlier scenarios pushed carry no `log` field at all,
// so every rule in them is non-logging -- which is itself the answer
// this starts from, and a fair reproduction of a real deployment whose
// rules do not log.
check(
  (await coverageFor(id)) === 'no-logging',
  'starting state: tables pushed by earlier scenarios have no logging rules, and mikroview says so',
)

feedSyslog(3, 'coverage-probe')
const device = (await api('GET', '/api/devices')).body?.devices?.[0]?.id
check(!!device, `the instance reports a device (${device})`)

// --- Rules that all log, and cover the entry ----------------------------

const token = await api('POST', '/api/tokens', { name: 'coverage', kind: 'ingest', device })
check(token.status === 201, `an ingest token is issued (${token.status})`)

async function push(records) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token.body.value}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'filter-rule', page: 1, pages: 1, records }),
  })
  return res.status
}

check(
  (await push([{ ordinal: 0, chain: 'forward', action: 'drop', log: true, logPrefix: 'D|any|' }])) === 200,
  'a filter table with one logging rule is accepted',
)
check(
  (await coverageFor(id)) === 'covered',
  'a logging rule with no port condition covers the entry -- an unscoped rule matches every port',
)

// --- Rules exist, none of them log --------------------------------------

check(
  (await push([
    { ordinal: 0, chain: 'forward', action: 'drop' },
    { ordinal: 1, chain: 'input', action: 'accept' },
  ])) === 200,
  'a filter table with no logging rules is accepted',
)
check(
  (await coverageFor(id)) === 'no-logging',
  'when no rule anywhere logs, mikroview says so -- nothing can feed the watchlist or the live view',
)

// --- Rules log, but none covers this entry ------------------------------

check(
  (await push([
    { ordinal: 0, chain: 'forward', action: 'drop', log: true, dstPort: '80' },
    { ordinal: 1, chain: 'forward', action: 'drop', log: true, dstPort: '443' },
  ])) === 200,
  'a filter table scoped to other ports is accepted',
)
check(
  (await coverageFor(id)) === 'out-of-scope',
  'when every logging rule scopes to ports this entry does not watch, mikroview says so',
)

// --- Anything unreadable goes back to silence ---------------------------

// An entry that actually scopes by destination, because a rule's
// dst-address is irrelevant to one that does not -- a rule with an
// unreadable address and no port condition genuinely covers an
// unscoped entry, which is the right answer and not a silence bug.
// The first version of this asserted otherwise and was wrong.
const scoped = await api('POST', '/api/definitions', {
  name: 'coverage scoped',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [22], destIp: '10.1.2.3' },
})
check(scoped.status === 201, `a destination-scoped entry is created (${scoped.status})`)

check(
  (await push([
    { ordinal: 0, chain: 'forward', action: 'drop', log: true, dstAddress: '192.168.0.0/16' },
    { ordinal: 1, chain: 'forward', action: 'drop', log: true, dstAddress: 'mgmt' },
  ])) === 200,
  'a filter table with an address-list name is accepted',
)
check(
  (await coverageFor(scoped.body?.id)) === 'unknown',
  'a condition mikroview cannot read makes it stop claiming -- the unreadable rule might have been the covering one',
)

// And with the unreadable rule gone, the same entry gets a definite
// answer, so the silence above is attributable to that rule rather than
// to the check simply never speaking.
check(
  (await push([
    { ordinal: 0, chain: 'forward', action: 'drop', log: true, dstAddress: '192.168.0.0/16' },
  ])) === 200,
  'the same table without the unreadable rule is accepted',
)
check(
  (await coverageFor(scoped.body?.id)) === 'out-of-scope',
  'with only readable rules, the destination-scoped entry gets a definite answer',
)
await api('DELETE', `/api/definitions/${scoped.body?.id}`)

// --- The warning is what the operator actually sees ---------------------

check(
  (await push([
    { ordinal: 0, chain: 'forward', action: 'drop' },
  ])) === 200,
  'back to a non-logging table for the UI check',
)

async function openMenuView(label) {
  await page.click('.nav-menu .trigger')
  await page.click(`.nav-menu button:has-text("${label}")`)
}

await page.reload({ waitUntil: 'networkidle' })
await openMenuView('Watchlist')
await page.waitForSelector('.card', { timeout: 15000 })

check(
  await page.isVisible('.coverage-warning'),
  'the entry carries a visible warning that nothing can match it',
)
check(
  await page.isVisible('.coverage-warning:has-text("logging turned on")'),
  'the warning says what to actually do about it, not just that something is wrong',
)

await api('DELETE', `/api/definitions/${id}`)
done()
