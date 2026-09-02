// SPDX-License-Identifier: AGPL-3.0-only
//
// #802, round 36's degraded map: with no `/ip address` table pushed, the
// router card carries one statement in place of its pushed-table facts,
// and every address slot on the map says what it truly holds. Nothing
// floats over the drawing.
//
// Sorts BEFORE every other live-topography-*.mjs scenario on purpose,
// and that ordering is the scenario. The degraded state is "no address
// table has been pushed yet", and every one of those siblings pushes a
// whole `ip-address` table; once any of them has run, this state cannot
// be recovered on a shared instance (a push replaces its kind's table,
// it does not clear it). Renaming this file to sort later silently
// turns the first half below into an assertion about a pushed map.
//
// The table it pushes at the end is deliberately the same one
// live-topography-coverage.mjs pushes for itself unconditionally, so
// what this leaves behind is what the next scenario would have set up
// anyway.

import { session, check, done, feedRaw } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Two real lines: one arriving on ether1 from a public source, which is
// what makes ether1 the wan interface (an observation, never a probe --
// zones.svelte.ts), and one leaving bridge1, which is the lane.
for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info D|slots-wan-in| forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 203.0.113.9:5${100 + i}->192.168.1.60:443, len 60`)
  feedRaw(`firewall,info A|slots-web| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.60:5${200 + i}->203.0.113.9:443, len 60`)
}

let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-address-slots', kind: 'ingest', device: DEVICE },
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

// The address table really is absent, rather than merely not drawn --
// the same endpoint the map itself reads (api.ts's fetchRouterAddresses).
const before = await page.request.get(`${URL_BASE}/api/routeros/${encodeURIComponent(DEVICE)}/addresses`)
const beforeBody = before.ok() ? await before.json() : null
check(
  beforeBody !== null && (!beforeBody.available || (beforeBody.rules ?? []).length === 0),
  `no /ip address table has been pushed yet — the degraded state is real, not staged (${before.status()} ${JSON.stringify(beforeBody)})`,
)

await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

const topo = '[data-card="topography"]'

// --- the one statement, on the router card ----------------------------------

const degLines = await page.locator(`${topo} .deg-t`).allTextContents()
check(
  degLines.length === 2 && degLines[0].trim() === 'no address table pushed — zones from boundaries',
  `the router card names the missing push (${JSON.stringify(degLines)})`,
)
check(degLines[1]?.trim() === 'Run setup… ▸ adds it', 'and names what adds it')

check(
  (await page.locator(`${topo} .isl.waist`).getAttribute('height')) === '100',
  'the card grew to hold the statement rather than the text overrunning it',
)
check((await page.locator(`${topo} .degraded`).count()) === 0, 'no note floats over the drawing')

// The statement is inside the waist island's own group, not loose on the
// stage: the drawing puts it where the card's other pushed-table facts sit.
check(
  (await page.locator(`${topo} .isl-card:has(.isl.waist) .deg-t`).count()) === 2,
  'both lines sit on the router card itself',
)

// --- every address slot reads honestly --------------------------------------

const zoneSlots = await page.locator(`${topo} .zone .n-cidr`).allTextContents()
check(
  zoneSlots.length > 0 && zoneSlots.every((t) => t.trim() === 'from boundaries'),
  `every zone card reads "from boundaries" instead of a CIDR (${JSON.stringify(zoneSlots)})`,
)

const internetText = (await page.locator(`${topo} .isl-card:has-text("Internet")`).first().textContent()) ?? ''
check(
  internetText.replace(/\s+/g, ' ').includes('ether1 · no address pushed'),
  `the wan card's own slot says no address was pushed (${internetText.replace(/\s+/g, ' ').trim()})`,
)

// --- the way in is real, not just named -------------------------------------

await page.click(`${topo} .deg-go`)
await page.waitForSelector('.setup-wizard', { timeout: 5000 })
check(true, 'the statement\'s "Run setup… ▸" opens the setup wizard')
// A reload rather than the modal's own close: the wizard opens at the
// first step still waiting, and on an instance where nothing is waiting
// that is the finish pane, whose close leaves for the landing card. The
// scenario is not testing the wizard, only the way in.
await page.reload()
await page.waitForSelector('.setup-wizard', { state: 'hidden', timeout: 10000 })

// --- with a table pushed, the card returns to its normal state --------------

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' },
      { address: '10.9.0.1/24', network: '10.9.0.0', interface: 'ether5', comment: 'The quiet lane' },
    ],
  })) === 200,
  'the zone table is pushed whole',
)

await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector(`${topo} .zone`, { timeout: 10000 })
await page.waitForSelector(`${topo} .deg-t`, { state: 'hidden', timeout: 10000 })

check((await page.locator(`${topo} .deg-t`).count()) === 0, 'the statement is gone — no leftover note')
check((await page.locator(`${topo} .zone .deg-slot`).count()) === 0, 'no zone slot still says "from boundaries"')
check(
  (await page.locator(`${topo} .isl.waist`).getAttribute('height')) === '68',
  'the router card is back to its resting height',
)
const pushedSlots = await page.locator(`${topo} .zone .n-cidr`).allTextContents()
check(
  pushedSlots.some((t) => t.trim() === '192.168.1.1/24'),
  `the pushed CIDR is what the slot now holds (${JSON.stringify(pushedSlots)})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
