// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { WatchlistMatch } from './types'

vi.mock('./api', () => ({
  fetchRecentMatches: vi.fn(),
}))

import { fetchRecentMatches } from './api'
import { MATCHES_PAGE_SIZE, matchesState } from './matches.svelte'

function match(id: string, lastSeen: string, firstSeen = lastSeen): WatchlistMatch {
  return {
    id,
    entryId: 'e1',
    tuple: { source: { mac: 'aa:bb:cc:dd:ee:ff' }, destIp: '198.51.100.7', port: 9999 },
    event: {
      id: 1,
      time: lastSeen,
      deviceId: 'router-1',
      sourceIp: '192.168.1.1',
      action: 'drop',
      ruleLabel: 'r1',
      chain: 'forward',
      raw: 'raw line',
    },
    firstSeen,
    lastSeen,
    count: 1,
  }
}

// A full page, so the state does not read a short response as "that is
// everything" while a paging behaviour is what is under test. One minute
// apart, newest first, the order the server promises.
const PAGE_START_MS = Date.parse('2026-08-24T10:00:00Z')

function fullPage(prefix: string): WatchlistMatch[] {
  return Array.from({ length: MATCHES_PAGE_SIZE }, (_, i) =>
    match(`${prefix}${i}`, new Date(PAGE_START_MS - i * 60_000).toISOString()),
  )
}

// matchesState is a module-level singleton, like flagsState and
// authState -- reset by hand between tests rather than re-imported.
beforeEach(() => {
  vi.resetAllMocks()
  matchesState.reset()
})

describe('matchesState.load', () => {
  it('asks for the newest page and keeps the server ordering', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([
      match('m2', '2026-08-24T10:05:00Z'),
      match('m1', '2026-08-24T10:01:00Z'),
    ])

    await matchesState.load()

    expect(fetchRecentMatches).toHaveBeenCalledWith({ limit: MATCHES_PAGE_SIZE })
    expect(matchesState.records.map((m) => m.id)).toEqual(['m2', 'm1'])
    expect(matchesState.loaded).toBe(true)
    expect(matchesState.error).toBeNull()
  })

  it('treats a short first page as the whole log, so nothing offers to load older', async () => {
    vi.mocked(fetchRecentMatches).mockResolvedValue([match('m1', '2026-08-24T10:01:00Z')])

    await matchesState.load()

    expect(matchesState.exhausted).toBe(true)
  })

  it('a failed load keeps whatever was already shown, and says why', async () => {
    // The one sentence this surface must never say wrongly is "nothing
    // has broken" -- so a failed refresh must not empty the list and
    // leave the empty state to draw that conclusion.
    vi.mocked(fetchRecentMatches).mockResolvedValue([match('m1', '2026-08-24T10:01:00Z')])
    await matchesState.load()

    vi.mocked(fetchRecentMatches).mockRejectedValue(new Error('fetchRecentMatches: 503'))
    await matchesState.load()

    expect(matchesState.records.map((m) => m.id)).toEqual(['m1'])
    expect(matchesState.error).toBe('fetchRecentMatches: 503')
    expect(matchesState.loaded).toBe(true)
  })
})

describe('matchesState.loadOlder', () => {
  it('pages back from the oldest record shown, using its lastSeen as the cursor', async () => {
    const first = fullPage('a')
    vi.mocked(fetchRecentMatches).mockResolvedValue(first)
    await matchesState.load()

    vi.mocked(fetchRecentMatches).mockResolvedValue([match('b0', '2026-08-24T09:00:00Z')])
    await matchesState.loadOlder()

    expect(fetchRecentMatches).toHaveBeenLastCalledWith({
      until: first[first.length - 1].lastSeen,
      limit: MATCHES_PAGE_SIZE,
    })
    expect(matchesState.records.length).toBe(MATCHES_PAGE_SIZE + 1)
    expect(matchesState.records[matchesState.records.length - 1].id).toBe('b0')
    expect(matchesState.exhausted).toBe(true)
  })

  it('drops the overlap a firstSeen cursor necessarily returns', async () => {
    // until filters on firstSeen, so a long-running record that began
    // before the cursor comes back in the next page as well. Filtered on
    // arrival rather than avoided -- avoiding it would mean skipping,
    // and a missed violation is the worse failure.
    const first = fullPage('a')
    vi.mocked(fetchRecentMatches).mockResolvedValue(first)
    await matchesState.load()

    vi.mocked(fetchRecentMatches).mockResolvedValue([first[0], first[1], match('b0', '2026-08-24T09:00:00Z')])
    await matchesState.loadOlder()

    const ids = matchesState.records.map((m) => m.id)
    expect(ids.length).toBe(new Set(ids).size)
    expect(ids[ids.length - 1]).toBe('b0')
  })

  it('a page that is nothing but overlap is the end of the list', async () => {
    const first = fullPage('a')
    vi.mocked(fetchRecentMatches).mockResolvedValue(first)
    await matchesState.load()

    vi.mocked(fetchRecentMatches).mockResolvedValue([first[0]])
    await matchesState.loadOlder()

    expect(matchesState.records.length).toBe(MATCHES_PAGE_SIZE)
    expect(matchesState.exhausted).toBe(true)
  })

  it('does nothing at all with an empty list, so there is no cursor to invent', async () => {
    await matchesState.loadOlder()
    expect(fetchRecentMatches).not.toHaveBeenCalled()
  })

  it('a failed older page keeps the records already loaded', async () => {
    const first = fullPage('a')
    vi.mocked(fetchRecentMatches).mockResolvedValue(first)
    await matchesState.load()

    vi.mocked(fetchRecentMatches).mockRejectedValue(new Error('network error'))
    await matchesState.loadOlder()

    expect(matchesState.records.length).toBe(MATCHES_PAGE_SIZE)
    expect(matchesState.error).toBe('network error')
    expect(matchesState.exhausted).toBe(false)
  })
})
