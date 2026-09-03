// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

const SYSLOG_COMMANDS =
  '/system logging action add name=mikroview target=remote remote=localhost remote-port=6514 ' +
  'remote-protocol=tls check-certificate=yes\n/system logging add topics=firewall,info action=mikroview'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { fetchSetupStatus, fetchSetupCommands, fetchDevices } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { journeyState } from '../lib/journey.svelte'
import JourneyAttach from './JourneyAttach.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(fetchSetupCommands).mockResolvedValue({
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: { commands: SYSLOG_COMMANDS, note: '' },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
  })
  authState.username = 'tom'
  journeyState.phase = 'attach'
  wizardState.status = null
  wizardState.commands = null
})

describe('JourneyAttach', () => {
  it('shows the real two RouterOS lines -- exactly what the server rendered', async () => {
    wizardState.status = {
      instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
      sources: [],
      devices: [],
      pushKinds: [],
      marks: [],
    }

    const { container } = render(JourneyAttach)

    await waitFor(() => {
      const shown = container.querySelector('.code')?.textContent ?? ''
      expect(shown).toBe(SYSLOG_COMMANDS)
    })
    // Never a placeholder -- the real host/port are already in it.
    expect(container.querySelector('.code')?.textContent).not.toContain('<mikroview-host>')
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
