// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { buildHour } from '../lib/metricsSeries'
import { formatHM } from '../lib/format'
import MetricsSeismograph from './MetricsSeismograph.svelte'

// jsdom implements no ResizeObserver, and `bind:clientWidth` compiles to
// one -- see Metrics.svelte.test.ts's own stub for why a no-op is enough
// here too (jsdom reports every box as zero-sized regardless, so the
// drum draws at its own minimum width).
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

// #634 round-13 verdict: the drum draws one mirrored stroke per minute
// -- an outer half for every event, an inner half for its refused
// share -- superseding the per-action horizon lanes this component used
// to draw. Pinned here so a future edit cannot silently reintroduce a
// per-series lane without the test noticing the stroke count is wrong.
describe('MetricsSeismograph', () => {
  it('says the drum has not started yet when the hour is empty', () => {
    const hour = buildHour([], [])
    render(MetricsSeismograph, { hour, cursor: -1, onselect: () => {} })
    expect(screen.getByText(/drum starts as soon as events arrive/)).toBeTruthy()
  })

  it('draws one mirrored outer/inner stroke pair per axis minute', () => {
    const hour = buildHour(
      [
        { time: minute(0), byAction: { accept: 400, drop: 9 } },
        { time: minute(1), byAction: { accept: 410, drop: 88, reject: 2 } },
        { time: minute(2), byAction: { accept: 421, drop: 12 } },
      ],
      [],
    )
    const { container } = render(MetricsSeismograph, { hour, cursor: -1, onselect: () => {} })
    expect(container.querySelectorAll('.stroke.outer').length).toBe(hour.axis.length)
    expect(container.querySelectorAll('.stroke.inner').length).toBe(hour.axis.length)
  })

  // #644: the ratified drum (round 13, and every round 20-29 mockup of
  // it) carries no per-flag-type panel -- flag detail lives only in the
  // register's flag columns and the table's flag-episodes column. This
  // file used to keep a FLAG EPISODES row per detector type left over
  // from the pre-#644 build; pinned here so it cannot silently return.
  it('draws no per-flag-type rows even when flag episodes were raised', () => {
    const hour = buildHour(
      [{ time: minute(0), byAction: { accept: 400, drop: 9 } }],
      [{ time: minute(0), byType: { port_scan: 2, new_device: 1 } }],
    )
    const { container } = render(MetricsSeismograph, { hour, cursor: -1, onselect: () => {} })
    expect(container.querySelector('.f-name')).toBeNull()
    expect(container.querySelector('.tick')).toBeNull()
    expect(screen.queryByText('FLAG EPISODES')).toBeNull()
  })

  it("does not grow the drum's height with the number of flag types", () => {
    const noFlags = buildHour([{ time: minute(0), byAction: { accept: 400 } }], [])
    const withFlags = buildHour(
      [{ time: minute(0), byAction: { accept: 400 } }],
      [{ time: minute(0), byType: { port_scan: 2 } }],
    )
    expect(withFlags.flags.length).toBeGreaterThan(0)
    const { container: a } = render(MetricsSeismograph, { hour: noFlags, cursor: -1, onselect: () => {} })
    const { container: b } = render(MetricsSeismograph, { hour: withFlags, cursor: -1, onselect: () => {} })
    expect(a.querySelector('svg')?.getAttribute('height')).toBe(b.querySelector('svg')?.getAttribute('height'))
  })

  it('draws the cursor only once a minute is selected', () => {
    const hour = buildHour(
      [
        { time: minute(0), byAction: { accept: 400 } },
        { time: minute(1), byAction: { accept: 410 } },
      ],
      [],
    )
    const { container: noCursor } = render(MetricsSeismograph, { hour, cursor: -1, onselect: () => {} })
    expect(noCursor.querySelector('.cursor')).toBeNull()

    const { container: withCursor } = render(MetricsSeismograph, { hour, cursor: 1, onselect: () => {} })
    expect(withCursor.querySelector('.cursor')).not.toBeNull()
    expect(withCursor.querySelector('.time.cursor-label')?.textContent).toBe(formatHM(minute(1)))
  })
})
