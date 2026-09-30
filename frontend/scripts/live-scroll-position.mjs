// SPDX-License-Identifier: AGPL-3.0-only
//
// Three scroll defects, pinned against a real browser at a real
// viewport: #383 and #384 from the v0.2.0 preview pass, #689 from the
// deck rebuild.
//
// None was visible from the code. #384 is actively misleading there:
// the Entities jump reads like a re-render problem, and the CSS and the
// keyed {#each} blocks both look correct -- it is a focus() call on a
// row that should never have existed. #689 is the same shape one layer
// up: Metrics.svelte's own sr-only live region is `position: absolute`
// with no offset of its own, and nothing between it and <html> used to
// be positioned, so the browser fell back to its CSS "static position"
// -- computed from the *unclipped* flow height of everything before it,
// ignoring every overflow:hidden/auto ancestor on the way. With no
// positioned ancestor that became real document coordinates: the
// operator could scroll the whole page away, leaving nothing on screen
// but the deck's fixed roll rail. Deck.svelte's `.card` is now
// `position: relative`, so this checks every rail destination rather
// than Metrics alone -- the gap it closed was shared chrome, not one
// page's mistake, and only measuring Metrics would leave the other six
// scenes' own version of the same defect uncaught.
//
// So every assertion here measures the thing the operator actually
// experiences -- can I reach the bottom of the page, am I still where I
// was, does the document itself ever scroll past its own content -- and
// not the mechanism, which is free to change.

import { session, check, done, goTo, feedSyslog, waitForStreamRows } from './live-browser.mjs'

// mocksApi: true, for the #383 section below, which needs to land on
// the wizard's first step regardless of this shared instance's own
// history of syslog traffic -- see that section's own comment.
const { page, consoleErrors } = await session({ mocksApi: true })
// Its own traffic: the instance is reset before every scenario (#1064),
// so nothing a sibling fed is there to count.
feedSyslog(60, 'live-scroll-position')
await waitForStreamRows(page, 60)

// The viewport both defects were reported at. Fixed rather than
// inherited: a scroll assertion that runs at whatever size the harness
// defaults to is a scroll assertion that can silently stop overflowing.
await page.setViewportSize({ width: 1280, height: 720 })

// --- #384: naming an entity leaves the operator where they were ---------
// The workflow the defect punished is the one the view exists for:
// working down a long discovered list naming things one after another.
//
// Entities was rebuilt into one sorted `hosts` table (#675/#681): a
// discovered-but-unnamed host is no longer its own `.row.discovered` in
// a separate "Discovered" section, it is a row of `.etable` whose name
// cell reads "— click to name —" (Entities.svelte:713), and the rename
// itself is an inline `<input class="rename-input">` in that same cell
// rather than a `.row .inline-input`. `.page` is still the one
// scrollable ancestor (Entities.svelte:576), so the scroll assertion
// itself is unchanged -- only how a row is found and named moves.
await goTo(page, 'Entities')
await page.waitForSelector('.etable tbody tr')

const entities = await page.$eval('.page', (el) => ({
  scrollHeight: el.scrollHeight,
  clientHeight: el.clientHeight,
}))
check(
  entities.scrollHeight > entities.clientHeight * 3,
  `the discovered list is long enough for losing your place to matter (${entities.scrollHeight}px in ${entities.clientHeight}px)`,
)

// Partway down, not at the top -- at the top the defect is invisible.
await page.$eval('.page', (el) => el.scrollTo(0, Math.floor(el.scrollHeight * 0.6)))
// .page has no scroll-behavior: smooth and no scroll listener of its own,
// so scrollTop is already set synchronously -- this only waits for the
// browser to have painted it, deterministically, rather than guessing.
await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))))
const before = await page.$eval('.page', (el) => el.scrollTop)
check(before > entities.clientHeight * 2, `the view is scrolled well down before the add (scrollTop ${before})`)

// Pick a row that is actually on screen at this position, so the click
// itself cannot be what moves the viewport. The address is column 3
// (name · lane · address · mac · first seen · last seen · marks) and is
// stable across the rename, unlike the name cell this is about to change.
const target = await page.evaluate(() => {
  const pageEl = document.querySelector('.page')
  const pr = pageEl.getBoundingClientRect()
  for (const row of document.querySelectorAll('.etable tbody tr')) {
    const btn = row.querySelector('.rename-btn')
    if (!btn || !/click to name/.test(btn.textContent ?? '')) continue
    const r = row.getBoundingClientRect()
    if (r.top > pr.top + 60 && r.bottom < pr.bottom - 60) return row.querySelector('td:nth-child(3)')?.textContent ?? null
  }
  return null
})
check(target !== null, 'a discovered row is on screen at this scroll position to name')

// The row, found by its address cell. Not `td:nth-child(3):text-is(...)`
// any more: `:text-is()` compares an element's *immediate* text nodes,
// so it stops matching a cell as soon as the address is wrapped in
// anything -- which is what #410 did, putting the dossier door on the
// address. The cell's own text is the stable fact, so match on that,
// anchored so one address is never a prefix of another.
const escaped = target.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
const targetRow = page
  .locator('.etable tbody tr')
  .filter({ has: page.locator('td:nth-child(3)', { hasText: new RegExp(`^${escaped}$`) }) })
await targetRow.locator('.rename-btn').click()
await targetRow.locator('.rename-input').fill('live-scroll-check')

// Saved with Enter, not by clicking Save: Playwright scrolls a click
// target into view first, which would hide exactly the defect under
// test if the button ever sat off screen.
await targetRow.locator('.rename-input').focus()
await page.keyboard.press('Enter')
// Native querySelector inside the browser has no :has() text filter --
// that is a Playwright-only extension -- so the row is found here by
// plain DOM matching on the same stable address column instead.
await page.waitForFunction(
  (key) => {
    const rows = document.querySelectorAll('.etable tbody tr')
    for (const row of rows) {
      if (row.querySelector('td:nth-child(3)')?.textContent !== key) continue
      return !row.querySelector('.rename-input') && /live-scroll-check/.test(row.textContent ?? '')
    }
    return false
  },
  target,
  { timeout: 15000 },
)

const after = await page.$eval('.page', (el) => el.scrollTop)
// The table re-sorts by the new label (Entities.svelte:305-318), which
// can move the row far from where it was -- but scrollTop is a raw
// pixel offset into the same scroll container, unaffected by content
// reordering below or above the fold, so it must not have moved at all
// once the DOM settles from a same-height row swap. Kept as a tolerance
// rather than an exact-zero check for that settling, and because the
// old defect moved it by seventeen thousand pixels, not a handful.
const drift = Math.abs(after - before)
check(
  drift <= 120,
  `naming an entity leaves the operator where they were (scrollTop ${before} -> ${after}, drift ${drift}px)`,
)

// --- #689: the document itself never scrolls past its own content --------
// Reproduced by loading every deck scene at least once with Metrics
// mounted somewhere in the near() window (it is a neighbour of
// Topography, Metrics and Stream, and near() mounts the centred card
// plus one on each side) and measuring the *document's* own scrollable
// height against the viewport it is standing in for -- not any one
// scene's internal scroll container, which was never the defect. Before
// the fix this was double the viewport on Topography, Metrics and
// Stream alike (Metrics mounted on all three) and exactly the viewport
// everywhere Metrics was not near -- proof the escape was Metrics' own
// sr-only region, not the deck's per-scene clipping.
feedSyslog(60, 'live-scroll-position-689')
await page.setViewportSize({ width: 1280, height: 720 })
for (const label of [
  'The fall',
  'Topography',
  'Metrics',
  'Stream',
  'The docket',
  'Entities',
  'Settings',
  'Log every rule',
]) {
  await goTo(page, label)
  const doc = await page.evaluate(() => ({
    scrollHeight: document.scrollingElement.scrollHeight,
    innerHeight: window.innerHeight,
  }))
  // A couple of px of tolerance for subpixel layout, and no more --
  // the defect was not a few pixels, it was the viewport doubling.
  check(
    doc.scrollHeight <= doc.innerHeight + 2,
    `${label}: the document never scrolls past the viewport (scrollHeight ${doc.scrollHeight} vs ${doc.innerHeight})`,
  )
}

// --- #383: every wizard step is reachable -------------------------------
// #app is height: 100vh; overflow: hidden, so anything that declares no
// scroll container of its own has its overflow clipped and unreachable.
// The wizard page was the only view missing the flex/min-height/
// overflow-y trio, which made the guided setup -- the first-run
// experience specifically -- impossible to read past the fold.
//
// #487 moved that page into a modal capped at 92vh; #1381 replaced the
// modal with a full screen (`.wiz.page`, wizard.css) that was built with
// the same trio from the start -- `.wiz .main { display: grid;
// grid-template-rows: 1fr auto }`, `.wiz .body { min-height: 0;
// overflow-y: auto }` -- so this still measures it, on whichever step is
// reachable without walking the whole run.
//
// The step with genuinely long content -- Paste once, the terminal
// block, which is where the old modal's push-script step carried this
// check -- is still a stub under #1382 (StepPaste.svelte renders a
// couple of lines today, nothing to overflow). So this measures The
// router (StepRouter.svelte) instead, at a viewport short enough to
// force *any* step's content past the fold rather than one sized to a
// specific step's height -- an assertion that cannot fail is worse than
// none. Retarget this to Paste once, at a viewport sized to its real
// content, once that step carries its block (#1382).
//
// Run last in this scenario, not restored to 1280x720 afterwards: the
// full-screen wizard has no ✕ or Escape to leave it by any more
// (DESIGN.md, "Superseded: the wizard as a modal") and nothing here
// needs `page` again once it opens.
//
// A bare "Run setup…" reads the whole fleet's evidence when no router is
// named yet (wizardRun.svelte.ts's evidence getter, the `!id` branch),
// and this instance has been shared with everything that ran before it
// in this shard -- so by now some source has almost always sent syslog,
// which would open past The router (on 'watch' or later) rather than on
// it. Stripped from /api/setup/status's response here for the same
// reason the wizard's own router-mint scenario does it: not a fake shape,
// just the one field ("sources not yet heard from") this instance
// cannot otherwise be made to show on demand.
await page.route('**/api/setup/status', async (route) => {
  const res = await route.fetch()
  const body = await res.json()
  body.sources = (body.sources ?? []).map(({ syslogFirstSeenAt, caFetchedAt, ...s }) => s)
  await route.fulfill({ response: res, body: JSON.stringify(body) })
})

await page.setViewportSize({ width: 1280, height: 260 })

await goTo(page, 'Run setup…')
const wizard = page.locator('.page.wiz')
await wizard.waitFor({ state: 'visible' })
await page.locator('.wiz .body h3').waitFor({ state: 'visible' })

const wizardBox = await page.$eval('.page.wiz', (el) => {
  const r = el.getBoundingClientRect()
  return { top: r.top, bottom: r.bottom, viewportHeight: window.innerHeight }
})
check(
  wizardBox.top >= -1 && wizardBox.bottom <= wizardBox.viewportHeight + 1,
  `the wizard fits the viewport rather than running off it (${Math.round(wizardBox.top)}px..${Math.round(wizardBox.bottom)}px in ${wizardBox.viewportHeight}px)`,
)

const body = await page.$eval('.wiz .body', (el) => ({
  scrollHeight: el.scrollHeight,
  clientHeight: el.clientHeight,
  overflowY: getComputedStyle(el).overflowY,
}))
check(
  body.scrollHeight > body.clientHeight,
  `the step body overflows this viewport, so scrolling it is a real question (content ${body.scrollHeight}px in ${body.clientHeight}px)`,
)
check(
  body.overflowY === 'auto' || body.overflowY === 'scroll',
  `the step body declares its own scroll container (overflow-y: ${body.overflowY})`,
)

await page.$eval('.wiz .body', (el) => el.scrollTo(0, el.scrollHeight))
const reached = await page.$eval('.wiz .body', (el) => ({
  scrollTop: el.scrollTop,
  atBottom: el.scrollTop >= el.scrollHeight - el.clientHeight - 2,
}))
// Both halves, because either alone passes while the defect is present:
// with overflow clipped, scrollHeight === clientHeight, so "at the
// bottom" is vacuously true at scrollTop 0.
check(
  reached.atBottom && reached.scrollTop > 0,
  `the step body scrolls, and reaches its bottom (scrollTop ${reached.scrollTop})`,
)

// The assertion #383 actually asked for: not "a scrollbar exists" but
// "the last element of the step is reachable".
//
// Measured against the browser viewport, never against the body's own
// box. When the overflow is clipped, that box is its full unclipped
// height, so a rect comparison against it says the last element is
// "inside" while the operator cannot see or reach it -- the assertion
// would pass on exactly the build it exists to catch. window.innerHeight
// is what the operator actually has.
const lastVisible = await page.evaluate(() => {
  const children = document.querySelectorAll('.wiz .body > *')
  const last = children[children.length - 1]
  if (!last) return null
  const r = last.getBoundingClientRect()
  return { top: r.top, bottom: r.bottom, viewportHeight: window.innerHeight }
})
check(lastVisible !== null, 'the wizard renders a step body')
check(
  lastVisible.bottom <= lastVisible.viewportHeight + 2 && lastVisible.bottom > 0,
  `the bottom of the step is on screen once scrolled to it -- not clipped past the fold (bottom ${Math.round(lastVisible.bottom)}px, viewport ${lastVisible.viewportHeight}px)`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
