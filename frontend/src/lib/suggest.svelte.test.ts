// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { dedupeSuggestions } from './suggest.svelte'
import type { Suggestion } from './types'

function suggestion(id: string, overrides: Partial<Suggestion> = {}): Suggestion {
  return {
    id,
    kind: 'port',
    status: 'off',
    name: 'port 445',
    justification: `${id} justification`,
    routerDevice: 'rb5009',
    ports: [445],
    firstSeen: '2026-08-24T09:00:00Z',
    updatedAt: '2026-08-24T09:00:00Z',
    ...overrides,
  }
}

// #1160: the watchlist listed "port 445 → :445 · suggested — from drop
// rule port 445" three times, and "port 139, 445" twice. internal/suggest
// records one candidate per justification, so overlapping drop rules --
// or the same rule on two routers -- arrive as separate candidates that
// draw the same row.
describe('dedupeSuggestions (#1160)', () => {
  it('keeps one row where several candidates would draw the same one', () => {
    const kept = dedupeSuggestions([suggestion('s1'), suggestion('s2'), suggestion('s3')])
    expect(kept.map((c) => c.id)).toEqual(['s1'])
  })

  it('keeps the first, which is the earliest and the id the row has always acted on', () => {
    const kept = dedupeSuggestions([
      suggestion('older', { firstSeen: '2026-08-24T09:00:00Z' }),
      suggestion('newer', { firstSeen: '2026-08-25T09:00:00Z' }),
    ])
    expect(kept[0].id).toBe('older')
    expect(kept[0].firstSeen).toBe('2026-08-24T09:00:00Z')
  })

  it('treats the same rule pushed by two routers as one suggestion', () => {
    const kept = dedupeSuggestions([
      suggestion('s1', { routerDevice: 'rb5009' }),
      suggestion('s2', { routerDevice: 'hap-ax2' }),
    ])
    expect(kept.length).toBe(1)
  })

  it('reads the same ports in any order as the same suggestion', () => {
    const kept = dedupeSuggestions([
      suggestion('s1', { name: 'smb', ports: [139, 445] }),
      suggestion('s2', { name: 'smb', ports: [445, 139] }),
    ])
    expect(kept.length).toBe(1)
  })

  it('keeps candidates that would draw different rows', () => {
    const kept = dedupeSuggestions([
      suggestion('s1'),
      suggestion('s2', { name: 'port 139', ports: [139] }),
      suggestion('s3', { kind: 'device', name: 'nas', ports: undefined, source: { ip: '10.0.10.5' } }),
      suggestion('s4', { kind: 'addressList', name: 'blocked', ports: undefined, addressList: 'blocked' }),
    ])
    expect(kept.map((c) => c.id)).toEqual(['s1', 's2', 's3', 's4'])
  })

  // A stale candidate says so on its own chip ("stale — the rule is
  // gone"), so it is not the same row as a live one.
  it('does not fold a stale candidate into a live one', () => {
    const kept = dedupeSuggestions([suggestion('s1'), suggestion('s2', { stale: true })])
    expect(kept.map((c) => c.id)).toEqual(['s1', 's2'])
  })

  it('leaves an empty list alone', () => {
    expect(dedupeSuggestions([])).toEqual([])
  })
})
