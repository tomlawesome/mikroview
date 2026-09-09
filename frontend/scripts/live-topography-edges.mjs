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
// 5): the waist-to-internet corridor carries exactly one trunk,
// and lanes fan at the waist card instead of running up the corridor
// side by side.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const { page, consoleErrors } = await session()

// Not `bridge1`/`bridge2`/`bridge3`: `bridge1` is the de facto generic
// "LAN" name roughly twenty other scenarios reuse, and two of this
// shard's own siblings that sort before this file (live-suggestions-
// matches.mjs, live-token-copy.mjs) each feed an *accepted* event
// (`A|...|`) on exactly `in:ether1 out:bridge1` before this scenario
// ever runs. Event history is never cleared between scenarios -- only
// the pushed rule/zone tables are -- so by the time this file pushes
// its own drop-only rule for that same pair, reality.ts's verdict rule
// (`r.accepts > 0 ? 'unplanned' : 'holding'`) still sees those stale
// accepts and calls the pair 'unplanned' instead of 'holding'. An
// 'unplanned' pair draws its reality line the full lane-to-waist
// corridor rather than a short calm one near the waist -- and that
// corridor runs directly along Lane 1's own (much shorter) dark
// coverage boundary, which is the "two edges drawn along each other"
// this scenario exists to catch (#726) -- a false positive from a
// sibling's leftover traffic, not the regression it looks like. Lane
// names nothing else in the suite touches sidestep the contamination
// rather than guessing which siblings to out-race.
const LANES = ['edges-lane1', 'edges-lane2', 'edges-lane3']

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
    outInterface: LANES[0],
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
    outInterface: LANES[1],
  },
  {
    ordinal: 12,
    comment: 'lane 3 resolves anywhere',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    inInterface: LANES[2],
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
    comment: `${LANES[0]} out to the web, turned off`,
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: '',
    inInterface: LANES[0],
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

// Round 49 deleted the lens row: traffic is the picture and coverage is
// always on, so there is no tab to pick before the map can be read. What
// is left of `openLens` is getting to a 2D stop.
async function open2D() {
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  // #869: off the city default and onto zones before waiting on anything
  // the 2D map draws -- see the coverage scenario for the full note.
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 10000 })
  await page.locator('[data-card="topography"] .altitude input[type="range"]').fill('2')
  await page.waitForTimeout(600)
}

await open2D()
await page.waitForSelector('[data-card="topography"] .cedge', { timeout: 10000 })

// Walk each rendered path in the browser: 61 points apiece, in the
// SVG's own user units, which is the space the map's geometry is
// written in.
//
// Every boundary line the map draws, not just the coverage material.
// Under the old lens row this read `path.cedge` because the Coverage
// lens drew one for every boundary-direction. Round 49 keeps coverage
// material only where nothing logs (Topography.svelte's
// `drawnCoverage`) and lets the traffic picture carry the rest, so
// `.cedge` alone is now a handful of paths and would let two traffic
// edges run along each other unnoticed -- which is the exact fault
// #726 is about.
//
// Filtered to boundaries naming one of *this* scenario's own lanes:
// reality.ts's realityEdges groups the device's whole event history by
// raw interface pair, unscoped to the zone table currently pushed, and
// that history is never cleared between scenarios -- only the pushed
// rule/zone tables are. So a shared instance deep into a shard carries
// "orphan" reality edges for every interface pair an earlier sibling's
// device rule table ever named (its own dark boundary reads a raw name
// like "bridge1", not a Lane N zone, because no current address table
// names it any more). Those orphans are real geometry on the same map,
// can legitimately run near each other or near this scenario's own
// lines by sheer coincidence of the auto-fit layout, and are not what
// this file's own aim (this scenario's boundaries do not overlap) is
// about -- counting them turned a sibling's leftover traffic into a
// false #726 regression. Every one of this scenario's own boundaries
// names a Lane in its aria-label (the ip-address table's own `comment`
// fields above); an orphan's does not.
const runs = await page.evaluate(() => {
  const paths = [...document.querySelectorAll('[data-card="topography"] path.cedge, [data-card="topography"] path.redge')].filter((p) =>
    /Lane \d/.test(p.closest('[aria-label]')?.getAttribute('aria-label') ?? ''),
  )
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

check(runs.count >= 4, `the pushed table draws its boundary lines (${runs.count} paths)`)

const worst = runs.pairs.reduce((w, p) => (p.shared > w.shared ? p : w), { shared: 0, a: -1, b: -1 })
check(
  worst.shared < 0.15,
  `no two edges are drawn along each other (worst pair ${worst.a}/${worst.b} shares ${(worst.shared * 100).toFixed(0)}% of its run)`,
)

// The corridor carries one trunk -- the ratified answer to "several
// lanes heading for the internet" (#726). This used to be checked once
// per lens; with the lens row gone there is one picture to check it in.
await open2D()
const trunks = await page.evaluate(() =>
  [...document.querySelectorAll('[data-card="topography"] path.rib')].filter((p) => (p.getAttribute('d') ?? '').replace(/\s+/g, ' ').trim() === 'M700 104 V 232').length,
)
check(trunks === 1, `the map draws exactly one waist-to-internet trunk (${trunks})`)

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
await open2D()
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

// The pill row in a real browser (#715 item 3, as #981 and #1018 left
// it). Asserted here rather than in a scenario of its own because the
// row is on every screen this file already drives.
//
// This block asserted two overlay *toggles* until now, which is what
// round 49 drew and what #981 then took away -- "something that's
// always there is easy to ignore" (owner, 2026-09-08). The scenario was
// not updated with the code, so it has been failing on `dev` ever since,
// asserting a control the app deliberately no longer has. Corrected
// here, with #1018 (which is what put a control back in the row), and
// recorded on its own issue.
//
// What the row carries now: no lens row at all, no toggle of any kind,
// and one filter -- the port pill, which does not switch a layer on and
// off, it redraws the map to an answer and goes away with its own ✕.
// Beside it, only when there is something to report, sits the
// off-baseline tally (`⟡ off-baseline today · N`, #1016/round 49) -- a
// count drawn by the data, not a control, so it is read as text below
// rather than counted among the row's buttons.
await open2D()
const row = await page.evaluate(() => {
  const card = document.querySelector('[data-card="topography"]')
  const overlays = card?.querySelector('[aria-label="Map overlays"]') ?? null
  return {
    lensRows: card?.querySelectorAll('[aria-label="Map lenses"]').length ?? 0,
    ovs: [...(overlays?.querySelectorAll('button') ?? [])].map((b) => ({
      text: b.textContent.trim(),
      pressed: b.getAttribute('aria-pressed'),
    })),
    // The tally, if the day has one -- a `.nmk` span, not a button, so
    // it never shows up in `ovs` above no matter how the row is read.
    tally: overlays?.querySelector('.nmk')?.textContent?.replace(/\s+/g, ' ').trim() ?? null,
  }
})
check(row.lensRows === 0, `no lens row is drawn at all (${row.lensRows})`)
check(row.ovs.length === 1, `one control in the row and no more (${row.ovs.map((o) => o.text).join(' · ')})`)
check(row.ovs[0]?.text === '⌕ port', `and it is the port filter (${row.ovs[0]?.text})`)
check(row.ovs[0]?.pressed === 'false', `which arrives unset, filtering nothing (${row.ovs[0]?.pressed})`)
check(
  row.tally === null || /off-baseline/.test(row.tally),
  `and the off-baseline tally, when there is one, still reads as a tally and not a control (${JSON.stringify(row.tally)})`,
)

// It opens into the picker bar in place, rather than latching a layer
// on. The click and the read are two steps on purpose: Svelte 5 applies
// a state change in a microtask, so clicking and reading inside one
// page.evaluate reads the value the click was about to replace.
await page.click('[data-card="topography"] [aria-label="Map overlays"] button >> nth=0')
await page.waitForTimeout(400)
const opened = await page.evaluate(() => {
  const card = document.querySelector('[data-card="topography"]')
  return {
    bar: !!card?.querySelector('.pill.p.edit'),
    idle: !!card?.querySelector('.pills .pill.p:not(.edit)'),
  }
})
check(opened.bar, 'clicking it opens the picker as a bar of the same shape')
check(!opened.idle, 'which takes the pill\'s place rather than sitting beside it')
await page.keyboard.press('Escape')
await page.waitForTimeout(200)
check(
  (await page.locator('[data-card="topography"] .pill.p.edit').count()) === 0,
  'and Esc puts it away again',
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
