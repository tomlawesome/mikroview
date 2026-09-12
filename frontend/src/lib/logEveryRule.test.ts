// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  countFilterRules,
  counterText,
  darkBoundaryKeys,
  exportProblem,
  groupRules,
  initialSelection,
  waitingMessage,
} from './logEveryRule'
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

describe('countFilterRules (#1134)', () => {
  // A real export's shape in miniature: `add` lines under
  // /ip firewall filter, one of them wrapped across a continuation
  // line, and `add` lines in other sections that must not be counted.
  const fixture = [
    '# 2026/09/01 10:00:00 by RouterOS 7.24.1',
    '/interface bridge',
    'add name=bridge1',
    '',
    '/ip firewall filter',
    'add action=accept chain=input comment="allow established"',
    'add action=accept chain=forward comment="lan to wan" \\',
    '    in-interface=bridge1 out-interface=ether1',
    'add action=drop chain=forward in-interface=ether1',
    '',
    '/ip firewall nat',
    'add action=masquerade chain=srcnat out-interface=ether1',
  ].join('\n')

  it('counts the add lines in the filter section and nothing else', () => {
    expect(countFilterRules(fixture)).toBe(3)
  })

  it('counts a slash-joined section header the same way', () => {
    expect(countFilterRules('/ip/firewall/filter\nadd action=drop chain=forward\n')).toBe(1)
  })

  it('is zero for an export with no filter section, and for nothing at all', () => {
    expect(countFilterRules('/ip firewall nat\nadd action=masquerade chain=srcnat\n')).toBe(0)
    expect(countFilterRules('')).toBe(0)
  })

  // #1186: junk pasted into the page reached Analyse and came back as
  // "watching for 0 hours", which reads as "nothing yet" rather than
  // "that was not an export" -- two states the operator has to be able
  // to tell apart.
  describe('exportProblem (#1186)', () => {
    it('says nothing about a real export', () => {
      expect(exportProblem(fixture)).toBeNull()
      expect(exportProblem('/ip/firewall/filter\nadd action=drop chain=forward\n')).toBeNull()
    })

    it('says nothing about an empty box, which is not a fault', () => {
      expect(exportProblem('')).toBeNull()
      expect(exportProblem('   \n\n')).toBeNull()
    })

    it('names text that is not an export at all', () => {
      expect(exportProblem('the quick brown fox')).toContain('no /ip firewall filter section')
    })

    it('tells an export with no filter rules apart from text that is not one', () => {
      const empty = '/ip firewall filter\n\n/ip firewall nat\nadd action=masquerade chain=srcnat\n'
      expect(exportProblem(empty)).toContain('no rules in it')
    })
  })
})
