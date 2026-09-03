// SPDX-License-Identifier: AGPL-3.0-only
//
// #869: the one altitude axis, walked end to end by keyboard against a
// real instance -- clients, services, zones, the city (centred, the
// default), then borough, district, street -- checking each stop draws
// what it should, and that the selected lens is still the one selected
// after crossing the centre twice (out to the far end and back).
//
// A single lane is enough to prove the axis; the city's own fidelity
// (walls, gates, the river) is live-city-stops.mjs's and live-city-
// walls.mjs's job, not this one's.

import { session, check, done, feedRaw } from './live-browser.mjs'

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
  data: { name: 'live-city-slider', kind: 'ingest', device: DEVICE },
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
  'one lane range is pushed',
)
check(
  (await push({
    kind: 'dhcp-lease',
    page: 1,
    pages: 1,
    records: [{ address: '10.0.10.20', mac: 'aa:bb:cc:00:00:01', hostname: 'desk' }],
  })) === 200,
  'the lease table names the host',
)
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 0,
        comment: 'LAN out to the web',
        chain: 'forward',
        action: 'accept',
        srcAddressList: '',
        logPrefix: 'A|lane|',
        log: true,
        inInterface: 'bridge-lan',
        outInterface: 'ether1',
        dstPort: 443,
        protocol: 'tcp',
      },
    ],
  })) === 200,
  'the filter-rule table is pushed',
)
feedRaw('firewall,info A|lane| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), 10.0.10.20:5100->203.0.113.9:443, len 60')

await new Promise((r) => setTimeout(r, 1200))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })

const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
check((await slider.getAttribute('max')) === '6', 'seven stops on one axis (max 6, #869)')

// The lens is one piece of shared state; pick policy before walking so
// crossing the centre (twice: out, then back) has something to prove
// it carried.
await page.click('[data-card="topography"] [aria-label="Map lenses"] >> text=Policy')

const measure = () =>
  page.evaluate(() => {
    const card = document.querySelector('[data-card="topography"]')
    const range = card.querySelector('.altitude input[type="range"]')
    const stage = card.querySelector('.stage')
    const city = card.querySelector('.city')
    const onLens = [...card.querySelectorAll('[aria-label="Map lenses"] button, [aria-label="Map lenses"] span.on')]
      .find((el) => el.classList.contains('on'))
      ?.textContent?.trim()
    const camera = card.querySelector('.camera')
    return {
      value: range.value,
      stageHidden: stage ? getComputedStyle(stage).display === 'none' : true,
      cityStop: city?.dataset.stop ?? null,
      diamondOn: !!card.querySelector('.tick.diamond.on'),
      camClasses: camera ? [...camera.classList].filter((c) => c.startsWith('cam-')) : [],
      groundFlatCard: card.querySelector('.ground-flat .gf-card')?.textContent?.trim() ?? null,
      onLens,
    }
  })

const STOP_LABELS = ['clients', 'services', 'zones', 'city', 'borough', 'district', 'street']

await slider.focus()
await page.keyboard.press('Home')
await new Promise((r) => setTimeout(r, 300))

for (let i = 0; i < STOP_LABELS.length; i++) {
  const label = STOP_LABELS[i]
  const m = await measure()
  check(m.value === String(i), `stop ${i} (${label}): the slider reports it (value ${m.value})`)
  check(m.onLens === 'policy', `${label}: the policy lens is still selected`)

  if (i < 3) {
    check(!m.stageHidden, `${label}: the 2D stage is showing`)
    check(m.cityStop === null, `${label}: the city is not mounted`)
    check(m.camClasses.includes(`cam-${label}`), `${label}: the camera wears its own class (${m.camClasses.join(', ') || 'none'})`)
    if (label === 'zones') {
      check(!!m.groundFlatCard, `zones: the flat ground plan draws a card`)
      check(/^\d+ hosts?$/.test(m.groundFlatCard?.match(/\d+ hosts?/)?.[0] ?? ''), `zones: the card carries a host count, not per-host names (${m.groundFlatCard})`)
    }
  } else {
    check(m.stageHidden, `${label}: the 2D stage is put away`)
    check(m.cityStop === label, `${label}: the city renders at its own stop (${m.cityStop})`)
    if (label === 'city') check(m.diamondOn, 'city: the centre diamond tick is on')
  }

  if (i < STOP_LABELS.length - 1) {
    await page.keyboard.press('ArrowRight')
    await new Promise((r) => setTimeout(r, 700))
  }
}

// Walk all the way back, crossing the centre a second time.
for (let i = STOP_LABELS.length - 1; i >= 0; i--) {
  const m = await measure()
  check(m.value === String(i), `walking back, stop ${i} (${STOP_LABELS[i]}): the slider reports it (value ${m.value})`)
  check(m.onLens === 'policy', `${STOP_LABELS[i]}: the policy lens survived the round trip`)
  if (i > 0) {
    await page.keyboard.press('ArrowLeft')
    await new Promise((r) => setTimeout(r, 700))
  }
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
