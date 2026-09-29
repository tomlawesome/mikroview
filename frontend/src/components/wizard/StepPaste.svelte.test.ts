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

// Only the network boundary is faked: the block the server rendered, the
// devices/status it is read against, and the two acts this step can take
// (Reroll, "enrol at <other> instead") both go through mintEnrolment, the
// same call Mint uses.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  fetchRefusedSenders: vi.fn(),
  fetchRouterBackups: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchEvents: vi.fn(),
  createToken: vi.fn(),
  mintEnrolment: vi.fn(),
}))

import {
  createToken,
  fetchDevices,
  fetchEvents,
  fetchRefusedSenders,
  fetchRouterBackups,
  fetchSetupCommands,
  fetchSetupStatus,
  mintEnrolment,
} from '../../lib/api'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import type { Device, SetupCommandsResponse, SetupStatus } from '../../lib/types'
import StepPaste from './StepPaste.svelte'
import Wizard from './Wizard.svelte'

function status(over: Partial<SetupStatus> = {}): SetupStatus {
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
    ...over,
  }
}

function device(over: Partial<Device> = {}): Device {
  return {
    id: 'rb5009',
    name: 'rb5009',
    sourceIp: '192.168.13.1',
    configured: true,
    firstSeen: '2026-09-27T14:03:04Z',
    lastSeen: '2026-09-27T14:05:00Z',
    eventCount: 0,
    status: 'live',
    acceptedIp: '192.168.13.1',
    enrolledAt: '2026-09-27T14:03:04Z',
    ...over,
  }
}

const ENROL_LINE = '/log info "mikroview-enrol demo0000demo0000demo"'

function commands(steps: Partial<SetupCommandsResponse['steps']> = {}): SetupCommandsResponse {
  return {
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [], upgrades: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '/tool fetch url="https://mikroview.lan/ca.crt" dst-path=mikroview-ca.crt', note: '', blocked: [] },
      syslog: {
        commands: `:if ([:len [/system logging action find name=mikroview]] = 0) do={ /system logging action add name=mikroview }\n${ENROL_LINE}`,
        note: '',
        blocked: [],
      },
      ruleTagging: { commands: '', note: '', blocked: [] },
      push: { commands: '', note: '', blocked: [] },
      schedule: { commands: '/system script add name=mv-push ...\n/system scheduler add name=mv-push interval=20m ...', note: '', blocked: [] },
      backup: { commands: '/system script add name=mv-backup ...', note: '', blocked: [] },
      backupSchedule: { commands: '/system scheduler add name=mv-backup start-time=03:00:00 ...', note: '', blocked: [] },
      ...steps,
    },
  }
}

// The run as it stands when Copy on the paste step lands here: named,
// addressed, a token minted, both push and backup answered.
function landOnPaste(over: { push?: boolean; backup?: boolean } = {}) {
  wizardState.ledgerDevice = 'rb5009'
  wizardState.devices = [device()]
  wizardState.status = status()
  wizardState.enrolment = { token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' }
  wizardState.enrolmentMintedAt = '2026-09-27T14:00:00Z'
  // Set already, so the step's own ingest-token effect (needed only for
  // push/backup) has nothing to do -- its own test below leaves this
  // unset on purpose.
  wizardState.token = 'ingest0000ingest0000'
  wizardState.tokenDevice = 'rb5009'
  wizardRun.name = 'rb5009'
  wizardRun.addr = '192.168.13.1'
  wizardRun.push = over.push ?? true
  wizardRun.backup = over.backup ?? true
  wizardRun.stage = 'paste'
}

function block(): string {
  return screen.getByLabelText('The block to paste').textContent ?? ''
}

function copyButton(): HTMLButtonElement {
  return screen.getByRole('button', { name: 'Copy' }) as HTMLButtonElement
}

// norm collapses the whitespace a multi-line template literally puts
// into textContent (the browser only collapses it visually) so an
// assertion can compare against one wrapped sentence.
function norm(text: string | null | undefined): string {
  return (text ?? '').replace(/\s+/g, ' ').trim()
}

describe('StepPaste: the block and Copy', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    vi.mocked(fetchRefusedSenders).mockResolvedValue([])
    vi.mocked(fetchRouterBackups).mockResolvedValue({
      enabled: true,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: 'open',
    } as never)
    vi.mocked(fetchSetupCommands).mockResolvedValue(commands())
    vi.mocked(fetchEvents).mockResolvedValue({ events: [], hasMore: false, windowStart: '', serverTime: '' })
    wizardState.reset()
    wizardRun.reset()
    wizardState.commands = commands()
    landOnPaste()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('numbers and titles every section, in order, with the enrol line last -- and never calls it a script', async () => {
    render(StepPaste)
    await tick()
    expect(norm(screen.getByText(/^4 parts, in order/).textContent)).toBe(
      '4 parts, in order — trust the certificate, push router state, back up the router, send logs — and the enrol line last. Into the router’s terminal (WinBox ▸ New Terminal, or ssh), not a script.',
    )
    const text = block()
    const lines = text.split('\n')
    expect(lines[0]).toBe('# 1 · Trust the certificate')
    expect(lines.find((l) => l.startsWith('# 2 ·'))).toBe('# 2 · Push router state')
    expect(lines.find((l) => l.startsWith('# 3 ·'))).toBe('# 3 · Back up the router')
    expect(lines.find((l) => l.startsWith('# 4 ·'))).toBe('# 4 · Send logs')
    expect(lines.at(-1)).toBe(ENROL_LINE)
    // "not a script" is the ratified wording (#1368) -- nowhere in the
    // step's own prose (outside the pasted RouterOS commands themselves,
    // which legitimately use RouterOS's own "/system script" feature)
    // calls the block a script.
    const prose = Array.from(document.querySelectorAll('h3, p, .obs, .copyrow, .warnbox, .cautionbox'))
      .map((el) => el.textContent ?? '')
      .join(' ')
    expect(prose).toContain('not a script')
    expect(prose.replace('not a script', '')).not.toMatch(/script/i)
  })

  it('drops a section with nothing said yes to', async () => {
    landOnPaste({ push: false, backup: false })
    render(StepPaste)
    await tick()
    expect(norm(screen.getByText(/^2 parts, in order/).textContent)).toMatch(
      /^2 parts, in order — trust the certificate, send logs — and the enrol line last\./,
    )
    expect(block()).not.toContain('Push router state')
    expect(block()).not.toContain('Back up the router')
  })

  it('Copy puts the whole block on the clipboard and starts the router’s turn', async () => {
    let written = ''
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: (t: string) => ((written = t), Promise.resolve()) },
    })
    render(StepPaste)
    await tick()
    expect(wizardRun.copied).toBe(false)
    await fireEvent.click(copyButton())
    await tick()
    expect(wizardRun.copied).toBe(true)
    expect(wizardRun.stage).toBe('watch')
    expect(written).toContain(ENROL_LINE)
    expect(written.split('\n')[0]).toBe('# 1 · Trust the certificate')
  })

  it('Next is disabled until Copy, in the shell', async () => {
    render(Wizard)
    await tick()
    landOnPaste()
    await tick()
    const next = screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement
    expect(next.disabled).toBe(true)
    expect(screen.getByText('Next checks what has arrived')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Copy' }))
    await tick()
    // Now on the router's turn: Next stays disabled until everything
    // chosen has arrived (wizardRun.test.ts covers the gating itself).
    expect(screen.getByRole('button', { name: 'Next' }).hasAttribute('disabled')).toBe(true)
  })

  it('Reroll asks for the password again and mints again for the same address', async () => {
    vi.mocked(mintEnrolment).mockResolvedValue({ token: 'reroll00000reroll0000', expiresAt: '2026-09-27T14:32:00Z' })
    render(StepPaste)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'Reroll' }))
    await tick()
    const pass = screen.getByLabelText('Your password, to reroll the token') as HTMLInputElement
    await fireEvent.input(pass, { target: { value: 'correct horse' } })
    await fireEvent.keyDown(pass, { key: 'Enter' })
    await waitFor(() => expect(mintEnrolment).toHaveBeenCalledWith('rb5009', 'correct horse', '192.168.13.1'))
    // Reroll never advances the stage -- it is only offered before Copy.
    expect(wizardRun.stage).toBe('paste')
    expect(wizardState.enrolment?.token).toBe('reroll00000reroll0000')
  })
})

describe('StepPaste: the router’s turn -- the track', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    vi.mocked(fetchRefusedSenders).mockResolvedValue([])
    vi.mocked(fetchRouterBackups).mockResolvedValue({
      enabled: true,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: 'open',
    } as never)
    vi.mocked(fetchSetupCommands).mockResolvedValue(commands())
    vi.mocked(fetchEvents).mockResolvedValue({ events: [], hasMore: false, windowStart: '', serverTime: '' })
    wizardState.reset()
    wizardRun.reset()
    wizardState.commands = commands()
    landOnPaste({ backup: false })
    wizardRun.copied = true
    wizardRun.copiedAt = '2026-09-27T14:00:05Z'
    wizardRun.stage = 'watch'
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  function stations(): HTMLDivElement[] {
    return Array.from(document.querySelectorAll<HTMLDivElement>('.track .stn'))
  }

  it('lights each station from the receipts that arrived, and struck the one set aside', async () => {
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [{ device: 'rb5009', configured: true, sourceIp: '192.168.13.1', events: 12, decodedActions: 0 }],
    })
    render(StepPaste)
    await tick()
    const rows = stations()
    // copy, cert, enrol, push, backup -- push was answered yes but has
    // not arrived, backup was answered no and is set aside.
    expect(rows).toHaveLength(5)
    expect(rows.map((r) => r.classList.contains('done'))).toEqual([true, true, true, false, false])
    expect(rows[4].classList.contains('skip')).toBe(true)
    expect(rows[4].querySelector('.lab')?.textContent).toBe('backup')
    expect(rows[3].classList.contains('wait')).toBe(true)
    // The latest arrival's own headline, in its ink via the shared .obs
    // classes -- the certificate is the most recent (real) arrival here.
    expect(document.querySelector('.obs.arrived')?.textContent).toMatch(/^Enrol line from 192\.168\.13\.1/)
    expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('The router’s turn.')
  })

  it('reads "<name> is sending" once everything chosen has arrived', async () => {
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [
        {
          device: 'rb5009',
          configured: true,
          sourceIp: '192.168.13.1',
          events: 12,
          decodedActions: 0,
          pushedKinds: { 'filter-rule': '2026-09-27T14:03:31Z' },
        },
      ],
    })
    wizardRun.push = true
    render(StepPaste)
    await waitFor(() => expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('rb5009 is sending.'))
    expect(document.querySelector('.obs.arrived')?.textContent).toMatch(/^First push from rb5009/)
  })

  it('shows the ahead-of-review caution box and names the reviewed version', async () => {
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: 'x', syslogFirstSeenAt: 'y' }],
      devices: [
        {
          device: 'rb5009',
          configured: true,
          sourceIp: '192.168.13.1',
          events: 12,
          decodedActions: 0,
          pushedKinds: { 'filter-rule': '2026-09-27T14:03:31Z' },
        },
      ],
    })
    wizardState.devices = [device({ routerosVersion: '7.25.1', routerosStanding: 'ahead-of-review' })]
    wizardRun.push = true
    render(StepPaste)
    await waitFor(() => expect(document.querySelector('.cautionbox')).not.toBeNull())
    expect(norm(document.querySelector('.cautionbox')?.textContent)).toBe(
      'RouterOS 7.25.1 is newer than these commands were reviewed on (7.24.1). They ran and the push arrived. If anything reads wrong, this is the first suspect.',
    )
  })
})

describe('StepPaste: the refused-sender recovery', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    // refreshRefused polls this while the wizard is open (Wizard.svelte),
    // so the refusal has to come from here -- setting wizardState.refused
    // directly would just be overwritten by the next poll tick.
    vi.mocked(fetchRefusedSenders).mockResolvedValue([
      { ip: '192.168.13.99', firstSeen: '2026-09-27T14:01:00Z', lastSeen: '2026-09-27T14:01:30Z', lines: 3 },
    ])
    vi.mocked(fetchRouterBackups).mockResolvedValue({
      enabled: true,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: 'open',
    } as never)
    vi.mocked(fetchSetupCommands).mockResolvedValue(commands())
    vi.mocked(fetchEvents).mockResolvedValue({ events: [], hasMore: false, windowStart: '', serverTime: '' })
    wizardState.reset()
    wizardRun.reset()
    wizardState.commands = commands()
    landOnPaste({ push: false, backup: false })
    wizardRun.copied = true
    wizardRun.stage = 'watch'
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('shows the warning box with the fix and the enrol line as one block, and a chip in the bar', async () => {
    render(Wizard)
    await tick()
    await waitFor(() => expect(document.querySelector('.warnbox')).not.toBeNull())
    expect(document.querySelector('.warnbox')?.textContent).toContain(
      'Lines from 192.168.13.99 arrived without the enrol line and were refused.',
    )
    const fix = document.querySelector('.warnbox pre')?.textContent ?? ''
    expect(fix.split('\n')).toEqual([
      ':if ([:len [/system logging action find name=mikroview]] = 0) do={ /system logging action add name=mikroview }',
      ENROL_LINE,
    ])
    expect(screen.getByRole('button', { name: 'enrol at 192.168.13.99 instead' })).toBeTruthy()
    expect(document.querySelector('.bar .chips .att.alarm')?.textContent).toBe('refused · 192.168.13.99')
  })

  it('"enrol at <other> instead" mints again for that address, and the box clears', async () => {
    vi.mocked(mintEnrolment).mockResolvedValue({ token: 'other000000other00000', expiresAt: '2026-09-27T14:35:00Z' })
    render(Wizard)
    await tick()
    await fireEvent.click(await screen.findByRole('button', { name: 'enrol at 192.168.13.99 instead' }))
    await tick()
    const pass = screen.getByLabelText('Your password, to enrol at 192.168.13.99 instead') as HTMLInputElement
    await fireEvent.input(pass, { target: { value: 'correct horse' } })
    await fireEvent.keyDown(pass, { key: 'Enter' })
    await waitFor(() => expect(mintEnrolment).toHaveBeenCalledWith('rb5009', 'correct horse', '192.168.13.99'))
    // Never an accept by itself: only the mint happened, and the walk's
    // own "since" moved to now, so the old refusal drops out on its own.
    await waitFor(() => expect(document.querySelector('.warnbox')).toBeNull())
  })
})

describe('StepPaste: minting the ingest token push and backup need', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([device()])
    vi.mocked(fetchRefusedSenders).mockResolvedValue([])
    vi.mocked(fetchRouterBackups).mockResolvedValue({
      enabled: true,
      keyUnreadable: false,
      routers: [],
      totalGenerations: 0,
      totalRouters: 0,
      totalBytes: 0,
      lock: 'open',
    } as never)
    vi.mocked(fetchEvents).mockResolvedValue({ events: [], hasMore: false, windowStart: '', serverTime: '' })
    wizardState.reset()
    wizardRun.reset()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('mints one the first time the block needs push or backup commands, and re-renders with it', async () => {
    landOnPaste()
    // No ingest token yet, unlike every other test above.
    wizardState.token = ''
    wizardState.tokenDevice = ''
    vi.mocked(createToken).mockResolvedValue({
      id: 't1',
      name: 'setup-rb5009',
      kind: 'ingest',
      device: 'rb5009',
      createdAt: '2026-09-27T14:00:00Z',
      value: 'ingest0000ingest0000',
    })
    vi.mocked(fetchSetupCommands).mockResolvedValue(commands({ schedule: { commands: '/system script add name=mv-push ...', note: '', blocked: [] } }))
    render(StepPaste)
    await waitFor(() => expect(createToken).toHaveBeenCalledWith('setup-rb5009', 'ingest', 'rb5009'))
    expect(wizardState.token).toBe('ingest0000ingest0000')
    await waitFor(() => expect(fetchSetupCommands).toHaveBeenCalledWith(expect.objectContaining({ token: 'ingest0000ingest0000', device: 'rb5009' })))
  })
})
