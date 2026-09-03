// SPDX-License-Identifier: AGPL-3.0-only
//
// #868: the reach as standing on a building, against a running
// instance. The unit tests prove the fade set, the flow direction, the
// gate-passing peer lighting and the byte-identical composer on
// fixtures; this walks the real thing -- a real host, a real accepted
// gate crossing, a real refusal named by its own rule, the composer's
// printed line, and Escape landing back on the exact camera standing
// started from.

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
  data: { name: 'live-city-reach', kind: 'ingest', device: DEVICE },
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

const LAN1 = '10.0.10.21'
const SRV1 = '10.0.40.10'
const IOT_PEER = '10.0.20.30'
// A third pair, involving neither LAN1 nor its own districts, that
// standing on LAN1 has nothing to do with -- the fade set's control.
const IOT_HOST = '10.0.20.31'
const WSH_HOST = '10.0.50.10'

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
      { address: '10.0.50.1/24', network: '10.0.50.0', interface: 'wlan-wsh', comment: 'Workshop' },
    ],
  })) === 200,
  'four lane ranges are pushed',
)

check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      // LAN1's own accepted crossing: a real gate in the wall.
      { ordinal: 0, comment: 'lan to servers', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: 'A|reach|', log: true, inInterface: 'bridge-lan', outInterface: 'vlan-srv' },
      // No accept rule at all toward iot: the named drop is the only
      // rule on that boundary, so the refusal is named honestly rather
      // than by an invented gate.
      { ordinal: 1, comment: 'iot-egress-drop', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|iot-egress-drop|', log: true, inInterface: 'bridge-lan', outInterface: 'vlan-iot' },
    ],
  })) === 200,
  'the filter-rule table is pushed: one gate, one named refusal',
)

// LAN1 speaks to srv1, accepted, through the lit gate.
for (let i = 0; i < 5; i++) {
  feedRaw(`firewall,info A|reach| forward: in:bridge-lan out:vlan-srv, connection-state:new, proto TCP (SYN), ${LAN1}:5${100 + i}->${SRV1}:443, len 60`)
}
// srv1 also has to be the *source* of some crossing to stand on the
// map at all -- zones.svelte.ts's own host-attribution counts only the
// private side (the source) of a boundary-crossing event, the same
// lesson live-city-importance.mjs's own comment records.
feedRaw(`firewall,info A|reach| forward: in:vlan-srv out:bridge-lan, connection-state:new, proto TCP (SYN), ${SRV1}:5000->${LAN1}:12345, len 60`)
// LAN1 asks the iot boundary for tcp/445 and is refused every time,
// named by its own rule.
for (let i = 0; i < 4; i++) {
  feedRaw(`firewall,info D|iot-egress-drop| forward: in:bridge-lan out:vlan-iot, connection-state:new, proto TCP (SYN), ${LAN1}:5${200 + i}->${IOT_PEER}:445, len 60`)
}
// A pair LAN1 has nothing to do with: iot-host to workshop-host.
for (let i = 0; i < 6; i++) {
  feedRaw(`firewall,info A|other| forward: in:vlan-iot out:wlan-wsh, connection-state:new, proto TCP (SYN), ${IOT_HOST}:5${300 + i}->${WSH_HOST}:22, len 60`)
}
await new Promise((r) => setTimeout(r, 1200))

await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
const slider = page.locator('[data-card="topography"] .altitude input[type="range"]')
await slider.fill('5') // the district stop; the seven-stop axis is clients 0 .. street 6 (#869)
await new Promise((r) => setTimeout(r, 900))

// Stand on LAN1 from the keyboard: the district stop's own camera can
// land anywhere (zones.svelte.ts sorts busiest-first, and this run's
// own traffic ties two of them), so rather than guess which arrow
// sequence reaches LAN1's district, walk each district's own first
// building in turn and stand on the one that is it -- each of this
// run's four lanes carries exactly one host. The same Enter the unit
// tests drive, never a click that depends on the building already
// being on screen.
const firstDistrict = page.locator('[data-card="topography"] .city .plate[tabindex="0"]')
await firstDistrict.focus()
let stoodOnLan1 = false
for (let i = 0; i < 4 && !stoodOnLan1; i++) {
  await page.keyboard.press('ArrowRight')
  const cid = await page.evaluate(() => document.activeElement?.getAttribute('data-cid') ?? null)
  if (cid && cid.endsWith(LAN1)) {
    stoodOnLan1 = true
    break
  }
  await page.keyboard.press('ArrowDown')
}
check(stoodOnLan1, 'the keyboard walk reaches LAN1 among the four districts')

// The keyboard walk itself pans the camera as it goes (ArrowRight and
// ArrowDown each recentre, same as every focus move); the position Esc
// must restore is wherever that walk actually left it the instant
// before Enter, not wherever the district stop first landed.
const viewportBefore = await page.locator('[data-card="topography"] .mini rect.viewport').getAttribute('x')
await page.keyboard.press('Enter')
await new Promise((r) => setTimeout(r, 900))

const cityRoot = page.locator('[data-card="topography"] .city')
check((await cityRoot.getAttribute('data-stop')) === 'street', 'standing on the building drops the camera to the street stop')

// --- the crumb --------------------------------------------------------

const crumbText = await page.locator('[data-card="topography"] .crumb').textContent()
check((crumbText ?? '').includes(LAN1), `the crumb states the address (${crumbText})`)
check((crumbText ?? '').includes('reaches'), 'the crumb states how many it reaches')
check((crumbText ?? '').includes('reached by'), 'the crumb states how many reached it')
check((crumbText ?? '').includes('Esc surfaces'), 'the crumb states that Esc surfaces')

// --- the fade set: its own road lights, an unrelated one fades --------

const ownOpacity = await page.locator('[data-card="topography"] .city [data-road="bridge-lan|vlan-srv"]').first().getAttribute('stroke-opacity')
const otherOpacity = await page.locator('[data-card="topography"] .city [data-road="vlan-iot|wlan-wsh"]').first().getAttribute('stroke-opacity')
check(ownOpacity !== null && otherOpacity !== null, `both roads are found (own ${ownOpacity}, other ${otherOpacity})`)
check(Number(ownOpacity) > Number(otherOpacity), `its own accepted road stays lit and the unrelated pair fades (${ownOpacity} > ${otherOpacity})`)

// --- the accepted peer lights through the gate -------------------------

const srv1Opacity = await page.locator(`[data-card="topography"] .city .blk[data-cid$="${SRV1}"] path`).first().getAttribute('fill-opacity')
check(srv1Opacity !== null, `the peer building is found (fill-opacity ${srv1Opacity})`)
// building()'s dim styling drops this same paint to 0.22; lit stands at 0.4.
check(Number(srv1Opacity) > 0.3, `the accepted peer, reached through the gate, is not dimmed (${srv1Opacity})`)

// --- the composer, pinned at the refused road, drafted never run ------

const composerText = await page.locator('[data-card="topography"] .composer').textContent()
check((composerText ?? '').includes("it's been asking"), 'the composer states what it has been asking for')
check((composerText ?? '').includes('tcp/445'), 'the composer names the port')
check((composerText ?? '').includes('4×'), 'the composer states the count')
check((composerText ?? '').includes('caught by iot-egress-drop'), 'the composer names the refusing rule from the event itself')
check((composerText ?? '').includes('drafted') && (composerText ?? '').includes('never run'), 'the composer states the invariant: drafted, never run')

const cmd = await page.locator('[data-card="topography"] .composer .cm-code').textContent()
check((cmd ?? '').includes(`src-address=${LAN1}`) && (cmd ?? '').includes(`dst-address=${IOT_PEER}`), `the printed line runs host to far side (${cmd})`)
check((cmd ?? '').includes('action=accept') && (cmd ?? '').includes('log=yes'), 'the printed line is a logged, named allow -- drafted, never run')

// --- Escape surfaces to the exact camera standing started from --------

await page.keyboard.press('Escape')
await new Promise((r) => setTimeout(r, 900))
check((await cityRoot.getAttribute('data-stop')) === 'district', 'Escape surfaces back to the district stop')
const viewportAfter = await page.locator('[data-card="topography"] .mini rect.viewport').getAttribute('x')
check(viewportAfter === viewportBefore, `Escape restores the exact pan position (${viewportBefore} -> ${viewportAfter})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
