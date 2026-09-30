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
import { preferencesState } from '../lib/preferences.svelte'
import { tourState } from '../lib/tour.svelte'
import TourOffer from './TourOffer.svelte'

beforeEach(() => {
  authState.role = 'admin'
  appState.view = 'fall'
  tourState.reset()
  preferencesState.seedForTest({})
  tourState.offering = true
})

describe('TourOffer', () => {
  it('asks in the menu row\'s own words, and says how long and where it ends', () => {
    render(TourOffer)
    expect(screen.getByRole('dialog', { name: 'Take the tour?' })).toBeTruthy()
    expect(screen.getByText('Eight cards. About three minutes. It ends back here, on the fall.')).toBeTruthy()
  })

  it('sizes the promise to the deck the signed-in tier can see', () => {
    authState.role = 'viewer'
    render(TourOffer)
    expect(screen.getByText('Six cards. About two minutes. It ends back here, on the fall.')).toBeTruthy()
  })

  it('"begin the tour" starts it on the fall and takes the offer down', async () => {
    render(TourOffer)
    await fireEvent.click(screen.getByRole('button', { name: 'begin the tour' }))
    expect(tourState.active).toBe(true)
    expect(tourState.cardIndex).toBe(0)
    expect(appState.view).toBe('fall')
    expect(tourState.offering).toBe(false)
    expect(preferencesState.get('tour')).toEqual({ offered: true })
  })

  it('"not now" takes it down without starting anything, and says where it lives', async () => {
    render(TourOffer)
    await fireEvent.click(screen.getByRole('button', { name: /not now — it stays in the account menu/ }))
    expect(tourState.active).toBe(false)
    expect(tourState.offering).toBe(false)
    expect(preferencesState.get('tour')).toEqual({ offered: true })
  })

  it('Escape is "not now"', async () => {
    render(TourOffer)
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(tourState.offering).toBe(false)
    expect(tourState.active).toBe(false)
    expect(preferencesState.get('tour')).toEqual({ offered: true })
  })
})
