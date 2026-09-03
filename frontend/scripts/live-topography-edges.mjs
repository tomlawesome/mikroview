// SPDX-License-Identifier: AGPL-3.0-only
//
// #726, the map's edges in a real browser: distinct edges must not be
// drawn along each other. Crossing is fine and unavoidable here; running
// together is not, because neither line can then be followed, and
// following where traffic goes is the whole point of the scene.
//
// The measurement is the same one the unit tests use, but taken from the
// rendered paths rather than the `d` strings the component emits:
// getPointAtLength walks each drawn path, and two edges are "smeared"
// when much of one's run lies within a few units of another's. A
// crossing dips close once and parts again; a smear never parts. A
// check on markup or on `d` strings would pass while the map still looks
// like one thick line, which is the fault this scenario exists for.
//
// Also asserts the decision that came out of the diagnosis (#726, Fable
// 5): the waist-to-internet corridor carries one trunk in every lens,
// and lanes fan at the waist card instead of running up the corridor
// side by side.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session()

const LANES = ['bridge1', 'bridge2', 'bridge3']

syslog(2, 'topo-edges-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

// Public sources arriving on ether1 make it the internet-facing
// boundary, the same way every other topography scenario resolves it.
syslog(30, 'topo-edges-traffic')

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-edges', kind: 'ingest', device: DEVICE },
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
    records: LANES.map((iface, i) => ({
      address: `192.168.${20 + i}.1/24`,
      network: `192.168.${20 + i}.0`,
      interface: iface,
      comment: `Lane ${i + 1}`,
    })),
  })) === 200,
  `the /ip address table is accepted, naming ${LANES.length} lanes`,
)

// Three lanes reaching out, two answered back with a refusal, and one
// lane reaching anywhere: the shapes the diagnosis measured as
// coincident, all on the map at once.
const rules = [
  ...LANES.map((iface, i) => ({
    ordinal: i,
    comment: `${iface} out to the web`,
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    inInterface: iface,
    outInterface: 'ether1',
    dstPort: 443,
    protocol: 'tcp',
  })),
  {
    ordinal: 10,
    comment: 'nothing unsolicited into lane 1',
    chain: 'forward',
    action: 'drop',
    srcAddressList: '',
    logPrefix: 'D|topo-edges|',
    log: true,
    inInterface: 'ether1',
    outInterface: 'bridge1',
  },
  {
    ordinal: 11,
    comment: 'nothing unsolicited into lane 2',
    chain: 'forward',
    action: 'drop',
    srcAddressList: '',
    logPrefix: 'D|topo-edges|',
    log: true,
    inInterface: 'ether1',
    outInterface: 'bridge2',
  },
  {
    ordinal: 12,
    comment: 'lane 3 resolves anywhere',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    inInterface: 'bridge3',
    dstPort: 53,
    protocol: 'udp',
  },
  // A disabled rule, for the waist card's count further down (#701 fact
  // 2). Deliberately on a pair the table already carries, so it folds
  // into that edge rather than adding one -- this scenario's real job is
  // measuring the edges apart, and a seventh edge would change the
  // geometry it measures.
  {
    ordinal: 13,
    comment: 'bridge1 out to the web, turned off',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    inInterface: 'bridge1',
    outInterface: 'ether1',
    dstPort: 8443,
    protocol: 'tcp',
    disabled: true,
  },
]

check(
  (await push({ kind: 'filter-rule', page: 1, pages: 1, routerosVersion: '7.23.3 (stable)', records: rules })) === 200,
  'the filter-rule table is accepted through the real ingest endpoint',
)

async function openLens(name) {
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] [aria-label="Map lenses"]', { timeout: 10000 })
  await page.click(`[data-card="topography"] [aria-label="Map lenses"] >> text=${name}`)
  await page.waitForTimeout(600)
}

await openLens('Policy')
await page.waitForSelector('[data-card="topography"] .edge-g', { timeout: 10000 })

// Walk each rendered path in the browser: 61 points apiece, in the
// SVG's own user units, which is the space the map's geometry is
// written in.
const runs = await page.evaluate(() => {
  const paths = [...document.querySelectorAll('[data-card="topography"] path.edge')]
  const sample = (p) => {
    const len = p.getTotalLength()
    return Array.from({ length: 61 }, (_, i) => {
      const pt = p.getPointAtLength((len * i) / 60)
      return [pt.x, pt.y]
    })
  }
  const pts = paths.map(sample)
  const out = []
  for (let a = 0; a < pts.length; a++) {
    for (let b = a + 1; b < pts.length; b++) {
      let near = 0
      for (const [x, y] of pts[a]) {
        let closest = Infinity
        for (const [qx, qy] of pts[b]) closest = Math.min(closest, Math.hypot(x - qx, y - qy))
        if (closest < 4) near++
      }
      out.push({ a, b, shared: near / pts[a].length })
    }
  }
  return { count: paths.length, pairs: out }
})

check(runs.count >= 4, `the pushed table draws its edges (${runs.count} paths)`)

const worst = runs.pairs.reduce((w, p) => (p.shared > w.shared ? p : w), { shared: 0, a: -1, b: -1 })
check(
  worst.shared < 0.15,
  `no two edges are drawn along each other (worst pair ${worst.a}/${worst.b} shares ${(worst.shared * 100).toFixed(0)}% of its run)`,
)

// The corridor carries one trunk, in every lens -- the ratified answer
// to "several lanes heading for the internet" (#726).
for (const lens of ['Policy', 'Traffic', 'Coverage']) {
  await openLens(lens)
  const trunks = await page.evaluate(() =>
    [...document.querySelectorAll('[data-card="topography"] path.rib')].filter((p) => (p.getAttribute('d') ?? '').replace(/\s+/g, ' ').trim() === 'M700 104 V 232').length,
  )
  check(trunks === 1, `the ${lens} lens draws exactly one waist-to-internet trunk (${trunks})`)
}

// The waist card, against a real push (#715 item 7, #701 fact 2). Round
// 30 draws "RouterOS <version> · the waist · <N> rules", and the count
// is enabled rules only.
//
// The card draws whichever device the map calls primary, and the gate's
// instance is shared -- an earlier scenario's router can hold that slot.
// So the shape is asserted unconditionally, and the exact count only
// when the card is actually showing the router this scenario pushed to.
// Asserting the string outright would fail on a neighbour's device and
// report a defect that is not there.
await openLens('Policy')
const waist = await page.evaluate(() => {
  const card = document.querySelector('[data-card="topography"] .isl.waist')?.parentElement
  return {
    name: card?.querySelector('.n-name')?.textContent?.trim() ?? '',
    sub: card?.querySelector('.n-sub')?.textContent?.trim() ?? '',
  }
})

check(
  /^(RouterOS .+ · )?the waist( · \d+ rules?)?$/.test(waist.sub),
  `the waist card reads round 30's fields and nothing else ("${waist.sub}")`,
)
check(!/events\/s/.test(waist.sub), 'the waist card no longer carries a rate round 30 draws nowhere on it')

const ours = await (await page.request.get(`${URL_BASE}/api/devices`)).json()
const mine = ours.devices?.find((d) => d.id === DEVICE)
if (mine && waist.name === mine.name) {
  // Seven rules went up, one disabled, so the honest answer is six. A
  // card reading seven would be counting rules that do nothing, which is
  // the distinction the owner's ruling turns on.
  check(
    waist.sub === 'RouterOS 7.23.3 (stable) · the waist · 6 rules',
    `the waist card counts enabled rules only ("${waist.sub}")`,
  )
} else {
  console.log(`  - waist count skipped: the map's primary device is "${waist.name}", not this scenario's "${mine?.name}"`)
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
