// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { journeyState } from '../lib/journey.svelte'
import JourneyTour from './JourneyTour.svelte'

beforeEach(() => {
  authState.role = 'admin'
  wizardState.open = false
  wizardState.status = null
  journeyState.beginTour()
})

describe('JourneyTour', () => {
  it('walks the deck\'s real card list, first card first -- "THE FALL · 1 OF N"', () => {
    const total = journeyState.cards.length
    render(JourneyTour)
    expect(screen.getByText(`THE FALL · 1 OF ${total}`)).toBeTruthy()
  })

  // Round 29's ratified shape: the fall's three rings, verbatim.
  it('rings the fall\'s key handles with concise labels', () => {
    render(JourneyTour)
    expect(screen.getByText('the brink — now arrives here')).toBeTruthy()
    expect(screen.getByText('a band per boundary — click reaches in')).toBeTruthy()
    expect(screen.getByText('the held hour — scroll looks back')).toBeTruthy()
  })

  it('advances card by card on next', async () => {
    const total = journeyState.cards.length
    render(JourneyTour)

    await fireEvent.click(screen.getByRole('button', { name: /next/ }))
    expect(journeyState.cardIndex).toBe(1)
    expect(screen.getByText(`TOPOGRAPHY · 2 OF ${total}`)).toBeTruthy()
  })

  it('the last card offers finish, not next, and ends the journey there', async () => {
    const total = journeyState.cards.length
    for (let i = 0; i < total - 1; i++) journeyState.nextCard()

    render(JourneyTour)
    expect(screen.getByRole('button', { name: /finish/ })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /finish/ }))
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })

  it('can be left at any time, and still ends at the wizard', async () => {
    render(JourneyTour)
    await fireEvent.click(screen.getByRole('button', { name: 'leave the tour' }))
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })
})
