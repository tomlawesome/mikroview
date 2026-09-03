// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Flag, FlagType, Verdict } from './types'

// flagsState only ever reaches the network through fetchFlags/setFlagVerdict
// (see api.ts) -- groupedBySource and extractSourceIp are pure logic
// over whatever's already sitting in .list, so no network mocking is
// needed here, unlike auth.svelte.test.ts. The judge*/undoVerdict tests
// below (#638) are the exception: they exercise setFlagVerdict/
// deleteFlagVerdict directly, so those two functions are mocked rather
// than the whole module, keeping fetchFlags/setFlagVerdict etc. as the real
// implementations for every other describe block in this file.
vi.mock('./api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./api')>()
  return { ...actual, setFlagVerdict: vi.fn(), deleteFlagVerdict: vi.fn() }
})

import { deleteFlagVerdict, setFlagVerdict } from './api'
import { extractSourceIp, flagsState } from './flags.svelte'

let nextId = 1

function flag(type: FlagType, target: string, overrides: Partial<Flag> = {}): Flag {
  return {
    id: `f${nextId++}`,
    type,
    target,
    detail: '',
    count: 1,
    firstSeen: '2026-01-01T00:00:00Z',
    lastSeen: '2026-01-01T00:00:00Z',
    cleared: false,
    ...overrides,
  }
}

// flagsState is a module-level singleton (see flags.svelte.ts), so every
// test shares the same instance -- reset its list by hand between tests
// rather than re-importing the module, mirroring auth.svelte.test.ts's
// approach for authState.
beforeEach(() => {
  flagsState.list = []
  nextId = 1
  vi.mocked(setFlagVerdict).mockReset()
  vi.mocked(deleteFlagVerdict).mockReset()
})

describe('extractSourceIp', () => {
  it('returns a bare source IP as-is', () => {
    // port_scan / activity_spike / critical_port / outbound_anomaly /
    // internal_recon / low_slow_scan all use a bare IP target.
    expect(extractSourceIp('203.0.113.9')).toBe('203.0.113.9')
  })

  it('strips the port suffix from a repeated_drops composite target', () => {
    // internal/detect/repeated_drops.go builds target as
    // fmt.Sprintf("%s -> port %d", e.SrcIP, e.DstPort).
    expect(extractSourceIp('203.0.113.9 -> port 22')).toBe('203.0.113.9')
  })

  it('excludes "global" -- global_spike has no single actor to group by', () => {
    expect(extractSourceIp('global')).toBeNull()
  })

  it('excludes a bare-port target (distributed_brute_force is keyed by port, not source)', () => {
    expect(extractSourceIp('port 22')).toBeNull()
  })

  it('excludes a rule-label target (rule_spike is keyed by rule, not source)', () => {
    expect(extractSourceIp('wan-block-scan')).toBeNull()
  })

  it('rejects an out-of-range IPv4-shaped string', () => {
    expect(extractSourceIp('999.999.999.999')).toBeNull()
  })

  it('accepts a bare IPv6 address', () => {
    expect(extractSourceIp('2001:db8::1')).toBe('2001:db8::1')
  })
})

describe('FlagsState.groupedBySource', () => {
  it('groups multiple active flags sharing a source IP', () => {
    flagsState.list = [flag('port_scan', '203.0.113.9'), flag('critical_port', '203.0.113.9')]

    const group = flagsState.groupedBySource.get('203.0.113.9')
    expect(group).toHaveLength(2)
    expect(group?.map((f) => f.type).sort()).toEqual(['critical_port', 'port_scan'])
  })

  it('extracts and groups a repeated_drops composite target under the bare IP', () => {
    flagsState.list = [flag('port_scan', '203.0.113.9'), flag('repeated_drops', '203.0.113.9 -> port 22')]

    const group = flagsState.groupedBySource.get('203.0.113.9')
    expect(group).toHaveLength(2)
    expect(group?.map((f) => f.type).sort()).toEqual(['port_scan', 'repeated_drops'])
  })

  it('does not surface a source with only one active flag as a group', () => {
    flagsState.list = [flag('port_scan', '203.0.113.9')]

    expect(flagsState.groupedBySource.has('203.0.113.9')).toBe(false)
  })

  it('excludes cleared flags from grouping, including from the group size check', () => {
    flagsState.list = [
      flag('port_scan', '203.0.113.9'),
      flag('critical_port', '203.0.113.9'),
      flag('activity_spike', '203.0.113.9', { cleared: true }),
    ]

    const group = flagsState.groupedBySource.get('203.0.113.9')
    expect(group).toHaveLength(2)
    expect(group?.every((f) => !f.cleared)).toBe(true)
  })

  it('drops a group down to nothing once only its cleared flags remain', () => {
    flagsState.list = [
      flag('port_scan', '203.0.113.9', { cleared: true }),
      flag('critical_port', '203.0.113.9'),
    ]

    expect(flagsState.groupedBySource.has('203.0.113.9')).toBe(false)
  })

  it('excludes global_spike flags entirely', () => {
    flagsState.list = [flag('global_spike', 'global'), flag('rule_spike', 'other-rule')]

    expect(flagsState.groupedBySource.size).toBe(0)
  })

  it('does not group non-source targets (distributed_brute_force / rule_spike) under a bogus shared key', () => {
    flagsState.list = [
      flag('distributed_brute_force', 'port 22'),
      flag('distributed_brute_force', 'port 22'),
      flag('rule_spike', 'wan-block-scan'),
      flag('rule_spike', 'wan-block-scan'),
    ]

    expect(flagsState.groupedBySource.size).toBe(0)
  })

  it('keeps unrelated source IPs in separate groups', () => {
    flagsState.list = [
      flag('port_scan', '203.0.113.9'),
      flag('critical_port', '203.0.113.9'),
      flag('port_scan', '198.51.100.4'),
      flag('activity_spike', '198.51.100.4'),
    ]

    expect(flagsState.groupedBySource.get('203.0.113.9')).toHaveLength(2)
    expect(flagsState.groupedBySource.get('198.51.100.4')).toHaveLength(2)
  })
})

// Issue #638's verdict loop. judgeAndClear posts at once and is
// awaited directly -- see its own doc comment in flags.svelte.ts for why
// an earlier, deferred version of this got replaced (a verdict judged
// just before a reload reached the server 0 times out of 6, because the
// PWA's service worker strips the keepalive guarantee that version
// depended on). #780 retired the old ~5s Undo window along with the
// toast it existed for: the row-level stamp now offers Undo for as
// long as the flag still carries the verdict, per isUndoable's own doc
// comment, so there is nothing timed left to test here.
describe('FlagsState verdicts (#638, #640)', () => {
  it('judgeAndClear posts the verdict immediately and clears the flag from the response', async () => {
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('port_scan', '203.0.113.9', { id, cleared: true, verdict: 'expected', verdictBy: 'alice', verdictAt: 't' }),
    )

    await flagsState.judgeAndClear(id, 'expected')

    expect(setFlagVerdict).toHaveBeenCalledWith(id, 'expected')
    expect(flagsState.list[0].cleared).toBe(true)
    expect(flagsState.list[0].verdict).toBe('expected')
    expect(flagsState.list[0].verdictBy).toBe('alice')
    expect(flagsState.isUndoable(id)).toBe(true)
  })

  it('judgeAndClear reverts the optimistic clear on failure and rethrows', async () => {
    vi.mocked(setFlagVerdict).mockRejectedValue(new Error('boom'))
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id

    await expect(flagsState.judgeAndClear(id, 'checked')).rejects.toThrow('boom')

    expect(flagsState.list[0].cleared).toBe(false)
    expect(flagsState.list[0].verdict).toBeUndefined()
    expect(flagsState.isUndoable(id)).toBe(false)
  })

  it('keeps offering Undo well past the old ~5s window, since #780 made it last as long as the verdict does', async () => {
    vi.useFakeTimers()
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('port_scan', '203.0.113.9', { id, cleared: true, verdict: 'expected', verdictBy: 'alice', verdictAt: 't' }),
    )

    await flagsState.judgeAndClear(id, 'expected')
    expect(flagsState.isUndoable(id)).toBe(true)

    await vi.advanceTimersByTimeAsync(60_000)

    expect(flagsState.isUndoable(id)).toBe(true)
    expect(setFlagVerdict).toHaveBeenCalledTimes(1)
    expect(deleteFlagVerdict).not.toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('undoVerdict sends a real DELETE and reopens the flag, within the window', async () => {
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('port_scan', '203.0.113.9', { id, cleared: true, verdict: 'checked', verdictBy: 'alice', verdictAt: 't' }),
    )
    await flagsState.judgeAndClear(id, 'checked')

    vi.mocked(deleteFlagVerdict).mockResolvedValue(flag('port_scan', '203.0.113.9', { id, cleared: false }))
    await flagsState.undoVerdict(id)

    expect(deleteFlagVerdict).toHaveBeenCalledWith(id)
    expect(flagsState.list[0].cleared).toBe(false)
    expect(flagsState.list[0].verdict).toBeUndefined()
    expect(flagsState.isUndoable(id)).toBe(false)
  })

  it('undoVerdict is a no-op on a flag that was never judged -- nothing to undo', async () => {
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id

    await flagsState.undoVerdict(id)

    expect(deleteFlagVerdict).not.toHaveBeenCalled()
    expect(flagsState.list[0].cleared).toBe(false)
  })

  it('undoVerdict sends a real DELETE for a standing investigate verdict, with no window at all (#780 item 3)', async () => {
    flagsState.list = [flag('critical_port', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('critical_port', '203.0.113.9', { id, verdict: 'investigate', verdictBy: 'alice', verdictAt: 't' }),
    )
    await flagsState.judgeInvestigate(id, 'alice')
    expect(flagsState.isUndoable(id)).toBe(true)

    vi.mocked(deleteFlagVerdict).mockResolvedValue(flag('critical_port', '203.0.113.9', { id, cleared: false }))
    await flagsState.undoVerdict(id)

    expect(deleteFlagVerdict).toHaveBeenCalledWith(id)
    expect(flagsState.list[0].verdict).toBeUndefined()
    expect(flagsState.isUndoable(id)).toBe(false)
  })

  it('undoVerdict reverts the optimistic reopen on failure and rethrows', async () => {
    flagsState.list = [flag('port_scan', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('port_scan', '203.0.113.9', { id, cleared: true, verdict: 'checked', verdictBy: 'alice', verdictAt: 't' }),
    )
    await flagsState.judgeAndClear(id, 'checked')

    vi.mocked(deleteFlagVerdict).mockRejectedValue(new Error('boom'))
    await expect(flagsState.undoVerdict(id)).rejects.toThrow('boom')

    expect(flagsState.list[0].cleared).toBe(true)
    expect(flagsState.list[0].verdict).toBe('checked')
  })

  it('judgeInvestigate records the verdict without clearing the flag', async () => {
    flagsState.list = [flag('critical_port', '203.0.113.9')]
    const id = flagsState.list[0].id
    vi.mocked(setFlagVerdict).mockResolvedValue(
      flag('critical_port', '203.0.113.9', { id, verdict: 'investigate', verdictBy: 'alice', verdictAt: 't' }),
    )

    await flagsState.judgeInvestigate(id, 'alice')

    expect(setFlagVerdict).toHaveBeenCalledWith(id, 'investigate')
    expect(flagsState.list[0].cleared).toBe(false)
    expect(flagsState.list[0].verdict).toBe('investigate')
    expect(flagsState.list[0].verdictBy).toBe('alice')
  })

  it('judgeInvestigate reverts the optimistic verdict on failure and rethrows', async () => {
    vi.mocked(setFlagVerdict).mockRejectedValue(new Error('boom'))
    flagsState.list = [flag('critical_port', '203.0.113.9')]

    await expect(flagsState.judgeInvestigate('f1', 'alice')).rejects.toThrow('boom')
    expect(flagsState.list[0].verdict).toBeUndefined()
  })

  it('never re-asks a flag that already carries a verdict', async () => {
    const already: Verdict = 'investigate'
    flagsState.list = [flag('critical_port', '203.0.113.9', { verdict: already, verdictBy: 'alice', verdictAt: 't' })]

    await flagsState.judgeInvestigate('f1', 'bob')

    expect(flagsState.list[0].verdictBy).toBe('alice')
    expect(setFlagVerdict).not.toHaveBeenCalled()
  })
})

// #642: the open-flags count is the settled ledger's count. A
// provisional flag -- raised while its baseline was still warming -- is
// visible on the learning shelf but never counted as an open flag, so
// the scene bar's flag mark and the bottom bar's badge only ever claim
// judgements mikroview actually trusts.
describe('provisional flags and the open-flags count (#642)', () => {
  it('activeCount excludes provisional flags', () => {
    flagsState.list = [flag('port_scan', '203.0.113.9'), flag('activity_spike', '203.0.113.10', { provisional: true })]
    expect(flagsState.activeCount).toBe(1)
  })

  it('provisionalCount counts open provisional flags only', () => {
    flagsState.list = [
      flag('port_scan', '203.0.113.9'),
      flag('activity_spike', '203.0.113.10', { provisional: true }),
      flag('port_scan', '203.0.113.11', { provisional: true, cleared: true }),
    ]
    expect(flagsState.provisionalCount).toBe(1)
  })

  it('a cleared provisional flag counts toward neither', () => {
    flagsState.list = [flag('port_scan', '203.0.113.9', { provisional: true, cleared: true })]
    expect(flagsState.activeCount).toBe(0)
    expect(flagsState.provisionalCount).toBe(0)
  })
})
