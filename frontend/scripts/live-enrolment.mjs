// SPDX-License-Identifier: AGPL-3.0-only
//
// #1281 (the syslog enrolment gate) and #1284/#1382/#1383 (the
// full-screen setup wizard's router steps), driven together: since
// #1281, a router earns a place on the fleet only by presenting a token
// minted for it, and the wizard's "Mint the token" / "Paste once" steps
// are the only place that token is ever shown. What needs a real
// browser and a real listener rather than a unit test:
//
//  1. The token has to be read off the rendered page (or the mint's own
//     network response), not guessed -- proving the exact block an
//     operator would paste carries a token TryEnrol actually accepts.
//  2. The "closed port" refusal is a real TCP accept being refused
//     before any line, and before TLS -- device.Registry.AcceptsUnknown
//     and syslog.EnrolmentGate can be unit-tested in isolation, but
//     only a real connection attempt proves the listener wires them
//     together correctly.
//  3. The wizard's refused-sender box -- present only once a line has
//     actually been refused since the current token was minted -- is a
//     rendering guarantee only a real refused connection can trigger.
//
// Ported for the full-screen wizard (#1381-#1384; DESIGN.md, "3 · Paste
// once", "The router's turn" and "Adding a router, re-enrolling"),
// replacing the retired modal's
// `.setup-wizard`/`.mint-ask`/`.token-life`/`.observation.shortfall.refused`
// markup this scenario used to drive. The walk: name and address a
// router -> Mint the token -> the paste-once block, with Copy -> a
// sender from the address the operator typed is refused and the
// refused-sender box names it -> "enrol at <other> instead" (the Mint
// act again) re-mints for that address -> lines from it arrive for real
// and the track's logs station lights -> with the window spent, a third
// address is refused for having no token pending at all (the "closed
// port" case) and the mistyped original address stays refused too ->
// back on Entities, Re-enrol… reopens the ledger straight at Mint the
// token and moves the router to a fresh address for real.
//
// One router, walked end to end. Every address from 127.0.0.21 up
// belongs to this scenario alone -- nothing else in the suite mints an
// enrolment token -- so "no token is pending anywhere" (the closed-port
// assertion's own premise) can be trusted rather than merely hoped for.
//
// Dropped from the old (modal-era) scenario, with no equivalent in the
// new design:
//
//  - The "Send logs asks before it mints" pair (entering the step shows
//    a mint-ask form, the block carries no token until asked). Minting
//    is now its own step (StepMint, before the block is ever shown), so
//    by the time the paste step renders there is always already a
//    token -- there is no "ask first" state left inside it to prove.
//  - Reroll invalidating the old token when replayed from a stray
//    address (the old STALE_TOKEN_IP check). That address mismatch is
//    exactly the same refusal the wrong-sender step already proves;
//    it never actually exercised token invalidation. Reroll itself is
//    still driven below (DESIGN.md lists it as part of step 3), just
//    without a network replay that would only re-prove the address
//    gate.
//
// Re-enrolling an already-enrolled router at a new address, dropped by
// the initial port (#1398: wizardRun.begin() read `evidence.enrol`
// before wizardState.reEnrolling, so Entities' "Re-enrol..." landed on
// the finished ledger, "(tick) Where setup stands", instead of Mint the
// token), is back. 4facdf82 ("Start '+ add a router' at The router,
// whatever else is sending") fixed begin() to consult reEnrolling
// first, exactly as DESIGN.md's "Adding a router, re-enrolling"
// promises, and pinned it in Wizard.svelte.test.ts ("lands Re-enrol…
// on Mint the token even with the router already sending"). Driven for
// real below, reusing REENROL_IP as the address it moves the router to,
// the way the old (pre-port) scenario did.
//
// The same commit also fixed the other defect this port had run
// straight into (#1397): wizardRun's evidence getter borrowed the
// fleet's first open syslog source whenever wizardState.ledgerDevice
// was empty, so The router step could never render for "+ add a
// router" once any syslog source anywhere had reported -- which
// run-scenarios.sh's mandatory baseline feed guarantees before this
// scenario ever runs. wizardState.addingRouter now gates that borrow,
// so an add-a-router walk reads only its own router's evidence.
// Verified live both ways: this scenario passes end to end against a
// freshly booted instance with no prior traffic, and passes the same
// way after run-scenarios.sh's 300-line baseline feed.

import { session, feedRawFrom, check, done, goTo, waitForEventsTotal, adminPassword } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const ROUTER_NAME = 'live-enrolment-router'
// The address the operator types into The router step -- mistyped, in
// the sense that the router never actually sends from it, so it stays
// refused for the whole scenario (DESIGN.md's "if that is this router,
// its logging action is sending from another address" case).
const ENROL_IP = '127.0.0.21'
// Where the router actually sends from: refused at first (a stranger to
// the window bound to ENROL_IP), then the address the recovery re-mints
// for and the router really enrols at.
const WRONG_IP = '127.0.0.22'
// Fed only once the enrolment window has been spent -- no token pending
// anywhere -- to prove the plain "closed port" refusal independently of
// the refused-sender recovery above.
const CLOSED_PORT_IP = '127.0.0.24'
// Where Re-enrol… moves the router to, for real, at the end (DESIGN.md,
// "Adding a router, re-enrolling").
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

// text reads a locator's content collapsed to one line, the way every
// other full-screen-wizard scenario (live-setup-wizard-router-mint.mjs)
// reads its own strings: the template's line breaks are not part of
// what is being asserted.
const text = async (loc) => ((await loc.textContent()) ?? '').replace(/\s+/g, ' ').trim()

// waitForCondition polls `read` until it returns something truthy or the
// deadline passes. Needed throughout: the wizard's own refused-box
// updates on its own poll tick (5s, Wizard.svelte's POLL_MS), not on the
// syslog line that will eventually be reflected in it.
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

const wiz = page.locator('.page.wiz')
const body = wiz.locator('.body')
const foot = wiz.locator('.foot')

// --- a. Entities' berth -> Add a router -> the ledger opens at The router

await goTo(page, 'Entities')
await page.click('button.berth-trigger[aria-label="Add a router"]')
await wiz.waitFor({ state: 'visible', timeout: 10000 })
check((await page.locator('#f-name').count()) === 1, 'the ledger opens at The router')
check((await text(body.locator('h3'))) === 'Your first router.', 'with the record’s own lead')

// --- b. The router: name, address, push/backup No -- naming alone

// creates nothing yet (#1382: the record is made at the moment of
// minting, on the next step, not here).
await page.fill('#f-name', ROUTER_NAME)
await page.fill('#f-addr', ENROL_IP)
const groups = wiz.locator('.form .seg[role="radiogroup"]')
await groups.nth(0).locator('button:text-is("No")').click()
await groups.nth(1).locator('button:text-is("No")').click()
check(
  !(await devicesList()).some((d) => d.name === ROUTER_NAME),
  'naming and addressing the router creates no record yet',
)
await foot.locator('button.primary:text-is("Next")').click()

// --- c. Mint the token: the password is spent on the one call, and

// that call is what creates the device record (#1382's mint()).
await body.locator('input[type="password"]').waitFor({ timeout: 10000 })
check(
  (await text(body.locator('h3'))) === `Your password, to mint ${ROUTER_NAME}’s token.`,
  'the mint step names the router',
)
await body.locator('input[type="password"]').fill(adminPassword)
const [minted] = await Promise.all([
  page.waitForResponse((r) => /\/api\/devices\/[^/]+\/enrolment$/.test(r.url()) && r.request().method() === 'POST'),
  foot.locator('button.primary:text-is("Mint the token")').click(),
])
check(minted.status() === 201, `Mint the token is POST /api/devices/{id}/enrolment (${minted.status()})`)
let { token } = await minted.json()
check(typeof token === 'string' && token.length > 0, 'the server answers with a fresh token')

const created = (await devicesList()).find((d) => d.name === ROUTER_NAME)
check(!!created, `the record was made on the server first (${ROUTER_NAME})`)
check(created?.acceptedIp === '', `and has no acceptedIp yet (got ${JSON.stringify(created?.acceptedIp)})`)
const deviceId = created.id

// --- d. Paste once: the block, sections numbered, the enrol line last,

// and the token on the page matches the one the mint call returned --
// proving the block an operator would paste carries a token TryEnrol
// actually accepts.
const block = wiz.locator('pre[aria-label="The block to paste"]')
await block.waitFor({ timeout: 10000 })
const blockText = (await block.textContent()) ?? ''
const blockLines = blockText.trim().split('\n')
check(blockLines[0] === '# 1 · Trust the certificate', 'the certificate section is numbered first')
check(blockLines.at(-1) === `/log info "mikroview-enrol ${token}"`, 'and the enrol line, with the minted token, is last')
check(
  (await text(body.locator('.hint'))) ===
    '2 parts, in order — trust the certificate, send logs — and the enrol line last. Into the router’s terminal (WinBox ▸ New Terminal, or ssh), not a script.',
  'push and backup both No leaves a 2-part block, and it is never called a script',
)

const tokenLife = wiz.locator('.token-life')
check(
  /^Token good until \d\d:\d\d \(15 minutes\) · Reroll$/.test(await text(tokenLife)),
  `the token-life line reads plainly (got "${await text(tokenLife)}")`,
)

// --- e. Reroll: still the mint act, still before Copy -- the page's

// own token changes, still readable from the block (DESIGN.md, step 3).
await tokenLife.locator('button:text-is("Reroll")').click()
const rerollPass = body.locator('input[aria-label="Your password, to reroll the token"]')
await rerollPass.waitFor({ timeout: 5000 })
await rerollPass.fill(adminPassword)
const [rerolled] = await Promise.all([
  page.waitForResponse((r) => /\/api\/devices\/[^/]+\/enrolment$/.test(r.url()) && r.request().method() === 'POST'),
  rerollPass.press('Enter'),
])
token = (await rerolled.json()).token
const rerolledLine = await waitForCondition(async () => {
  const last = ((await block.textContent()) ?? '').trim().split('\n').at(-1)
  return last?.includes(token) ? last : null
}, 10000)
check(!!rerolledLine, `Reroll changes the token on the page (now ends in "${token}")`)

// --- f. Copy: the router's turn begins ---------------------------------

await body.locator('button.primary:text-is("Copy")').click()
// waitForCondition returns null on timeout rather than throwing, so the
// result must be checked -- an unconditional pass here would say nothing
// about whatever heading was actually showing.
const copyStarted = await waitForCondition(async () => ((await text(body.locator('h3'))) === 'The router’s turn.' ? true : null), 5000)
check(!!copyStarted, 'Copy starts the router’s turn')

// Standing in for the router's own `/tool fetch` of the first section of
// the block: this harness drives a real browser and a real listener but
// not a real RouterOS device (the AGENTS.md invariant -- MikroView never
// connects to a router, and nothing here can make one connect to
// MikroView either), so the certificate leg is exercised the same way
// live-journey.mjs already does, a plain GET of the public endpoint the
// pasted command would have hit.
await page.request.get(`${URL_BASE}/ca.crt`)
const track = wiz.locator('.track')
const certLit = await waitForCondition(async () => {
  const cls = (await track.locator('.stn').nth(1).getAttribute('class')) ?? ''
  return /\bdone\b/.test(cls) ? cls : null
}, 10000)
check(!!certLit, 'the certificate station lights once /ca.crt is fetched')

// --- g. Wrong sender: refused, and the box names it ---------------------

// The window bound to ENROL_IP, so a line from WRONG_IP is turned away
// at accept -- before TLS, before a byte reaches the parser.
let wrongRefusedAtConnect = false
try {
  feedRawFrom(WRONG_IP, plainLine('live-enrolment-wrong'))
} catch {
  wrongRefusedAtConnect = true
}
check(wrongRefusedAtConnect, `while the window is bound to ${ENROL_IP}, a connection from ${WRONG_IP} is refused at accept`)

const refusedAfterWrong = await waitForCondition(async () => {
  const list = await refusedList()
  return list.some((r) => r.ip === WRONG_IP) ? list : null
})
check(!!refusedAfterWrong, `GET /api/devices/refused carries ${WRONG_IP}`)

const warnbox = wiz.locator('.warnbox')
const warnboxText = await waitForCondition(async () => {
  if ((await warnbox.count()) === 0) return null
  const t = await text(warnbox)
  return t.includes(WRONG_IP) ? t : null
}, 25000)
check(!!warnboxText, `the refused-sender box names ${WRONG_IP} -- got "${warnboxText}"`)
check(
  (warnboxText ?? '').includes(`Lines from ${WRONG_IP} arrived without the enrol line and were refused.`),
  'in the record’s own wording',
)
const fixLine = ((await warnbox.locator('pre').textContent()) ?? '').trim().split('\n').at(-1)
check(/^\/log info "mikroview-enrol [a-z0-9]{20}"$/.test(fixLine ?? ''), `the box's fix block ends with a fresh enrol line (got "${fixLine}")`)
check(
  (await text(wiz.locator('.bar .chips .att.alarm'))) === `refused · ${WRONG_IP}`,
  'and the bar wears a refused chip for it',
)

// --- h. "enrol at <other> instead": the Mint act again, for WRONG_IP ----

await warnbox.locator(`button:text-is("enrol at ${WRONG_IP} instead")`).click()
const useOtherPass = body.locator(`input[aria-label="Your password, to enrol at ${WRONG_IP} instead"]`)
await useOtherPass.waitFor({ timeout: 5000 })
await useOtherPass.fill(adminPassword)
const [reminted] = await Promise.all([
  page.waitForResponse((r) => /\/api\/devices\/[^/]+\/enrolment$/.test(r.url()) && r.request().method() === 'POST'),
  useOtherPass.press('Enter'),
])
check(reminted.status() === 201, `"enrol at ${WRONG_IP} instead" is the same mint call (${reminted.status()})`)
const remintBody = await reminted.request().postDataJSON()
check(remintBody.expectedAddress === WRONG_IP, `re-minted for the address that actually sent (${remintBody.expectedAddress})`)
token = (await reminted.json()).token
check(typeof token === 'string' && token.length > 0, 'and a fresh token comes back')

await warnbox.waitFor({ state: 'detached', timeout: 10000 }).catch(() => {})
check((await warnbox.count()) === 0, 'the refused-sender box clears once the walk’s own "since" moves to now')

// --- i. Lines from the new address arrive for real -----------------------

const before = await (async () => (await api('GET', '/api/stats')).body?.total ?? 0)()
try {
  feedRawFrom(WRONG_IP, enrolLine(token))
} catch (e) {
  check(false, `the enrol line from ${WRONG_IP} should be accepted at connect: ${e}`)
}

const enrolled = await waitForCondition(async () => {
  const d = (await devicesList()).find((dv) => dv.id === deviceId)
  return d?.acceptedIp === WRONG_IP ? d : null
})
check(!!enrolled, `the device's acceptedIp becomes ${WRONG_IP} within a few seconds`)

const logsStationDone = await waitForCondition(async () => {
  const cls = (await track.locator('.stn').nth(2).getAttribute('class')) ?? ''
  const lab = (await track.locator('.stn').nth(2).locator('.lab').textContent()) ?? ''
  return /\bdone\b/.test(cls) && lab === 'logs' ? cls : null
}, 15000)
check(!!logsStationDone, `the track's logs station lights once the enrol line lands (class="${logsStationDone}")`)

const stillRefused = await refusedList()
check(!stillRefused.some((r) => r.ip === WRONG_IP), `${WRONG_IP} comes off the refused list once it enrols`)

try {
  feedRawFrom(WRONG_IP, plainLine('live-enrolment-accepted'))
  const after = await waitForEventsTotal(page, before + 1)
  check(after >= before + 1, `a plain line from the now-enrolled ${WRONG_IP} is accepted (events ${before} -> ${after})`)
} catch (e) {
  check(false, `a plain line from ${WRONG_IP} should now be accepted: ${e}`)
}

// waitForCondition returns null on timeout rather than throwing, so the
// result must be checked -- an unconditional pass here would say nothing
// about whatever heading was actually showing.
const sendingArrived = await waitForCondition(async () => ((await text(body.locator('h3'))) === `${ROUTER_NAME} is sending.` ? true : null), 10000)
check(!!sendingArrived, 'everything chosen (cert, logs; push and backup both left dark) has arrived')

// --- j. Closed port: the window is spent, so a stranger is refused too --

let closedAtConnect = false
try {
  feedRawFrom(CLOSED_PORT_IP, plainLine('live-enrolment-closed'))
} catch {
  closedAtConnect = true
}
check(closedAtConnect, `with no token pending anywhere, a connection from ${CLOSED_PORT_IP} is refused at accept`)

let oldAddressRefused = false
try {
  feedRawFrom(ENROL_IP, plainLine('live-enrolment-mistyped'))
} catch {
  oldAddressRefused = true
}
check(oldAddressRefused, `and the originally-typed ${ENROL_IP}, which the router never actually sent from, stays refused too`)

const refusedFinal = await waitForCondition(async () => {
  const list = await refusedList()
  return list.some((r) => r.ip === CLOSED_PORT_IP) && list.some((r) => r.ip === ENROL_IP) ? list : null
})
check(!!refusedFinal, `GET /api/devices/refused carries both ${CLOSED_PORT_IP} and ${ENROL_IP}`)

// --- k. Leave the wizard the only way it offers: forward, to Finish ------

// The full-screen wizard has no close button (DESIGN.md's modal was
// retired with it): the way out is Next once everything chosen has
// arrived, Skip the rules step (this scenario tags nothing), Finish.
await foot.locator('button.primary:text-is("Next")').click()
await foot.locator('button:text-is("Skip this step")').click()
await foot.locator('button.primary:text-is("Finish")').click()
await wiz.waitFor({ state: 'hidden', timeout: 10000 })
// safe: waitFor above throws on timeout
check(true, 'Finish leaves the wizard (admin lands back on Entities -- deckCards.ts’s shared card)')

// --- l. Entities: the refused cards, and no accept control on them ------

await goTo(page, 'Entities')
for (const ip of [CLOSED_PORT_IP, ENROL_IP]) {
  const refusedCard = page.locator('.fcard.refused', { hasText: ip })
  const refusedCardText = await waitForCondition(async () => {
    if ((await refusedCard.count()) === 0) return null
    return (await refusedCard.first().textContent()) ?? null
  }, 20000)
  check(!!refusedCardText, `Entities draws a refused card for ${ip} -- got "${refusedCardText}"`)
  check((refusedCardText ?? '').includes('no router is enrolled at'), 'the card says what it is rather than diagnosing whose it is')
  check((await refusedCard.first().locator('button').count()) === 0, 'and carries no control that would accept it, by ruling')
}
check(
  !(await page.locator('.fcard.refused', { hasText: WRONG_IP }).count()),
  `and none for ${WRONG_IP}, which enrolled`,
)
check(
  (await page.locator(`.fcard button.row-action[aria-label^="Re-enrol ${ROUTER_NAME}"]`).count()) === 1,
  'the router’s own card offers Re-enrol…',
)

// --- m. Re-enrol: move the router to a new address -----------------------

// Entities' own Re-enrol… (DESIGN.md, "Adding a router, re-enrolling"):
// the ledger reopens at Mint the token for this router, with a fresh
// token -- #1398, fixed by wizardRun.begin() consulting
// wizardState.reEnrolling before evidence.enrol (see the header
// comment).
await page.click(`.fcard button.row-action[aria-label^="Re-enrol ${ROUTER_NAME}"]`)
await wiz.waitFor({ state: 'visible', timeout: 10000 })
check((await page.locator('#f-name').count()) === 0, 'Re-enrol… skips The router -- there is nothing left to name')
check(
  (await text(body.locator('h3'))) === `Your password, to mint ${ROUTER_NAME}’s token.`,
  'and lands straight on Mint the token, not the finished ledger (#1398)',
)

// Back still reaches The router: Mint's own 'ask' stage is not the
// watch/tune/done a first-time walk reaches once its own paste has
// landed (railRows' `can`), so the run is still the operator's to
// change -- move the router to a new address before minting, the way
// an operator whose router changed address would.
await foot.locator('button:text-is("Back")').click()
await page.locator('#f-addr').waitFor({ timeout: 5000 })
check((await page.locator('#f-addr').inputValue()) === WRONG_IP, `The router remembers where it stands (${WRONG_IP})`)
await page.fill('#f-addr', REENROL_IP)
await foot.locator('button.primary:text-is("Next")').click()
await body.locator('input[type="password"]').waitFor({ timeout: 10000 })
check((await text(body.locator('.hint'))).includes(REENROL_IP), `Mint now names the new address (${REENROL_IP})`)

await body.locator('input[type="password"]').fill(adminPassword)
const [reenrolMinted] = await Promise.all([
  page.waitForResponse((r) => /\/api\/devices\/[^/]+\/enrolment$/.test(r.url()) && r.request().method() === 'POST'),
  foot.locator('button.primary:text-is("Mint the token")').click(),
])
check(reenrolMinted.status() === 201, `Re-enrol…’s Mint the token is the same call (${reenrolMinted.status()})`)
const reenrolMintBody = await reenrolMinted.request().postDataJSON()
check(reenrolMintBody.expectedAddress === REENROL_IP, `bound to the new address (${reenrolMintBody.expectedAddress})`)
const { token: reenrolToken } = await reenrolMinted.json()
check(typeof reenrolToken === 'string' && reenrolToken.length > 0, 'and a fresh token comes back')

try {
  feedRawFrom(REENROL_IP, enrolLine(reenrolToken))
} catch (e) {
  check(false, `the enrol line from ${REENROL_IP} should be accepted at connect: ${e}`)
}

const reenrolled = await waitForCondition(async () => {
  const d = (await devicesList()).find((dv) => dv.id === deviceId)
  return d?.acceptedIp === REENROL_IP ? d : null
})
check(!!reenrolled, `the device's acceptedIp becomes ${REENROL_IP} within a few seconds`)

let movedFromRefused = false
try {
  feedRawFrom(WRONG_IP, plainLine('live-enrolment-moved'))
} catch {
  movedFromRefused = true
}
if (!movedFromRefused) movedFromRefused = (await refusedList()).some((r) => r.ip === WRONG_IP)
check(movedFromRefused, `${WRONG_IP}'s lines are refused now that the router has moved to ${REENROL_IP}`)

// --- n. Clean up: leave the fleet the way every other scenario finds it -

const del = await api('DELETE', `/api/devices/${encodeURIComponent(deviceId)}`)
check(del.status === 204, `the scenario's device is deleted (${del.status})`)
const remaining = await devicesList()
check(!remaining.some((d) => d.id === deviceId), 'no trace of it remains for later scenarios')

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
