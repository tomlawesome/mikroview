// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { CoverageDeclaration } from './api'

vi.mock('./api', () => ({
  fetchCoverageDeclarations: vi.fn(),
  putCoverageDeclaration: vi.fn(),
  deleteCoverageDeclaration: vi.fn(),
}))

import { fetchCoverageDeclarations } from './api'
import { coverageState } from './coverage.svelte'

function declaration(overrides: Partial<CoverageDeclaration> = {}): CoverageDeclaration {
  return {
    key: 'ether1|bridge1',
    reason: 'printer, checked quarterly',
    declaredBy: 'admin',
    declaredAt: '2026-09-01T12:00:00Z',
    ...overrides,
  }
}

// coverageState is a module-level singleton, like hostsState and
// flagsState -- reset by hand between tests rather than re-imported.
beforeEach(() => {
  vi.resetAllMocks()
  coverageState.declarations = []
  coverageState.unreadable = false
  coverageState.error = null
})

describe('coverageState', () => {
  it('refresh loads the declarations and keys them', async () => {
    vi.mocked(fetchCoverageDeclarations).mockResolvedValue([declaration()])

    await coverageState.refresh()

    expect(coverageState.declarations).toHaveLength(1)
    expect(coverageState.byKey.get('ether1|bridge1')?.reason).toBe('printer, checked quarterly')
  })

  it('refresh leaves the declarations as they were when the read fails -- stale beats empty', async () => {
    coverageState.declarations = [declaration()]
    vi.mocked(fetchCoverageDeclarations).mockRejectedValue(new Error('503'))

    await coverageState.refresh()

    expect(coverageState.declarations).toHaveLength(1)
  })

  // #1237: a failed read must be distinguishable from a genuinely empty
  // store, or the coverage lens cannot tell "not declared" from
  // "unknown" apart.
  it('refresh flags the declarations unreadable when the read fails, without touching declarations', async () => {
    coverageState.declarations = [declaration()]
    vi.mocked(fetchCoverageDeclarations).mockRejectedValue(new Error('503'))

    await coverageState.refresh()

    expect(coverageState.unreadable).toBe(true)
    expect(coverageState.declarations).toHaveLength(1)
  })

  it('refresh clears the unreadable flag once a read succeeds again', async () => {
    coverageState.unreadable = true
    vi.mocked(fetchCoverageDeclarations).mockResolvedValue([declaration()])

    await coverageState.refresh()

    expect(coverageState.unreadable).toBe(false)
  })
})
