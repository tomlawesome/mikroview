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

import { chromium } from 'playwright'
import { dismissSetupWizard } from './live-browser.mjs'

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

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- source the demo credentials file first.')
  process.exit(2)
}

const ROLL_LABELS = ['The fall', 'Topography', 'Metrics', 'Stream', 'Flags', 'Entities', 'Settings']
const ROUNDS = 3
const SCROLL_MS = 3000
const IDLE_MS = 5000

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

function summarizeFrames(samples, label) {
  // Drop the first sample: it is the delta from the sampler's own start
  // timestamp to the first rAF, not a rendered frame's duration.
  const frames = samples.slice(1)
  if (frames.length === 0) {
    console.log(`  ${label}: no frames captured`)
    return
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

function summarizeLongTasks(tasks, label) {
  const total = tasks.reduce((s, t) => s + t.duration, 0)
  console.log(`  ${label}: ${tasks.length} long tasks, ${fmt(total)}ms total` + (tasks.length ? `, longest ${fmt(Math.max(...tasks.map((t) => t.duration)))}ms` : ''))
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

  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('#main-content', { timeout: 15000 })
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
        elapsed = await rollTo(page, label, 45000)
      } catch (err) {
        // A roll that never settles is itself the finding -- record it
        // as a lower bound rather than aborting the whole probe.
        elapsed = 45000
        timedOut = true
        console.log(`  !! ${label}: did not settle within 45000ms (${err.message.split('\n')[0]})`)
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

  for (const label of ROLL_LABELS) {
    const times = rollTimes.get(label)
    const numeric = times.filter((t) => typeof t === 'number')
    const avg = numeric.length ? numeric.reduce((s, v) => s + v, 0) / numeric.length : NaN
    const sums = rollMetricSums.get(label)
    const perRoll = DURATION_METRICS.map((m) => `${m} ${fmt(sums[m] / ROUNDS)}ms`).join(', ')
    console.log(`  ${label.padEnd(12)}: wall ${times.map((t) => t + 'ms').join(', ')} (avg ${fmt(avg)}ms) | ${perRoll}`)
  }
  summarizeLongTasks(rollLongTasks, 'long tasks across all rolls')
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
  summarizeFrames(scrollFrames, 'scroll frame durations')
  summarizeLongTasks(scrollLongTasks, 'long tasks while scrolling')
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
  summarizeLongTasks(idleLongTasks, 'long tasks while idling')
  printTopFunctions(topFunctions(idleProfile, 10), 'top self-time functions while idling on the live stream')

  await browser.close()
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
