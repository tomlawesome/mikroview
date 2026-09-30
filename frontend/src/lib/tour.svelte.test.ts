// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', async (orig) => ({
  ...(await orig<typeof import('./api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  fetchMyPreferences: vi.fn().mockResolvedValue({ version: 1, prefs: {} }),
  saveMyPreferences: vi.fn().mockResolvedValue(null),
}))

import { appState } from './state.svelte'
import { authState } from './auth.svelte'
import { preferencesState } from './preferences.svelte'
import { wizardState } from './wizard.svelte'
import { wizardJourney } from './wizardJourney.svelte'
import { wizardRun } from './wizardRun.svelte'
import { OFFER_DELAY_MS, tourState, tourLengthSentence, tourMinutes } from './tour.svelte'

beforeEach(() => {
  vi.useFakeTimers()
  authState.role = 'admin'
  appState.view = 'engineroom'
  wizardState.open = false
  wizardState.status = null
  wizardJourney.end()
  wizardJourney.wayOutHandler = null
  tourState.reset()
  preferencesState.seedForTest({})
})

afterEach(() => {
  vi.useRealTimers()
})

describe('tourState', () => {
  // The same table and the same two gates Deck.svelte renders from --
  // a user-tier account gets the user tier's deck, not a viewer's.
  it('derives the card count from the deck itself, per tier', () => {
    authState.role = 'admin'
    // fall, topography, metrics, live, docket, entities, engineroom
    // (#647), log-every-rule (#1134).
    expect(tourState.cards.length).toBe(8)

    authState.role = 'user'
    expect(tourState.cards.length).toBe(8)

    authState.role = 'viewer'
    // Fleet stands in for Entities and Settings (#657); no Log every rule.
    expect(tourState.cards.length).toBe(6)
    expect(tourState.cards.map((c) => c.key)).toEqual(['fall', 'topography', 'metrics', 'live', 'docket', 'fleet'])
  })

  it('begin() starts on the deck\'s first card', () => {
    tourState.begin()
    expect(tourState.active).toBe(true)
    expect(tourState.cardIndex).toBe(0)
    expect(appState.view).toBe(tourState.cards[0].views[0])
  })

  it('nextCard() walks the deck one card at a time', () => {
    tourState.begin()
    tourState.nextCard()
    expect(tourState.cardIndex).toBe(1)
    expect(appState.view).toBe(tourState.cards[1].views[0])
  })

  // The tour is a detour: finishing it and leaving it both put the
  // operator back on the card they started from.
  it('finishing the last card ends the tour back where it started', () => {
    appState.view = 'metrics'
    tourState.begin()
    const last = tourState.cards.length - 1
    for (let i = 0; i < last; i++) tourState.nextCard()
    expect(tourState.cardIndex).toBe(last)

    tourState.nextCard()
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('metrics')
    expect(wizardState.open).toBe(false)
  })

  it('leave() partway through ends back where it started too', () => {
    appState.view = 'live'
    tourState.begin()
    tourState.nextCard()
    tourState.leave()
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('live')
  })

  it('a viewer can take the whole tour', () => {
    authState.role = 'viewer'
    appState.view = 'fall'
    tourState.begin()
    for (let i = 0; i < 5; i++) tourState.nextCard()
    expect(appState.view).toBe('fleet')
    tourState.nextCard()
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('fall')
  })
})

describe('the offer after Finish', () => {
  it('rises a beat after the way out has landed, not at once', () => {
    tourState.offerAfterFinish()
    expect(tourState.offering).toBe(false)
    vi.advanceTimersByTime(OFFER_DELAY_MS - 1)
    expect(tourState.offering).toBe(false)
    vi.advanceTimersByTime(1)
    expect(tourState.offering).toBe(true)
  })

  it('taken: starts the tour on the fall and spends the offer for this account', () => {
    appState.view = 'fall'
    tourState.offerAfterFinish()
    vi.runAllTimers()
    tourState.acceptOffer()

    expect(tourState.offering).toBe(false)
    expect(tourState.active).toBe(true)
    expect(preferencesState.get('tour')).toEqual({ offered: true })

    tourState.offerAfterFinish()
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })

  it('declined: spends the offer the same way', () => {
    tourState.offerAfterFinish()
    vi.runAllTimers()
    tourState.declineOffer()

    expect(tourState.offering).toBe(false)
    expect(tourState.active).toBe(false)
    expect(preferencesState.get('tour')).toEqual({ offered: true })

    tourState.offerAfterFinish()
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })

  // A reload hydrates the account's record; an offer already answered
  // never comes back.
  it('is never made again once the account\'s record says it was answered', () => {
    preferencesState.seedForTest({ tour: { offered: true } })
    tourState.offerAfterFinish()
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })

  it('an unanswered offer is forgotten on sign-out', () => {
    tourState.offerAfterFinish()
    tourState.reset()
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })

  it('is not made while a tour is already running', () => {
    tourState.begin()
    tourState.offerAfterFinish()
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })
})

// wizardRun.finish() is what makes the offer: after the way out where
// it plays, straight after the hand-over where it cannot (no canvas
// mounted here, so wayOut() reports false and finish swaps outright).
describe('wizardRun.finish()', () => {
  it('offers the tour once a first-run walk has landed on the fall', () => {
    wizardState.open = true
    wizardState.finishTo = 'fall'
    wizardRun.finish()

    expect(wizardState.open).toBe(false)
    expect(appState.view).toBe('fall')
    vi.runAllTimers()
    expect(tourState.offering).toBe(true)
  })

  it('never offers it after an add-a-router walk, which lands on the fleet', () => {
    wizardState.open = true
    wizardState.finishTo = 'fleet'
    wizardRun.finish()

    expect(appState.view).toBe('fleet')
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)
  })

  it('waits for the way out to land where it plays', () => {
    let landed: (() => void) | null = null
    wizardJourney.wayOutHandler = (swap, after) => {
      swap()
      landed = after
      return true
    }
    wizardState.open = true
    wizardState.finishTo = 'fall'
    wizardRun.finish()

    expect(wizardState.open).toBe(false)
    vi.runAllTimers()
    expect(tourState.offering).toBe(false)

    landed!()
    vi.runAllTimers()
    expect(tourState.offering).toBe(true)
  })
})

// Round 27 draws "Six cards. About two minutes." -- and the deck it is
// describing is six for a viewer, eight for an admin now, so the
// sentence has to move with the count instead of being fixed prose.
describe('tourLengthSentence', () => {
  it('says what round 27 drew, for the deck round 27 drew, ending on the fall', () => {
    expect(tourLengthSentence(6)).toBe('Six cards. About two minutes. It ends back here, on the fall.')
  })

  it('scales the minutes with the count rather than fixing them at two', () => {
    expect(tourMinutes(6)).toBe(2)
    expect(tourMinutes(9)).toBe(3)
    expect(tourLengthSentence(9)).toContain('About three minutes.')
  })

  it('never rounds a short deck down to no time at all', () => {
    expect(tourMinutes(1)).toBe(1)
    expect(tourLengthSentence(1)).toBe('One card. About a minute. It ends back here, on the fall.')
  })
})
