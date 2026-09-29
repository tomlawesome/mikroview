// SPDX-License-Identifier: AGPL-3.0-only

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
// /api/devices/{id}/enrolment with the password and the address.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  fetchSetupCommands: vi.fn(),
  createDevice: vi.fn(),
  mintEnrolment: vi.fn(),
}))

import { createDevice, fetchDevices, fetchSetupCommands, fetchSetupStatus, mintEnrolment } from '../../lib/api'
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
      address: '192.168.13.15:8080',
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
  wizardRun.addr = '192.168.13.1'
  wizardRun.push = true
  wizardRun.backup = false
  wizardRun.q = 4
}

function password(): HTMLInputElement {
  return screen.getByLabelText('Your password') as HTMLInputElement
}

describe('StepMint: your password, to mint the token', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    vi.mocked(fetchSetupCommands).mockResolvedValue(null as never)
    vi.mocked(createDevice).mockResolvedValue(device())
    vi.mocked(mintEnrolment).mockResolvedValue({ token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' })
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

  it("names the router and its address in the lead, and gives the password field focus", async () => {
    render(StepMint)
    await tick()
    expect(screen.getByText('Your password, to mint rb5009’s token.')).toBeTruthy()
    expect(screen.getByText(/Minting opens the log port for 192\.168\.13\.1, for 15 minutes/)).toBeTruthy()
    expect(screen.getByText(/The token goes at the end of the block\./)).toBeTruthy()
    expect(password().type).toBe('password')
    expect(document.activeElement).toBe(password())
    // Nothing of the token is drawn here: it goes at the end of the
    // block, on the paste step.
    expect(document.querySelector('pre')).toBeNull()
  })

  it('Enter mints: the record is made, the password is spent on the one call, and the run moves to the paste step', async () => {
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    expect(wizardRun.pass).toBe('correct horse')
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(wizardRun.stage).toBe('paste'))
    expect(createDevice).toHaveBeenCalledWith('rb5009')
    expect(wizardState.ledgerDevice).toBe('rb5009')
    expect(mintEnrolment).toHaveBeenCalledTimes(1)
    expect(mintEnrolment).toHaveBeenCalledWith('rb5009', 'correct horse', '192.168.13.1')
    // Spent: neither copy of the password outlives the call.
    expect(wizardRun.pass).toBe('')
    expect(wizardState.enrolPassword).toBe('')
    expect(wizardState.enrolment?.token).toBe('demo0000demo0000demo')
    expect(wizardState.enrolmentError).toBeNull()
    // The rail's receipt for this step, in decision blue until the
    // certificate lands.
    const row = rowState(wizardRun.answers, wizardRun.evidence, 'pass')
    expect(row.cls).toBe('chosen')
    expect(row.ink).toBe('token')
    expect(row.receipt).toMatch(/^token good until \d\d:\d\d$/)
  })

  it('does not make a second record for a router the walk already has (Re-enrol…)', async () => {
    wizardState.ledgerDevice = 'rb5009'
    wizardState.devices = [device()]
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(wizardRun.stage).toBe('paste'))
    expect(createDevice).not.toHaveBeenCalled()
    expect(mintEnrolment).toHaveBeenCalledWith('rb5009', 'correct horse', '192.168.13.1')
  })

  it('reads a refusal back under the field and asks again', async () => {
    vi.mocked(mintEnrolment).mockResolvedValue('password: not accepted')
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'wrong' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('password: not accepted')).toBeTruthy())
    expect(document.querySelector('.form .problem')?.getAttribute('aria-live')).toBe('polite')
    expect(wizardRun.stage).toBe('ask')
    expect(wizardRun.pass).toBe('')
    expect(wizardState.enrolment).toBeNull()
    expect(rowState(wizardRun.answers, wizardRun.evidence, 'pass').receipt).toBe('your password, once')

    // The record was made on the first try; a second Enter mints again
    // without making another.
    vi.mocked(mintEnrolment).mockResolvedValue({ token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' })
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(wizardRun.stage).toBe('paste'))
    expect(createDevice).toHaveBeenCalledTimes(1)
    expect(mintEnrolment).toHaveBeenCalledTimes(2)
  })

  it("reads a refused record where the mint's refusal would, and mints nothing", async () => {
    vi.mocked(createDevice).mockResolvedValue('a router named rb5009 already exists')
    render(StepMint)
    await tick()
    await fireEvent.input(password(), { target: { value: 'correct horse' } })
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await waitFor(() => expect(screen.getByText('a router named rb5009 already exists')).toBeTruthy())
    expect(mintEnrolment).not.toHaveBeenCalled()
    expect(wizardRun.pass).toBe('')
    expect(wizardRun.stage).toBe('ask')
  })

  it('does nothing on Enter with no password typed', async () => {
    render(StepMint)
    await tick()
    await fireEvent.keyDown(password(), { key: 'Enter' })
    await tick()
    expect(createDevice).not.toHaveBeenCalled()
    expect(mintEnrolment).not.toHaveBeenCalled()
  })
})
