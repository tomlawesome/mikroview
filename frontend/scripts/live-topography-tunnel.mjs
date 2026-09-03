// SPDX-License-Identifier: AGPL-3.0-only
//
// #877, the tunnel node in a real browser: round 30 draws a second
// upper node beside Internet -- `WireGuard` / `wg0 · 10.99.0.0/24` /
// `QUIET`, with its own watch bar -- and the build drew neither it nor
// its two connecting lines.
//
// The complement to live-city-river.mjs, which deliberately pushes no
// tunnel table at all so the city's "state not pushed" path is what a
// real instance shows. This one pushes the real tables through the real
// ingest endpoint, so the state on the card is a router's answer rather
// than a fixture's: a peer whose last handshake is seconds old makes
// the interface up, and no traffic having crossed it makes the card
// read QUIET rather than claiming traffic it never saw.
//
// It also pins the half that unit tests cannot see: that the tunnel has
// left the lane row. A wg0 lane card and a wg0 node would both look
// plausible in isolation, and the same interface drawn twice in one
// scene is the fault this guards.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session()

syslog(2, 'topo-tunnel-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

// Public sources on ether1 resolve the internet-facing boundary, the
// same way every other topography scenario does it, and give the lane
// row something to hold beside the tunnel.
syslog(30, 'topo-tunnel-traffic')

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-tunnel', kind: 'ingest', device: DEVICE },
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

// wg0's own range is what the card's subnet line reads, exactly as the
// WAN card reads ether1's -- `network` plus the prefix, so the drawn
// `10.99.0.0/24` needs no arithmetic and no guess.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '192.168.30.1/24', network: '192.168.30.0', interface: 'bridge1', comment: 'Lane 1' },
      { address: '10.99.0.1/24', network: '10.99.0.0', interface: 'wg0', comment: 'Road warriors' },
    ],
  })) === 200,
  'the /ip address table is accepted, naming a lane and the tunnel',
)

check(
  (await push({
    kind: 'wireguard-interface',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [{ name: 'wg0', comment: 'Road warriors', publicKey: 'not-a-real-wireguard-key-interface', listenPort: 51820 }],
  })) === 200,
  'the wireguard-interface table is accepted',
)

// A handshake seconds old is what makes the interface up -- the server
// classifies the peer from the reported elapsed time itself, so this
// needs no clock agreement between here and there.
check(
  (await push({
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
        lastHandshake: '5s',
        interface: 'wg0',
      },
    ],
  })) === 200,
  'the wireguard-peer table is accepted, with a handshake seconds old',
)

await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] [aria-label="Map lenses"]', { timeout: 10000 })
await page.waitForTimeout(1200)

const node = await page.evaluate(() => {
  const card = document.querySelector('[data-card="topography"]')
  if (!card) return null
  const name = [...card.querySelectorAll('.n-name')].find((n) => n.textContent?.trim() === 'WireGuard')
  const g = name?.parentElement
  const laneNames = [...card.querySelectorAll('.n-cidr')].map((n) => n.textContent?.replace(/\s+/g, ' ').trim() ?? '')
  const ribs = [...card.querySelectorAll('path.rib')].map((p) => p.getAttribute('d'))
  return {
    drawn: !!g,
    cidr: g?.querySelector('.n-cidr')?.textContent?.replace(/\s+/g, ' ').trim() ?? null,
    badge: g?.querySelector('.n-cov')?.textContent?.trim() ?? null,
    badgeClass: g?.querySelector('.n-cov')?.getAttribute('class') ?? '',
    bar: !!g?.querySelector('.hb-w'),
    zoneLabels: [...card.querySelectorAll('.zone-label')].map((n) => n.textContent?.trim() ?? ''),
    allCidrs: laneNames,
    ribs,
  }
})

check(!!node?.drawn, 'the tunnel is drawn as its own node beside Internet')
check(node?.cidr === 'wg0 · 10.99.0.0/24', `the node names its interface and subnet (got ${node?.cidr})`)

// The router says up; nothing has crossed it in this window. QUIET is
// mikroview's own reading of that pair, and what round 30 draws here.
check(node?.badge === 'QUIET', `a lit but empty tunnel reads QUIET (got ${node?.badge})`)
check(!node?.badgeClass.includes('cov-d'), `QUIET is never the alarm ink (class: ${node?.badgeClass})`)

// The watch bar is absent with no watcher inside the tunnel's range --
// present only when something correlates, the same refusal a degraded
// lane's bar makes. Asserted as absence here rather than seeded, so
// this scenario leaves the watchlist untouched for later ones.
check(!node?.bar, 'the node draws no watch bar while nothing watches inside its range')

// The half the unit tests cannot see: one interface, drawn once.
const wgLanes = (node?.allCidrs ?? []).filter((t) => t.startsWith('wg0 ')).length
check(wgLanes === 1, `wg0 appears exactly once on the map, as the node (found ${wgLanes})`)
check(
  !(node?.zoneLabels ?? []).includes('wg0'),
  `the lane row does not also carry wg0 (labels: ${(node?.zoneLabels ?? []).join(', ')})`,
)

// Ported from the-whole.html:935 -- the node joined to the router.
check(
  (node?.ribs ?? []).includes('M1080 186 C 990 215, 880 240, 830 252'),
  'the tunnel is joined to the router by its own line',
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)

done()
