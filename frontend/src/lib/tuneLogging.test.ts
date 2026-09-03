// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { counterText, darkBoundaryKeys, groupRules, initialSelection, waitingMessage } from './tuneLogging'
import type { PolicyEdge } from './policy.svelte'
import type { TuneLoggingRule } from './types'

function edge(over: Partial<PolicyEdge> = {}): PolicyEdge {
  return {
    key: 'bridge|ether1',
    from: 'bridge',
    to: 'ether1',
    accepted: true,
    refused: false,
    acceptPorts: [],
    refusePorts: [],
    comment: '',
    ruleCount: 1,
    logged: false,
    ...over,
  }
}

function rule(over: Partial<TuneLoggingRule> = {}): TuneLoggingRule {
  return {
    id: 3,
    chain: 'forward',
    action: 'accept',
    comment: 'lan to wan',
    inInterface: 'bridge',
    outInterface: 'ether1',
    inInterfaceList: '',
    outInterfaceList: '',
    boundary: 'bridge|ether1',
    crossesDark: true,
    log: false,
    logPrefix: '',
    packets: 41230,
    bytes: 8817212,
    countersKnown: true,
    line: 41,
    ...over,
  }
}

describe('darkBoundaryKeys', () => {
  it('is dark when neither logged nor declared quiet', () => {
    const edges = [edge({ key: 'a|b', logged: false }), edge({ key: 'c|d', logged: true })]
    expect(darkBoundaryKeys(edges, new Set())).toEqual(['a|b'])
  })

  it('excludes a boundary declared intentionally quiet', () => {
    const edges = [edge({ key: 'a|b', logged: false })]
    expect(darkBoundaryKeys(edges, new Set(['a|b']))).toEqual([])
  })
})

describe('waitingMessage', () => {
  it('states hours watched and when suggestions arrive', () => {
    expect(waitingMessage(9)).toBe('Watching for 9 hours; suggestions arrive at 24 hours.')
  })

  it('singularises one hour', () => {
    expect(waitingMessage(1)).toBe('Watching for 1 hour; suggestions arrive at 24 hours.')
  })

  it('floors a fractional hour rather than rounding up past what has really elapsed', () => {
    expect(waitingMessage(3.9)).toBe('Watching for 3 hours; suggestions arrive at 24 hours.')
  })
})

describe('initialSelection', () => {
  it('ticks every rule that crosses a dark connection, and none other', () => {
    const rules = [rule({ id: 1, crossesDark: true }), rule({ id: 2, crossesDark: false }), rule({ id: 3, crossesDark: true })]
    expect(initialSelection(rules)).toEqual(new Set([1, 3]))
  })
})

describe('groupRules', () => {
  it('splits dark-crossing rules from the rest, collapsed', () => {
    const dark = rule({ id: 1, crossesDark: true })
    const other = rule({ id: 2, crossesDark: false })
    expect(groupRules([dark, other])).toEqual({ dark: [dark], other: [other] })
  })
})

describe('counterText', () => {
  it('renders the fired-N-times / M-bytes line only when counters are known', () => {
    const r = rule({ packets: 41230, bytes: 8817212, countersKnown: true })
    expect(counterText(r, '2026-09-01T10:00:00Z')).toBe(
      `fired 41,230 times / 8,817,212 bytes since ${new Date('2026-09-01T10:00:00Z').toLocaleString()}`,
    )
  })

  it('is null when the push could not be matched to this rule', () => {
    const r = rule({ countersKnown: false })
    expect(counterText(r, '2026-09-01T10:00:00Z')).toBeNull()
  })

  it('singularises one fired time', () => {
    const r = rule({ packets: 1, countersKnown: true })
    expect(counterText(r, '2026-09-01T10:00:00Z')).toContain('fired 1 time /')
  })
})
