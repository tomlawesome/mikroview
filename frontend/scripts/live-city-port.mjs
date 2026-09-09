// SPDX-License-Identifier: AGPL-3.0-only
//
// #1055 (round 54): the port filter, wired into the city -- the same
// pill, the same store, the same answer #1018 built for the flat map,
// now read at city altitude too.
//
// live-topography-port-trace.mjs already proves the pill, the picker
// and the trace on the flat map, against real pushed tables and real
// logged lines. What round 54 changed is that the city reads the same
// store rather than needing its own: the pill collapses the same way
// wherever the slider sits, a district plaque grows the filter's own
// tally line under itself, and a pushed rule's door stands in the
// city's own gate. A unit test can render a ground plan with a filter
// answer handed to it by hand; only this can show the pill, the plaque
// and the door agreeing with a real answer from a real endpoint, at the
// altitude an operator actually lands on (city is the slider's default,
// #869).
//
// The estate is the shared one several live-city-*.mjs scenarios
// already declare -- bridge-lan/vlan-srv/vlan-iot/vlan-guest, named LAN,
// Servers, IoT and Guest -- so the districts this scenario needs already
// have roads and gates between them by the time it runs. This file
// pushes its own filter-rule table and its own traffic on top, naming
// two ports: 445, which the traffic below actually crosses, and 3389,
// which only a rule names -- the scenario's own nothing-seen case, the
// same shape live-topography-port-trace.mjs uses for 3389 on the flat
// map.
//
// Runs early in the `city` family (alphabetically before -declared and
// -marks... actually after those two, before -reach/-river/-slider/
// -stops/-walls), so its own assertions land before any sibling adds
// traffic of its own to the shared estate.
import { session, check, done, feedRaw } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.CITY_PORT_SHOTS || '/tmp/1055-shots'
mkdirSync(OUT, { recursive: true })

const CARD = '[data-card="topography"]'

const { page, consoleErrors } = await session()

let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-city-port', kind: 'ingest', device: DEVICE },
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

// The shared city estate (live-city-reach.mjs, live-city-stops.mjs,
// live-city-walls.mjs all declare the same four lanes) -- so the
// districts and the gates between them already exist by the time this
// scenario runs.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge-lan', comment: 'LAN' },
      { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'vlan-srv', comment: 'Servers' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'vlan-iot', comment: 'IoT' },
      { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'vlan-guest', comment: 'Guest' },
    ],
  })) === 200,
  'the shared city estate is pushed',
)

// One rule names 445 and is what the traffic below actually crosses --
// LAN to Servers, accepted -- the door this scenario expects open. The
// other names 3389, and nothing is ever sent to it: the nothing-seen
// case below picks this one.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 54,
        chain: 'forward',
        action: 'accept',
        protocol: 'tcp',
        dstPort: 445,
        inInterface: 'bridge-lan',
        outInterface: 'vlan-srv',
        log: true,
        comment: 'SMB to the servers',
      },
      {
        ordinal: 55,
        chain: 'input',
        action: 'drop',
        protocol: 'tcp',
        dstPort: 3389,
        inInterface: 'vlan-guest',
        log: true,
        comment: 'RDP blocked from guest',
      },
    ],
  })) === 200,
  'the filter table is pushed, one rule naming 445 and one naming 3389',
)

// Real SMB traffic, LAN to Servers, accepted: 445/tcp is what this
// scenario filters to. 3389 is named by a rule above and never crossed.
for (const [i, host] of [61, 62, 63].entries()) {
  feedRaw(
    `firewall,info A|city-smb| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), 10.0.10.${host}:5${100 + i}->10.0.40.61:445, len 60`,
  )
}

await new Promise((r) => setTimeout(r, 1500))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector(`${CARD} .altitude input[type="range"]`, { timeout: 15000 })
await page.locator(`${CARD} .altitude input[type="range"]`).fill('3') // the city stop (#869)
await page.waitForSelector(`${CARD} .city`, { state: 'attached', timeout: 15000 })
await page.waitForTimeout(800)

// --- the port pill, at city altitude ------------------------------------
//
// One pill, drawn once in Topography and shared by both views (#1055) --
// so the idle shape and the picker are exactly what
// live-topography-port-trace.mjs already exercises on the flat map. What
// this scenario is actually here to prove is what happens once it
// collapses: does the city read the same settled answer.

const idle = page.locator(`${CARD} .pills .pill.p`)
check((await idle.textContent())?.trim() === '⌕ port', 'the port pill sits idle bottom-left at the city stop too')

await idle.click()
await page.waitForSelector(`${CARD} .pill.p.edit`, { timeout: 10000 })
await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^445$/ }).first().click()
// No Done button on the picker: a click away from it is what closes it.
// `.stage` (the flat map) is hidden at city altitude, so the click has
// to land on the city itself rather than on the surface port-trace uses.
await page.locator(`${CARD} .city`).click({ position: { x: 20, y: 20 } })
await page.waitForSelector(`${CARD} .pill.p.on`, { timeout: 10000 })
await page.waitForTimeout(500)

const on = page.locator(`${CARD} .pill.p.on`)
const label = (await on.locator('b').textContent())?.trim()
check(label === '445/tcp', `the pill collapses onto the label at city altitude too (${label})`)

// Both the flat map and the city render a plaque/tally/door/note layer
// off the same store -- the flat map's stays in the DOM merely hidden
// (`.stage[hidden]`), so every read below is scoped to `.city` rather
// than to the card as a whole, or it would see both.
const tallies = await page.locator(`${CARD} .city text.chip-t`).allTextContents()
check(
  tallies.some((t) => /^\d+ of \d+ · 445\/tcp$/.test(t.trim())),
  `a district plaque carries the filter's own tally under it (${tallies.join(' | ')})`,
)

const doors = await page.locator(`${CARD} .city .door`).count()
check(doors > 0, `a door stands in the city for the rule that names 445 (${doors})`)

await page.screenshot({ path: `${OUT}/city-port.png` })

// --- nothing seen, at city altitude -------------------------------------

await on.click()
await page.waitForSelector(`${CARD} .pill.p.edit`, { timeout: 10000 })
await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^445$/ }).first().click()
await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^3389$/ }).first().click()
await page.locator(`${CARD} .city`).click({ position: { x: 20, y: 20 } })
await page.waitForSelector(`${CARD} .city .note-t`, { state: 'attached', timeout: 10000 })
await page.waitForTimeout(300)

const note = (await page.locator(`${CARD} .city .note-t`).textContent())?.trim()
check(
  /^no logged traffic on 3389\/tcp in the window/.test(note ?? ''),
  `the nothing-seen note reads under the city too, one line rather than an empty state (${note})`,
)

await page.screenshot({ path: `${OUT}/city-port-empty.png` })

await page.locator(`${CARD} .pill-x`).click()
await page.waitForTimeout(300)
check((await page.locator(`${CARD} .city .note-t`).count()) === 0, 'the ✕ puts the city back too')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
