// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  drawerEvents,
  flaggedSources,
  groupEvents,
  groupKeyOf,
  hiddenInDrawer,
  maxDrawerEvents,
} from './grouping'
import type { FirewallEvent } from './types'

let nextId = 1
function ev(over: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: nextId++,
    time: '2026-08-13T10:00:00Z',
    deviceId: 'r1',
    sourceIp: '127.0.0.1',
    action: 'drop',
    ruleLabel: 'input-def',
    chain: 'input',
    srcIp: '1.2.3.4',
    dstIp: '203.0.113.9',
    dstPort: 3389,
    protocol: 'TCP',
    ...over,
  } as FirewallEvent
}

describe('the golden group', () => {
  it('collapses exact repeats', () => {
    const groups = groupEvents([ev(), ev(), ev()])
    expect(groups).toHaveLength(1)
    expect(groups[0].count).toBe(3)
  })

  // Source, destination and port are all part of identity -- any two of
  // the three tells you little, so none of them may be merged across.
  it.each([
    ['source', { srcIp: '9.9.9.9' }],
    ['destination', { dstIp: '198.51.100.1' }],
    ['port', { dstPort: 5900 }],
    ['protocol', { protocol: 'UDP' }],
    ['action', { action: 'accept' as const }],
  ])('never merges across a different %s', (_what, diff) => {
    const groups = groupEvents([ev(), ev(diff)])
    expect(groups).toHaveLength(2)
  })

  // A port sweep is several groups, not one -- collapsing it would drop
  // the port, which is exactly what the constraint forbids.
  it('keeps a port sweep as one group per port', () => {
    const groups = groupEvents([ev({ dstPort: 3389 }), ev({ dstPort: 5900 }), ev({ dstPort: 23 })])
    expect(groups).toHaveLength(3)
  })

  it('treats a missing port as its own identity, not as a wildcard', () => {
    const groups = groupEvents([ev({ dstPort: undefined }), ev({ dstPort: 22 })])
    expect(groups).toHaveLength(2)
    expect(groupKeyOf(ev({ dstPort: undefined }))).not.toBe(groupKeyOf(ev({ dstPort: 22 })))
  })
})

describe('rule and chain are how it was handled, not what happened', () => {
  it('does not split a group when only the rule differs', () => {
    const groups = groupEvents([ev({ ruleLabel: 'input-def' }), ev({ ruleLabel: 'invalid' })])
    expect(groups).toHaveLength(1)
    expect(groups[0].count).toBe(2)
  })

  // Worth surfacing rather than hiding: the same traffic matching two
  // rules usually means a rule-ordering surprise.
  it('reports every rule it saw', () => {
    const groups = groupEvents([ev({ ruleLabel: 'a' }), ev({ ruleLabel: 'b' }), ev({ ruleLabel: 'a' })])
    expect(groups[0].rules).toEqual(['a', 'b'])
  })
})

describe('ordering', () => {
  // The count climbs in place; the row does not jump down the screen
  // every time it is hit again.
  it('anchors a group at its first occurrence', () => {
    const first = ev({ srcIp: '1.1.1.1' })
    const second = ev({ srcIp: '2.2.2.2' })
    const groups = groupEvents([first, second, ev({ srcIp: '1.1.1.1' })])
    expect(groups.map((g) => g.head.srcIp)).toEqual(['1.1.1.1', '2.2.2.2'])
    expect(groups[0].count).toBe(2)
  })

  it('accounts for every event it was given', () => {
    const events = [ev(), ev({ dstPort: 22 }), ev(), ev({ srcIp: '5.5.5.5' }), ev({ dstPort: 22 })]
    const total = groupEvents(events).reduce((n, g) => n + g.count, 0)
    expect(total).toBe(events.length)
  })
})

describe('the drawer', () => {
  it('shows the most recent members, newest first', () => {
    const events = Array.from({ length: 5 }, (_, i) => ev({ id: 100 + i }))
    const [group] = groupEvents(events)
    expect(drawerEvents(group).map((e) => e.id)).toEqual([104, 103, 102, 101, 100])
  })

  it('caps what it renders, and says how many it is not showing', () => {
    const events = Array.from({ length: 400 }, () => ev())
    const [group] = groupEvents(events)
    expect(drawerEvents(group)).toHaveLength(maxDrawerEvents)
    expect(hiddenInDrawer(group)).toBe(400 - maxDrawerEvents)
  })

  it('hides nothing when the group fits', () => {
    const [group] = groupEvents([ev(), ev()])
    expect(hiddenInDrawer(group)).toBe(0)
  })
})

describe('the flag marker', () => {
  it('marks sources with an active flag', () => {
    const set = flaggedSources([
      { target: '1.2.3.4', cleared: false },
      { target: '9.9.9.9', cleared: true },
    ])
    expect(set.has('1.2.3.4')).toBe(true)
    expect(set.has('9.9.9.9')).toBe(false)
  })

  it('understands a target carrying a port suffix', () => {
    expect(flaggedSources([{ target: '1.2.3.4 -> port 22', cleared: false }]).has('1.2.3.4')).toBe(true)
  })
})
