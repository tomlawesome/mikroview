// SPDX-License-Identifier: AGPL-3.0-only
//
// The baseline (#1016, round 49): which lines are off the established
// pattern today.
//
// The city design's second always-on rule is "colour is the verdict;
// brightness is the baseline". A *line* is `source -> destination · port
// · proto`. A line seen on at least `config.days` distinct days of the
// last `config.of` is *established* and recedes -- thin, dim, no flow. A
// line off that pattern is *off-baseline*: full width, bright, flow
// dashes, and an outline on the building at the end it arrived at
// (#1057; it was a ring on the ground until the city learned to mark a
// building by its own silhouette).
//
// The server sends only the off-baseline lines, never the established
// ones. That is the design, not an optimisation: on a busy network the
// established set is the entire traffic set, and the whole claim is that
// this payload is proportional to novelty rather than to volume. So
// there is no "is this line established?" function here -- absence from
// the list *is* the answer, and a caller that tries to derive
// establishment in the browser is asking a question the payload
// deliberately cannot answer.
//
// Pure by design: no Svelte state, no fetching. The fetched document
// lives in baseline.svelte.ts, so this half can be unit-tested and
// reasoned about without a component tree. Same split as reach.ts and
// its own state module.

/** One line the server judged off-baseline today. */
export interface OffBaselineLine {
  key: string
  srcIp: string
  dstIp: string
  /** null when the event carried no destination port -- 0 is not a port. */
  port: number | null
  proto: string
  /** How many events on this line today, not all time. */
  count: number
  /** Epoch ms of the first event on this line today. */
  firstSeenToday: number
  /** The worst verdict this line drew today. */
  outcome: 'accept' | 'drop'
}

/** The threshold the server applied: seen on `days` of the last `of`. */
export interface BaselineConfig {
  days: number
  of: number
}

/**
 * The whole off-baseline answer.
 *
 * config travels with the lines so the UI can say "3 of the last 14
 * days" using the numbers the server actually used, rather than a
 * hard-coded pair that could drift out of step with the deployment.
 */
export interface OffBaseline {
  config: BaselineConfig
  generatedAt: number
  count: number
  lines: readonly OffBaselineLine[]
}

/**
 * The lines attributable to one drawn element, by a caller-supplied
 * predicate.
 *
 * The predicate is the caller's because only the caller knows what its
 * element is: a rib rolls up a zone pair, a road a district pair, a host
 * dot one host's lane to its gate. Putting that mapping here would mean
 * this module knowing about the city's geometry, which is the drawing
 * layer's business and changes with it.
 *
 * Returns a new array; the argument is never mutated.
 */
export function offBaselineMatching(
  off: OffBaseline,
  pred: (l: OffBaselineLine) => boolean,
): OffBaselineLine[] {
  return off.lines.filter(pred)
}

/**
 * The roll-up: is any line attributable to this element off-baseline
 * today?
 *
 * "A rib, road or dot is as bright as the brightest line it rolls up:
 * one off-baseline line among a thousand lights the one place it
 * happened." Every line in the document is off-baseline by construction,
 * so this is "does any line match" -- stopping at the first, because the
 * answer is a yes/no about how to draw one element, not a count.
 */
export function isOffBaseline(off: OffBaseline, pred: (l: OffBaselineLine) => boolean): boolean {
  return off.lines.some(pred)
}

/**
 * The empty answer: what the map draws before the first fetch lands, and
 * what it falls back to when the register cannot be read.
 *
 * Nothing off-baseline reads as "nothing is unusual", which is the
 * honest picture while the truth is unknown -- the same choice
 * hostsState.refresh makes when it swallows its own failure. The
 * alternative, brightening everything until proven ordinary, would cry
 * wolf on every transient error.
 */
export const EMPTY_OFF_BASELINE: OffBaseline = {
  config: { days: 3, of: 14 },
  generatedAt: 0,
  count: 0,
  lines: [],
}
