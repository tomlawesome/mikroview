// SPDX-License-Identifier: AGPL-3.0-only
//
// #863, the city's four stops against a running instance. The unit
// tests prove the ground model on a fixture; this walks the real thing:
// lanes, hosts and rules pushed the way a router would, traffic fed as
// syslog, then the altitude slider taken through city, borough,
// district and street with a screenshot at each.
//
// The estate is fed here rather than by #870's demo feeder, which was
// being built in parallel and was not available to import. Once it
// lands this scenario should lean on it instead of carrying its own.

import { session, check, done, feedRaw } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.CITY_SHOTS || '/tmp/city863'
mkdirSync(OUT, { recursive: true })

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
  data: { name: 'live-city-stops', kind: 'ingest', device: DEVICE },
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
  { iface: 'vlan-srv', cidr: '10.0.40.1/24', comment: 'Servers' },
  { iface: 'vlan-iot', cidr: '10.0.20.1/24', comment: 'IoT' },
  { iface: 'vlan-guest', cidr: '10.0.30.1/24', comment: 'Guest' },
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
  'four lane ranges are pushed',
)

const HOSTS = {
  'bridge-lan': ['tom-desktop', 'anna-macbook', 'living-room-tv', 'printer-hall'],
  'vlan-srv': ['nas', 'pihole', 'unifi'],
  'vlan-iot': ['cam-porch', 'hue-bridge', 'thermostat'],
  'vlan-guest': ['guest-phone'],
}
const leases = []
LANES.forEach((l, li) => {
  HOSTS[l.iface].forEach((name, hi) => {
    leases.push({ address: l.cidr.replace(/\.1\/24$/, `.${20 + hi}`), mac: `aa:bb:cc:0${li}:0${hi}:01`, hostname: name })
  })
})
check((await push({ kind: 'dhcp-lease', page: 1, pages: 1, records: leases })) === 200, 'the lease table names the hosts')

// Guest has no logged rule, so it is the one dark district once the
// table is in.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: LANES.flatMap((l, i) => [
      {
        ordinal: i * 2,
        comment: `${l.comment} out to the web`,
        chain: 'forward',
        action: 'accept',
        srcAddressList: '',
        logPrefix: l.iface === 'vlan-guest' ? '' : 'A|city|',
        log: l.iface !== 'vlan-guest',
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
        logPrefix: 'D|city-drop|',
        log: true,
        inInterface: 'ether1',
        outInterface: l.iface,
      },
    ]),
  })) === 200,
  'the filter-rule table is pushed',
)

// Traffic: every lane out to the web and back, one lane-to-lane pair
// that the rules never planned (IoT into LAN), and a tunnel out of LAN
// so the river gets a footbridge.
for (const l of LANES) {
  const net = l.cidr.replace(/\.1\/24$/, '')
  for (let i = 0; i < 4; i++) {
    feedRaw(`firewall,info A|city| forward: in:${l.iface} out:ether1, connection-state:new, proto TCP (SYN), ${net}.${20 + (i % 3)}:5${100 + i}->203.0.113.9:443, len 60`)
    feedRaw(`firewall,info D|city-drop| forward: in:ether1 out:${l.iface}, connection-state:new, proto TCP (SYN), 198.51.100.${30 + i}:44${i}->${net}.${20 + (i % 3)}:445, len 60`)
  }
}
for (let i = 0; i < 3; i++) {
  feedRaw(`firewall,info A|city| forward: in:vlan-iot out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.20.20:5${200 + i}->10.0.10.21:445, len 60`)
  feedRaw(`firewall,info A|city| forward: in:bridge-lan out:wg0, connection-state:new, proto UDP, 10.0.10.20:5${300 + i}->10.8.0.2:51820, len 60`)
}

await new Promise((r) => setTimeout(r, 1500))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })

const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
check((await slider.getAttribute('max')) === '7', 'the altitude slider carries four city stops beyond survey (max 7)')

const STOPS = ['city', 'borough', 'district', 'street']

const measure = () =>
  page.evaluate(() => {
    const card = document.querySelector('[data-card="topography"]')
    const city = card.querySelector('.city')
    if (!city) return null
    const box = (el) => {
      const r = el.getBoundingClientRect()
      return { x: r.x, y: r.y, right: r.right, bottom: r.bottom, w: r.width, h: r.height }
    }
    const stageHidden = card.querySelector('.stage') ? getComputedStyle(card.querySelector('.stage')).display === 'none' : true
    const plates = [...city.querySelectorAll('.plate')]
    const blks = [...city.querySelectorAll('.blk')]
    const pieces = [...city.querySelectorAll('.solids > path[data-v]')]
    // The painter's order, read off the real DOM: a road piece that is
    // no nearer than a building it overlaps on screen must come first.
    let checked = 0
    let wrong = []
    for (const b of blks) {
      const bb = box(b)
      const near = Number(b.dataset.near)
      for (const p of pieces) {
        if (Number(p.dataset.v) > near) continue
        const pb = box(p)
        if (!(pb.x < bb.right && bb.x < pb.right && pb.y < bb.bottom && bb.y < pb.bottom)) continue
        checked++
        if (p.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_PRECEDING) wrong.push(b.dataset.cid)
      }
    }
    const vp = city.querySelector('.mini rect.viewport')
    return {
      stop: city.dataset.stop,
      stageHidden,
      plates: plates.length,
      blks: blks.length,
      unnamed: [...plates, ...blks].filter((e) => !(e.getAttribute('aria-label') || '').trim()).length,
      pieces: pieces.length,
      checked,
      wrong: [...new Set(wrong)],
      viewport: vp ? { w: Number(vp.getAttribute('width')), h: Number(vp.getAttribute('height')) } : null,
      nodes: city.querySelectorAll('svg *').length,
    }
  })

let lastViewport = null
for (let i = 0; i < STOPS.length; i++) {
  const stop = STOPS[i]
  await slider.fill(String(4 + i))
  await new Promise((r) => setTimeout(r, 900))
  const m = await measure()
  const at = `${stop}`
  check(!!m && m.stop === stop, `${at}: the city renders at its stop (${m?.stop})`)
  if (!m) continue
  check(m.stageHidden, `${at}: the 2D stage is put away`)
  check(m.plates === 4, `${at}: one district per lane (${m.plates})`)
  check(m.blks >= 8, `${at}: buildings stand on the plates (${m.blks})`)
  check(m.unnamed === 0, `${at}: every district and building carries an accessible name (${m.unnamed} bare)`)
  check(!!m.viewport, `${at}: the minimap shows the viewport`)
  if (m.viewport && lastViewport) check(m.viewport.w < lastViewport.w, `${at}: the viewport rect is smaller than at the last stop (${m.viewport.w.toFixed(1)} < ${lastViewport.w.toFixed(1)})`)
  lastViewport = m.viewport
  check(m.checked > 0 && m.wrong.length === 0, `${at}: no road piece is painted over a nearer building (${m.checked} pairs, wrong: ${m.wrong.join(', ') || 'none'})`)
  check(m.nodes < 4000, `${at}: the SVG stays bounded (${m.nodes} nodes)`)
  await page.screenshot({ path: `${OUT}/${stop}.png` })
}

// Keyboard: a district takes focus, Right walks its buildings, Down
// walks to the next district; Shift+arrow pans.
await page.locator('.city .plate').first().focus()
const first = await page.evaluate(() => document.activeElement?.dataset.cid)
await page.keyboard.press('ArrowRight')
await new Promise((r) => setTimeout(r, 200))
const walked = await page.evaluate(() => ({ cid: document.activeElement?.dataset.cid, cls: document.activeElement?.getAttribute('class') }))
check(walked.cls?.includes('blk') && walked.cid?.startsWith(first + '/'), `Right walks from the district ${first} into its first building (${walked.cid})`)
await page.keyboard.press('ArrowDown')
await new Promise((r) => setTimeout(r, 200))
const next = await page.evaluate(() => ({ cid: document.activeElement?.dataset.cid, cls: document.activeElement?.getAttribute('class') }))
check(next.cls?.includes('plate') && next.cid !== first, `Down walks to the next district (${next.cid})`)
const before = await page.evaluate(() => document.querySelector('.city .mini rect.viewport').getAttribute('x'))
await page.keyboard.press('Shift+ArrowLeft')
await new Promise((r) => setTimeout(r, 900))
const after = await page.evaluate(() => document.querySelector('.city .mini rect.viewport').getAttribute('x'))
check(before !== after, `Shift+arrow pans the camera (minimap viewport ${before} -> ${after})`)
await page.screenshot({ path: `${OUT}/street-keyboard.png` })

// Drag pans too.
const stage = await page.locator('.city > svg').boundingBox()
const dragBefore = after
await page.mouse.move(stage.x + stage.width / 2, stage.y + stage.height / 2)
await page.mouse.down()
await page.mouse.move(stage.x + stage.width / 2 - 200, stage.y + stage.height / 2 - 80, { steps: 8 })
await page.mouse.up()
await new Promise((r) => setTimeout(r, 300))
const dragAfter = await page.evaluate(() => document.querySelector('.city .mini rect.viewport').getAttribute('x'))
check(dragBefore !== dragAfter, `dragging the stage pans the camera (minimap viewport ${dragBefore} -> ${dragAfter})`)

// Reduced motion: the camera lands at once.
await page.emulateMedia({ reducedMotion: 'reduce' })
await slider.fill('4')
await new Promise((r) => setTimeout(r, 30))
const instant = await page.evaluate(() => document.querySelector('.city')?.dataset.stop)
const scale = await page.evaluate(() => {
  const g = document.querySelector('.city > svg > g[transform]')
  return g ? g.getAttribute('transform') : null
})
check(instant === 'city' && /scale\(1\)/.test(scale || ''), `reduced motion lands the city stop at once (${scale})`)
await page.emulateMedia({ reducedMotion: 'no-preference' })

// Back to a 2D stop, and the city goes away.
await slider.fill('2')
await new Promise((r) => setTimeout(r, 300))
const gone = await page.evaluate(() => ({
  city: !!document.querySelector('.city'),
  stage: getComputedStyle(document.querySelector('[data-card="topography"] .stage')).display !== 'none',
}))
check(!gone.city && gone.stage, 'the zones stop brings the 2D stage back and drops the city')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
