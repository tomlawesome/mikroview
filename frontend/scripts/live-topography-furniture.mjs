// SPDX-License-Identifier: AGPL-3.0-only
//
// #648, topography's ratified furniture: the altitude slider, the
// health dials, the aggregate bar, node info cards, and that the reach
// backdrop still reads as the map (round 24), not as this scenario's
// own concern -- live-topography-reach-compose.mjs already covers the
// reach itself end to end.
//
// Runs after the other live-topography-*.mjs scenarios by filename
// order; a shared suite instance may already carry flags/watchers from
// earlier scenarios, so every count below is asserted "at least", never
// "exactly" (the same reasoning live-topography-reality.mjs gives for
// its own alarm-count check).

import { session, check, done, feedRaw, feedPortScan, waitForFlag, goTo } from './live-browser.mjs'

/**
 * Poll a selector's own transform+opacity signature until it stops
 * changing -- the real end of Topography.svelte's camera transitions
 * (`.camera { transition: transform 0.35s ease }`, and 0.55s opacity
 * fades on its child layers), not a guessed margin over them.
 */
async function waitForSettle(page, selector, timeoutMs = 2000) {
  const read = () =>
    page.evaluate((sel) => {
      const el = document.querySelector(sel)
      if (!el) return null
      const cs = getComputedStyle(el)
      return cs.transform + '|' + cs.opacity
    }, selector)
  let last = await read()
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    await page.waitForTimeout(50)
    const cur = await read()
    if (cur === last && cur !== null) return cur
    last = cur
  }
  return last
}

// #869: the city is the axis's centre and its default, so a scenario about
// the 2D map's furniture must say which side it means. `zones` is the
// left-of-centre stop that draws the zone cards this file asserts on.
// The aggregate bar's halves are SVG groups whose children do not cover
// the group's own centre, so a click aimed there lands on the map behind
// them. They carry role="button" and an Enter handler for exactly this
// reason, so drive them the way a keyboard user does -- it proves the
// same binding without depending on where the ink happens to fall.
async function activate(page, selector) {
  const el = page.locator(selector).first()
  await el.waitFor({ state: 'attached', timeout: 10000 })
  await el.evaluate((n) => n.focus())
  await page.keyboard.press('Enter')
}

async function toZonesStop(page) {
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
  await slider.evaluate((el) => {
    el.value = '2'
    el.dispatchEvent(new Event('input', { bubbles: true }))
    el.dispatchEvent(new Event('change', { bubbles: true }))
  })
  await waitForSettle(page, '[data-card="topography"] .camera')
}


const URL_BASE = process.env.MV_URL

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
  data: { name: 'live-topo-furniture', kind: 'ingest', device: DEVICE },
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

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// The LAN's own pushed range: the aggregate bar and the node card's
// address/lane both correlate against this CIDR (lib/addressMatch.ts's
// addressInCidr), so it has to be a real pushed table, not a guess.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: '192.168.1.1/24', network: '192.168.1.0', interface: 'bridge1', comment: 'The LAN' }],
  })) === 200,
  'the LAN range is pushed',
)

// One host, 192.168.1.60, real traffic so it stands on the zone card.
for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info A|furniture-web| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.60:5${100 + i}->203.0.113.9:443, len 60`)
}

// A real flag targeting that same host -- port_scan is the "scan"
// family, which carries the ✱ alarm mark (see lib/flagPalette.ts).
feedPortScan(20, '192.168.1.60')

// A real watchlist entry (#407: an expectation definition) scoped to
// the same host, so its node card shows both a warning and a watch.
const entry = await api('POST', '/api/definitions', {
  name: 'live furniture watch',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { source: { ip: '192.168.1.60' }, ports: [22] },
})
check(entry.status === 201, `the watch entry is created (${entry.status})`)
const entryId = entry.body?.id

// The port-scan flag is raised by an async detector, not by the feed
// call returning -- wait for it to actually reach the server (#354's
// pattern) rather than guessing how long detection takes.
const flagArrived = await waitForFlag(page, '192.168.1.60')
check(flagArrived.ok, flagArrived.message)
// zonesState (router addresses/lanes) and watchlistState both only
// refresh on load or their own explicit calls, not from this scenario's
// direct API pushes, so the dials and node card below need a reload to
// see the entry, the flag and the estate together.
await page.reload()
// goTo, not a bare rail click: it waits for the deck to actually finish
// rolling the card to centre, which a docket detour and back needs and a
// fixed sleep here used to paper over (the camera's own transform often
// does not change on a repeat visit to the same stop, so a wait keyed to
// it alone resolves before the roll itself is done).
await goTo(page, 'Topography')
// #869: the slider now defaults to the city, its centre -- check that
// default here, before moving to a 2D stop so the rest of this scenario
// (which predates the join and draws entirely from the 2D map) can wait
// on what the 2D map draws.
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 10000 })
check((await page.locator('[data-card="topography"] .altitude input[type="range"]').inputValue()) === '3', 'the altitude defaults to "city", the axis\' centre (#869)')
await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('2') // zones
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// --- the health dials -----------------------------------------------------

const dnums = await page.locator('[data-card="topography"] .dnum').allTextContents()
check(dnums.length === 2, `both dials print a count (${JSON.stringify(dnums)})`)
check(Number(dnums[0]) >= 1, `the flags dial counts at least the one just raised (${dnums[0]})`)
check(Number(dnums[1]) >= 1, `the watchers dial counts at least the one just created (${dnums[1]})`)
check(
  (await page.locator('[data-card="topography"] .dring.d-alarm').count()) >= 1,
  'the flags ring draws an alarm segment, not the rest state',
)

// #724: a dial click opens its own condensed panel first (one flag or
// watcher raised above, so its first row is the only one); a second
// click, on that row, is what actually leaves for the docket (filed
// separately as #892, found while fixing this scenario for #869/#880 --
// unrelated to either, so fixed here rather than left broken).
await page.click('[data-card="topography"] .dial >> nth=0')
await page.waitForSelector('[data-card="topography"] .dial-panel .dp-row', { timeout: 5000 })
await page.click('[data-card="topography"] .dial-panel .dp-row >> nth=0')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=flags', { timeout: 5000 })
check(true, 'clicking the flags dial, then its own panel row, opens the docket on the flags tab')

// goTo, not a bare rail click: it waits for the deck to actually finish
// rolling the card to centre, which a docket detour and back needs and a
// fixed sleep here used to paper over (the camera's own transform often
// does not change on a repeat visit to the same stop, so a wait keyed to
// it alone resolves before the roll itself is done).
await goTo(page, 'Topography')
// #869 put the city at the centre of the axis and made it the default, so
// the 2D map's own furniture is only drawn left of centre. Everything below
// is about that furniture, so move to the zones stop first rather than
// asserting against whichever side happened to open.
await toZonesStop(page)
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })
await waitForSettle(page, '[data-card="topography"] .zone .hbar-g')

// --- the aggregate bar ------------------------------------------------------

const bars = await page.locator('[data-card="topography"] .zone .hbar-g').count()
check(bars >= 2, `the LAN island draws both halves of the aggregate bar (${bars} found)`)
check((await page.locator('[data-card="topography"] .zone .hb-div').count()) >= 1, 'a centre divider is drawn between them')

await activate(page, '[data-card="topography"] .zone .hbar-g[aria-label*="watcher"]')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=watchlist', { timeout: 5000 })
check(true, 'the purple half opens the watchlist')

// goTo, not a bare rail click: it waits for the deck to actually finish
// rolling the card to centre, which a docket detour and back needs and a
// fixed sleep here used to paper over (the camera's own transform often
// does not change on a repeat visit to the same stop, so a wait keyed to
// it alone resolves before the roll itself is done).
await goTo(page, 'Topography')
await toZonesStop(page)
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })
await activate(page, '[data-card="topography"] .zone .hbar-g[aria-label*="open flag"]')
await page.waitForSelector('[data-card="docket"] [role="tab"][aria-selected="true"] >> text=flags', { timeout: 5000 })
check(true, 'the red half opens the flags tab, pre-filtered to the zone')

// goTo, not a bare rail click: it waits for the deck to actually finish
// rolling the card to centre, which a docket detour and back needs and a
// fixed sleep here used to paper over (the camera's own transform often
// does not change on a repeat visit to the same stop, so a wait keyed to
// it alone resolves before the roll itself is done).
await goTo(page, 'Topography')
await toZonesStop(page)
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// --- the altitude slider (#869) -----------------------------------------------

const range = page.locator('[data-card="topography"] .alt-range')
// Navigating away (into the docket, above) and back is exactly the
// "across visits" the persisted last stop (#869) is for: this instance
// was left at zones a few steps up, so it is still there, not back at
// the city's own default.
check((await range.inputValue()) === '2', 'the last stop visited (zones) persisted across navigating away and back')
check((await page.locator('[data-card="topography"] .tick').count()) === 7, 'seven stops (three 2D, the city, three more), tick symbols only')
// #880: the ticks between the two named ends carry no text -- symbols
// only -- but the two ends ("clients", "street") are real furniture
// labels, not ticks, and are meant to carry text. Reading only
// `.alt-ticks` is the fix #880 asked for.
const ticksText = await page.locator('[data-card="topography"] .alt-ticks').textContent()
check((ticksText ?? '').trim() === '', 'the ticks carry no text, symbols only')

await range.fill('2')
await page.waitForSelector('[data-card="topography"] .camera.cam-zones', { timeout: 5000 })
check(true, 'moving the slider to zones applies the flat ground-plan camera')

// #976 item 1: the lane-based trunk and the traffic edges used to stay
// on screen at zones -- present in the markup at every altitude like
// every other camera layer, but never added to the stylesheet's
// cam-zones hide list the way `.isl-card`/`.detail` were -- so the
// ground plan's own river and roads and the old lane lines painted at
// once. This scenario already has real accepted traffic on one lane, so
// the trunk and its edge both exist to check.
await waitForSettle(page, '[data-card="topography"] .ground-flat')
const zonesVisibility = await page.evaluate(() => {
  const vis = (el) => (el ? getComputedStyle(el).opacity !== '0' : null)
  return {
    ground: vis(document.querySelector('[data-card="topography"] .ground-flat')),
    rib: vis(document.querySelector('[data-card="topography"] path.rib')),
    edge: vis(document.querySelector('[data-card="topography"] .edge-g')),
  }
})
check(zonesVisibility.ground === true, `the ground plan is shown at zones (${JSON.stringify(zonesVisibility)})`)
check(zonesVisibility.rib === false, `the lane-based trunk is hidden at zones (${JSON.stringify(zonesVisibility)})`)
check(zonesVisibility.edge === false, `the traffic edges are hidden at zones, so they no longer paint over the ground plan (${JSON.stringify(zonesVisibility)})`)

// --- node info cards ---------------------------------------------------------

// Round 49's living hosts (#1016): the `.host-link` list this section
// used to follow is gone. Hosts are now drawn as a row of dots inside
// each lane card at the `services` stop -- ten dots then `+N`, every
// dot clickable to its reach (DESIGN.md "Living hosts") -- so that dot
// is the way down to the host, and `descendFromHost` is what the click
// runs. `.host-link` still has a stylesheet rule with no markup left to
// match it, which is why waiting on it timed out rather than failing.
await range.fill('1')
await page.waitForSelector('[data-card="topography"] .hostrow .hot', { timeout: 10000 })
await waitForSettle(page, '[data-card="topography"] .hostrow .hot[aria-label*="192.168.1.60"]')
await page.click('[data-card="topography"] .hostrow .hot[aria-label*="192.168.1.60"]')
await page.waitForSelector('[data-card="topography"] .membrane-layer', { timeout: 5000 })

await page.click('[data-card="topography"] .host-node')
await page.waitForSelector('.node-card', { timeout: 5000 })
const cardText = await page.textContent('.node-card')
check(cardText.includes('192.168.1.60'), `the card names the host's own address (${cardText})`)
check(cardText.includes('The LAN'), 'the card names its lane')
check(cardText.includes('open flag'), 'the card surfaces the open flag as a warning')
check(cardText.includes('watched'), 'the card says it is watched')

await page.click('.node-card .nc-act >> text=open in stream ▸')
await page.waitForFunction(() => location.search.includes('Query='), null, { timeout: 5000 })
check(true, 'the open-in-stream action filters the live view to this address')

// #972: leave the shared instance as found -- later scenarios that count
// watch entries exactly should not inherit this one.
await api('DELETE', `/api/definitions/${entryId}`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
