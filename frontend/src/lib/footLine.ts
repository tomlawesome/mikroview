// SPDX-License-Identifier: AGPL-3.0-only
//
// The stream's foot line (#691, round-30's ratified .foot-legend): a
// band across the foot of the live table carrying the three facts that
// are the reason you would look at the stream at all --
//
//   cam-porch → nas :445 — every ~64 s since 13:52:07 · 14 so far · flagged
//   ▲3.1× wan drops since 13:30
//   guest → wan dark — nothing logged, not nothing sent
//
// -- a repeating pathway, a surge against a baseline, and a boundary
// nothing logs. None of the three is a new detection. Each one restates
// something mikroview has already decided elsewhere, in the fewest words
// that stay true:
//
//   - the pathway is an open repeated_drops flag, phrased through
//     episodeShape.ts's own cadence wording (the drawer's "every ~64 s
//     since 13:52") rather than a second, subtly different one
//   - the surge is an open activity/global/rule_spike flag, sized
//     against the event buffer the table above is already showing
//   - the dark boundary is fall.svelte.ts's own coverage === 'dark',
//     the same reading the fall paints, in the fall's own words
//
// Honesty rule, the one this whole band lives or dies by: a fact with no
// data behind it renders nothing -- not a zero, not a dash, not a
// plausible example. With none of the three available the band does not
// render at all. Every `return null` below is that rule, not an
// oversight, and the multiple in particular is only ever printed when
// the buffer actually holds both sides of the comparison.
import { episodeShapeFor } from './episodeShape'
import { formatHM } from './format'
import type { FallBoundary } from './fall.svelte'
import type { ClientEvent, FirewallEvent, Flag, FlagType } from './types'

/**
 * One fact, split where the ratified markup splits it: `salient` is the
 * `.k` token drawn in full ink, `lead`/`tail` the plain-ink text either
 * side of it. Empty strings where the drawing has nothing there --
 * "▲3.1× wan drops since 13:30" has no lead, "guest → wan dark — …" has
 * one.
 */
export interface FootFact {
  key: 'pathway' | 'surge' | 'dark'
  lead: string
  salient: string
  tail: string
}

// The three spike detectors -- flagPalette.ts's "surge" family minus the
// two members that are not about volume at all (off_hours_activity is
// about the clock, stale_rule about absence). Only these three ask "how
// much, against this subject's own baseline", which is the only question
// a multiple can honestly answer.
const SURGE_TYPES = new Set<FlagType>(['activity_spike', 'global_spike', 'rule_spike'])

// Both sides of the multiple need a window long enough that the rate is
// a rate rather than an accident of when the buffer starts. A minute
// each, matching the one-minute resolution everything else in this
// codebase measures traffic at (stats.timeSeries, the whisper).
const RATE_WINDOW_FLOOR_MS = 60_000

const REFUSALS = new Set(['drop', 'reject'])

function ms(iso: string): number {
  return new Date(iso).getTime()
}

// The most recently active flag of the given kinds, still open. Newest
// by lastSeen: the band has room for one fact of each kind, and the one
// still happening is the one worth the space.
function newestOpen(flags: readonly Flag[], want: (f: Flag) => boolean): Flag | null {
  let best: Flag | null = null
  for (const f of flags) {
    if (f.cleared || !want(f)) continue
    const t = ms(f.lastSeen)
    if (Number.isNaN(t)) continue
    if (best === null || t > ms(best.lastSeen)) best = f
  }
  return best
}

/**
 * The friendly name the event buffer has already resolved for an
 * address, or null when it has none. Names are display only -- the
 * caller falls back to the address itself rather than to a guess, the
 * same relationship EventRow has to srcHostName/dstHostName.
 */
export function nameForAddress(events: readonly ClientEvent[], ip: string): string | null {
  for (const e of events) {
    if (e.srcIp === ip && e.srcHostName) return e.srcHostName
    if (e.dstIp === ip && e.dstHostName) return e.dstHostName
  }
  return null
}

/**
 * Fact one: a pathway being refused over and over.
 *
 * repeated_drops keys one window per (source, destination port) pair and
 * carries that pair in its own target, "10.0.20.11 -> port 445" (see
 * shipped_declarative.go's buildRepeatedDropsDefinition), with the
 * distinct destinations it actually reached in evidence.hosts. The
 * pathway names the destination only when there was exactly one of them:
 * the detector deliberately does not key on the destination, so with
 * several of them there is no single "→ nas" this flag could truthfully
 * claim, and the arrow is dropped rather than pointed at whichever host
 * happened to sort first.
 *
 * The cadence is episodeShape.ts's, computed over the flag's own events
 * where the buffer still holds them and falling back to its
 * firstSeen/lastSeen where it does not -- exactly what the flag drawer
 * does, so the two never phrase the same episode differently.
 */
export function repeatingPathwayFact(
  flags: readonly Flag[],
  events: readonly ClientEvent[],
  nowMs: number,
): FootFact | null {
  const f = newestOpen(flags, (x) => x.type === 'repeated_drops')
  if (!f) return null
  const parsed = /^(.+) -> port (\d+)$/.exec(f.target)
  if (!parsed) return null
  const srcIp = parsed[1]
  const port = Number(parsed[2])

  const hosts = f.evidence?.hosts ?? []
  const src = nameForAddress(events, srcIp) ?? srcIp
  const dst = hosts.length === 1 ? (nameForAddress(events, hosts[0]) ?? hosts[0]) : null
  const pathway = dst ? `${src} → ${dst} :${port}` : `${src} :${port}`

  const episode: FirewallEvent[] = events.filter(
    (e) => e.srcIp === srcIp && e.dstPort === port && REFUSALS.has(e.action),
  )
  const shape = episodeShapeFor(f, episode, nowMs)

  // "· 14 so far" is the flag's own count. No count, nothing to say
  // about how many -- the clause goes, the fact stays.
  const parts = [shape, f.count > 0 ? `${f.count} so far` : '', 'flagged'].filter(Boolean)
  return { key: 'pathway', lead: '', salient: pathway, tail: `— ${parts.join(' · ')}` }
}

// Which slice of the buffer a spike flag is about. global_spike has no
// actor at all (its target is the literal "global"), activity_spike is
// one source, rule_spike one rule -- so the multiple is measured over
// exactly the events that flag is a statement about, never over the
// whole buffer regardless of subject.
function surgeSubject(f: Flag): { match: (e: ClientEvent) => boolean; noun: string } | null {
  switch (f.type) {
    case 'global_spike':
      return { match: () => true, noun: 'traffic' }
    case 'activity_spike':
      return { match: (e) => e.srcIp === f.target, noun: '' }
    case 'rule_spike':
      return { match: (e) => e.ruleLabel === f.target, noun: '' }
    default:
      return null
  }
}

/**
 * Fact two: something running well above its own usual rate.
 *
 * The flag says a surge happened and when it started; the multiple sizes
 * it, from the same buffer the table above is displaying -- the rate of
 * this flag's own subject since it was first seen, over that subject's
 * rate in the buffer before then. That is the flag's own question
 * ("volume against a baseline") answered with data already on screen,
 * not a second detector.
 *
 * It returns null far more often than it returns a number, and that is
 * the point. No events before the flag started, less than a minute of
 * buffer on either side, or a multiple that does not round to an
 * increase, and there is nothing here that can be said truthfully -- so
 * nothing is said. The ▲ never appears over a figure that is not a rise.
 */
export function surgeFact(flags: readonly Flag[], events: readonly ClientEvent[], nowMs: number): FootFact | null {
  const f = newestOpen(flags, (x) => SURGE_TYPES.has(x.type))
  if (!f) return null
  const subject = surgeSubject(f)
  if (!subject) return null
  const since = ms(f.firstSeen)
  if (Number.isNaN(since)) return null

  let before = 0
  let after = 0
  let oldest = Infinity
  for (const e of events) {
    if (!subject.match(e)) continue
    if (e.receivedAt < oldest) oldest = e.receivedAt
    if (e.receivedAt < since) before++
    else after++
  }
  if (before === 0) return null

  const beforeSpan = since - oldest
  const afterSpan = nowMs - since
  if (beforeSpan < RATE_WINDOW_FLOOR_MS || afterSpan < RATE_WINDOW_FLOOR_MS) return null

  const multiple = after / afterSpan / (before / beforeSpan)
  if (!Number.isFinite(multiple)) return null
  const shown = multiple.toFixed(1)
  // "▲1.0×" is not a surge, and a figure below the baseline contradicts
  // the mark it would be drawn with. Either way there is no rise to
  // report, so the slot stays empty.
  if (Number(shown) <= 1) return null

  const name = f.type === 'activity_spike' ? (nameForAddress(events, f.target) ?? f.target) : f.target
  const what = subject.noun || (f.type === 'rule_spike' ? `on rule ${name}` : `from ${name}`)
  const at = formatHM(f.firstSeen)
  return { key: 'surge', lead: '', salient: `▲${shown}×`, tail: `${what}${at ? ` since ${at}` : ''}` }
}

/**
 * Fact three: a boundary nothing logs.
 *
 * fall.svelte.ts already answers this, with the same "only ever claim a
 * definite answer" rule internal/engine/coverage.go uses -- a boundary
 * is 'dark' only when rules were pushed, they do name this exact
 * boundary-direction, and none of them log. 'unknown' is not dark and
 * never renders here; a blank stretch of stream whose cause we cannot
 * name gets no caption.
 */
export function darkBoundaryFact(boundaries: readonly FallBoundary[]): FootFact | null {
  const b = boundaries.find((x) => x.coverage === 'dark')
  if (!b || !b.label) return null
  return { key: 'dark', lead: b.label, salient: 'dark', tail: '— nothing logged, not nothing sent' }
}

/**
 * The band's contents, in the drawing's own order. An empty array means
 * the band does not render -- the caller draws nothing rather than an
 * empty strip.
 */
export function footLineFacts(input: {
  flags: readonly Flag[]
  events: readonly ClientEvent[]
  boundaries: readonly FallBoundary[]
  nowMs: number
}): FootFact[] {
  return [
    repeatingPathwayFact(input.flags, input.events, input.nowMs),
    surgeFact(input.flags, input.events, input.nowMs),
    darkBoundaryFact(input.boundaries),
  ].filter((f): f is FootFact => f !== null)
}
