// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { ROUTER_STEPS } from '../lib/setupsteps'
import { wizardState } from '../lib/wizard.svelte'
import type { RefusedSender } from '../lib/types'
import Fleet from './Fleet.svelte'

// Fleet reads appState.devices directly; the one request of its own is
// the refused-senders list (#1281), faked at the module boundary.
const fetchRefusedSenders = vi.fn(async (): Promise<RefusedSender[]> => [])
vi.mock('../lib/api', () => ({
  fetchRefusedSenders: () => fetchRefusedSenders(),
}))

function device(overrides: Record<string, unknown>) {
  return {
    id: 'r1',
    name: 'router1',
    configured: true,
    status: 'live',
    lastSeen: new Date(Date.now() - 60_000).toISOString(),
    sourceIp: '10.0.0.1',
    eventCount: 3,
    routerosVersion: '7.15',
    ...overrides,
  }
}

function setDevices(list: ReturnType<typeof device>[]) {
  appState.devices = list as unknown as (typeof appState)['devices']
}

// #549's Loading and first-run chrome states, applied to Fleet the same
// way LiveTable.svelte.test.ts covers them for the live view: a
// zero-device view is either "the app's one loadInitial() call hasn't
// come back yet" (ghost rows) or "it has, and mikroview has genuinely
// never seen a device" (the first-run pointer). Fleet has no admin gate
// of its own, so the viewer-vs-admin wording split matters here too.
describe('Fleet Loading and first-run empty states (#549)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = ''
    flagsState.list = []
  })

  it('shows ghost rows while the initial fetch has not settled yet', () => {
    appState.initialLoadDone = false
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.ghost-rows')).not.toBeNull()
    expect(container.querySelector('.empty')).toBeNull()
  })

  it('points an admin at your account menu ▸ Run setup… once settled with no devices ever seen', () => {
    authState.role = 'admin'
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.empty')?.textContent).toMatch(/your account menu ▸ Run setup…/)
  })

  it('tells a viewer to ask an administrator instead', () => {
    authState.role = 'viewer'
    const { container } = render(Fleet)
    flushSync()

    const text = container.querySelector('.empty')?.textContent ?? ''
    expect(text).not.toMatch(/Run setup…/)
    expect(text.toLowerCase()).toMatch(/administrator/)
  })
})

// #657/#706: the viewer's Fleet is a card of round 30's deck, so it
// wears the deck's identity -- Entities' own router cards (#675/#718),
// not the retired table-page frame.
describe('Fleet deck identity (#657/#706)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = 'viewer'
    flagsState.list = []
  })

  it('renders router cards, never the old table', () => {
    setDevices([device({})])
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.fcard')).not.toBeNull()
    expect(container.querySelector('table')).toBeNull()
  })

  it('carries no page heading, and no add-router affordance for a viewer', () => {
    setDevices([device({})])
    const { container } = render(Fleet)
    flushSync()

    // #697: the row's label is the .og h3, never an h1/h2 page heading.
    expect(container.querySelector('h1, h2')).toBeNull()
    // #657's grammar: adding a router is a change, so the affordance is
    // absent, not disabled -- no berth, no button but the flag door.
    expect(container.querySelector('.berth')).toBeNull()
    for (const b of container.querySelectorAll('button')) {
      expect(b.textContent?.toLowerCase()).not.toMatch(/add|re-enrol/)
    }
  })

  it('states status as a mark plus a written label, never colour alone (#616)', () => {
    setDevices([
      device({ id: 'r1', name: 'alpha', status: 'live' }),
      device({ id: 'r2', name: 'beta', status: 'stale', lastSeen: new Date(Date.now() - 3_600_000).toISOString() }),
      device({ id: 'r3', name: 'gamma', status: 'never_seen', lastSeen: '', sourceIp: '', eventCount: 0 }),
    ])
    const { container } = render(Fleet)
    flushSync()

    const states = [...container.querySelectorAll('.fstate')].map((el) => el.textContent?.trim())
    expect(states).toContain('● LIVE')
    expect(states).toContain('◌ QUIET')
    expect(states).toContain('◌ NEVER SEEN')
  })

  it('reads a quiet router as a fact, not a fault', () => {
    setDevices([device({ status: 'stale', lastSeen: new Date(Date.now() - 3_600_000).toISOString() })])
    const { container } = render(Fleet)
    flushSync()

    expect(container.textContent).toMatch(/quiet is a fact, not a fault/)
  })

  it('names an unregistered device as seen on the wire, not in the devices config', () => {
    setDevices([device({ configured: false })])
    const { container } = render(Fleet)
    flushSync()

    expect(container.textContent).toMatch(/seen on the wire, not in the devices config/)
  })

  // #442's echo: the fleet already shows the pair -- a declared router
  // that has sent nothing, an unregistered one streaming -- so the
  // configured-silent card carries one sentence pointing at the wizard,
  // where the command is printed. Nothing here diagnoses; the card
  // states what was declared and what arrived.
  it('echoes the source-address split on the configured-silent card, pointing at step 2', () => {
    setDevices([
      device({
        id: 'office',
        name: 'office',
        status: 'never_seen',
        lastSeen: '',
        sourceIp: '192.168.88.1',
        eventCount: 0,
        multihomedCandidates: ['10.0.20.1'],
      }),
      device({ id: '10.0.20.1', name: '10.0.20.1', configured: false, sourceIp: '10.0.20.1' }),
    ])
    const { container } = render(Fleet)
    flushSync()

    const cards = [...container.querySelectorAll('.fcard')]
    const office = cards.find((c) => c.textContent?.includes('office'))
    expect(office?.textContent).toContain(
      'Declared as 192.168.88.1, nothing arrived. If 10.0.20.1 below is the same router on another of its addresses, Run setup… step 2 shows the one-line fix.',
    )
    const arriving = cards.find((c) => c.textContent?.includes('seen on the wire'))
    expect(arriving?.textContent).not.toContain('Declared as')
  })

  it('carries no echo once the declared router is the one sending', () => {
    setDevices([device({ id: 'office', name: 'office', sourceIp: '192.168.88.1' })])
    const { container } = render(Fleet)
    flushSync()

    expect(container.textContent).not.toContain('Declared as')
  })

  it('gives an active silence flag a real door into the docket', async () => {
    setDevices([device({ status: 'stale', lastSeen: new Date(Date.now() - 3_600_000).toISOString() })])
    flagsState.list = [
      { id: 'f1', type: 'device_silence', target: 'r1', cleared: false },
    ] as unknown as (typeof flagsState)['list']
    const { container } = render(Fleet)
    flushSync()

    const door = container.querySelector<HTMLButtonElement>('.flag-door')
    expect(door).not.toBeNull()
    expect(door?.getAttribute('aria-label')).toMatch(/router1/)
    door?.click()
    flushSync()
    expect(appState.view).toBe('flags')
  })

  it('draws no flag door for a cleared flag', () => {
    setDevices([device({})])
    flagsState.list = [
      { id: 'f1', type: 'device_silence', target: 'r1', cleared: true },
    ] as unknown as (typeof flagsState)['list']
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.flag-door')).toBeNull()
  })
})

// #1284: adding a router is the wizard's own router steps, opened from
// the fleet -- and #657's grammar still holds, so both actions are drawn
// for an admin and are simply not there for anyone else.
describe('Fleet -- Add a router and Re-enrol… (#1284)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = 'admin'
    flagsState.list = []
    fetchRefusedSenders.mockResolvedValue([])
    wizardState.reset()
  })

  it('opens the router ledger at Name your router from the header row', () => {
    setDevices([device({})])
    const { getByRole } = render(Fleet)
    flushSync()

    getByRole('button', { name: 'Add a router' }).click()
    flushSync()

    expect(wizardState.open).toBe(true)
    expect(wizardState.steps).toEqual(ROUTER_STEPS)
    expect(wizardState.pane).toBe(1)
    expect(wizardState.ledgerDevice).toBe('')
    // Opened from the fleet, so the finish leads back to it.
    expect(wizardState.finishTo).toBe('fleet')
  })

  it('opens the same ledger at Send logs for one router, from its own row', () => {
    setDevices([device({ id: 'edge-1', name: 'edge-1' })])
    const { getByLabelText } = render(Fleet)
    flushSync()

    getByLabelText(/Re-enrol edge-1/).click()
    flushSync()

    expect(wizardState.open).toBe(true)
    expect(wizardState.steps).toEqual(ROUTER_STEPS)
    // Send logs is the second step of the router ledger.
    expect(wizardState.pane).toBe(2)
    expect(wizardState.ledgerDevice).toBe('edge-1')
  })

  it('draws neither action for a viewer', () => {
    authState.role = 'viewer'
    setDevices([device({})])
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.og-action')).toBeNull()
    expect(container.querySelector('.row-action')).toBeNull()
  })
})

// The refused-senders strip (#1281): under the list, present only when
// it has something to say, and carrying no accept control by ruling.
describe('Fleet -- refused senders (#1281)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = 'admin'
    flagsState.list = []
    wizardState.reset()
  })

  it('is absent entirely when nothing has been refused', async () => {
    fetchRefusedSenders.mockResolvedValue([])
    setDevices([device({})])
    const { container } = render(Fleet)
    await vi.waitFor(() => expect(fetchRefusedSenders).toHaveBeenCalled())
    flushSync()

    expect(container.querySelector('.og.refused')).toBeNull()
    expect(container.textContent).not.toContain('Refused senders')
  })

  it('names each address, its lines and when it was last seen -- and offers no way to accept it', async () => {
    fetchRefusedSenders.mockResolvedValue([
      { ip: '192.168.88.1', firstSeen: '2026-09-19T14:03:00Z', lastSeen: '2026-09-19T14:09:00Z', lines: 12 },
    ])
    setDevices([device({})])
    const { container } = render(Fleet)
    await vi.waitFor(() => expect(container.querySelector('.og.refused')).not.toBeNull())
    flushSync()

    const strip = container.querySelector('.og.refused')
    expect(strip?.textContent).toContain(
      'Refused senders — logs from an address that is not enrolled are dropped.',
    )
    expect(strip?.textContent).toContain('192.168.88.1')
    expect(strip?.textContent).toContain('12 lines')
    // No accept control, by ruling: an address is accepted only by a
    // router presenting a token.
    expect(strip?.querySelector('button')).toBeNull()
    expect(strip?.textContent?.toLowerCase()).not.toContain('accept')
  })
})
