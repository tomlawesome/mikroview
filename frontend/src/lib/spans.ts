// SPDX-License-Identifier: AGPL-3.0-only
//
// The stream's SPAN control (#703): round 30 draws 15 m · 1 h · 24 h ·
// 14 d on the filter line.
//
// Events live in an in-memory ring only -- there is no event archive --
// so 14 days of lines do not exist anywhere, and 24 hours is true only
// while traffic is slow. The owner's ruling (2026-08-31) is to offer the
// spans the buffer can actually cover and say plainly where the data
// stops, rather than build an archive or let a long button quietly show
// a short answer.
//
// That last case is the one this module exists to prevent. A `14 d`
// button showing nine hours claims the network was quiet for thirteen
// days, when the truth is that we were not looking -- reporting an
// absence of our own as a fact about the network, which this project
// does not do.

export interface Span {
  key: string
  label: string
  seconds: number
}

export const SPANS: Span[] = [
  { key: '15m', label: '15 m', seconds: 900 },
  { key: '1h', label: '1 h', seconds: 3600 },
  { key: '24h', label: '24 h', seconds: 86400 },
  { key: '14d', label: '14 d', seconds: 1209600 },
]

// How far back the buffer reaches right now, in seconds, or null when it
// holds nothing. Driven by the store's own oldest held event, never by
// the configured retention window: the two agree until capacity eviction
// bites, which is exactly when an operator would be misled.
export function reachSeconds(oldestHeld: string | null | undefined, now: number): number | null {
  if (!oldestHeld) return null
  const t = Date.parse(oldestHeld)
  if (!Number.isFinite(t)) return null
  return Math.max(0, (now - t) / 1000)
}

// The shortest span is always offered, so there is always something to
// select and the view has a defined state on an empty buffer. Every
// longer one is offered only once the buffer reaches back that far.
export function spanAvailable(span: Span, reach: number | null): boolean {
  if (span === SPANS[0]) return true
  return reach !== null && reach >= span.seconds
}

// Plain words for how much history is really here. Deliberately coarse:
// this is a statement about what the view can answer, not a readout.
export function describeReach(reach: number | null): string {
  if (reach === null) return 'nothing held yet'
  if (reach < 90) return `holding ${Math.round(reach)} s`
  if (reach < 5400) return `holding ${Math.round(reach / 60)} min`
  if (reach < 172800) return `holding ${Math.round(reach / 3600)} h`
  return `holding ${Math.round(reach / 86400)} d`
}

// Why a span the buffer cannot cover is not offered, in the same voice.
export function unavailableReason(span: Span, reach: number | null): string {
  return `${span.label} of history is not held — the buffer is ${describeReach(reach)}`
}
