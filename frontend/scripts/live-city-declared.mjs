// SPDX-License-Identifier: AGPL-3.0-only
//
// #1014, a declared-quiet boundary against a running instance. The unit
// tests prove the derivation on fixtures (coverageRule.ts, input.ts);
// this walks the real thing end to end: push a lane whose rules log in
// neither direction, read DARK on both surfaces that report a district
// -- the 2D zones card and the city plaque -- then declare both
// directions intentionally quiet through the real API and read the same
// two surfaces again, where DARK must be gone.
//
// Both surfaces are checked because the bug was exactly that they
// disagreed with the map's own lane cards: those read the declarations
// (Topography's zoneCaption), the district did not, so an admin who had
// explained a boundary still saw it accused of being dark.
//
// A lane of its own (`vlan-quiet`) rather than one of the shared estate
// names: the suite runs every scenario against one instance in filename
// order, and a lane an earlier scenario also pushed rules for would
// make this scenario's reading depend on run order.

import { session, check, done, feedRaw } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// The lane declared quiet, and a control lane that logs -- so a plaque
// reading LOGGED proves the surface is actually being read, rather than
// a selector quietly matching nothing.
const QUIET = { iface: 'vlan-quiet', cidr: '10.0.70.1/24', comment: 'QuietLane' }
const LIT = { iface: 'vlan-lit', cidr: '10.0.71.1/24', comment: 'LitLane' }
const WAN = 'ether1'

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
  data: { name: 'live-city-declared', kind: 'ingest', device: DEVICE },
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
    records: [QUIET, LIT].map((l) => ({
      address: l.cidr,
      network: l.cidr.replace(/\.1\/24$/, '.0'),
      interface: l.iface,
      comment: l.comment,
    })),
  })) === 200,
  'the two lane ranges are pushed',
)

// The quiet lane answers both ways and logs neither way: dark until
// somebody says why. The lit lane logs outbound, so it never is.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 0, comment: 'quiet lane out', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', log: false, inInterface: QUIET.iface, outInterface: WAN },
      { ordinal: 1, comment: 'nothing unsolicited into the quiet lane', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: '', log: false, inInterface: WAN, outInterface: QUIET.iface },
      { ordinal: 2, comment: 'lit lane out', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: 'A|declared|', log: true, inInterface: LIT.iface, outInterface: WAN },
    ],
  })) === 200,
  'the filter-rule table is pushed: one unlogged lane, one logged',
)

for (let i = 0; i < 4; i++) {
  feedRaw(`firewall,info A|declared| forward: in:${QUIET.iface} out:${WAN}, connection-state:new, proto TCP (SYN), 10.0.70.2${i}:5${100 + i}->203.0.113.9:443, len 60`)
  feedRaw(`firewall,info A|declared| forward: in:${LIT.iface} out:${WAN}, connection-state:new, proto TCP (SYN), 10.0.71.2${i}:5${200 + i}->203.0.113.9:443, len 60`)
}
await new Promise((r) => setTimeout(r, 1500))

async function toTopography() {
  await page.setViewportSize({ width: 1600, height: 900 })
  await page.reload()
  await page.click('.rail-name >> text=Topography')
  await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })
}

const slider = () => page.locator('[data-card="topography"] .altitude input[type="range"]')

// The 2D zones card: "N hosts · DARK" on the card whose accessible name
// is the lane's (Topography.svelte's flatCards).
async function zonesCard(name) {
  await slider().fill('2') // the zones stop, the 2D stage
  await new Promise((r) => setTimeout(r, 900))
  return page.evaluate((n) => {
    const card = [...document.querySelectorAll('[data-card="topography"] .gf-card')].find((c) => (c.getAttribute('aria-label') || '').endsWith(n))
    if (!card) return null
    return { count: card.querySelector('.gf-count')?.textContent ?? '', dim: card.classList.contains('dark') }
  }, name)
}

// The city plaque: the word under the district's name (City.svelte's
// scene.plaques), plus what the plate itself says about the lane.
//
// Read at the borough stop, not the city stop: the city stop draws the
// compact plaque, which prints DARK or NO RULES and otherwise stays
// silent, so a lane reading nothing there is ambiguous between "fixed"
// and "selector matched nothing". The full plaque says LOGGED out loud,
// which is what makes the control lane below worth checking.
async function cityPlaque(name) {
  await slider().fill('4') // the borough stop, full plaques
  await new Promise((r) => setTimeout(r, 900))
  return page.evaluate((n) => {
    const groups = [...document.querySelectorAll('[data-card="topography"] .city .flat > g')]
    const g = groups.find((el) => el.querySelector('.p-name')?.textContent === n)
    const plate = [...document.querySelectorAll('[data-card="topography"] .city .plate')].find((p) => (p.getAttribute('aria-label') || '').startsWith(n + ' district'))
    return {
      cov: g ? (g.querySelector('.cov')?.textContent ?? '') : null,
      aria: plate ? (plate.getAttribute('aria-label') ?? '') : null,
    }
  }, name)
}

// --- Undeclared: the lane is dark, and both surfaces say so ---------------

await toTopography()

const darkCard = await zonesCard(QUIET.comment)
check(!!darkCard, `the zones stop draws a card for the quiet lane (${JSON.stringify(darkCard)})`)
check(darkCard?.count.includes('DARK') ?? false, `undeclared, the zones card reads DARK (${darkCard?.count})`)

const darkPlaque = await cityPlaque(QUIET.comment)
check(darkPlaque?.cov === 'DARK', `undeclared, the city plaque reads DARK (${darkPlaque?.cov})`)
check(darkPlaque?.aria?.includes('nothing logs here') ?? false, `undeclared, the district itself says nothing logs there (${darkPlaque?.aria})`)

const litPlaque = await cityPlaque(LIT.comment)
check(litPlaque?.cov === 'LOGGED', `the logged control lane reads LOGGED on the same plaque (${litPlaque?.cov})`)

// --- Declare both directions intentionally quiet (#392) -------------------

for (const key of [`${QUIET.iface}|${WAN}`, `${WAN}|${QUIET.iface}`]) {
  const res = await page.request.put(`${URL_BASE}/api/coverage/declarations/${encodeURIComponent(key)}`, {
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: { reason: 'this lane is deliberately unlogged -- #1014 live check' },
  })
  check(res.ok(), `the declaration for ${key} is accepted (${res.status()})`)
}

const listed = await (await page.request.get(`${URL_BASE}/api/coverage/declarations`)).json()
const keys = (listed.declarations ?? []).map((d) => d.key)
check(
  keys.includes(`${QUIET.iface}|${WAN}`) && keys.includes(`${WAN}|${QUIET.iface}`),
  `both declarations are persisted and readable back (${JSON.stringify(keys)})`,
)

// --- Declared: neither surface calls it dark any more ---------------------

await toTopography()

const quietCard = await zonesCard(QUIET.comment)
check(!!quietCard, `the zones stop still draws a card for the declared lane (${JSON.stringify(quietCard)})`)
check(!(quietCard?.count.includes('DARK') ?? true), `declared quiet, the zones card no longer reads DARK (${quietCard?.count})`)
check(quietCard?.dim === false, 'declared quiet, the zones card is not drawn dimmed either')

// "Not DARK", deliberately, rather than a word of its own: this slice
// only fixes what the district is derived from. A declared lane
// currently borrows the covered plaque's own wording; drawing declared
// quiet distinctly is the next slice of #1016, and the assertion tightens
// to that word when it lands.
const quietPlaque = await cityPlaque(QUIET.comment)
check(quietPlaque?.cov !== 'DARK', `declared quiet, the city plaque no longer reads DARK (${quietPlaque?.cov})`)
check(!(quietPlaque?.aria?.includes('nothing logs here') ?? true), `declared quiet, the district no longer says nothing logs there (${quietPlaque?.aria})`)

// The declaration explains one lane, not the estate: the control lane
// is untouched, and a lane nobody declared would still read DARK.
const litAfter = await cityPlaque(LIT.comment)
check(litAfter?.cov === 'LOGGED', `the logged lane is unchanged by somebody else's declaration (${litAfter?.cov})`)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
