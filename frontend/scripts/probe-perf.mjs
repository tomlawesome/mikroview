// SPDX-License-Identifier: AGPL-3.0-only
//
// Measurement-only probe for #690 ("the UI is very laggy"). Not a
// scenario -- it prints numbers rather than asserting pass/fail, so it
// is named probe-perf.mjs rather than live-*.mjs: the scenario glob in
// scripts/ picks up live-*.mjs, and this is not one of those.
//
// Drives a real signed-in session against a running instance (MV_URL/
// MV_USER/MV_PASS, same env contract as live-browser.mjs) and records,
// with Chrome's own instrumentation:
//
//   - wall-clock time for each deck-to-deck roll (the fall, topography,
//     metrics, stream, the docket, entities, settings)
//   - CDP Performance.getMetrics deltas (script/layout/recalc-style/
//     task duration) across all rolls in a round
//   - a sampling CPU profile's top self-time functions across all rolls
//   - requestAnimationFrame frame durations while scrolling the docket
//     (thousands of flag cards, no known virtualisation) and while
//     idling on the live stream (websocket arrivals)
//   - Long Tasks (PerformanceObserver 'longtask') during each phase
//
// Read-only against the target: no data is written, nothing is
// restarted. Usage:
//
//   source /tmp/mikroview-atlas-demo/credentials.txt
//   node scripts/probe-perf.mjs
//
// --json <path> additionally writes a machine-readable summary (schema
// 2: commit, per-deck wallAvgMs, per-phase long-task totalMs, and the
// docket-scroll mean frame duration) for perf-compare.mjs to diff against a
// baseline. It is derived from the same numbers this prints -- nothing
// is re-measured, only reshaped once the run is over. Console output is
// unchanged whether or not --json is given.

import fs from 'node:fs'
import { chromium } from 'playwright'
// Dynamic, not a static import: live-browser.mjs has its own
// unconditional MV_URL check that process.exit(2)s at module load
// (scripts/live-browser.mjs:25-27). A static import here ran that
// check the moment anything imported probe-perf.mjs -- including
// perf-compare.test.mjs importing deckAvgMs, killing the whole test
// process before a single test ran. Deferred into main() below, where
// it is actually needed, so importing this file for its exports never
// touches live-browser.mjs at all.

// Mirrors live-browser.mjs's private SCENES table (rail label -> deck
// card, plus the docket's tab). Reimplemented locally, not imported,
// because this probe needs a tunable timeout per roll: the whole point
// of #690 is that a roll can be slow, and live-browser.mjs's goTo()
// hardcodes Playwright's own actionability timeouts, which abort the
// entire run rather than reporting how slow.
const CARD_BY_LABEL = {
  'The fall': { rail: 'The fall', card: 'fall' },
  Topography: { rail: 'Topography', card: 'topography' },
  Metrics: { rail: 'Metrics', card: 'metrics' },
  Stream: { rail: 'Stream', card: 'live' },
  Flags: { rail: 'The docket', card: 'docket', tab: 'flags' },
  Entities: { rail: 'Entities', card: 'entities' },
  Settings: { rail: 'Settings', card: 'engineroom' },
}

/**
 * rollTo clicks a rail destination and waits for its card to centre,
 * exactly like live-browser.mjs's goTo -- but with a generous,
 * per-call timeout and no throw on the click step itself timing out,
 * so a genuinely slow roll is measured rather than aborting the probe.
 * Returns the elapsed wall time in ms, or throws only if the card never
 * centres within timeoutMs (the caller decides what to do with that).
 */
async function rollTo(page, label, timeoutMs = 60000) {
  const scene = CARD_BY_LABEL[label]
  const t0 = Date.now()
  await page.click(`.roll-rail button.rail-name:text-is("${scene.rail}")`, { timeout: timeoutMs })
  await page.waitForFunction(
    (c) => {
      const deck = document.querySelector('.deck')
      const el = deck?.querySelector(`.card[data-card="${c}"]`)
      if (!el) return false
      return Math.abs(el.getBoundingClientRect().top - deck.getBoundingClientRect().top) < 2
    },
    scene.card,
    { timeout: timeoutMs },
  )
  if (scene.tab) {
    await page.click(`.card[data-card="${scene.card}"] [role="tab"]:text-is("${scene.tab}")`, {
      timeout: timeoutMs,
    })
  }
  return Date.now() - t0
}

// Read at import time only when this file is the entry point, not when
// perf-compare.test.mjs imports it for deckAvgMs -- a test run has none
// of MV_URL/MV_USER/MV_PASS set, and the old unconditional checks below
// used to process.exit(2) on module load before a single test ran.
let URL_BASE, USER, PASS, JSON_PATH

const ROLL_LABELS = ['The fall', 'Topography', 'Metrics', 'Stream', 'Flags', 'Entities', 'Settings']
const ROUNDS = 3
const SCROLL_MS = 3000
const IDLE_MS = 5000
// The ceiling a timed-out rollTo() is deemed to have taken. Used both
// as the per-call Playwright timeout and, per #1084, as the value a
// timed-out sample contributes to deckAvgMs -- a roll that never
// settles is the slowest possible outcome, not a missing measurement.
const ROLL_TIMEOUT_MS = 45000

// Installed before any page script runs (addInitScript), so it survives
// this SPA's one real navigation and stays available for every phase.
const INSTRUMENT_INIT = () => {
  window.__mvLongTasks = []
  try {
    new PerformanceObserver((list) => {
      for (const e of list.getEntries()) {
        window.__mvLongTasks.push({ start: e.startTime, duration: e.duration })
      }
    }).observe({ entryTypes: ['longtask'] })
  } catch {
    /* longtask unsupported -- reported as zero below rather than failing the probe */
  }
  window.__mvDrainLongTasks = () => window.__mvLongTasks.splice(0)
  window.__mvFrameSamples = []
  window.__mvStartFrameSampler = (durationMs) => {
    window.__mvFrameSamples = []
    let last = performance.now()
    const start = last
    function loop(t) {
      window.__mvFrameSamples.push(t - last)
      last = t
      if (t - start < durationMs) requestAnimationFrame(loop)
    }
    requestAnimationFrame(loop)
  }
}

function fmt(n, digits = 1) {
  return Number.isFinite(n) ? n.toFixed(digits) : 'n/a'
}

function percentile(sorted, p) {
  if (sorted.length === 0) return NaN
  const idx = Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length))
  return sorted[idx]
}

// Returns the computed stats (used to feed --json) as well as printing
// the same line as before; callers that only want the console output
// can ignore the return value.
function summarizeFrames(samples, label) {
  // Drop the first sample: it is the delta from the sampler's own start
  // timestamp to the first rAF, not a rendered frame's duration.
  const frames = samples.slice(1)
  if (frames.length === 0) {
    console.log(`  ${label}: no frames captured`)
    return null
  }
  const sorted = [...frames].sort((a, b) => a - b)
  const avg = frames.reduce((s, v) => s + v, 0) / frames.length
  const p50 = percentile(sorted, 50)
  const p95 = percentile(sorted, 95)
  const max = sorted[sorted.length - 1]
  const dropped = frames.filter((f) => f > 16.7).length
  const janky = frames.filter((f) => f > 50).length
  console.log(
    `  ${label}: ${frames.length} frames, avg ${fmt(avg)}ms, p50 ${fmt(p50)}ms, p95 ${fmt(p95)}ms, ` +
      `max ${fmt(max)}ms, >16.7ms ${dropped} (${fmt((100 * dropped) / frames.length)}%), >50ms ${janky}`,
  )
  return { avg, p50, p95, max }
}

async function metricsMap(cdp) {
  const { metrics } = await cdp.send('Performance.getMetrics')
  return Object.fromEntries(metrics.map((m) => [m.name, m.value]))
}

function diffMetricsMs(before, after, names) {
  const out = {}
  for (const name of names) {
    if (typeof before[name] === 'number' && typeof after[name] === 'number') {
      out[name] = (after[name] - before[name]) * 1000
    }
  }
  return out
}

const DURATION_METRICS = ['TaskDuration', 'ScriptDuration', 'LayoutDuration', 'RecalcStyleDuration']

function topFunctions(profile, n = 8) {
  if (!profile?.nodes?.length) return []
  const totalHits = profile.nodes.reduce((s, nd) => s + (nd.hitCount || 0), 0) || 1
  const byLabel = new Map()
  for (const nd of profile.nodes) {
    const hits = nd.hitCount || 0
    if (hits === 0) continue
    const cf = nd.callFrame
    const file = (cf.url || '').split('/').pop() || (cf.url ? cf.url : 'native')
    const label = `${cf.functionName || '(anonymous)'} @ ${file}:${cf.lineNumber}`
    byLabel.set(label, (byLabel.get(label) || 0) + hits)
  }
  return [...byLabel.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([label, hits]) => ({ label, pct: (100 * hits) / totalHits }))
}

function printTopFunctions(entries, label) {
  console.log(`  ${label}:`)
  if (entries.length === 0) {
    console.log('    (no samples -- interval shorter than the sampling period)')
    return
  }
  for (const e of entries) {
    console.log(`    ${fmt(e.pct)}%  ${e.label}`)
  }
}

async function drainLongTasks(page) {
  return page.evaluate(() => window.__mvDrainLongTasks())
}

// Returns { total } alongside printing, same reasoning as summarizeFrames.
function summarizeLongTasks(tasks, label) {
  const total = tasks.reduce((s, t) => s + t.duration, 0)
  console.log(`  ${label}: ${tasks.length} long tasks, ${fmt(total)}ms total` + (tasks.length ? `, longest ${fmt(Math.max(...tasks.map((t) => t.duration)))}ms` : ''))
  return { total }
}

// Averages one deck's per-round rollTo() samples. `times` is a mix of
// numbers (settled, wall-clock ms) and '>${timeoutMs}' strings (rollTo
// threw -- see the catch in main()'s roll loop). Per #1084, a timed-out
// sample is not dropped: it counts at timeoutMs, the worst-case bound
// rollTo was given, so a deck that never renders averages as the
// slowest possible deck rather than as NaN (numeric-only averaging
// silently vanished it from perf-compare.mjs's comparison). Exported
// for perf-compare.test.mjs; empty `times` returns NaN, same as before.
export function deckAvgMs(times, timeoutMs) {
  if (!times.length) return NaN
  const values = times.map((t) => (typeof t === 'number' ? t : timeoutMs))
  return values.reduce((s, v) => s + v, 0) / values.length
}

// Sign in, and if the door is not the one this probe expects, say what
// was on the screen instead of what selector was missing (#1024).
//
// `page.fill` on the username field used to be the first thing this did.
// When it timed out -- which is how perf:promotion failed in job 6360 --
// all anyone got was the name of a locator, and every explanation for it
// stayed a guess. There are three states behind that one selector
// (App.svelte:202-208): AuthLogin, which has the field; AuthSetup's gate,
// a lone Enter button shown while no account exists; and 'loading',
// which draws neither. They fail identically and mean entirely different
// things, so the probe reads the page and the session endpoint before it
// gives up.
//
// What tells the two doors apart is the fields, not the button: both
// label their button "Enter" (AuthScreen.svelte's gate branch and the
// login form's submit read the same), and the gate is the one with no
// inputs at all. `setupRequired` in the session body says the same thing
// from the server's side, which is why both are printed.
async function signIn(page) {
  try {
    await page.waitForSelector('input[autocomplete="username"]', { timeout: 30000 })
  } catch {
    let session = 'unreadable'
    try {
      session = JSON.stringify(await page.request.get(`${URL_BASE}/api/auth/session`).then((r) => r.json()))
    } catch (e) {
      session = `request failed: ${e.message}`
    }
    const seen = await page.evaluate(() => ({
      url: location.href,
      title: document.title,
      buttons: [...document.querySelectorAll('button')].map((b) => b.textContent?.trim()).filter(Boolean),
      inputs: [...document.querySelectorAll('input')].map((i) => i.getAttribute('autocomplete') || i.type),
      text: (document.body.innerText || '').replace(/\s+/g, ' ').trim().slice(0, 400),
    }))
    console.error('probe-perf: no login field after 30s.')
    console.error(`  url: ${seen.url}`)
    console.error(`  title: ${seen.title}`)
    console.error(`  inputs on the page: ${seen.inputs.length ? seen.inputs.join(', ') : 'none'}`)
    console.error(`  buttons on the page: ${seen.buttons.length ? seen.buttons.join(' | ') : 'none'}`)
    console.error(`  GET /api/auth/session: ${session}`)
    console.error(`  body text: ${seen.text || '(empty -- the app rendered nothing)'}`)
    throw new Error('probe-perf: the app never showed a login field; see the page state above')
  }
  await page.fill('input[autocomplete="username"]', USER)
}

async function main() {
  const browser = await chromium.launch()
  const context = await browser.newContext({ colorScheme: 'dark', ignoreHTTPSErrors: true })
  await context.addInitScript(INSTRUMENT_INIT)
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page)
  await cdp.send('Performance.enable')
  await cdp.send('Profiler.enable')

  console.log(`probe-perf against ${URL_BASE}`)
  console.log('')

  // GET /api/healthz needs no auth or session (server.go's Version doc
  // comment), so this is safe before login. Falls back to CI_COMMIT_SHA
  // -- set on every GitLab CI job -- so a target that cannot be reached
  // for some reason still tags the JSON with the commit under test
  // rather than leaving it blank.
  let mvCommit = process.env.CI_COMMIT_SHA || null
  try {
    const healthz = await page.request.get(`${URL_BASE}/api/healthz`).then((r) => r.json())
    if (healthz?.version) mvCommit = healthz.version
  } catch {
    /* left as CI_COMMIT_SHA/null above */
  }

  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await signIn(page)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
  const { dismissSetupWizard } = await import('./live-browser.mjs')
  await dismissSetupWizard(page)
  await rollTo(page, 'The fall')

  // Let the websocket connect and the first stats poll land before
  // measuring anything, so the numbers below are steady-state, not
  // startup cost.
  await page.waitForTimeout(3000)

  const flagCount = await page.evaluate(() => document.querySelectorAll('.card[data-card="docket"] .card-grid > *').length)
  const rowCount = await page.evaluate(() => document.querySelectorAll('.card[data-card="live"] .row').length)
  console.log(`context: ${flagCount || 'n/a (docket not yet mounted)'} flag cards in the DOM at first mount, ${rowCount || 'n/a'} stream rows at first mount`)
  console.log('')

  // ---- Phase 1: deck-to-deck roll timing ------------------------------
  console.log(`== Deck switching: ${ROUNDS} rounds through ${ROLL_LABELS.join(' -> ')} ==`)
  const rollTimes = new Map(ROLL_LABELS.map((l) => [l, []]))
  const rollMetricSums = new Map(ROLL_LABELS.map((l) => [l, Object.fromEntries(DURATION_METRICS.map((m) => [m, 0]))]))

  await cdp.send('Profiler.setSamplingInterval', { interval: 200 })
  await cdp.send('Profiler.start')
  const rollLongTasks = []

  for (let round = 0; round < ROUNDS; round++) {
    for (const label of ROLL_LABELS) {
      const before = await metricsMap(cdp)
      let elapsed
      let timedOut = false
      try {
        elapsed = await rollTo(page, label, ROLL_TIMEOUT_MS)
      } catch (err) {
        // A roll that never settles is itself the finding -- record it
        // as a lower bound rather than aborting the whole probe.
        elapsed = ROLL_TIMEOUT_MS
        timedOut = true
        console.log(`  !! ${label}: did not settle within ${ROLL_TIMEOUT_MS}ms (${err.message.split('\n')[0]})`)
      }
      const after = await metricsMap(cdp)
      rollTimes.get(label).push(timedOut ? `>${elapsed}` : elapsed)
      const diffs = diffMetricsMs(before, after, DURATION_METRICS)
      const sums = rollMetricSums.get(label)
      for (const m of DURATION_METRICS) sums[m] += diffs[m] || 0
      rollLongTasks.push(...(await drainLongTasks(page)))
    }
  }

  const rollProfile = (await cdp.send('Profiler.stop')).profile

  const deckAvg = {}
  const timedOutDecks = []
  for (const label of ROLL_LABELS) {
    const times = rollTimes.get(label)
    const avg = deckAvgMs(times, ROLL_TIMEOUT_MS)
    deckAvg[label] = avg
    if (times.length && times.every((t) => typeof t !== 'number')) timedOutDecks.push(label)
    const sums = rollMetricSums.get(label)
    const perRoll = DURATION_METRICS.map((m) => `${m} ${fmt(sums[m] / ROUNDS)}ms`).join(', ')
    console.log(`  ${label.padEnd(12)}: wall ${times.map((t) => t + 'ms').join(', ')} (avg ${fmt(avg)}ms) | ${perRoll}`)
  }
  const rollLongTaskStats = summarizeLongTasks(rollLongTasks, 'long tasks across all rolls')
  printTopFunctions(topFunctions(rollProfile, 10), 'top self-time functions during deck switching')
  console.log('')

  // ---- Phase 2: scrolling the docket (unvirtualised flag cards) -------
  console.log(`== Scrolling the docket for ${SCROLL_MS}ms ==`)
  await rollTo(page, 'Flags', 45000).catch((err) => console.log(`  !! Flags roll did not settle: ${err.message.split('\n')[0]}`))
  await page.waitForTimeout(500)
  const flagsInDom = await page.evaluate(() => document.querySelectorAll('.card[data-card="docket"] .card-grid > *').length)
  console.log(`  ${flagsInDom} flag cards in the DOM`)

  await cdp.send('Profiler.setSamplingInterval', { interval: 200 })
  await cdp.send('Profiler.start')
  await page.evaluate((ms) => window.__mvStartFrameSampler(ms), SCROLL_MS)
  const scrollBox = await page.locator('.card[data-card="docket"] .flags').boundingBox()
  const deadline = Date.now() + SCROLL_MS
  while (Date.now() < deadline) {
    if (scrollBox) {
      await page.mouse.move(scrollBox.x + scrollBox.width / 2, scrollBox.y + scrollBox.height / 2)
    }
    await page.mouse.wheel(0, 400)
    await page.waitForTimeout(80)
  }
  await page.waitForTimeout(200)
  const scrollFrames = await page.evaluate(() => window.__mvFrameSamples)
  const scrollProfile = (await cdp.send('Profiler.stop')).profile
  const scrollLongTasks = await drainLongTasks(page)
  const scrollFrameStats = summarizeFrames(scrollFrames, 'scroll frame durations')
  const scrollLongTaskStats = summarizeLongTasks(scrollLongTasks, 'long tasks while scrolling')
  printTopFunctions(topFunctions(scrollProfile, 10), 'top self-time functions while scrolling')
  console.log('')

  // ---- Phase 3: idling on the live stream (websocket arrivals) --------
  console.log(`== Idling on the live stream for ${IDLE_MS}ms (websocket arrivals) ==`)
  await rollTo(page, 'Stream', 45000).catch((err) => console.log(`  !! Stream roll did not settle: ${err.message.split('\n')[0]}`))
  await page.waitForTimeout(500)
  const rowsBefore = await page.evaluate(() => document.querySelectorAll('.card[data-card="live"] .row').length)

  await cdp.send('Profiler.setSamplingInterval', { interval: 200 })
  await cdp.send('Profiler.start')
  await page.evaluate((ms) => window.__mvStartFrameSampler(ms), IDLE_MS)
  await page.waitForTimeout(IDLE_MS + 200)
  const idleFrames = await page.evaluate(() => window.__mvFrameSamples)
  const idleProfile = (await cdp.send('Profiler.stop')).profile
  const idleLongTasks = await drainLongTasks(page)
  const rowsAfter = await page.evaluate(() => document.querySelectorAll('.card[data-card="live"] .row').length)
  console.log(`  rows in DOM: ${rowsBefore} -> ${rowsAfter} over ${IDLE_MS}ms`)
  summarizeFrames(idleFrames, 'idle-with-live-arrivals frame durations')
  const idleLongTaskStats = summarizeLongTasks(idleLongTasks, 'long tasks while idling')
  printTopFunctions(topFunctions(idleProfile, 10), 'top self-time functions while idling on the live stream')

  await browser.close()

  if (JSON_PATH) {
    const summary = {
      schema: 2,
      commit: mvCommit,
      decks: Object.fromEntries(ROLL_LABELS.map((label) => [label, { wallAvgMs: deckAvg[label] }])),
      longTasks: {
        rolls: { totalMs: rollLongTaskStats.total },
        scroll: { totalMs: scrollLongTaskStats.total },
        idle: { totalMs: idleLongTaskStats.total },
      },
      // The mean, not the p95: over ~165 frames the p95 is quantised to
      // whole 16.7 ms periods and flipped the gate on one frame (#1111).
      // The p95 is still printed above for the eye.
      docketScroll: { frameAvgMs: scrollFrameStats ? scrollFrameStats.avg : null },
    }
    fs.writeFileSync(JSON_PATH, JSON.stringify(summary, null, 2) + '\n')
  }

  // A deck that never rendered in any round is a failure, not a
  // measurement -- deckAvgMs() above already folds it into the average
  // at the timeout ceiling so perf-compare.mjs sees it as the slowest
  // possible result, but the probe itself must also refuse to pass
  // silently (#1084). The JSON is written first so the artifact still
  // captures what happened.
  if (timedOutDecks.length) {
    console.error(`probe-perf: ${timedOutDecks.join(', ')} never rendered within ${ROLL_TIMEOUT_MS}ms in any round -- failing rather than reporting a measurement.`)
    process.exitCode = 1
  }
}

// Skipped when imported for its exports (perf-compare.test.mjs does
// exactly that, for deckAvgMs) -- only runs as a probe when invoked
// directly as a script, matching perf-compare.mjs's own guard.
if (import.meta.url === `file://${process.argv[1]}`) {
  URL_BASE = process.env.MV_URL
  USER = process.env.MV_USER
  PASS = process.env.MV_PASS
  if (!URL_BASE || !USER || !PASS) {
    console.error('MV_URL/MV_USER/MV_PASS unset -- source the demo credentials file first.')
    process.exit(2)
  }

  const argv = process.argv.slice(2)
  const jsonFlagIdx = argv.indexOf('--json')
  JSON_PATH = jsonFlagIdx === -1 ? null : argv[jsonFlagIdx + 1]
  if (jsonFlagIdx !== -1 && !JSON_PATH) {
    console.error('--json needs a path')
    process.exit(2)
  }

  main().catch((err) => {
    console.error(err)
    process.exit(1)
  })
}
