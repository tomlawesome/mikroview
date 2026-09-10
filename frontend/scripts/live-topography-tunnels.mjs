// SPDX-License-Identifier: AGPL-3.0-only
//
// #890, round 52's tunnel cluster in a real browser. `live-topography-
// tunnel.mjs` beside this one covers the single tunnel #877 built --
// its card, its state words, its literal rib -- and nothing there can
// see the thing round 52 actually decided: what the map does with the
// second, third and fifth tunnel, which before this change simply
// stood in the lane row.
//
// Unit tests can check the packing rule's arithmetic (lib/topography/
// cluster.test.ts) and the rendered attributes (Topography.svelte.
// test.ts). What they cannot see is the real layout engine's answer:
// whether two cards that the rule says do not collide actually draw
// clear of one another at the size the browser lays them out at. So the
// overlap check below is computed from getBoundingClientRect, not from
// the transforms -- the boxes as they land on glass.
//
// Everything is pushed through the real ingest endpoint, so the state on
// each card is a router's answer rather than a fixture's.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session()

/**
 * Poll a selector's own transform+opacity signature until it stops
 * changing -- the real end of Topography.svelte's camera transitions
 * (`.camera { transition: transform 0.35s ease }`, and 0.55s opacity
 * fades on its child layers), not a guessed margin over them.
 */
async function waitForSettle(selector, timeoutMs = 2000) {
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

syslog(2, 'topo-tunnels-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

// Public sources on ether1 resolve the internet-facing boundary and give
// the lane row something real to hold, so the cluster is packed around
// furniture rather than around an empty map.
syslog(30, 'topo-tunnels-traffic')

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-tunnels', kind: 'ingest', device: DEVICE },
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

const TUNNELS = [
  { name: 'wg0', net: '10.99.0.0', comment: 'Road warriors' },
  { name: 'wg1', net: '10.98.0.0', comment: 'Site to site' },
  { name: 'wg2', net: '10.97.0.0', comment: 'Branch office' },
  { name: 'wg3', net: '10.96.0.0', comment: 'Backup path' },
  { name: 'wg4', net: '10.95.0.0', comment: 'Lab' },
]

/** Push the first `n` tunnels as the device's whole WireGuard estate:
 * the address table that names their ranges, the interface table, and a
 * peer per interface handshaken seconds ago, so every one of them is up
 * from the router's own answer. */
async function pushTunnels(n) {
  const list = TUNNELS.slice(0, n)
  const ok = []
  ok.push(
    await push({
      kind: 'ip-address',
      page: 1,
      pages: 1,
      routerosVersion: '7.23.3 (stable)',
      records: [
        { address: '192.168.30.1/24', network: '192.168.30.0', interface: 'bridge1', comment: 'Lane 1' },
        ...list.map((t) => ({ address: `${t.net.replace(/\.0$/, '.1')}/24`, network: t.net, interface: t.name, comment: t.comment })),
      ],
    }),
  )
  ok.push(
    await push({
      kind: 'wireguard-interface',
      page: 1,
      pages: 1,
      routerosVersion: '7.23.3 (stable)',
      records: list.map((t, i) => ({
        name: t.name,
        comment: t.comment,
        publicKey: `not-a-real-wireguard-key-interface-${i}`,
        listenPort: 51820 + i,
      })),
    }),
  )
  ok.push(
    await push({
      kind: 'wireguard-peer',
      page: 1,
      pages: 1,
      routerosVersion: '7.23.3 (stable)',
      records: list.map((t, i) => ({
        publicKey: `not-a-real-wireguard-key-peer-${i}`,
        allowedAddress: [`${t.net.replace(/\.0$/, '.2')}/32`],
        endpointAddress: `198.51.100.${30 + i}`,
        comment: `peer-${t.name}`,
        lastHandshake: '5s',
        interface: t.name,
      })),
    }),
  )
  check(
    ok.every((s) => s === 200),
    `the ${n}-tunnel tables are accepted (${ok.join(', ')})`,
  )
}

/** Reload onto the map, off the city default and onto the services
 * stop. Two reasons for that stop in particular: the 2D stage is hidden
 * altogether while the slider sits on the city, and at the zones stop
 * the card drawing is faded out for the ground plan (`.camera.cam-zones
 * .isl-card { opacity: 0 }`), so the boxes below would be measured off a
 * layer nobody can see. */
async function openTheMap() {
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] [aria-label="Map overlays"]', { timeout: 10000 })
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
  await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('1')
  await waitForSettle('[data-card="topography"] .camera')
}

/** Everything this scenario judges, read off the rendered map. */
function readMap() {
  const card = document.querySelector('[data-card="topography"]')
  const svg = card?.querySelector('.stage > svg')
  const groups = [...(card?.querySelectorAll('g[data-tunnel]') ?? [])]
  const box = (g) => {
    // The card's own footprint, not its group's -- a long state word
    // hangs past the rect, and the rect is what the packing rule places.
    const r = g.querySelector('rect.isl').getBoundingClientRect()
    return { x: r.x, y: r.y, w: r.width, h: r.height }
  }
  const unit = (g) => {
    const m = /translate\(([-\d.]+) ([-\d.]+)\)/.exec(g.getAttribute('transform') ?? '')
    // Round 30's card is drawn -84..+104 about its own point.
    return { x: Number(m[1]) - 84, y: Number(m[2]) - 28, w: 188, h: 56 }
  }
  return {
    ifaces: groups.map((g) => g.getAttribute('data-tunnel')),
    boxes: groups.map(box),
    units: groups.map(unit),
    strands: [...(card?.querySelectorAll('path[data-tunnel-strand]') ?? [])].map((p) => ({
      iface: p.getAttribute('data-tunnel-strand'),
      d: p.getAttribute('d'),
    })),
    viewBox: svg?.getAttribute('viewBox') ?? null,
    fitChip: card?.querySelector('.fitchip')?.textContent?.replace(/\s+/g, ' ').trim() ?? null,
    laneCidrs: [...(card?.querySelectorAll('.zone .n-cidr') ?? [])].map((n) => n.textContent?.replace(/\s+/g, ' ').trim() ?? ''),
  }
}

const overlaps = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

for (const n of [2, 3, 5]) {
  await pushTunnels(n)
  await openTheMap()
  const m = await page.evaluate(readMap)
  const at = `${n} tunnels`

  // 1. Every pushed tunnel is drawn, as its own card. Before round 52
  //    only the busiest was, and the rest stood in the lane row.
  check(m.ifaces.length === n, `${at}: the map draws one card per tunnel (got ${m.ifaces.length}: ${m.ifaces.join(', ')})`)
  check(
    TUNNELS.slice(0, n).every((t) => m.ifaces.includes(t.name)),
    `${at}: each pushed interface is on the map by name (${m.ifaces.join(', ')})`,
  )

  // 2. And none of them is a lane. The same interface drawn twice --
  //    once as a card, once as a lane -- is the fault this guards.
  const asLanes = m.laneCidrs.filter((t) => TUNNELS.some((x) => t.startsWith(`${x.name} `)))
  check(asLanes.length === 0, `${at}: no tunnel is also drawn as a lane (${asLanes.join(', ') || 'none'})`)

  // 3. No two cards touch, measured as the browser actually laid them
  //    out rather than as the rule intended.
  let worst = null
  for (let i = 0; i < m.boxes.length; i++) {
    for (let j = i + 1; j < m.boxes.length; j++) {
      if (overlaps(m.boxes[i], m.boxes[j])) worst = `${m.ifaces[i]} over ${m.ifaces[j]}`
    }
  }
  check(worst === null, `${at}: no two tunnel cards overlap on screen (${worst ?? 'clear'})`)
  check(
    m.boxes.every((b) => b.w > 0 && b.h > 0),
    `${at}: every card is actually rendered with a box`,
  )

  // 4. One strand per tunnel, each its own line to the waist.
  check(m.strands.length === n, `${at}: one strand per tunnel (got ${m.strands.length})`)
  check(
    m.strands.every((s) => m.ifaces.includes(s.iface)),
    `${at}: every strand belongs to a drawn card (${m.strands.map((s) => s.iface).join(', ')})`,
  )
  check(new Set(m.strands.map((s) => s.d)).size === n, `${at}: no two strands are the same line`)

  // 5. The frame fits the map: never smaller than round 49's own frame,
  //    always holding every card with its 40 px of air, and exactly
  //    round 49's frame while the group still fits inside it.
  const [vx, vy, vw, vh] = (m.viewBox ?? '').split(' ').map(Number)
  check(vw >= 1400 && vh >= 720, `${at}: the frame never comes in closer than 1:1 (${m.viewBox})`)
  const holdsAll = m.units.every((u) => u.x - 40 >= vx && u.y - 40 >= vy && u.x + u.w + 40 <= vx + vw && u.y + u.h + 40 <= vy + vh)
  check(holdsAll, `${at}: the frame holds every card with 40 px of air (${m.viewBox})`)
  const fitsNominal = m.units.every((u) => u.x - 40 >= 0 && u.y - 40 >= 0 && u.x + u.w + 40 <= 1400 && u.y + u.h + 40 <= 720)
  check(
    fitsNominal ? m.viewBox === '0 0 1400 720' : vw > 1400 || vh > 720,
    `${at}: the frame grows only when the group needs it to (${m.viewBox}, fits nominal: ${fitsNominal})`,
  )

  // 6. The fit chip: the zoom against the nominal frame, and the way back.
  // `NN%`, the divider, `⤢ fit` -- the divider is a rule rather than a
  // character, so the text runs the two together.
  check(
    /^\d+%⤢fit$/.test((m.fitChip ?? '').replace(/\s+/g, '')),
    `${at}: the fit chip reads NN% | ⤢ fit (got ${JSON.stringify(m.fitChip)})`,
  )
  check(
    m.fitChip?.startsWith('100%') === fitsNominal,
    `${at}: the chip reads 100 % exactly while the frame has not grown (got ${JSON.stringify(m.fitChip)})`,
  )
}

// --- the map moves under the hand, and comes back (#890 item 4) --------
//
// The pan and the fit chip are one control between them: a map that can
// be dragged away with no way home is worse than one that cannot move.
const svgSel = '[data-card="topography"] .stage > svg'

/**
 * Poll the SVG's own viewBox attribute until it stops changing. Pan and
 * wheel zoom (Topography.svelte's onMapWheel/panMap) write it straight
 * from the pointer with no animation; the fit chip's own return-to-frame
 * (fitMap) eases over a real 240ms, cancelled via cancelAnimationFrame if
 * interrupted. Either way this is the actual end state, not a guess at it.
 */
async function waitForViewBoxSettle(timeoutMs = 800) {
  const read = () => page.getAttribute(svgSel, 'viewBox')
  let last = await read()
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    await page.waitForTimeout(30)
    const cur = await read()
    if (cur === last) return cur
    last = cur
  }
  return last
}

const before = await page.getAttribute(svgSel, 'viewBox')
const stage = await page.locator(svgSel).boundingBox()
await page.mouse.move(stage.x + stage.width / 2, stage.y + stage.height / 2)
await page.mouse.down()
await page.mouse.move(stage.x + stage.width / 2 - 180, stage.y + stage.height / 2 - 90, { steps: 12 })
await page.mouse.up()
const panned = await waitForViewBoxSettle()
check(panned !== before, `dragging the map pans it (${before} -> ${panned})`)

const chipSel = '[data-card="topography"] .fitchip'
check((await page.getAttribute(chipSel, 'class'))?.includes('hand'), 'the fit chip marks a view moved by hand')
await page.click(chipSel)
const backToFit1 = await waitForViewBoxSettle()
check(backToFit1 === before, `the fit chip returns to the fitted frame (${backToFit1})`)

// Wheel zoom, about the pointer, and the chip's percentage moving with it.
await page.mouse.move(stage.x + stage.width / 2, stage.y + stage.height / 2)
await page.mouse.wheel(0, -600)
const zoomed = (await waitForViewBoxSettle()).split(' ').map(Number)
check(zoomed[2] < Number(before.split(' ')[2]), `the wheel zooms the map in (${zoomed.join(' ')})`)
await page.click(chipSel)
const backToFit2 = await waitForViewBoxSettle()
check(backToFit2 === before, 'and the fit chip brings the whole map back again')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)

// --- put the estate back the way the scenario before this one left it ---
//
// Scenarios share one instance in filename order (AGENTS.md, "A scenario
// cannot be judged on its own"), so five extra WireGuard interfaces
// would follow this file into every later one's map and city. Push the
// tables back to exactly what live-topography-tunnel.mjs leaves: wg0
// alone, with the days-old handshake that makes it read down.
const restored = []
restored.push(
  await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '192.168.30.1/24', network: '192.168.30.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.99.0.1/24', network: '10.99.0.0', interface: 'wg0', comment: 'Road warriors' },
    ],
  }),
)
restored.push(
  await push({
    kind: 'wireguard-interface',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ name: 'wg0', comment: 'Road warriors', publicKey: 'not-a-real-wireguard-key-interface', listenPort: 51820 }],
  }),
)
restored.push(
  await push({
    kind: 'wireguard-peer',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        publicKey: 'not-a-real-wireguard-key-peer',
        allowedAddress: ['10.99.0.2/32'],
        endpointAddress: '198.51.100.30',
        comment: 'phone-tom',
        lastHandshake: '3d4h20m',
        interface: 'wg0',
      },
    ],
  }),
)
check(
  restored.every((s) => s === 200),
  `the estate is left as the single-tunnel scenario had it (${restored.join(', ')})`,
)

done()
