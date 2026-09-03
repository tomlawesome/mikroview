// SPDX-License-Identifier: AGPL-3.0-only
//
// Pure helpers behind TuneLogging.svelte (#435), kept out of the
// component the way setupsteps.ts is kept out of SetupWizard.svelte --
// unit-testable without a DOM, and the one place this logic can live
// rather than being re-derived wherever the component needs it.
import type { PolicyEdge } from './policy.svelte'
import type { TuneLoggingRule } from './types'

// darkBoundaryKeys is the set of boundary-direction pairs the analyse
// request's `darkBoundaries` field names (contract §3): every pushed
// pair that neither logs nor has been declared intentionally quiet.
// Mirrors Topography.svelte's own coverageOf exactly (logged -> observed,
// declared -> quiet, neither -> dark) so the two surfaces can never
// disagree about what counts as dark.
export function darkBoundaryKeys(edges: readonly PolicyEdge[], quietKeys: ReadonlySet<string>): string[] {
  return edges.filter((e) => !e.logged && !quietKeys.has(e.key)).map((e) => e.key)
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
