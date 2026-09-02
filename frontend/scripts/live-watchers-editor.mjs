// SPDX-License-Identifier: AGPL-3.0-only
//
// #787: the watchers station as a full editor, driven in a real browser
// against a real instance.
//
// Why this cannot be a component test. Everything here crosses the joint
// between a rendered control and the definition the engine is actually
// evaluating: the panel builds its fields from GET
// /api/definitions/schema, writes them back through PUT
// /api/definitions/{id}, and the server validates them against the same
// schema before the engine rebuilds what it evaluates. A mocked store
// agrees with whatever the component sends it, so the two failures this
// class of change really produces -- a control that renders a value the
// server will not accept back, and an edit that the UI reports as saved
// but that never reached the store -- are both invisible from either end
// alone. Each assertion below therefore reads the *server's* answer after
// driving the *browser's* control, never the browser's own optimism.
//
// Shares one instance with every other scenario in this directory, so it
// puts port_scan back to stock at the end rather than leaving a lowered
// threshold behind for whatever runs next.

import { session, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

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

// --- open the bench -----------------------------------------------------

await goTo(page, 'Settings')
await page.click('.olink:has-text("tune")')
await page.waitForSelector('.bench .row')

const row = page.locator('.bench li.row:has(.id:text-is("port_scan"))')
check((await row.count()) === 1, 'the port_scan row is on the bench')

// The schema endpoint is what the panel's fields are built from. Read it
// here too, so the bounds this scenario types within are the server's own
// rather than numbers copied out of the detector's Go source.
const schema = await api('GET', '/api/definitions/schema')
check(schema.status === 200, `GET /api/definitions/schema answers 200 (${schema.status})`)
const portScanSchema = schema.body?.schemas?.port_scan ?? []
const thresholdSchema = portScanSchema.find((p) => p.name === 'threshold')
check(
  thresholdSchema !== undefined,
  `port_scan declares a threshold param (${JSON.stringify(portScanSchema.map((p) => p.name))})`,
)

const before = await api('GET', '/api/definitions/port_scan')
check(before.status === 200, `GET /api/definitions/port_scan answers 200 (${before.status})`)
const shippedThreshold = before.body?.provenance?.shippedParams?.threshold ?? before.body?.params?.threshold
check(typeof shippedThreshold === 'number', `the row starts from a known threshold (${shippedThreshold})`)

// --- the row expands in place ------------------------------------------

await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })
check(true, 'the row expands downward into its editing panel')

check(
  (await page.locator('.bench .panel').count()) === 1,
  'exactly one panel is open -- the bench stays a bench',
)

const thresholdField = row.locator('.panel input[type="number"]').first()
check(
  (await thresholdField.getAttribute('min')) === String(thresholdSchema.min ?? ''),
  `the threshold field carries the schema's own minimum (${await thresholdField.getAttribute('min')}, schema says ${thresholdSchema.min})`,
)
check(
  (await thresholdField.inputValue()) === String(shippedThreshold),
  `the field opens on the definition's current value (${await thresholdField.inputValue()}, server says ${shippedThreshold})`,
)

// --- edit a threshold ---------------------------------------------------

const tuned = Math.max(Number(thresholdSchema.min ?? 2), shippedThreshold - 6)
await thresholdField.fill(String(tuned))
await row.locator('.panel .save').click()
await row.locator('.panel').waitFor({ state: 'detached' })

const afterEdit = await api('GET', '/api/definitions/port_scan')
check(
  afterEdit.body?.params?.threshold === tuned,
  `the edit reached the definition the engine evaluates (server says ${afterEdit.body?.params?.threshold}, typed ${tuned})`,
)
check(
  afterEdit.body?.distance?.threshold?.shipped === shippedThreshold,
  `the server reports how far that leaves it from stock (${JSON.stringify(afterEdit.body?.distance?.threshold)})`,
)
check(
  (await row.locator('.tuned').count()) === 1,
  'the collapsed row says it is no longer on its shipped numbers',
)

// The window param is a duration, edited in whole seconds and written
// back as a Go duration string. This is the conversion most likely to
// break silently -- a seconds count sent as a bare number is refused by
// the server, and a refusal here would show as an unchanged value.
await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })
const windowField = row.locator('.panel input[type="number"]').nth(1)
const windowSeconds = await windowField.inputValue()
check(Number(windowSeconds) > 0, `the window param reads as a whole second count (${windowSeconds})`)
await windowField.fill(String(Number(windowSeconds) + 30))
await row.locator('.panel .save').click()
await row.locator('.panel').waitFor({ state: 'detached' })
const afterWindow = await api('GET', '/api/definitions/port_scan')
check(
  typeof afterWindow.body?.params?.window === 'string' && afterWindow.body.params.window.length > 0,
  `the window went back as the Go duration string the server stores (${JSON.stringify(afterWindow.body?.params?.window)})`,
)

// --- scope as chips -----------------------------------------------------

await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })

const hostBox = row.locator('.panel input[aria-label="add a host"]')
await hostBox.fill('198.51.100.42')
await hostBox.press('Enter')
check(
  (await row.locator('.panel .chip:has-text("198.51.100.42")').count()) === 1,
  'a typed host becomes its own removable chip',
)

const portBox = row.locator('.panel input[aria-label="add a port"]')
await portBox.fill('8000-8002')
await portBox.press('Enter')
check(
  (await row.locator('.panel .chip:has-text("8001")').count()) === 1,
  'a typed port range expands into one chip per port',
)

await portBox.fill('not-a-port')
await portBox.press('Enter')
check(
  (await row.locator('.panel .adderr').count()) > 0,
  'a value that is not a port is refused with a reason rather than silently dropped',
)

await row.locator('.panel .save').click()
await row.locator('.panel').waitFor({ state: 'detached' })
const afterScope = await api('GET', '/api/definitions/port_scan')
check(
  (afterScope.body?.scope?.hosts ?? []).includes('198.51.100.42'),
  `the chip reached the stored scope (${JSON.stringify(afterScope.body?.scope?.hosts)})`,
)
check(
  [8000, 8001, 8002].every((p) => (afterScope.body?.scope?.ports ?? []).includes(p)),
  `the expanded range reached the stored scope as numbers (${JSON.stringify(afterScope.body?.scope?.ports)})`,
)

// Removing a chip and saving has to actually shorten the axis. An
// assertion that only checked the chip vanished from the DOM would pass
// even if the write never happened.
await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })
await row.locator('.panel button[aria-label="remove host 198.51.100.42"]').click()
await row.locator('.panel .save').click()
await row.locator('.panel').waitFor({ state: 'detached' })
const afterDrop = await api('GET', '/api/definitions/port_scan')
check(
  !(afterDrop.body?.scope?.hosts ?? []).includes('198.51.100.42'),
  `removing a chip removes it from the stored scope (${JSON.stringify(afterDrop.body?.scope?.hosts)})`,
)

// --- reset it back to stock ---------------------------------------------

await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })
await row.locator('.panel button:has-text("Reset to stock")').click()
await page.waitForFunction(
  () => {
    const li = [...document.querySelectorAll('.bench li.row')].find(
      (r) => r.querySelector('.id')?.textContent.trim() === 'port_scan',
    )
    return li !== undefined && li.querySelector('.tuned') === null
  },
  undefined,
  { timeout: 15000 },
)

const afterReset = await api('GET', '/api/definitions/port_scan')
check(
  afterReset.body?.params?.threshold === shippedThreshold,
  `reset puts the shipped threshold back (${afterReset.body?.params?.threshold}, want ${shippedThreshold})`,
)
check(
  Object.keys(afterReset.body?.distance ?? {}).length === 0,
  `after a reset nothing is overridden (${JSON.stringify(afterReset.body?.distance)})`,
)
// Reset is a params operation server-side, and the button must not be
// quietly doing more than that: the scope edited above survives it.
check(
  [8000, 8001, 8002].every((p) => (afterReset.body?.scope?.ports ?? []).includes(p)),
  `reset left the scope alone (${JSON.stringify(afterReset.body?.scope?.ports)})`,
)
check(
  (await row.locator('.panel').count()) === 1,
  'the panel stays open on the freshly stock values, so the operator can see what reset did',
)

// --- clone -------------------------------------------------------------
//
// What the server does, not what the button hopes for. POST
// /api/definitions/{id}/clone copies an *expectation*; for a shipped
// detection definition it refuses, because that definition's logic is Go
// compiled into this binary and keyed by its own id, so a copy would list
// and look configurable and evaluate nothing. Every row on this bench is
// a detection definition, so pressing clone here reaches that refusal.
//
// The refusal is the contract (internal/api's handleDefinitionsClone has
// the reasoning), and what this scenario pins is that the panel shows it
// in the server's own words -- which name the operation that does exist
// -- and that no phantom copy appears on the bench in the meantime.
const definitionsBefore = await api('GET', '/api/definitions')
const countBefore = (definitionsBefore.body?.definitions ?? []).length

await row.locator('.panel button:has-text("Clone")').click()
await row.locator('.error').waitFor({ state: 'visible', timeout: 15000 })
const refusal = (await row.locator('.error').textContent()) ?? ''
check(
  refusal.includes('cannot be cloned'),
  `the clone refusal is shown in the server's own words (${JSON.stringify(refusal.slice(0, 120))})`,
)
check(
  refusal.includes('/api/definitions/'),
  'the refusal keeps the sentence naming the operation that does exist instead',
)

const definitionsAfter = await api('GET', '/api/definitions')
check(
  (definitionsAfter.body?.definitions ?? []).length === countBefore,
  `a refused clone created nothing (${(definitionsAfter.body?.definitions ?? []).length}, was ${countBefore})`,
)

// The clone path itself -- create the copy, pause it, open it with its
// name selected -- is exercised against a definition the server can
// actually copy. It is authored and removed here rather than left behind:
// scenarios share one instance.
const seed = await api('POST', '/api/definitions', {
  name: 'live-watchers-editor seed',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { ports: [7871] },
})
check(seed.status === 201, `a clonable definition is authored for the check (${seed.status})`)
const copy = await api('POST', `/api/definitions/${encodeURIComponent(seed.body?.id)}/clone`, {
  name: 'live-watchers-editor seed (copy)',
})
check(copy.status === 201, `it clones under the "(copy)" name the panel offers (${copy.status})`)
check(
  copy.body?.name === 'live-watchers-editor seed (copy)',
  `the copy carries that name (${JSON.stringify(copy.body?.name)})`,
)
const paused = await api('PUT', `/api/definitions/${encodeURIComponent(copy.body?.id)}`, {
  enabled: false,
})
check(
  paused.status === 200 && paused.body?.enabled === false,
  `the copy is paused, so a half-edited definition never runs (${paused.status}, enabled ${paused.body?.enabled})`,
)

for (const id of [seed.body?.id, copy.body?.id]) {
  if (!id) continue
  await api('DELETE', `/api/definitions/${encodeURIComponent(id)}`)
}

// --- leave the bench as it was found ------------------------------------

await api('PUT', '/api/definitions/port_scan', { scope: {} })
await api('POST', '/api/definitions/port_scan/reset')
const final = await api('GET', '/api/definitions/port_scan')
check(
  final.body?.params?.threshold === shippedThreshold &&
    (final.body?.scope?.ports ?? []).length === 0,
  `this scenario left port_scan as it found it (${JSON.stringify(final.body?.params)}, scope ${JSON.stringify(final.body?.scope)})`,
)

const leftovers = (await api('GET', '/api/definitions')).body?.definitions ?? []
check(
  leftovers.filter((d) => (d.name ?? '').startsWith('live-watchers-editor')).length === 0,
  'this scenario left no definitions of its own behind',
)

// The clone refusal above is a 400 this scenario asks for on purpose, and
// the browser logs every 4xx as a failed resource load. Filtered here, as
// live-filter-refetch-failure.mjs does for its 503s, rather than loosening
// the shared helper: any other 400 is still a defect.
const unexpectedErrors = consoleErrors.filter((e) => !/400 \(Bad Request\)/.test(e))
check(
  unexpectedErrors.length === 0,
  `no unexpected console errors -- got ${JSON.stringify(unexpectedErrors.slice(0, 3))}`,
)

done()
