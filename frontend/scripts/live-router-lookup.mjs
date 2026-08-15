// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #186 step 4's UI: the rule lookup button on event rows, backed
// by a table genuinely pushed through POST /api/ingest/routeros with a
// real ingest token -- not seeded into the store directly. Covers the
// three honest popover states in order: no table pushed yet, a pushed
// table with no matching prefix, and a shared prefix resolving to
// several rules. Also the naming half: a DNS static entry pushed by the
// "router" names that address in the event rows, since RouterOS-pushed
// names win at ingest time.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page } = await session()

// The ingest token must be scoped to exactly the device the event rows
// carry, or the pushed tables attach to a different device and every
// lookup below reports "nothing pushed".
//
// Discovered, not assumed. This was hardcoded to 127.0.0.1, which is
// what the syslog feeder's source address happens to be under
// live-env.sh -- and is not what it is inside a container, where lines
// arrive from the Docker gateway. Both #186's table lookups and the
// per-device host-name scoping added in #289 key on this, so a scenario
// that guesses it wrong fails in a way that looks like a product defect.
// Asking the running instance which device it saw is correct everywhere.
syslog(2, 'device-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-router-lookup', kind: 'ingest', device: DEVICE },
})
check(tokenRes.status() === 201, `an ingest token is issued (${tokenRes.status()})`)
const token = (await tokenRes.json()).value

async function push(payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

// --- State 1: events exist, nothing pushed yet ---------------------------

syslog(3, 'live-lookup-rule')

// The live view is the default view -- rows appear as the events land.
await page.waitForSelector('.cell.rule .investigate', { timeout: 15000 })

await page.click('.cell.rule .investigate >> nth=0')
await page.waitForSelector('.popover', { timeout: 5000 })
check(
  (await page.textContent('.popover')).includes('No rule table pushed'),
  'before any push, the popover says no table has been pushed -- not an empty table',
)
await page.keyboard.press('Escape')
check(!(await page.isVisible('.popover')), 'Escape closes the popover')

// --- Push the tables and a DNS name through the real endpoint ------------

check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    records: [
      { ordinal: 2, comment: 'Drop the scanners', chain: 'input', action: 'drop', srcAddressList: 'scanners', logPrefix: 'D|live-lookup-rule|' },
      { ordinal: 5, comment: 'Belt and braces drop', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|live-lookup-rule|' },
      { ordinal: 0, comment: 'Unrelated allow', chain: 'forward', action: 'accept', srcAddressList: 'lan', logPrefix: '' },
    ],
  })) === 200,
  'the filter-rule table is accepted through the real ingest endpoint',
)
check(
  (await push({
    kind: 'dns-static',
    page: 1,
    pages: 1,
    records: [{ name: 'camera.lan', address: '203.0.113.2' }],
  })) === 200,
  'a DNS static entry is accepted',
)

// --- State 2: a label no pushed rule carries -----------------------------
// Distinct events whose label matches nothing in the pushed table --
// targeted via the button's own accessible title, so it can't
// accidentally hit a matching row's button.

syslog(2, 'unmatched-rule')
await page.waitForSelector('button[title="Look up rule for prefix unmatched-rule"]', { timeout: 15000 })
await page.click('button[title="Look up rule for prefix unmatched-rule"] >> nth=0')
await page.waitForSelector('.popover', { timeout: 5000 })
{
  const text = await page.textContent('.popover')
  check(
    text.includes('No rule in the pushed table'),
    'a label with no matching prefix reports exactly that, with the table size, not "no data"',
  )
}
await page.keyboard.press('Escape')

// --- State 3: fresh events whose label two pushed rules carry ------------
// Also the naming assertion: 203.0.113.2 (src of the 3rd event, i%250=2)
// was just named camera.lan by the pushed DNS entry, and names resolve at
// ingest time -- so these NEW events must show the router-pushed name.

syslog(5, 'live-lookup-rule')
await page.waitForSelector('.addr-btn:has-text("camera.lan")', { timeout: 15000 })
check(true, 'a router-pushed DNS name labels the address in new event rows (RouterOS wins)')

await page.click('button[title="Look up rule for prefix live-lookup-rule"] >> nth=0')
await page.waitForSelector('.popover .entry', { timeout: 5000 })
{
  const text = await page.textContent('.popover')
  check(text.includes('#2') && text.includes('Drop the scanners'), 'the matching rule shows its RouterOS ordinal and comment')
  check(text.includes('#5') && text.includes('Belt and braces drop'), 'a shared prefix honestly shows every matching rule')
  check(!text.includes('Unrelated allow'), 'rules with other prefixes stay out of the match')
  check(text.includes('src-address-list: scanners'), 'the src-address-list detail is shown when set')
}
await page.keyboard.press('Escape')

done()
