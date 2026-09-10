// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import type { RouterFilterRule, RouterNatRule } from './api'
import type { Device } from './types'

// FallState.refresh() (below) fetches devices, both rule tables and the
// watchlist entries (#806) -- mocked so the refresh test does not reach
// the network. The pure boundariesFromRules/boundaryKeyOf/
// brokenWatchesByKey tests below never call refresh(), so this mock does
// not touch them.
vi.mock('./api', () => ({
  fetchDevices: vi.fn(),
  fetchRouterRules: vi.fn(),
  fetchRouterNat: vi.fn(),
  fetchWatchlistEntries: vi.fn(),
}))

import { fetchDevices, fetchRouterNat, fetchRouterRules, fetchWatchlistEntries } from './api'
import { boundariesFromRules, boundaryKeyOf, brokenWatchesByKey, fallState } from './fall.svelte'
import type { WatchlistEntry } from './types'

function rule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
  return {
    ordinal: 0,
    comment: '',
    chain: 'forward',
    action: 'drop',
    srcAddressList: '',
    logPrefix: '',
    log: false,
    ...over,
  }
}

function natRule(over: Partial<RouterNatRule> = {}): RouterNatRule {
  return {
    ordinal: 0,
    comment: '',
    chain: 'srcnat',
    action: 'masquerade',
    ...over,
  }
}

describe('FallState.refresh (#695)', () => {
  it('feeds pushed NAT rules into the boundaries it builds, not just filter rules', async () => {
    vi.mocked(fetchDevices).mockResolvedValue([{ id: 'router1' } as Device])
    vi.mocked(fetchRouterRules).mockResolvedValue({ available: true, rules: [] })
    vi.mocked(fetchRouterNat).mockResolvedValue({
      available: true,
      rules: [natRule({ chain: 'srcnat', outInterface: 'ether1' })],
    })
    vi.mocked(fetchWatchlistEntries).mockResolvedValue({ entries: [], coverage: {} })
    await fallState.refresh()
    // Before #695's fix, refresh() never called fetchRouterNat at all,
    // so a pushed masquerade rule produced no boundary and every live
    // NAT event fell through to __unmatched__ regardless.
    expect(fallState.boundaries.map((b) => b.key)).toContain(boundaryKeyOf('srcnat', undefined, 'ether1'))
  })
})

describe('FallState.refresh loads watchlist entries alongside rules (#806)', () => {
  it('is populated on entries, non-fatally if the watchlist read fails', async () => {
    vi.mocked(fetchDevices).mockResolvedValue([])
    vi.mocked(fetchRouterRules).mockResolvedValue({ available: true, rules: [] })
    vi.mocked(fetchRouterNat).mockResolvedValue({ available: true, rules: [] })
    vi.mocked(fetchWatchlistEntries).mockRejectedValue(new Error('boom'))
    await fallState.refresh()
    // A watchlist read failing must never take the fall's own boundaries
    // down with it -- the same non-fatal contract fetchRouterRules/
    // fetchRouterNat already have above.
    expect(fallState.error).toBeNull()
    expect(fallState.entries).toEqual([])
  })
})

describe('boundaryKeyOf', () => {
  it('treats an absent interface as the empty string, not undefined', () => {
    expect(boundaryKeyOf('forward', undefined, 'bridge1')).toBe('forward||bridge1')
    expect(boundaryKeyOf('forward', '', 'bridge1')).toBe('forward||bridge1')
  })
})

describe('boundariesFromRules', () => {
  it('reports unknown, not dark, when no device has pushed anything', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1' })], false)
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('unknown')
  })

  it('is dark only when rules were pushed, cover this boundary, and none of them log', () => {
    const bands = boundariesFromRules(
      [rule({ inInterface: 'ether9', outInterface: 'bridge9', log: false })],
      true,
    )
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('dark')
  })

  it('is observed as soon as one matching rule logs, even if others on the same boundary do not', () => {
    const bands = boundariesFromRules(
      [
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: false, comment: 'silent variant' }),
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true, comment: 'logging variant' }),
      ],
      true,
    )
    expect(bands).toHaveLength(1)
    expect(bands[0].coverage).toBe('observed')
  })

  it('never claims dark for a boundary no pushed rule actually references', () => {
    // Only ether1<->bridge1 was pushed; nothing says anything about
    // ether9<->bridge9 -- boundariesFromRules should not fabricate an
    // entry for it at all (the honesty rule: only ever a definite
    // answer, and only about a boundary it has actual evidence for).
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true })], true)
    expect(bands.map((b) => b.key)).not.toContain(boundaryKeyOf('forward', 'ether9', 'bridge9'))
  })

  it('labels a boundary with both interfaces as "in -> out"', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge1' })], true)
    expect(bands[0].label).toBe('ether1 → bridge1')
  })

  it('sorts alphabetically by label, deterministic across calls', () => {
    const bands = boundariesFromRules(
      [
        rule({ inInterface: 'wan1', outInterface: 'bridge1', log: true }),
        rule({ inInterface: 'ether1', outInterface: 'bridge1', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['ether1 → bridge1', 'wan1 → bridge1'])
  })

  it('groups distinct chains for the same interface pair as distinct boundaries', () => {
    const bands = boundariesFromRules(
      [
        rule({ chain: 'forward', inInterface: 'ether1', outInterface: 'bridge1', log: true }),
        rule({ chain: 'input', inInterface: 'ether1', outInterface: '', log: false }),
      ],
      true,
    )
    expect(bands).toHaveLength(2)
  })

  // Amendment (a), Fable's 2026-08-29 review: a pushed rule's own
  // srcAddressList is real evidence of an operator-named group, and
  // replaces the interface name on that side of the label.
  it('names the source side from srcAddressList when a pushed rule carries one', () => {
    const bands = boundariesFromRules(
      [rule({ inInterface: 'ether1', outInterface: 'bridge9', srcAddressList: 'lan', log: true })],
      true,
    )
    expect(bands[0].label).toBe('lan → bridge9')
    expect(bands[0].srcAddressList).toBe('lan')
    // The raw interface is kept alongside, not discarded.
    expect(bands[0].inInterface).toBe('ether1')
  })

  it('falls back to the interface name when no rule on this boundary names an address list', () => {
    const bands = boundariesFromRules([rule({ inInterface: 'ether1', outInterface: 'bridge9' })], true)
    expect(bands[0].label).toBe('ether1 → bridge9')
    expect(bands[0].srcAddressList).toBe('')
  })

  // Amendment (b): semantic ordering -- input-chain/WAN-facing first,
  // observed forward next, dark/unknown last, alphabetical within class.
  it('orders input-chain bands before observed forward bands, and dark bands last', () => {
    const bands = boundariesFromRules(
      [
        // forward, dark (no log) -- last class.
        rule({ chain: 'forward', inInterface: 'z-dark', outInterface: 'bridge1', log: false }),
        // forward, observed -- middle class.
        rule({ chain: 'forward', inInterface: 'a-observed', outInterface: 'bridge1', log: true }),
        // input chain -- first class, regardless of its own coverage.
        rule({ chain: 'input', inInterface: 'zzz-wan', outInterface: '', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['zzz-wan · input', 'a-observed → bridge1', 'z-dark → bridge1'])
  })

  // #695: NAT rules never reached this grouping at all, so every NAT
  // event fell through to __unmatched__ regardless of what was pushed.
  // A NAT rule carries chain/inInterface/outInterface exactly like a
  // filter rule, so it must key and band the same way -- no second key
  // scheme (the decision on #695).
  it('bands a pushed srcnat rule under the same key a live NAT event resolves to', () => {
    const bands = boundariesFromRules([natRule({ chain: 'srcnat', outInterface: 'ether1' })], true)
    expect(bands).toHaveLength(1)
    expect(bands[0].key).toBe('srcnat||ether1')
    // The key a live srcnat/masquerade event on this interface would
    // resolve to (Fall.svelte buckets events by this same call) --
    // matching, not falling through to __unmatched__.
    expect(boundaryKeyOf('srcnat', undefined, 'ether1')).toBe(bands[0].key)
  })

  it('keeps alphabetical order within a class', () => {
    const bands = boundariesFromRules(
      [
        rule({ chain: 'input', inInterface: 'z-wan', outInterface: '', log: true }),
        rule({ chain: 'input', inInterface: 'a-wan', outInterface: '', log: true }),
      ],
      true,
    )
    expect(bands.map((b) => b.label)).toEqual(['a-wan · input', 'z-wan · input'])
  })
})

function entry(over: Partial<WatchlistEntry> = {}): WatchlistEntry {
  return {
    id: 'e1',
    name: 'cam-porch quiet hours',
    enabled: true,
    createdAt: '',
    ...over,
  }
}

// #806: the fall's own join of watchlist entries onto the boundary they
// name. See fall.svelte.ts's own doc comment for why an unscoped or
// disabled entry, or one whose ring holds, makes no appearance -- each
// case here pins one of those refusals plus the one case that should
// actually produce a claim.
describe('brokenWatchesByKey (#806)', () => {
  it('keys a scoped, enabled, ring-broken entry by boundaryKeyOf', () => {
    const e = entry({ boundary: { chain: 'forward', inInterface: 'iot', outInterface: 'ether1' }, ring: { broken: true } })
    const m = brokenWatchesByKey([e])
    expect(m.get(boundaryKeyOf('forward', 'iot', 'ether1'))).toEqual([e])
  })

  it('makes no per-band claim for an entry with no boundary at all', () => {
    const e = entry({ ring: { broken: true } })
    const m = brokenWatchesByKey([e])
    expect(m.size).toBe(0)
  })

  it('never places a watcher whose ring is intact', () => {
    const e = entry({ boundary: { chain: 'forward', inInterface: 'iot', outInterface: 'ether1' } })
    const m = brokenWatchesByKey([e])
    expect(m.size).toBe(0)
  })

  it('never places a paused entry, even a scoped, ring-broken one', () => {
    const e = entry({
      enabled: false,
      boundary: { chain: 'forward', inInterface: 'iot', outInterface: 'ether1' },
      ring: { broken: true },
    })
    const m = brokenWatchesByKey([e])
    expect(m.size).toBe(0)
  })

  it('groups more than one broken watcher naming the same boundary', () => {
    const boundary = { chain: 'forward', inInterface: 'iot', outInterface: 'ether1' }
    const a = entry({ id: 'a', boundary, ring: { broken: true } })
    const b = entry({ id: 'b', boundary, ring: { broken: true } })
    const m = brokenWatchesByKey([a, b])
    expect(m.get(boundaryKeyOf('forward', 'iot', 'ether1'))).toEqual([a, b])
  })
})
