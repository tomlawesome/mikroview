// SPDX-License-Identifier: AGPL-3.0-only
//
// Entities built to round 29's ratified scene (#675): router cards, then
// one table of named things with inline rename. Replaces the #647-era
// suite entirely -- that page's separate add-entity form, discovered-
// rules/-ports sections and the "Fleet absorbed" framing are gone, per
// the ratified scene, which has exactly one table and no other CRUD
// surface (see this file's own component for the reasoning).
//
// #681 adds a tab strip -- hosts/rules/ports -- back over that one
// table, so the suite below gained its own describe blocks for the two
// new tabs; the hosts-tab tests above are otherwise untouched (hosts
// stays the default tab, unchanged).
//
// Round 30's #ent draws no tab strip at all: the entities table follows
// the router cards directly, one table of named things, exactly as #675
// first built it. The strip is unmounted behind TABS_ENABLED (see the
// comment on that flag in Entities.svelte), not deleted -- #691 tracks
// remounting it. The tab-strip describe block below now asserts the
// strip's absence and that hosts renders by default with no way to
// switch away from it; the rules-tab and ports-tab describe blocks are
// skipped rather than deleted, since every one of their tests reaches
// its table by clicking a tab button that round 30 does not render --
// un-skip them when #691 remounts the strip.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { Entity, RuleUsage } from '../lib/types'
import type { RouterFilterRule } from '../lib/api'

const fetchEntities = vi.fn(async (): Promise<Entity[]> => [])
const upsertEntity = vi.fn(async (_e: Entity): Promise<string | null> => null)
const fetchRules = vi.fn(async (): Promise<RuleUsage[]> => [])
const fetchRouterRules = vi.fn(async (): Promise<{ available: boolean; rules: RouterFilterRule[] }> => ({
  available: false,
  rules: [],
}))

vi.mock('../lib/api', () => ({
  fetchEntities: () => fetchEntities(),
  upsertEntity: (e: Entity) => upsertEntity(e),
  deleteEntity: vi.fn(),
  fetchDeviceMACs: vi.fn(async () => []),
  fetchRouterRules: () => fetchRouterRules(),
  fetchRouterAddresses: vi.fn(async () => ({ available: false, rules: [] })),
  fetchRules: () => fetchRules(),
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
  fetchRules.mockResolvedValue([])
  fetchRouterRules.mockResolvedValue({ available: false, rules: [] })
  appState.devices = []
  appState.events = []
  appState.initialLoadDone = true
  entitiesState.list = []
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  zonesState.pushed = []
})

// A pushed rule: comment is the operator-facing label RouterOS shows,
// logPrefix is the "<ACTION>|<slug>|" convention mikroview's own ingest
// decodes -- the slug (not comment) is the rule's entity key throughout
// this suite, matching ruleLabelFromLogPrefix (lib/routerLookup.svelte.ts).
function filterRule(over: Partial<RouterFilterRule> = {}): RouterFilterRule {
  return {
    ordinal: 0,
    comment: 'a filter rule',
    chain: 'forward',
    action: 'accept',
    srcAddressList: '',
    logPrefix: 'A|lan-wan|',
    log: true,
    ...over,
  }
}

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

describe('Entities tab strip (#681, unmounted for round-30 fidelity)', () => {
  it('draws no tab strip -- the entities table follows the router cards directly, and hosts (the ratified table) is what renders since there is no button left to switch away from it (TABS_ENABLED, #691)', async () => {
    const { container, queryByRole } = render(Entities)
    await settle()

    expect(queryByRole('tablist')).toBeNull()
    expect(container.querySelectorAll('.tab').length).toBe(0)
    expect([...container.querySelectorAll('.etable th')].map((th) => th.textContent)).toEqual([
      'name',
      'lane',
      'address',
      'mac',
      'first seen',
      'last seen',
      'marks',
    ])
  })
})

describe.skip('Entities rules tab (#681) -- skipped while the tab strip is unmounted for round-30 fidelity (TABS_ENABLED, #691): every test below reaches the rules table by clicking a tab button that round 30 does not render. The rules table and its rename path are still implemented in Entities.svelte; un-skip this block when #691 remounts the strip.', () => {
  it('lists a rule that has been pushed but has never fired as its own row, reading as never-fired rather than blank', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule({ chain: 'forward', action: 'drop', logPrefix: 'D|guest-block|' })],
    })
    fetchRules.mockResolvedValue([]) // nothing has ever fired

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'rules' }))
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('guest-block'))
    expect(row).toBeTruthy()
    expect(row?.textContent).toContain('forward')
    expect(row?.textContent).toContain('drop')
    expect(row?.textContent).toContain('has not fired')
  })

  it('shows a fired rule\'s last-fired time and folds in its saved name', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 1 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule({ chain: 'input', action: 'accept', logPrefix: 'A|lan-wan|' })],
    })
    const lastSeen = new Date(Date.now() - 5 * 60_000).toISOString()
    fetchRules.mockResolvedValue([{ rule: 'lan-wan', firstSeen: lastSeen, lastSeen, count: 12 }])
    fetchEntities.mockResolvedValue([{ type: 'rule', key: 'lan-wan', label: 'LAN to WAN', tags: [] }])

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'rules' }))
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('LAN to WAN'))
    expect(row).toBeTruthy()
    expect(row?.textContent).toMatch(/[45]m ago/)
    expect(row?.textContent).not.toContain('has not fired')
  })

  it('renames a never-fired rule inline, the same store and behaviour as a host', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({ available: true, rules: [filterRule({ logPrefix: 'A|lan-wan|' })] })

    const { container, getByRole, getByText } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'rules' }))
    await settle()

    await fireEvent.click(getByText('lan-wan'))
    await settle()
    const input = container.querySelector('.rename-input') as HTMLInputElement
    expect(input).toBeTruthy()
    await fireEvent.input(input, { target: { value: 'LAN to WAN' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()

    expect(upsertEntity).toHaveBeenCalledWith({ type: 'rule', key: 'lan-wan', label: 'LAN to WAN', tags: [] })
  })

  it('states a true, specific empty state when no router has pushed a rule table yet', async () => {
    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'rules' }))
    await settle()

    expect(container.querySelector('.etable tbody')?.textContent).toContain(
      'No router has pushed a rule table yet — once one does, every rule it carries appears here, fired or not.',
    )
  })

  it('leaves an unlogged rule off the tab -- it can never fire under a label, so naming it would be a dead end', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule({ log: false, logPrefix: '', comment: 'no logging on this one' })],
    })

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'rules' }))
    await settle()

    expect(container.textContent).not.toContain('no logging on this one')
    expect(container.querySelector('.etable tbody')?.textContent).toContain('No router has pushed a rule table yet')
  })
})

describe.skip('Entities ports tab (#681) -- skipped while the tab strip is unmounted for round-30 fidelity (TABS_ENABLED, #691): every test below reaches the ports table by clicking a tab button that round 30 does not render. The ports table and its rename path are still implemented in Entities.svelte; un-skip this block when #691 remounts the strip.', () => {
  it('folds a discovered-but-unnamed port into the ports tab', async () => {
    appState.events = [
      { srcIp: '10.0.10.9', dstIp: '10.0.10.1', srcPort: 51413, dstPort: 443, time: new Date().toISOString(), receivedAt: Date.now() },
    ] as unknown as (typeof appState)['events']

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'ports' }))
    await settle()

    expect(container.textContent).toContain('51413')
    expect(container.textContent).toContain('443')
    expect(container.textContent).toContain('— click to name —')
  })

  it('shows an already-named port by its label', async () => {
    fetchEntities.mockResolvedValue([{ type: 'port', key: '8384', label: 'syncthing', tags: [] }])

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('tab', { name: 'ports' }))
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('syncthing'))
    expect(row).toBeTruthy()
    expect(row?.textContent).toContain('8384')
  })
})
