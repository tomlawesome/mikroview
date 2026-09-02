// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { appState } from './state.svelte'
import { authState } from './auth.svelte'
import { wizardState } from './wizard.svelte'
import { journeyState, tourLengthSentence, tourMinutes } from './journey.svelte'
import type { ClientEvent } from './types'

// One line off the wire -- the whole of what beat 3 waits for.
function firstEvent(): ClientEvent {
  return {
    id: 1,
    time: '2026-01-01T13:46:00.000Z',
    deviceId: 'core',
    sourceIp: '192.168.1.1',
    action: 'drop',
    ruleLabel: 'r13',
    chain: 'forward',
    raw: '',
    receivedAt: 0,
  }
}

beforeEach(() => {
  authState.role = 'admin'
  appState.view = 'engineroom'
  appState.events = []
  journeyState.phase = 'idle'
  journeyState.cardIndex = 0
  wizardState.open = false
  wizardState.status = null
})

describe('journeyState', () => {
  it('derives the card count from the deck itself, admin or not', () => {
    authState.role = 'admin'
    // fall, topography, metrics, live, docket, entities, engineroom (#647).
    expect(journeyState.cards.length).toBe(7)

    authState.role = 'user'
    // Entities keeps its own admin gate -- absent, not present-and-broken.
    expect(journeyState.cards.length).toBe(6)
  })

  it('begin() opens on Attach', () => {
    journeyState.begin()
    expect(journeyState.phase).toBe('attach')
    expect(journeyState.active).toBe(true)
  })

  it('fromAttach() rolls to the fall and moves to Connecting', () => {
    journeyState.begin()
    journeyState.fromAttach()
    expect(journeyState.phase).toBe('connecting')
    expect(appState.view).toBe('fall')
  })

  it('fromConnecting() moves to the glass', () => {
    journeyState.begin()
    journeyState.fromAttach()
    journeyState.fromConnecting()
    expect(journeyState.phase).toBe('glass')
  })

  // #750 B1: beat 3 is gated on a line actually landing, so `arrived`
  // reads the client's own event buffer and nothing else -- no clock,
  // no "the router probably has it by now".
  it('arrived() is false until an event is actually in the buffer', () => {
    appState.events = []
    expect(journeyState.arrived).toBe(false)

    appState.events = [firstEvent()]
    expect(journeyState.arrived).toBe(true)
  })

  it('beginTour() starts touring on the deck\'s first card', () => {
    journeyState.beginTour()
    expect(journeyState.phase).toBe('touring')
    expect(journeyState.cardIndex).toBe(0)
    expect(appState.view).toBe(journeyState.cards[0].views[0])
  })

  it('nextCard() walks the deck one card at a time', () => {
    journeyState.beginTour()
    journeyState.nextCard()
    expect(journeyState.cardIndex).toBe(1)
    expect(appState.view).toBe(journeyState.cards[1].views[0])
  })

  // Round 27's "it ends at the wizard either way": finishing the walk,
  // skipping it from the glass, and leaving it partway all hand off to
  // the wizard the same way.
  it('finishing the last card hands off to the wizard and ends the journey', () => {
    journeyState.beginTour()
    const last = journeyState.cards.length - 1
    for (let i = 0; i < last; i++) journeyState.nextCard()
    expect(journeyState.cardIndex).toBe(last)

    journeyState.nextCard()
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })

  it('skipToWizard() from the glass hands off the same way', () => {
    journeyState.begin()
    journeyState.fromAttach()
    journeyState.fromConnecting()
    journeyState.skipToWizard()
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })

  it('leaveTour() partway through hands off the same way', () => {
    journeyState.beginTour()
    journeyState.leaveTour()
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })

  // The hand-off spends wizard.svelte.ts's own once-only auto-launch
  // slot, so the ordinary first-run check does not reopen what the
  // journey itself just opened and the operator then closed. Status is
  // set to exactly the conditions that would otherwise re-trigger it
  // (no devices, no marks) -- the point is proving the slot was spent,
  // not that the check had nothing to answer.
  it('spends the ordinary auto-launch slot on hand-off', () => {
    wizardState.status = {
      instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
      sources: [],
      devices: [],
      pushKinds: [],
      marks: [],
    }

    journeyState.beginTour()
    journeyState.leaveTour()
    wizardState.close()

    wizardState.maybeAutoLaunch(false)
    expect(wizardState.open).toBe(false)
  })
})

// Round 27 draws "Six cards. About two minutes." -- and the deck it is
// describing is six for a viewer, seven for an admin (#647), so the
// sentence has to move with the count instead of being fixed prose.
describe('tourLengthSentence', () => {
  it('says exactly what round 27 draws, for the deck round 27 drew', () => {
    expect(tourLengthSentence(6)).toBe('Six cards. About two minutes. It ends at the wizard either way.')
  })

  it('scales the minutes with the count rather than fixing them at two', () => {
    expect(tourMinutes(6)).toBe(2)
    expect(tourMinutes(9)).toBe(3)
    expect(tourLengthSentence(9)).toContain('About three minutes.')
  })

  it('never rounds a short deck down to no time at all', () => {
    expect(tourMinutes(1)).toBe(1)
    expect(tourLengthSentence(1)).toBe('One card. About a minute. It ends at the wizard either way.')
  })
})
