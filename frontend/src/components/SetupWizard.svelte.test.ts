// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'
import { tick } from 'svelte'

// jsdom has no matchMedia, which lib/viewport.svelte.ts reads at module
// load. Installed through vi.hoisted so it is in place before the
// component's import chain runs -- stubbing the viewport module instead
// would take the small-screen sheet out of reach of these tests, and
// that sheet is half of what the record specifies.
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

// Only the network boundary is faked, so these exercise the real
// component markup -- which is the point: what is guarded here is what
// the operator can and cannot do to a half-finished setup.
vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  createToken: vi.fn(),
}))

import { fetchDevices, fetchSetupStatus, markSetupStep } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { appState } from '../lib/state.svelte'
import { wizardState } from '../lib/wizard.svelte'
import type { SetupStatus } from '../lib/types'
import SetupWizard from './SetupWizard.svelte'

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: ['filter-rule', 'arp'],
    marks: [],
    ...over,
  }
}

beforeEach(async () => {
  vi.resetAllMocks()
  vi.mocked(fetchSetupStatus).mockResolvedValue(status())
  vi.mocked(fetchDevices).mockResolvedValue([])
  authState.state = 'authenticated'
  authState.role = 'admin'
  authState.username = 'tom'
  appState.initialLoadDone = true
  appState.devices = []
  wizardState.status = status()
  wizardState.devices = []
  wizardState.pane = 1
  wizardState.showStepList = false
  wizardState.open = true
})

describe('SetupWizard', () => {
  // The owner-recorded rule, and the reason the modal exists in this
  // shape at all: progress is never lost accidentally. A veil that
  // dismissed on click would undo a half-finished setup with a stray
  // one.
  it('does not dismiss on a click outside', async () => {
    const { container } = render(SetupWizard)
    const veil = container.querySelector('.veil')
    expect(veil).toBeTruthy()

    await fireEvent.click(veil!)
    expect(wizardState.open).toBe(true)
    expect(container.querySelector('.setup-wizard')).toBeTruthy()
  })

  it('closes on Esc, which is a keystroke and not a stray click', async () => {
    render(SetupWizard)
    // Dispatched on the document and left to bubble to window, where
    // <svelte:window> listens -- fireEvent takes an Element, and the
    // window object is not one.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await tick()
    expect(wizardState.open).toBe(false)
  })

  // The ✕ says where setup lives afterwards, so closing early is not a
  // one-way door the operator has to guess their way back through.
  it('names where setup lives on the close control', () => {
    render(SetupWizard)
    expect(screen.getByLabelText(/Run setup…/)).toBeTruthy()
  })

  it('always shows five steps, whatever state they are in', () => {
    const { container } = render(SetupWizard)
    const rows = container.querySelectorAll('.steps .step-row')
    // Five steps plus the read-back row.
    expect(rows.length).toBe(6)
  })

  // Next runs the check where one exists. Waiting does not proceed: it
  // hands the body to the heavy warning, which is the only way past.
  it('raises the heavy warning instead of proceeding on a waiting step', async () => {
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(container.querySelector('.heavy')).toBeTruthy()
    expect(wizardState.pane).toBe(1)
    // Two choices, no third option and no "are you sure".
    expect(container.querySelectorAll('.heavy button').length).toBe(2)
    expect(screen.getByRole('button', { name: 'Keep waiting' })).toBeTruthy()
  })

  // The amber button quotes the exact record it will write. That is the
  // feature, not a warning about it.
  it('quotes the record the amber button will write, before it is pressed', async () => {
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    const quote = container.querySelector('.heavy .quote')?.textContent ?? ''
    expect(quote).toContain('setup · step 1 forced past')
    expect(quote).toContain('no router has fetched /ca.crt')
    expect(quote).toContain('tom')
  })

  it('keeps waiting without recording anything when asked to', async () => {
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Keep waiting' }))

    expect(container.querySelector('.heavy')).toBeFalsy()
    expect(markSetupStep).not.toHaveBeenCalled()
    expect(wizardState.pane).toBe(1)
  })

  it('records the force, with what was not observed, and moves on', async () => {
    vi.mocked(markSetupStep).mockResolvedValue({
      step: 1,
      outcome: 'forced',
      actor: 'tom',
      at: '2026-08-23T09:00:00Z',
    })
    render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    await fireEvent.click(screen.getByRole('button', { name: /Go on anyway/ }))

    await waitFor(() => {
      expect(markSetupStep).toHaveBeenCalledWith(1, 'forced', 'no router has fetched /ca.crt')
    })
    await waitFor(() => expect(wizardState.pane).toBe(2))
  })

  it('skips quietly -- recorded, but with no ceremony in the way', async () => {
    vi.mocked(markSetupStep).mockResolvedValue({
      step: 1,
      outcome: 'skipped',
      actor: 'tom',
      at: '2026-08-23T09:00:00Z',
    })
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }))

    await waitFor(() => expect(markSetupStep).toHaveBeenCalledWith(1, 'skipped', expect.any(String)))
    expect(container.querySelector('.heavy')).toBeFalsy()
  })

  // A step whose evidence has arrived just proceeds -- there is nothing
  // to warn about, and warning anyway would train the operator to click
  // through warnings.
  it('proceeds without a warning once the evidence is in', async () => {
    wizardState.status = status({
      sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-23T09:00:00Z' }],
    })
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))

    expect(container.querySelector('.heavy')).toBeFalsy()
    expect(wizardState.pane).toBe(2)
  })

  // Step 3 counts, and can only count upward. There is no waiting check
  // to force past, so Next is free and the hint is absent.
  it('leaves Next free on the counting step', async () => {
    wizardState.pane = 3
    const { container } = render(SetupWizard)
    expect(container.querySelector('.hint')).toBeFalsy()

    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(container.querySelector('.heavy')).toBeFalsy()
    expect(wizardState.pane).toBe(4)
  })

  it('offers Finish on the last step, and reads the ledger back after it', async () => {
    wizardState.pane = 5
    const { container } = render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Finish' }))

    expect(container.querySelector('.headline')).toBeTruthy()
    expect(container.querySelectorAll('.readback li').length).toBe(5)
  })

  // The step list carries each step's receipt for the wizard's life --
  // a decision that has been recorded goes on saying so.
  it('carries a recorded decision in the step list', () => {
    wizardState.status = status({
      marks: [{ step: 2, outcome: 'skipped', actor: 'tom', at: '2026-08-23T09:00:00Z' }],
    })
    const { container } = render(SetupWizard)
    const row = container.querySelectorAll('.steps .step-row')[1]
    expect(row.className).toContain('skipped')
    expect(row.textContent).toContain('skipped by tom')
    // Its consequence, stated plainly -- never a reproach.
    expect(row.textContent).toContain('no logs arrive')
  })

  it('announces the step it moved to, not just that it moved', () => {
    const { container } = render(SetupWizard)
    const live = container.querySelector('[role="status"]')?.textContent ?? ''
    expect(live).toContain('Step 1 of 5')
    expect(live).toContain('Trust the certificate')
  })
})
