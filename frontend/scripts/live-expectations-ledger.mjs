// SPDX-License-Identifier: AGPL-3.0-only
//
// #640 part C: the expectations ledger under the bench on the watchers
// station, driven in a real browser against a real instance.
//
// Why this cannot be a component test. The ledger's whole claim is that
// it shows what the *store* has been told is normal here -- the size a
// real firing recorded, and the entry actually going away when Forget
// is pressed. A mocked list agrees with whatever the component asks for,
// so the two failures this feature really produces -- a row rendering a
// size the server never recorded, and a Forget that empties the list on
// screen without the expectation being gone -- are invisible from
// either end alone. Every assertion below therefore reads the server's
// own answer after driving the browser's control.
//
// Shares one instance with every other scenario in this directory, so it
// uses a source address nothing else here feeds and asserts about that
// address rather than about the ledger being globally empty.

import { session, check, done, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// Unused by every other scenario in this directory (checked against
// every 198.51.100.* literal already in use here before picking it).
const SCAN_IP = '198.51.100.108'

feedPortScan(20, SCAN_IP)

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

async function expectations() {
  const res = await api('GET', '/api/flags/expectations')
  return { status: res.status, list: res.body?.expectations ?? [] }
}

// --- raise a real flag, and judge it ------------------------------------
//
// Server-side first (#354): a locator timeout later cannot say whether
// the scan never raised a flag at all or just has not rendered yet.

const raised = await waitForFlag(page, SCAN_IP)
check(raised.ok, raised.message)

const flags = await api('GET', '/api/flags')
const flag = (flags.body?.flags ?? []).find((f) => f.target === SCAN_IP)
check(flag !== undefined, `the port scan is on the server's flag list (${SCAN_IP})`)
check(
  typeof flag?.size === 'number' && flag.size > 0,
  `port_scan declares a size, so the flag carries the measure an expectation records (${flag?.size})`,
)

const verdict = await api('POST', `/api/flags/${encodeURIComponent(flag.id)}/verdict`, {
  verdict: 'expected',
})
check(verdict.status === 200, `the operator calls it expected (${verdict.status})`)

// The Expected verdict is what records the expectation (#640 part B),
// so the ledger below is driven against a genuinely recorded entry
// carrying a real firing's size, not one inserted by hand.
const afterRecord = await expectations()
check(afterRecord.status === 200, `GET /api/flags/expectations answers 200 (${afterRecord.status})`)
const entry = afterRecord.list.find((e) => e.target === SCAN_IP)
check(entry !== undefined, `the expectation is on the ledger endpoint (${JSON.stringify(afterRecord.list.map((e) => e.target))})`)
check(
  entry?.size === flag.size,
  `it recorded the firing's own size, not a number typed in (ledger says ${entry?.size}, the flag was ${flag.size})`,
)

// --- the ledger on the watchers station ---------------------------------

await goTo(page, 'Settings')
await page.click('.olink:has-text("tune")')
await page.waitForSelector('.bench .row')

const ledger = page.locator('.expectations')
await ledger.waitFor({ state: 'visible', timeout: 15000 })
check(
  ((await ledger.locator('h3').textContent()) ?? '').trim() === 'What it has been told to expect',
  'the station carries the ledger under the bench, headed in its own words',
)

const row = ledger.locator(`li.erow:has-text("${SCAN_IP}")`)
await row.waitFor({ timeout: 15000 })
check((await row.count()) === 1, `the expectation has exactly one row on the ledger (${SCAN_IP})`)

const rowText = ((await row.textContent()) ?? '').replace(/\s+/g, ' ').trim()
check(
  rowText.includes(`up to ${flag.size}`),
  `the row states the size the server recorded (${JSON.stringify(rowText)})`,
)
check(rowText.includes('absorbed '), `the row states how many firings it has absorbed (${JSON.stringify(rowText)})`)
check(rowText.includes('since '), `the row states when the expectation was made (${JSON.stringify(rowText)})`)

// --- forget it ----------------------------------------------------------

await row.locator('button.forget').click()
await row.waitFor({ state: 'detached', timeout: 15000 })
check(true, 'Forget removes the row from the ledger')

// The row vanishing is not the claim -- the expectation being gone is.
const afterForget = await expectations()
check(
  !afterForget.list.some((e) => e.target === SCAN_IP),
  `the expectation is gone server-side too, not just optimistically hidden (${JSON.stringify(afterForget.list.map((e) => e.target))})`,
)

// Asserted about this scenario's own address rather than the whole list
// being empty: every scenario here shares one instance, and a sibling
// leaving an entry behind is not this check's failure to report.
check(
  (await ledger.locator(`li.erow:has-text("${SCAN_IP}")`).count()) === 0,
  'and the reloaded ledger agrees',
)

// A forgotten pair must be able to flag again -- that is the whole point
// of forgetting one.
feedPortScan(20, SCAN_IP)
const refired = await waitForFlag(page, SCAN_IP)
check(refired.ok, `forgetting an expectation re-arms detection: ${refired.message}`)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
