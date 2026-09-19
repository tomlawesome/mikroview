// SPDX-License-Identifier: AGPL-3.0-only
//
// #1281 (the syslog enrolment gate) and #1284 (the router ledger
// wizard), driven together: since #1281, a router earns a place on the
// fleet only by presenting a token minted for it, and the ledger
// (SetupWizard's router steps) is the only place that token is ever
// shown. What needs a real browser and a real listener rather than a
// unit test:
//
//  1. The token has to be read off the rendered page, not the API --
//     proving the exact block an operator would paste carries a token
//     TryEnrol actually accepts.
//  2. The "closed port" refusal is a real TCP accept being refused
//     before any line, and before TLS -- device.Registry.AcceptsUnknown
//     and syslog.EnrolmentGate can be unit-tested in isolation, but
//     only a real connection attempt proves the listener wires them
//     together correctly.
//  3. The wizard's own warning box -- present only once a line has
//     actually been refused since the current token was minted -- is a
//     rendering guarantee only a real refused connection can trigger.
//
// A note on where the ledger actually opens from, and what this
// deliberately does not exercise. deckCards.ts (#785, pinned by its own
// test "the entities card answers for the fleet view too") gives an
// admin's deck an "Entities" card that answers for both the entities
// and fleet views -- an admin's deck never carries a 'fleet'-keyed card
// at all, so Fleet.svelte itself only ever mounts for the viewer tier.
// That is why this scenario opens the ledger from Entities' own berth
// ("+ add a router"), not from a "Fleet" rail button an admin's deck
// does not have.
//
// That same reading is why Re-enrol… and the refused senders now live
// on Entities rather than Fleet: #1284 first built both into
// Fleet.svelte, where no admin session ever mounts them and the
// admin-only refused endpoint 403s for the one tier that does. Both are
// checked below on Entities' own routers row, where an admin meets
// them.
//
// One router, walked end to end: name it, read its token, misdirect a
// stranger at it, reroll, misdirect the stale token, enrol it for
// real, hit the now-closed port, re-enrol it at a new address. Every
// address from 127.0.0.21 up belongs to this scenario alone -- nothing
// else in the suite mints an enrolment token -- so "no token is pending
// anywhere" (the closed-port assertion's own premise) can be trusted
// rather than merely hoped for.

import { session, feedRawFrom, check, done, goTo, eventsTotal, waitForEventsTotal } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const ROUTER_NAME = 'live-enrolment-router'
const WRONG_IP = '127.0.0.22'
const STALE_TOKEN_IP = '127.0.0.23'
const ENROL_IP = '127.0.0.21'
const CLOSED_PORT_IP = '127.0.0.24'
const REENROL_IP = '127.0.0.25'

const { page, consoleErrors } = await session()

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json().catch(() => null) : null }
}

async function devicesList() {
  return (await api('GET', '/api/devices')).body?.devices ?? []
}

async function refusedList() {
  return (await api('GET', '/api/devices/refused')).body ?? []
}

const plainLine = (rule, dst = '192.168.1.10') =>
  `firewall,info D|${rule}| forward: in:ether1 out:bridge1, connection-state:new, ` +
  `proto TCP (SYN), 203.0.113.60:51500->${dst}:8291, len 60`

// The enrol line itself: the regex the server matches (enrolLineRE,
// internal/device/enrolment.go) only cares that "mikroview-enrol
// <token>" appears somewhere in the raw bytes, so this does not need to
// be a real RouterOS log line -- only to carry the marker the way one
// would.
const enrolLine = (token) => `<14>Jan  1 00:00:00 ${ROUTER_NAME} mikroview-enrol ${token}`

// tokenFromBlock reads the enrolment token straight off the rendered
// Send logs block -- never off the API -- because the thing under test
// is that the block an operator actually pastes carries a token TryEnrol
// accepts. Returns the exact last line too, in case a check wants to
// show what was read.
function tokenFromBlock(text) {
  const lines = text.trim().split('\n')
  const last = lines[lines.length - 1] ?? ''
  const m = last.match(/mikroview-enrol ([a-z0-9]{20})/)
  return { line: last, token: m ? m[1] : null }
}

// waitForCondition polls `read` until it returns something truthy or the
// deadline passes. Needed throughout: the wizard's own refused-box
// updates on its own poll tick (5s), not on the syslog line that will
// eventually be reflected in it.
async function waitForCondition(read, timeoutMs = 16000, intervalMs = 500) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await read()
    if (last) return last
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  return last
}

const wizard = page.locator('.setup-wizard')
// The Send logs block, queried once and reused across the whole
// scenario: every step below (b through f) stays on the syslog pane, so
// this locator keeps resolving to the same step's rendered block.
const block = wizard.locator('.body pre').first()

// --- a. Entities' berth -> Add a router -> the ledger opens at Name --

await goTo(page, 'Entities')
await page.click('button.berth-trigger[aria-label="Add a router"]')
await wizard.waitFor({ timeout: 10000 })
check((await wizard.locator('#router-name').count()) === 1, 'the ledger opens at Name your router')

await page.fill('#router-name', ROUTER_NAME)
await page.click('.setup-wizard footer button.primary:text-is("Next")')

const created = await waitForCondition(async () => (await devicesList()).find((d) => d.name === ROUTER_NAME) ?? null)
check(!!created, `Next creates the device (${ROUTER_NAME})`)
check(created?.acceptedIp === '', `the new device has no acceptedIp yet (got ${JSON.stringify(created?.acceptedIp)})`)
if (!created) {
  check(true, 'skipped -- the rest of the scenario needs the created device')
  done()
}
const deviceId = created.id

// --- b. Send logs: the token comes off the page, not the API ---------

await block.waitFor({ timeout: 10000 })
// The mint that fires on entering the step is async, so the block can
// render once before its last line carries the token.
let read = await waitForCondition(async () => {
  const t = tokenFromBlock((await block.textContent()) ?? '')
  return t.token ? t : null
})
check(!!read?.token, `the block's last line carries a fresh token ("${read?.line}")`)

const tokenLife = wizard.locator('.token-life')
await tokenLife.waitFor({ timeout: 5000 })
const lifeText = ((await tokenLife.textContent()) ?? '').trim()
check(
  /^Token good until \d\d:\d\d \(\d{1,2} minutes?\) · Reroll$/.test(lifeText),
  `the "good until" line reads plainly (got "${lifeText}")`,
)
check((await tokenLife.locator('button:text-is("Reroll")').count()) === 1, 'a Reroll control sits beside it')

// --- c. Wrong sender: refused, and offered no accept anywhere --------

try {
  feedRawFrom(WRONG_IP, plainLine('live-enrolment-wrong'))
} catch (e) {
  check(false, `a line from an unenrolled address while a token is pending should be accepted (and dropped), not refused at connect: ${e}`)
}

const warningBox = wizard.locator('.observation.shortfall.refused')
const warningText = await waitForCondition(async () => {
  if ((await warningBox.count()) === 0) return null
  const t = (await warningBox.textContent()) ?? ''
  return t.includes(WRONG_IP) ? t : null
}, 25000)
check(!!warningText, `the wizard's warning box lists the wrong sender (${WRONG_IP}) -- got "${warningText}"`)

const refusedAfterWrong = await waitForCondition(async () => {
  const list = await refusedList()
  return list.some((r) => r.ip === WRONG_IP) ? list : null
})
check(!!refusedAfterWrong, `GET /api/devices/refused carries ${WRONG_IP}`)


// --- d. Reroll: the token changes; the old one no longer enrols ------

const staleLine = read.line
await page.click('.setup-wizard .token-life button:text-is("Reroll")')
read = await waitForCondition(async () => {
  const t = tokenFromBlock((await block.textContent()) ?? '')
  return t.token && t.line !== staleLine ? t : null
})
check(!!read?.token, `Reroll changes the token on the page (was "${staleLine}", now "${read?.line}")`)

try {
  feedRawFrom(STALE_TOKEN_IP, staleLine)
} catch (e) {
  check(false, `the stale token from a fresh address should be accepted (and refused), not refused at connect: ${e}`)
}

const refusedAfterStale = await waitForCondition(async () => {
  const list = await refusedList()
  return list.some((r) => r.ip === STALE_TOKEN_IP) ? list : null
})
check(!!refusedAfterStale, `the rerolled-away token from ${STALE_TOKEN_IP} is refused, not honoured`)

const afterStale = (await devicesList()).find((d) => d.id === deviceId)
check(
  afterStale?.acceptedIp === '',
  `the device still has no acceptedIp after the stale-token attempt (got ${JSON.stringify(afterStale?.acceptedIp)})`,
)

// --- e. Enrol: the new token from the right address enrols for real --

const before = await eventsTotal(page)
try {
  feedRawFrom(ENROL_IP, read.line)
} catch (e) {
  check(false, `the enrol line from ${ENROL_IP} should be accepted at connect: ${e}`)
}

const enrolled = await waitForCondition(async () => {
  const d = (await devicesList()).find((dv) => dv.id === deviceId)
  return d?.acceptedIp === ENROL_IP ? d : null
})
check(!!enrolled, `the device's acceptedIp becomes ${ENROL_IP} within a few seconds`)

// TryEnrol drops the address from the refused list as it enrols it: the
// router's own logging-action change is logged before its enrol line
// reaches the listener, so its first line or two are refused, and
// leaving the address in the list would show the router just enrolled
// as a wrong sender beside the "enrolled at" line saying the opposite.
// Strangers stay refused -- only the address that just proved itself
// comes off the list.
const stillRefusedAfterEnrol = await refusedList()
check(
  !stillRefusedAfterEnrol.some((r) => r.ip === ENROL_IP),
  `${ENROL_IP} comes off the refused list once it enrols (list now: ${stillRefusedAfterEnrol.map((r) => r.ip).join(', ') || '(empty)'})`,
)
check(
  stillRefusedAfterEnrol.some((r) => r.ip === WRONG_IP) && stillRefusedAfterEnrol.some((r) => r.ip === STALE_TOKEN_IP),
  `while the actual strangers (${WRONG_IP}, ${STALE_TOKEN_IP}) stay on it`,
)

const syslogRow = wizard.locator('nav.steps .step-row').nth(1)
const syslogDone = await waitForCondition(async () => {
  const cls = (await syslogRow.getAttribute('class')) ?? ''
  return /\bdone\b/.test(cls) ? cls : null
})
check(!!syslogDone, `the Send logs step reads as done once enrolled (class="${syslogDone}")`)

try {
  feedRawFrom(ENROL_IP, plainLine('live-enrolment-accepted'))
  const after = await waitForEventsTotal(page, before + 1)
  check(after >= before + 1, `a plain line from the now-enrolled ${ENROL_IP} is accepted (events ${before} -> ${after})`)
} catch (e) {
  check(false, `a plain line from ${ENROL_IP} should now be accepted: ${e}`)
}

// --- f. Closed port: no token pending, so the connection is refused --

let closedAtConnect = false
try {
  feedRawFrom(CLOSED_PORT_IP, plainLine('live-enrolment-closed'))
} catch {
  closedAtConnect = true
}
check(closedAtConnect, `with no token pending, a connection from ${CLOSED_PORT_IP} is refused at accept`)

const refusedClosed = await waitForCondition(async () => {
  const list = await refusedList()
  return list.some((r) => r.ip === CLOSED_PORT_IP) ? list : null
})
check(!!refusedClosed, `${CLOSED_PORT_IP} appears in the refused list`)

// --- g. Re-enrol: move the router to a new address --------------------
//
// Re-enrol… on the router's own card on Entities: the ledger reopens at
// Send logs for this router with a freshly minted token, and the router
// moves to its new address while the old one stops being trusted. The
// control lives here rather than on Fleet because an admin's deck never
// draws Fleet at all (deckCards.ts, #785).

await page.click('.setup-wizard button.close')
await wizard.waitFor({ state: 'detached', timeout: 5000 }).catch(() => {})

// The refused senders on the same screen: a card per address, and
// nothing on it that would accept one.
const refusedCard = page.locator('.fcard.refused', { hasText: WRONG_IP })
const refusedCardText = await waitForCondition(async () => {
  if ((await refusedCard.count()) === 0) return null
  return (await refusedCard.first().textContent()) ?? null
}, 20000)
check(!!refusedCardText, `Entities draws a refused card for ${WRONG_IP} -- got "${refusedCardText}"`)
check(
  (refusedCardText ?? '').includes('no router is enrolled at'),
  'the card says what it is rather than diagnosing whose it is',
)
check(
  (await refusedCard.first().locator('button').count()) === 0,
  'and carries no control that would accept it, by ruling',
)
check(
  !(await page.locator('.fcard.refused', { hasText: ENROL_IP }).count()),
  `and none for ${ENROL_IP}, which enrolled`,
)

await page.click(`.fcard button.row-action[aria-label^="Re-enrol ${ROUTER_NAME}"]`)
await wizard.waitFor({ timeout: 10000 })
const remintedLine = await waitForCondition(async () => {
  const t = tokenFromBlock((await block.textContent()) ?? '')
  return t.token && t.line !== read.line ? t : null
}, 20000)
check(!!remintedLine?.token, `Re-enrol… reopens the ledger with a fresh token ("${remintedLine?.line}")`)

if (remintedLine?.token) {
  try {
    feedRawFrom(REENROL_IP, enrolLine(remintedLine.token))
  } catch (e) {
    check(false, `the enrol line from ${REENROL_IP} should be accepted at connect: ${e}`)
  }
}

const reenrolled = await waitForCondition(async () => {
  const d = (await devicesList()).find((dv) => dv.id === deviceId)
  return d?.acceptedIp === REENROL_IP ? d : null
})
check(!!reenrolled, `after re-enrolling, acceptedIp becomes ${REENROL_IP}`)

let oldAddressRefused = false
try {
  feedRawFrom(ENROL_IP, plainLine('live-enrolment-old'))
} catch {
  oldAddressRefused = true
}
if (!oldAddressRefused) {
  oldAddressRefused = (await refusedList()).some((r) => r.ip === ENROL_IP)
}
check(oldAddressRefused, `${ENROL_IP}'s lines are refused now that the router has moved to ${REENROL_IP}`)

// --- h. Clean up: leave the fleet the way every other scenario finds it

const del = await api('DELETE', `/api/devices/${encodeURIComponent(deviceId)}`)
check(del.status === 204, `the scenario's device is deleted (${del.status})`)
const remaining = await devicesList()
check(!remaining.some((d) => d.id === deviceId), 'no trace of it remains for later scenarios')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
