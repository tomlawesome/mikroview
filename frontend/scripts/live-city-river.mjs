// SPDX-License-Identifier: AGPL-3.0-only
//
// #866, the river and its bridges, against a running instance. The unit
// tests prove river.ts and tunnelState.ts on fixtures; this walks the
// real thing: a WAN interface and a tunnel fed as syslog only -- no
// wireguard-interface, wireguard-peer or ppp-active table is ever
// pushed here -- so the honest "state not pushed" path is what a real
// browser renders when nothing named the tunnel's state, and the river
// itself carries no dash of any kind.
import { session, check, done, feedRaw } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.CITY_SHOTS || '/tmp/city866'
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
  data: { name: 'live-city-river', kind: 'ingest', device: DEVICE },
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

check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge-lan', comment: 'LAN' }],
  })) === 200,
  'the lane range is pushed',
)

// A public inbound source on ether1 is what makes it the WAN (the WAN
// interface is inferred from traffic, never pushed as a table); a
// tunnel-shaped interface name (wg0) crossed by an event is enough for
// the city to know a tunnel exists at all, with no state, since nothing
// here ever pushes a wireguard-interface, wireguard-peer or ppp-active
// table for this device.
for (let i = 0; i < 4; i++) {
  feedRaw(`firewall,info A|city| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), 10.0.10.2${i}:5${100 + i}->203.0.113.9:443, len 60`)
  feedRaw(`firewall,info A|river-in| input: in:ether1 out:bridge, connection-state:new, proto UDP, 198.51.100.9:500->10.0.10.1:500, len 200`)
}
feedRaw(`firewall,info A|city| forward: in:bridge-lan out:wg0, connection-state:new, proto UDP, 10.0.10.20:51820->10.9.0.2:51820, len 60`)

await new Promise((r) => setTimeout(r, 1500))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })

const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
await slider.fill('3') // the city stop; the seven-stop axis is clients 0 .. street 6 (#869)
await new Promise((r) => setTimeout(r, 900))

const river = await page.evaluate(() => {
  const city = document.querySelector('[data-card="topography"] .city')
  const g = city?.querySelector('g[aria-label^="The Internet as a river"]')
  if (!g) return null
  const els = [...g.querySelectorAll('*')]
  const dashed = els.filter((el) => el.hasAttribute('stroke-dasharray') || getComputedStyle(el).strokeDasharray !== 'none')
  return { present: true, count: els.length, dashed: dashed.map((el) => el.getAttribute('class') || el.tagName) }
})
check(!!river?.present, 'the river element is drawn at the city stop')
check((river?.count ?? 0) > 0, `the river carries drawn elements (${river?.count})`)
check((river?.dashed.length ?? 1) === 0, `the river carries no dash of any kind (offending: ${river?.dashed.join(', ') || 'none'})`)

const chips = await page.evaluate(() => [...document.querySelectorAll('[data-card="topography"] .city text.chip-t')].map((t) => t.textContent))
check(
  chips.some((t) => t === 'wg0 · tunnel · state not pushed'),
  `wg0's bridge says its state was never pushed (chips: ${chips.join(' | ')})`,
)

// Screenshots for review (#866): the city stop, whose framing already
// includes the far bank, and the street stop panned to the river via
// the minimap -- the same "look there" the operator would use.
await page.screenshot({ path: `${OUT}/city.png` })

await slider.fill('6') // the street stop, the right-hand end of the seven-stop axis (#869)
await new Promise((r) => setTimeout(r, 900))
// Shift+arrow pans the camera at any stop (live-city-stops.mjs uses the
// same keys); north is toward the river, so walking it there is the
// same "look at the water" an operator would do.
await page.locator('.city .plate').first().focus()
for (let i = 0; i < 6; i++) {
  await page.keyboard.press('Shift+ArrowUp')
  await new Promise((r) => setTimeout(r, 60))
}
await new Promise((r) => setTimeout(r, 600))
await page.screenshot({ path: `${OUT}/street.png` })
console.log(`${OUT}/city.png`)
console.log(`${OUT}/street.png`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
