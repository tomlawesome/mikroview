// SPDX-License-Identifier: AGPL-3.0-only
//
// Entities built to round 29's ratified scene (#675): router cards, then
// one table of named things with inline rename. Replaces the #647-era
// suite entirely -- that page's separate add-entity form, discovered-
// rules/-ports sections and the "Fleet absorbed" framing are gone, per
// the ratified scene, which has exactly one table and no other CRUD
// surface (see this file's own component for the reasoning).
//
// #681 put hosts/rules/ports back over that one table as a tab strip.
// Round 30 drew no strip, so it was unmounted behind TABS_ENABLED and
// the rules/ports blocks below were skipped -- every one of their tests
// reaches its table through a control that was not rendered.
//
// Rounds 37-38 (#804) settle it the other way: the three are views, not
// tabs -- the metrics page's idiom, names carrying their counts, one
// underlined, over the same table. The gate is gone, so those blocks
// run again, now clicking a view button instead of a tab. Round 38 also
// removed the descriptor line under each view on the owner's word, and
// the read-only viewer is declared once on the account chip rather than
// anywhere on this page -- both asserted below, as absences.

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
  // The berth panel's paste lines (#436 moved the RouterOS syntax
  // server-side) -- built from the request's own syslogPort, the same
  // way the retired client-side syslogCommands() did, so the port
  // assertion below still means something.
  fetchSetupCommands: vi.fn(async (req: { syslogPort?: string }) => ({
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: {
        commands:
          `/system logging action add name=mikroview target=remote remote=localhost ` +
          `remote-port=${(req.syslogPort ?? ':6514').replace(/^:/, '')} remote-protocol=tls ` +
          `check-certificate=yes\n/system logging add topics=firewall,info action=mikroview`,
        note: '',
      },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
  })),
}))

import { appState } from '../lib/state.svelte'
import { entitiesState } from '../lib/entities.svelte'
import { flagsState } from '../lib/flags.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { zonesState } from '../lib/zones.svelte'
import { authState } from '../lib/auth.svelte'
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
  // Renaming is tier-gated since #804 (the read-only viewer), and an
  // unset role is least-privileged by design (see auth.svelte.ts), so a
  // suite that exercises renaming has to say who is signed in. The
  // viewer's own view of the same table has its own block below.
  authState.role = 'admin'
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

  it('keeps a space after the middot on the push-rate line, same as the rule/zone line above it (#718)', async () => {
    appState.devices = [
      {
        id: 'rb5009',
        name: 'rb5009',
        configured: true,
        status: 'live',
        lastSeen: new Date().toISOString(),
        sourceIp: '10.0.0.1',
        eventCount: 3,
      },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule()],
      updatedAt: new Date().toISOString(),
    } as unknown as { available: boolean; rules: ReturnType<typeof filterRule>[] })
    const { container } = render(Entities)
    await settle()

    const card = container.querySelector('.fcard.live')
    expect(card?.textContent).toContain('last push')
    expect(card?.textContent).not.toMatch(/·\d/)
    expect(card?.textContent).toMatch(/· \d/)
  })

  it('renders every router card\'s events/s figure at the same one-decimal precision, never a bare whole number (#718)', async () => {
    const now = Date.now()
    appState.now = now
    appState.devices = [
      {
        id: 'quiet-rate',
        name: 'quiet-rate',
        configured: true,
        status: 'live',
        lastSeen: new Date(now).toISOString(),
        sourceIp: '10.0.0.1',
        eventCount: 3,
      },
      {
        id: 'busy-rate',
        name: 'busy-rate',
        configured: true,
        status: 'live',
        lastSeen: new Date(now).toISOString(),
        sourceIp: '10.0.0.2',
        eventCount: 600,
      },
    ] as unknown as (typeof appState)['devices']
    appState.events = [
      ...Array.from({ length: 3 }, (_, i) => ({ deviceId: 'quiet-rate', receivedAt: now - i })),
      ...Array.from({ length: 600 }, (_, i) => ({ deviceId: 'busy-rate', receivedAt: now - i })),
    ] as unknown as (typeof appState)['events']

    const { container } = render(Entities)
    await settle()

    const cards = [...container.querySelectorAll('.fcard.live')]
    const quiet = cards.find((c) => c.textContent?.includes('quiet-rate'))
    const busy = cards.find((c) => c.textContent?.includes('busy-rate'))
    expect(quiet?.textContent).toMatch(/\d\.\d events\/s now/)
    expect(busy?.textContent).toContain('2.0 events/s now')
    expect(busy?.textContent).not.toContain('2 events/s now')
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

  it('draws the empty berth as one more card at the end of the router row, with no visible label (#718)', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 3 },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Entities)
    await settle()

    const cards = [...container.querySelectorAll('.fcards > .fcard')]
    expect(cards.length).toBe(2) // one router card, one berth
    expect(cards.at(-1)?.className).toContain('berth')
    expect(cards.at(-1)?.textContent?.trim()).toBe('') // the shape is the affordance, no words
  })

  it('is the whole row when there are no routers at all -- the correct first-run state (#718)', async () => {
    const { container } = render(Entities)
    await settle()

    const cards = [...container.querySelectorAll('.fcards > .fcard')]
    expect(cards.length).toBe(1)
    expect(cards[0].className).toContain('berth')
  })

  it('names itself to a screen reader even though the visual carries no words (#718)', async () => {
    const { getByRole } = render(Entities)
    await settle()

    expect(getByRole('button', { name: 'Add a router' })).toBeTruthy()
  })

  it('keeps the add-router explanation and commands off the page until the berth is activated (#718)', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.textContent).not.toContain('Routers push to mikroview')
    expect(container.querySelector('.paste')).toBeNull()
  })

  it('reveals the port, the paste-able RouterOS lines and the never-connects assurance when the berth is clicked (#718)', async () => {
    const { container, getByRole } = render(Entities)
    await settle()

    await fireEvent.click(getByRole('button', { name: 'Add a router' }))
    await settle()

    expect(container.textContent).toContain(':16893')
    expect(container.textContent).toContain('Routers push to mikroview — it never connects to them.')
    expect(container.textContent).not.toMatch(/mikroview (connects|reaches out|polls)/i)

    const pre = container.querySelector('.paste')
    expect(pre?.textContent).toContain('remote-port=16893')
    expect(pre?.textContent).toContain('remote-protocol=tls')
  })

  it('is a real <button> element, focusable and Enter/Space-activatable for free under HTML\'s own semantics (#718)', async () => {
    // jsdom does not implement a real browser's default action of firing
    // a click from Enter/Space on a <button> (that behaviour lives in the
    // browser's event loop, not in JS a test can trigger), so the
    // meaningful assertion here is the element itself: a native <button>
    // gets Enter/Space activation and focusability without any key
    // handler of this component's own -- a <div role="button"> would not.
    const { container, getByRole } = render(Entities)
    await settle()

    const trigger = getByRole('button', { name: 'Add a router' })
    expect(trigger).toBe(container.querySelector('.berth-trigger'))
    expect(trigger.tagName).toBe('BUTTON')
    expect(trigger.getAttribute('tabindex')).toBeNull() // native focusability, not a synthetic tabindex
  })

  it('closes the unfolded berth on Escape and returns focus to the trigger, without moving the table (#718)', async () => {
    const { container, getByRole } = render(Entities)
    await settle()

    await fireEvent.click(getByRole('button', { name: 'Add a router' }))
    await settle()
    expect(container.querySelector('.berth-panel')).toBeTruthy()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await settle()

    expect(container.querySelector('.berth-panel')).toBeNull()
    expect(container.querySelector('.etable')).toBeTruthy()
    expect(document.activeElement).toBe(container.querySelector('.berth-trigger'))
  })

  it('closes the unfolded berth from its own close control', async () => {
    const { container, getByRole } = render(Entities)
    await settle()

    await fireEvent.click(getByRole('button', { name: 'Add a router' }))
    await settle()

    await fireEvent.click(getByRole('button', { name: 'Close' }))
    await settle()

    expect(container.querySelector('.berth-panel')).toBeNull()
  })

  it('replaces the dashed "another router?" card and the old pill with the empty berth (#718)', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.fcard.add')).toBeNull()
    expect(container.textContent).not.toContain('another router?')
    expect(container.querySelector('.add-router-btn')).toBeNull()
    expect(container.querySelector('.fcard.berth')).toBeTruthy()
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

  it('marks a host carrying two open alarm-family flags with the same single ✱, not a doubled one (#718)', async () => {
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
      {
        id: 'f3',
        type: 'known_bad_ip',
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
    expect(row?.querySelector('.mk-flagged')?.textContent).toBe('✱ flagged')
  })

  // Round 38 removed the hint under every view on the owner's word
  // ("Remove all these little descriptor lines on the entities views, I
  // don't want them"). Asserted as an absence so it cannot creep back.
  it('prints no descriptor line under the table', async () => {
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.table-hint')).toBeNull()
    expect(container.querySelector('.oghint')).toBeNull()
    expect(container.textContent).not.toContain('a name is yours to give')
  })
})

// The one fact the round-30 device-status strip carried that had no
// home once that strip became the router cards: a router that pushes
// without being registered. Rounds 37-38 make it a state of the dashed
// third slot rather than one more card in the fleet.
describe('Entities unregistered router (#804, moved from #802)', () => {
  const unregistered = [
    {
      id: 'disc-10.0.50.1',
      name: '10.0.50.1',
      configured: false,
      status: 'live',
      firstSeen: new Date().toISOString(),
      lastSeen: new Date().toISOString(),
      sourceIp: '10.0.50.1',
      eventCount: 12,
      routerosVersion: '7.19.4',
    },
  ] as unknown as (typeof appState)['devices']

  it('draws it in the berth\'s slot, saying its lines are kept', async () => {
    appState.devices = unregistered
    const { container } = render(Entities)
    await settle()

    const card = container.querySelector('.fcard.unreg')
    expect(card).not.toBeNull()
    expect(card?.querySelector('.fhead b')?.textContent).toBe('10.0.50.1')
    expect(card?.querySelector('.fstate')?.textContent).toContain('PUSHING · UNREGISTERED')
    expect(card?.textContent).toContain('RouterOS 7.19.4')
    expect(card?.textContent).toContain('its lines are kept; it has no name and no zones until it is registered')
  })

  it('gives way from the berth rather than sitting beside it -- the slot says one thing or the other', async () => {
    appState.devices = unregistered
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.fcard.berth')).toBeNull()
  })

  it('leaves the berth in place when every router is registered', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 3 },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.fcard.unreg')).toBeNull()
    expect(container.querySelector('.fcard.berth')).not.toBeNull()
  })

  // It is not one of the fleet's cards: an unregistered router has no
  // name and no zones, so drawing it as a live router card would claim
  // both.
  it('is not drawn as an ordinary router card', async () => {
    appState.devices = unregistered
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.fcard.live')).toBeNull()
  })
})

describe('Entities views (#804, rounds 37-38)', () => {
  it('draws three views with their counts, not tabs, with hosts underlined by default', async () => {
    fetchEntities.mockResolvedValue([
      { type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] },
      { type: 'port', key: '443', label: 'HTTPS', tags: [] },
    ])
    const { container, queryByRole } = render(Entities)
    await settle()

    // Not tabs: no tablist, and no tab furniture.
    expect(queryByRole('tablist')).toBeNull()
    expect(container.querySelectorAll('.tab').length).toBe(0)

    const views = [...container.querySelectorAll('#eviews [data-v]')]
    expect(views.map((v) => v.getAttribute('data-v'))).toEqual(['hosts', 'rules', 'ports'])
    expect(views.map((v) => v.textContent?.replace(/\s+/g, ' ').trim())).toEqual(['hosts 1', 'rules 0', 'ports 1'])
    expect(views.filter((v) => v.classList.contains('on')).map((v) => v.getAttribute('data-v'))).toEqual(['hosts'])
  })

  it('switches the table under the views, keeping one table rather than three destinations', async () => {
    const { container } = render(Entities)
    await settle()

    const view = (name: string) => container.querySelector<HTMLButtonElement>(`#eviews [data-v="${name}"]`)!

    await fireEvent.click(view('ports'))
    await settle()
    expect([...container.querySelectorAll('.etable th')].map((th) => th.textContent)).toEqual([
      'name',
      'port',
      'last seen',
    ])
    expect(view('ports').classList.contains('on')).toBe(true)
    expect(view('hosts').classList.contains('on')).toBe(false)

    await fireEvent.click(view('hosts'))
    await settle()
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

  it('the views are buttons, so the keyboard reaches them', async () => {
    const { container } = render(Entities)
    await settle()

    for (const v of container.querySelectorAll('#eviews [data-v]')) {
      expect(v.tagName).toBe('BUTTON')
    }
  })
})

describe('Entities read-only viewer (#804)', () => {
  it('gives a viewer no rename affordance -- names are plain text, not buttons', async () => {
    authState.role = 'viewer'
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] }])
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.rename-btn')).toBeNull()
    const name = container.querySelector('.etable td.k')
    expect(name?.textContent?.trim()).toBe('tom-desktop')
    expect(name?.querySelector('.static-name')).not.toBeNull()
  })

  // The whole point of the ratified grammar: the fact is said once, on
  // the account chip, and this page says nothing about it at all.
  it('says nothing about read-only on the page itself', async () => {
    authState.role = 'viewer'
    const { container } = render(Entities)
    await settle()

    expect(container.textContent?.toLowerCase()).not.toContain('read-only')
    expect(container.textContent?.toLowerCase()).not.toContain('admin')
  })

  it('still lets a user rename -- read-only is the viewer tier only', async () => {
    authState.role = 'user'
    fetchEntities.mockResolvedValue([{ type: 'host', key: '10.0.10.2', label: 'tom-desktop', tags: [] }])
    const { container } = render(Entities)
    await settle()

    expect(container.querySelector('.rename-btn')).not.toBeNull()
  })
})

describe('Entities rules view (#681, reachable again since #804)', () => {
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
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
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
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
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
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
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
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
    await settle()

    expect(container.querySelector('.etable tbody')?.textContent).toContain(
      'No router has pushed a rule table yet — once one does, every rule it carries appears here, fired or not.',
    )
  })

  // #681 left an unlogged rule off the table entirely, reasoning that a
  // rule that can never fire under a label would be a dead end to show.
  // Rounds 37-38 overrule that: the rule is on the router and fires on
  // the router, so a rules view that omits it is not the rule table. It
  // appears, and it is simply not nameable.
  it('shows an unlogged rule by its router comment, with no rename affordance', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule({ log: false, logPrefix: '', comment: 'no logging on this one' })],
    })

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
    await settle()

    const row = container.querySelector('.etable tbody tr')
    expect(row?.textContent).toContain('no logging on this one')
    expect(row?.querySelector('.rename-btn')).toBeNull()
    expect(row?.querySelector('.static-name')).not.toBeNull()
  })

  // The drawn case: no comment on the router at all, so the rule has no
  // name to show and states its number instead of going blank.
  it('shows a rule with no comment on the router by its number', async () => {
    appState.devices = [
      { id: 'rb5009', name: 'rb5009', configured: true, status: 'live', lastSeen: new Date().toISOString(), sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']
    fetchRouterRules.mockResolvedValue({
      available: true,
      rules: [filterRule({ ordinal: 17, log: false, logPrefix: '', comment: '' })],
    })

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('button', { name: /^rules/ }))
    await settle()

    const row = container.querySelector('.etable tbody tr')
    expect(row?.textContent).toContain('#17 — no comment on the router')
    expect(row?.querySelector('.rename-btn')).toBeNull()
  })
})

describe('Entities ports view (#681, reachable again since #804)', () => {
  it('folds a discovered-but-unnamed port into the ports tab', async () => {
    appState.events = [
      { srcIp: '10.0.10.9', dstIp: '10.0.10.1', srcPort: 51413, dstPort: 443, time: new Date().toISOString(), receivedAt: Date.now() },
    ] as unknown as (typeof appState)['events']

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('button', { name: /^ports/ }))
    await settle()

    expect(container.textContent).toContain('51413')
    expect(container.textContent).toContain('443')
    // Rounds 37-38: a port nobody has named shows a dash, not an
    // instruction -- the descriptor lines went with the hint lines.
    expect(container.textContent).toContain('— · unnamed')
    expect(container.textContent).not.toContain('click to name')
  })

  it('shows an already-named port by its label', async () => {
    fetchEntities.mockResolvedValue([{ type: 'port', key: '8384', label: 'syncthing', tags: [] }])

    const { container, getByRole } = render(Entities)
    await settle()
    await fireEvent.click(getByRole('button', { name: /^ports/ }))
    await settle()

    const row = [...container.querySelectorAll('.etable tbody tr')].find((tr) => tr.textContent?.includes('syncthing'))
    expect(row).toBeTruthy()
    expect(row?.textContent).toContain('8384')
  })
})
