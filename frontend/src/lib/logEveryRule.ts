// SPDX-License-Identifier: AGPL-3.0-only
//
// Pure helpers behind LogEveryRule.svelte (#435; the page was "Tune
// logging" until #1134 renamed it), kept out of the component the way
// setupsteps.ts is kept out of SetupWizard.svelte -- unit-testable
// without a DOM, and the one place this logic can live rather than
// being re-derived wherever the component needs it.
//
// The `TuneLogging*` types below keep their names: they mirror the two
// /api/tune-logging endpoints, and those paths do not change (#1134).
import { edgeCoverage } from './coverageRule'
import type { PolicyEdge } from './policy.svelte'
import type { TuneLoggingRule } from './types'

// darkBoundaryKeys is the set of boundary-direction pairs the analyse
// request's `darkBoundaries` field names (contract §3): every pushed
// pair that neither logs nor has been declared intentionally quiet.
// The rule itself is coverageRule.ts's, shared with the map's own
// coverage lens and the city's ground plan, so no surface can drift
// into a different reading of dark (#1014).
export function darkBoundaryKeys(edges: readonly PolicyEdge[], quietKeys: ReadonlySet<string>): string[] {
  return edges.filter((e) => edgeCoverage(e, quietKeys) === 'dark').map((e) => e.key)
}

// waitingMessage is the under-24h state's own words (contract §6):
// "watching for N hours; suggestions arrive at 24 hours" -- no list, no
// counters, nothing derived early (issue decision 5).
export function waitingMessage(hours: number): string {
  const h = Math.max(0, Math.floor(hours))
  return `Watching for ${h} hour${h === 1 ? '' : 's'}; suggestions arrive at 24 hours.`
}

// initialSelection is the record's own default: every rule that crosses
// a dark connection starts ticked, everything else starts unticked
// (contract §6, issue decision 3 -- "every one starts ticked; the
// operator unticks").
export function initialSelection(rules: readonly TuneLoggingRule[]): Set<number> {
  return new Set(rules.filter((r) => r.crossesDark).map((r) => r.id))
}

// counterText is the cost line beside each rule's tick-box (issue
// decision 4): "fired N times / M bytes since <since>", only when the
// push that supplied the counters could actually be matched to this
// rule (countersKnown). Null otherwise -- an unknown count must never
// render as zero, which would be a claim about the network dressed as a
// fact about our own silence.
export function counterText(rule: TuneLoggingRule, since: string): string | null {
  if (!rule.countersKnown) return null
  const when = new Date(since)
  const whenText = Number.isNaN(when.getTime()) ? since : when.toLocaleString()
  return `fired ${rule.packets.toLocaleString()} time${rule.packets === 1 ? '' : 's'} / ${rule.bytes.toLocaleString()} bytes since ${whenText}`
}

// groupRules splits the analysed rules into the two the record draws
// differently: every crosses-dark rule, ticked and shown open, and
// everything else, collapsed and unticked (contract §6). Chain order is
// preserved within each group -- the export's own line order, which is
// also id order since ids are the export's own ordinal.
export function groupRules(rules: readonly TuneLoggingRule[]): {
  dark: TuneLoggingRule[]
  other: TuneLoggingRule[]
} {
  return {
    dark: rules.filter((r) => r.crossesDark),
    other: rules.filter((r) => !r.crossesDark),
  }
}

// countFilterRules is what the drop zone says it is holding before
// anything is sent (#1134: "showing the file name and rule count once
// something is in it") -- the `add` lines in the export's own
// /ip firewall filter section, counted the way
// internal/routeros/export/parser.go counts them: continuation lines
// (trailing `\`) belong to the `add` above them, and a section header
// is written either spaced or slash-joined.
//
// A reading for the label, not a parse: the server's parser is the one
// that decides what the export actually contains, and it is what the
// rule list below is drawn from.
export function countFilterRules(text: string): number {
  return scanFilterSection(text).adds
}

// scanFilterSection is the single pass both countFilterRules above and
// exportProblem below read: whether the text has an
// /ip firewall filter section at all, and how many `add` lines are in
// it. Split out for exportProblem's sake -- "no section" and "a section
// with nothing in it" are different things to say, and counting alone
// cannot tell them apart.
function scanFilterSection(text: string): { found: boolean; adds: number } {
  let adds = 0
  let found = false
  let inFilter = false
  let continuing = false
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    const wasContinuing = continuing
    continuing = line.endsWith('\\')
    if (wasContinuing || line === '' || line.startsWith('#')) continue
    if (line.startsWith('/')) {
      inFilter = line.replace(/\\$/, '').trim().replace(/[\s/]+/g, '/') === '/ip/firewall/filter'
      if (inFilter) found = true
      continue
    }
    if (inFilter && /^add(\s|$)/.test(line)) adds++
  }
  return { found, adds }
}

// exportProblem is what the page says about text that cannot be a
// RouterOS export, before anything is sent (#1186). Pasting a stray
// paragraph used to reach Analyse and come back as the under-24h
// waiting line -- "watching for 0 hours" reads as "nothing yet", not as
// "that was not an export", and the operator had no way to tell which
// they were looking at.
//
// Like countFilterRules, a reading rather than a parse: the server's
// parser is still the one that decides, and it can reject text this
// passes (an unredacted export, contract §3). Null means nothing here
// contradicts an export, never that one is known to be good.
export function exportProblem(text: string): string | null {
  if (!text.trim()) return null
  const { found, adds } = scanFilterSection(text)
  if (!found) {
    return (
      'This does not look like a RouterOS export — there is no /ip firewall filter section in it. ' +
      'Run /export hide-sensitive on the router and hand over the whole output.'
    )
  }
  if (adds === 0) {
    return 'This export has an /ip firewall filter section with no rules in it, so there is nothing to switch logging on for.'
  }
  return null
}
