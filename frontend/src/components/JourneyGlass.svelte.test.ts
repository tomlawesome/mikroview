// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { journeyState, CONNECTING_MS } from '../lib/journey.svelte'
import JourneyGlass from './JourneyGlass.svelte'

beforeEach(() => {
  authState.role = 'admin'
  journeyState.phase = 'connecting'
  wizardState.open = false
  wizardState.status = null
})

describe('JourneyGlass -- Connecting', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('moves on to the glass by itself after the beat', () => {
    vi.useFakeTimers()
    render(JourneyGlass)
    expect(screen.getByText('The pipe is coming alive.')).toBeTruthy()

    vi.advanceTimersByTime(CONNECTING_MS)
    expect(journeyState.phase).toBe('glass')
  })

  it('Skip ahead moves on immediately, for anyone who does not want to wait', async () => {
    render(JourneyGlass)
    await fireEvent.click(screen.getByRole('button', { name: 'Skip ahead' }))
    expect(journeyState.phase).toBe('glass')
  })
})

describe('JourneyGlass -- the glass', () => {
  beforeEach(() => {
    journeyState.phase = 'glass'
  })

  it('states the deck\'s own real card count, never a hardcoded one', () => {
    render(JourneyGlass)
    expect(screen.getByText(new RegExp(`${journeyState.cards.length} cards`))).toBeTruthy()
  })

  it('begin the tour starts touring', async () => {
    render(JourneyGlass)
    await fireEvent.click(screen.getByRole('button', { name: 'begin the tour' }))
    expect(journeyState.phase).toBe('touring')
  })

  it('skipping straight to the wizard ends the journey and opens it', async () => {
    render(JourneyGlass)
    await fireEvent.click(screen.getByRole('button', { name: /skip straight to the wizard/ }))
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })
})
