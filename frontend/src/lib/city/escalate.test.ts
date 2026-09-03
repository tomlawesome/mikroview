// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { worstUnplanned } from './escalate'
import type { CityEdge } from './input'

function edge(over: Partial<CityEdge> = {}): CityEdge {
  return { key: 'a|b', from: 'a', to: 'b', events: 1, verdict: 'unplanned', ...over }
}

describe('worstUnplanned', () => {
  it('nothing unplanned escalates nothing', () => {
    expect(worstUnplanned([edge({ verdict: 'planned' }), edge({ verdict: 'holding' })])).toBeNull()
  })

  it('the busiest unplanned pair wins', () => {
    const quiet = edge({ key: 'a|b', events: 5 })
    const loud = edge({ key: 'c|d', events: 50 })
    expect(worstUnplanned([quiet, loud])).toBe(loud)
  })

  it('a tie on events breaks on drops', () => {
    const fewer = edge({ key: 'a|b', events: 10, drops: 2 })
    const more = edge({ key: 'c|d', events: 10, drops: 9 })
    expect(worstUnplanned([fewer, more])).toBe(more)
  })

  it('a tie on events and drops breaks on the key, so the same data always escalates the same pair', () => {
    const x = edge({ key: 'x', events: 10, drops: 3 })
    const y = edge({ key: 'y', events: 10, drops: 3 })
    expect(worstUnplanned([y, x])).toBe(x)
    expect(worstUnplanned([x, y])).toBe(x)
  })

  it('ignores planned and holding pairs even when busier', () => {
    const planned = edge({ key: 'a|b', events: 1000, verdict: 'planned' })
    const unplanned = edge({ key: 'c|d', events: 1 })
    expect(worstUnplanned([planned, unplanned])).toBe(unplanned)
  })
})
