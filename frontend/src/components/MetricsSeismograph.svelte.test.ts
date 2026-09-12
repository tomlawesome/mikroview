// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { buildHour } from '../lib/metricsSeries'
import { formatHM } from '../lib/format'
import MetricsSeismograph from './MetricsSeismograph.svelte'

// jsdom implements no ResizeObserver, and the drum measures itself with
// one (#690 -- it used to be `bind:clientWidth`, which compiled to one
// too) -- see Metrics.svelte.test.ts's own stub for why a no-op is
// enough here too (jsdom reports every box as zero-sized regardless, so
// the drum draws at its own minimum width).
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

  // #1192: the record's gutter is this view's axis and its legend --
  // name, now-value and declared scale per series -- which is why it
  // carries no numbered axis and no colour key. The drum had been
  // drawing the gutter empty.
  it('prints each series name, its now-value and its declared scale in the gutter', () => {
    const hour = buildHour(
      [
        { time: minute(0), byAction: { accept: 400, drop: 9 } },
        { time: minute(1), byAction: { accept: 90, drop: 8, reject: 2 } },
      ],
      [],
    )
    const { container } = render(MetricsSeismograph, { hour, cursor: -1, onselect: () => {} })
    const names = [...container.querySelectorAll('.g-name')].map((e) => e.textContent)
    expect(names).toEqual(['TRAFFIC', 'REFUSED'])
    const now = [...container.querySelectorAll('.g-now')].map((e) => e.textContent)
    expect(now).toEqual(['100/min', '10/min'])
    // One shared ceiling, declared on both rows: refused is a subset of
    // the minute's total, so the inner half is drawn against the total's
    // scale (the hour peaked at 409) and never one of its own.
    const scales = [...container.querySelectorAll('.g-scale')].map((e) => e.textContent)
    expect(scales).toEqual(['scale 409/min', 'scale 409/min'])
  })

  // #1192, second half: minutes that ended before this process started
  // counting are blank paper, not the stub stroke MIN_HALF gives a
  // genuine zero -- the same distinction the table's em dashes draw
  // (#1169) -- and the note says why the paper is empty there.
  it('leaves the minutes before counting began blank, with the note anchored at the first counted one', () => {
    const hour = buildHour(
      [
        { time: minute(0), byAction: {} },
        { time: minute(1), byAction: {} },
        { time: minute(2), byAction: { accept: 400 } },
        { time: minute(3), byAction: { accept: 410 } },
      ],
      [],
    )
    const { container } = render(MetricsSeismograph, {
      hour,
      cursor: -1,
      onselect: () => {},
      liveSince: minute(2),
    })
    expect(container.querySelectorAll('.stroke.outer').length).toBe(2)
    expect(container.querySelector('.note')?.textContent).toBe(
      `counting since ${formatHM(minute(2))} — nothing before`,
    )
  })

  it('draws every minute and no note when the whole hour was counted', () => {
    const hour = buildHour(
      [
        { time: minute(0), byAction: {} },
        { time: minute(1), byAction: { accept: 400 } },
      ],
      [],
    )
    const { container } = render(MetricsSeismograph, {
      hour,
      cursor: -1,
      onselect: () => {},
      liveSince: minute(0),
    })
    expect(container.querySelectorAll('.stroke.outer').length).toBe(2)
    expect(container.querySelector('.note')).toBeNull()
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
