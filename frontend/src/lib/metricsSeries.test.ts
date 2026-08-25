// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  buildAxis,
  buildHour,
  inkFor,
  METRIC_ACTION_ORDER,
  minuteIndexOf,
  readMinute,
  scaleFor,
  SCALE_FLOOR,
} from './metricsSeries'
import type { FlagTimeBucket, TimeBucket } from './types'

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

const traffic: TimeBucket[] = [
  { time: minute(0), byAction: { accept: 400, drop: 9 } },
  { time: minute(1), byAction: { accept: 410, drop: 88, reject: 2 } },
  { time: minute(2), byAction: { accept: 421, drop: 12 } },
]

const flags: FlagTimeBucket[] = [
  { time: minute(1), byType: { repeated_drops: 2, activity_spike: 1 } },
  { time: minute(2), byType: {} },
]

describe('the metrics hour', () => {
  it('reads both payloads against one shared minute axis', () => {
    // The record's "flags share the traffic coordinate space" only holds
    // if one axis covers both -- otherwise an episode at 13:01 would not
    // sit under the burst at 13:01.
    const axis = buildAxis(traffic, flags)
    expect(axis).toEqual([minute(0), minute(1), minute(2)])
  })

  it('matches minutes that are spelled differently by the two endpoints', () => {
    const axis = buildAxis([{ time: '2026-08-24T13:00:00Z', byAction: {} }], [
      { time: '2026-08-24T13:00:00.000+00:00', byType: {} },
    ])
    expect(axis).toHaveLength(1)
  })

  it('ignores buckets whose time does not parse rather than inventing a column', () => {
    expect(buildAxis([{ time: 'not a time', byAction: { accept: 1 } }], [])).toEqual([])
  })

  it('draws every action, including one that stayed silent all hour', () => {
    const hour = buildHour(traffic, flags)
    expect(hour.traffic.map((s) => s.key)).toEqual([...METRIC_ACTION_ORDER])
    expect(hour.traffic.find((s) => s.key === 'marked')!.total).toBe(0)
  })

  it('groups the two inks contiguously so the group brackets can bracket them', () => {
    const inks = METRIC_ACTION_ORDER.map(inkFor)
    expect(inks.slice(0, 5).every((i) => i === 'traffic')).toBe(true)
    expect(inks.slice(5)).toEqual(['refused', 'refused'])
  })

  it('declares a per-series scale, floored at 12/min', () => {
    const hour = buildHour(traffic, flags)
    expect(hour.traffic.find((s) => s.key === 'accept')!.scale).toBe(421)
    // reject peaked at 2 -- without the floor it would fill its strip
    // and read as a busy series.
    expect(hour.traffic.find((s) => s.key === 'reject')!.scale).toBe(SCALE_FLOOR)
    expect(scaleFor([])).toBe(SCALE_FLOOR)
    expect(scaleFor([0, 1, 2])).toBe(SCALE_FLOOR)
    expect(scaleFor([0, 40])).toBe(40)
  })

  it('keeps every flag type, live ones first and the silent ones as labelled hairlines', () => {
    const hour = buildHour(traffic, flags)
    expect(hour.flags).toHaveLength(16)
    expect(hour.flags.slice(0, 2).map((s) => s.key)).toEqual(['activity_spike', 'repeated_drops'])
    expect(hour.flags.slice(0, 2).every((s) => s.spoke)).toBe(true)
    expect(hour.flags.slice(2).every((s) => !s.spoke && s.total === 0)).toBe(true)
    // Every row is named, silent or not -- "never twelve empty charts
    // and never hidden".
    expect(hour.flags.every((s) => s.label.length > 0 && s.short.length > 0)).toBe(true)
  })

  it('counts the hour the way the header reports it', () => {
    const hour = buildHour(traffic, flags)
    expect(hour.eventsInHour).toBe(400 + 9 + 410 + 88 + 2 + 421 + 12)
    expect(hour.episodesInHour).toBe(3)
    expect(hour.typesThatSpoke).toBe(2)
    expect(hour.episodesPerMinute).toEqual([0, 3, 0])
    expect(hour.brink).toBe(minute(2))
  })

  it('reads a whole minute at once, across every series', () => {
    const hour = buildHour(traffic, flags)
    const reading = readMinute(hour, 1)!
    expect(reading.time).toBe(minute(1))
    expect(reading.traffic).toHaveLength(7)
    expect(reading.traffic.find((t) => t.key === 'drop')).toMatchObject({ value: 88, ink: 'refused' })
    expect(reading.traffic.find((t) => t.key === 'accept')).toMatchObject({ value: 410, ink: 'traffic' })
    expect(reading.episodeTotal).toBe(3)
    expect(reading.episodes.map((e) => e.value)).toEqual([1, 2])
  })

  it('has no reading for a minute off the axis', () => {
    const hour = buildHour(traffic, flags)
    expect(readMinute(hour, -1)).toBeNull()
    expect(readMinute(hour, 3)).toBeNull()
  })

  it('finds the cursor by its own minute, so a sliding axis cannot move it', () => {
    const axis = buildAxis(traffic, flags)
    expect(minuteIndexOf(axis, minute(1))).toBe(1)
    // The same minute, spelled by the other endpoint.
    expect(minuteIndexOf(axis, '2026-08-24T13:01:00.000+00:00')).toBe(1)
    // Aged off the hour: the cursor is simply gone, not clamped onto a
    // neighbouring minute the operator never selected.
    expect(minuteIndexOf(axis, minute(9))).toBe(-1)
    expect(minuteIndexOf(axis, null)).toBe(-1)
  })

  it('is empty, not broken, before any data arrives', () => {
    const hour = buildHour([], [])
    expect(hour.axis).toEqual([])
    expect(hour.brink).toBeNull()
    expect(hour.eventsInHour).toBe(0)
    expect(hour.traffic.every((s) => s.scale === SCALE_FLOOR && s.now === 0)).toBe(true)
  })
})
