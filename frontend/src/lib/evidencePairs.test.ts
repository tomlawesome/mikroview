// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { groupPairsByHost, pairsTruncated, pairsTruncationLabel } from './evidencePairs'
import type { HostPort } from './types'

describe('groupPairsByHost', () => {
  it('groups ports under the host they were actually seen with', () => {
    const pairs: HostPort[] = [
      { host: '192.168.1.10', port: 22 },
      { host: '192.168.1.11', port: 23 },
    ]
    expect(groupPairsByHost(pairs)).toEqual([
      { host: '192.168.1.10', ports: [22] },
      { host: '192.168.1.11', ports: [23] },
    ])
  })

  it('never implies a combination that was never seen -- .10 never paired with 23, .11 never paired with 22', () => {
    const pairs: HostPort[] = [
      { host: '192.168.1.10', port: 22 },
      { host: '192.168.1.11', port: 23 },
    ]
    const groups = groupPairsByHost(pairs)
    const host10 = groups.find((g) => g.host === '192.168.1.10')
    const host11 = groups.find((g) => g.host === '192.168.1.11')
    expect(host10?.ports).not.toContain(23)
    expect(host11?.ports).not.toContain(22)
  })

  it('collects every port seen with the same host into one group', () => {
    const pairs: HostPort[] = [
      { host: '192.168.1.10', port: 443 },
      { host: '192.168.1.10', port: 22 },
      { host: '192.168.1.10', port: 22 }, // duplicate
    ]
    expect(groupPairsByHost(pairs)).toEqual([{ host: '192.168.1.10', ports: [22, 443] }])
  })

  it('sorts groups by host and each group’s ports ascending, regardless of input order', () => {
    const pairs: HostPort[] = [
      { host: '192.168.1.11', port: 80 },
      { host: '192.168.1.10', port: 443 },
      { host: '192.168.1.10', port: 22 },
    ]
    expect(groupPairsByHost(pairs)).toEqual([
      { host: '192.168.1.10', ports: [22, 443] },
      { host: '192.168.1.11', ports: [80] },
    ])
  })

  it('an empty list groups to an empty list', () => {
    expect(groupPairsByHost([])).toEqual([])
  })
})

describe('pairsTruncated', () => {
  it('is false when pairsTotal is absent -- the pairs list is the complete set', () => {
    expect(pairsTruncated([{ host: 'a', port: 1 }], undefined)).toBe(false)
  })

  it('is false when pairsTotal equals the list length', () => {
    expect(pairsTruncated([{ host: 'a', port: 1 }], 1)).toBe(false)
  })

  it('is true when pairsTotal exceeds the list length -- the cap actually truncated it', () => {
    expect(pairsTruncated(new Array(50).fill({ host: 'a', port: 1 }), 214)).toBe(true)
  })

  it('is false for an absent pairs list even with a pairsTotal', () => {
    expect(pairsTruncated(undefined, 0)).toBe(false)
  })
})

// pairsTruncationLabel's two cases -- exact vs. "at least" -- must read
// visibly differently (#654's owner correction): a flat "50 of 200"
// looks exactly as precise as a genuine "50 of 214" while lying about
// it once pairsTotal is itself only a floor (internal/engine's
// maxEvidencePairsTracked ceiling). These two tests pin that the two
// cases are in fact distinct outputs, not just distinct inputs.
describe('pairsTruncationLabel', () => {
  it('renders the exact case with no suffix', () => {
    expect(pairsTruncationLabel(50, 214, false)).toBe('50 of 214')
  })

  it('renders the floor case with a "+" suffix, distinctly from the exact case', () => {
    expect(pairsTruncationLabel(50, 200, true)).toBe('50 of 200+')
  })

  it('treats an absent pairsTotalIsFloor the same as false (the exact case)', () => {
    expect(pairsTruncationLabel(50, 214, undefined)).toBe('50 of 214')
  })
})
