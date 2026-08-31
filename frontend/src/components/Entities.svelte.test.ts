// SPDX-License-Identifier: AGPL-3.0-only
//
// Entities built to round 29's ratified scene (#675): router cards, then
// one table of named things with inline rename. Replaces the #647-era
// suite entirely -- that page's separate add-entity form, discovered-
// rules/-ports sections and the "Fleet absorbed" framing are gone, per
// the ratified scene, which has exactly one table and no other CRUD
// surface (see this file's own component for the reasoning).

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { Entity } from '../lib/types'

const fetchEntities = vi.fn(async (): Promise<Entity[]> => [])
const upsertEntity = vi.fn(async (_e: Entity): Promise<string | null> => null)

vi.mock('../lib/api', () => ({
  fetchEntities: () => fetchEntities(),
  upsertEntity: (e: Entity) => upsertEntity(e),
  deleteEntity: vi.fn(),
  fetchDeviceMACs: vi.fn(async () => []),
  fetchRouterRules: vi.fn(async () => ({ available: false, rules: [] })),
  fetchRouterAddresses: vi.fn(async () => ({ available: false, rules: [] })),
  fetchSetupStatus: vi.fn(
    async () =>
      ({
        instance: { tlsEnabled: true, syslogPort: ':16893', hosts: [] },
      }) as unknown,
  ),
}))

import { appState } from '../lib/state.svelte'
import { entitiesState } from '../lib/entities.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { zonesState } from '../lib/zones.svelte'
import Entities from './Entities.svelte'

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

beforeEach(() => {
  vi.clearAllMocks()
  fetchEntities.mockResolvedValue([])
  upsertEntity.mockResolvedValue(null)
  appState.devices = []
  appState.events = []
  appState.initialLoadDone = true
  entitiesState.list = []
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  zonesState.pushed = []
})

describe('Entities router cards (#675)', () => {
  it('renders one card per router, its live state and RouterOS version', async () => {
    appState.devices = [
      {
        id: 'rb5009',
        name: 'rb5009',
        configured: true,
        status: 'live',
        lastSeen: new Date().toISOString(),
        sourceIp: '10.0.0.1',
        eventCount: 3,
        routerosVersion: '7.20.1 (stable)',
      },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Entities)
    await settle()

    const card = container.querySelector('.fcard.live')
    expect(card?.querySelector('.fhead b')?.textContent).toBe('rb5009')
    expect(card?.querySelector('.fstate')?.textContent).toContain('LIVE')
    expect(card?.textContent).toContain('RouterOS 7.20.1 (stable)')
  })

  it('states a quiet router as a fact, not a fault', async () => {
    appState.devices = [
      {
        id: 'hap-ax2',
        name: 'hap-ax2',
        configured: true,
        status: 'stale',
        lastSeen: new Date(Date.now() - 3 * 86_400_000).toISOString(),
        sourceIp: '10.0.0.2',
        eventCount: 1,
      },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Entities)
    await settle()

    expect(container.textContent).toContain('QUIET')
    expect(container.textContent).toContain('quiet is a fact, not a fault')
  })

  it('carries the standing promise and never implies mikroview connects out', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.textContent).toContain('Routers push to mikroview — it never connects to them.')
    expect(container.textContent).not.toMatch(/mikroview (connects|reaches out|polls)/i)
  })

  it('discloses the real RouterOS syslog lines to paste on request', async () => {
    const { container, getByText } = render(Entities)
    await settle()

    expect(container.querySelector('.paste')).toBeNull()
    await fireEvent.click(getByText(/show the RouterOS lines to paste/))
    await settle()

    const pre = container.querySelector('.paste')
    expect(pre?.textContent).toContain('remote-port=16893')
    expect(pre?.textContent).toContain('remote-protocol=tls')
  })
})

describe('Entities named-things table (#675)', () => {
  it('renders name · lane · address · mac · first seen · last seen · marks', async () => {
    const { container } = render(Entities)
    await settle()

    const headers = [...container.querySelectorAll('.etable th')].map((th) => th.textContent)
    expect(headers).toEqual(['name', 'lane', 'address', 'mac', 'first seen', 'last seen', 'marks'])
  })

  it('shows a named host entity with its label and address', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] }])
    const { container } = render(Entities)
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('tom-desktop'))
    expect(row).toBeTruthy()
    expect(row?.textContent).toContain('10.0.10.2')
  })

  it('folds a discovered-but-unnamed host into the same table', async () => {
    appState.events = [
      { srcIp: '10.0.10.9', dstIp: '', time: new Date().toISOString(), receivedAt: Date.now() },
    ] as unknown as (typeof appState)['events']
    const { container } = render(Entities)
    await settle()

    expect(container.textContent).toContain('10.0.10.9')
    expect(container.textContent).toContain('— click to name —')
  })

  it('elides a known MAC and reads "private" when none is known', async () => {
    const { fetchDeviceMACs } = await import('../lib/api')
    vi.mocked(fetchDeviceMACs).mockResolvedValue([
      { mac: '2c:f0:5d:11:22:8a', firstSeen: '2025-01-01T00:00:00Z', lastSeen: new Date().toISOString(), lastIp: '10.0.10.2' },
    ])
    fetchEntities.mockResolvedValue([
      { type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] },
      { type: 'host', key: '10.0.30.12', label: 'guest-e8b2', tags: [] },
    ])
    const { container } = render(Entities)
    await settle()

    const rows = [...container.querySelectorAll('.etable tbody tr')]
    const named = rows.find((tr) => tr.textContent?.includes('tom-desktop'))
    const guest = rows.find((tr) => tr.textContent?.includes('guest-e8b2'))
    expect(named?.textContent).toContain('2c:f0:5d:…:8a')
    expect(guest?.textContent).toContain('private')
  })

  it('renames inline: click the name, edit, Enter saves', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: ['trusted'] }])
    const { container, getByText } = render(Entities)
    await settle()

    await fireEvent.click(getByText('tom-desktop'))
    await settle()
    const input = container.querySelector('.rename-input') as HTMLInputElement
    expect(input).toBeTruthy()
    await fireEvent.input(input, { target: { value: 'toms-desktop' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()

    expect(upsertEntity).toHaveBeenCalledWith({ type: 'host', key: '10.0.10.2', label: 'toms-desktop', tags: ['trusted'] })
  })

  it('Esc cancels a rename without saving', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] }])
    const { container, getByText } = render(Entities)
    await settle()

    await fireEvent.click(getByText('tom-desktop'))
    await settle()
    const input = container.querySelector('.rename-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'oops' } })
    await fireEvent.keyDown(input, { key: 'Escape' })
    await settle()

    expect(container.querySelector('.rename-input')).toBeNull()
    expect(upsertEntity).not.toHaveBeenCalled()
  })

  it('saves on blur, same as Enter -- and a blur that arrives after a cancel must not resurrect it', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] }])
    const { container, getByText } = render(Entities)
    await settle()

    await fireEvent.click(getByText('tom-desktop'))
    await settle()
    let input = container.querySelector('.rename-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'toms-desktop' } })
    await fireEvent.blur(input)
    await settle()
    expect(upsertEntity).toHaveBeenCalledTimes(1)

    // A real browser can fire blur when Escape's cancel removes the
    // input's focus along with the element itself (jsdom does not
    // reproduce this, which is exactly why this is a direct call rather
    // than relying on fireEvent.keyDown to trigger it) -- that trailing
    // blur must be a no-op, not a second save carrying the stale draft.
    // Reopened by selector, not by its (still-mocked, unrefreshed) label
    // text -- there is only one row in this fixture.
    await fireEvent.click(container.querySelector('.rename-btn') as HTMLElement)
    await settle()
    input = container.querySelector('.rename-input') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'oops' } })
    await fireEvent.keyDown(input, { key: 'Escape' })
    await fireEvent.blur(input)
    await settle()
    expect(upsertEntity).toHaveBeenCalledTimes(1)
  })

  it('marks a host with an active new_device flag as a new talker, in the family ink', async () => {
    const { fetchDeviceMACs } = await import('../lib/api')
    vi.mocked(fetchDeviceMACs).mockResolvedValue([
      { mac: 'aa:bb:cc:dd:ee:ff', firstSeen: new Date().toISOString(), lastSeen: new Date().toISOString(), lastIp: '10.0.10.7' },
    ])
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.7', label: 'laptop-anna', tags: [] }])
    flagsState.list = [
      {
        id: 'f1',
        type: 'new_device',
        target: 'aa:bb:cc:dd:ee:ff',
        detail: '',
        count: 1,
        firstSeen: new Date().toISOString(),
        lastSeen: new Date().toISOString(),
        cleared: false,
      },
    ]
    const { container } = render(Entities)
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('laptop-anna'))
    expect(row?.textContent).toContain('▲ new talker')
  })

  it('marks a watched host, and appends ring broken when its coverage has none', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.40.2', label: 'nas', tags: [] }])
    watchlistState.entries = [
      { id: 'w1', enabled: true, destIp: '10.0.40.2', createdAt: new Date().toISOString() },
    ]
    watchlistState.coverage = { w1: 'no-logging' }
    const { container } = render(Entities)
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('nas'))
    expect(row?.textContent).toContain('◉ watched')
    expect(row?.textContent).toContain('○ ring broken')
  })

  it('marks a host carrying open alarm-family flags as flagged', async () => {
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.20.14', label: 'cam-porch', tags: [] }])
    flagsState.list = [
      {
        id: 'f2',
        type: 'critical_port',
        target: '10.0.20.14',
        detail: '',
        count: 1,
        firstSeen: new Date().toISOString(),
        lastSeen: new Date().toISOString(),
        cleared: false,
      },
    ]
    const { container } = render(Entities)
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('cam-porch'))
    expect(row?.textContent).toContain('✱ flagged')
    expect(row?.className).toContain('warn')
  })

  it('states the rename affordance in the footer', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.table-hint')?.textContent).toContain(
      "a name is yours to give — click one to rename it; the router's own names arrive with its pushes",
    )
  })
})
