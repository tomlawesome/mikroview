// SPDX-License-Identifier: AGPL-3.0-only
//
// #1050 (round 56): the trace's own list -- the crumb's "and N more
// like it" opens into two columns, SAME LINE and SAME MINUTE, and the
// same crumb + list now mount on the city too, not only the flat map.
//
// live-topography-port-trace.mjs already proves the crumb, the ✕, the
// ghost and the chip against a real pushed table and real logged lines;
// this scenario reuses that shape for the traced pair (IoT to LAN,
// caught by the unnamed default drop, so reality.ts calls it
// 'unplanned' and the escalated callout offers `trace ▸`) and adds what
// round 56 built on top of it: several more lines matching the exact
// same line, a few more from the same source in the same clock minute,
// the list those two groups open into, keyboard re-tracing a row, the
// Esc ladder (list first, then the trace), and the same crumb + list
// reappearing once the altitude slider drops into the city.
//
// This scenario shares one live instance with whatever sorted ahead of
// it in the slice (scripts/run-scenarios.sh: filename order, leftovers
// persist). Its escalated callout has to be the ether4->bridge-tl
// refused pair pushed below -- reality.ts's worstUnplannedOf picks
// whichever 'unplanned' pair is busiest, and a sibling's own traffic
// still sitting on the server on a pair no table here names is exactly
// what 'unplanned' means too. Left alone, that leftover -- not this
// scenario's own pair -- would be the one the callout and the trace
// escalate to. So, the same device as live-topography-port-trace.mjs,
// every pair already on record other than the one this scenario means
// to escalate gets an explicit forward accept of its own, named rather
// than left to fall to 'unplanned' by default.
import { session, check, done, feedRaw } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.TRACE_LIST_SHOTS || '/tmp/1050-shots'
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
  data: { name: 'live-trace-list', kind: 'ingest', device: DEVICE },
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

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      // The destination district is this scenario's own -- its own bridge
      // name and its own range -- so no sibling's leftover hosts can share
      // it. The city draws eight buildings per district (layout.ts's
      // MAX_BUILDINGS) and folds the rest into `more`; on a shared
      // `bridge-lan` with 33 siblings' hosts on record, DST's one reply
      // lost that ranking and the ghost had no building to reach
      // (pipeline 819, gate shard 3/4).
      { address: '10.0.15.1/24', network: '10.0.15.0', interface: 'bridge-tl', comment: 'LAN' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'ether3', comment: 'Servers' },
      { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'ether4', comment: 'IoT' },
      { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'ether5', comment: 'Guest' },
    ],
  })) === 200,
  'the four lane ranges are pushed',
)

// Neutralize every leftover pair other than this scenario's own, the
// same device live-topography-port-trace.mjs uses for the same reason.
const before = await (await page.request.get(`${URL_BASE}/api/events`)).json()
const TARGET_PAIR = 'ether4|bridge-tl'
const leftoverPairs = new Set()
for (const e of before.events ?? []) {
  if (!e.inInterface || !e.outInterface) continue
  const key = `${e.inInterface}|${e.outInterface}`
  if (key !== TARGET_PAIR) leftoverPairs.add(key)
}
const neutralizers = [...leftoverPairs].map((pair, i) => {
  const [inInterface, outInterface] = pair.split('|')
  return {
    ordinal: 900 + i,
    chain: 'forward',
    action: 'accept',
    inInterface,
    outInterface,
    log: true,
    comment: "a sibling scenario's leftover pair, named here so it cannot outrank this scenario's own callout",
  }
})

// One accepting rule so the map has a lane's worth of ordinary traffic,
// and the default drop that actually catches the traced pair -- naming
// no interface at all, which is what leaves the pair 'unplanned' rather
// than 'holding' (reality.ts: a rule that names the pair explicitly as
// refused, with nothing but drops on it, reads as policy doing its job,
// not as the alarm this scenario needs the callout to raise).
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 12, chain: 'forward', action: 'accept', protocol: 'tcp', dstPort: 443, inInterface: 'bridge-tl', outInterface: 'ether3', log: true, comment: 'web out' },
      { ordinal: 40, chain: 'forward', action: 'drop', log: true },
      ...neutralizers,
    ],
  })) === 200,
  'the filter table is pushed: one named accept, and the unnamed default drop that catches the traced pair',
)

// The traced line, and five more exactly like it (src, dst, port and
// proto all the same) -- SAME LINE's own six rows, the traced one
// included, so the crumb reads "and 5 more like it" and the column
// shows every one of them without hitting its eight-row cap.
// Addresses of this scenario's own, not shared with any sibling's fixed
// hosts (live-topography-port-trace.mjs's 10.0.30.14/10.0.10.21,
// live-city-reach.mjs's LAN1 at 10.0.10.21 among them) -- a shared host
// address is exactly what let an earlier draft of this file's own
// leftover traffic outrank live-city-reach.mjs's own refused pair for
// its composer, once both scenarios had run against one instance.
const SRC = '10.0.30.77'
const DST = '10.0.15.77'
for (let i = 0; i < 6; i++) {
  feedRaw(
    `firewall,info D|default drop| forward: in:ether4 out:bridge-tl, connection-state:new, proto TCP (SYN), ${SRC}:5${200 + i}->${DST}:445, len 60`,
  )
}
// Three more from the same source, same clock minute, a different
// destination and port each time -- SAME MINUTE's own rows, excluded
// from SAME LINE because the port differs.
for (let i = 0; i < 3; i++) {
  feedRaw(
    `firewall,info A|print| forward: in:ether4 out:ether3, connection-state:new, proto TCP (SYN), ${SRC}:5${300 + i}->10.0.20.79:${9100 + i}, len 60`,
  )
}
// A little ordinary traffic elsewhere, so the map has more than one
// lane's worth of activity to dim.
for (let i = 0; i < 4; i++) {
  feedRaw(
    `firewall,info A|web| forward: in:bridge-tl out:ether1, connection-state:new, proto TCP (SYN), 10.0.15.${90 + i}:5${400 + i}->203.0.113.9:443, len 60`,
  )
}
// The traced destination also has to be the *source* of some crossing to
// stand on the map as a building at all (zones.svelte.ts's own
// host-attribution counts only the private side of a boundary-crossing
// event) -- without this, the far side of the ghost has nowhere to
// point, the same trap live-city-reach.mjs's own comment names for srv1.
feedRaw(`firewall,info A|reply| forward: in:bridge-tl out:ether3, connection-state:new, proto TCP (SYN), ${DST}:5500->10.0.20.80:443, len 60`)

await new Promise((r) => setTimeout(r, 1500))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector(`${CARD} .altitude input[type="range"]`, { timeout: 15000 })
await page.locator(`${CARD} .altitude input[type="range"]`).fill('1') // the zone stop, the flat map's own
await page.waitForSelector(`${CARD} .zone`, { state: 'attached', timeout: 15000 })
await page.waitForTimeout(600)

// --- opening the trace from the unplanned callout's own `trace ▸` -------

await page.waitForSelector(`${CARD} .uc-trace`, { state: 'attached', timeout: 15000 })
await page.locator(`${CARD} .uc-trace .uc-trace-t`).click()
await page.waitForSelector(`${CARD} .trace-crumb`, { timeout: 10000 })
await page.waitForTimeout(600)

const flat = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const lit = [...card.querySelectorAll('.lit-half')]
  return {
    crumb: card.querySelector('.trace-crumb')?.textContent?.replace(/\s+/g, ' ').trim(),
    lit: lit.length,
    litRefused: lit.filter((l) => l.classList.contains('refused')).length,
    stop: !!card.querySelector('.trace-stop'),
    ghost: !!card.querySelector('.trace-ghost'),
    note: card.querySelector('.trace-note')?.textContent?.replace(/\s+/g, ' ').trim(),
    leader: card.querySelector('.trace-chip .trace-leader')?.getAttribute('d'),
  }
}, CARD)

check(new RegExp(SRC.replace(/\./g, '\\.')).test(flat.crumb ?? ''), `the crumb names who spoke (${flat.crumb})`)
check(new RegExp(DST.replace(/\./g, '\\.')).test(flat.crumb ?? ''), 'and who it was speaking to')
check(/445\/tcp/.test(flat.crumb ?? ''), 'and the port')
check(/and 5 more like it/.test(flat.crumb ?? ''), `and how many more lines like it there are (${flat.crumb})`)
check(/Esc ▸/.test(flat.crumb ?? ''), 'and how to put the map back')
// C1 (round 56): the log names an out-interface, so the router forwarded
// the line before the far boundary refused it -- both halves light, the
// ✕ stands at that far gate rather than the router, no ghost is drawn
// (there is nowhere left to draw one to), and the chip's leader no
// longer runs to the router's own fixed spot.
check(flat.lit === 2 && flat.litRefused === 2, `both halves of the crossing light, in the alarm ink (${flat.lit})`)
check(flat.stop, 'the ✕ still stands')
check(!flat.ghost, `but there is nowhere left to draw a dashed rib to, unlike a router-death refusal (${flat.ghost})`)
check(/stopped at the .* boundary/.test(flat.note ?? ''), `the far gate says where it stopped, not "never left the router" (${flat.note})`)
check(flat.leader !== 'M556 254 L 572 258', `the chip's leader runs to the gate, not the router's fixed spot (${flat.leader})`)

await page.screenshot({ path: `${OUT}/flat-trace.png` })

// --- the list, both columns, the traced row marked -----------------------
//
// B1 (round 56): the unplanned callout opens the list itself as soon as
// the trace answer lands, so it is already open here rather than
// waiting on a toggle click.

await page.waitForSelector(`${CARD} .picker`, { timeout: 10000 })
await page.waitForTimeout(300)
check(
  (await page.locator(`${CARD} .trace-crumb .crumb-link[aria-expanded="true"]`).count()) === 1,
  "B1: the callout's own trace opens the list without a click",
)

const list = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const picker = card.querySelector('.picker')
  const cols = [...picker.querySelectorAll('.col')]
  return {
    cols: cols.length,
    headers: cols.map((c) => c.querySelector('h5')?.textContent?.replace(/\s+/g, ' ').trim()),
    tracedRows: picker.querySelectorAll('.row.on').length,
    firstRowText: picker.querySelector('.row')?.textContent?.replace(/\s+/g, ' ').trim(),
  }
}, CARD)
check(list.cols === 2, `the list opens with two columns (${list.cols})`)
check(list.headers.some((h) => /^SAME LINE/.test(h ?? '')), `one column is SAME LINE (${list.headers.join(' | ')})`)
check(list.headers.some((h) => /^SAME MINUTE/.test(h ?? '')), 'and the other is SAME MINUTE')
check(list.tracedRows === 1, `exactly one row carries the traced mark (${list.tracedRows})`)

await page.screenshot({ path: `${OUT}/flat-trace-list.png` })

// --- ↓+Enter re-traces another row, the list stays open ------------------

await page.keyboard.press('ArrowDown')
await page.keyboard.press('Enter')
await page.waitForTimeout(500)

const retraced = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const rows = [...card.querySelectorAll('.picker .row')]
  return {
    pickerOpen: !!card.querySelector('.picker'),
    onIndex: rows.findIndex((r) => r.classList.contains('on')),
  }
}, CARD)
check(retraced.pickerOpen, 'the list stays open across a keyboard re-trace')
check(retraced.onIndex === 1, `the second row is now the traced one (${retraced.onIndex})`)

// --- Esc closes the list first, a second Esc clears the trace -----------

await page.keyboard.press('Escape')
await page.waitForTimeout(300)
check((await page.locator(`${CARD} .picker`).count()) === 0, 'the first Esc closes the list')
check((await page.locator(`${CARD} .trace-crumb`).count()) === 1, 'and leaves the crumb standing')

await page.keyboard.press('Escape')
await page.waitForTimeout(300)
check((await page.locator(`${CARD} .trace-crumb`).count()) === 0, 'the second Esc clears the trace underneath it')

// --- the same crumb, on the city ------------------------------------------
//
// Re-open the trace from the callout (the underlying data has not
// changed), then drop the altitude slider into the city and check the
// same story stands there too: the traced road lit, the rest dimmed, the
// ✕ at the destination's own gate (round 54's city drawing already puts
// it there; there is somewhere left to draw a ghost to, unlike the
// flat map's own C1 case above), and the crumb mounted rather than
// missing.

await page.waitForSelector(`${CARD} .uc-trace`, { state: 'attached', timeout: 10000 })
await page.locator(`${CARD} .uc-trace .uc-trace-t`).click()
await page.waitForSelector(`${CARD} .trace-crumb`, { timeout: 10000 })
await page.waitForTimeout(400)

// The district stop (#869), not the wider city/borough overview: roads
// there carry the raw interface ids this scenario's pairs are keyed by
// (the same stop live-city-reach.mjs and the unit tests read roads at);
// city/borough draw inter-borough roads instead, which do not.
await page.locator(`${CARD} .altitude input[type="range"]`).fill('5')
await page.waitForSelector(`${CARD} .city`, { state: 'attached', timeout: 15000 })
await page.waitForTimeout(900)

const city = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const city = card.querySelector('.city')
  const roads = [...city.querySelectorAll('path[data-road]')]
  const traced = roads.filter((r) => {
    const id = r.getAttribute('data-road') ?? ''
    return id.includes('ether4') && id.includes('bridge-tl')
  })
  const others = roads.filter((r) => !traced.includes(r))
  return {
    tracedStroke: traced.map((r) => r.getAttribute('stroke')),
    otherDimmed: others.length > 0 && others.every((r) => r.getAttribute('stroke') === 'var(--fg-dim)'),
    stop: city.querySelectorAll('.trace-stop').length,
    ghost: city.querySelectorAll('.trace-ghost').length,
    crumbs: card.querySelectorAll('.trace-crumb').length,
  }
}, CARD)
check(
  city.tracedStroke.length > 0 && city.tracedStroke.every((s) => s === 'var(--alarm)'),
  `the traced road on the city stays lit, in the alarm ink (${city.tracedStroke.join(', ')})`,
)
check(city.otherDimmed, 'every other road on the city dims')
check(city.stop === 1, `the ✕ stands once, at the destination's own gate (${city.stop})`)
check(city.ghost === 1, `and a ghost is drawn to it, unlike the flat map's far-gate case (${city.ghost})`)
check(city.crumbs === 1, `the same crumb mounts on the city too, exactly once (${city.crumbs})`)

await page.screenshot({ path: `${OUT}/city-trace.png` })

await page.keyboard.press('Escape')
await page.waitForTimeout(300)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)

done()
