// SPDX-License-Identifier: AGPL-3.0-only
//
// #796: the memory bar is a slider. Round 39's `#set` memory group,
// driven for real -- the track dragged, the figure applied, the ring
// resized underneath, and the surfaces that read it agreeing afterwards.
//
// Three of the issue's Done-when lines are exercised here. The fourth is
// not, and cannot be from this scenario:
//
//   "A viewer sees the bar and figure, cannot move it, and is not told
//    with a lock icon."
//
// A viewer cannot reach Settings at all. #657's ratified surface matrix
// puts the whole card out of a viewer's navigation, which
// live-viewer-surfaces.mjs pins directly ("Settings is absent from a
// viewer's navigation"), and round 39's own README says the same:
// "settings is an admin surface in this drawing". Putting the memory
// group in front of a viewer means reopening a gate #657 deliberately
// closed, which is a design and security call, not a build decision --
// escalated on #796 rather than guessed past.
//
// What IS reachable is the same read-only rendering one tier up: a
// `user` gets Settings and is not an admin, so the bar, the figure and
// the absence of any lock icon are all proved below against a real user
// account. The moment a viewer is ever given Settings, that account sees
// exactly what this scenario checks a user seeing.

import { session, check, done, goTo, feedSyslog, launchBrowser, responsive } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const MIB = 1024 * 1024
const BYTES_PER_EVENT = 624

const MEMG = '#memg'
const SLIDER = `${MEMG} svg[role="slider"]`

// Something in the ring before anything is asserted about it: every
// claim below is about what the buffer holds, and an empty one has no
// reach, no rate and nothing to evict.
feedSyslog(200, 'live-memory-slider')
const { page, consoleErrors } = await session({ waitForEvents: 100 })

/** stats reads the server's own answer, which is what the row claims to restate. */
async function stats(p) {
  return p.evaluate(async () => {
    const res = await fetch('/api/stats')
    return res.json()
  })
}

/** The memory group's "event buffer" row, as rendered. */
async function bufferRowText(p) {
  return p.$eval(`${MEMG} .orow .ov`, (el) => el.textContent.trim().replace(/\s+/g, ' '))
}

const figureNow = (p) => p.getAttribute(SLIDER, 'aria-valuenow').then(Number)

/**
 * Moves the handle to an exact figure.
 *
 * Driven by keyboard rather than by a synthesised pointer path: the
 * control takes both (see MemoryControl.svelte) and the keys land on
 * exact figures, so what is asserted afterwards is the resize rather
 * than how accurately a mouse was moved. The pointer path is exercised
 * separately, on the reader's copy below.
 *
 * Home first, so the walk starts from a known place rather than from
 * wherever the last step left it, then Page Up (a doubling) while the
 * next one still fits, then arrows (one 8 MiB snap step). Bounded, so a
 * control that stops moving fails here rather than spinning.
 */
async function moveTo(p, targetBytes) {
  await p.focus(SLIDER)
  await p.keyboard.press('Home')
  for (let i = 0; i < 12; i++) {
    if ((await figureNow(p)) * 2 > targetBytes) break
    await p.keyboard.press('PageUp')
  }
  for (let i = 0; i < 300; i++) {
    const now = await figureNow(p)
    if (now >= targetBytes) break
    await p.keyboard.press('ArrowRight')
  }
  return figureNow(p)
}

async function moveToAndApply(p, targetBytes) {
  const reached = await moveTo(p, targetBytes)
  await p.click(`${MEMG} .memnote button:has-text("apply")`)
  await p.waitForSelector(`${MEMG} .memnote`, { state: 'detached', timeout: 30000 })
  return reached
}

await goTo(page, 'Settings')
await page.waitForSelector(MEMG)

const before = await stats(page)
check(!!before.memory, 'GET /api/stats carries the memory object the control reads')
check(
  before.memory.min === 32 * MIB,
  `the slider starts at 32 MiB (got ${before.memory.min} bytes)`,
)
check(
  before.memory.max > before.memory.min,
  `the host's ceiling (${before.memory.max}) is above the floor (${before.memory.min})`,
)
check(
  before.memory.resident > 0,
  `the trade-off's other half is reported: resident memory (${before.memory.resident})`,
)

// --- 1: dragging only proposes -----------------------------------------
// The handle moves, the sentence appears, and the server is untouched
// until apply is pressed. A shrink discards events, so this is the whole
// reason the control has an apply at all.

const startFigure = before.memory.maxMemory
await page.focus(SLIDER)
await page.keyboard.press('ArrowRight')
const proposedFigure = await figureNow(page)
check(proposedFigure > startFigure, `the handle moved on a key press (${startFigure} -> ${proposedFigure})`)
check(
  (await page.locator(`${MEMG} .memnote`).count()) === 1,
  'a consequence sentence appears while a proposal is open',
)
check(
  (await page.locator(`${MEMG} .memnote button:has-text("keep")`).count()) === 1,
  'the sentence offers "keep" beside "apply", as round 39 draws it',
)
check(
  (await page.locator(`${MEMG} circle.mghost`).count()) === 1,
  "the handle's old place stays as a dotted ghost while a proposal is open",
)
const midDrag = await stats(page)
check(
  midDrag.memory.maxMemory === startFigure,
  `dragging changed nothing on the server (still ${midDrag.memory.maxMemory})`,
)

// keep puts it back, and still without asking the server.
await page.click(`${MEMG} .memnote button:has-text("keep")`)
await page.waitForSelector(`${MEMG} .memnote`, { state: 'detached', timeout: 5000 })
check((await figureNow(page)) === startFigure, 'keep puts the handle back where it was')

// --- 2: drag to 480 MiB, and the surfaces agree on the new reach --------

const grown = await moveToAndApply(page, 480 * MIB)
check(grown === 480 * MIB, `the handle reached 480 MiB exactly (got ${grown} bytes)`)

const after = await stats(page)
check(
  after.memory.maxMemory === 480 * MIB,
  `the server is running on 480 MiB (got ${after.memory.maxMemory} bytes)`,
)
const wantCapacity = Math.floor((480 * MIB) / BYTES_PER_EVENT)
check(
  after.capacity === wantCapacity,
  `the ring was resized to ${wantCapacity} events (got ${after.capacity})`,
)

const row = await bufferRowText(page)
check(row.startsWith('480 MiB · '), `the row leads with the new figure -- got "${row}"`)
check(
  /~\d{1,3}( \d{3})* events/.test(row),
  `the row states what that buys in events -- got "${row}"`,
)

// The stored figure is what a restart would read back, not just what
// this process is running on.
check(after.memory.stored === true, 'the figure was stored, not only applied to the running ring')

// --- 3: drag down, and the oldest events fall away ----------------------
//
// The floor is 32 MiB, which is about 53,760 events, so a shrink only
// evicts something if the ring is holding more than that. The buffer is
// filled here deliberately rather than the assertion being made
// conditional: a test that only checks eviction when eviction happens to
// occur passes identically against code that never evicts at all.
//
// Bounded on purpose. live-buffer-depth.mjs refuses to fill the shared
// instance's default 200,000-event ring for exactly the right reason,
// and this stays well inside that: 56,000 events, and the figure is put
// back at the end so later scenarios meet a normal-looking instance.
const FLOOR = 32 * MIB
const floorCapacity = Math.floor(FLOOR / BYTES_PER_EVENT)

const fillStart = Date.now()
let held = (await stats(page)).count
while (held <= floorCapacity + 2000) {
  feedSyslog(8000, 'live-memory-slider')
  held = (await stats(page)).count
  if (Date.now() - fillStart > 240000) break
}
check(
  held > floorCapacity,
  `the ring holds more than the 32 MiB floor would keep (${held} events against ${floorCapacity}) ` +
    `-- filled in ${Math.round((Date.now() - fillStart) / 1000)}s`,
)

// The page polls /api/stats every 5s (App.svelte's STATS_REFRESH_MS);
// wait one round rather than reloading, which would put the first-run
// setup modal back over the shell.
await page.waitForTimeout(7000)

// While the shrink is proposed, the bar itself says what would go.
await page.focus(SLIDER)
await page.keyboard.press('Home')
check((await figureNow(page)) === FLOOR, 'Home takes the handle to the 32 MiB floor')
check(
  (await page.locator(`${MEMG}.mshrink`).count()) === 1,
  "the group is in round 39's mshrink state while a shrink is proposed",
)
check(
  (await page.locator(`${MEMG} g.mcut`).count()) === 1,
  'the hours that would let go are marked on the bar itself',
)
const cutLabel = (await page.locator(`${MEMG} g.mcut text`).textContent()) ?? ''
check(
  /\d\d:\d\d — the oldest that 32 MiB would keep/.test(cutLabel.replace(/\s+/g, ' ')),
  `the new oldest time is named on the bar -- got "${cutLabel.replace(/\s+/g, ' ').trim()}"`,
)
const note = (await page.locator(`${MEMG} .memnote`).textContent())?.replace(/\s+/g, ' ').trim() ?? ''
check(
  /everything before \d\d:\d\d lets go/.test(note),
  `the sentence says what a shrink costs -- got "${note}"`,
)

const oldestBefore = Date.parse((await stats(page)).oldestHeld)
await page.click(`${MEMG} .memnote button:has-text("apply")`)
await page.waitForSelector(`${MEMG} .memnote`, { state: 'detached', timeout: 30000 })

const shrunk = await stats(page)
check(
  shrunk.capacity === floorCapacity,
  `the ring shrank to ${floorCapacity} events (got ${shrunk.capacity})`,
)
check(
  shrunk.count === floorCapacity,
  `the ring is full at its new size, so events were evicted (holding ${shrunk.count})`,
)
check(
  shrunk.count < held,
  `${held - shrunk.count} events fell away (${held} held before, ${shrunk.count} after)`,
)
const oldestAfter = Date.parse(shrunk.oldestHeld)
check(
  oldestAfter > oldestBefore,
  `the oldest events went, not the newest: the buffer's reach moved forward from ` +
    `${new Date(oldestBefore).toISOString()} to ${new Date(oldestAfter).toISOString()}`,
)

// The stream's own window chip is the second surface reading the same
// reach, and it has to agree with the memory group's, or the operator
// has two answers to one question. Its words come from lib/spans.ts's
// describeReach, restated here rather than imported: a scenario that
// imported the same function would agree with the page by construction
// and could not catch the two disagreeing.
function chipSeconds(text) {
  const m = text.match(/holding (\d+(?:\.\d+)?) (s|min|h|d)/)
  if (!m) return null
  const n = Number(m[1])
  return n * { s: 1, min: 60, h: 3600, d: 86400 }[m[2]]
}

await goTo(page, 'Stream')
const chipText = ((await page.locator('.filterline .reach').first().textContent()) ?? '').trim()
const live = await stats(page)
// Compared as a duration rather than as a string: the chip rounds to
// whichever unit suits it and reads a clock that ticks independently of
// this script's, so equal text is the wrong test. #796's own line is
// "agree within a minute", which is the tolerance used here.
const chipReach = chipSeconds(chipText)
const serverReach = Math.max(0, (Date.now() - Date.parse(live.oldestHeld)) / 1000)
check(
  chipReach !== null && Math.abs(chipReach - serverReach) <= 60,
  `the stream's window chip agrees with the shrunk buffer's reach within a minute: ` +
    `"${chipText}" against ${Math.round(serverReach)} s from the server`,
)
await goTo(page, 'Settings')
await page.waitForSelector(MEMG)

// Both surfaces still agree afterwards, which is the point of the resize
// being one operation rather than a stored figure and a ring that drift.
const shrunkRow = await bufferRowText(page)
check(shrunkRow.startsWith('32 MiB · '), `the row followed the shrink -- got "${shrunkRow}"`)

check(await responsive(page), 'the main thread is still answering after two live resizes')

// --- 4: the read-only rendering, one tier up from a viewer --------------
//
// See this file's header for why a `user` and not a `viewer`.

const USER_NAME = 'live-memory-796'
const USER_PASS = 'live-memory-796-password'

await page.click('#people .ogfoot .olink')
await page.waitForSelector('#people .pform')
await page.fill('#people .pform input[aria-label="username"]', USER_NAME)
await page.fill('#people .pform input[aria-label="password"]', USER_PASS)
await page.click('#people .pform button:has-text("let them in")')
await page.waitForSelector(`#people .prow:has-text("${USER_NAME}")`)

const browser2 = await launchBrowser()
const ctx = await browser2.newContext({ ignoreHTTPSErrors: true })
const reader = await ctx.newPage()
await reader.goto(URL_BASE, { waitUntil: 'networkidle' })
await reader.fill('input[autocomplete="username"]', USER_NAME)
await reader.fill('input[autocomplete="current-password"]', USER_PASS)
await reader.click('button[type="submit"]')
await reader.waitForSelector('#main-content', { timeout: 15000 })
await goTo(reader, 'Settings')
await reader.waitForSelector(MEMG)

check(
  (await reader.locator(`${MEMG} svg.stmem`).count()) === 1,
  'a non-admin sees the hours bar',
)
check(
  (await reader.locator(`${MEMG} svg.stmemctl`).count()) === 1,
  'a non-admin sees the size track and the figure on it',
)
const readerFigure = (await reader.locator(`${MEMG} svg.stmemctl text.sp-k`).first().textContent())?.trim()
check(readerFigure === '32 MiB', `a non-admin sees the figure in effect -- got "${readerFigure}"`)

check(
  (await reader.locator(SLIDER).count()) === 0,
  'a non-admin is offered no slider to move: there is nothing with role="slider"',
)
await reader.locator(`${MEMG} svg.stmemctl`).click({ position: { x: 20, y: 12 } })
await reader.keyboard.press('ArrowRight')
check(
  (await reader.locator(`${MEMG} .memnote`).count()) === 0,
  'neither a click nor an arrow key opens a proposal for a non-admin',
)

// "and is not told with a lock icon" -- #796, verbatim. Nothing appears
// to explain the absence: no padlock, no "admin only", nothing greyed.
const groupText = ((await reader.locator(MEMG).textContent()) ?? '').toLowerCase()
for (const forbidden of ['🔒', 'lock', 'admin only', 'read-only', 'permission']) {
  check(!groupText.includes(forbidden), `the memory group says nothing about "${forbidden}" to a non-admin`)
}
check(
  (await reader.locator(`${MEMG} [disabled], ${MEMG} [aria-disabled="true"]`).count()) === 0,
  'nothing in the memory group renders disabled for a non-admin',
)

// The gate is real underneath the missing control, not only missing from
// the page: the endpoint itself refuses.
const refusal = await reader.evaluate(async () => {
  const res = await fetch('/api/settings/store', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    body: JSON.stringify({ maxMemory: 480 * 1024 * 1024 }),
  })
  return res.status
})
check(refusal === 403, `PUT /api/settings/store refuses a non-admin outright (got ${refusal})`)
check(
  (await stats(page)).memory.maxMemory === FLOOR,
  "the refused call changed nothing: the buffer is still where the admin left it",
)

await browser2.close()

// --- put it back --------------------------------------------------------
// Scenarios share one instance and run in filename order, so this leaves
// the buffer at the size it found it rather than at the floor.

await page.waitForSelector(SLIDER)
const restored = await moveToAndApply(page, startFigure)
check(
  restored === startFigure && (await stats(page)).memory.maxMemory === startFigure,
  `the buffer is put back where this scenario found it (${startFigure} bytes)`,
)

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)

done()
