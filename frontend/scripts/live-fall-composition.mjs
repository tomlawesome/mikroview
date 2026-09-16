// SPDX-License-Identifier: AGPL-3.0-only
//
// #1204: the fall's composition, at the two sizes Fable's 2026-09-15
// ruling names. The owner's full-width screenshot showed the alert chip
// row cut through the middle of its words by the top of the screen,
// with every lane empty and the rig taking more height than the window
// had. The ruling's done-when is a layout fact, and layout is the one
// thing jsdom cannot answer: "at 1920×1080 and 1366×768 the chips are
// whole with no scrolling, and scrolling the rig reaches the foot".
// Fall.svelte.test.ts pins the two drawing rules from the same ruling
// (the caption at the head of the pour, and the ranked label column);
// this is the half that needs a real browser and a real viewport.
//
// Its estate is the ruling's own worst case: seven boundaries that log
// and that nothing feeds, so every lane is quiet and the fall is at its
// tallest, plus one that never logs so the chip row is never empty.
// Interface pairs (ether30..ether36, ether37) no other scenario in this
// suite names, and the table is reset to the bare non-logging rule at
// the end, so nothing downstream inherits this one's push -- the same
// discipline live-fall-watch-broken.mjs keeps for the same reason.

import { session, feedSyslog, check, responsive, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const OUT = process.env.FALL_SHOTS || '/tmp/fall1204'

// The two sizes the ruling's done-when names, and a third that is not
// in it. 1366×768 is the tight one of the two: the rig is 800 units
// tall on its own, so its foot only comes into reach by scrolling the
// rig. 1366×420 is the short window where the scene used to become a
// second scrolling box around the chip row -- the fall's own floor of
// 320 units for the rig, plus its head, is more than that window has --
// which is the state the ruling rules out ("the chip row and the column
// headers stay put"). Neither of the first two reproduced the owner's
// clipped chip row on their own, so this is the size that pins the rule
// rather than the screenshot.
const SIZES = [
  { width: 1920, height: 1080 },
  { width: 1366, height: 768 },
  { width: 1366, height: 420 },
]

const { page, consoleErrors } = await session({ landing: 'fall', viewport: SIZES[0] })

feedSyslog(3, 'fall-composition-probe')

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : null }
}

// The ingest token must be scoped to exactly the device feedSyslog's
// events carry, or the pushed table attaches to a different device and
// every boundary below reports 'unknown' instead of 'observed'/'dark'
// -- the same lookup live-waterfall.mjs makes for the same reason.
let device
for (let i = 0; i < 40 && !device; i++) {
  await new Promise((r) => setTimeout(r, 250))
  device = (await api('GET', '/api/devices')).body?.devices?.[0]?.id
}
check(!!device, `the instance reports the device events arrive from (${device})`)

const tokenRes = await api('POST', '/api/tokens', { name: 'fall-composition', kind: 'ingest', device })
check(tokenRes.status === 201, `an ingest token is issued (${tokenRes.status})`)
const token = tokenRes.body?.value

async function push(records) {
  const res = await fetch(`${URL_BASE}/api/ingest/routeros`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ kind: 'filter-rule', page: 1, pages: 1, records }),
  })
  return res.status
}

const QUIET_LANES = 7
const records = Array.from({ length: QUIET_LANES }, (_, i) => ({
  ordinal: i,
  comment: `composition probe ${i}`,
  chain: 'forward',
  action: 'drop',
  log: true,
  logPrefix: `D|fall-composition-${i}|`,
  inInterface: `ether${30 + i}`,
  outInterface: `bridge${30 + i}`,
}))
records.push({
  ordinal: QUIET_LANES,
  comment: 'composition probe (never logs)',
  chain: 'forward',
  action: 'drop',
  log: false,
  inInterface: 'ether37',
  outInterface: 'bridge37',
})
check((await push(records)) === 200, `a ${records.length}-boundary filter table is accepted`)

// ── what "whole, with nothing scrolling" means, measured ──────────────
// Everything is read in one evaluate so the numbers all describe the
// same frame.
async function measure() {
  return page.evaluate(() => {
    const doc = document.documentElement
    const box = (el) => {
      const r = el.getBoundingClientRect()
      return { top: r.top, bottom: r.bottom, left: r.left, right: r.right }
    }
    const rig = document.querySelector('.fall .rig')
    const svg = rig?.querySelector('svg')
    const fall = document.querySelector('.fall')
    const deck = document.querySelector('.deck')
    const card = document.querySelector('.card[data-card="fall"]')
    return {
      vw: window.innerWidth,
      vh: window.innerHeight,
      scrollY: window.scrollY,
      docScrollTop: document.scrollingElement?.scrollTop ?? 0,
      docOverflow: doc.scrollHeight - doc.clientHeight,
      bodyOverflow: document.body.scrollHeight - window.innerHeight,
      chips: [...document.querySelectorAll('.fall .attention .att')].map((el) => ({
        ...box(el),
        text: (el.textContent ?? '').trim().slice(0, 44),
      })),
      fallOverflow: fall ? fall.scrollHeight - fall.clientHeight : null,
      fallScrollTop: fall?.scrollTop ?? null,
      // The deck rolls between scenes by design; what matters here is
      // that it is resting on the fall's own card, not part-way past it.
      deckOffset: deck && card ? card.getBoundingClientRect().top - deck.getBoundingClientRect().top : null,
      rig: rig
        ? {
            scrollTop: rig.scrollTop,
            scrollHeight: rig.scrollHeight,
            clientHeight: rig.clientHeight,
            ...box(rig),
          }
        : null,
      svgBottom: svg ? svg.getBoundingClientRect().bottom : null,
      quietAnnoY: [...document.querySelectorAll('.fall .quiet-anno')].map((el) => Number(el.getAttribute('y'))),
      bands: document.querySelectorAll('.fall .band').length,
    }
  })
}

function chipsWhole(m) {
  return m.chips.every((c) => c.top >= 0 && c.bottom <= m.vh + 0.5 && c.left >= 0 && c.right <= m.vw + 0.5)
}

function worstChip(m) {
  return JSON.stringify(m.chips.find((c) => c.top < 0 || c.bottom > m.vh + 0.5) ?? null)
}

let lastWidth = null
for (const size of SIZES) {
  const at = `${size.width}×${size.height}`
  await page.setViewportSize(size)
  // A change of width is reloaded rather than merely resized: two of the
  // app's width rules are read once at module load (live-browser.mjs's
  // own note on this), and this scenario is about the state a fresh
  // arrival at the fall is in. A change of height alone is not -- and
  // must not be, because the roll rail's own buttons leave the viewport
  // in a short window, so goTo would have nothing to click.
  if (size.width !== lastWidth) {
    await page.reload({ waitUntil: 'networkidle' })
    await page.waitForSelector('#main-content', { timeout: 15000 })
    await goTo(page, 'The fall')
    lastWidth = size.width
  }
  await page.waitForTimeout(300)
  // Quiet lanes, not a lane count: the narrower window pages some of
  // the eight boundaries out of view (#722's sizing policy), and the
  // rig is 800 units tall either way -- what this scenario needs on
  // screen is the all-quiet fall, not any particular number of it.
  await page.waitForFunction(() => document.querySelectorAll('.fall .quiet-anno').length > 0, null, {
    timeout: 25000,
  })

  let m = await measure()
  check(m.bands > 0, `${at}: the fall draws its quiet lanes (${m.bands} of ${QUIET_LANES + 1} boundaries on this page)`)
  check(m.chips.length > 0, `${at}: the chip row carries something to clip (${m.chips.length} chips)`)
  check(chipsWhole(m), `${at}: every alert chip is whole inside the viewport -- worst ${worstChip(m)}`)
  check(m.scrollY === 0 && m.docScrollTop === 0, `${at}: the page is not scrolled (scrollY ${m.scrollY}/${m.docScrollTop})`)
  check(m.docOverflow <= 1 && m.bodyOverflow <= 1, `${at}: no page-level overflow (doc ${m.docOverflow}, body ${m.bodyOverflow})`)
  check(m.fallOverflow !== null && m.fallOverflow <= 1, `${at}: the scene itself does not scroll (overflow ${m.fallOverflow})`)
  check(Math.abs(m.deckOffset ?? 99) < 2, `${at}: the deck rests on the fall's own card (offset ${m.deckOffset})`)

  // The caption the same ruling moved: at the head of the pour (the rig
  // draws its floor at 760), not two thirds of the way down it.
  check(
    m.quietAnnoY.length > 0 && m.quietAnnoY.every((y) => y > 196 && y < 240),
    `${at}: the quiet caption sits at the head of the pour -- got ${JSON.stringify(m.quietAnnoY)}`,
  )

  await page.screenshot({ path: `${OUT}/fall-${size.width}x${size.height}.png` })

  // The chip row is not inside a scrolling box: asking the scene itself
  // to scroll moves nothing, at any height.
  await page.$eval('.fall', (el) => {
    el.scrollTop = 999
  })
  m = await measure()
  check(
    chipsWhole(m) && (m.fallScrollTop ?? 0) === 0,
    `${at}: the scene cannot be scrolled out from under the chip row (scrollTop ${m.fallScrollTop}) -- worst ${worstChip(m)}`,
  )

  // A wheel over the rig moves the rig, and nothing else: the deck's
  // roll between scenes is its own gesture (Deck.svelte), and the chip
  // row above the rig is not part of what scrolls. Only worth asserting
  // where the rig really is taller than the space it has -- at 1080 it
  // fits whole, and a wheel there is the deck's to answer.
  if (m.rig.scrollHeight > m.rig.clientHeight + 2) {
    await page.mouse.move(size.width / 2, m.rig.top + m.rig.clientHeight / 2)
    await page.mouse.wheel(0, 700)
    await page.waitForTimeout(300)
    m = await measure()
    check(m.rig.scrollTop > 0, `${at}: a wheel over the rig scrolls the rig (scrollTop ${m.rig.scrollTop})`)
    check(
      chipsWhole(m) && Math.abs(m.deckOffset ?? 99) < 2 && m.scrollY === 0,
      `${at}: ...and carries nothing above it away -- worst ${worstChip(m)}, deck offset ${m.deckOffset}`,
    )
  }

  // ...and the rig reaches its own foot, which is what #1141 chose
  // scrolling for in the first place.
  await page.$eval('.fall .rig', (el) => {
    el.scrollTop = el.scrollHeight
  })
  await page.waitForTimeout(200)
  m = await measure()
  const atFoot = m.rig.scrollTop + m.rig.clientHeight >= m.rig.scrollHeight - 2
  check(atFoot, `${at}: the rig scrolls to its own end (${m.rig.scrollTop} + ${m.rig.clientHeight} of ${m.rig.scrollHeight})`)
  check(
    m.svgBottom <= m.rig.bottom + 2,
    `${at}: the foot of the rig -- port labels and "+n quieter" -- comes into view (svg bottom ${m.svgBottom?.toFixed(1)} against ${m.rig.bottom.toFixed(1)})`,
  )
  check(
    chipsWhole(m),
    `${at}: the chip row stays put while the rig scrolls -- worst ${worstChip(m)}`,
  )
  await page.screenshot({ path: `${OUT}/fall-${size.width}x${size.height}-foot.png` })
}

// --- Cleanup: leave no state a later scenario inherits ---------------------
check((await push([{ ordinal: 0, chain: 'forward', action: 'drop' }])) === 200, 'the table is reset to non-logging for whatever runs next')

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors.slice(0, 3))}`)

done()
