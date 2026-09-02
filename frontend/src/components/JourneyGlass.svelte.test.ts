// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { journeyState, tourLengthSentence } from '../lib/journey.svelte'
import type { ClientEvent } from '../lib/types'
import JourneyGlass from './JourneyGlass.svelte'

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

const STATUS = {
  instance: { tlsEnabled: true, hosts: ['mv.example'], syslogPort: ':6514', syslogEnabled: true },
  sources: [],
  devices: [],
  pushKinds: [],
  marks: [],
}

beforeEach(() => {
  authState.role = 'admin'
  journeyState.phase = 'connecting'
  appState.events = []
  wizardState.open = false
  wizardState.status = null
})

// #750 B1: nothing may claim the pipe is alive until a line has landed.
describe('JourneyGlass -- waiting for the first line', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('says nothing has arrived, and no clock moves it on', () => {
    vi.useFakeTimers()
    render(JourneyGlass)
    expect(screen.getByText('Nothing has arrived yet.')).toBeTruthy()

    vi.advanceTimersByTime(60_000)
    expect(journeyState.phase).toBe('connecting')
    expect(screen.queryByText('MikroView is flowing.')).toBeNull()
  })

  it('keeps the two router lines up, so they can still be pasted', () => {
    wizardState.status = STATUS
    render(JourneyGlass)
    const code = document.querySelector('.glass .code')
    expect(code?.textContent).toContain('/system logging action add name=mikroview')
    expect(code?.textContent).toContain('/system logging add topics=firewall')
  })

  it('offers the wizard rather than a way to skip into an untrue claim', async () => {
    render(JourneyGlass)
    expect(screen.queryByRole('button', { name: 'Skip ahead' })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: /skip straight to the wizard/ }))
    expect(journeyState.phase).toBe('idle')
    expect(wizardState.open).toBe(true)
  })

  it('opens the glass the moment a real event lands', async () => {
    render(JourneyGlass)
    appState.events = [firstEvent()]
    await new Promise((r) => setTimeout(r, 0))
    expect(journeyState.phase).toBe('glass')
  })
})

describe('JourneyGlass -- the glass', () => {
  beforeEach(() => {
    journeyState.phase = 'glass'
  })

  it('states the deck\'s own real card count and a length that follows it', () => {
    render(JourneyGlass)
    expect(screen.getByText(tourLengthSentence(journeyState.cards.length))).toBeTruthy()
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
