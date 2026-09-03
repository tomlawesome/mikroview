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
  fetchSetupCommands: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  createToken: vi.fn(),
}))

import { fetchDevices, fetchSetupCommands, fetchSetupStatus, markSetupStep } from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { appState } from '../lib/state.svelte'
import { wizardState } from '../lib/wizard.svelte'
import type { SetupCommandsResponse, SetupStatus } from '../lib/types'
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

// commandsFixture is built from #436's fixed API contract sample
// response (POST /api/setup/commands), the same shape internal/routeros
// serves -- one dialect, three rows, and edge-1's version below the
// table's floor.
function commandsFixture(over: Partial<SetupCommandsResponse> = {}): SetupCommandsResponse {
  return {
    routeros: {
      minimum: '7.18',
      newest: '7.24.1',
      rows: [
        { from: '7.18', to: '7.23.3', dialect: 'a', verifiedBy: 'exercised on CHR 7.23.3', note: '' },
        {
          from: '7.24',
          to: '7.24',
          dialect: 'a',
          verifiedBy: 'release notes read 2026-08-29',
          note:
            '7.24.0 has a `find` argument-lookup bug, fixed in 7.24.1: on this release, tag rules ' +
            'one at a time rather than with the bulk commands.',
        },
        { from: '7.24.1', to: '7.24.1', dialect: 'a', verifiedBy: 'release notes read 2026-08-29', note: '' },
      ],
    },
    picked: null,
    routers: [{ id: 'edge-1', name: 'edge-1', routerosVersion: '7.16', standing: 'below-minimum', note: '' }],
    steps: {
      caTrust: { commands: 'CA_TRUST_COMMANDS', note: '' },
      syslog: { commands: 'SYSLOG_COMMANDS', note: '' },
      ruleTagging: { commands: 'RULE_TAGGING_COMMANDS', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
    },
    ...over,
  }
}

beforeEach(async () => {
  vi.resetAllMocks()
  vi.mocked(fetchSetupStatus).mockResolvedValue(status())
  vi.mocked(fetchDevices).mockResolvedValue([])
  vi.mocked(fetchSetupCommands).mockResolvedValue(commandsFixture())
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
  wizardState.commands = null
  wizardState.pickedVersion = ''
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

  // #442: a router declared under one address whose logs arrive from
  // another. Step 2 reads it as partial -- evidence arrived, composed
  // wrongly -- states both facts, and prints the remedy with the
  // operator's values. It never claims the two addresses are one box.
  it('surfaces the source-address split on step 2 with the printed remedy', async () => {
    wizardState.pane = 2
    wizardState.status = status({
      sources: [{ source: '10.0.20.1', syslogFirstSeenAt: '2026-08-23T09:00:00Z' }],
    })
    wizardState.devices = [
      {
        id: 'office',
        name: 'office',
        sourceIp: '192.168.88.1',
        configured: true,
        firstSeen: '',
        lastSeen: '',
        eventCount: 0,
        status: 'never_seen',
        multihomedCandidates: ['10.0.20.1'],
      },
      {
        id: '10.0.20.1',
        name: '10.0.20.1',
        sourceIp: '10.0.20.1',
        configured: false,
        firstSeen: '2026-08-23T09:00:00Z',
        lastSeen: '2026-08-23T09:00:00Z',
        eventCount: 12,
        status: 'live',
      },
    ]
    const { container } = render(SetupWizard)

    const observation = container.querySelector('.observation')
    expect(observation?.textContent?.trim()).toBe(
      "Connected — but from 10.0.20.1, an address you haven't declared, while 192.168.88.1, which you declared in config.yaml, has sent nothing.",
    )
    // Partial reads in the arrived voice, never attention.
    expect(observation?.classList.contains('attention')).toBe(false)

    const body = container.querySelector('.split')?.textContent?.replace(/\s+/g, ' ') ?? ''
    expect(body).toContain("MikroView can't tell whether these are the same router")
    expect(body).toContain('You can tell.')
    expect(body).toContain('Keep 192.168.88.1 (recommended). Run this on the router')
    expect(body).toContain('Or keep 10.0.20.1: change sourceIp to 10.0.20.1 in config.yaml and restart.')
    expect(body).toContain('If they are two different routers, nothing is wrong.')
    expect(body).toContain('this notice clears itself when 192.168.88.1 sends its first log.')

    const pres = [...container.querySelectorAll('.split pre')].map((p) => p.textContent)
    expect(pres).toEqual(['/system logging action set mikroview src-address=192.168.88.1'])

    // The step list carries the split as its receipt.
    const rows = container.querySelectorAll('.steps .step-row')
    expect(rows[1].querySelector('.step-receipt')?.textContent).toBe(
      'syslog from 10.0.20.1 · declared 192.168.88.1 silent',
    )
  })

  it('shows no split body when the declared router is the one sending', () => {
    wizardState.pane = 2
    wizardState.status = status({
      sources: [{ source: '192.168.88.1', syslogFirstSeenAt: '2026-08-23T09:00:00Z' }],
    })
    wizardState.devices = [
      {
        id: 'office',
        name: 'office',
        sourceIp: '192.168.88.1',
        configured: true,
        firstSeen: '2026-08-23T09:00:00Z',
        lastSeen: '2026-08-23T09:00:00Z',
        eventCount: 12,
        status: 'live',
      },
    ]
    const { container } = render(SetupWizard)
    expect(container.querySelector('.split')).toBeNull()
    expect(container.querySelector('.observation')?.textContent).toContain('open syslog connection')
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

  // #646: the wizard ends by taking the operator back to the fall,
  // mikroview's real landing page (#616) -- whichever path opened the
  // wizard, the journey's own hand-off included.
  it('the finish leads back to the fall', async () => {
    wizardState.pane = 5
    appState.view = 'engineroom'
    render(SetupWizard)
    await fireEvent.click(screen.getByRole('button', { name: 'Finish' }))

    await fireEvent.click(screen.getByRole('button', { name: 'Take me to the fall' }))
    expect(appState.view).toBe('fall')
    expect(wizardState.open).toBe(false)
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

// #436: the wizard stopped generating RouterOS syntax itself and now
// renders what POST /api/setup/commands sends back -- these pin the
// request it sends, the pick-list it builds from routeros.rows, and the
// router-standing warning it renders from the same fixture.
describe('SetupWizard -- RouterOS version-aware commands (#436)', () => {
  it("requests the command blocks with the wizard's address, syslog port and push kinds, no version until picked", async () => {
    render(SetupWizard)
    await waitFor(() => expect(fetchSetupCommands).toHaveBeenCalled())

    const req = vi.mocked(fetchSetupCommands).mock.calls[0][0]
    expect(req.address).toBe(wizardState.address)
    expect(req.syslogPort).toBe(':6514')
    expect(req.kinds).toEqual(['filter-rule', 'arp'])
    expect(req.version).toBeUndefined()
  })

  it('re-requests with the picked version when the operator chooses one', async () => {
    const { container } = render(SetupWizard)
    await waitFor(() => expect(container.querySelector('.routeros-version select')).toBeTruthy())

    const select = container.querySelector('.routeros-version select') as HTMLSelectElement
    await fireEvent.change(select, { target: { value: '7.24.1' } })

    await waitFor(() => {
      const last = vi.mocked(fetchSetupCommands).mock.calls.at(-1)?.[0]
      expect(last?.version).toBe('7.24.1')
    })
  })

  it('lists the dialect table\'s rows, range-labelled, with "Not sure" first', async () => {
    const { container } = render(SetupWizard)
    await waitFor(() => expect(container.querySelector('.routeros-version select')).toBeTruthy())

    const options = [...container.querySelectorAll('.routeros-version option')].map((o) => o.textContent)
    expect(options).toEqual(['Not sure — the router will report it', '7.18–7.23.3', '7.24', '7.24.1'])
  })

  it("warns once, in the note register, for a router below the table's floor", async () => {
    const { container } = render(SetupWizard)
    await waitFor(() => {
      const note = container.querySelector('.note.below-minimum')
      expect(note?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
        'edge-1 runs RouterOS 7.16. These commands were written for 7.18 and later; on 7.16 some ' +
          'may not apply as written. Check each against your router before running it.',
      )
    })
  })

  it('carries the amber left rule only on the below-minimum warning', async () => {
    const { container } = render(SetupWizard)
    await waitFor(() => expect(container.querySelector('.note.below-minimum')).toBeTruthy())
    // Never a modal, never dismissable -- just a note in the existing
    // register, so there is exactly one warning paragraph here.
    expect(container.querySelectorAll('.note.below-minimum').length).toBe(1)
  })

  it('warns for the operator\'s own picked version too, worded "Your picked version"', async () => {
    vi.mocked(fetchSetupCommands).mockResolvedValue(
      commandsFixture({ picked: { version: '7.25', standing: 'ahead-of-review', dialect: 'a' }, routers: [] }),
    )
    const { container } = render(SetupWizard)

    await waitFor(() => {
      const notes = [...container.querySelectorAll('p.note')].map((p) => p.textContent?.replace(/\s+/g, ' ').trim())
      expect(
        notes.some((t) =>
          t?.startsWith(
            'Your picked version runs RouterOS 7.25. These commands were last checked against 7.24.1.',
          ),
        ),
      ).toBe(true)
    })
    expect(container.querySelector('.note.below-minimum')).toBeNull()
  })

  it('says nothing for a router whose standing is unknown or reviewed', async () => {
    vi.mocked(fetchSetupCommands).mockResolvedValue(
      commandsFixture({
        routers: [
          { id: 'core', name: 'core', routerosVersion: '7.20', standing: 'reviewed', note: '' },
          { id: 'edge-2', name: 'edge-2', routerosVersion: '', standing: 'unknown', note: '' },
        ],
      }),
    )
    const { container } = render(SetupWizard)
    await waitFor(() => expect(fetchSetupCommands).toHaveBeenCalled())
    expect(container.querySelector('.note.below-minimum')).toBeNull()
    expect(container.textContent).not.toContain('runs RouterOS')
  })

  it("renders a step's own note directly under that step's block", async () => {
    vi.mocked(fetchSetupCommands).mockResolvedValue(
      commandsFixture({
        routers: [],
        steps: {
          caTrust: { commands: 'CA', note: '' },
          syslog: { commands: 'SYS', note: '' },
          ruleTagging: {
            commands: 'TAG',
            note: 'on this release, tag rules one at a time rather than with the bulk commands',
          },
          push: { commands: '', note: '' },
          schedule: { commands: '', note: '' },
        },
      }),
    )
    wizardState.pane = 3
    const { container } = render(SetupWizard)

    await waitFor(() => expect(container.querySelector('pre')?.textContent).toBe('TAG'))
    expect(container.textContent).toContain('on this release, tag rules one at a time')
  })
})
