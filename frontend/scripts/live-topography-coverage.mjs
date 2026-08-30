// SPDX-License-Identifier: AGPL-3.0-only
//
// #630, map layer 4: the Coverage lens paints every boundary-direction
// by what it logs -- observed solid, dark dotted and labelled dark,
// drawn never omitted -- and the zone cards carry their coverage
// captions on every lens. Runs after live-topography-reality.mjs, whose
// pushed table it reads: ether1→bridge1 refusal logs (observed),
// bridge1→ether1 and ether5→ether1 accepts do not (dark), so The LAN
// reads DARK TOWARD WAN and the quiet lane DARK BOTH WAYS.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// Standalone runs need the table the reality scenario normally leaves
// behind; a fed instance already has it.
syslog(2, 'topo-coverage-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const pre = await page.request.get(`${URL_BASE}/api/routeros/${DEVICE}/rules`)
const havePush = pre.ok() && (await pre.json()).available
if (!havePush) {
  const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: { name: 'live-topo-coverage', kind: 'ingest', device: DEVICE },
  })
  const token = (await tokenRes.json()).value
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      kind: 'filter-rule',
      page: 1,
      pages: 1,
      routerosVersion: '7.23.3 (stable)',
      records: [
        { ordinal: 0, comment: 'LAN out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443, protocol: 'tcp' },
        { ordinal: 1, comment: 'Nothing unsolicited comes in', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|forward-drop|', log: true, inInterface: 'ether1', outInterface: 'bridge1' },
      ],
    }),
  })
  check(res.status === 200, 'a rule table is pushed for the standalone case')
  await page.reload()
}

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .lenses', { timeout: 10000 })
await page.click('[data-card="topography"] .lenses >> text=Coverage')
await page.waitForSelector('[data-card="topography"] .cedge', { timeout: 10000 })

const darkCount = await page.locator('[data-card="topography"] .cedge.dark').count()
const observedCount = await page.locator('[data-card="topography"] .cedge.observed').count()
check(darkCount >= 1, `a boundary-direction nothing logs draws dark, not omitted (${darkCount})`)
check(observedCount >= 1, `a logging boundary-direction draws solid (${observedCount})`)

const stageText = await page.locator('[data-card="topography"] .stage svg').textContent()
check(stageText.includes('dark'), 'the dark line is labelled in words')
check(stageText.includes('DARK TOWARD WAN') || stageText.includes('DARK BOTH WAYS') || stageText.includes('LOGGED BOTH WAYS'), 'the zone cards carry their coverage captions')

// The captions hold on the other lenses too (the shaped surface: the
// Coverage lens carries the full model, the others keep the captions).
await page.click('[data-card="topography"] .lenses >> text=Traffic')
await new Promise((r) => setTimeout(r, 400))
const trafficText = await page.locator('[data-card="topography"] .stage svg').textContent()
check(
  trafficText.includes('DARK') || trafficText.includes('LOGGED BOTH WAYS'),
  'the coverage caption stays on the zone card outside the Coverage lens',
)

// --- Declare-a-gap (#392): one acknowledgement, stored with a reason --

await page.click('[data-card="topography"] .lenses >> text=Coverage')
await new Promise((r) => setTimeout(r, 400))

// An admin clicks a dark edge and the panel opens.
await page.click('[data-card="topography"] .edge-badge.dark-t >> nth=0')
await page.waitForSelector('.declare', { timeout: 5000 })
await page.fill('.declare textarea', 'DNS to my own resolver is not logged, on purpose.')
await page.click('.declare .d-primary')
await page.waitForSelector('.declare', { state: 'detached', timeout: 5000 })

// The edge repaints quiet -- muted, named, reason on hover -- without a
// reload: the acknowledgement is immediate.
await page.waitForSelector('[data-card="topography"] .cedge.quiet', { timeout: 5000 })
const quietBadges = await page.locator('[data-card="topography"] .edge-badge.quiet-t').allTextContents()
check(
  quietBadges.some((b) => b.includes('quiet') && b.includes('DNS to my own resolver')),
  `the declared gap is named on the map (${JSON.stringify(quietBadges)})`,
)

// And it is on the record server-side, with who and when.
const decls = await (await page.request.get(`${URL_BASE}/api/coverage/declarations`)).json()
check(
  decls.declarations?.some((d) => d.reason.includes('on purpose') && d.declaredBy),
  'the declaration is stored with its reason and author',
)

// Removing it sends the boundary honestly back to dark.
await page.click('[data-card="topography"] .edge-badge.quiet-t >> nth=0')
await page.waitForSelector('.declare', { timeout: 5000 })
await page.click('.declare .d-danger')
await page.waitForSelector('.declare', { state: 'detached', timeout: 5000 })
const quietLeft = await page.locator('[data-card="topography"] .cedge.quiet').count()
check(quietLeft === 0, 'removing the declaration returns the boundary to dark')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
