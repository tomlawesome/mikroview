// SPDX-License-Identifier: AGPL-3.0-only
//
// #786: Try on the watchers editor, driven in a real browser against a
// real instance.
//
// Why this cannot be a component test. A receipt is an answer about the
// traffic the running instance is actually holding: the count, the
// window it was counted over and the hosts named in it all come out of
// the event ring, through the definition's own evaluation logic, with
// the candidate numbers substituted for its stored ones. A mocked
// replay agrees with whatever shape the component asks for, so the two
// failures this really produces -- a candidate the server refuses
// because the panel sent params its replay does not accept, and a slot
// that renders a decline as though it were a receipt of zero -- are
// invisible from either end alone.
//
// Both answers are exercised, because they are different answers and not
// two degrees of the same one: a receipt over a corpus long enough to
// judge, and a decline when the candidate window is longer than the
// traffic held.
//
// Shares one instance with every other scenario in this directory. It
// presses Try, never Save, and cancels out of the panel, so it leaves
// port_scan exactly as it found it -- and it checks that from the
// server, since "a trial writes nothing" is the property most worth
// pinning here.

import { session, check, done, goTo, feedSyslog, feedPortScan } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const SCAN_SOURCE = '198.51.100.86'

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

const tidy = (s) => (s ?? '').replace(/\s+/g, ' ').trim()

// --- traffic worth replaying over ---------------------------------------
//
// Two feeds with a gap between them, not one: the corpus has to *span*
// more than the candidate window below, and every line of a single feed
// arrives down one connection within the same instant.

feedSyslog(40, 'live-watchers-try')
await page.waitForTimeout(2500)
feedPortScan(25, SCAN_SOURCE)
await page.waitForTimeout(1500)

// --- what the server says the corpus covers -----------------------------
//
// Asked here rather than assumed, and used to pick the window that must
// decline further down: a suite run's instance can be the better part of
// an hour old by the time this scenario runs, so a hardcoded "an hour
// must be longer than the corpus" would rot into a flake.

const probe = await api('POST', '/api/definitions/port_scan/replay', {
  params: { window: '1s', threshold: 2 },
})
check(probe.status === 200, `a replay over the retained corpus answers 200 (${probe.status})`)
check(
  Boolean(probe.body?.receipt),
  `the corpus is long enough for a one-second window to be judged (${JSON.stringify(probe.body).slice(0, 160)})`,
)
const corpusSeconds =
  (new Date(probe.body?.receipt?.window?.end).getTime() -
    new Date(probe.body?.receipt?.window?.start).getTime()) /
  1000
check(corpusSeconds > 0, `the receipt states the window it covered (${corpusSeconds}s)`)

// --- open the row -------------------------------------------------------

await goTo(page, 'Settings')
await page.click('.olink:has-text("tune")')
await page.waitForSelector('.bench .row')

const row = page.locator('.bench li.row:has(.id:text-is("port_scan"))')
await row.locator('.row-knob').click()
await row.locator('.panel').waitFor({ state: 'visible' })

const before = await api('GET', '/api/definitions/port_scan')
check(before.status === 200, `port_scan reads back before anything is tried (${before.status})`)

const thresholdField = row.locator('.panel input[type="number"]').first()
const windowField = row.locator('.panel input[type="number"]').nth(1)

check((await row.locator('.panel .try').count()) === 1, 'Try sits at the foot of the open panel')
check(
  (await row.locator('.panel .tried').count()) === 0,
  'the slot is empty until something has been tried',
)

// --- a replay that fires ------------------------------------------------
//
// Two distinct ports inside a second, from a source that just sent
// twenty-five of them.

await thresholdField.fill('2')
await windowField.fill('1')
await row.locator('.panel .try').click()
await row.locator('.panel .tried-count').waitFor({ state: 'visible', timeout: 20000 })

const receiptLine = tidy(await row.locator('.panel .tried-count').textContent())
check(
  /^Would have fired (at least )?\d+ times? in the last .+$/.test(receiptLine),
  `the receipt says what would have fired, over the window it was counted across (${JSON.stringify(receiptLine)})`,
)
check(
  !/ 0 times/.test(receiptLine),
  `the candidate numbers really did fire over this corpus (${JSON.stringify(receiptLine)})`,
)

// The count the detector makes as it stands, measured by the server over
// the same corpus in the same request (#786). Either form is a pass: over
// a corpus this short the live 60s window can honestly decline, and that
// is the answer rather than a missing one -- what must never happen is
// silence, which would leave the candidate's number with nothing to be
// read against.
const currentLine = tidy(await row.locator('.panel .tried-current').textContent())
check(
  /^currently: (at least )?\d+$|^currently: not replayable over the traffic held \(.+\)$/.test(
    currentLine,
  ),
  `the candidate's count is shown against the detector's current one (${JSON.stringify(currentLine)})`,
)

const hosts = tidy(await row.locator('.panel .tried-hosts').textContent())
check(
  hosts.includes(SCAN_SOURCE),
  `the flagged hosts are listed beneath, and include the source that scanned (${JSON.stringify(hosts)})`,
)

// The receipt is the panel's answer, not the row's problem: nothing about
// a successful Try belongs in the error line.
check(
  (await row.locator('.error').count()) === 0,
  'a receipt is not reported as an error on the row',
)

// --- a trial is not an edit ---------------------------------------------

const afterTry = await api('GET', '/api/definitions/port_scan')
check(
  JSON.stringify(afterTry.body?.params) === JSON.stringify(before.body?.params),
  `Try wrote nothing: the definition the engine evaluates still has ${JSON.stringify(afterTry.body?.params)}`,
)
check(
  Object.keys(afterTry.body?.distance ?? {}).length ===
    Object.keys(before.body?.distance ?? {}).length,
  'a replay left the definition exactly as far from stock as it already was',
)
check(
  await row.locator('.panel .save').isEnabled(),
  'Save is still there to press -- Try never blocks it',
)

// --- a replay that declines ---------------------------------------------
//
// A window several times longer than the corpus actually holds. Not an
// error and not a receipt of zero: the honest answer is that the question
// cannot be asked of this much traffic yet.

const decliningWindow = Math.max(3600, Math.ceil(corpusSeconds * 4))
await windowField.fill(String(decliningWindow))
await row.locator('.panel .try').click()
await row.locator('.panel .declined').waitFor({ state: 'visible', timeout: 20000 })

const declineLine = tidy(await row.locator('.panel .declined').textContent())
check(
  /^Can't replay: needs a .+ window, only .+ held$/.test(declineLine),
  `the decline says what it needed and what is held (${JSON.stringify(declineLine)})`,
)
check(
  (await row.locator('.panel .tried-count').count()) === 0,
  'a decline replaces the receipt in the same slot rather than sitting beside it',
)
check(
  (await row.locator('.error').count()) === 0,
  'a decline is an honest limit, not an error on the row',
)
check(
  await row.locator('.panel .save').isEnabled(),
  'a decline does not block Save either',
)

// --- leave the bench as it was found ------------------------------------

await row.locator('.panel .cancel').click()
await row.locator('.panel').waitFor({ state: 'detached' })

const final = await api('GET', '/api/definitions/port_scan')
check(
  JSON.stringify(final.body?.params) === JSON.stringify(before.body?.params),
  `this scenario left port_scan as it found it (${JSON.stringify(final.body?.params)})`,
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
