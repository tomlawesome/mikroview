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
  fetchBlocklistBuilder: vi.fn(),
  fetchBlocklistCommands: vi.fn(),
  markSetupStep: vi.fn(),
  createToken: vi.fn(),
}))

import {
  createToken,
  fetchBlocklistBuilder,
  fetchBlocklistCommands,
  fetchDevices,
  fetchRefusedSenders,
  fetchRouterBackups,
  fetchSetupStatus,
  markSetupStep,
  type BlocklistBuilder,
} from '../../lib/api'
import { blocklistState } from '../../lib/blocklist.svelte'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import { wizardJourney } from '../../lib/wizardJourney.svelte'
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

  // #1406: a run placed from fleet-wide evidence (no router record --
  // '+ add a router' and Re-enrol… both name the run themselves) used
  // to leave wizardRun.name empty, so Where setup stands read " is
  // sending." Falling back to the evidence's own sending address reads
  // sensibly instead, and never touches a walk that names itself.
  it('names the run for the sending address when fleet-wide evidence places it with no router record', async () => {
    wizardState.status = status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
    })
    render(Wizard)
    await tick()
    expect(wizardRun.name).toBe('192.168.13.1')
    expect(screen.getByText('192.168.13.1 is sending.')).toBeTruthy()
  })

  it('is not in the tree while closed', async () => {
    wizardState.open = false
    render(Wizard)
    await tick()
    expect(document.querySelector('.page.wiz')).toBeNull()
  })

  // The way in's beats (#1386, DESIGN.md's sixth beat): the rail and
  // the body stand as the rows strike (4.55s); the footer has its own
  // beat (5.35s) and stays away until it. It used to come with the
  // rail, 800ms early, and then strike a second time.
  it('on the way in, the footer waits for its own beat after the rail and body stand', async () => {
    wizardState.open = false
    wizardJourney.begin('in')
    wizardState.open = true
    render(Wizard)
    await tick()
    const away = (sel: string) => document.querySelector(sel)?.classList.contains('away')
    expect(away('.wiz .rail')).toBe(true)
    expect(away('.wiz .body')).toBe(true)
    expect(away('.wiz .foot')).toBe(true)
    wizardJourney.rows = true
    await tick()
    expect(away('.wiz .rail')).toBe(false)
    expect(away('.wiz .body')).toBe(false)
    expect(away('.wiz .foot')).toBe(true)
    wizardJourney.foot = true
    await tick()
    expect(away('.wiz .foot')).toBe(false)
    wizardJourney.end()
  })

  // The way out: the bar's wordmark goes as the ride leaves it and comes
  // back when the ride lands (an.js's wayOut). Otherwise the bar keeps
  // its wordmark while a second one rides to the centre.
  it('on the way out, the bar gives up its wordmark until the ride lands', async () => {
    render(Wizard)
    await waitFor(() => expect(document.querySelector('.page.wiz.live')).not.toBeNull())
    wizardJourney.begin('out')
    await tick()
    expect(document.querySelector('.page.wiz.live')).toBeNull()
    wizardJourney.landed = true
    await tick()
    expect(document.querySelector('.page.wiz.live')).not.toBeNull()
    wizardJourney.end()
  })
})

// #1360's first-run tail (round 2, tail.html; owner, 2a).
describe('Wizard: the first-run tail', () => {
  const sending = () =>
    status({
      sources: [{ source: '192.168.13.1', caFetchedAt: '2026-09-27T14:02:58Z', syslogFirstSeenAt: '2026-09-27T14:03:04Z' }],
      devices: [{ device: 'rb5009', configured: true, sourceIp: '192.168.13.1', events: 69, decodedActions: 0 }],
    })

  function builder(held: boolean): BlocklistBuilder {
    return {
      device: 'rb5009',
      deviceName: 'rb5009',
      devices: [{ id: 'rb5009', name: 'rb5009' }],
      routerosVersion: '7.24.4',
      reportedAt: '2026-09-27T14:23:00Z',
      reviewedVersion: '7.24.4',
      minimumVersion: '7.18',
      standing: 'ok',
      pushCurrent: held,
      catalogueDate: '2026-10-01',
      catalogue: [
        { key: 'spamhaus', name: 'Spamhaus DROP', short: 'Spamhaus DROP', url: 'u', terms: 't', default: true, defaultDirection: 'from', ipv6: true, refresh: [{ value: 'daily' }], refreshDefault: 'daily', facts: 'f', guide: 'g', flaggedByMikroView: true, startTime: '04:17' },
        { key: 'et', name: 'Emerging Threats compromised IPs', short: 'Emerging Threats', url: 'u', terms: 't', default: true, defaultDirection: 'both', ipv6: false, refresh: [{ value: 'daily' }], refreshDefault: 'daily', facts: 'f', guide: 'g', flaggedByMikroView: true, startTime: '04:31' },
      ],
      leftOut: [],
      lists: [
        held
          ? { key: 'spamhaus', state: 'held', count: 1692, count6: 0, loadedAt: '2026-09-27 14:07:10', firedToday: 3, flags24h: 0, undo: 'u' }
          : { key: 'spamhaus', state: 'off', count: 0, count6: 0, firedToday: null, flags24h: null, undo: 'u' },
        { key: 'et', state: 'off', count: 0, count6: 0, firedToday: null, flags24h: null, undo: 'u' },
      ],
      ownDroplist: { held: 0, total: 0 },
      undoAll: '/ip firewall raw remove [find comment~"^mikroview blocklist: "]',
      disableAll: 'd',
    }
  }

  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(sending())
    vi.mocked(fetchDevices).mockResolvedValue([device({ acceptedIp: '192.168.13.1', enrolledAt: '2026-09-27T14:03:04Z' })])
    vi.mocked(fetchRefusedSenders).mockResolvedValue([])
    vi.mocked(fetchRouterBackups).mockResolvedValue({ enabled: true, keyUnreadable: false, routers: [], totalGenerations: 0, totalRouters: 0, totalBytes: 0, lock: 'open' } as never)
    vi.mocked(fetchBlocklistBuilder).mockResolvedValue(builder(false))
    vi.mocked(fetchBlocklistCommands).mockResolvedValue({ parts: [{ ordinal: 1, ink: 'push', title: 'The push', note: [], shown: [], commands: 'x' }], copyText: 'x' })
    vi.mocked(markSetupStep).mockResolvedValue({ step: 8, outcome: 'skipped', actor: 'admin', at: '2026-09-27T14:10:00Z' } as never)
    vi.mocked(createToken).mockResolvedValue({ value: 'tok' } as never)
    wizardState.reset()
    wizardRun.reset()
    blocklistState.reset()
    wizardState.status = sending()
    wizardState.devices = [device({ acceptedIp: '192.168.13.1', enrolledAt: '2026-09-27T14:03:04Z' })]
    wizardState.ledgerDevice = 'rb5009'
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    blocklistState.reset()
    vi.clearAllMocks()
  })

  async function onTheLedger() {
    render(Wizard)
    await waitFor(() => expect(wizardRun.stage).toBe('done'))
    await waitFor(() => expect(blocklistState.data).not.toBeNull())
    await tick()
  }

  it('offers a sixth row, a ledger row with Set it up, and a button beside Finish on the launch walk', async () => {
    await onTheLedger()
    expect(rows().map((b) => b.querySelector('.step-title')?.textContent)).toEqual([...TITLES, 'Block known-bad addresses'])
    const tail = row('Block known-bad addresses')
    expect(tail.classList.contains('offer')).toBe(true)
    expect(tail.querySelector('.step-n')?.textContent).toBe('+')
    expect(screen.getByText(/not blocked yet — rb5009 lets them in, and MikroView flags them from Spamhaus DROP and Emerging Threats ·/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Set it up' })).toBeTruthy()
    const foot = Array.from(document.querySelectorAll<HTMLButtonElement>('.foot button')).map((b) => [b.textContent?.trim(), b.classList.contains('primary')])
    expect(foot).toEqual([
      ['Add another router', false],
      ['Block known-bad addresses', false],
      ['Finish', true],
    ])
    // An offer nobody took mints nothing.
    expect(createToken).not.toHaveBeenCalled()
  })

  it('does not offer it on Add another router or Re-enrol…', async () => {
    wizardState.openAddRouter()
    const { unmount } = render(Wizard)
    await tick()
    expect(rows().length).toBe(5)
    unmount()
    wizardRun.reset()
    wizardState.openReEnrol('rb5009')
    render(Wizard)
    await tick()
    expect(wizardRun.tailOffered).toBe(false)
    expect(fetchBlocklistBuilder).not.toHaveBeenCalled()
  })

  it('opens the builder in the wizard’s frame, with Back · Not now · Copy · Finish', async () => {
    await onTheLedger()
    await fireEvent.click(screen.getByRole('button', { name: 'Set it up' }))
    await waitFor(() => expect(document.querySelector('.body.wide')).not.toBeNull())
    expect(screen.getByText('Block known-bad addresses on rb5009.')).toBeTruthy()
    expect(row('Block known-bad addresses').getAttribute('aria-current')).toBe('step')
    await waitFor(() => expect(document.querySelector('.foot button.primary')?.textContent?.trim()).toBe('Copy — 2 lists · 1 part'))
    expect(Array.from(document.querySelectorAll('.foot button')).map((b) => b.textContent?.trim())).toEqual(['Back', 'Not now', 'Copy — 2 lists · 1 part', 'Finish'])
    expect((document.querySelector('.foot button.primary') as HTMLButtonElement).disabled).toBe(false)
  })

  it('keeps Where setup stands clickable on the stage, and it goes back', async () => {
    await onTheLedger()
    await fireEvent.click(screen.getByRole('button', { name: 'Set it up' }))
    await waitFor(() => expect(wizardRun.stage).toBe('block'))
    const stand = row('Where setup stands') as HTMLButtonElement
    expect(stand.disabled).toBe(false)
    expect(stand.classList.contains('locked')).toBe(false)
    expect(stand.querySelector('.step-n')?.textContent).toBe('✓')
    await fireEvent.click(stand)
    expect(wizardRun.stage).toBe('done')
  })

  it('records Not now under record 8 and reads the row as set aside', async () => {
    await onTheLedger()
    await fireEvent.click(screen.getByRole('button', { name: 'Block known-bad addresses' }))
    await waitFor(() => expect(wizardRun.stage).toBe('block'))
    const notNow = Array.from(document.querySelectorAll<HTMLButtonElement>('.foot button')).find((b) => b.textContent?.trim() === 'Not now')
    await fireEvent.click(notNow as HTMLButtonElement)
    await waitFor(() => expect(wizardRun.stage).toBe('done'))
    expect(markSetupStep).toHaveBeenCalledWith(8, 'skipped', 'not now · Settings ▸ drop list')
    expect(screen.getAllByText('not now · Settings ▸ drop list').length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: 'Set it up' })).toBeNull()
  })

  it('turns the row into a proof with its receipt once the push holds a list', async () => {
    vi.mocked(fetchBlocklistBuilder).mockResolvedValue(builder(true))
    await onTheLedger()
    expect(screen.getByText('Known-bad addresses dropped')).toBeTruthy()
    expect(screen.getByText(/Spamhaus DROP 1,692 held · loaded 14:07 · confirmed by the push \d\d:\d\d · rules fired 3/)).toBeTruthy()
    expect(row('Block known-bad addresses').classList.contains('done')).toBe(true)
  })

  it('shows no sixth row on Run setup… once record 8 holds a mark or a witness', async () => {
    for (const st of [
      { ...sending(), marks: [{ step: 8, outcome: 'skipped' as const, actor: 'admin', at: '2026-09-27T14:10:00Z' }] },
      { ...sending(), witnesses: [{ step: 8, receipt: 'Spamhaus DROP 1,692 held · on rb5009', at: '2026-09-27T14:23:00Z' }] },
    ]) {
      vi.mocked(fetchSetupStatus).mockResolvedValue(st)
      wizardState.status = st
      const { unmount } = render(Wizard)
      await waitFor(() => expect(wizardRun.stage).toBe('done'))
      await tick()
      expect(rows().length).toBe(5)
      expect(screen.queryByRole('button', { name: 'Set it up' })).toBeNull()
      unmount()
    }
  })
})
