// SPDX-License-Identifier: AGPL-3.0-only
//
// #407: the one definitions surface, driven against a real instance --
// list, param override, reset, clone, replay, and the two refusals that
// are part of the contract rather than incidental errors.
//
// Why a live scenario rather than only handler tests. Every endpoint
// here crosses the same joint: the API layer decodes a request, the
// definitions store validates and persists it, and the engine rebuilds
// and re-registers what it evaluates -- on a real binary, with a real
// document on disk, with events actually arriving. The unit tests cover
// each of those in isolation and none of them together, and the two
// defects this class of change actually produces (a definition that
// stores fine and then cannot be built, and an edit that persists but
// never reaches the engine) are both invisible from either end alone.
//
// It also covers the thing the removal itself risks: /api/detectors and
// /api/watchlist/entries are gone with no alias and no friendlier-error
// stub, so a stale caller must get a plain 404. A shim quietly surviving
// the removal is exactly what AGENTS.md's "removals are wholesale" rule
// exists to prevent, and it would not be visible in any Go test that
// only asks the routes that do exist.
//
// Shares one instance with every other scenario in this directory, so it
// creates its own definitions and cleans them up, and never assumes the
// store is empty.

import { session, check, done, feedPortScan } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  const text = await res.text()
  let parsed = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch {
    parsed = null
  }
  return { status: res.status(), body: parsed, text }
}

// --- the list, and what an envelope has to carry -----------------------

const list = await api('GET', '/api/definitions')
check(list.status === 200, `GET /api/definitions answers 200 (${list.status})`)

const definitions = list.body?.definitions ?? []
check(definitions.length > 0, `the list is non-empty (${definitions.length} definition(s))`)

const portScan = definitions.find((d) => d.id === 'port_scan')
check(portScan !== undefined, 'the shipped port_scan definition is listed')
check(portScan?.intent === 'detection', `port_scan's intent is detection (${portScan?.intent})`)
check(portScan?.provenance?.origin === 'shipped', `port_scan's provenance is shipped (${portScan?.provenance?.origin})`)
check(
  Array.isArray(portScan?.paramSchema) && portScan.paramSchema.some((p) => p.name === 'threshold'),
  'port_scan declares its own param schema, so the UI renders controls from the server rather than re-listing every knob in TypeScript',
)
check(
  portScan?.replay?.known === true,
  `port_scan resolves its replayability rather than leaving it unanswered (${JSON.stringify(portScan?.replay)})`,
)

// The five definitions #405 gave an envelope to for the first time. They
// were always-on passes with no toggle at all before, and the legacy
// detector-settings endpoint deliberately never listed them -- so their
// presence here is the operator-visible half of that port landing.
for (const id of ['unexpected_mail_sender', 'stale_rule', 'known_bad_ip', 'netclass', 'reputation']) {
  check(
    definitions.some((d) => d.id === id),
    `the newly-toggleable ${id} definition is listed (it had no toggle at all before the engine port)`,
  )
}

const schema = await api('GET', '/api/definitions/schema')
check(schema.status === 200, `GET /api/definitions/schema answers 200 (${schema.status})`)
check(
  Array.isArray(schema.body?.schemas?.port_scan),
  'the schema endpoint carries port_scan’s param declarations',
)

// --- the old routes are gone, not aliased ------------------------------

for (const [method, path_] of [
  ['GET', '/api/detectors'],
  ['PUT', '/api/detectors/port_scan'],
  ['GET', '/api/watchlist/entries'],
  ['POST', '/api/watchlist/entries'],
  ['GET', '/api/watchlist/matches?ip=203.0.113.1'],
]) {
  const gone = await api(method, path_)
  check(gone.status === 404, `${method} ${path_} is gone, not aliased (${gone.status})`)
}

// --- param override, validated against the schema ----------------------

const originalThreshold = portScan?.params?.threshold
const badParams = await api('PUT', '/api/definitions/port_scan', {
  params: { ...portScan.params, threshold: -5 },
})
check(badParams.status === 400, `an out-of-bounds param value is refused (${badParams.status})`)

const afterBad = await api('GET', '/api/definitions/port_scan')
check(
  afterBad.body?.params?.threshold === originalThreshold,
  `the refused value was never stored (threshold is still ${afterBad.body?.params?.threshold}, was ${originalThreshold})`,
)

const tuned = await api('PUT', '/api/definitions/port_scan', {
  params: { ...portScan.params, threshold: 9 },
})
check(tuned.status === 200, `a valid param override is accepted (${tuned.status})`)
check(tuned.body?.params?.threshold === 9, `the override is what comes back (${tuned.body?.params?.threshold})`)
check(
  tuned.body?.provenance?.origin === 'shipped',
  'an edited shipped definition stays shipped -- editing one never turns it into a custom definition',
)
check(
  tuned.body?.distance?.threshold?.shipped === originalThreshold,
  `the response says how far the definition now is from stock (${JSON.stringify(tuned.body?.distance?.threshold)})`,
)

// The edit is live immediately, not on the next restart: a threshold of
// 9 means a source touching 10 distinct ports now raises a flag it would
// not have raised at the shipped threshold of 15.
feedPortScan(12, '198.51.100.77')
const flagged = await waitForPortScanFlag('198.51.100.77')
check(flagged, 'a lowered threshold takes effect on the very next events, with no restart')

const reset = await api('POST', '/api/definitions/port_scan/reset')
check(reset.status === 200, `reset answers 200 (${reset.status})`)
check(
  reset.body?.params?.threshold === originalThreshold,
  `reset puts the shipped default back (${reset.body?.params?.threshold}, want ${originalThreshold})`,
)
check(
  Object.keys(reset.body?.distance ?? {}).length === 0,
  `after a reset nothing is overridden (${JSON.stringify(reset.body?.distance)})`,
)

// --- the two refusals that are contract, not accident ------------------

const shippedDelete = await api('DELETE', '/api/definitions/port_scan')
check(shippedDelete.status === 409, `a shipped definition cannot be deleted (${shippedDelete.status})`)
const stillThere = await api('GET', '/api/definitions/port_scan')
check(stillThere.status === 200, 'the refused delete left the shipped definition in place')

const programmatic = await api('POST', '/api/definitions', {
  name: 'smuggled logic',
  intent: 'expectation',
  kind: 'programmatic',
  expectation: { ports: [1234] },
})
check(
  programmatic.status === 400,
  `a custom programmatic definition cannot be created -- programmatic logic is Go in this binary, not data (${programmatic.status})`,
)

const shippedClone = await api('POST', '/api/definitions/port_scan/clone', { name: 'port scan copy' })
check(
  shippedClone.status === 400,
  `a shipped detection definition refuses to be cloned rather than producing a copy that evaluates nothing (${shippedClone.status})`,
)

// --- custom definitions: create, clone, replay, delete ------------------

const created = await api('POST', '/api/definitions', {
  name: 'live-definitions watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [7411] },
})
check(created.status === 201, `a custom expectation is created (${created.status})`)
const createdID = created.body?.id
check(typeof createdID === 'string' && createdID.length > 0, 'it is given a server-generated id')
check(
  created.body?.provenance?.origin === 'custom',
  `an operator-authored definition is custom (${created.body?.provenance?.origin})`,
)
check(created.body?.expectation?.ports?.[0] === 7411, 'its operator-facing entry comes back with the response')

const cloned = await api('POST', `/api/definitions/${encodeURIComponent(createdID)}/clone`, {})
check(cloned.status === 201, `an expectation clones (${cloned.status})`)
check(cloned.body?.id !== createdID, 'the clone has its own identity, never the original’s id')
check(
  cloned.body?.expectation?.ports?.[0] === 7411,
  `the clone carries the original’s matching data (${JSON.stringify(cloned.body?.expectation?.ports)})`,
)

const replay = await api('POST', `/api/definitions/${encodeURIComponent(createdID)}/replay`, {})
check(replay.status === 200, `replay answers over the stored corpus (${replay.status})`)
const receipt = replay.body?.receipt
const decline = replay.body?.decline
check(
  Boolean(receipt) !== Boolean(decline),
  `a replay answers with exactly one of a receipt or a decline (${JSON.stringify(replay.body)})`,
)
if (receipt) {
  check(
    typeof receipt.window?.start === 'string' && typeof receipt.window?.end === 'string' && typeof receipt.window?.eventCount === 'number',
    `the receipt states the window it actually covered (${JSON.stringify(receipt.window)})`,
  )
} else {
  check(
    typeof decline?.reason === 'string' && decline.reason.length > 0,
    `a declined replay says why rather than reporting a misleading zero (${JSON.stringify(decline)})`,
  )
}

// An inverted expectation declines permanently, with its reason: its
// judgement comes from an observation period measured in days, and the
// corpus is an in-memory ring measured in minutes.
const inverted = await api('POST', '/api/definitions', {
  name: 'live-definitions inverted',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { invert: true, source: { mac: 'aa:bb:cc:00:74:11' } },
})
check(inverted.status === 201, `an inverted expectation is created (${inverted.status})`)
check(inverted.body?.replay?.known === true, 'its replayability is resolved')
check(
  inverted.body?.replay?.capable === false && (inverted.body?.replay?.reason ?? '').length > 0,
  `an inverted expectation declares itself non-replayable with a stated reason (${JSON.stringify(inverted.body?.replay)})`,
)

for (const id of [createdID, cloned.body?.id, inverted.body?.id]) {
  if (!id) continue
  const removed = await api('DELETE', `/api/definitions/${encodeURIComponent(id)}`)
  check(removed.status === 200, `the custom definition ${id} is deleted (${removed.status})`)
}

const finalList = await api('GET', '/api/definitions')
const leftovers = (finalList.body?.definitions ?? []).filter((d) => (d.name ?? '').startsWith('live-definitions'))
check(leftovers.length === 0, `this scenario left nothing behind (${leftovers.length} leftover(s))`)

done()

// waitForPortScanFlag polls the flags API rather than the UI: this is
// about whether the *engine* picked the edit up, and routing the answer
// through a page render would make a rendering failure look like an
// evaluation one. Named in the diagnostic either way, so a failure says
// what was actually being waited for.
async function waitForPortScanFlag(target, { timeoutMs = 20000 } = {}) {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    const flags = await api('GET', '/api/flags')
    if ((flags.body?.flags ?? []).some((f) => f.type === 'port_scan' && f.target === target)) return true
    await new Promise((r) => setTimeout(r, 500))
  }
  return false
}
