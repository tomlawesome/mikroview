// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Host, HostMarkKind } from './api'

vi.mock('./api', () => ({
  fetchHosts: vi.fn(),
  putHostMark: vi.fn(),
  deleteHostMark: vi.fn(),
}))

import { deleteHostMark, fetchHosts, putHostMark } from './api'
import { HOST_QUIET_AFTER_MS, hostsState, presenceOf } from './hosts.svelte'

const NOW = Date.parse('2026-09-01T12:00:00Z')

function host(overrides: Partial<Host> = {}): Host {
  return {
    key: 'bridge-lan|10.0.10.5',
    iface: 'bridge-lan',
    ip: '10.0.10.5',
    label: 'lab-nas',
    firstSeen: new Date(NOW - 86_400_000).toISOString(),
    lastSeen: new Date(NOW).toISOString(),
    events: 42,
    ...overrides,
  }
}

function marked(kind: HostMarkKind, lastSeenMsAgo: number): Host {
  return host({
    lastSeen: new Date(NOW - lastSeenMsAgo).toISOString(),
    mark: { kind, reason: 'on purpose', by: 'admin', at: new Date(NOW).toISOString() },
  })
}

describe('presenceOf', () => {
  it('calls a host that spoke recently live', () => {
    expect(presenceOf(host(), NOW, HOST_QUIET_AFTER_MS)).toBe('live')
    expect(
      presenceOf(host({ lastSeen: new Date(NOW - 60_000).toISOString() }), NOW, HOST_QUIET_AFTER_MS),
    ).toBe('live')
  })

  it('calls a host quiet once it has been silent past the threshold', () => {
    const silent = host({ lastSeen: new Date(NOW - HOST_QUIET_AFTER_MS - 1).toISOString() })
    expect(presenceOf(silent, NOW, HOST_QUIET_AFTER_MS)).toBe('quiet')
  })

  it('treats exactly the threshold as still live -- quiet needs the silence to be longer than it, not equal', () => {
    const borderline = host({ lastSeen: new Date(NOW - HOST_QUIET_AFTER_MS).toISOString() })
    expect(presenceOf(borderline, NOW, HOST_QUIET_AFTER_MS)).toBe('live')
  })

  it('reports a dismissed host as dismissed however recently it was seen', () => {
    // The server clears a dismissal the moment the host speaks again, so
    // a dismissal that is still here means the host has not been back.
    expect(presenceOf(marked('dismissed', 0), NOW, HOST_QUIET_AFTER_MS)).toBe('dismissed')
    expect(presenceOf(marked('dismissed', 86_400_000), NOW, HOST_QUIET_AFTER_MS)).toBe('dismissed')
  })

  it('reports an intended host as intended only while it is actually quiet', () => {
    expect(presenceOf(marked('intended', HOST_QUIET_AFTER_MS + 1), NOW, HOST_QUIET_AFTER_MS)).toBe(
      'intended',
    )
    // Still talking: the explanation is waiting for the next silence,
    // not colouring a working host.
    expect(presenceOf(marked('intended', 0), NOW, HOST_QUIET_AFTER_MS)).toBe('live')
  })

  it('treats an unparseable last-seen stamp as quiet, never as live', () => {
    expect(presenceOf(host({ lastSeen: 'not a date' }), NOW, HOST_QUIET_AFTER_MS)).toBe('quiet')
  })

  it('takes the threshold from its argument, so the drawing round can change one constant', () => {
    const silent = host({ lastSeen: new Date(NOW - 60_000).toISOString() })
    expect(presenceOf(silent, NOW, 30_000)).toBe('quiet')
    expect(presenceOf(silent, NOW, 120_000)).toBe('live')
  })
})

// hostsState is a module-level singleton, like coverageState and
// flagsState -- reset by hand between tests rather than re-imported.
beforeEach(() => {
  vi.resetAllMocks()
  hostsState.hosts = []
  hostsState.error = null
})

describe('hostsState', () => {
  it('refresh loads the register and keys it', async () => {
    vi.mocked(fetchHosts).mockResolvedValue([host(), host({ key: 'bridge-lan|10.0.10.9', ip: '10.0.10.9' })])

    await hostsState.refresh()

    expect(hostsState.hosts).toHaveLength(2)
    expect(hostsState.byKey.get('bridge-lan|10.0.10.5')?.label).toBe('lab-nas')
    expect(hostsState.byKey.get('bridge-lan|10.0.10.9')?.ip).toBe('10.0.10.9')
  })

  it('refresh leaves the register as it was when the read fails -- dark stays dark', async () => {
    hostsState.hosts = [host()]
    vi.mocked(fetchHosts).mockRejectedValue(new Error('503'))

    await hostsState.refresh()

    expect(hostsState.hosts).toHaveLength(1)
  })

  it('mark sends the kind and reason, then re-reads', async () => {
    vi.mocked(putHostMark).mockResolvedValue(marked('intended', 0))
    vi.mocked(fetchHosts).mockResolvedValue([marked('intended', 0)])

    const ok = await hostsState.mark('bridge-lan|10.0.10.5', 'intended', 'powered on twice a month')

    expect(ok).toBe(true)
    expect(putHostMark).toHaveBeenCalledWith(
      'bridge-lan|10.0.10.5',
      'intended',
      'powered on twice a month',
    )
    expect(fetchHosts).toHaveBeenCalled()
    expect(hostsState.byKey.get('bridge-lan|10.0.10.5')?.mark?.kind).toBe('intended')
    expect(hostsState.error).toBeNull()
  })

  it('mark surfaces the server refusal and does not re-read', async () => {
    vi.mocked(putHostMark).mockResolvedValue('kind must be intended or dismissed')

    const ok = await hostsState.mark('bridge-lan|10.0.10.5', 'intended')

    expect(ok).toBe(false)
    expect(hostsState.error).toBe('kind must be intended or dismissed')
    expect(fetchHosts).not.toHaveBeenCalled()
  })

  it('unmark clears the mark and re-reads', async () => {
    vi.mocked(deleteHostMark).mockResolvedValue(null)
    vi.mocked(fetchHosts).mockResolvedValue([host()])

    const ok = await hostsState.unmark('bridge-lan|10.0.10.5')

    expect(ok).toBe(true)
    expect(deleteHostMark).toHaveBeenCalledWith('bridge-lan|10.0.10.5')
    expect(hostsState.byKey.get('bridge-lan|10.0.10.5')?.mark).toBeUndefined()
  })

  it('unmark surfaces the server refusal', async () => {
    vi.mocked(deleteHostMark).mockResolvedValue('not found')

    const ok = await hostsState.unmark('bridge-lan|10.0.10.5')

    expect(ok).toBe(false)
    expect(hostsState.error).toBe('not found')
    expect(fetchHosts).not.toHaveBeenCalled()
  })

  it('a later mark clears the previous error', async () => {
    hostsState.error = 'stale failure'
    vi.mocked(putHostMark).mockResolvedValue(marked('dismissed', 0))
    vi.mocked(fetchHosts).mockResolvedValue([])

    await hostsState.mark('bridge-lan|10.0.10.5', 'dismissed')

    expect(hostsState.error).toBeNull()
  })
})
