// SPDX-License-Identifier: AGPL-3.0-only
//
// The one data set behind all three metrics views (#488,
// docs/design/screens/metrics/DESIGN.md). Seismograph, Register and
// Table are three readings of what this module produces, so the shape,
// the ordering, the per-series scale and the 12/min floor are decided
// once here rather than three times in three components that could
// drift apart.
//
// Pure functions over the two server payloads (GET /api/stats's
// timeSeries and GET /api/flags's timeSeries) -- no runes, no DOM -- so
// the record's load-bearing rules are unit-testable without rendering
// anything.
import type { Action, FlagTimeBucket, FlagType, TimeBucket } from './types'
import { ACTION_LABELS } from './actions'

// The record's floor: "per-series scale, declared beside the series
// name, floored at 12/min so a series that whispered all hour draws as
// a thread, never inflated to look busy". Without it, a series whose
// hour peaked at 2/min would be drawn against a scale of 2 and fill its
// strip -- reading as busy when it was nearly silent.
export const SCALE_FLOOR = 12

// The record's "two chart inks only": traffic blue, refused red. Which
// of the two a series wears is its *meaning*, never its identity --
// identity is always the label, which is why every view prints one.
export type ChartInk = 'traffic' | 'refused'

// drop and reject are the fall's refused grammar; everything else is
// traffic. This is the whole colour story on the page.
const REFUSED: ReadonlySet<Action> = new Set<Action>(['drop', 'reject'])

export function inkFor(action: Action): ChartInk {
  return REFUSED.has(action) ? 'refused' : 'traffic'
}

// Grouped, not the filter-bar order (lib/actions.ts's ACTIONS): the
// record's group brackets ("TRAFFIC and REFUSED group brackets replace a
// legend") only work if the two inks are contiguous. Every action is
// always drawn, including one that stayed at zero -- with the floor
// above, a silent series is an honest thread on its baseline, which is
// what the record asks for and what a hidden series cannot say.
export const METRIC_ACTION_ORDER: readonly Action[] = [
  'accept',
  'log',
  'marked',
  'natted',
  'unknown',
  'drop',
  'reject',
] as const

// The detector registry, in types.ts's own declaration order -- the
// record's "the flag-type list is the detector registry". Order within
// the spoke/silent halves is this order; which half a type is in is
// decided by whether it spoke.
export const FLAG_TYPE_ORDER: readonly FlagType[] = [
  'port_scan',
  'activity_spike',
  'critical_port',
  'global_spike',
  'distributed_brute_force',
  'outbound_anomaly',
  'internal_recon',
  'rule_spike',
  'repeated_drops',
  'low_slow_scan',
  'off_hours_activity',
  'device_silence',
  'new_device',
  'stale_rule',
  'unexpected_mail_sender',
  'known_bad_ip',
] as const

// Same labels Flags.svelte and Exclusions.svelte carry -- duplicated
// rather than shared, which is the convention already established in
// this codebase for these two tables (see those files' own notes). This
// copy replaces the one that lived in the removed FlagsChart.svelte.
export const FLAG_TYPE_LABELS: Record<FlagType, string> = {
  port_scan: 'Port scan',
  activity_spike: 'Activity spike',
  critical_port: 'Critical-port attempts',
  global_spike: 'Network-wide volume spike',
  distributed_brute_force: 'Distributed brute-force',
  outbound_anomaly: 'Outbound anomaly',
  internal_recon: 'Internal reconnaissance',
  rule_spike: 'Rule hit-rate spike',
  repeated_drops: 'Repeated drops on a port',
  low_slow_scan: 'Low-and-slow port scan',
  off_hours_activity: 'Off-hours activity',
  device_silence: 'Device gone quiet',
  new_device: 'New device',
  stale_rule: 'Stale firewall rule',
  unexpected_mail_sender: 'Unexpected mail sender',
  known_bad_ip: 'Known-bad IP (blocklist match)',
}

// The record allows labels "abbreviated only where a gutter demands
// it" -- the register's rotated column heads are that gutter, and
// nowhere else uses these. Every one is still a word an operator can
// read, never an initialism.
export const FLAG_TYPE_SHORT_LABELS: Record<FlagType, string> = {
  port_scan: 'Port scan',
  activity_spike: 'Activity spike',
  critical_port: 'Critical port',
  global_spike: 'Network spike',
  distributed_brute_force: 'Brute force',
  outbound_anomaly: 'Outbound',
  internal_recon: 'Internal recon',
  rule_spike: 'Rule spike',
  repeated_drops: 'Repeated drops',
  low_slow_scan: 'Low and slow',
  off_hours_activity: 'Off-hours',
  device_silence: 'Device quiet',
  new_device: 'New device',
  stale_rule: 'Stale rule',
  unexpected_mail_sender: 'Mail sender',
  known_bad_ip: 'Known-bad IP',
}

/** One traffic action, as a rate per minute across the hour. */
export interface RateSeries {
  key: Action
  label: string
  ink: ChartInk
  /** One value per axis minute, oldest first. */
  values: number[]
  /** The brink minute's value -- what the gutter prints as "now". */
  now: number
  /** The hour's total, which is what the Table and ledger add up. */
  total: number
  /** The declared, per-series scale: the hour's peak, floored. */
  scale: number
}

/** One flag type, as discrete episodes raised per minute. */
export interface EpisodeSeries {
  key: FlagType
  label: string
  short: string
  values: number[]
  total: number
  /** False for a type that raised nothing this hour: a labelled hairline. */
  spoke: boolean
}

export interface MetricsHour {
  /** Minute-aligned ISO times, oldest first. Empty before first data. */
  axis: string[]
  traffic: RateSeries[]
  /** Spoke first, then silent; registry order within each half. */
  flags: EpisodeSeries[]
  /** Episodes raised in each axis minute, all types summed. */
  episodesPerMinute: number[]
  eventsInHour: number
  episodesInHour: number
  typesThatSpoke: number
  /** The newest minute on the axis -- the brink. Null when empty. */
  brink: string | null
}

const MINUTE_MS = 60_000

function minuteKey(iso: string): number | null {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return null
  return Math.floor(t / MINUTE_MS)
}

/**
 * The shared time axis both payloads are read against.
 *
 * The two series arrive from different endpoints on different refresh
 * cadences, so their bucket lists are not guaranteed to be the same
 * length or to start at the same minute. The record's "flags share the
 * traffic coordinate space" only holds if one axis is derived from both
 * and every series is then mapped onto it -- otherwise a flag tick at
 * 13:52 would not sit under the traffic burst at 13:52, which is the
 * whole reason the two are on one surface.
 *
 * Times are normalised to the minute rather than compared as strings:
 * two endpoints can spell the same minute differently, and a mismatch
 * would silently split one minute into two columns.
 */
export function buildAxis(traffic: TimeBucket[], flags: FlagTimeBucket[]): string[] {
  const keys = new Set<number>()
  for (const b of traffic) {
    const k = minuteKey(b.time)
    if (k !== null) keys.add(k)
  }
  for (const b of flags) {
    const k = minuteKey(b.time)
    if (k !== null) keys.add(k)
  }
  return [...keys].sort((a, b) => a - b).map((k) => new Date(k * MINUTE_MS).toISOString())
}

/** The declared scale for a series: its own hour peak, floored. */
export function scaleFor(values: number[]): number {
  return Math.max(SCALE_FLOOR, ...values, 0)
}

/** The axis position of a minute, or -1 if that minute has aged off. */
export function minuteIndexOf(axis: string[], iso: string | null): number {
  if (!iso) return -1
  const want = minuteKey(iso)
  if (want === null) return -1
  return axis.findIndex((t) => minuteKey(t) === want)
}

export function buildHour(traffic: TimeBucket[], flags: FlagTimeBucket[]): MetricsHour {
  const axis = buildAxis(traffic, flags)
  const slot = new Map<number, number>()
  axis.forEach((t, i) => {
    const k = minuteKey(t)
    if (k !== null) slot.set(k, i)
  })

  const zeros = () => new Array<number>(axis.length).fill(0)

  const byAction = new Map<Action, number[]>()
  for (const action of METRIC_ACTION_ORDER) byAction.set(action, zeros())
  for (const bucket of traffic) {
    const k = minuteKey(bucket.time)
    const i = k === null ? undefined : slot.get(k)
    if (i === undefined) continue
    for (const action of METRIC_ACTION_ORDER) {
      const v = bucket.byAction[action] ?? 0
      byAction.get(action)![i] += v
    }
  }

  const byType = new Map<FlagType, number[]>()
  for (const type of FLAG_TYPE_ORDER) byType.set(type, zeros())
  for (const bucket of flags) {
    const k = minuteKey(bucket.time)
    const i = k === null ? undefined : slot.get(k)
    if (i === undefined) continue
    for (const type of FLAG_TYPE_ORDER) {
      const v = bucket.byType[type] ?? 0
      byType.get(type)![i] += v
    }
  }

  const trafficSeries: RateSeries[] = METRIC_ACTION_ORDER.map((key) => {
    const values = byAction.get(key)!
    return {
      key,
      label: ACTION_LABELS[key],
      ink: inkFor(key),
      values,
      now: values.length > 0 ? values[values.length - 1] : 0,
      total: values.reduce((a, b) => a + b, 0),
      scale: scaleFor(values),
    }
  })

  const flagSeries: EpisodeSeries[] = FLAG_TYPE_ORDER.map((key) => {
    const values = byType.get(key)!
    const total = values.reduce((a, b) => a + b, 0)
    return {
      key,
      label: FLAG_TYPE_LABELS[key],
      short: FLAG_TYPE_SHORT_LABELS[key],
      values,
      total,
      spoke: total > 0,
    }
  })
  // "Live rows sort to the paper's top with their ticks; silent rows
  // sink, dimmed": a stable partition, not a sort by count -- the eye
  // meets news first, and the registry order is still readable inside
  // each half.
  const ordered = [...flagSeries.filter((s) => s.spoke), ...flagSeries.filter((s) => !s.spoke)]

  const episodesPerMinute = zeros()
  for (const s of flagSeries) s.values.forEach((v, i) => (episodesPerMinute[i] += v))

  return {
    axis,
    traffic: trafficSeries,
    flags: ordered,
    episodesPerMinute,
    eventsInHour: trafficSeries.reduce((a, s) => a + s.total, 0),
    episodesInHour: flagSeries.reduce((a, s) => a + s.total, 0),
    typesThatSpoke: flagSeries.filter((s) => s.spoke).length,
    brink: axis.length > 0 ? axis[axis.length - 1] : null,
  }
}

/**
 * Every figure for one minute, in one object -- the record's "the
 * cursor reads a whole minute at once across every series". One
 * function so the drum's gutter readings, the register's cross-section
 * and the keyboard announcement can never disagree about what a minute
 * contained.
 */
export interface MinuteReading {
  time: string
  traffic: { key: Action; label: string; ink: ChartInk; value: number }[]
  episodes: { key: FlagType; label: string; value: number }[]
  episodeTotal: number
}

export function readMinute(hour: MetricsHour, index: number): MinuteReading | null {
  if (index < 0 || index >= hour.axis.length) return null
  return {
    time: hour.axis[index],
    traffic: hour.traffic.map((s) => ({ key: s.key, label: s.label, ink: s.ink, value: s.values[index] })),
    episodes: hour.flags
      .filter((s) => s.values[index] > 0)
      .map((s) => ({ key: s.key, label: s.label, value: s.values[index] })),
    episodeTotal: hour.episodesPerMinute[index] ?? 0,
  }
}
