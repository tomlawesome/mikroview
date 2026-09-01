// SPDX-License-Identifier: AGPL-3.0-only
//
// #732: the totals cards stop being one uniform blue bar each. This
// suite checks the inks the ratified design grounds in data already in
// hand -- device lane, action accept/refused, flag family -- the
// proportion mark replacing the ranked-bar mark for the hour-by-action
// and by-protocol cards, the "top N of M" coverage caption, and the
// guest-ap 0 row (an empty bar, not an omission) surviving all of it.

import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { appState } from '../lib/state.svelte'
import { zonesState } from '../lib/zones.svelte'
import { familyOf } from '../lib/flagPalette'
import { buildHour } from '../lib/metricsSeries'
import { emptyFilters } from '../lib/types'
import type { ClientEvent, Device, FlagTimeBucket, RuleCount, Stats, TimeBucket } from '../lib/types'
import MetricsTotals from './MetricsTotals.svelte'

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

function device(over: Partial<Device>): Device {
  return {
    id: over.id ?? 'd1',
    name: over.name ?? 'device',
    sourceIp: over.sourceIp ?? '10.0.0.5',
    configured: true,
    firstSeen: minute(0),
    lastSeen: minute(0),
    eventCount: 0,
    status: 'live',
    ...over,
  }
}

function event(over: Partial<ClientEvent>): ClientEvent {
  return {
    id: over.id ?? Math.random(),
    time: minute(0),
    deviceId: 'd1',
    sourceIp: '10.0.0.5',
    action: 'accept',
    ruleLabel: '',
    chain: 'forward',
    receivedAt: Date.now(),
    ...over,
    // raw is required on ClientEvent, and Partial<ClientEvent> makes it
    // string | undefined, so the spread alone cannot satisfy the type.
    raw: over.raw ?? '',
  }
}

function stats(overrides: Partial<Stats> = {}): Stats {
  return {
    total: 0,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 0,
    capacity: 100000,
    count: 0,
    oldestHeld: null,
    windowSeconds: 3600,
    connectedClients: 0,
    ...overrides,
  }
}

function ruleCount(rule: string, count: number): RuleCount {
  return { rule, count }
}

// jsdom's CSSOM normalizes a literal colour written to `background`
// (e.g. '#ff9e64' -> 'rgb(255, 158, 100)') when the style attribute is
// read back, so compare like for like rather than against the raw hex
// literal the component wrote.
function asBackground(color: string): string {
  const probe = document.createElement('div')
  probe.style.background = color
  return probe.style.background
}

function findColumn(container: HTMLElement, title: string): HTMLElement {
  const heading = [...container.querySelectorAll('h4')].find((h) => h.textContent?.startsWith(title))
  if (!heading) throw new Error(`no column titled ${title}`)
  return heading.closest('section')! as HTMLElement
}

beforeEach(() => {
  appState.devices = []
  appState.events = []
  appState.stats = null
  appState.filters = emptyFilters()
  zonesState.pushed = []
})

describe('By device (#732: devices take their lane colours)', () => {
  it('wears the rank-ordered lane ink zonesState already assigns, and keeps the guest-ap 0 row an empty, honest bar', () => {
    zonesState.pushed = [{ address: '10.0.0.1/24', network: '10.0.0.0', interface: 'bridge1', comment: 'Lane 1' }]
    appState.events = [event({ srcIp: '10.0.0.5', inInterface: 'bridge1' })]
    appState.devices = [
      device({ name: 'nas', sourceIp: '10.0.0.5', eventCount: 10 }),
      device({ name: 'guest-ap', sourceIp: '10.0.0.9', eventCount: 0 }),
    ]

    const hour = buildHour([], [])
    const { container } = render(MetricsTotals, { hour })

    const column = findColumn(container, 'By device')
    const rows = [...column.querySelectorAll('.row')]
    const nasRow = rows.find((r) => r.textContent?.includes('nas'))!
    const guestRow = rows.find((r) => r.textContent?.includes('guest-ap'))!

    expect(nasRow.querySelector('.bar')!.getAttribute('style')).toContain('var(--lane-lan)')
    expect(guestRow.querySelector('.bar')!.getAttribute('style')).toContain('width: 0%')
    expect(guestRow.querySelector('.count')!.textContent).toBe('0')
  })
})

describe('The hour by action (#732: one split bar, accept-green and drop-red)', () => {
  it('draws one shared track with a green accept segment and a red refused segment, not a ranked bar per action', () => {
    const traffic: TimeBucket[] = [{ time: minute(0), byAction: { accept: 400, drop: 9 } }]
    const hour = buildHour(traffic, [])
    const { container } = render(MetricsTotals, { hour })

    const column = findColumn(container, 'The hour by action')
    expect(column.querySelector('.track')).toBeNull()
    const segments = [...column.querySelectorAll('.segment')]
    expect(segments).toHaveLength(2)
    expect(segments.some((s) => s.getAttribute('style')?.includes('var(--accept)'))).toBe(true)
    expect(segments.some((s) => s.getAttribute('style')?.includes('var(--chart-refused)'))).toBe(true)
  })
})

describe('Episodes by flag type (#732: flag types take the flag ink)', () => {
  it("wears each spoken type's own family ink from lib/flagPalette.ts", () => {
    const flags: FlagTimeBucket[] = [{ time: minute(0), byType: { port_scan: 3 } }]
    const hour = buildHour([], flags)
    const { container } = render(MetricsTotals, { hour })

    const column = findColumn(container, 'Episodes by flag type')
    const bar = column.querySelector('.bar')! as HTMLElement
    expect(bar.style.background).toBe(asBackground(familyOf('port_scan').ink))
  })
})

describe('Coverage caption (#732: a card says how much of the whole it covers)', () => {
  it('appends "top N of M" to the heading when the ranking is known to be partial', () => {
    appState.stats = stats({ topRules: Array.from({ length: 9 }, (_, i) => ruleCount(`rule-${i}`, 9 - i)) })
    const hour = buildHour([], [])
    const { container } = render(MetricsTotals, { hour })

    const heading = [...container.querySelectorAll('h4')].find((h) => h.textContent?.startsWith('Top rules'))!
    expect(heading.textContent).toBe('Top rules · top 8 of 9')
  })

  it('adds no caption when the card already shows everything it knows about', () => {
    appState.stats = stats({ topRules: [ruleCount('lan-wan', 5)] })
    const hour = buildHour([], [])
    const { container } = render(MetricsTotals, { hour })

    const heading = [...container.querySelectorAll('h4')].find((h) => h.textContent?.startsWith('Top rules'))!
    expect(heading.textContent).toBe('Top rules')
  })
})
