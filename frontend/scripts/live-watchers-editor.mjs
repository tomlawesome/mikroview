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

// --- a detector this bench can copy -------------------------------------
//
// Clone works on an operator-authored detector, whose structure is stored
// data (#502/#810), so the scenario authors one before the bench is
// opened -- the bench reads its list once on mount, and a definition
// created afterwards would not be on it. Removed again at the end:
// scenarios share one instance.
//
// Deliberately watching a port nothing in this environment talks to, and
// left running: it must not raise a flag another scenario then has to
// account for, and the copy's paused state has to be the clone's doing
// rather than inherited.
const SEED_NAME = 'live-watchers-editor detector'
const seed = await api('POST', '/api/definitions', {
  name: SEED_NAME,
  intent: 'detection',
  detection: {
    conditions: [{ field: 'destinationPort', operator: 'equals', values: ['7871'] }],
    key: 'perSource',
    counting: 'total',
    detailTemplate: '{Count} attempts against port 7871 from {SourceAddress}',
    threshold: 100,
    window: '60s',
  },
})
check(seed.status === 201, `a custom detector is authored for the clone check (${seed.status} ${seed.text.slice(0, 200)})`)
const seedID = seed.body?.id ?? ''

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
// Clone is offered where it can succeed and nowhere else (#810). A
// shipped detector's logic is Go keyed by its own id, so the server
// refuses to copy it and always will -- that row carries no button. An
// operator-authored one is stored structure, so it copies, and what this
// section pins is the interaction #787 decision C describes: the copy
// appears, paused, expanded, with its name selected to be typed over.
//
// Driven through the bench and then read back from the server, never from
// the browser's own optimism: a copy the UI shows and the store never
// stored would pass any assertion made against the DOM alone.
check(
  (await row.locator('.panel button:has-text("Clone")').count()) === 0,
  'the shipped row offers no Clone -- the one outcome it could have is a refusal',
)

const custom = page.locator(`.bench li.row:has(.id:text-is("${seedID}"))`)
check((await custom.count()) === 1, 'the authored detector is a row on this bench like any other')
await custom.locator('.row-knob').click()
await custom.locator('.panel').waitFor({ state: 'visible' })
check(
  (await custom.locator('.panel button:has-text("Clone")').count()) === 1,
  'the custom row offers Clone',
)

await custom.locator('.panel button:has-text("Clone")').click()
const copyRow = page.locator(`.bench li.row:has-text("${SEED_NAME} (copy)")`)
await copyRow.waitFor({ state: 'visible', timeout: 15000 })
check(true, 'pressing Clone produces the copy with no prompt in between')
check(
  (await copyRow.locator('.panel').count()) === 1,
  'the copy is already expanded, ready to be edited',
)
check(
  (await page.locator('.bench .panel').count()) === 1,
  'and it is the only panel open -- the original closed behind it',
)
check(
  ((await copyRow.locator('.state').textContent()) ?? '').includes('paused'),
  `the copy is paused, so a half-edited detector never runs (${await copyRow.locator('.state').textContent()})`,
)

// The name field, focused and selected, is what makes this "start
// typing" rather than "now go and find the name box".
const focused = await page.evaluate(() => {
  const el = document.activeElement
  return {
    tag: el?.tagName ?? '',
    value: el instanceof HTMLInputElement ? el.value : null,
    selected: el instanceof HTMLInputElement ? el.value.slice(el.selectionStart ?? 0, el.selectionEnd ?? 0) : '',
  }
})
check(
  focused.tag === 'INPUT' && focused.value === `${SEED_NAME} (copy)`,
  `the copy's name field has focus (${JSON.stringify(focused)})`,
)
check(
  focused.selected === `${SEED_NAME} (copy)`,
  'its text is selected, so the operator types the real name straight over it',
)

const copyID = ((await copyRow.locator('.id').textContent()) ?? '').trim()
const stored = await api('GET', `/api/definitions/${encodeURIComponent(copyID)}`)
check(
  stored.status === 200 && stored.body?.enabled === false,
  `the store agrees the copy is paused (${stored.status}, enabled ${stored.body?.enabled})`,
)
check(
  copyID !== seedID && stored.body?.provenance?.origin === 'custom',
  `the copy is a second custom detector with its own id (${copyID}, original ${seedID})`,
)
// The whole point of copying a custom detection rather than refusing it:
// the copy carries the structure that makes it evaluate anything.
check(
  JSON.stringify(stored.body?.detection) === JSON.stringify(seed.body?.detection),
  `the copy carries the original's conditions and aggregation (${JSON.stringify(stored.body?.detection)})`,
)
check(
  stored.body?.params?.threshold === seed.body?.params?.threshold &&
    stored.body?.params?.window === seed.body?.params?.window,
  `and its tuning (${JSON.stringify(stored.body?.params)}, original ${JSON.stringify(seed.body?.params)})`,
)

for (const id of [seedID, copyID]) {
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

// Nothing here asks the server for a refusal any more (#810 offers Clone
// only where it succeeds), so every console error is a defect and none is
// filtered out.
check(
  consoleErrors.length === 0,
  `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`,
)

done()
