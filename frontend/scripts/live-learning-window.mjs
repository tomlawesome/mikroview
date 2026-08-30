// SPDX-License-Identifier: AGPL-3.0-only
//
// #639: the learning-window status line the watchers bench renders per
// definition, driven against a real instance -- both halves of the
// change: the wire contract (learning omitted entirely for a definition
// with no warm-up concept, present with a floor even at zero keys,
// nearest omitted exactly when there is nothing left to report progress
// on) and the five-state presentation text EngineRoomWatchers.svelte
// renders from it.
//
// The floor is (at least) 14 days of history -- see
// internal/engine.BaselineFloor -- so nothing a live-check run does can
// ever clear it. States 4 ("Ready for N of M sources...") and 5
// ("Baselines established...") are therefore unreachable here and are
// not asserted against specific numbers; everything else is. The JS
// below recomputes the expected sentence from the server's own
// `learning` object (mirroring frontend/src/lib/detectorCopy.ts's
// learningSummary) and compares it against what each row actually
// renders, so the check holds whatever state a given detector's
// keys/ready happen to be in when this runs -- which depends on every
// scenario that fed traffic before it in filename order, not on
// anything this file controls.
//
// Shares one instance with every other scenario in this directory.

import { session, feedSyslog, check, done, goTo } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

const { page, consoleErrors } = await session()

// A little more traffic of our own, spread over enough source IPs that
// activity_spike/global_spike/rule_spike/off_hours_activity/low_slow_scan
// all have something to have observed by the time this runs, on top of
// whatever every earlier scenario in filename order already fed.
feedSyslog(80, 'live-learning-window')

async function fetchDefinitions() {
  return page.request
    .get(`${URL_BASE}/api/definitions`)
    .then((r) => r.json())
    .then((b) => b.definitions ?? [])
}

const defs = await fetchDefinitions()
check(defs.length > 0, `the definitions list is non-empty (${defs.length})`)

const BASELINE_BACKED = ['activity_spike', 'global_spike', 'rule_spike', 'off_hours_activity', 'low_slow_scan']

// --- the wire contract ---------------------------------------------------

for (const d of defs) {
  const hasLearning = Object.prototype.hasOwnProperty.call(d, 'learning')
  if (BASELINE_BACKED.includes(d.id)) {
    check(hasLearning, `${d.id} (baseline-backed) carries a learning object`)
  } else {
    check(!hasLearning, `${d.id} (no warm-up concept) omits learning entirely rather than a null/empty one`)
  }
}

for (const id of BASELINE_BACKED) {
  const d = defs.find((x) => x.id === id)
  const l = d?.learning
  if (!l) continue // already failed above

  // Each floor dimension carries omitempty server-side (baselineFloorView),
  // so a dimension that does not bind is absent entirely, not sent as 0 --
  // present-and-number or absent are the only honest shapes.
  check(
    (l.floor?.minDurationSeconds === undefined || typeof l.floor.minDurationSeconds === 'number') &&
      (l.floor?.minSamples === undefined || typeof l.floor.minSamples === 'number'),
    `${id}'s floor dimensions, where present, are numbers (${JSON.stringify(l.floor)})`,
  )
  // Not asserted: that every floor binds at least one dimension.
  // global_spike ships with an all-zero BaselineFloor by default (no
  // baselineFloorDuration param set -- see shipped_global_spike.go), so
  // {} is a real, reachable shape here, not a bug in this scenario.
  check(
    Number.isInteger(l.keys) && l.keys >= 0 && Number.isInteger(l.ready) && l.ready >= 0 && l.ready <= l.keys,
    `${id}'s keys/ready are sane integers (keys ${l.keys}, ready ${l.ready})`,
  )

  const wantNearest = l.keys > 0 && l.ready < l.keys
  check(
    wantNearest ? l.nearest !== undefined : l.nearest === undefined,
    `${id}'s nearest is present only while something observed is short of ready -- keys ${l.keys}, ready ${l.ready}, nearest ${JSON.stringify(l.nearest)}`,
  )
  if (l.nearest) {
    check(
      typeof l.nearest.observedForSeconds === 'number' && typeof l.nearest.samples === 'number',
      `${id}'s nearest carries observedForSeconds/samples as numbers (${JSON.stringify(l.nearest)})`,
    )
  }
}

// --- the presentation: recompute the expected sentence, compare to the DOM

const SECONDS_PER_DAY = 86400

function floorPhrase(floor) {
  const parts = []
  const minDurationSeconds = floor.minDurationSeconds ?? 0
  const minSamples = floor.minSamples ?? 0
  if (minDurationSeconds > 0) {
    const days = Math.ceil(minDurationSeconds / SECONDS_PER_DAY)
    parts.push(`${days} day${days === 1 ? '' : 's'}`)
  }
  if (minSamples > 0) {
    parts.push(`${minSamples} sample${minSamples === 1 ? '' : 's'}`)
  }
  return parts.join(', ')
}

function nearestPhrase(floor, observedForSeconds, samples) {
  const parts = []
  const minDurationSeconds = floor.minDurationSeconds ?? 0
  const minSamples = floor.minSamples ?? 0
  if (minDurationSeconds > 0) {
    const haveDays = Math.floor(observedForSeconds / SECONDS_PER_DAY)
    const needDays = Math.ceil(minDurationSeconds / SECONDS_PER_DAY)
    parts.push(`${haveDays} of ${needDays} days`)
  }
  if (minSamples > 0) {
    parts.push(`${samples} of ${minSamples} samples`)
  }
  return parts.join(', ')
}

// Fable's ruling (2026-08-30) on a BaselineFloor binding neither
// dimension -- global_spike ships that way by default. "3 of 14 days"
// has no 14 to report, so both the fresh (keys 0) and observed-but-
// not-ready (ready 0, keys > 0) states collapse into this rather than
// faking an X with a dash or a zero.
const NO_FLOOR_TEXT =
  'Learning — no traffic seen yet; starts evaluating from its first reading (no minimum history required)'

function floorless(floor) {
  return (floor.minDurationSeconds ?? 0) === 0 && (floor.minSamples ?? 0) === 0
}

// Deliberately the same branches as detectorCopy.ts's learningSummary --
// this is the independent check that the shipped wording is what
// actually renders, so it must not import the module under test.
function expectedSummary(l) {
  if (!l) return null
  const { floor, keys, ready, nearest } = l
  if (keys === 0) {
    return floorless(floor) ? NO_FLOOR_TEXT : `Learning — no traffic seen yet; needs ${floorPhrase(floor)} of history per source`
  }
  if (ready === keys) return `Baselines established (${keys} source${keys === 1 ? '' : 's'})`
  if (ready === 0) {
    if (floorless(floor)) return NO_FLOOR_TEXT
    const progress = nearest ? nearestPhrase(floor, nearest.observedForSeconds, nearest.samples) : floorPhrase(floor)
    return keys === 1
      ? `Learning: ${progress}`
      : `Learning — nearest source ${progress} (${ready} of ${keys} sources ready)`
  }
  return `Ready for ${ready} of ${keys} sources; ${keys - ready} still learning`
}

await goTo(page, 'Settings')
await page.waitForFunction(
  () => document.querySelector('.page-header h2')?.textContent.trim() === 'Settings',
  null,
  { timeout: 5000 },
)
await page.click('.path .station:has-text("The watchers") .shead')
await page.waitForSelector('.st-open .bench .row')

// The bench only lists detection definitions this binary can build --
// the same filter detectorSettings.svelte.ts applies -- so that is the
// set worth checking rows for.
const bench = defs.filter((d) => d.intent === 'detection' && d.available)
check(bench.length > 0, `the bench has rows to check (${bench.length})`)

let sawNoTraffic = false
let sawSingleKeyLearning = false
let sawManyKeyLearning = false
let sawFloorlessCollapse = false

for (const d of bench) {
  const row = page.locator(`.st-open .bench .row:has(.id:text-is("${d.id}"))`)
  const rowCount = await row.count()
  check(rowCount === 1, `exactly one row renders for ${d.id} (${rowCount})`)

  const learningEl = row.locator('.learning')
  const present = (await learningEl.count()) > 0
  const expected = expectedSummary(d.learning)

  if (expected === null) {
    check(!present, `${d.id} has no warm-up concept, so its row shows no learning line at all -- not an "n/a" badge`)
    continue
  }
  check(present, `${d.id} carries a learning object, so its row shows a learning line`)
  if (present) {
    const text = (await learningEl.textContent())?.trim()
    check(
      text === expected,
      `${d.id}'s rendered line matches the state its own API data describes -- got "${text}", want "${expected}"`,
    )
  }

  if (d.learning?.keys === 0) sawNoTraffic = true
  if (d.learning && d.learning.ready === 0 && d.learning.keys === 1) sawSingleKeyLearning = true
  if (d.learning && d.learning.ready === 0 && d.learning.keys > 1) sawManyKeyLearning = true
  if (d.learning && d.learning.ready < d.learning.keys && floorless(d.learning.floor)) sawFloorlessCollapse = true
}

// Not asserted as failures if absent -- which of the five states each
// baseline-backed definition is actually in depends on every scenario
// that fed traffic before this one in filename order -- but recorded so
// a run's log says which states this pass actually exercised beyond the
// generic per-row check above.
console.log(
  `  info states observed this run: no-traffic=${sawNoTraffic} single-key-learning=${sawSingleKeyLearning} many-key-learning=${sawManyKeyLearning}`,
)

// global_spike is the one shipped definition with an all-zero
// BaselineFloor (#639's follow-up ruling on NO_FLOOR_TEXT), but its
// baseline also primes on a zero-length window -- see
// buildGlobalSpikeDefinition -- so Ready flips true in the same instant
// Keys goes from 0 to 1. By the time this scenario (mid-alphabet, after
// several traffic-feeding scenarios) opens the bench, global_spike has
// long since settled into "Baselines established" -- confirmed, not
// assumed: the generic per-row loop above already compared its rendered
// line against expectedSummary(), floorless branch included, for
// whatever state it was actually in. The NO_FLOOR_TEXT sentence itself
// is not reachable live here -- it is covered instead, deterministically,
// by detectorCopy.test.ts's unit coverage.
if (!sawFloorlessCollapse) {
  console.log('  info NO_FLOOR_TEXT state not reached live this run (global_spike already past both gates by the time this ran) -- see detectorCopy.test.ts for that coverage')
}

check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
