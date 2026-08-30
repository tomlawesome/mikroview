// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { fetchSetupStatus, fetchDevices } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { journeyState } from '../lib/journey.svelte'
import { syslogCommands } from '../lib/setupsteps'
import JourneyAttach from './JourneyAttach.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  authState.username = 'tom'
  journeyState.phase = 'attach'
  wizardState.status = null
})

describe('JourneyAttach', () => {
  it('shows the real two RouterOS lines -- exactly what setupsteps.ts emits', () => {
    wizardState.status = {
      instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
      sources: [],
      devices: [],
      pushKinds: [],
      marks: [],
    }

    const { container } = render(JourneyAttach)

    const shown = container.querySelector('.code')?.textContent ?? ''
    expect(shown).toBe(syslogCommands(wizardState.address, ':6514'))
    // Never a placeholder -- the real host/port are already in it.
    expect(shown).not.toContain('<mikroview-host>')
  })

  it('asks for its own status when nothing has loaded one yet', () => {
    render(JourneyAttach)
    expect(fetchSetupStatus).toHaveBeenCalled()
    expect(fetchDevices).toHaveBeenCalled()
    expect(screen.getByText(/Fetching this instance/)).toBeTruthy()
  })

  it('Continue moves the journey on to Connecting', async () => {
    wizardState.status = {
      instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
      sources: [],
      devices: [],
      pushKinds: [],
      marks: [],
    }
    render(JourneyAttach)

    await fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    expect(journeyState.phase).toBe('connecting')
  })
})
