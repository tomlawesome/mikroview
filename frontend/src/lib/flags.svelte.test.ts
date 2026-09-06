// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Flag, FlagType, Verdict } from './types'

// flagsState only ever reaches the network through fetchFlags/setFlagVerdict
// (see api.ts) -- buildCampaigns and extractSourceIp are pure logic
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
import { buildCampaigns, extractSourceIp, flagsState } from './flags.svelte'

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

// buildCampaigns (#988, round 47): same source IP *and* inside one
// 30-minute window. Pure over whatever list it is handed -- Flags.svelte
// hands it the settled table's rows -- so no state is involved here.
describe('buildCampaigns', () => {
  const at = (hhmm: string) => `2026-01-01T${hhmm}:00Z`

  it('folds two flags from one source inside the window into one campaign', () => {
    const a = flag('port_scan', '203.0.113.9', { firstSeen: at('13:28'), lastSeen: at('13:30'), count: 20 })
    const b = flag('critical_port', '203.0.113.9', { firstSeen: at('13:40'), lastSeen: at('13:41'), count: 6 })
    const { campaigns, singles } = buildCampaigns([b, a])

    expect(singles).toEqual([])
    expect(campaigns).toHaveLength(1)
    expect(campaigns[0].ip).toBe('203.0.113.9')
    expect(campaigns[0].flags.map((f) => f.type)).toEqual(['port_scan', 'critical_port'])
    expect(campaigns[0].firstSeen).toBe(at('13:28'))
    expect(campaigns[0].lastSeen).toBe(at('13:41'))
    expect(campaigns[0].count).toBe(26)
    expect(campaigns[0].id).toBe(`campaign:${a.id}`)
  })

  it('a flag from the same source outside the window keeps its own row', () => {
    // cam-porch's activity spike at 11:10 is two hours from the 13:28
    // campaign -- the round-47 drawing's own example.
    const spike = flag('activity_spike', '10.0.20.14', { firstSeen: at('11:10'), lastSeen: at('11:50') })
    const a = flag('outbound_anomaly', '10.0.20.14', { firstSeen: at('13:28'), lastSeen: at('13:50') })
    const b = flag('repeated_drops', '10.0.20.14 -> port 22', { firstSeen: at('13:45'), lastSeen: at('13:52') })
    const { campaigns, singles } = buildCampaigns([spike, a, b])

    expect(singles).toEqual([spike])
    expect(campaigns).toHaveLength(1)
    expect(campaigns[0].flags).toEqual([a, b])
  })

  it('measures the gap from the end of the campaign so far, not from the previous flag alone', () => {
    // A long first flag still active at 13:50 reaches a 14:10 flag even
    // though their starts are 42 minutes apart.
    const a = flag('port_scan', '203.0.113.9', { firstSeen: at('13:28'), lastSeen: at('13:50') })
    const b = flag('critical_port', '203.0.113.9', { firstSeen: at('14:10'), lastSeen: at('14:10') })
    expect(buildCampaigns([a, b]).campaigns).toHaveLength(1)

    const c = flag('critical_port', '203.0.113.9', { firstSeen: at('14:21'), lastSeen: at('14:21') })
    expect(buildCampaigns([a, c]).campaigns).toHaveLength(0)
  })

  it('groups a repeated_drops composite target under its bare IP', () => {
    const a = flag('port_scan', '203.0.113.9')
    const b = flag('repeated_drops', '203.0.113.9 -> port 22')
    expect(buildCampaigns([a, b]).campaigns[0]?.flags).toEqual([a, b])
  })

  it('a source with one flag is a single, never a campaign', () => {
    const a = flag('port_scan', '203.0.113.9')
    const { campaigns, singles } = buildCampaigns([a])
    expect(campaigns).toEqual([])
    expect(singles).toEqual([a])
  })

  it('never groups targets that are not a single source IP', () => {
    const list = [
      flag('global_spike', 'global'),
      flag('distributed_brute_force', 'port 22'),
      flag('distributed_brute_force', 'port 22'),
      flag('rule_spike', 'wan-block-scan'),
      flag('rule_spike', 'wan-block-scan'),
    ]
    const { campaigns, singles } = buildCampaigns(list)
    expect(campaigns).toEqual([])
    expect(singles).toHaveLength(5)
  })

  it('keeps unrelated source IPs in separate campaigns', () => {
    const list = [
      flag('port_scan', '203.0.113.9'),
      flag('critical_port', '203.0.113.9'),
      flag('port_scan', '198.51.100.4'),
      flag('activity_spike', '198.51.100.4'),
    ]
    const { campaigns } = buildCampaigns(list)
    expect(campaigns.map((c) => c.ip).sort()).toEqual(['198.51.100.4', '203.0.113.9'])
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
