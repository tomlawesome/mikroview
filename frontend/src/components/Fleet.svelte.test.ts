// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { ROUTER_STEPS } from '../lib/setupsteps'
import { wizardState } from '../lib/wizard.svelte'
import Fleet from './Fleet.svelte'

// Fleet reads appState.devices directly and asks the server for nothing
// of its own: Re-enrol… and the refused senders moved to Entities, the
// surface an admin's deck actually draws (#785). The mock stays so an
// accidental new call fails loudly rather than hitting fetch.
vi.mock('../lib/api', () => ({}))

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

  // #1372: the address line under the router's name -- acceptedIp when
  // enrolled, else sourceIp, else an honest "no address yet".
  describe('the address line under the name', () => {
    it('shows acceptedIp when the router has enrolled', () => {
      setDevices([device({ id: 'r1', name: 'alpha', sourceIp: '192.168.1.1', acceptedIp: '192.168.1.5' })])
      const { container } = render(Fleet)
      flushSync()

      const card = container.querySelector('.fcard')
      expect(card?.textContent).toContain('192.168.1.5')
      expect(card?.textContent).not.toContain('192.168.1.1')
    })

    it('falls back to sourceIp when nothing has enrolled', () => {
      setDevices([device({ id: 'r1', name: 'alpha', sourceIp: '192.168.1.1', acceptedIp: undefined })])
      const { container } = render(Fleet)
      flushSync()

      expect(container.querySelector('.fcard')?.textContent).toContain('192.168.1.1')
    })

    it('says "no address yet" when neither is known', () => {
      setDevices([device({ id: 'r1', name: 'alpha', sourceIp: '', acceptedIp: undefined })])
      const { container } = render(Fleet)
      flushSync()

      expect(container.querySelector('.fcard')?.textContent).toContain('no address yet')
    })
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
      'Declared as 192.168.88.1, nothing arrived. If 10.0.20.1 below is the same router on another of its addresses, Run setup… ▸ Send logs shows the one-line fix.',
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
// the fleet -- and #657's grammar still holds, so the action is drawn
// for an admin and is simply not there for anyone else. Re-enrol… and
// the refused senders are Entities' now (#785: an admin's deck answers
// the fleet view with that card and never draws this one).
describe('Fleet -- Add a router (#1284)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = 'admin'
    flagsState.list = []
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

  it('draws no action for a viewer', () => {
    authState.role = 'viewer'
    setDevices([device({})])
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.og-action')).toBeNull()
    expect(container.querySelector('.row-action')).toBeNull()
  })
})
