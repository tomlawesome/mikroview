// SPDX-License-Identifier: AGPL-3.0-only
//
// #1028: a card that grows for the declare form must not come down on
// the subject it is describing.
//
// This is deliberately a live scenario and not a vitest one. The first
// attempt at #1028 was gated in jsdom, where `getBoundingClientRect()`
// returns zeros and every rectangle under test is one the test invented.
// It failed before that fix and passed after it, and said nothing at all
// about the screen -- the defect it was meant to gate was still there,
// photographed, while the test was green. A geometry rule is only proven
// against the geometry the browser actually produced, so every number
// here is read out of the rendered DOM.
//
// The rule, on both surfaces: with the declare form open, the card's own
// rendered rectangle does not intersect the rendered rectangle of any
// subject named in the card's own title -- the zone plates on the 2D
// map, the district plates in the city.

import { session, check, done, feedSyslog as syslog } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

/* ---------------- an estate with a dark lane-to-lane boundary ------- */

syslog(2, 'topo-card-placement-probe')
let DEVICE
for (let i = 0; i < 40 && !DEVICE; i++) {
  await new Promise((r) => setTimeout(r, 250))
  const res = await page.request.get(`${URL_BASE}/api/devices`)
  if (res.ok()) DEVICE = (await res.json()).devices?.[0]?.id
}
check(!!DEVICE, `the instance reports the device events arrive from (${DEVICE})`)

const tokenRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-topo-card-placement', kind: 'ingest', device: DEVICE },
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

// Whole tables, pushed unconditionally: a push replaces its kind's table,
// so this is the same estate standalone and mid-suite.
check(
  (await push({
    kind: 'ip-address',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { address: '10.0.80.1/24', network: '10.0.80.0', interface: 'ether2', comment: 'DarkLane' },
      { address: '10.0.81.1/24', network: '10.0.81.0', interface: 'ether3', comment: 'QuietLane' },
      { address: '10.0.82.1/24', network: '10.0.82.0', interface: 'ether4', comment: 'LitLane' },
    ],
  })) === 200,
  'the zone table is pushed whole',
)
// Lane-to-lane accepts that nothing logs: the boundary is drawn, because
// traffic is permitted across it, and dark, because no rule reports it.
// That is the pair a card names and the pair it must not sit on.
// `bridge1` is deliberately left out of the address table, so it arrives
// as a lane inferred from the boundaries -- this is the estate the defect
// was photographed in, and the estate is what decides where the ground
// plan puts each plate and therefore where the card collides.
check(
  (await push({
    kind: 'filter-rule',
    page: 1,
    pages: 1,
    routerosVersion: '7.23.3 (stable)',
    records: [
      { ordinal: 0, comment: 'LitLane to DarkLane', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'ether4', outInterface: 'ether2' },
      { ordinal: 1, comment: 'DarkLane to LitLane', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'ether2', outInterface: 'ether4' },
      { ordinal: 2, comment: 'QuietLane to LitLane', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'ether3', outInterface: 'ether4' },
      { ordinal: 3, comment: 'bridge1 to LitLane', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether4' },
      { ordinal: 4, comment: 'LitLane to bridge1', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'ether4', outInterface: 'bridge1' },
      { ordinal: 5, comment: 'LAN out to the web', chain: 'forward', action: 'accept', srcAddressList: '', logPrefix: '', inInterface: 'bridge1', outInterface: 'ether1', dstPort: 443, protocol: 'tcp' },
      { ordinal: 6, comment: 'Nothing unsolicited comes in', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: 'D|forward-drop|', log: true, inInterface: 'ether1', outInterface: 'bridge1' },
    ],
  })) === 200,
  'the rule table is pushed whole',
)
await page.reload()

/* ---------------- measuring, in the browser's own numbers ----------- */

/**
 * Read the card and its named subjects out of the rendered DOM.
 *
 * This runs in the page, and it refuses to report a rectangle it cannot
 * see: an element hidden by a `hidden` attribute, `display:none`,
 * `visibility:hidden` or a transparent ancestor still measures as a
 * perfectly clear rectangle, and a rule proven against an invisible
 * layer is the jsdom mistake again with a browser attached.
 *
 * `surface` picks the selectors that differ and nothing else, so both
 * surfaces are measured by one piece of code.
 */
const READ_SURFACE = ({ cardSel, titleSel, surface }) => {
  const tag = (n) => '<' + n.tagName.toLowerCase() + '>'
  const hiddenReason = (el) => {
    for (let n = el; n && n.nodeType === 1; n = n.parentElement) {
      if (n.hasAttribute('hidden')) return 'a hidden ancestor ' + tag(n)
      const cs = getComputedStyle(n)
      if (cs.display === 'none') return 'display:none on ' + tag(n)
      if (cs.visibility === 'hidden' || cs.visibility === 'collapse') return 'visibility:' + cs.visibility + ' on ' + tag(n)
      if (parseFloat(cs.opacity) < 0.05) return 'opacity:' + cs.opacity + ' on ' + tag(n)
    }
    return null
  }
  const measured = (el, what) => {
    if (!el) return { what, found: false }
    const why = hiddenReason(el)
    const r = el.getBoundingClientRect()
    return {
      what,
      found: true,
      visible: why === null && r.width > 0 && r.height > 0 && el.getClientRects().length > 0,
      hiddenBy: why,
      rect: { x: r.left, y: r.top, w: r.width, h: r.height },
    }
  }

  const card = document.querySelector(cardSel)
  // The card's title reads "<A> → <B><small>boundary</small>", so the
  // names are the title element's own text nodes, not its <small>'s.
  const titleEl = card ? card.querySelector(titleSel) : null
  let text = ''
  if (titleEl) for (const n of titleEl.childNodes) if (n.nodeType === 3) text += n.textContent
  const names = text
    .split('→')
    .map((s) => s.trim())
    .filter(Boolean)

  // A subject is the plate the reader is being stopped from seeing.
  //
  // On the 2D map a zone is drawn by one of two layers, and which one
  // depends on the stop: the lane row along the foot (`g.zone` holding
  // `rect.isl`) at clients and services, and the ground plan's scattered
  // district cards (`g.gf-card` holding `rect.gf-plate`) at zones, where
  // `.camera.cam-zones .isl-card` takes the lane row to `opacity: 0`.
  // Both label their group with the zone's own name, so both are found
  // the same way -- and only the layer actually on screen is asserted
  // against, because a rule proven against the hidden one is proven
  // against nothing.
  const subjects = []
  let searched
  if (surface === 'flat') {
    const groups = Array.from(document.querySelectorAll('[data-card="topography"] g.zone[aria-label], [data-card="topography"] g.gf-card[aria-label]'))
    searched = groups.map((g) => g.getAttribute('class') + ' :: ' + g.getAttribute('aria-label'))
    for (const name of names) {
      for (const g of groups.filter((el) => el.getAttribute('aria-label') === 'Open the stream filtered to ' + name)) {
        // The plate itself, not the whole group: a lane group also holds
        // the host dots hanging below its card.
        const plate = g.querySelector('rect.isl, rect.gf-plate')
        if (!plate) continue
        const m = measured(plate, name + (g.classList.contains('gf-card') ? ' ground-plan plate' : ' lane plate'))
        if (m.visible) subjects.push(m)
      }
    }
  } else {
    const plates = Array.from(document.querySelectorAll('[data-card="topography"] .city g.plate'))
    const titleOf = (p) => (p.querySelector('title') ? p.querySelector('title').textContent : '')
    searched = plates.map(titleOf)
    for (const name of names) {
      const p = plates.find((el) => titleOf(el).startsWith(name + ' district'))
      if (!p) continue
      const m = measured(p, name + ' district plate')
      if (m.visible) subjects.push(m)
    }
  }

  return {
    card: measured(card, surface === 'flat' ? 'the boundary card' : 'the wall card'),
    formOpen: !!(card && card.querySelector('.form')),
    names,
    subjects,
    searched: JSON.stringify(searched),
  }
}

/**
 * Move the real pointer onto a shape, at a point that really hits it.
 *
 * Playwright aims at an element's bounding-box centre, and for an SVG
 * shape -- a boundary's rib, a city wall -- the centre of the box is
 * usually not on the shape at all, so the stage swallows the event and
 * the hover never arrives. Sampling the box and asking the browser's own
 * `elementFromPoint` which node is on top finds a point that does, and
 * the pointer is then moved there for real rather than the event being
 * synthesised onto the element.
 */
async function hoverShape(page, locator) {
  const handle = await locator.elementHandle()
  if (!handle) return null
  const point = await page.evaluate((el) => {
    const r = el.getBoundingClientRect()
    if (!(r.width > 0) || !(r.height > 0)) return null
    for (let i = 1; i <= 15; i++) {
      for (let j = 1; j <= 15; j++) {
        const x = r.left + (r.width * i) / 16
        const y = r.top + (r.height * j) / 16
        const top = document.elementFromPoint(x, y)
        if (top !== null && (top === el || el.contains(top))) return { x, y }
      }
    }
    return null
  }, handle)
  await handle.dispose()
  if (!point) return null
  await page.mouse.move(point.x, point.y)
  return point
}

const overlap = (a, b) => {
  const w = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
  const h = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
  return w > 0 && h > 0 ? w * h : 0
}
const r2 = (n) => Math.round(n * 10) / 10
const show = (r) => `x=${r2(r.x)} y=${r2(r.y)} w=${r2(r.w)} h=${r2(r.h)}`

/** The one rule, asserted the same way on both surfaces. */
function assertClear(surface, m) {
  check(m.card.found, `${surface}: the card is in the DOM`)
  if (!m.card.found) return
  check(m.card.visible, `${surface}: the card is actually visible (${m.card.hiddenBy ?? show(m.card.rect)})`)
  check(m.formOpen, `${surface}: the declare form is open, so the card has grown`)
  check(m.grew, `${surface}: the card is taller with the form open than without it (${r2(m.heightBefore)} → ${r2(m.card.rect.h)})`)

  // `subjects` holds only the plates that are really on screen. If that
  // is empty the overlap assertions below would all pass while proving
  // nothing at all, which is exactly how #1028's first fix was declared
  // good, so an empty set is a failure in its own right.
  check(m.names.length >= 1, `${surface}: the card's title names its subjects (${JSON.stringify(m.names)})`)
  check(
    m.subjects.length >= 1,
    `${surface}: a subject named in the title "${m.names.join(' → ')}" is drawn and visible` +
      // The full search only matters when nothing was found; printed
      // every time, it buries the assertion that did the work.
      (m.subjects.length >= 1 ? ` (${m.subjects.map((s) => s.what).join(', ')})` : ` — found none; searched ${m.searched}`),
  )

  for (const s of m.subjects) {
    const area = overlap(m.card.rect, s.rect)
    check(
      area === 0,
      `${surface}: the card does not cover ${JSON.stringify(s.what)}, which its own title names` +
        ` — card ${show(m.card.rect)}, plate ${show(s.rect)}, overlap ${r2(area)}px²`,
    )
  }
}

/* ---------------- the 2D map ---------------- */

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .altitude input[type="range"]', { timeout: 15000 })

const FLAT_CARD = '[data-card="topography"] .card[role="dialog"]'

/**
 * Open every dark boundary's card at one 2D stop, pin each so the declare
 * form goes in, and measure what that did.
 *
 * Both stops that draw zones are checked, because they draw them with
 * different layers in different places: services draws the lane row along
 * the foot, and zones replaces it with the ground plan's scattered
 * district cards. A card placed against one layer's rectangles while the
 * other is on screen is placed against rectangles nobody can see, which
 * is the whole of #1028.
 */
async function flatAt(altitude, what) {
  // Let go of anything still pinned, so the next stop starts clean.
  const held = page.locator(`${FLAT_CARD} .pin`)
  if ((await held.count()) > 0) await held.first().click().catch(() => {})
  await page.locator('[data-card="topography"] .altitude input[type="range"]').fill(String(altitude))
  // Round 49 draws the coverage material under everything, on every lens
  // -- there is no lens to pick any more.
  await page.waitForSelector('[data-card="topography"] .cedge.dark', { timeout: 15000 })
  // The camera transition is 0.35s, and a plate measured mid-flight is
  // not where it comes to rest.
  await new Promise((r) => setTimeout(r, 900))

  // Every dark boundary, not just the first. Which one collides depends
  // on where the estate's own layout puts the plates, so opening one and
  // calling the rule proven is how a placement bug hides: at 1280x720
  // the pair that collides is not the first pair drawn.
  const darkEdges = page.locator('[data-card="topography"] .cov-g:has(.cedge.dark)')
  const edgeCount = await darkEdges.count()
  check(edgeCount >= 1, `${what}: the map draws a dark boundary to open (${edgeCount})`)

  let measured = 0
  for (let i = 0; i < edgeCount; i++) {
    // Let go of the previous card, and off the shape, or entering the
    // next one from inside this one fires no `pointerenter` at all.
    const pinned = page.locator(`${FLAT_CARD} .pin`)
    if ((await pinned.count()) > 0) await pinned.first().click().catch(() => {})
    await page.mouse.move(4, 4)
    await new Promise((r) => setTimeout(r, 250))

    // Hover the dark material to open its card. The pointer then travels
    // to the card to reach the pin, which is what #1027's grace period
    // is for: if that regressed, the card is gone before this click
    // lands, and the scenario says so here rather than mysteriously
    // later.
    if ((await hoverShape(page, darkEdges.nth(i))) === null) continue
    try {
      await page.waitForSelector(FLAT_CARD, { timeout: 3000 })
    } catch {
      continue
    }

    const heightBefore = await page.locator(FLAT_CARD).evaluate((el) => el.getBoundingClientRect().height)
    await page.click(`${FLAT_CARD} .pin`)
    await page.waitForSelector(`${FLAT_CARD} .form`, { timeout: 5000 })
    check(await page.locator(FLAT_CARD).isVisible(), `${what}: the card survived the journey from the boundary to the pin (#1027)`)
    // The placement is recomputed from a ResizeObserver report, so let
    // the browser lay out and re-place before measuring.
    await page.waitForFunction((sel) => document.querySelector(sel)?.classList.contains('placed') === true, FLAT_CARD, { timeout: 5000 })
    await new Promise((r) => setTimeout(r, 400))

    const m = await page.evaluate(READ_SURFACE, { cardSel: FLAT_CARD, titleSel: '.t .n', surface: 'flat' })
    m.heightBefore = heightBefore
    m.grew = m.card.found && m.card.rect.h > heightBefore + 1
    assertClear(`${what}, ${m.names.join(' → ')}`, m)
    measured++
  }
  check(measured >= 1, `${what}: at least one boundary card was opened and measured (${measured} of ${edgeCount})`)
}

// Zones first: that is the stop the defect was photographed at, where the
// ground plan is what a reader can actually see.
await flatAt(2, 'the 2D map at zones')
await flatAt(1, 'the 2D map at services')

// Let the card go and take the pointer off the map, so the city arrives
// with nothing pinned and nothing hovered.
const stillPinned = page.locator(`${FLAT_CARD} .pin`)
if ((await stillPinned.count()) > 0) await stillPinned.first().click().catch(() => {})
await page.mouse.move(4, 4)
await page.waitForSelector(FLAT_CARD, { state: 'detached', timeout: 5000 }).catch(() => {})
await new Promise((r) => setTimeout(r, 300))

/* ---------------- the city ---------------- */

// The same rule, on the other surface, so it is proven on both rather
// than fixed on one.
const CITY_CARD = '[data-card="topography"] .bcard[role="dialog"]:not(.hcard)'

// Which city stop shows a wall with a boundary behind it depends on the
// estate -- a wall face whose district has no gate on that side has no
// boundary to describe and opens nothing (City.svelte's `wallCard`) -- so
// the stops are searched rather than assumed. Walk them, and the walls at
// each, until one opens a card offering the declare form: a boundary that
// is already logged has no form to grow, and measuring that card would be
// measuring a card that never grows.
let cityOpened = false
let wallsSeen = 0
for (const stop of [3, 4, 5, 6]) {
  if (cityOpened) break
  await page.locator('[data-card="topography"] .altitude input[type="range"]').fill(String(stop))
  await page.waitForSelector('[data-card="topography"] .city g.plate', { timeout: 15000 }).catch(() => {})
  await new Promise((r) => setTimeout(r, 900))

  const walls = page.locator('[data-card="topography"] g.wall-hot')
  const wallCount = await walls.count()
  wallsSeen += wallCount
  for (let i = 0; i < wallCount && !cityOpened; i++) {
    await page.mouse.move(4, 4)
    await new Promise((r) => setTimeout(r, 200))
    if ((await hoverShape(page, walls.nth(i))) === null) continue
    try {
      await page.waitForSelector(CITY_CARD, { timeout: 1500 })
    } catch {
      continue
    }
    const pin = page.locator(`${CITY_CARD} .pin`)
    if ((await pin.count()) === 0) continue
    await pin.first().click()
    try {
      await page.waitForSelector(`${CITY_CARD} .form`, { timeout: 1500 })
      cityOpened = true
    } catch {
      // Unpin and try the next wall.
      if ((await pin.count()) > 0) await pin.first().click()
    }
  }
}
check(wallsSeen >= 1, `the city draws walls to open (${wallsSeen} across its stops)`)
check(cityOpened, 'the city: a wall card opened with its declare form, so there is a grown card to measure')

if (cityOpened) {
  // The city's card is measured with the form already in, so its
  // pre-form height is the difference rather than a second reading.
  const cityHeightBefore = await page.evaluate((sel) => {
    const el = document.querySelector(sel)
    const form = el?.querySelector('.form')
    return el && form ? el.getBoundingClientRect().height - form.getBoundingClientRect().height : 0
  }, CITY_CARD)
  await page.waitForFunction((sel) => document.querySelector(sel)?.classList.contains('placed') === true, CITY_CARD, { timeout: 5000 })
  await new Promise((r) => setTimeout(r, 400))

  const city = await page.evaluate(READ_SURFACE, { cardSel: CITY_CARD, titleSel: '.bc-t .n', surface: 'city' })
  city.heightBefore = cityHeightBefore
  city.grew = city.card.found && city.card.rect.h > cityHeightBefore + 1
  assertClear('the city', city)
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
