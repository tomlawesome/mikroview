// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { buildHour, FLAG_TYPE_SHORT_LABELS } from '../lib/metricsSeries'
import MetricsRegister from './MetricsRegister.svelte'

// jsdom implements no ResizeObserver, and `bind:clientWidth` compiles to
// one -- see Metrics.svelte.test.ts's own stub for why a no-op is enough
// here too (jsdom reports every box as zero-sized regardless, so the
// register draws at its own minimum width).
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

// The same estimate MetricsRegister.svelte itself uses to size the
// header and the flag columns -- duplicated here (not imported) so the
// test pins the *intended relationship* between the constants rather
// than just re-running the component's own arithmetic against itself.
const FLAG_LABEL_CHAR_W = 6
const FLAG_ROTATE_RAD = (60 * Math.PI) / 180
const LONGEST_FLAG_LABEL_LEN = Math.max(...Object.values(FLAG_TYPE_SHORT_LABELS).map((s) => s.length))
const FLAG_LABEL_DROP = Math.sin(FLAG_ROTATE_RAD) * LONGEST_FLAG_LABEL_LEN * FLAG_LABEL_CHAR_W
const FLAG_LABEL_REACH = Math.cos(FLAG_ROTATE_RAD) * LONGEST_FLAG_LABEL_LEN * FLAG_LABEL_CHAR_W

// #716 reverses 778203f: the owner ruled the flag-type labels should be
// diagonal again, just placed so nothing collides. These tests pin that
// reversal so a future edit can't silently re-flatten the labels, or
// restore the rotation without also fixing the position.
describe('MetricsRegister flag-type labels', () => {
  it('rotates a fired flag-type label -60deg, anchored at its end', () => {
    const hour = buildHour(
      [{ time: minute(0), byAction: { accept: 400 } }],
      [{ time: minute(0), byType: { activity_spike: 3 } }],
    )
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    const label = container.querySelector('.f-name')
    expect(label).not.toBeNull()
    expect(label?.getAttribute('text-anchor')).toBe('end')
    expect(label?.getAttribute('transform')).toMatch(/^rotate\(-60 /)
  })

  it("keeps the longest flag-type label's rotated tail clear of the brink/axis line", () => {
    const hour = buildHour(
      [{ time: minute(0), byAction: { accept: 400 } }],
      [{ time: minute(0), byType: { activity_spike: 3 } }],
    )
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    const label = container.querySelector('.f-name')
    const axisLine = container.querySelector('.axis')
    const anchorY = Number(label?.getAttribute('y'))
    const axisY = Number(axisLine?.getAttribute('y1'))
    // The axis/brink line is the same y for every column (it's `HEADER`);
    // the rotated label's tail -- anchorY + the downward sweep -- must
    // stay above it, not sweep across it the way 778203f's predecessor did.
    expect(anchorY + FLAG_LABEL_DROP).toBeLessThan(axisY)
  })

  it('spaces adjacent fired flag columns wide enough that their rotated labels do not overlap', () => {
    const hour = buildHour(
      [{ time: minute(0), byAction: { accept: 400 } }],
      [{ time: minute(0), byType: { activity_spike: 3, internal_recon: 2 } }],
    )
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    const labels = Array.from(container.querySelectorAll('.f-name'))
    expect(labels.length).toBe(2)
    const [x0, x1] = labels.map((l) => Number(l.getAttribute('x')))
    expect(Math.abs(x1 - x0)).toBeGreaterThan(FLAG_LABEL_REACH)
  })

  it('draws no rotated label at all when no flag type fired this hour', () => {
    const hour = buildHour([{ time: minute(0), byAction: { accept: 400 } }], [])
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    expect(container.querySelector('.f-name')).toBeNull()
  })
})

// #1155: the ×N annotations beside the ticks are 9px type on rows 8px
// apart, so a run of busy minutes printed them on top of each other.
describe('MetricsRegister episode-count annotations', () => {
  // The annotation's own line height -- MetricsRegister's TICK_N_GAP,
  // named here rather than imported so the test pins the claim (they do
  // not overlap) instead of re-running the component's arithmetic.
  const LINE_H = 11

  it('spreads the ×N figures of a run of busy minutes so none overlaps its neighbour', () => {
    const hour = buildHour(
      [0, 1, 2, 3].map((m) => ({ time: minute(m), byAction: { accept: 400 } })),
      [
        { time: minute(0), byType: { activity_spike: 6 } },
        { time: minute(1), byType: { activity_spike: 5 } },
        { time: minute(2), byType: { activity_spike: 4 } },
        { time: minute(3), byType: { activity_spike: 3 } },
      ],
    )
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    const labels = Array.from(container.querySelectorAll('.tick-n'))
    // Every minute still prints its own figure: spreading them, not
    // dropping the ones that would not fit.
    expect(labels.map((l) => l.textContent)).toEqual(['×3', '×4', '×5', '×6'])
    const ys = labels.map((l) => Number(l.getAttribute('y')))
    for (let i = 1; i < ys.length; i++) {
      expect(ys[i] - ys[i - 1]).toBeGreaterThanOrEqual(LINE_H)
    }
  })

  it('leaves a lone ×N on its own tick rather than nudging it', () => {
    const hour = buildHour(
      [0, 1].map((m) => ({ time: minute(m), byAction: { accept: 400 } })),
      [{ time: minute(0), byType: { activity_spike: 4 } }],
    )
    const { container } = render(MetricsRegister, { hour, cursor: -1, onselect: () => {} })
    const label = container.querySelector('.tick-n')
    const tick = container.querySelector('.tick')
    expect(label?.textContent).toBe('×4')
    // The text sits on the tick's baseline offset (+3), nowhere else --
    // within the half-pixel by which snapFill (text) and snapLine (the
    // tick) differ from each other.
    expect(Math.abs(Number(label?.getAttribute('y')) - (Number(tick?.getAttribute('y1')) + 3))).toBeLessThanOrEqual(0.5)
  })
})
