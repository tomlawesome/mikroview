// SPDX-License-Identifier: AGPL-3.0-only
//
// #413: hosts, ports and rule names become editable from the live view
// itself -- the pencil in the slot #439 reserved for it, opening an
// anchored editor over the token you were already looking at.
//
// The case this scenario exists for is the *refusal*. The owner ruled on
// 2026-08-22 that "RouterOS always wins" stands (#186 step 4c), so a
// label saved for a host the router already names is stored faithfully
// and then never displayed. POST /api/entities answers 200 to that
// write, because from its side the write did succeed -- which is exactly
// how an operator ends up typing a name, seeing a confirmation, and
// watching nothing change. The editor must refuse the edit outright, say
// which pushed table holds the winning name, and name the router as the
// place to change it.
//
// None of that is provable from the jsdom suite: it turns on a real
// router-pushed DHCP lease resolving through internal/naming at ingest
// and reaching a real rendered row, which needs the real server. The
// same is true of the other half -- a rename of a token the router does
// *not* name has to visibly take on rows already on screen, and rows
// already on screen were named once, at ingest.
//
// Both addresses are in 203.0.113.0/24 (RFC 5737 TEST-NET-3) and neither
// collides with another scenario's claim: live-token-copy.mjs owns .221
// and .44, live-router-lookup.mjs owns .2. Deliberately NOT
// 192.0.2.0/24 or 198.51.100.0/24 -- live-router-lookup.mjs pushes a
// wireguard-peer covering both, named "branch office", which would
// shadow this scenario's own labels exactly as it once shadowed
// live-token-copy.mjs's (see that file's HOST_IP comment).

import { session, feedRaw, feedSyslog as syslog, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const RULE = 'live-inline-editing'
// Nothing router-supplied names this one, so labelling it takes effect.
const FREE_IP = '203.0.113.171'
// A DHCP lease pushed below names this one, so labelling it must be
// refused.
const LEASED_IP = '203.0.113.172'
const LEASE_NAME = 'android-dhcp-1234'
const NEW_LABEL = 'nas-inline-edit'

const { page, consoleErrors } = await session()

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// --- A router that actually names one of the two addresses ---------------

// Discovered, never assumed: host names are scoped to the device that
// pushed them (#289, and #285/#283/#284 for why), so an ingest token
// scoped to the wrong device would push a lease that names nothing and
// this scenario would "pass" having tested the opposite case.
syslog(2, 'device-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await api('POST', '/api/tokens', { name: RULE, kind: 'ingest', device: DEVICE })
check(tokenRes.status === 201, `an ingest token is issued (${tokenRes.status})`)

async function pushLeases(records) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${tokenRes.body.value}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'dhcp-lease', page: 1, pages: 1, records }),
  })
  return res.status
}

const pushed = await pushLeases([{ hostname: LEASE_NAME, mac: 'aa:bb:cc:dd:ee:13', address: LEASED_IP }])
check(pushed === 200, `the router pushes a DHCP lease naming ${LEASED_IP} (${pushed})`)

// --- One event carrying both tokens --------------------------------------

const line =
  `firewall,info A|${RULE}| forward: in:ether1 out:bridge1, connection-state:new, ` +
  `proto TCP (SYN), ${FREE_IP}:51513->${LEASED_IP}:8291, len 60`
feedRaw(line)

// Wait for the event on the server before asking the UI about it --
// #354's pattern. Under the full suite's load a single fed line can be
// dropped or arrive seconds late, and waiting on the rendered row alone
// turns that into an uncaught Playwright timeout with no RESULT line.
let arrived = null
const deadline = Date.now() + 25000
while (Date.now() < deadline) {
  const events = await api('GET', `/api/events?rule=${RULE}&limit=5`)
  if ((events.body?.events?.length ?? 0) > 0) {
    arrived = events.body.events[0]
    break
  }
  await new Promise((r) => setTimeout(r, 2500))
  feedRaw(line)
}
check(!!arrived, `the test event reached the server (rule=${RULE})`)
if (!arrived) {
  check(true, 'skipped -- the editor cannot be exercised on a row that never arrived')
  done()
}

// Diagnostic rather than an assertion: if the lease did not reach the
// naming layer, every refusal assertion below would fail for a reason
// that has nothing to do with the editor.
console.log(`DIAGNOSTIC srcHostName=${arrived.srcHostName ?? '(none)'} dstHostName=${arrived.dstHostName ?? '(none)'}`)
check(
  arrived.dstHostName === LEASE_NAME,
  `the router-pushed lease names the destination at ingest (got "${arrived.dstHostName ?? '(none)'}")`,
)

await page.fill('input.rule', RULE)

let rowFound = true
try {
  // waitFor on a locator, never page.isVisible() -- isVisible answers
  // immediately from the current DOM and does not wait, which turns a
  // row that is one render away into a false negative.
  await page.locator('.row', { hasText: LEASE_NAME }).first().waitFor({ timeout: 15000 })
} catch {
  rowFound = false
}
check(rowFound, `a row showing "${LEASE_NAME}" rendered`)
if (!rowFound) {
  check(true, 'skipped -- the editor cannot be exercised on a row that never rendered')
  done()
}

const row = page.locator('.row', { hasText: LEASE_NAME }).first()
// The two address cells in arrival order: source first, destination
// second -- the same cell ordering EventRow.svelte lays out.
const srcCell = row.locator('.cell.addr').nth(0)
const dstCell = row.locator('.cell.addr').nth(1)

// --- The pencil exists, in the reserved slot -----------------------------

check(
  (await row.locator('.edit-btn').count()) > 0,
  'the row carries pencils (this session is an admin)',
)
check(
  (await srcCell.locator('.copy-btn').count()) === 1 &&
    (await srcCell.locator('.edit-btn').count()) === 1,
  'the pencil sits beside the copy glyph on the address token, not inside the filter target',
)

// --- THE NO-OP CASE: a router-named token must refuse the edit -----------

await dstCell.locator('.edit-btn').click()
const editor = page.locator('.popover.name-editor')
await editor.waitFor({ timeout: 5000 })

const refusalText = (await editor.textContent()) ?? ''

// The assertion this whole scenario exists for. Not "the input is
// disabled" -- there must be no input at all. A disabled field is still
// a field, and `disabled` is one authored `display:` declaration away
// from being typeable anyway; counting the elements is the check that
// cannot be satisfied by a hidden-but-present control.
check(
  (await editor.locator('input').count()) === 0,
  'a router-named token offers NO text field -- an edit here would be discarded',
)
check(
  (await editor.locator('button.save').count()) === 0,
  'and no Save button, so there is no path to a silent no-op save',
)
check(
  refusalText.includes('RouterOS'),
  'the editor says plainly that the router supplies this name',
)
check(
  refusalText.includes('DHCP lease'),
  'and names the pushed table holding it, not just "the router"',
)
check(
  refusalText.includes(DEVICE),
  `and names ${DEVICE} as the place to change it`,
)
check(
  refusalText.includes(LEASED_IP),
  'while still showing the raw value, which is what filters and copies use',
)

// The entity store must be untouched by any of that: the refusal is
// before the write, not a write that gets undone.
const afterRefusal = await api('GET', '/api/entities')
check(
  !(afterRefusal.body?.entities ?? []).some((e) => e.type === 'host' && e.key === LEASED_IP),
  'nothing was written for the router-named host',
)

await page.keyboard.press('Escape')
await editor.waitFor({ state: 'detached', timeout: 5000 })
check(true, 'Escape closes the editor')

// --- The gate opens as readily as it shuts -------------------------------

await srcCell.locator('.edit-btn').click()
await editor.waitFor({ timeout: 5000 })

const input = editor.locator('input')
await input.waitFor({ timeout: 5000 })
check(true, `a token the router does not name offers a field (${FREE_IP})`)
check(
  ((await editor.textContent()) ?? '').includes('Display only'),
  'the standing subtext says the raw value is what filters, groups and copies use',
)

// The Autoscroll preference must survive the transient hold the editor
// takes while it is open: the button states what it will do next time,
// and flipping it under the operator would make it lie.
const autoscroll = page.locator('button:has-text("Autoscroll")')
const autoscrollBefore = await autoscroll.getAttribute('class')

await input.fill(NEW_LABEL)
await editor.locator('button.save').click()
await editor.waitFor({ state: 'detached', timeout: 5000 })

check(
  (await autoscroll.getAttribute('class')) === autoscrollBefore,
  'the Autoscroll toggle is unchanged -- the hold while editing is transient, not a preference change',
)

// The requirement that a rename visibly takes. Names are resolved once,
// at ingest, so the row on screen carried the old name a moment ago and
// nothing would re-resolve it on its own; this is a rewrite of what is
// already rendered, with no reload.
let renamed = true
try {
  await srcCell.locator(`text=${NEW_LABEL}`).waitFor({ timeout: 5000 })
} catch {
  renamed = false
}
check(renamed, `the row already on screen now reads "${NEW_LABEL}" -- no reload, no re-query`)

const saved = await api('GET', '/api/entities')
check(
  (saved.body?.entities ?? []).some(
    (e) => e.type === 'host' && e.key === FREE_IP && e.label === NEW_LABEL,
  ),
  `the entity is keyed by the raw ${FREE_IP}, with the label as display only`,
)

// --- Removing a label restores the raw value -----------------------------

await srcCell.locator('.edit-btn').click()
await editor.waitFor({ timeout: 5000 })
await editor.locator('input').fill('')
await editor.locator('button.save').click()
await editor.waitFor({ state: 'detached', timeout: 5000 })

let restored = true
try {
  await srcCell.locator(`text=${FREE_IP}`).waitFor({ timeout: 5000 })
} catch {
  restored = false
}
check(restored, 'an emptied field removes the label and the raw value shows again')

// --- Put the instance back the way it was found -------------------------
//
// Not tidiness. run-scenarios.sh globs alphabetically against ONE shared
// server, so everything left behind here is an input to every scenario
// that sorts after this one -- and the failure surfaces over there, as
// an unrelated scenario asserting a number that used to be right.
//
// That is not hypothetical: this scenario's DHCP lease carries a
// hostname and a MAC, which is exactly what internal/suggest's Generate
// turns into a *device candidate* (see generate.go -- a lease needs both
// to become one). Generate scans every device in RouterState, and
// live-suggestions-matches.mjs pushes its own lease under a device name of its
// own ("live-suggest-router"), so the two coexisted rather than
// replacing each other and its "the Undecided filter counts both"
// assertion saw three candidates where it expects two. Confirmed by
// running live-suggestions-matches.mjs alone on a clean instance (pass), then
// after this scenario (fail on exactly that line).
//
// Retiring the lease, rather than avoiding a MAC to dodge the candidate
// rule, is the fix: a real lease has a MAC, and a scenario quietly
// shaped around another package's generation rule would break again the
// moment that rule changed, in someone else's scenario.
//
// Asserted, not merely attempted: a cleanup that silently stops working
// has to fail here, where the cause is obvious, rather than somewhere
// downstream where it is not.
const retired = await pushLeases([])
check(retired === 200, `the pushed DHCP lease is retired (${retired})`)

const revoked = await api('DELETE', `/api/tokens/${tokenRes.body.id}`)
check(revoked.status === 200, `the ingest token is revoked (${revoked.status})`)

// The emptied-field step above already removed the host entity, but
// that is an assertion about the product, not a guarantee about this
// scenario's footprint -- if it ever regresses, the entity would leak
// into every later scenario. Delete unconditionally, then prove it.
await api('DELETE', '/api/entities', { type: 'host', key: FREE_IP })
await api('DELETE', '/api/entities', { type: 'host', key: LEASED_IP })
const leftovers = await api('GET', '/api/entities')
check(
  !(leftovers.body?.entities ?? []).some((e) => e.key === FREE_IP || e.key === LEASED_IP),
  'no entity from this scenario is left behind for the next one to trip over',
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
