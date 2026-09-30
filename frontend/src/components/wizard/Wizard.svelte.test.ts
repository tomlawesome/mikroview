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

// Only the network boundary is faked, so these exercise the real shell
// markup: which rows are real buttons, which are locked and why, and
// what the bar says as evidence arrives.
vi.mock('../../lib/api', async (orig) => ({
  ...(await orig<typeof import('../../lib/api')>()),
  fetchSetupStatus: vi.fn(),
  fetchDevices: vi.fn(),
  fetchRefusedSenders: vi.fn(),
  fetchRouterBackups: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchRouterRules: vi.fn(),
  fetchRouterNat: vi.fn(),
  fetchWatchlistEntries: vi.fn(),
  mintEnrolment: vi.fn(),
}))

import { fetchDevices, fetchRefusedSenders, fetchRouterBackups, fetchSetupStatus } from '../../lib/api'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import type { Device, SetupStatus } from '../../lib/types'
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
    eventCount: 69,
    status: 'live',
    ...over,
  }
}

const TITLES = ['The router', 'Mint the token', 'Paste once', 'Tag firewall rules', 'Where setup stands']

function row(title: string): HTMLButtonElement {
  const found = rows().find((b) => b.querySelector('.step-title')?.textContent === title)
  if (!found) throw new Error(`no rail row titled ${title}`)
  return found
}

// The rail's rows are found by their accessible name whether or not
// they are disabled; a locked row is still in the tree, just not a
// control you can press.
function rows(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('.rail .step-row'))
}

// Answer the router form and take Next, the way an operator would.
async function answerRouter() {
  await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
  await fireEvent.input(screen.getByLabelText(/^Its address/), { target: { value: '192.168.13.1' } })
  const [pushYes] = screen.getAllByRole('radio', { name: 'Yes' })
  const [, backupNo] = screen.getAllByRole('radio', { name: 'No' })
  await fireEvent.click(pushYes)
  await fireEvent.click(backupNo)
  await tick()
}

describe('Wizard: the full-screen shell', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
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
    wizardState.status = status()
    wizardState.devices = []
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('renders the bar, the strip and the five rows, with the router current', async () => {
    render(Wizard)
    await tick()
    expect(screen.getByText('MIKRO')).toBeTruthy()
    expect(screen.getByText('no router yet')).toBeTruthy()
    expect(document.querySelectorAll('.ovstrip .ovtick')).toHaveLength(1)
    expect(document.querySelector('.ovstrip .ovtick.on')).toBeNull()
    const list = rows()
    expect(list.map((b) => b.querySelector('.step-title')?.textContent)).toEqual(TITLES)
    expect(list[0].getAttribute('aria-current')).toBe('step')
    expect(list[4].querySelector('.step-n')?.textContent).toBe('✓')
    expect(screen.getByRole('status').textContent).toBe('Step 1 of 4 — The router — name, address, push, backup')
  })

  it('locks every step ahead of the furthest reached, with the reason in its title', async () => {
    render(Wizard)
    await tick()
    for (const b of rows().slice(1)) {
      expect(b.disabled).toBe(true)
      expect(b.getAttribute('aria-disabled')).toBe('true')
      expect(b.title).toBe('After the step before it')
      expect(b.classList.contains('locked')).toBe(true)
    }
    // The current row is not a control either, but it is not locked.
    expect(rows()[0].disabled).toBe(true)
    expect(rows()[0].getAttribute('aria-disabled')).toBeNull()
    expect(rows()[0].title).toBe('')
  })

  it('holds Next until all four are answered, then unlocks the token step', async () => {
    render(Wizard)
    await tick()
    const next = screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement
    expect(next.disabled).toBe(true)
    expect(screen.getByText('All four, then Next')).toBeTruthy()
    await answerRouter()
    expect(next.disabled).toBe(false)
    expect(row('The router').querySelector('.step-receipt')?.textContent).toBe(
      'rb5009 · 192.168.13.1 · push yes · backup not now',
    )
    expect(row('The router').classList.contains('chosen')).toBe(true)
    await fireEvent.click(next)
    await tick()
    expect(rows()[1].getAttribute('aria-current')).toBe('step')
    expect(rows()[1].title).toBe('')
    expect(screen.getByRole('button', { name: 'Mint the token' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Back' })).toBeTruthy()
  })

  it('lets an earlier completed row be clicked before the paste lands, and not after', async () => {
    render(Wizard)
    await tick()
    await answerRouter()
    await fireEvent.click(screen.getByRole('button', { name: 'Next' }))
    await tick()
    const router = rows()[0]
    expect(router.disabled).toBe(false)
    await fireEvent.click(router)
    await tick()
    expect(rows()[0].getAttribute('aria-current')).toBe('step')
    expect(screen.getByLabelText(/^Name/)).toBeTruthy()

    // The token minted and the block copied: the router is answering,
    // and the list is the record from here on.
    wizardState.enrolment = { token: 'demo0000demo0000demo', expiresAt: '2026-09-27T14:17:00Z' }
    wizardRun.q = 4
    wizardRun.stage = 'paste'
    await tick()
    expect(rows()[0].disabled).toBe(false)
    expect(rows()[1].disabled).toBe(false)
    wizardRun.markCopied()
    await tick()
    expect(rows()[2].getAttribute('aria-current')).toBe('step')
    for (const b of rows().slice(0, 3)) expect(b.disabled).toBe(true)
    expect(rows()[0].getAttribute('aria-disabled')).toBeNull()
    expect(rows()[3].getAttribute('aria-disabled')).toBe('true')
    expect(screen.getByText('Next checks what has arrived')).toBeTruthy()
  })

  it('adds chips as the evidence arrives and turns the strip green when logs flow', async () => {
    render(Wizard)
    await tick()
    await answerRouter()
    expect(document.querySelectorAll('.bar .chips .att')).toHaveLength(1)
    expect(document.querySelector('.bar .chips .att.dec')?.textContent).toBe('rb5009')

    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z' }],
    })
    await tick()
    expect(document.querySelector('.bar .chips .att.cert')?.textContent).toMatch(/^cert · /)
    expect(document.querySelector('.ovstrip .ovtick.on')).toBeNull()

    wizardState.ledgerDevice = 'rb5009'
    wizardState.devices = [device({ acceptedIp: '192.168.13.1', enrolledAt: '2026-09-27T14:03:04Z' })]
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [{ device: 'rb5009', configured: true, sourceIp: '192.168.13.1', events: 69, decodedActions: 12 }],
    })
    await tick()
    await waitFor(() => expect(document.querySelector('.bar .chips .att.logs')?.textContent).toMatch(/^logs · /))
    expect(document.querySelector('.ovstrip .ovtick.on')).not.toBeNull()
    expect(screen.getByText('69 lines')).toBeTruthy()
    expect(screen.getByText(/^live · \d+\/s$/)).toBeTruthy()
    expect(row('The router').classList.contains('done')).toBe(true)
    expect(row('The router').querySelector('.step-receipt')?.textContent).toMatch(/^rb5009 · enrolled 192\.168\.13\.1 · /)
  })

  // #1397: '+ add a router' is a walk for a router not yet named, so
  // another address's traffic is not its evidence. It borrowed the
  // fleet's first syslog source (right only for the first-run ledger),
  // placed the walk past The router, and never showed the form.
  it('starts an add-a-router walk at The router whatever else is sending', async () => {
    wizardState.status = status({
      sources: [{ source: '10.9.9.9', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [{ device: 'other', configured: true, sourceIp: '10.9.9.9', events: 300, decodedActions: 0 }],
    })
    wizardState.openAddRouter()
    render(Wizard)
    await tick()
    expect(screen.getByLabelText(/^Name/)).toBeTruthy()
    expect(row('The router').classList.contains('done')).toBe(false)
    expect(wizardRun.evidence.enrol).toBe('')
    expect(wizardRun.evidence.cert).toBe('')
    expect(wizardRun.evidence.lines).toBe(0)
  })

  // #1400: the certificate fetch leaves from whatever address routes to
  // MikroView, not always the one the router logs from. An add-a-router
  // walk counts a fetch made since its token was minted, and still not
  // one made before (#1397).
  it('counts a certificate fetched since the mint, from any address, and not one from before', async () => {
    wizardState.status = status({
      sources: [{ source: '10.9.9.9', caFetchedAt: '2026-09-27T14:00:00Z' }],
    })
    wizardState.openAddRouter()
    render(Wizard)
    await tick()
    wizardState.walkMintedAt = '2026-09-27T14:01:00Z'
    wizardState.enrolmentMintedAt = '2026-09-27T14:01:00Z'
    expect(wizardRun.evidence.cert).toBe('')

    wizardState.status = status({
      sources: [
        { source: '10.9.9.9', caFetchedAt: '2026-09-27T14:00:00Z' },
        { source: '10.0.0.7', caFetchedAt: '2026-09-27T14:02:58Z' },
      ],
    })
    expect(wizardRun.evidence.cert).toBe('2026-09-27T14:02:58Z')

    // #1401: the wrong-address recovery mints again after the fetch; the
    // fetch this walk's paste made still counts.
    wizardState.enrolmentMintedAt = '2026-09-27T14:04:00Z'
    expect(wizardRun.evidence.cert).toBe('2026-09-27T14:02:58Z')
  })

  // #1398: Re-enrol… on a router whose logs already stand lands on Mint
  // the token for it, not on the ledger its evidence would otherwise
  // place the walk at.
  it('lands Re-enrol… on Mint the token even with the router already sending', async () => {
    wizardState.devices = [device({ acceptedIp: '192.168.13.1', enrolledAt: '2026-09-27T14:03:04Z' })]
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [{ device: 'rb5009', configured: true, sourceIp: '192.168.13.1', events: 69, decodedActions: 12 }],
    })
    wizardState.openReEnrol('rb5009')
    render(Wizard)
    await tick()
    expect(wizardRun.stage).toBe('ask')
    expect(wizardRun.q).toBe(4)
  })

  // #1404: Run setup… places the run on the server's evidence as it
  // stands when the door opens, not on the read taken at sign-in.
  it('places Run setup… on a fresh read of the evidence, not the one held since sign-in', async () => {
    wizardState.open = false
    vi.mocked(fetchSetupStatus).mockResolvedValue(
      status({ sources: [{ source: '192.168.13.1', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }] }),
    )
    wizardState.launch()
    render(Wizard)
    await waitFor(() => expect(wizardRun.evidence.enrol).toBe('2026-09-27T14:03:04Z'))
    await waitFor(() => expect(wizardRun.stage).not.toBe('ask'))
  })

  it('keeps what was typed before the fresh read lands', async () => {
    wizardState.open = false
    let land: (s: SetupStatus) => void = () => {}
    vi.mocked(fetchSetupStatus).mockReturnValue(new Promise((r) => (land = r)))
    wizardState.launch()
    render(Wizard)
    await tick()
    await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
    land(status({ sources: [{ source: '192.168.13.1', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }] }))
    await waitFor(() => expect(wizardRun.evidence.enrol).toBe('2026-09-27T14:03:04Z'))
    await tick()
    expect(wizardRun.stage).toBe('ask')
    expect(wizardRun.name).toBe('rb5009')
  })

  it('is not in the tree while closed', async () => {
    wizardState.open = false
    render(Wizard)
    await tick()
    expect(document.querySelector('.page.wiz')).toBeNull()
  })
})
