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
import { session, check, done, feedRaw, goTo } from './live-browser.mjs'
import { mkdirSync } from 'node:fs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.PORT_TRACE_SHOTS || '/tmp/1018-shots'
mkdirSync(OUT, { recursive: true })

const CARD = '[data-card="topography"]'
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
    ],
  })) === 200,
  'the filter table is pushed, three rules naming a port and one naming none',
)

// Two accepted SMB lines, LAN -> Servers, and fourteen refusals from
// IoT: round 49's own unplanned pair, which is what the trace opens on.
for (const [i, host] of [21, 34].entries()) {
  feedRaw(
    `firewall,info A|smb-out| forward: in:bridge-lan out:ether3, connection-state:new, proto TCP (SYN), 10.0.10.${host}:5${100 + i}->10.0.20.5:445, len 60`,
  )
}
for (let i = 0; i < 14; i++) {
  feedRaw(
    `firewall,info D|default drop| forward: in:ether4 out:bridge-lan, connection-state:new, proto TCP (SYN), 10.0.30.14:5${200 + i}->10.0.10.21:445, len 60`,
  )
}
// Traffic on other ports, so there is something for the filter to dim
// and something else for the picker to offer.
for (let i = 0; i < 6; i++) {
  feedRaw(
    `firewall,info A|web| forward: in:bridge-lan out:ether1, connection-state:new, proto TCP (SYN), 10.0.10.${40 + i}:5${300 + i}->203.0.113.9:443, len 60`,
  )
}
// A lane with nothing to do with 445 and no rule naming it: the card
// that has to recede whole while staying on the map.
for (let i = 0; i < 3; i++) {
  feedRaw(
    `firewall,info A|print| forward: in:ether5 out:ether3, connection-state:new, proto TCP (SYN), 10.0.40.${10 + i}:5${400 + i}->10.0.20.9:9100, len 60`,
  )
}

await new Promise((r) => setTimeout(r, 1500))
await page.setViewportSize({ width: 1600, height: 900 })
await page.reload()
await page.click('.rail-name >> text=Topography')
// The altitude defaults to the city (#869); these two tools are the
// flat map's, so the scenario has to come left of centre first.
await page.waitForSelector(`${CARD} .altitude input[type="range"]`, { timeout: 15000 })
await page.locator(`${CARD} .altitude input[type="range"]`).fill('1')
await page.waitForSelector(`${CARD} .zone`, { state: 'attached', timeout: 15000 })
await page.waitForTimeout(600)

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
await page.waitForTimeout(400)

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
await page.waitForTimeout(300)
check((await page.locator(`${CARD} .lit-half`).count()) === 0, 'the ✕ puts the map back')

// --- the trace ----------------------------------------------------------
//
// Opened from the flag's own chip on the map -- round 49's escalated
// unplanned callout, which is where the refused pair is already named.

await page.waitForSelector(`${CARD} .uc-trace`, { state: 'attached', timeout: 15000 })
await page.locator(`${CARD} .uc-trace .uc-trace-t`).click()
await page.waitForSelector(`${CARD} .trace-crumb`, { timeout: 10000 })
await page.waitForTimeout(600)

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
    ghostNote: card.querySelector('.trace-note')?.textContent?.trim(),
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
check(traced.lit === 1 && traced.litRefused === 1, `one half is lit, in the verdict ink (${traced.lit})`)
check(traced.stop, 'and it ends at a ✕ on the router')
check(traced.ghost, 'the rib it would have taken is dashed')
check(
  traced.ghostNote === 'would have reached 10.0.10.21 · never left the router',
  `with the note beside it (${traced.ghostNote})`,
)
check(
  traced.tallies.some((t) => /never reached/.test(t ?? '')),
  `the end it never reached says so (${traced.tallies.join(' | ')})`,
)
check(
  traced.tallies.some((t) => /14× in the window/.test(t ?? '')),
  'and the end it came from carries its own count',
)
check(/would have gone/.test(traced.legend ?? ''), `the legend swaps for the trace's own entries (${traced.legend})`)
check(traced.off > 0, 'everything else is grey')

await page.screenshot({ path: `${OUT}/trace.png` })

await page.keyboard.press('Escape')
await page.waitForTimeout(300)
check((await page.locator(`${CARD} .trace-crumb`).count()) === 0, 'Esc clears the trace')
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
await page.waitForTimeout(600)

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
await page.waitForTimeout(300)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)

done()
