// SPDX-License-Identifier: AGPL-3.0-only
//
// perf-compare.mjs baseline.json current.json
//
// Compares two probe-perf.mjs --json summaries (see probe-perf.mjs's own
// comment for the schema) metric by metric and prints every one side by
// side: baseline, current, delta %, verdict. Exits 1 if any metric
// regressed, 0 otherwise.
//
// A metric regresses when it is worse (higher -- every metric here is a
// duration) than the baseline by more than 30% AND by more than 300ms
// (frame p95: 30% and 8ms) -- both conditions, so a tiny number's large
// percentage swing alone doesn't fail the gate, and a large number's
// small percentage move alone doesn't either. Improvements never fail.
// A metric present in only one file prints as new/missing, not a
// failure -- comparing across a schema change should not be a false
// regression.
//
// The comparison rule (regressed()) and the per-file diff (compareMetrics())
// are exported and unit-tested in perf-compare.test.mjs; the rest of this
// file is CLI plumbing (reading the files, printing the table, the exit
// code).

import fs from 'node:fs'

const DEFAULT_PCT = 0.3
const DEFAULT_ABS_MS = 300
const FRAME_P95_PCT = 0.3
const FRAME_P95_ABS_MS = 8

// schema/commit are metadata, not measurements -- excluded from the
// metric walk below so a commit-SHA string never lands in a numeric
// comparison.
const NON_METRIC_KEYS = new Set(['schema', 'commit'])

// Flattens the decks/longTasks/docketScroll trees into dotted metric
// names ("decks.The fall.wallAvgMs", "docketScroll.frameP95Ms"), so a new
// metric group added later is picked up with no change here.
function flattenMetrics(obj) {
  const out = {}
  function walk(node, prefix) {
    if (node !== null && typeof node === 'object' && !Array.isArray(node)) {
      for (const [k, v] of Object.entries(node)) {
        if (!prefix && NON_METRIC_KEYS.has(k)) continue
        walk(v, prefix ? `${prefix}.${k}` : k)
      }
    } else {
      out[prefix] = node
    }
  }
  walk(obj || {}, '')
  return out
}

function thresholdsFor(name) {
  if (name === 'docketScroll.frameP95Ms') return { pct: FRAME_P95_PCT, absMs: FRAME_P95_ABS_MS }
  return { pct: DEFAULT_PCT, absMs: DEFAULT_ABS_MS }
}

// The comparison rule. baselineValue/currentValue are ms; { pct, absMs }
// is the pair of thresholds both of which must be exceeded. A current
// value at or below baseline is never a regression, whatever the
// percentage -- "improvements never fail".
export function regressed(baselineValue, currentValue, { pct, absMs }) {
  const deltaMs = currentValue - baselineValue
  if (deltaMs <= 0) return false
  const deltaPct = baselineValue === 0 ? Infinity : deltaMs / baselineValue
  return deltaPct > pct && deltaMs > absMs
}

/**
 * compareMetrics: pure function over two probe-perf.mjs --json summaries.
 * Returns { rows, failed }. Each row is
 * { name, baseline, current, deltaPct, verdict }, verdict one of
 * 'ok' | 'improved' | 'regressed' | 'new' | 'missing'. `failed` is true
 * iff any row verdict is 'regressed'.
 */
export function compareMetrics(baseline, current) {
  const baseFlat = flattenMetrics(baseline)
  const curFlat = flattenMetrics(current)
  const names = [...new Set([...Object.keys(baseFlat), ...Object.keys(curFlat)])].sort()

  const rows = []
  let failed = false
  for (const name of names) {
    const hasBase = Object.prototype.hasOwnProperty.call(baseFlat, name) && baseFlat[name] != null
    const hasCur = Object.prototype.hasOwnProperty.call(curFlat, name) && curFlat[name] != null

    if (!hasBase && !hasCur) continue
    if (!hasBase) {
      rows.push({ name, baseline: null, current: curFlat[name], deltaPct: null, verdict: 'new' })
      continue
    }
    if (!hasCur) {
      rows.push({ name, baseline: baseFlat[name], current: null, deltaPct: null, verdict: 'missing' })
      continue
    }

    const b = baseFlat[name]
    const c = curFlat[name]
    const deltaPct = b === 0 ? (c === 0 ? 0 : Infinity) : ((c - b) / b) * 100
    const bad = regressed(b, c, thresholdsFor(name))
    const verdict = bad ? 'regressed' : c < b ? 'improved' : 'ok'
    if (bad) failed = true
    rows.push({ name, baseline: b, current: c, deltaPct, verdict })
  }
  return { rows, failed }
}

function fmtNum(n) {
  return typeof n === 'number' ? n.toFixed(1) : String(n)
}

function fmtPct(p) {
  if (p === null) return '--'
  if (!Number.isFinite(p)) return 'inf'
  return `${p >= 0 ? '+' : ''}${p.toFixed(1)}%`
}

function printTable(rows) {
  const header = ['metric', 'baseline', 'current', 'delta %', 'verdict']
  const lines = [
    header,
    ...rows.map((r) => [
      r.name,
      r.baseline === null ? '--' : fmtNum(r.baseline),
      r.current === null ? '--' : fmtNum(r.current),
      fmtPct(r.deltaPct),
      r.verdict,
    ]),
  ]
  const widths = header.map((_, i) => Math.max(...lines.map((l) => String(l[i]).length)))
  for (const line of lines) {
    console.log(line.map((cell, i) => String(cell).padEnd(widths[i])).join('  '))
  }
}

function main() {
  const [baselinePath, currentPath] = process.argv.slice(2)
  if (!baselinePath || !currentPath) {
    console.error('usage: perf-compare.mjs baseline.json current.json')
    process.exit(2)
  }
  const baseline = JSON.parse(fs.readFileSync(baselinePath, 'utf8'))
  const current = JSON.parse(fs.readFileSync(currentPath, 'utf8'))
  const { rows, failed } = compareMetrics(baseline, current)
  printTable(rows)
  console.log('')
  console.log(failed ? 'REGRESSED' : 'ok')
  process.exit(failed ? 1 : 0)
}

// Skipped when imported for its exports (the test file does exactly
// that) -- only runs main() when invoked directly as a script.
if (import.meta.url === `file://${process.argv[1]}`) {
  main()
}
