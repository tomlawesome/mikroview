// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, getDefaultNormalizer } from '@testing-library/svelte'
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

// Only the network boundary is faked: the ledger's own Undo lines come
// from POST /api/setup/commands (wizardState.commands, set directly
// below rather than through a fetch this step re-triggers), the tagged
// rule's Undo from the same pushed rule table StepTune reads
// (fetchRouterRules), and the forget act from DELETE /api/devices/{id}.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  fetchRefusedSenders: vi.fn(),
  fetchRouterBackups: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchRouterRules: vi.fn(),
  deleteDevice: vi.fn(),
}))

import { deleteDevice, fetchDevices, fetchRefusedSenders, fetchRouterBackups, fetchRouterRules, fetchSetupStatus, type RouterFilterRule, type RouterTable } from '../../lib/api'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import type { Device, SetupCommandsResponse, SetupStatus } from '../../lib/types'
import StepStand from './StepStand.svelte'

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
    sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
    devices: [
      {
        device: 'rb5009',
        configured: true,
        sourceIp: '192.168.13.1',
        events: 1204,
        decodedActions: 5,
        pushedKinds: { 'filter-rule': '2026-09-27T14:03:31Z' },
      },
    ],
    pushKinds: ['filter-rule'],
    marks: [],
    witnesses: [],
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
    eventCount: 1204,
    status: 'live',
    acceptedIp: '192.168.13.1',
    enrolledAt: '2026-09-27T14:03:04Z',
    routerosVersion: '7.24.4',
    ...over,
  }
}

const keepLines = getDefaultNormalizer({ collapseWhitespace: false })

const UNDO = {
  caTrust: '/certificate remove [find where name~"^mikroview-ca.crt"]\n/file remove [find name=mikroview-ca.crt]',
  syslog: '/system logging remove [find action=mikroview]\n/system logging action remove [find name=mikroview]',
  schedule: '/system scheduler remove [find name=mv-push]\n/system script remove [find name=mv-push]',
  backup: '/system scheduler remove [find name=mv-backup]\n/system script remove [find name=mv-backup]',
}

function commands(): SetupCommandsResponse {
  return {
    routeros: { minimum: '7.18', newest: '7.24.1', rows: [], upgrades: [] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '/tool fetch ...', note: '', blocked: [] },
      syslog: { commands: ':if ...', note: '', blocked: [] },
      ruleTagging: { commands: '', note: '', blocked: [] },
      push: { commands: '', note: '', blocked: [] },
      schedule: { commands: '/system script add name=mv-push ...', note: '', blocked: [] },
      backup: { commands: '/system script add name=mv-backup ...', note: '', blocked: [] },
      backupSchedule: { commands: '/system scheduler add name=mv-backup ...', note: '', blocked: [] },
      undo: UNDO,
    },
  }
}

function rule(over: Partial<RouterFilterRule> & { ordinal: number }): RouterFilterRule {
  return { comment: '', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: '', log: false, ...over }
}

const TABLE: RouterTable<RouterFilterRule> = {
  available: true,
  rules: [
    rule({ ordinal: 0, chain: 'input', action: 'accept', comment: 'ssh from mgmt', log: true, logPrefix: 'A|in-ssh|' }),
    rule({ ordinal: 4, chain: 'input', action: 'drop', comment: 'drop from WAN', inInterface: 'ether1' }),
  ],
}

// The run as it stands once every green row has arrived and Finish is
// offered (StepStand's own guard, arrivedAll -- Wizard.svelte only ever
// mounts this body at that point).
function landOnStand(over: { push?: boolean; backup?: boolean; tagged?: boolean } = {}) {
  wizardState.ledgerDevice = 'rb5009'
  wizardState.devices = [device()]
  wizardState.status = status()
  wizardRun.name = 'rb5009'
  wizardRun.addr = '192.168.13.1'
  wizardRun.push = over.push ?? true
  wizardRun.backup = over.backup ?? true
  wizardRun.stage = 'done'
  if (over.tagged) {
    wizardRun.tuneCopied = true
    wizardRun.chosenCount = 1
    wizardRun.tunedAt = '14:06:00'
    wizardRun.tuneChosen = [
      { ordinal: 4, chain: 'input', action: 'drop', comment: 'drop from WAN', why: 'a boundary nobody watches', prefix: 'D|drop-from-wan|', boundaryKey: 'wan-in' },
    ]
  }
}

describe('StepStand: the ledger (#1385)', () => {
  beforeEach(() => {
    vi.mocked(fetchRouterRules).mockResolvedValue(TABLE)
    vi.mocked(deleteDevice).mockResolvedValue(null)
    // forget() re-reads the ledger after a successful delete (so the
    // reopened walk does not read the just-forgotten router's own stale
    // evidence) -- an empty fleet is what a real server reports once
    // Delete has actually removed the only router this suite ever names.
    vi.mocked(fetchSetupStatus).mockResolvedValue({
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
    })
    vi.mocked(fetchDevices).mockResolvedValue([])
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
    wizardState.reset()
    wizardRun.reset()
    wizardState.commands = commands()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('reads certificate and logs as green rows with a receipt, and router state/backup dashed and struck when set aside, in the design’s own words', async () => {
    landOnStand({ push: false, backup: false })
    const { container } = render(StepStand)
    await tick()

    const rows = container.querySelectorAll('.ledger .row')
    // certificate, logs, router state, backup -- rules is left off
    // entirely because push itself was never taken.
    expect(rows).toHaveLength(4)

    const cert = rows[0]
    expect(cert.classList.contains('skip')).toBe(false)
    expect(cert.querySelector('.step-n')?.textContent).toBe('✓')
    expect(cert.textContent).toContain('Certificate trusted')

    const routerState = Array.from(rows).find((r) => r.textContent?.includes('Router state'))!
    expect(routerState.classList.contains('skip')).toBe(true)
    expect(routerState.querySelector('.step-n')?.textContent).toBe('–')
    expect(routerState.textContent).toContain('not now · the fall stays address-only')
    expect(routerState.querySelector('button')).toBeNull() // no Undo on a set-aside row

    const backup = Array.from(rows).find((r) => r.textContent?.includes('Backup'))!
    expect(backup.textContent).toContain('not now · no backups kept here')
  })

  it('wears each row’s own ink through the CSS custom property, never the rail’s style', async () => {
    landOnStand()
    const { container } = render(StepStand)
    await tick()

    const rows = container.querySelectorAll('.ledger .row')
    const cert = rows[0] as HTMLElement
    expect(cert.style.getPropertyValue('--ink')).toBe('var(--ink-cert)')
    const push = Array.from(rows).find((r) => r.textContent?.includes('Router state pushed')) as HTMLElement
    expect(push.style.getPropertyValue('--ink')).toBe('var(--ink-push)')
  })

  it('shows the compact track with no per-station timestamps', async () => {
    landOnStand()
    const { container } = render(StepStand)
    await tick()
    const track = container.querySelector('.track')
    expect(track?.classList.contains('compact')).toBe(true)
    expect(track?.querySelector('.st')).toBeNull()
  })

  it('reveals the right lines per row on Undo, and the row’s own note, then hides them again', async () => {
    landOnStand()
    render(StepStand)
    await tick()

    await fireEvent.click(screen.getAllByRole('button', { name: 'Undo' })[0])
    await tick()
    // Two lines: compared as the <pre> shows them, newline and all.
    expect(screen.getByText(UNDO.caTrust, { normalizer: keepLines })).toBeTruthy()
    expect(screen.getByText(/MikroView notices when the lines stop/)).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Hide' }))
    await tick()
    expect(screen.queryByText(UNDO.caTrust, { normalizer: keepLines })).toBeNull()
  })

  it('sources the tagged-rules Undo from the pushed table, not a hand-written line', async () => {
    landOnStand({ tagged: true })
    render(StepStand)
    await tick()

    const undoButtons = screen.getAllByRole('button', { name: 'Undo' })
    await fireEvent.click(undoButtons[undoButtons.length - 1])
    await tick()
    expect(fetchRouterRules).toHaveBeenCalledWith('rb5009')
    expect(screen.getByText('/ip firewall filter set [find comment="drop from WAN"] log=no log-prefix=""')).toBeTruthy()
  })

  it('undo everything reveals every green row’s block in order, then the forget act -- collapsed by default', async () => {
    landOnStand({ tagged: true })
    render(StepStand)
    await tick()

    expect(screen.queryByRole('button', { name: /forget rb5009 on MikroView/ })).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: 'undo everything on the router first' }))
    await tick()

    const forgetButton = screen.getByRole('button', { name: /forget rb5009 on MikroView — its token, its enrolment and its record/ })
    expect(forgetButton).toBeTruthy()
    // cert, logs, push, backup, tune -- in that order, in one block above
    // the forget button.
    const pres = Array.from(document.querySelectorAll('pre')).map((p) => p.textContent)
    const wholeBlock = pres.find((t) => t?.includes(UNDO.caTrust) && t?.includes('mv-backup'))!
    const order = [UNDO.caTrust, UNDO.syslog, UNDO.schedule, UNDO.backup, 'drop from WAN'].map((s) => wholeBlock.indexOf(s))
    expect(order).toEqual([...order].sort((a, b) => a - b))

    await fireEvent.click(screen.getByRole('button', { name: 'hide the undo lines' }))
    await tick()
    expect(screen.queryByRole('button', { name: /forget rb5009 on MikroView/ })).toBeNull()
  })

  it('forget calls the API once and the wizard reopens at The router', async () => {
    landOnStand()
    render(StepStand)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'undo everything on the router first' }))
    await tick()

    await fireEvent.click(screen.getByRole('button', { name: /forget rb5009 on MikroView/ }))
    await waitFor(() => expect(wizardRun.answers.stage).toBe('ask'))

    expect(deleteDevice).toHaveBeenCalledTimes(1)
    expect(deleteDevice).toHaveBeenCalledWith('rb5009')
    expect(wizardState.ledgerDevice).toBe('')
    expect(wizardRun.answers.q).toBe(0)
  })

  it('shows the API’s refusal beside the button and leaves the walk untouched when forgetting fails', async () => {
    vi.mocked(deleteDevice).mockResolvedValue('device is declared in config.yaml')
    landOnStand()
    render(StepStand)
    await tick()
    await fireEvent.click(screen.getByRole('button', { name: 'undo everything on the router first' }))
    await tick()

    await fireEvent.click(screen.getByRole('button', { name: /forget rb5009 on MikroView/ }))
    await waitFor(() => expect(screen.getByText('device is declared in config.yaml')).toBeTruthy())

    expect(wizardState.ledgerDevice).toBe('rb5009')
  })
})

// The doors (#1385, DESIGN.md "Adding a router, re-enrolling"): the
// fleet's own "+ add a router" berth and a router card's "Re-enrol…"
// both call straight into wizardState -- the same calls Entities.svelte
// makes (openAddRouter/openReEnrol) -- so exercising them here proves
// what wizardRun.begin() actually lands on without re-driving Entities'
// own rendering.
describe('the doors: Entities berth and Re-enrol open the wizard at the right step', () => {
  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
  })

  it('"+ add a router" opens fresh at The router, with no router named', () => {
    wizardState.openAddRouter()
    wizardRun.begin()

    expect(wizardState.open).toBe(true)
    expect(wizardRun.answers.stage).toBe('ask')
    expect(wizardRun.answers.q).toBe(0)
    expect(wizardRun.name).toBe('')
  })

  it('Re-enrol… opens at Mint the token for that router, carrying its push/backup answers, with a fresh token to come', () => {
    wizardState.devices = [device()]
    wizardState.status = status()

    wizardState.openReEnrol('rb5009')
    wizardRun.begin()

    expect(wizardState.open).toBe(true)
    expect(wizardState.ledgerDevice).toBe('rb5009')
    expect(wizardRun.answers.stage).toBe('ask')
    expect(wizardRun.answers.q).toBe(4)
    expect(wizardRun.name).toBe('rb5009')
    expect(wizardRun.addr).toBe('192.168.13.1')
    // Consumed -- a later ordinary reopen (Admin ▸ Run setup…) must not
    // keep forcing Mint on this router.
    expect(wizardState.reEnrolling).toBe(false)
  })
})
