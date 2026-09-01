// SPDX-License-Identifier: AGPL-3.0-only
//
// Pure math behind the whisper (issue #644, round-9/round-22/round-29's
// ratified "quiet line above the table": rate curve, drop share, top
// talker, top port). Kept apart from lib/whisper.svelte.ts's reactive
// click/fence state so the arithmetic is testable without a component or
// $state at all.
import { topNBy } from './topN'
import type { Action, ClientEvent, TimeBucket } from './types'

// The mockup's own span (round-29's #wbar: "13:47"..."14:02 · now", its
// aria-label says "the last 15 minutes"). Stats.TimeSeries
// (internal/store/ring.go) always carries 60 one-minute buckets, so this
// is a slice of data already fetched for the scene bar's own eps/buffer
// readouts, not a new query.
export const WHISPER_WINDOW_MINUTES = 15

// drop and reject both read as "refused" here, mirroring Fall.svelte's
// laneOf -- the whisper and the fall must not answer "what fraction of
// this traffic got refused" differently.
function isDropAction(action: string): boolean {
  return action === 'drop' || action === 'reject'
}

export function bucketTotal(b: TimeBucket): number {
  let total = 0
  for (const n of Object.values(b.byAction)) total += n ?? 0
  return total
}

function bucketDrops(b: TimeBucket): number {
  let drops = 0
  for (const [action, n] of Object.entries(b.byAction) as [Action, number | undefined][]) {
    if (isDropAction(action)) drops += n ?? 0
  }
  return drops
}

// The most recent `n` buckets off a TimeSeries, oldest first -- matching
// Stats.TimeSeries' own order. Never more than the series actually has.
export function recentBuckets(series: TimeBucket[], n: number): TimeBucket[] {
  return series.slice(Math.max(0, series.length - n))
}

// null (not 0) over a window with no traffic at all -- there is nothing
// to report a share of, and 0% would misread as "clean" rather than "no
// data yet" (the "leave the slot honestly absent" rule this issue asks
// for).
export function dropShare(buckets: TimeBucket[]): number | null {
  let drops = 0
  let total = 0
  for (const b of buckets) {
    drops += bucketDrops(b)
    total += bucketTotal(b)
  }
  return total > 0 ? drops / total : null
}

// The bucket whose minute contains `atMs`, if this series has one --
// every click on the whisper's curve targets one of its own buckets,
// never an arbitrary timestamp.
export function bucketAt(buckets: TimeBucket[], atMs: number): TimeBucket | undefined {
  return buckets.find((b) => {
    const start = new Date(b.time).getTime()
    return atMs >= start && atMs < start + 60_000
  })
}

export function eventsBetween(events: ClientEvent[], startMs: number, endMs: number): ClientEvent[] {
  return events.filter((e) => e.receivedAt >= startMs && e.receivedAt < endMs)
}

// Top talker/top port ask the same "who/what led this window" question
// MetricsTotals.svelte's ledger already asks of the whole buffer (see
// lib/topN.ts) -- reused rather than re-implemented, just over the
// whisper's own narrower, time-scoped slice of events.
export function topTalker(events: ClientEvent[]): string | undefined {
  return topNBy(events, (e) => e.srcHostName || e.srcIp, 1)[0]?.label
}

export function topPort(events: ClientEvent[]): string | undefined {
  return topNBy(
    events,
    (e) => (e.dstPort ? String(e.dstPort) : e.srcPort ? String(e.srcPort) : undefined),
    1,
  )[0]?.label
}
