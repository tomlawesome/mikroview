// SPDX-License-Identifier: AGPL-3.0-only
//
// #600: renaming a device from the live view, once a device name has
// somewhere everyone can read it.
//
// This scenario exists for the half that could not be built inside #413:
// a device rename stored per-browser would have shown to the one admin
// who typed it, and everybody else would have carried on reading the old
// name with nothing on screen to say why. So the assertion that matters
// here is made in a SECOND signed-in browser, with its own cookie jar,
// which never touched the editor: it must read the new name in the live
// view and on the routers surface. Nothing in the jsdom suite can show
// that -- it turns on the name being resolved on the server and served
// to every session by GET /api/devices.
//
// The other half is the refusal. A device declared in config.yaml is
// named by config.yaml, and a label saved under it would be stored and
// never displayed -- the same lying affordance "RouterOS always wins"
// produces for a host name, one layer along. The editor must offer no
// field at all and say where the name lives, in #413's grammar.
//
// Two devices, and the harness only declares one. The live-env.sh
// config declares live-router on 127.0.0.1 and every feeder sends from
// there.
//
// #1170 changed how the second, undeclared router comes to exist: a
// syslog source is never enough by itself any more, so 127.0.0.9
// logging first only proves the other half of that ruling -- an
// address nobody has declared or claimed sits in GET /api/devices'
// `unattributed` list, never in `devices`. The router itself arrives
// the way every push-discovered device now does: an ingest token
// minted for a fresh id (UNDECLARED_ID, distinct from the address on
// purpose) whose first push both Ensures the device and claims
// 127.0.0.9 as its own /ip/address entry, so the syslog already
// arriving from that address attributes to it from the next line on.
// That leaves a push-created device behind on the shared instance for
// every scenario sorting after this one, which is safe in a way it was
// not before: Registry.List now orders configured devices first and
// then by id (#600), so the devices[0] a dozen scenarios read is still
// live-router. The entity this scenario writes is deleted at the end,
// so the leftover device is named after its own id again, exactly as
// it arrived.

import {
  session,
  feedRaw,
  feedRawFrom,
  check,
  done,
  launchBrowser,
  dismissSetupWizard,
  goTo,
  unfoldStreamFilter,
} from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS

// The undeclared router's syslog address: in 127.0.0.0/8, nothing else
// feeds from it, so it is this scenario's alone. #1170: this is no
// longer the device's identity -- it is the address its own pushed
// /ip/address table claims, which is a different thing on purpose.
const UNDECLARED_IP = '127.0.0.9'
// The undeclared router's actual identity: the id its ingest token
// names. Deliberately not UNDECLARED_IP -- #1170's whole point is that
// a device id is never merely a syslog source address.
const UNDECLARED_ID = 'mv-rename-undeclared'
// Neither label is a prefix of the other: the filter box matches on a
// substring, so "live-device-rename" alone would show both routers'
// rows and every locator below would be ambiguous.
const RULE_UNDECLARED = 'live-device-rename-undeclared'
const RULE_DECLARED = 'live-device-rename-declared'
const NEW_NAME = 'lab-crs-renamed'

const { page, consoleErrors } = await session()

async function api(client, method, path, body) {
  const res = await client.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
}

// push, unlike api() above, goes straight over fetch with a bearer
// token rather than through the signed-in session's cookie jar --
// there is no session-based path to the ingest endpoint at all (see
// handleIngestRouterOS's own comment). Same shape as
// live-fleet-setup-standing.mjs's push().
async function push(token, payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

const line = (rule, dst) =>
  `firewall,info D|${rule}| forward: in:ether1 out:bridge1, connection-state:new, ` +
  `proto TCP (SYN), 203.0.113.60:51500->${dst}:8291, len 60`

async function devices(client) {
  return (await api(client, 'GET', '/api/devices')).body?.devices ?? []
}

// --- The declared router, as the server sees it ---------------------------

feedRaw(line(RULE_DECLARED, '192.168.1.10'))

let declared = null
{
  const deadline = Date.now() + 25000
  while (Date.now() < deadline && !declared) {
    const list = await devices(page.request)
    declared = list.find((d) => d.configured)
    if (declared) break
    await new Promise((r) => setTimeout(r, 1500))
    feedRaw(line(RULE_DECLARED, '192.168.1.10'))
  }
}
check(!!declared, `the harness's declared router is reported (${declared?.id})`)
check(
  declared?.nameSource === 'config-device',
  `and says config.yaml decides its name (nameSource=${declared?.nameSource}, name="${declared?.name}")`,
)
if (!declared) {
  check(true, 'skipped -- the rename cannot be exercised without the declared-router baseline')
  done()
}

// --- #1170: an unclaimed syslog source is not a router --------------------
//
// Fed before anything claims it as an address. This is the ruling this
// scenario now proves for its "undeclared router" half: a source
// address with no config.yaml entry and no router's own address table
// naming it never becomes a device, however many lines it sends -- it
// sits under GET /api/devices' `unattributed` list instead.

feedRawFrom(UNDECLARED_IP, line(RULE_UNDECLARED, '192.168.1.11'))

let unattributedSrc = null
{
  const deadline = Date.now() + 25000
  while (Date.now() < deadline && !unattributedSrc) {
    const { body } = await api(page.request, 'GET', '/api/devices')
    unattributedSrc = (body?.unattributed ?? []).find((s) => s.address === UNDECLARED_IP)
    if (unattributedSrc) break
    await new Promise((r) => setTimeout(r, 1500))
    feedRawFrom(UNDECLARED_IP, line(RULE_UNDECLARED, '192.168.1.11'))
  }
}
check(
  !!unattributedSrc,
  `a syslog source nobody has declared or claimed shows up as unattributed, not a device (${UNDECLARED_IP})`,
)
check(
  !(await devices(page.request)).some((d) => d.id === UNDECLARED_IP),
  'and never appears in the devices array -- #1170: a syslog source alone never invents a router',
)

// --- The undeclared router arrives by push, not by syslog source ----------
//
// A device exists only because the operator minted an ingest token for
// it (Ensure) or declared it in config.yaml. The token below names a
// fresh device id, distinct from the address it will end up
// attributed to; its first push both creates the device and claims
// 127.0.0.9 as its own (an /ip/address table entry), so the syslog
// already arriving from that address resolves to it from the next
// line on.

const undeclaredToken = await api(page.request, 'POST', '/api/tokens', {
  name: 'live-device-rename-undeclared',
  kind: 'ingest',
  device: UNDECLARED_ID,
})
check(undeclaredToken.status === 201, `an ingest token is issued for ${UNDECLARED_ID} (${undeclaredToken.status})`)

let undeclared = null
if (undeclaredToken.status === 201 && undeclaredToken.body?.value) {
  const pushStatus = await push(undeclaredToken.body.value, {
    kind: 'ip-address',
    page: 1,
    pages: 1,
    records: [{ address: `${UNDECLARED_IP}/32`, network: '', interface: '', comment: '' }],
  })
  check(pushStatus === 200, `the router's own address-table push is accepted (${pushStatus})`)

  const list = await devices(page.request)
  undeclared = list.find((d) => d.id === UNDECLARED_ID)
}
check(
  !!undeclared,
  `an undeclared router appears once it pushes its own state, not once its address logs (${UNDECLARED_ID})`,
)
if (!undeclared) {
  check(true, 'skipped -- the rename cannot be exercised without a device to rename')
  done()
}
check(
  undeclared.name === UNDECLARED_ID && undeclared.nameSource === 'none',
  `and arrives named after its own device id (name="${undeclared.name}", nameSource=${undeclared.nameSource})`,
)

// --- The rename, from the live view --------------------------------------

// Fed again now that 127.0.0.9 is claimed: Resolve attributes this
// line, and everything after it, to UNDECLARED_ID instead of leaving
// it as an unattributed source.
feedRawFrom(UNDECLARED_IP, line(RULE_UNDECLARED, '192.168.1.11'))

await page.fill('input.rule', RULE_UNDECLARED)

let rowFound = true
try {
  await page.locator('.row', { hasText: UNDECLARED_ID }).first().waitFor({ timeout: 15000 })
} catch {
  rowFound = false
}
check(rowFound, `a row from the undeclared router rendered, showing ${UNDECLARED_ID}`)
if (!rowFound) {
  check(true, 'skipped -- the editor cannot be exercised on a row that never rendered')
  done()
}

// Located by the rule label, never by the device name: the name in that
// cell is the thing under test, so a locator that matched on it would
// stop finding the row at the moment the rename works.
const deviceCell = page.locator('.row', { hasText: RULE_UNDECLARED }).first().locator('.cell.device')
check(
  (await deviceCell.locator('.copy-btn').count()) === 1 &&
    (await deviceCell.locator('.edit-btn').count()) === 1,
  'the device token carries the pencil beside the copy glyph, in #439\'s reserved slot',
)

await deviceCell.locator('.edit-btn').click()
const editor = page.locator('.popover.name-editor')
await editor.waitFor({ timeout: 5000 })

const input = editor.locator('input')
let offered = true
try {
  await input.waitFor({ timeout: 5000 })
} catch {
  offered = false
}
check(offered, 'a device config.yaml does not name offers a field')
check(
  ((await editor.textContent()) ?? '').includes('stays the identity'),
  'and says the device id stays the identity -- the rename is display only',
)

await input.fill(NEW_NAME)
await editor.locator('button.save').click()
await editor.waitFor({ state: 'detached', timeout: 5000 })

let renamedHere = true
try {
  await deviceCell.locator(`text=${NEW_NAME}`).waitFor({ timeout: 5000 })
} catch {
  renamedHere = false
}
check(renamedHere, `the row already on screen reads "${NEW_NAME}" -- no reload`)

const entities = await api(page.request, 'GET', '/api/entities')
check(
  (entities.body?.entities ?? []).some(
    (e) => e.type === 'device' && e.key === UNDECLARED_ID && e.label === NEW_NAME,
  ),
  `the name is stored against the device id ${UNDECLARED_ID}, which is untouched`,
)

const served = (await devices(page.request)).find((d) => d.id === UNDECLARED_ID)
check(
  served?.name === NEW_NAME && served?.nameSource === 'entity',
  `GET /api/devices serves the new name to anyone who asks (name="${served?.name}", nameSource=${served?.nameSource})`,
)

// --- The second signed-in person, who never opened the editor ------------

const otherBrowser = await launchBrowser()
const otherCtx = await otherBrowser.newContext({ ignoreHTTPSErrors: true })
const other = await otherCtx.newPage()
await other.goto(URL_BASE, { waitUntil: 'networkidle' })
await other.fill('input[autocomplete="username"]', USER)
await other.fill('input[autocomplete="current-password"]', PASS)
await other.click('button[type="submit"]')
await other.waitForSelector('#main-content', { timeout: 15000 })
await dismissSetupWizard(other)
await goTo(other, 'Stream')
await other.waitForSelector('input.rule', { timeout: 15000 })
await other.fill('input.rule', RULE_UNDECLARED)

let secondSees = true
try {
  await other
    .locator('.row', { hasText: NEW_NAME })
    .first()
    .locator('.cell.device')
    .waitFor({ timeout: 20000 })
} catch {
  secondSees = false
}
check(secondSees, `a second signed-in session reads "${NEW_NAME}" in its own live view`)

// The routers surface the same session reaches -- Entities' leading
// section for an admin, the Fleet card for a viewer, both drawn from
// lib/fleet.ts over the same GET /api/devices list.
await goTo(other, 'Entities')
let fleetSees = true
try {
  await other.locator(`.card[data-card="entities"] :text-is("${NEW_NAME}")`).first().waitFor({ timeout: 20000 })
} catch {
  fleetSees = false
}
check(fleetSees, 'and reads it on the routers surface too, not just in the stream')

const otherServed = (await devices(other.request)).find((d) => d.id === UNDECLARED_ID)
check(
  otherServed?.name === NEW_NAME && otherServed?.sourceIp === UNDECLARED_IP,
  'the second session is served the same name over the same raw address',
)

// --- The refusal: a device config.yaml names ----------------------------

// Fed again rather than relying on the one at the top: the shared
// instance keeps taking events from every other feeder while this
// scenario runs, and a single line from minutes ago may have fallen out
// of what the table renders.
feedRaw(line(RULE_DECLARED, '192.168.1.10'))
// Unfolded again first: the filter strip closes on a click outside it,
// and the click that opened the editor above is one -- without this the
// fill below waits 30s for an input that is folded away, and the
// scenario dies before printing a result.
await unfoldStreamFilter(page)
await page.fill('input.rule', RULE_DECLARED)
let declaredRow = true
try {
  await page.locator('.row', { hasText: declared.name }).first().waitFor({ timeout: 15000 })
} catch {
  declaredRow = false
}
check(declaredRow, `a row from the declared router rendered, showing "${declared.name}"`)

if (declaredRow) {
  await page.locator('.row', { hasText: declared.name }).first().locator('.cell.device .edit-btn').click()
  await editor.waitFor({ timeout: 5000 })
  // The popover opens loading (NameEditorPopover.svelte's `st.loading`
  // branch) and only renders `p.refusal` once fetchNameProvenance
  // resolves -- reading the text before that landed read the loading
  // copy instead and made the checks below flaky (#1128).
  await editor.locator('p.refusal').waitFor({ timeout: 5000 })
  const refusal = (await editor.textContent()) ?? ''

  // Counting the elements, not reading a `disabled` attribute: a
  // disabled field is still a field, and one authored declaration away
  // from being typeable.
  check(
    (await editor.locator('input').count()) === 0,
    'a device config.yaml names offers NO text field -- an edit here would be discarded',
  )
  check(
    (await editor.locator('button.save').count()) === 0,
    'and no Save button, so there is no path to a silent no-op save',
  )
  check(refusal.includes('config.yaml'), 'the editor says plainly that config.yaml supplies this name')
  check(
    refusal.includes('stored and never displayed'),
    'in the same grammar #413 uses for a name the router supplies',
  )
  check(refusal.includes(declared.id), `while still showing the raw device id (${declared.id})`)
  check(!refusal.includes('RouterOS'), 'and sends nobody to a router, because no router holds this name')

  await page.keyboard.press('Escape')
  await editor.waitFor({ state: 'detached', timeout: 5000 })
}

const afterRefusal = await api(page.request, 'GET', '/api/entities')
check(
  !(afterRefusal.body?.entities ?? []).some((e) => e.type === 'device' && e.key === declared.id),
  'nothing was written for the config-named device -- the refusal is before the write',
)

// --- Put the instance back the way it was found -------------------------
//
// run-scenarios.sh runs one shared instance in filename order, so an
// entity left here is an input to every scenario after this one. The
// push-created device itself cannot be removed and does not need to
// be: with its label gone it is named after its own device id again --
// it was never named after its address (#1170).
await api(page.request, 'DELETE', '/api/entities', { type: 'device', key: UNDECLARED_ID })
const leftovers = await api(page.request, 'GET', '/api/entities')
check(
  !(leftovers.body?.entities ?? []).some((e) => e.type === 'device'),
  'no device entity is left behind for the next scenario to trip over',
)
const restored = (await devices(page.request)).find((d) => d.id === UNDECLARED_ID)
check(
  restored?.name === UNDECLARED_ID,
  `and the device shows its own id again (name="${restored?.name}")`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
