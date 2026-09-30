// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', async (orig) => ({
  ...(await orig<typeof import('../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  fetchMyPreferences: vi.fn().mockResolvedValue({ version: 1, prefs: {} }),
  saveMyPreferences: vi.fn().mockResolvedValue(null),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { tourState } from '../lib/tour.svelte'
import JourneyTour from './JourneyTour.svelte'

beforeEach(() => {
  authState.role = 'admin'
  wizardState.open = false
  wizardState.status = null
  tourState.reset()
  // Started from Metrics, as the account menu would: the tour ends back
  // here.
  appState.view = 'metrics'
  tourState.begin()
})

describe('JourneyTour', () => {
  it('walks the deck\'s real card list, first card first -- "THE FALL · 1 OF N"', () => {
    const total = tourState.cards.length
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
    const total = tourState.cards.length
    render(JourneyTour)

    await fireEvent.click(screen.getByRole('button', { name: /next/ }))
    expect(tourState.cardIndex).toBe(1)
    expect(screen.getByText(`TOPOGRAPHY · 2 OF ${total}`)).toBeTruthy()
  })

  it('the last card offers finish, not next, and ends back where the tour started', async () => {
    const total = tourState.cards.length
    for (let i = 0; i < total - 1; i++) tourState.nextCard()

    render(JourneyTour)
    expect(screen.getByRole('button', { name: /finish/ })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /finish/ }))
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('metrics')
    // Never the wizard: that was the retired #646 walk's ending.
    expect(wizardState.open).toBe(false)
  })

  it('can be left at any time, and ends back where it started too', async () => {
    render(JourneyTour)
    await fireEvent.click(screen.getByRole('button', { name: 'leave the tour' }))
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('metrics')
    expect(wizardState.open).toBe(false)
  })

  it('walks a viewer\'s own six cards, Fleet last', async () => {
    tourState.reset()
    authState.role = 'viewer'
    appState.view = 'fall'
    tourState.begin()
    render(JourneyTour)
    expect(screen.getByText('THE FALL · 1 OF 6')).toBeTruthy()

    for (let i = 0; i < 5; i++) await fireEvent.click(screen.getByRole('button', { name: /next/ }))
    expect(screen.getByText('FLEET · 6 OF 6')).toBeTruthy()
    expect(appState.view).toBe('fleet')

    await fireEvent.click(screen.getByRole('button', { name: /finish/ }))
    expect(tourState.active).toBe(false)
    expect(appState.view).toBe('fall')
  })
})
