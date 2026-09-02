// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { provenanceFor, towardFor, watchDraftForFlag } from './watchDraft'
import type { Flag, HostPort } from './types'

function flag(overrides: Partial<Flag> = {}): Flag {
  return {
    id: 'f1',
    type: 'internal_recon',
    target: '192.168.1.50',
    detail: '12 distinct internal destinations in 60s',
    count: 1,
    firstSeen: '2026-09-02T09:00:00Z',
    lastSeen: '2026-09-02T09:01:00Z',
    cleared: true,
    verdict: 'resolved',
    ...overrides,
  }
}

const pair = (host: string, port: number): HostPort => ({ host, port })

describe('watchDraftForFlag (#641)', () => {
  it('fills the draft from the flag: the host, the pairs seen, and where to go back to', () => {
    const draft = watchDraftForFlag(
      flag({ evidence: { pairs: [pair('192.168.1.10', 445), pair('192.168.1.10', 139)], pairsTotal: 2 } }),
    )
    expect(draft).toEqual({
      who: '192.168.1.50',
      toward: '192.168.1.10 · :139, :445',
      mode: 'expect',
      provenance: expect.stringContaining('2 pairs'),
      returnTo: 'flags',
    })
  })

  it('scopes the watch by MAC where the evidence carries one', () => {
    const draft = watchDraftForFlag(
      flag({ evidence: { pairs: [pair('192.168.1.10', 445)], srcMac: '52:55:0a:00:02:02' } }),
    )
    expect(draft?.who).toBe('52:55:0a:00:02:02')
    expect(draft?.provenance).toContain('MAC-bound')
  })

  it('falls back to the target IP, and says the watch is IP-bound', () => {
    const draft = watchDraftForFlag(flag({ evidence: { pairs: [pair('192.168.1.10', 445)] } }))
    expect(draft?.who).toBe('192.168.1.50')
    expect(draft?.provenance).toContain('IP-bound')
  })

  // Nothing honest to prefill: no offer. A flag whose detector records no
  // pairs has nothing to watch *for*, and a flag whose target is a rule
  // label or a bare port names no host to watch.
  it('declines a flag with no evidence pairs', () => {
    expect(watchDraftForFlag(flag({ evidence: { ports: [22, 23], hosts: ['192.168.1.10'] } }))).toBeNull()
    expect(watchDraftForFlag(flag({ evidence: undefined }))).toBeNull()
  })

  it('declines a flag whose target names no host', () => {
    expect(
      watchDraftForFlag(flag({ type: 'distributed_brute_force', target: 'port 445', evidence: { pairs: [pair('192.168.1.10', 445)] } })),
    ).toBeNull()
    expect(watchDraftForFlag(flag({ type: 'global_spike', target: 'global', evidence: { pairs: [pair('1.2.3.4', 443)] } }))).toBeNull()
  })
})

describe('towardFor', () => {
  it('names the destination when every pair shares one, with its ports', () => {
    expect(towardFor([pair('192.168.1.10', 445), pair('192.168.1.10', 139)])).toBe('192.168.1.10 · :139, :445')
  })

  // A watchlist entry scopes one destination. Naming only the first of
  // several would quietly drop the rest -- a tripwire that misses most of
  // what it was drafted from -- so the ports are watched toward any
  // destination instead: broader than the pairs, never narrower.
  it('watches the ports toward any destination when the pairs name several', () => {
    expect(towardFor([pair('192.168.1.10', 445), pair('192.168.1.11', 445), pair('192.168.1.12', 139)])).toBe(
      '· :139, :445',
    )
  })
})

describe('provenanceFor', () => {
  it('states the firing window and an exact count when nothing was truncated', () => {
    const pairs = [pair('192.168.1.10', 445)]
    expect(provenanceFor(flag({ evidence: { pairs, pairsTotal: 1 } }), pairs, false)).toBe(
      'From the last firing window, 1 pair — IP-bound, so it stops matching if this device gets a new address.',
    )
  })

  it('says how many of how many when the list is a sample', () => {
    const pairs = [pair('a', 1), pair('b', 2), pair('c', 3), pair('d', 4), pair('e', 5), pair('f', 6)]
    const text = provenanceFor(flag({ evidence: { pairs, pairsTotal: 14 } }), pairs, false)
    expect(text).toContain('6 of 14 pairs')
  })

  // Past internal/engine's maxEvidencePairsTracked the total stops being
  // exact, and a flat "6 of 14" would read as precise while not being
  // (#654's own rule, in this sentence's register).
  it('reads a floored total as "at least"', () => {
    const pairs = [pair('a', 1), pair('b', 2), pair('c', 3), pair('d', 4), pair('e', 5), pair('f', 6)]
    const text = provenanceFor(flag({ evidence: { pairs, pairsTotal: 14, pairsTotalIsFloor: true } }), pairs, false)
    expect(text).toContain('6 of at least 14 pairs')
  })

  it('says when the ports are being watched toward any destination', () => {
    const pairs = [pair('192.168.1.10', 445), pair('192.168.1.11', 445)]
    const text = provenanceFor(flag({ evidence: { pairs } }), pairs, false)
    expect(text).toContain('2 destinations')
    expect(text).toContain('toward any destination')
  })
})
