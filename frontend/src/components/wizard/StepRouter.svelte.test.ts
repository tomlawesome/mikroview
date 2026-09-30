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

// The drop box's switch (#1361) is the one call that leaves this step:
// faked at the network boundary, like StepPaste's own acts. Everything
// else stays real.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupCommands: vi.fn(),
  setRouterBackupSwitch: vi.fn(),
}))

import { fetchSetupCommands, setRouterBackupSwitch } from '../../lib/api'
import type { RouterBackupsResponse, SetupCommandsResponse, SetupStatus } from '../../lib/types'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import { rowState } from '../../lib/wizardRun'
import StepRouter from './StepRouter.svelte'

// The router step is one form and nothing else: no call leaves it. So
// nothing is faked here; the step is exercised the way an operator
// types into it, and what it leaves in the run is read back.
describe('StepRouter: the router, one form', () => {
  beforeEach(() => {
    wizardState.reset()
    wizardRun.reset()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
  })

  function address(): HTMLInputElement {
    return screen.getByLabelText(/^Its address/) as HTMLInputElement
  }

  function problem(): string {
    return document.querySelector('.form .problem')?.textContent ?? ''
  }

  it('gives the name field focus when the step arrives', async () => {
    render(StepRouter)
    await tick()
    expect(document.activeElement).toBe(screen.getByLabelText(/^Name/))
    expect(screen.getByText('Your first router.')).toBeTruthy()
    expect(screen.getByText(/MikroView never connects to it — the router sends\./)).toBeTruthy()
  })

  it('says what is wrong with the address as you type, and holds the four until it is right (#1380)', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
    await fireEvent.click(screen.getAllByRole('radio', { name: 'Yes' })[0])
    await fireEvent.click(screen.getAllByRole('radio', { name: 'No' })[1])

    const cases: [string, RegExp][] = [
      ['192.168.1', /^Four numbers, 0–255, separated by dots\.$/],
      ['192.168.1.1:514', /^No port here — just the address\. The port is MikroView’s side\.$/],
      ['rb5009.lan', /^A name will not do: the enrolment window binds to an address\.$/],
      ['10.0.0.0/24', /^No prefix length — the router’s own address, not its network\.$/],
      ['192.168.001.1', /^Four numbers, 0–255, separated by dots\.$/],
    ]
    for (const [typed, line] of cases) {
      await fireEvent.input(address(), { target: { value: typed } })
      await tick()
      expect(problem(), typed).toMatch(line)
      expect(address().getAttribute('aria-invalid'), typed).toBe('true')
      expect(address().classList.contains('bad'), typed).toBe(true)
      expect(wizardRun.routerDone, typed).toBe(false)
    }
    // The line is aria-live, so the wording reaches a screen reader as
    // it changes.
    expect(document.querySelector('.form .problem')?.getAttribute('aria-live')).toBe('polite')

    await fireEvent.input(address(), { target: { value: '192.168.13.1' } })
    await tick()
    expect(problem()).toBe('')
    expect(address().getAttribute('aria-invalid')).toBe('false')
    expect(address().classList.contains('bad')).toBe(false)
    expect(wizardRun.routerDone).toBe(true)
  })

  it('accepts an IPv6 literal, as the server does', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(address(), { target: { value: 'fd00:13::1' } })
    await tick()
    expect(problem()).toBe('')
    expect(address().getAttribute('aria-invalid')).toBe('false')
  })

  it('takes push and backup as a plain Yes / No each, one answer at a time', async () => {
    render(StepRouter)
    await tick()
    const groups = screen.getAllByRole('radiogroup')
    expect(groups).toHaveLength(2)
    expect(groups[0].getAttribute('aria-labelledby')).toBe('l-push')
    expect(groups[1].getAttribute('aria-labelledby')).toBe('l-backup')
    const [pushYes, backupYes] = screen.getAllByRole('radio', { name: 'Yes' })
    const [pushNo, backupNo] = screen.getAllByRole('radio', { name: 'No' })
    // Unanswered: neither side is on, and the button carries no
    // interval -- the label's own line already says what each does
    // (owner, round 15).
    expect(pushYes.getAttribute('aria-checked')).toBe('false')
    expect(pushNo.getAttribute('aria-checked')).toBe('false')
    expect(pushYes.textContent).toBe('Yes')
    expect(pushNo.textContent).toBe('No')
    expect(wizardRun.push).toBeNull()

    await fireEvent.click(pushYes)
    await tick()
    expect(wizardRun.push).toBe(true)
    expect(pushYes.classList.contains('on')).toBe(true)
    expect(pushYes.getAttribute('aria-checked')).toBe('true')
    expect(pushNo.classList.contains('on')).toBe(false)

    await fireEvent.click(pushNo)
    await tick()
    expect(wizardRun.push).toBe(false)
    expect(pushNo.classList.contains('on')).toBe(true)
    expect(pushNo.classList.contains('no')).toBe(true)
    expect(pushYes.getAttribute('aria-checked')).toBe('false')

    await fireEvent.click(backupYes)
    await tick()
    expect(wizardRun.backup).toBe(true)
    expect(backupNo.getAttribute('aria-checked')).toBe('false')
    expect(wizardRun.push).toBe(false)
  })

  it('feeds the receipt the rail shows once all four are answered', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
    await fireEvent.input(address(), { target: { value: '192.168.13.1' } })
    await fireEvent.click(screen.getAllByRole('radio', { name: 'Yes' })[0])
    await fireEvent.click(screen.getAllByRole('radio', { name: 'No' })[1])
    await tick()
    const row = rowState(wizardRun.answers, wizardRun.evidence, 'router')
    expect(row.cls).toBe('chosen')
    expect(row.ink).toBe('token')
    expect(row.receipt).toBe('rb5009 · 192.168.13.1 · push yes · backup not now')
  })
})

// #1361: the drop box, when closed, is shown where backups are chosen
// (DESIGN.md, "1 · The router") and can be opened there -- the same
// dialog and endpoint as Settings → router backups. A per-router No
// never closes it; a Yes never opens it.
describe('StepRouter: the drop box, when closed (#1361)', () => {
  function backups(over: Partial<RouterBackupsResponse> = {}): RouterBackupsResponse {
    return {
      enabled: true,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: { passphraseSet: false, locked: false, unlockedForYou: false, minPassphraseLength: 12, idleTimeoutSeconds: 900 },
      ...over,
    }
  }

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

  function commands(): SetupCommandsResponse {
    const step = (commands: string) => ({ commands, undo: '' })
    return {
      steps: {
        caTrust: step('/certificate add'),
        syslog: step('/system logging add'),
        push: step(''),
        schedule: step(''),
        backup: step(''),
        backupSchedule: step(''),
      },
    } as unknown as SetupCommandsResponse
  }

  beforeEach(() => {
    wizardState.reset()
    wizardRun.reset()
    wizardState.open = true
    vi.mocked(setRouterBackupSwitch).mockReset()
    vi.mocked(fetchSetupCommands).mockReset()
  })

  function caution(): HTMLElement | null {
    return document.querySelector('.cautionbox.dropbox')
  }

  it('says nothing until the backups read has landed', async () => {
    wizardState.backups = null
    render(StepRouter)
    await tick()
    expect(caution()).toBeNull()
    expect(screen.queryByRole('button', { name: 'Open it now' })).toBeNull()
  })

  it('says nothing while the drop box is open', async () => {
    wizardState.backups = backups({ port: ':47022' })
    render(StepRouter)
    await tick()
    expect(caution()).toBeNull()
  })

  it('says nothing on an HTTPS-only install, which has no port to open (#955)', async () => {
    wizardState.backupTransport = 'https'
    wizardState.backups = backups({ port: undefined })
    render(StepRouter)
    await tick()
    expect(caution()).toBeNull()
  })

  it('shows the caution with one act under Back up nightly when the drop box is closed, and Yes / No still answer', async () => {
    wizardState.backups = backups({ port: undefined })
    render(StepRouter)
    await tick()
    const box = caution()
    expect(box).toBeTruthy()
    const said = box!.textContent!.replace(/\s+/g, ' ')
    expect(said).toMatch(/The drop box is closed — a backup would have nowhere to arrive\./)
    expect(said).toMatch(/Open it now, or later from Settings → router backups\./)
    expect(screen.getByRole('button', { name: 'Open it now' })).toBeTruthy()
    // The caution sits in the backup choice's own column, after its Yes / No.
    const seg = document.querySelector('[aria-labelledby="l-backup"]')!
    expect(seg.parentElement).toBe(box!.parentElement)
    expect(seg.compareDocumentPosition(box!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    // No dialog, no call, until asked.
    expect(screen.queryByLabelText('your password')).toBeNull()
    expect(vi.mocked(setRouterBackupSwitch)).not.toHaveBeenCalled()
    // A per-router answer is still a per-router answer.
    await fireEvent.click(screen.getAllByRole('radio', { name: 'Yes' })[1])
    await tick()
    expect(wizardRun.backup).toBe(true)
    await fireEvent.click(screen.getAllByRole('radio', { name: 'No' })[1])
    await tick()
    expect(wizardRun.backup).toBe(false)
    expect(vi.mocked(setRouterBackupSwitch)).not.toHaveBeenCalled()
    expect(caution()).toBeTruthy()
  })

  it('Open it now unfolds the shared dialog, and cancel folds it back with the caution still standing', async () => {
    wizardState.backups = backups({ port: undefined })
    render(StepRouter)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Open it now' }))
    expect(screen.getByLabelText('your password')).toBeTruthy()
    expect(screen.getByText(/RouterOS never checks who it is sending to/)).toBeTruthy()
    expect(document.querySelector('.dbox.wizard')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Open it now' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'cancel' }))
    expect(screen.queryByLabelText('your password')).toBeNull()
    expect(screen.getByRole('button', { name: 'Open it now' })).toBeTruthy()
    expect(vi.mocked(setRouterBackupSwitch)).not.toHaveBeenCalled()
  })

  it('opening folds the port into the backups read and leaves a receipt where the caution stood', async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue({ open: true, port: '47022' })
    wizardState.backups = backups({ port: undefined })
    render(StepRouter)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Open it now' }))
    await fireEvent.input(screen.getByLabelText('your password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'open' }))
    await waitFor(() => expect(caution()).toBeNull())
    expect(vi.mocked(setRouterBackupSwitch)).toHaveBeenCalledWith(true, 'hunter2')
    expect(wizardState.backups?.port).toBe('47022')
    expect(wizardState.dropBoxClosed).toBe(false)
    expect(screen.queryByLabelText('your password')).toBeNull()
    expect(document.querySelector('.opened')?.textContent).toMatch(/open on port 47022/)
    // No block stood yet, so nothing is re-rendered.
    expect(vi.mocked(fetchSetupCommands)).not.toHaveBeenCalled()
  })

  it('opening after Back from Paste once asks for the block again, with the ingest token', async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue({ open: true, port: '47022' })
    vi.mocked(fetchSetupCommands).mockResolvedValue(commands())
    wizardState.status = status()
    wizardState.backups = backups({ port: undefined })
    wizardState.commands = commands()
    wizardState.ledgerDevice = 'rb5009'
    wizardState.token = 'tok-ingest'
    wizardState.tokenDevice = 'rb5009'
    render(StepRouter)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Open it now' }))
    await fireEvent.input(screen.getByLabelText('your password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'open' }))
    await waitFor(() => expect(vi.mocked(fetchSetupCommands)).toHaveBeenCalledTimes(1))
    expect(vi.mocked(fetchSetupCommands).mock.calls[0][0]).toMatchObject({ device: 'rb5009', token: 'tok-ingest' })
  })

  it('a refusal shows the server\'s words and leaves the caution and the dialog standing', async () => {
    vi.mocked(setRouterBackupSwitch).mockResolvedValue('incorrect password')
    wizardState.backups = backups({ port: undefined })
    render(StepRouter)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Open it now' }))
    await fireEvent.input(screen.getByLabelText('your password'), { target: { value: 'wrong' } })
    await fireEvent.click(screen.getByRole('button', { name: 'open' }))
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('incorrect password'))
    expect(caution()).toBeTruthy()
    expect(screen.getByLabelText('your password')).toBeTruthy()
    expect(wizardState.backups?.port).toBeUndefined()
    expect(wizardState.dropBoxClosed).toBe(true)
  })
})
