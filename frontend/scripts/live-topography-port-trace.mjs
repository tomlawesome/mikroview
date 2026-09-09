// SPDX-License-Identifier: AGPL-3.0-only
//
// #1018 (round 53), the two filters on the living topology in a real
// browser: show me where a port is used, and show me the path one line
// took.
//
// The unit tests prove the arithmetic on fixtures -- which ribs light,
// where a door goes, what the empty line says. What they cannot see is
// the half this scenario exists for: that the answer is assembled from
// a real pushed rule table and real logged lines, through the real
// endpoints, and that the drawing that comes out of it is the round-53
// one rather than a plausible-looking neighbour. Both halves of that
// have caught real defects on this map before -- a lane row and a node
// drawing the same interface twice (#877), a filtered read that looked
// right in a fixture and was empty against the server.
//
// The data story is round 49's, which round 53 filters: 445/tcp
// accepted from LAN to Servers, refused from IoT, and 3389/tcp seen
// nowhere with one rule naming it.
import { session, check, done, feedRaw, goTo, eventsTotal, waitForEventsTotal } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.PORT_TRACE_SHOTS || '/tmp/1018-shots'
mkdirSync(OUT, { recursive: true })

const CARD = '[data-card="topography"]'

const { page, consoleErrors } = await session()

// Measured in the page, not asserted from the CSS: the fit chip's label
// is `NN%` and grows with the zoom, so "does the legend clear it" is a
// question about two rendered boxes rather than about two `right`
// values. Read on its own rather than folded into the two big reads
// below, so the same measurement serves both without either of them
// needing a helper shipped into the page.
/**
 * Poll a selector's own transform+opacity signature until it stops
 * changing -- the real end of Topography.svelte's camera transitions
 * (`.camera { transition: transform 0.35s ease }`, and 0.55s opacity
 * fades on its child layers), not a guessed margin over them.
 */
async function waitForSettle(page, selector, timeoutMs = 2000) {
  const read = () =>
    page.evaluate((sel) => {
      const el = document.querySelector(sel)
      if (!el) return null
      const cs = getComputedStyle(el)
      return cs.transform + '|' + cs.opacity
    }, selector)
  let last = await read()
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    await page.waitForTimeout(50)
    const cur = await read()
    if (cur === last && cur !== null) return cur
    last = cur
  }
  return last
}

/** Two real paints -- Svelte 5 applies state to the DOM in a microtask,
 * so a value read in the same tick as the click that changed it is the
 * value about to be replaced. No CSS transition backs these particular
 * elements, so this is what "let it settle" actually needs. */
function nextPaint(page) {
  return page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))))
}

async function legendClearsFitChip() {
  return page.evaluate((sel) => {
    const card = document.querySelector(sel)
    const legend = card?.querySelector('.map-legend')
    const chip = card?.querySelector('.fitchip')
    if (!legend || !chip) return { clear: false, why: 'legend or fit chip missing' }
    const l = legend.getBoundingClientRect()
    const c = chip.getBoundingClientRect()
    const overlaps = l.left < c.right && c.left < l.right && l.top < c.bottom && c.top < l.bottom
    return {
      clear: !overlaps && l.right <= c.left,
      gap: Math.round(c.left - l.right),
      legendRight: Math.round(l.right),
      chipLeft: Math.round(c.left),
    }
  }, CARD)
}

let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-port-trace', kind: 'ingest', device: DEVICE },
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

// The lanes, so the map has cards to dim rather than boundary-derived
// names -- the same push every other topography scenario makes.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '10.0.10.1/24', network: '10.0.10.0', interface: 'bridge-lan', comment: 'LAN' },
      { address: '10.0.20.1/24', network: '10.0.20.0', interface: 'ether3', comment: 'Servers' },
      { address: '10.0.30.1/24', network: '10.0.30.0', interface: 'ether4', comment: 'IoT' },
      { address: '10.0.40.1/24', network: '10.0.40.0', interface: 'ether5', comment: 'Guest' },
    ],
  })) === 200,
  'the four lane ranges are pushed',
)

// A third table kind this scenario does not otherwise touch, cleared
// explicitly: `live-topography-layout.mjs` runs right before this
// scenario and pushes a DHCP-lease table naming `10.0.10.21` as
// `anna-macbook-pro-16` on this same device and interface. A push
// replaces its own kind's whole table, but ip-address and filter-rule
// pushes above do nothing to a *lease* table, so that name would
// otherwise still be on record when the trace below reads it back --
// and it names the same host this scenario traces by raw IP. Pushing
// an empty lease table makes the answer this scenario's own, not
// whatever a sibling happened to leave named.
check((await push({ kind: 'dhcp-lease', page: 1, pages: 1, records: [] })) === 200, 'this device\'s lease table is cleared, not inherited')

// This scenario shares one live instance with whatever sorted ahead of
// it in the slice (scripts/run-scenarios.sh: filename order, leftovers
// persist). Its escalated callout has to be the ether4->bridge-lan
// refused pair pushed below -- reality.ts's worstUnplannedOf picks
// whichever 'unplanned' pair is busiest, and a sibling's own traffic
// (live-topography-edges.mjs's syslog-fed probe, say) is still sitting
// on the server with a pair no table here names, which is exactly what
// 'unplanned' means. Left alone, that leftover -- not this scenario's
// own pair -- would be the one the callout and the trace escalate to.
//
// So every pair already on record, other than the one this scenario
// means to escalate, gets an explicit forward rule of its own: named
// (accepted) rather than left to fall to 'unplanned' by default. It
// draws no door (no dst-port), and log:true keeps coverageRule.ts
// reading it 'logged' rather than 'dark' -- an unlogged accept still
// makes coverage.ts treat the pair as a boundary-direction nothing logs,
// and Topography.svelte draws that as its own `.cedge` material, which
// is exactly the stray-material bug an earlier version of this fix
// produced when it left log unset. Since none of these interfaces are
// in the /ip address table pushed above, that leaves nothing on the
// map at all -- it only removes a pair from the callout's own contest.
const before = await (await page.request.get(`${URL_BASE}/api/events`)).json()
const TARGET_PAIR = 'ether4|bridge-lan'
const leftoverPairs = new Set()
for (const e of before.events ?? []) {
  if (!e.inInterface || !e.outInterface) continue
  const key = `${e.inInterface}|${e.outInterface}`
  if (key !== TARGET_PAIR) leftoverPairs.add(key)
}
const neutralizers = [...leftoverPairs].map((pair, i) => {
  const [inInterface, outInterface] = pair.split('|')
  return {
    ordinal: 900 + i,
    chain: 'forward',
    action: 'accept',
    inInterface,
    outInterface,
    log: true,
    comment: 'a sibling scenario\'s leftover pair, named here so it cannot outrank this scenario\'s own callout',
  }
})

// A second, independent shape the same shared instance leaves behind:
// the flat map's lane row is capped at five, busiest first
// (`zonesState.zones`, over the whole shared event buffer) -- the same
// cap live-city-port.mjs's own header describes for the isometric city.
// `live-topography-layout.mjs` runs right before this scenario in
// scripts/run-scenarios.sh's filename order and leaves five lanes of
// its own (`bridge-srv`, `bridge-iot`, `bridge-guest`, `bridge-lab`,
// each with real traffic both ways), which can outrank `ether3`
// (Servers) and `ether5` (Guest) below -- both light on traffic of
// their own -- and evict them from the row. A lane with no row entry
// gets no gate for a door to stand on, so `#12`'s and `#31`'s doors
// (both on `ether3`) would silently stop drawing even though the API's
// own answer still names them, and the accepted trace's landing zone
// (`ether3` again) would have nowhere to place its ring.
//
// So this scenario reads the busiest lane already on record (the same
// WAN-exclusion `zonesState.deviceWans` uses: whichever inbound
// interface most often carries a public source) and sizes its own
// `ether3`/`ether5` traffic to clear the cap regardless of what a
// sibling left behind.
function isPublicIp(ip) {
  const m = /^(\d+)\.(\d+)\.(\d+)\.(\d+)$/.exec(ip || '')
  if (!m) return false
  const a = Number(m[1])
  const b = Number(m[2])
  if (a === 10 || a === 127) return false
  if (a === 172 && b >= 16 && b <= 31) return false
  if (a === 192 && b === 168) return false
  return true
}
const wanIn = new Map()
for (const e of before.events ?? []) {
  if (e.inInterface && isPublicIp(e.srcIp)) wanIn.set(e.inInterface, (wanIn.get(e.inInterface) ?? 0) + 1)
}
let wanIface = null
let wanCount = 0
for (const [iface, n] of wanIn) {
  if (n > wanCount) {
    wanIface = iface
    wanCount = n
  }
}
const laneCounts = new Map()
for (const e of before.events ?? []) {
  for (const iface of [e.inInterface, e.outInterface]) {
    if (!iface || iface === wanIface) continue
    laneCounts.set(iface, (laneCounts.get(iface) ?? 0) + 1)
  }
}
// This scenario is about to add four lanes of its own (`bridge-lan`,
// `ether3`, `ether4`, `ether5`), leaving room for at most one existing
// lane to also survive the cap -- so the weakest of its own four has to
// beat the *second*-busiest lane already on record, not the busiest.
const beatCount = [...laneCounts.values()].sort((a, b) => b - a)[1] ?? 0
const printLines = beatCount + 5

// The rule table the doors come from. Three rules name a port; the
// fourth deliberately names none, and must draw no door at all -- a
// rule with no dst-port covers every port and so says nothing about
// any one of them.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      {
        ordinal: 12,
        chain: 'forward',
        action: 'accept',
        protocol: 'tcp',
        dstPort: 445,
        inInterface: 'bridge-lan',
        outInterface: 'ether3',
        log: true,
        comment: 'SMB to the servers',
      },
      // A list, so the door for 3389 comes out of a spec naming two
      // ports rather than one -- the shape the picker has to survive.
      { ordinal: 23, chain: 'input', action: 'drop', dstPort: '3389,445', inInterface: 'ether1', log: true },
      // A range, on a lane nobody talks on: the door that has to stay
      // visible on a rib the port never crossed.
      { ordinal: 31, chain: 'forward', action: 'drop', dstPort: '137-445', inInterface: 'ether4', outInterface: 'ether3', log: true },
      { ordinal: 40, chain: 'forward', action: 'drop', log: true },
      ...neutralizers,
    ],
  })) === 200,
  'the filter table is pushed, three rules naming a port and one naming none',
)

// Two accepted SMB lines, LAN -> Servers, and fourteen refusals from
// IoT: round 49's own unplanned pair, which is what the trace opens on.
// Collected and fed each as one call rather than one per line -- a
// separate TLS handshake and process start per line is the exact cost
// #1061 exists to cut.
const smbLines = [21, 34].map(
  (host, i) =>
    `firewall,info A|smb-out| forward: in:bridge-lan out:ether3, connection-state:new, proto TCP (SYN), 10.0.10.${host}:5${100 + i}->10.0.20.5:445, len 60`,
)
const refusedLines = Array.from(
  { length: 14 },
  (_, i) =>
    `firewall,info D|default drop| forward: in:ether4 out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.30.14:5${200 + i}->10.0.10.21:445, len 60`,
)
// Traffic on other ports, so there is something for the filter to dim
// and something else for the picker to offer.
const webLines = Array.from(
  { length: 6 },
  (_, i) =>
    `firewall,info A|web| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), 10.0.10.${40 + i}:5${300 + i}->203.0.113.9:443, len 60`,
)
// A lane with nothing to do with 445 and no rule naming it: the card
// that has to recede whole while staying on the map. Also this
// scenario's only traffic on `ether5`, and (with `smb-out` above) on
// `ether3` -- sized to clear the lane-row cap (see `printLines` above)
// rather than a fixed handful that only happened to be enough before
// live-topography-layout.mjs existed.
const printLinesArr = Array.from(
  { length: printLines },
  (_, i) =>
    `firewall,info A|print| forward: in:ether5 out:ether3, connection-state:new, proto TCP (SYN), 10.0.40.${10 + (i % 240)}:5${400 + (i % 500)}->10.0.20.9:9100, len 60`,
)

const beforeFeed = await eventsTotal(page)
feedRaw(...smbLines, ...refusedLines, ...webLines, ...printLinesArr)
await waitForEventsTotal(page, beforeFeed + smbLines.length + refusedLines.length + webLines.length + printLinesArr.length)

await page.setViewportSize({ width: 1600, height: 900 })
// zonesState/policyState/coverageState (and the reality picture they
// feed) only refresh on load, not from a push made straight over the
// ingest API -- the lane row, the doors and the escalated callout below
// all need a reload to see this estate and traffic together.
await page.reload()
await page.click('.rail-name >> text=Topography')
// The altitude defaults to the city (#869); these two tools are the
// flat map's, so the scenario has to come left of centre first.
await page.waitForSelector(`${CARD} .altitude input[type="range"]`, { timeout: 15000 })
await page.locator(`${CARD} .altitude input[type="range"]`).fill('1')
await page.waitForSelector(`${CARD} .zone`, { state: 'attached', timeout: 15000 })
await waitForSettle(page, `${CARD} .camera`)

// --- the port pill ------------------------------------------------------

const lanesBefore = await page.locator(`${CARD} .zone`).count()
check(lanesBefore > 0, `the map draws lanes to filter (${lanesBefore})`)

const idle = page.locator(`${CARD} .pills .pill.p`)
check((await idle.textContent())?.trim() === '⌕ port', 'the port pill sits idle bottom-left, where round 49\'s pills were')

await idle.click()
await page.waitForSelector(`${CARD} .pill.p.edit`, { timeout: 10000 })

const picker = await page.evaluate((sel) => {
  const bar = document.querySelector(`${sel} .pill.p.edit`)
  return {
    ports: [...(bar?.querySelectorAll('.ports .chip') ?? [])].map((c) => c.textContent?.trim()),
    protos: [...(bar?.querySelectorAll('.seg .chip') ?? [])].map((c) => c.textContent?.trim()),
    typed: !!bar?.querySelector('input'),
    show: [...(bar?.querySelectorAll('button') ?? [])].some((b) => /show/i.test(b.textContent ?? '')),
  }
}, CARD)
check(picker.ports.includes('445'), `the picker offers a port the window carried (${picker.ports.join(' ')})`)
// 3389 was never logged. It is offered because a pushed rule names it,
// which is the whole reason policy is drawn beside traffic at all.
check(picker.ports.includes('3389'), 'the picker offers a port only a rule names')
check(picker.protos.join(' ') === 'tcp udp', `the protocol chips are tcp and udp (${picker.protos.join(' ')})`)
check(picker.typed, 'a short field takes a typed list')
check(!picker.show, 'there is no Show button: the map filters as the selection changes')

await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^445$/ }).first().click()
await page.waitForSelector(`${CARD} .lit-half`, { state: 'attached', timeout: 10000 })
// The picker has no Done button: clicking away from it closes it, and
// the selection it made stays on the map.
await page.locator(`${CARD} .stage`).click({ position: { x: 20, y: 20 } })
await page.waitForSelector(`${CARD} .pill.p.on`, { timeout: 10000 })
await nextPaint(page)

const filtered = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const lit = [...card.querySelectorAll('.lit-half')]
  const off = [...card.querySelectorAll('.redge.port-off, .cedge.port-off')]
  const doors = [...card.querySelectorAll('.door')].map((d) => ({
    text: d.querySelector('.door-t')?.textContent?.replace(/\s+/g, ' ').trim(),
    shut: d.classList.contains('shut'),
  }))
  return {
    pill: card.querySelector('.pill.p.on')?.textContent?.replace(/\s+/g, ' ').trim(),
    clear: !!card.querySelector('.pill-x'),
    lit: lit.length,
    refused: lit.filter((l) => l.classList.contains('refused')).length,
    off: off.length,
    offGrey: off.every((o) => (o.getAttribute('style') ?? '').includes('var(--fg-muted)')),
    doorGuides: card.querySelectorAll('.door-guide').length,
    doors,
    tallies: [...card.querySelectorAll('.hosttally')].map((t) => t.textContent?.trim()),
    laneOff: card.querySelectorAll('.zone.lane-off').length,
    zones: card.querySelectorAll('.zone').length,
    dotsOff: card.querySelectorAll('.hostrow .dot-off').length,
    dots: card.querySelectorAll('.hostrow .h-dot').length,
    legend: card.querySelector('.map-legend')?.textContent?.replace(/\s+/g, ' ').trim(),
    badges: card.querySelectorAll('.edge-badge').length,
  }
}, CARD)

check(/^⌕ 445\/tcp · \d+ lines? seen · \d+ doors?$/.test(filtered.pill ?? ''), `the pill collapses onto the answer (${filtered.pill})`)
check(filtered.clear, 'the ✕ that clears it is its own control beside the pill')
check(filtered.lit > 0, `the ribs the port crossed keep their verdict ink (${filtered.lit} halves lit)`)
check(filtered.refused > 0, `the refused direction is lit in the alarm ink (${filtered.refused})`)
check(filtered.off > 0 && filtered.offGrey, `every other rib becomes one thin grey line (${filtered.off} dimmed)`)
// Nothing is removed: the dimmed ribs are still on the map, and so is
// every lane card and every host dot.
check(filtered.zones === lanesBefore, `every lane stays drawn (${filtered.zones} of ${lanesBefore})`)
check(filtered.laneOff > 0, `a lane with nothing on the port dims whole (${filtered.laneOff})`)
check(filtered.dotsOff > 0 && filtered.dots > filtered.dotsOff, `hosts off the port recede without leaving the card (${filtered.dotsOff} of ${filtered.dots})`)
check(
  filtered.tallies.some((t) => /^\d+ of \d+ hosts? on 445\/tcp$/.test(t ?? '')),
  `a lane card counts itself against the port (${filtered.tallies.join(' | ')})`,
)
check(
  filtered.doors.some((d) => d.text === '#12 accept' && !d.shut),
  `the accepting rule draws an open door (${JSON.stringify(filtered.doors)})`,
)
check(
  filtered.doors.some((d) => d.text === '#31 drop' && d.shut),
  'a rule whose dst-port is a range draws its door too',
)
check(
  !filtered.doors.some((d) => d.text?.startsWith('#40')),
  'a rule naming no port draws no door: it covers every port and says nothing about this one',
)
check(filtered.doorGuides > 0, `a door on a rib the map draws none of its own brings its own faint one (${filtered.doorGuides})`)
check(/door open/.test(filtered.legend ?? '') && /off the port/.test(filtered.legend ?? ''), `the legend swaps for the filter's own entries (${filtered.legend})`)
const portLegendBox = await legendClearsFitChip()
check(
  portLegendBox.clear,
  `the legend sits clear of the fit chip rather than under it (${JSON.stringify(portLegendBox)})`,
)
check(filtered.badges === 0, 'the map goes quiet about everything the filter is not about')

await page.screenshot({ path: `${OUT}/port.png` })

// --- nothing seen -------------------------------------------------------

await page.locator(`${CARD} .pill.p.on`).click()
await page.waitForSelector(`${CARD} .pill.p.edit`, { timeout: 10000 })
await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^445$/ }).first().click()
await page.locator(`${CARD} .pill.p.edit .ports .chip`, { hasText: /^3389$/ }).first().click()
await page.locator(`${CARD} .stage`).click({ position: { x: 20, y: 20 } })
await page.waitForSelector(`${CARD} .note-t`, { state: 'attached', timeout: 10000 })

const empty = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  return {
    note: card.querySelector('.note-t')?.textContent?.trim(),
    lit: card.querySelectorAll('.lit-half').length,
    doors: card.querySelectorAll('.door').length,
    zones: card.querySelectorAll('.zone').length,
  }
}, CARD)
check(
  /^no logged traffic on 3389\/tcp in the window · one rule names it — #23 /.test(empty.note ?? ''),
  `nothing seen is one line under the map, not an empty state (${empty.note})`,
)
check(empty.lit === 0, 'nothing is lit for a port the window never carried')
check(empty.doors === 1, `the door is still drawn (${empty.doors})`)
check(empty.zones === lanesBefore, `and the map is still there behind the sentence (${empty.zones})`)

await page.screenshot({ path: `${OUT}/port-empty.png` })

await page.locator(`${CARD} .pill-x`).click()
await page.waitForSelector(`${CARD} .lit-half`, { state: 'detached', timeout: 3000 }).catch(() => {})
check((await page.locator(`${CARD} .lit-half`).count()) === 0, 'the ✕ puts the map back')

// --- the trace ----------------------------------------------------------
//
// Opened from the flag's own chip on the map -- round 49's escalated
// unplanned callout, which is where the refused pair is already named.

await page.waitForSelector(`${CARD} .uc-trace`, { state: 'attached', timeout: 15000 })
await page.locator(`${CARD} .uc-trace .uc-trace-t`).click()
await page.waitForSelector(`${CARD} .trace-crumb`, { timeout: 10000 })

// B1 (round 56, #1050): the callout's own `trace ▸` now opens the list
// itself as soon as the answer lands, not only a click on the crumb's
// own "and N more like it" -- live-topography-trace-list.mjs proves the
// list's own two columns; this scenario only needs to know it is open,
// since that is what makes the Esc ladder below two presses rather than
// one.
await page.waitForSelector(`${CARD} .picker`, { timeout: 10000 })

const traced = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const lit = [...card.querySelectorAll('.lit-half')]
  return {
    crumb: card.querySelector('.trace-crumb')?.textContent?.replace(/\s+/g, ' ').trim(),
    verdict: card.querySelector('.trace-chip .chip-verdict')?.textContent?.trim(),
    path: card.querySelector('.trace-chip .chip-t')?.textContent?.trim(),
    refusedChip: !!card.querySelector('.trace-chip.refused'),
    lit: lit.length,
    litRefused: lit.filter((l) => l.classList.contains('refused')).length,
    stop: !!card.querySelector('.trace-stop'),
    ghost: !!card.querySelector('.trace-ghost'),
    ghostNote: card.querySelector('.trace-note')?.textContent?.replace(/\s+/g, ' ').trim(),
    tallies: [...card.querySelectorAll('.hosttally')].map((t) => t.textContent?.trim()),
    legend: card.querySelector('.map-legend')?.textContent?.replace(/\s+/g, ' ').trim(),
    off: card.querySelectorAll('.redge.port-off, .cedge.port-off').length,
  }
}, CARD)

check(/10\.0\.30\.14/.test(traced.crumb ?? ''), `the crumb names who spoke (${traced.crumb})`)
check(/10\.0\.10\.21/.test(traced.crumb ?? ''), 'and who it was speaking to')
check(/445\/tcp/.test(traced.crumb ?? ''), 'and the port')
check(/refused at default drop/.test(traced.crumb ?? ''), 'and the rule that refused it')
// The others are said, never drawn as a union: 14 lines, 13 more.
check(/and 13 more like it/.test(traced.crumb ?? ''), 'and how many more like it there are, in words')
check(/Esc ▸/.test(traced.crumb ?? ''), 'and how to put the map back')
check(traced.refusedChip && /^✕ REFUSED/.test(traced.verdict ?? ''), `the router's decision is a chip beside it (${traced.verdict})`)
check(/^in: ether4 → out: bridge-lan · 445\/tcp/.test(traced.path ?? ''), `the chip names both lanes and the port (${traced.path})`)
// C1 (round 56, #1050): the log names an out-interface (out:
// bridge-lan), so the router forwarded the line before the LAN boundary
// refused it -- both halves light in the alarm ink, same as
// live-topography-trace-list.mjs's own IoT-to-LAN pair proves. Round 53
// had this pair dying at the router with one half lit; C1 moved its ✕
// to the far gate instead.
check(traced.lit === 2 && traced.litRefused === 2, `both halves of the crossing light, in the alarm ink (${traced.lit})`)
check(traced.stop, 'and it still ends at a ✕, now at the far gate rather than the router (C1, round 56)')
// There is nowhere left to draw a dashed rib to once the ✕ already
// stands at the far boundary -- unlike round 53's router-death case
// (an input-chain drop naming no out-interface), which this file does
// not trace.
check(!traced.ghost, `no ghost is drawn to the far gate (${traced.ghost})`)
check(
  /would have reached 10\.0\.10\.21.*stopped at the LAN boundary/.test(traced.ghostNote ?? ''),
  `the far gate's own two grey words say where it stopped, not "never left the router" (${traced.ghostNote})`,
)
check(
  traced.tallies.some((t) => /never reached/.test(t ?? '')),
  `the end it never reached says so (${traced.tallies.join(' | ')})`,
)
check(
  traced.tallies.some((t) => /14× in the window/.test(t ?? '')),
  'and the end it came from carries its own count',
)
// "would have gone" glosses the ghost's own dashed rib; with none drawn
// for a far-gate refusal (C1, round 56) the legend has nothing to swap
// it in for, though the plain accepted/refused/off entries still show.
check(!/would have gone/.test(traced.legend ?? ''), `no ghost, nothing for the legend to gloss (${traced.legend})`)
check(/off the line/.test(traced.legend ?? ''), `but the ordinary entries still show (${traced.legend})`)
const traceLegendBox = await legendClearsFitChip()
check(
  traceLegendBox.clear,
  `and it too sits clear of the fit chip (${JSON.stringify(traceLegendBox)})`,
)
check(traced.off > 0, 'everything else is grey')

await page.screenshot({ path: `${OUT}/trace.png` })

// B1 (round 56, #1050): the list opened on top of the trace above, so
// Esc now has two rungs -- the first closes the list and leaves the
// crumb standing, the second clears the trace underneath it -- rather
// than clearing everything in one press.
await page.keyboard.press('Escape')
await page.waitForSelector(`${CARD} .picker`, { state: 'detached', timeout: 3000 }).catch(() => {})
check((await page.locator(`${CARD} .picker`).count()) === 0, 'the first Esc closes the list')
check((await page.locator(`${CARD} .trace-crumb`).count()) === 1, 'and leaves the crumb standing')

await page.keyboard.press('Escape')
await page.waitForSelector(`${CARD} .trace-crumb`, { state: 'detached', timeout: 3000 }).catch(() => {})
check((await page.locator(`${CARD} .trace-crumb`).count()) === 0, 'the second Esc clears the trace underneath it')
check((await page.locator(`${CARD} .map-legend`).count()) === 0, 'and the legend goes with it')

// --- the other way in, and the other verdict ----------------------------
//
// A stream row's own ⌖, beside the interfaces it is about, and an
// accepted line: the far end is reached rather than refused, so the
// drawing has a ring on it and no ✕ anywhere.

await goTo(page, 'Stream')
const accepted = page
  .locator('.row', { hasText: '10.0.20.5' })
  .filter({ hasText: '445' })
  .first()
await accepted.scrollIntoViewIfNeeded()
await accepted.locator('.cell.iface button[aria-label^="Trace"]').click()
await page.waitForSelector(`${CARD} .trace-crumb`, { timeout: 15000 })
await nextPaint(page)

const reached = await page.evaluate((sel) => {
  const card = document.querySelector(sel)
  const lit = [...card.querySelectorAll('.lit-half')]
  return {
    crumb: card.querySelector('.trace-crumb')?.textContent?.replace(/\s+/g, ' ').trim(),
    verdict: card.querySelector('.trace-chip .chip-verdict')?.textContent?.trim(),
    refusedChip: !!card.querySelector('.trace-chip.refused'),
    lit: lit.length,
    litRefused: lit.filter((l) => l.classList.contains('refused')).length,
    ring: !!card.querySelector('.trace-ring'),
    stop: !!card.querySelector('.trace-stop'),
    ghost: !!card.querySelector('.trace-ghost'),
  }
}, CARD)

check(/accepted at smb-out/.test(reached.crumb ?? ''), `a stream row traces its own line onto the map (${reached.crumb})`)
check(!reached.refusedChip && /^✓ ACCEPTED/.test(reached.verdict ?? ''), `and the chip says so (${reached.verdict})`)
// A crossing is one packet through the router, so both halves light.
check(reached.lit === 2 && reached.litRefused === 0, `both halves of the crossing are lit (${reached.lit})`)
check(reached.ring, 'and a ring marks where it arrived')
check(!reached.stop && !reached.ghost, 'nothing stopped it, so there is no ✕ and no ghost')

await page.screenshot({ path: `${OUT}/trace-accepted.png` })
await page.keyboard.press('Escape')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)

done()
