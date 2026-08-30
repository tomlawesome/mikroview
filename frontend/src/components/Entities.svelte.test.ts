// SPDX-License-Identifier: AGPL-3.0-only
//
// The merged Entities page (#647, #634 round 23): "Fleet rolled into
// Entities -- routers lead the page." The routers section reuses
// lib/fleet.ts, the same module Fleet.svelte itself reads (see that
// file's own test for its half of the same contract); this file covers
// the parts specific to the merge -- the section leading the page,
// under the "Entities" title, never "Fleet" -- plus the entitiesState
// refresh that used to ride the now-retired nav rail's onMount and had
// nothing left to trigger it (#647 caught and fixed that alongside the
// merge -- see Entities.svelte's own onMount comment).

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'

const fetchEntities = vi.fn(async () => [])

vi.mock('../lib/api', () => ({
  fetchRules: vi.fn(async () => []),
  fetchEntities: () => fetchEntities(),
  upsertEntity: vi.fn(),
  deleteEntity: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { entitiesState } from '../lib/entities.svelte'
import Entities from './Entities.svelte'

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

beforeEach(() => {
  fetchEntities.mockClear()
  appState.devices = []
  appState.initialLoadDone = true
  entitiesState.list = []
})

describe('Entities absorbs Fleet (#647)', () => {
  it('leads with a Routers section, under the "Entities" title -- never "Fleet"', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.page-header h2')?.textContent).toBe('Entities')
    expect(container.textContent).not.toMatch(/\bFleet\b/)
    expect(container.querySelector('.section.routers .section-title')?.textContent).toBe('Routers')
  })

  it('shows ghost rows while devices have not loaded, same as Fleet did', async () => {
    appState.initialLoadDone = false
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.section.routers .ghost-rows')).not.toBeNull()
  })

  it('renders the router table once at least one device exists', async () => {
    appState.devices = [
      {
        id: 'r1',
        name: 'router1',
        configured: true,
        status: 'live',
        lastSeen: '2026-08-24T00:00:00Z',
        sourceIp: '10.0.0.1',
        eventCount: 3,
      },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.section.routers table')).not.toBeNull()
    expect(container.querySelector('.section.routers .rname')?.textContent).toBe('router1')
  })

  it('refreshes entitiesState on mount -- nothing else triggers it now the old rail is gone', async () => {
    render(Entities)
    await settle()

    expect(fetchEntities).toHaveBeenCalledTimes(1)
  })
})
