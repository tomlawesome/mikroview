// SPDX-License-Identifier: AGPL-3.0-only
//
// #699, the topography's layout against the ratified round-30 drawing.
// Every check here is a geometry measurement of the real, built map at
// three viewport widths -- not a class-name check, because every fault
// this covers was invisible from the markup and only showed up as boxes
// overlapping boxes in a browser.
//
// Five lanes, deliberately: the old laneX() spread N lanes between two
// fixed x values, so four cards fitted and five overlapped by 8.25 map
// units. Long host names, deliberately too: the reproduction on #699 saw
// hosts render as bare IPs, which understated how far the row overran.

import { session, check, done, feedRaw, feedPortScan } from './live-browser.mjs'

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
  data: { name: 'live-topo-layout', kind: 'ingest', device: DEVICE },
})
const token = (await tokenRes.json()).value

async function push(payload) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  })
  return res.status
}

const LANES = [
  { iface: 'bridge-lan', cidr: '10.0.10.1/24', comment: 'LAN' },
  { iface: 'bridge-srv', cidr: '10.0.40.1/24', comment: 'Servers' },
  { iface: 'bridge-iot', cidr: '10.0.20.1/24', comment: 'IoT' },
  { iface: 'bridge-guest', cidr: '10.0.30.1/24', comment: 'Guest' },
  { iface: 'bridge-lab', cidr: '10.0.50.1/24', comment: 'Lab' },
]

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: LANES.map((l) => ({
      address: l.cidr,
      network: l.cidr.replace(/\.1\/24$/, '.0'),
      interface: l.iface,
      comment: l.comment,
    })),
  })) === 200,
  'five lane ranges are pushed',
)

// Real host names, the length a real deployment carries -- the DHCP
// lease table is where the map reads them from.
const HOSTS = {
  'bridge-lan': ['tom-desktop-workstation', 'anna-macbook-pro-16', 'living-room-appletv'],
  'bridge-srv': ['nas-primary-storage', 'pihole-resolver-01', 'unifi-controller'],
  'bridge-iot': ['cam-porch-doorbell', 'hue-bridge-upstairs', 'thermostat-hallway'],
  'bridge-guest': ['guest-e8b2-laptop', 'guest-phone-visitor'],
  'bridge-lab': ['lab-proxmox-node-01', 'lab-testbench-pi4'],
}

const leases = []
LANES.forEach((l, li) => {
  HOSTS[l.iface].forEach((name, hi) => {
    leases.push({
      address: l.cidr.replace(/\.1\/24$/, `.${20 + hi}`),
      mac: `aa:bb:cc:0${li}:0${hi}:01`,
      hostname: name,
    })
  })
})
check((await push({ kind: 'dhcp-lease', page: 1, pages: 1, records: leases })) === 200, 'the lease table names the hosts')

// A rule table too: without one the coverage caption on each card and
// the coverage lens both stay in their waiting-for-data state, so the
// card's two-line caption and the survey dot's DARK mark would never be
// exercised. Guest is deliberately left with no outbound log rule, so
// one lane really is dark.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      ...LANES.flatMap((l, i) => [
        {
          ordinal: i * 2,
          comment: `${l.comment} out to the web`,
          chain: 'forward',
          action: 'accept',
          srcAddressList: '',
          logPrefix: l.iface === 'bridge-guest' ? '' : `A|topo-layout|`,
          log: l.iface !== 'bridge-guest',
          inInterface: l.iface,
          outInterface: 'ether1',
          dstPort: 443,
          protocol: 'tcp',
        },
        {
          ordinal: i * 2 + 1,
          comment: 'nothing unsolicited comes in',
          chain: 'forward',
          action: 'drop',
          srcAddressList: '',
          logPrefix: `D|topo-layout-drop|`,
          log: true,
          inInterface: 'ether1',
          outInterface: l.iface,
        },
      ]),
    ],
  })) === 200,
  'the filter-rule table is pushed',
)

// Traffic on every lane, both ways past the waist, so each card has a
// count and the reality overlay has pairs to draw.
for (const l of LANES) {
  const net = l.cidr.replace(/\.1\/24$/, '')
  for (let i = 0; i < 4; i++) {
    feedRaw(
      `firewall,info A|topo-layout| forward: in:${l.iface} out:ether1, connection-state:new, proto TCP (SYN), ${net}.${20 + (i % 3)}:5${100 + i}->203.0.113.9:443, len 60`,
    )
    feedRaw(
      `firewall,info D|topo-layout-drop| forward: in:ether1 out:${l.iface}, connection-state:new, proto TCP (SYN), 198.51.100.${30 + i}:44${i}->${net}.${20 + (i % 3)}:445, len 60`,
    )
  }
}

// Flags on two lanes and one from the internet side, so the aggregate
// bars -- lane and island -- have something real to carry.
feedPortScan(20, '10.0.20.20')
feedPortScan(20, '10.0.10.21')
feedPortScan(20, '203.0.113.77')

async function api(method, path, body) {
  const res = await page.request.fetch(`${URL_BASE}${path}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return res.status()
}
await api('POST', '/api/definitions', {
  name: 'live layout watch lan',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { source: { ip: '10.0.10.20' }, ports: [22] },
})
await api('POST', '/api/definitions', {
  name: 'live layout watch wan',
  intent: 'expectation',
  kind: 'declarative',
  expectation: { source: { ip: '203.0.113.9' }, ports: [443] },
})

await new Promise((r) => setTimeout(r, 1500))

const OUT = process.env.TOPO_SHOTS || '/tmp/topo699'
const WIDTHS = [1280, 1600, 1920]

// Geometry is read in the browser: getBoundingClientRect on the real
// laid-out SVG, which is the only thing that knows what the viewBox and
// preserveAspectRatio actually produced at this width.
const measure = () =>
  page.evaluate(() => {
    const card = document.querySelector('[data-card="topography"]')
    const box = (el) => {
      const r = el.getBoundingClientRect()
      return { x: r.x, y: r.y, w: r.width, h: r.height, right: r.right, bottom: r.bottom }
    }
    const svg = card.querySelector('.stage svg')
    return {
      stage: box(svg),
      cards: [...card.querySelectorAll('.zone .isl')].map(box),
      hosts: [...card.querySelectorAll('.zone .n-hosts')].map(box),
      bars: [...card.querySelectorAll('.zone .hb')].map(box),
      badges: [...card.querySelectorAll('.edge-badge')].map(box),
      plates: [...card.querySelectorAll('.edge-plate')].map(box),
      dials: card.querySelector('.dials') ? box(card.querySelector('.dials')) : null,
      dialSvg: card.querySelector('.dial svg') ? box(card.querySelector('.dial svg')) : null,
      texts: [...card.querySelectorAll('.stage svg text')].map((t) => t.textContent.trim()),
      internetBars: [...card.querySelectorAll('.stage svg > g > g.passive .hb')].map(box),
      islands: [...card.querySelectorAll('.stage svg > g > g.passive .isl')].map(box),
      svgLabel: svg.getAttribute('aria-label'),
    }
  })

for (const width of WIDTHS) {
  await page.setViewportSize({ width, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .zone .isl', { timeout: 15000 })
  await new Promise((r) => setTimeout(r, 900))
  const m = await measure()
  const at = `at ${width} wide`

  check(m.cards.length === 5, `${at}: five lane cards are drawn (${m.cards.length})`)

  // 1. no two cards overlap.
  const sorted = [...m.cards].sort((a, b) => a.x - b.x)
  let worst = Infinity
  for (let i = 1; i < sorted.length; i++) worst = Math.min(worst, sorted[i].x - sorted[i - 1].right)
  check(worst >= 0, `${at}: no two zone cards overlap (tightest gap ${worst.toFixed(1)}px)`)

  // 1b. and the row stays on the stage.
  const off = sorted.length ? Math.max(m.stage.x - sorted[0].x, sorted[sorted.length - 1].right - m.stage.right) : 0
  check(off <= 0.5, `${at}: the lane row stays inside the stage (worst overhang ${off.toFixed(1)}px)`)

  // 2 & 3. the two captions round 30 draws nowhere.
  check(!m.texts.some((t) => /pairs? not drawn/.test(t)), `${at}: no "pairs not drawn" caption on the map`)
  check(!m.texts.some((t) => /^unjudged — push the rule table/.test(t)), `${at}: no "unjudged — push the rule table" line`)
  check(typeof m.svgLabel === 'string' && m.svgLabel.startsWith('The network map'), `${at}: the map still names itself`)

  // 4. the host row is inside its own card.
  let hostOver = 0
  for (const h of m.hosts) {
    const own = m.cards.find((c) => h.x >= c.x - 2 && h.x <= c.right + 2)
    if (own) hostOver = Math.max(hostOver, h.right - own.right)
  }
  check(hostOver <= 0.5, `${at}: no host row overruns its card (worst ${hostOver.toFixed(1)}px)`)

  // 7. the aggregate bar is flush with its card.
  let barGap = 0
  for (const c of m.cards) {
    const own = m.bars.filter((b) => b.x >= c.x - 2 && b.right <= c.right + 2)
    if (own.length === 0) continue
    const l = Math.min(...own.map((b) => b.x))
    const r = Math.max(...own.map((b) => b.right))
    barGap = Math.max(barGap, Math.abs(l - c.x), Math.abs(r - c.right))
  }
  check(barGap <= 1.5, `${at}: the aggregate bar is flush with its card (worst inset ${barGap.toFixed(1)}px)`)

  // 8. every edge label sits on a plate.
  let unplated = 0
  for (const b of m.badges) {
    const on = m.plates.some((p) => p.x <= b.x + 1 && p.right >= b.right - 1 && p.y <= b.y + 1 && p.bottom >= b.bottom - 1)
    if (!on) unplated++
  }
  check(m.badges.length === 0 || unplated === 0, `${at}: every edge label sits on a plate (${m.badges.length} labels, ${unplated} bare)`)

  // An opaque plate hides whatever it lands on, so the plates have to
  // clear each other as well as the lines they annotate.
  let collided = 0
  for (let a = 0; a < m.plates.length; a++) {
    for (let b = a + 1; b < m.plates.length; b++) {
      const p1 = m.plates[a]
      const p2 = m.plates[b]
      if (p1.x < p2.right - 1 && p2.x < p1.right - 1 && p1.y < p2.bottom - 1 && p2.y < p1.bottom - 1) collided++
    }
  }
  check(collided === 0, `${at}: no two label plates overlap (${collided} pairs)`)

  // The islands paint over the edges, so a plate that lands under one
  // is a label nobody can read.
  let buried = 0
  for (const p1 of m.plates) {
    for (const isl of m.islands) {
      if (p1.x < isl.right - 1 && isl.x < p1.right - 1 && p1.y < isl.bottom - 1 && isl.y < p1.bottom - 1) buried++
    }
  }
  check(buried === 0, `${at}: no label plate is buried under an island (${buried})`)

  // 9. the dials, top-right and full size.
  const stageMid = m.stage.x + m.stage.w / 2
  check(!!m.dials && m.dials.x > stageMid, `${at}: the dials hang in the right half of the card`)
  check(!!m.dialSvg && Math.abs(m.dialSvg.w - 56) < 1, `${at}: each dial draws at 56px (${m.dialSvg?.w.toFixed(1)})`)

  // 10. the internet island carries an aggregate bar.
  check(m.internetBars.length > 0, `${at}: the internet island carries an aggregate bar (${m.internetBars.length} halves)`)

  await page.screenshot({ path: `${OUT}/zones-${width}.png` })

  // 5. survey: cards out, zone dots in.
  const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
  await slider.fill('3')
  await new Promise((r) => setTimeout(r, 900))
  const survey = await page.evaluate(() => {
    const card = document.querySelector('[data-card="topography"]')
    const vis = (sel) =>
      [...card.querySelectorAll(sel)].filter((e) => Number(getComputedStyle(e).opacity) > 0.05).length
    return {
      cardsVisible: vis('.zone .isl-card'),
      dotsVisible: vis('.zone .g-dot'),
      aggMarks: vis('.zone .g-dot .gd-agg'),
      detailVisible: vis('.detail'),
    }
  })
  check(survey.cardsVisible === 0, `${at}: survey hides the lane cards (${survey.cardsVisible} still shown)`)
  check(survey.dotsVisible === 5, `${at}: survey reveals a zone dot per lane (${survey.dotsVisible})`)
  check(survey.aggMarks > 0, `${at}: the survey dots carry their gd-agg marks (${survey.aggMarks})`)
  check(survey.detailVisible === 0, `${at}: survey drops the edge callouts (${survey.detailVisible} shown)`)
  await page.screenshot({ path: `${OUT}/survey-${width}.png` })

  // 6. clients: layers added, nothing clipped off the stage.
  await slider.fill('0')
  await new Promise((r) => setTimeout(r, 900))
  const clients = await page.evaluate(() => {
    const card = document.querySelector('[data-card="topography"]')
    const svg = card.querySelector('.stage svg')
    const sb = svg.getBoundingClientRect()
    const vis = (sel) => [...card.querySelectorAll(sel)].filter((e) => Number(getComputedStyle(e).opacity) > 0.05)
    const cli = vis('.cli .c-label')
    const svc = vis('.svc .svc-t')
    let over = 0
    for (const el of [...card.querySelectorAll('.stage svg text, .stage svg rect, .stage svg circle')]) {
      const r = el.getBoundingClientRect()
      if (r.width === 0 && r.height === 0) continue
      if (Number(getComputedStyle(el).opacity) <= 0.05) continue
      over = Math.max(over, r.right - sb.right, sb.x - r.x, r.bottom - sb.bottom, sb.y - r.y)
    }
    return { clients: cli.length, services: svc.length, over }
  })
  check(clients.clients > 0, `${at}: the clients altitude draws a client tier (${clients.clients} names)`)
  check(clients.services > 0, `${at}: the services layer is drawn too (${clients.services} chips)`)
  check(clients.over <= 1, `${at}: nothing at the clients altitude leaves the stage (worst ${clients.over.toFixed(1)}px)`)
  await page.screenshot({ path: `${OUT}/clients-${width}.png` })

  await slider.fill('2')
  await new Promise((r) => setTimeout(r, 600))

  // The other two lenses draw their own labels, and their own plates
  // have to clear each other the same way.
  for (const lensName of ['policy', 'coverage']) {
    await page.click(`[data-card="topography"] .wlens2 >> text=${lensName}`)
    await new Promise((r) => setTimeout(r, 700))
    const lm = await measure()
    let lc = 0
    for (let a = 0; a < lm.plates.length; a++) {
      for (let b = a + 1; b < lm.plates.length; b++) {
        const p1 = lm.plates[a]
        const p2 = lm.plates[b]
        if (p1.x < p2.right - 1 && p2.x < p1.right - 1 && p1.y < p2.bottom - 1 && p2.y < p1.bottom - 1) lc++
      }
    }
    check(lc === 0, `${at}: the ${lensName} lens's plates clear each other (${lm.plates.length} plates, ${lc} overlapping pairs)`)
    let lb = 0
    for (const p1 of lm.plates) {
      for (const isl of lm.islands) {
        if (p1.x < isl.right - 1 && isl.x < p1.right - 1 && p1.y < isl.bottom - 1 && isl.y < p1.bottom - 1) lb++
      }
    }
    check(lb === 0, `${at}: the ${lensName} lens buries no label under an island (${lb})`)
    check(!lm.texts.some((t) => /pairs? not drawn/.test(t)), `${at}: the ${lensName} lens draws no "pairs not drawn" caption`)
    if (width === 1600) await page.screenshot({ path: `${OUT}/${lensName}-${width}.png` })
  }
  await page.click(`[data-card="topography"] .wlens2 >> text=traffic`)
  await new Promise((r) => setTimeout(r, 400))
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.slice(0, 3).join(' | ')})`)
done()
