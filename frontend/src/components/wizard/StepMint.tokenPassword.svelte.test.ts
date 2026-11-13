// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 33): the wizard's mint step already asks for the admin's
// password; gauntlet's POST /api/tokens now takes it too (ASVS 7.5.3: a token
// outlives the session that minted it), so the ingest token's mint must send
// the same password. createToken(name, kind, device, password).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'

// jsdom has no matchMedia, which lib/viewport.svelte.ts reads at module
// load somewhere down the import chain.
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

// Only the network boundary is faked: the step's own markup, the run
// and wizardState are real, so what is checked is the calls the modal
// used to make -- POST /api/devices for the record, then POST
// /api/devices/{id}/enrolment with the password and the address, and
// (#1383) POST /api/tokens for the ingest token push/backup need,
// minted inside this same act.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  fetchSetupCommands: vi.fn(),
  createDevice: vi.fn(),
  createToken: vi.fn(),
  mintEnrolment: vi.fn(),
}))

import { createDevice, createToken, fetchDevices, fetchSetupCommands, fetchSetupStatus, mintEnrolment } from '../../lib/api'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import { rowState } from '../../lib/wizardRun'
import type { Device, SetupStatus } from '../../lib/types'
import StepMint from './StepMint.svelte'

function status(): SetupStatus {
  return {
    instance: {
      tlsEnabled: true,
      hosts: [],
      syslogPort: '6514',
      syslogEnabled: true,
      address: '192.0.2.15:8080',
      addressCandidates: [],
      backupTransport: 'sftp',
    },
    sources: [],
    devices: [],
    pushKinds: ['filter-rule'],
    marks: [],
    witnesses: [],
  }
}

function device(over: Partial<Device> = {}): Device {
  return {
    id: 'rb5009',
    name: 'rb5009',
    sourceIp: '',
    configured: true,
    firstSeen: '0001-01-01T00:00:00Z',
    lastSeen: '0001-01-01T00:00:00Z',
    eventCount: 0,
    status: 'never_seen',
    ...over,
  }
}

// The router step's four answers, as the run holds them on arrival here.
function answered() {
  wizardRun.name = 'rb5009'
  wizardRun.addr = '192.0.2.1'
  wizardRun.push = true
  wizardRun.backup = false
  wizardRun.q = 4
}

function password(): HTMLInputElement {
  return screen.getByLabelText('Your password') as HTMLInputElement
}

describe('StepMint: the password goes to the ingest token mint too (#1202 spec 33)', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    vi.mocked(fetchSetupCommands).mockResolvedValue(null as never)
    vi.mocked(createDevice).mockResolvedValue(device())
    vi.mocked(mintEnrolment).mockResolvedValue({ token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' })
    vi.mocked(createToken).mockResolvedValue({
      id: 't1',
      name: 'setup-rb5009',
      kind: 'ingest',
      device: 'rb5009',
      createdAt: '2026-09-27T14:00:00Z',
      value: 'ingest0000ingest0000',
    })
    wizardState.reset()
    wizardRun.reset()
    wizardState.status = status()
    wizardState.open = true
    answered()
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('sends the typed password with the ingest token request when push (or backup) was answered yes', async () => {
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(wizardRun.stage).toBe('paste'))
    expect(createToken).toHaveBeenCalledTimes(1)
    expect(createToken).toHaveBeenCalledWith('setup-rb5009', 'ingest', 'rb5009', 'correct horse')
  })

  // Pair for the above: no ingest token, so no password sent to /api/tokens.
  it('does not call createToken at all when both push and backup are answered no', async () => {
    wizardRun.push = false
    wizardRun.backup = false
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(wizardRun.stage).toBe('paste'))
    expect(mintEnrolment).toHaveBeenCalledWith('rb5009', 'correct horse', '192.0.2.1')
    expect(createToken).not.toHaveBeenCalled()
  })
})
