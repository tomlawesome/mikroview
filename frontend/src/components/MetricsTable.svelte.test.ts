// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import { buildHour } from '../lib/metricsSeries'
import { formatHM } from '../lib/format'
import type { HourTopBucket, TimeBucket } from '../lib/types'
import MetricsTable from './MetricsTable.svelte'

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

const traffic: TimeBucket[] = [
  { time: minute(0), byAction: { accept: 400, drop: 9 } },
  { time: minute(1), byAction: { accept: 410, drop: 88, reject: 2 } },
]

// #644 round 21's table columns: minute 0 is fully covered by the ring
// buffer (Complete: true, a real winner); minute 1 is one the buffer no
// longer holds every event from (Complete: false) -- the exact case the
// honesty rule exists for: a busy minute that was partially evicted must
// never show a number computed from the surviving fragment.
function rowCells(row: HTMLElement): string[] {
  return within(row)
    .getAllByRole('cell')
    .map((c) => c.textContent ?? '')
}

describe('MetricsTable top port / top talker columns (#644 round 21)', () => {
  it('shows the server-reported winner for a minute the buffer fully covers', () => {
    const tops: HourTopBucket[] = [
      { time: minute(0), talker: 'nas', port: '443', complete: true },
      { time: minute(1), complete: false },
    ]
    const hour = buildHour(traffic, [], tops)
    render(MetricsTable, { hour, cursor: -1, onselect: () => {} })

    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent?.trim())
    expect(headers).toContain('Top port')
    expect(headers).toContain('Top talker')

    const row0 = screen.getByRole('button', { name: formatHM(minute(0)) }).closest('tr')!
    const cells = rowCells(row0)
    expect(cells.at(-2)).toBe('443')
    expect(cells.at(-1)).toBe('nas')
  })

  it('shows an em dash, never a number, for a minute the ring no longer fully holds', () => {
    const tops: HourTopBucket[] = [
      { time: minute(0), talker: 'nas', port: '443', complete: true },
      { time: minute(1), complete: false },
    ]
    const hour = buildHour(traffic, [], tops)
    render(MetricsTable, { hour, cursor: -1, onselect: () => {} })

    const row1 = screen.getByRole('button', { name: formatHM(minute(1)) }).closest('tr')!
    const cells = rowCells(row1)
    expect(cells.at(-2)).toBe('—')
    expect(cells.at(-1)).toBe('—')
  })

  it('shows an em dash for a minute GET /api/stats/tops has not answered for at all', () => {
    // No tops payload yet (the poll hasn't landed) -- every minute
    // defaults to MinuteTop's unknown state, not a stale zero.
    const hour = buildHour(traffic, [])
    render(MetricsTable, { hour, cursor: -1, onselect: () => {} })

    const row0 = screen.getByRole('button', { name: formatHM(minute(0)) }).closest('tr')!
    const cells = rowCells(row0)
    expect(cells.at(-2)).toBe('—')
    expect(cells.at(-1)).toBe('—')
  })

  it('leaves the hour-total row blank rather than answering a different question', () => {
    const tops: HourTopBucket[] = [
      { time: minute(0), talker: 'nas', port: '443', complete: true },
      { time: minute(1), talker: 'phone', port: '8080', complete: true },
    ]
    const hour = buildHour(traffic, [], tops)
    const { container } = render(MetricsTable, { hour, cursor: -1, onselect: () => {} })

    const footerRow = container.querySelector('tfoot tr')!
    const cells = Array.from(footerRow.querySelectorAll('td')).map((c) => c.textContent)
    expect(cells.at(-2)).toBe('—')
    expect(cells.at(-1)).toBe('—')
  })
})
