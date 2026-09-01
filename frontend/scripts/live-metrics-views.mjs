// SPDX-License-Identifier: AGPL-3.0-only
//
// #488: metrics ships three views of one data set -- Seismograph
// (default), Register, Table -- chosen in the page header and persisted
// as a per-user preference, with the cursor's minute surviving every
// switch (docs/design/screens/metrics/DESIGN.md).
//
// Three claims here cannot be proved anywhere but a real browser:
//
//  - The drawn views measure their own box and size their SVG in real
//    CSS pixels. jsdom reports every box as zero, so a unit test renders
//    a drum at its fallback width and would pass while the real page
//    drew nothing.
//  - "Persisted and applied before first paint" is a claim about a
//    reload: the preference has to be in localStorage *and* read
//    synchronously at module load. A store test proves the write; only a
//    reload proves the read beats the paint.
//  - "The cursor's selected minute survives every view switch" is a
//    claim across three components mounting and unmounting, which is
//    exactly the wiring a mocked store hides.
//
// Note on waiting: page.isVisible() returns immediately, so a check
// written with it races the render and passes or fails on timing. Every
// wait below goes through a locator's waitFor().
//
// And a second trap this scenario walked straight into on its first run:
// **an SVG <line> is never "visible" to Playwright.** Visibility is
// decided on getBoundingClientRect(), which for SVG returns the geometry
// box, not the stroked one -- so a vertical line has width 0, an empty
// box, and resolves hidden however plainly the operator can see it. The
// amber brink edge fails the same test, and it is on screen in every
// screenshot of this page. So the cursor is waited for by its band (a
// real 8px-wide rect at the same x) and then asserted on its geometry
// and its stroke, which is what "the cursor is drawn here" actually
// means. Do not "fix" a hidden line by widening the timeout: it will
// never become visible.

import { session, feedSyslog, feedPortScan, waitForFlag, check, responsive, done, goTo } from './live-browser.mjs'

// Enough traffic for several minutes of the hour to carry a rate, and a
// scan so at least one flag type has an episode to draw a tick for.
feedSyslog(240, 'metrics-views')
feedPortScan(20, '203.0.113.44')

const { page, consoleErrors } = await session({ waitForEvents: 100 })
await waitForFlag(page, '203.0.113.44')

const VIEW_BUTTON = (name) => `.views button:text-is("${name}")`
const SEISMOGRAPH = '.drum svg'
const REGISTER = '.register .paper svg'
const TABLE = '.table-view table'
// The cursor's band is the part with a real box; the line beside it is
// what carries the geometry. See the note above for why the line itself
// can never be waited on.
const cursorBand = (root) => `${root} rect.cursor-band`

/**
 * Reads the drawn cursor: is it there, is it a real line across the
 * paper, and is it wearing the time colour rather than a series ink.
 */
async function cursorLine(page, root) {
  const loc = page.locator(`${root} line.cursor`)
  if ((await loc.count()) === 0) return null
  return loc.evaluate((el) => {
    const resolveToken = (name) => {
      const probe = document.createElement('span')
      probe.style.color = getComputedStyle(document.documentElement).getPropertyValue(name)
      document.body.appendChild(probe)
      const resolved = getComputedStyle(probe).color
      probe.remove()
      return resolved
    }
    const x1 = Number(el.getAttribute('x1'))
    const x2 = Number(el.getAttribute('x2'))
    const y1 = Number(el.getAttribute('y1'))
    const y2 = Number(el.getAttribute('y2'))
    return {
      vertical: x1 === x2,
      horizontal: y1 === y2,
      length: Math.max(Math.abs(x2 - x1), Math.abs(y2 - y1)),
      stroke: getComputedStyle(el).stroke,
      // The stroked box, unlike the geometry box, is not empty -- this is
      // the number that proves the line occupies real pixels.
      painted: el.getBoundingClientRect().height + el.getBoundingClientRect().width,
      // --now resolves to a hex in the token, to rgb() in a computed
      // stroke, so the two are compared after a round trip through a
      // throwaway element rather than as strings.
      timeColour: getComputedStyle(el).stroke === resolveToken('--now'),
    }
  })
}

/** apiUrl resolves a path against the page's own origin, for page.request. */
function apiUrl(page, path) {
  return new URL(path, page.url()).toString()
}

// Mirrors lib/format.ts's formatHM exactly, so a minute label read from
// GET /api/stats/tops (a bare ISO string) can be matched against the
// same HH:MM the table itself prints -- Node and the browser share one
// OS clock/timezone in this harness, which is what makes the two agree.
function hmLabel(iso) {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

/**
 * tableAgreesWithTops polls GET /api/stats/tops and the rendered table
 * together until every row's top-port/top-talker cell matches a fresh
 * answer, or the deadline passes.
 *
 * Polled rather than checked once: Metrics.svelte's own tops poll
 * (TOPS_POLL_MS) runs on a 5s cadence independent of this fetch, so a
 * single fresh read can legitimately be a few seconds ahead of what the
 * page has painted. That is a timing gap, not a defect, and the two
 * should converge well inside the deadline.
 */
async function tableAgreesWithTops(page, deadlineMs) {
  const deadline = Date.now() + deadlineMs
  let detail = 'never sampled'
  while (Date.now() < deadline) {
    const res = await page.request.get(apiUrl(page, '/api/stats/tops'))
    if (res.ok()) {
      const body = await res.json()
      const byLabel = new Map((body.tops ?? []).map((t) => [hmLabel(t.time), t]))
      const rows = await page.$$eval('.table-view tbody tr', (trs) =>
        trs.map((tr) => ({
          minute: tr.querySelector('th button.minute')?.textContent.trim(),
          port: tr.querySelectorAll('td.top')[0]?.textContent.trim(),
          talker: tr.querySelectorAll('td.top')[1]?.textContent.trim(),
        })),
      )
      const mismatches = []
      for (const row of rows) {
        const t = byLabel.get(row.minute)
        const wantPort = t && t.complete && t.port ? t.port : '—'
        const wantTalker = t && t.complete && t.talker ? t.talker : '—'
        if (row.port !== wantPort || row.talker !== wantTalker) mismatches.push({ ...row, wantPort, wantTalker })
      }
      if (rows.length > 0 && mismatches.length === 0) return { ok: true, rows: rows.length }
      detail = JSON.stringify(mismatches.slice(0, 3))
    }
    await page.waitForTimeout(500)
  }
  return { ok: false, detail }
}

await goTo(page, 'Metrics')

// --- The default view, actually drawn -----------------------------------
await page.locator(SEISMOGRAPH).waitFor({ state: 'visible', timeout: 10000 })
check(true, 'Metrics opens on the seismograph and draws it')

const buttons = await page.$$eval('.views button', (els) => els.map((e) => e.textContent.trim()))
check(
  JSON.stringify(buttons) === JSON.stringify(['Seismograph', 'Register', 'Table']),
  `all three views are offered in the page header -- got ${JSON.stringify(buttons)}`,
)

const pressed = await page.$eval('.views button[aria-pressed="true"]', (e) => e.textContent.trim())
check(pressed === 'Seismograph', `the seismograph is the default -- got "${pressed}"`)

// The SVG is sized from the measured box, not stretched from a viewBox:
// a width attribute that matches the element's own client width is what
// makes one unit one CSS pixel, which the record's sharpness clause
// depends on.
const sizing = await page.$eval(SEISMOGRAPH, (svg) => ({
  attr: Number(svg.getAttribute('width')),
  box: Math.round(svg.getBoundingClientRect().width),
  viewBox: svg.getAttribute('viewBox'),
}))
check(sizing.attr > 400, `the drum is sized in real pixels -- width attribute ${sizing.attr}`)
check(
  Math.abs(sizing.attr - sizing.box) <= 2,
  `one SVG unit is one CSS pixel -- width attribute ${sizing.attr} vs box ${sizing.box}`,
)
check(
  (sizing.viewBox ?? '').startsWith(`0 0 ${sizing.attr} `),
  `the viewBox matches the pixel size rather than stretching it -- got "${sizing.viewBox}"`,
)

// Two chart inks only: no per-series hue cycling survived the rewrite.
const inks = await page.$$eval('.drum svg path', (els) =>
  [...new Set(els.map((e) => getComputedStyle(e).fill))].sort(),
)
check(inks.length > 0 && inks.length <= 2, `two chart inks only -- got ${JSON.stringify(inks)}`)

// --- The drum: one outer+inner stroke pair per minute on the axis --------
//
// MetricsSeismograph's own MIN_HALF floor means every minute draws
// something, even a silent one -- so the stroke count is checked against
// the server's own axis length (GET /api/stats) rather than a guessed
// number: the suite's shared instance has arbitrary history by the time
// this scenario runs.
const statsForAxis = await (await page.request.get(apiUrl(page, '/api/stats'))).json()
const axisLen = statsForAxis.timeSeries?.length ?? 0
check(axisLen > 0, `the server reports a non-empty axis -- ${axisLen} minutes`)

const outerCount = await page.locator(`${SEISMOGRAPH} line.stroke.outer`).count()
const innerCount = await page.locator(`${SEISMOGRAPH} line.stroke.inner`).count()
check(
  outerCount === axisLen && innerCount === axisLen,
  `one outer+inner stroke pair per minute on the axis -- outer ${outerCount}, inner ${innerCount}, axis ${axisLen}`,
)

// Geometry, not visibility -- see this file's own header note on SVG
// <line> visibility. Each minute's refused (inner) half must never reach
// further from the midline than its own total (outer) half, and the two
// halves of one pair must share the same x.
const strokeGeometry = await page.$eval(SEISMOGRAPH, (svg) => {
  const read = (sel) =>
    [...svg.querySelectorAll(sel)].map((l) => ({
      x: Number(l.getAttribute('x1')),
      half: Math.abs(Number(l.getAttribute('y2')) - Number(l.getAttribute('y1'))) / 2,
    }))
  return { outers: read('line.stroke.outer'), inners: read('line.stroke.inner') }
})
const geometryHolds = strokeGeometry.outers.every((o, i) => {
  const inner = strokeGeometry.inners[i]
  return inner !== undefined && inner.x === o.x && inner.half <= o.half + 0.01
})
check(geometryHolds, "every minute's refused half never exceeds its own total half, at the same x")

// A direct pointer click on the paper itself selects a minute -- distinct
// from the cursor-survives-a-view-switch test below, which only ever
// selects through the table's own buttons.
const drumBox = await page.locator(SEISMOGRAPH).boundingBox()
await page
  .locator(SEISMOGRAPH)
  .click({ position: { x: Math.round(drumBox.width * 0.4), y: Math.round(drumBox.height * 0.3) } })
await page.locator(cursorBand(SEISMOGRAPH)).waitFor({ state: 'visible', timeout: 5000 })
const pointerMinute = (await page.locator(`${SEISMOGRAPH} text.cursor-label`).textContent()).trim()
check(/^\d\d:\d\d$/.test(pointerMinute), `clicking the drum's paper selects a minute -- got "${pointerMinute}"`)
const hourlineBig = (await page.locator('.metrics .hourline .big').textContent()).trim()
check(
  hourlineBig.startsWith(pointerMinute),
  `the hourline reflects the minute clicked on the drum -- got "${hourlineBig}" for "${pointerMinute}"`,
)

// --- The old surfaces are gone, not hidden -------------------------------
for (const gone of ['Event volume — last hour', 'Flags raised — last hour']) {
  const found = await page.locator(`text=${gone}`).count()
  check(found === 0, `the old overlay chart "${gone}" is gone`)
}

// --- The cursor, and its survival across a view switch -------------------
await page.click(VIEW_BUTTON('Table'))
await page.locator(TABLE).waitFor({ state: 'visible', timeout: 10000 })

// --- The table's top port/top talker agree with the API, never a guess ---
//
// #644 round 21: store.Store.HourTops is the one server-side answer for
// "who/what led this minute" -- the table's own columns have to print
// exactly that answer (or an honest em dash for an incomplete minute),
// not a number reconstructed from the client's own capped buffer. This
// checks agreement with a fresh fetch of the same endpoint rather than
// any hardcoded value, since the suite's shared instance has arbitrary
// history by the time this scenario runs.
const toposRes = await page.request.get(apiUrl(page, '/api/stats/tops'))
check(toposRes.ok(), `GET /api/stats/tops responds -- status ${toposRes.status()}`)
const toposBody = await toposRes.json()
const toposAxis = toposBody.tops ?? []
check(toposAxis.length > 0, `GET /api/stats/tops returns the axis -- ${toposAxis.length} minutes`)
const toposTimesMs = toposAxis.map((t) => new Date(t.time).getTime())
const oneMinuteApart = toposTimesMs.every((t, i) => i === 0 || t - toposTimesMs[i - 1] === 60_000)
check(oneMinuteApart, 'the tops axis is one-minute buckets, oldest first')

const tableTops = await tableAgreesWithTops(page, 15000)
check(
  tableTops.ok,
  tableTops.ok
    ? `the table's top port/talker cells agree with GET /api/stats/tops -- ${tableTops.rows} minutes checked`
    : `the table's top port/talker cells disagree with GET /api/stats/tops -- ${tableTops.detail}`,
)

const minuteButtons = page.locator('.table-view tbody th button.minute')
await minuteButtons.first().waitFor({ state: 'visible', timeout: 10000 })
const chosenMinute = (await minuteButtons.nth(1).textContent()).trim()
await minuteButtons.nth(1).click()

await page.locator('.table-view tbody tr.selected').waitFor({ state: 'visible', timeout: 5000 })
const selectedInTable = (await page.locator('.table-view tbody tr.selected th button.minute').textContent()).trim()
check(selectedInTable === chosenMinute, `the table highlights the minute clicked -- got "${selectedInTable}"`)

await page.click(VIEW_BUTTON('Seismograph'))
await page.locator(cursorBand(SEISMOGRAPH)).waitFor({ state: 'visible', timeout: 10000 })
const drumCursor = await cursorLine(page, SEISMOGRAPH)
check(
  drumCursor !== null && drumCursor.vertical && drumCursor.length > 100 && drumCursor.painted > 100,
  `the drum's cursor is a real vertical line down the paper -- got ${JSON.stringify(drumCursor)}`,
)
const drumMinute = (await page.locator('.drum svg text.cursor-label').textContent()).trim()
check(drumMinute === chosenMinute, `the drum's cursor is on the same minute -- got "${drumMinute}" for "${chosenMinute}"`)

await page.click(VIEW_BUTTON('Register'))
await page.locator(cursorBand(REGISTER)).waitFor({ state: 'visible', timeout: 10000 })
const registerCursor = await cursorLine(page, REGISTER)
check(
  registerCursor !== null && registerCursor.horizontal && registerCursor.length > 100 && registerCursor.painted > 100,
  `the register's cursor is a real horizontal line across the rows -- got ${JSON.stringify(registerCursor)}`,
)
// Amber is time. A cursor drawn in a series ink would be the record's
// one colour rule broken on the most prominent mark on the page.
check(registerCursor?.timeColour === true, `the cursor wears the time colour -- got ${registerCursor?.stroke}`)
const crossSection = (await page.locator('.cross-section h3').textContent()).trim()
check(
  crossSection === `The minute ${chosenMinute}`,
  `the register reads the same minute across the page -- got "${crossSection}"`,
)

// The cursor reads the whole minute, not one series: every traffic
// series plus the episode count is in the cross-section.
const crossRows = await page.$$eval('.cross-section .xs-row dt', (els) => els.map((e) => e.textContent.trim()))
check(
  crossRows.length === 8 && crossRows[crossRows.length - 1] === 'flag episodes',
  `the cursor reads every series at once -- got ${JSON.stringify(crossRows)}`,
)

// --- The preference is persisted, and applied before first paint ---------
const stored = await page.evaluate(() => localStorage.getItem('mikroview-metrics-view'))
check(stored === 'register', `the chosen view is persisted -- got ${JSON.stringify(stored)}`)

await page.reload({ waitUntil: 'networkidle' })
await goTo(page, 'Metrics')
// No click on a view button between the reload and this wait: if the
// preference were applied after first paint, the seismograph would be
// on screen here instead.
await page.locator(REGISTER).waitFor({ state: 'visible', timeout: 10000 })
const pressedAfterReload = await page.$eval('.views button[aria-pressed="true"]', (e) => e.textContent.trim())
check(pressedAfterReload === 'Register', `the stored view survives a reload -- got "${pressedAfterReload}"`)
check(
  (await page.locator(SEISMOGRAPH).count()) === 0,
  'the default view is not mounted first and then replaced',
)

// The cursor is deliberately *not* persisted: a minute selected an hour
// ago has aged off the axis, so a reload starts with none.
check(
  (await page.locator(`${REGISTER} line.cursor`).count()) === 0 &&
    (await page.locator(cursorBand(REGISTER)).count()) === 0,
  'the cursor starts clear after a reload rather than pointing at a stale minute',
)

// --- The keyboard contract ----------------------------------------------
const surface = page.locator('.surface[role="slider"]')
await surface.waitFor({ state: 'visible', timeout: 5000 })
await surface.focus()
await page.keyboard.press('End')
await page.locator(cursorBand(REGISTER)).waitFor({ state: 'visible', timeout: 5000 })
const atBrink = await surface.getAttribute('aria-valuetext')
check(/\d\d:\d\d — Accept /.test(atBrink ?? ''), `End reads the brink minute and its figures -- got "${atBrink}"`)

await page.keyboard.press('ArrowLeft')
const oneBack = await surface.getAttribute('aria-valuetext')
check(oneBack !== atBrink, 'an arrow moves the cursor one minute')

await page.keyboard.press('Escape')
check(
  (await page.locator(`${REGISTER} line.cursor`).count()) === 0 &&
    (await page.locator(cursorBand(REGISTER)).count()) === 0,
  'Escape clears the cursor',
)

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
