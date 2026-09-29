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

// Only the network boundary is faked: the pushed rule table the proposal
// is read from, and the status the count of decoded lines comes from.
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

import { fetchDevices, fetchRefusedSenders, fetchRouterBackups, fetchRouterRules, fetchSetupStatus, type RouterFilterRule } from '../../lib/api'
import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import type { Device, SetupStatus } from '../../lib/types'
import StepTune from './StepTune.svelte'
import Wizard from './Wizard.svelte'

function status(decoded: number): SetupStatus {
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
        decodedActions: decoded,
        pushedKinds: { 'filter-rule': '2026-09-27T14:03:31Z' },
      },
    ],
    pushKinds: ['filter-rule'],
    marks: [],
    witnesses: [],
  }
}

function device(): Device {
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
  }
}

function rule(over: Partial<RouterFilterRule> & { ordinal: number }): RouterFilterRule {
  return { comment: '', chain: 'forward', action: 'drop', srcAddressList: '', logPrefix: '', log: false, ...over }
}

// The prototype's data story: 47 rules read, three logging, five dark.
const TABLE: RouterFilterRule[] = [
  rule({ ordinal: 0, chain: 'input', action: 'accept', comment: 'ssh from mgmt', inInterface: 'bridge', log: true, logPrefix: 'A|in-ssh|' }),
  rule({ ordinal: 1, chain: 'forward', action: 'accept', comment: 'established, related', connectionState: ['established', 'related'], log: true, logPrefix: 'A|est-rel|' }),
  rule({ ordinal: 2, chain: 'input', action: 'accept', comment: 'icmp', log: true, logPrefix: 'A|icmp|' }),
  rule({ ordinal: 3, chain: 'forward', action: 'drop', comment: 'drop everything else', inInterface: 'bridge', outInterface: 'ether1' }),
  rule({ ordinal: 4, chain: 'input', action: 'drop', comment: 'drop from WAN', inInterface: 'ether1' }),
  rule({ ordinal: 5, chain: 'forward', action: 'accept', comment: 'port-forward 443', inInterface: 'ether1', outInterface: 'bridge' }),
  rule({ ordinal: 6, chain: 'forward', action: 'drop', comment: 'guest to LAN', inInterface: 'bridge-guest', outInterface: 'bridge' }),
  rule({ ordinal: 7, chain: 'input', action: 'drop', comment: 'wg0 to input', inInterface: 'wg0' }),
]

function rows(): HTMLLabelElement[] {
  return Array.from(document.querySelectorAll<HTMLLabelElement>('.rules label:not(.h)'))
}

function boxes(): HTMLInputElement[] {
  return Array.from(document.querySelectorAll<HTMLInputElement>('.rules input[type=checkbox]'))
}

function copyButton(): HTMLButtonElement {
  return screen.getByRole('button', { name: /^Copy|^Copied$/ }) as HTMLButtonElement
}

// The run as it stands when Next on the router's turn lands here: the
// router enrolled, the push arrived, everything chosen has arrived.
function landOnTune() {
  wizardState.ledgerDevice = 'rb5009'
  wizardState.devices = [device()]
  wizardState.status = status(12)
  wizardRun.name = 'rb5009'
  wizardRun.addr = '192.168.13.1'
  wizardRun.push = true
  wizardRun.backup = false
  wizardRun.copied = true
  wizardRun.stage = 'tune'
}

describe('StepTune: the rule list proposed from the push', () => {
  let written: string[]

  beforeEach(() => {
    written = []
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: (t: string) => (written.push(t), Promise.resolve()) },
    })
    vi.mocked(fetchSetupStatus).mockResolvedValue(status(12))
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
    vi.mocked(fetchRouterRules).mockResolvedValue({ available: true, updatedAt: '2026-09-27T14:03:31Z', rules: TABLE })
    wizardState.reset()
    wizardRun.reset()
    landOnTune()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  it('leads with the counts, lists the dark rules with their boundary and prefix, all ticked', async () => {
    render(StepTune)
    await waitFor(() => expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('5 rules log nothing.'))
    expect(fetchRouterRules).toHaveBeenCalledWith('rb5009')
    expect(screen.getByText(/^The push read 8 rules; 3 already log\. These 5 sit on boundaries/)).toBeTruthy()
    const list = rows()
    expect(list).toHaveLength(5)
    expect(list.map((l) => l.querySelector('.mono')?.textContent)).toEqual(['forward', 'input', 'forward', 'forward', 'input'])
    expect(list.map((l) => l.querySelector('.act')?.textContent)).toEqual(['drop', 'drop', 'accept', 'drop', 'drop'])
    expect(list[2].querySelector('.act')?.classList.contains('accept')).toBe(true)
    expect(list.map((l) => l.querySelector('.why')?.textContent)).toEqual([
      'bridge → ether1',
      'ether1 · input',
      'ether1 → bridge',
      'bridge-guest → bridge',
      'wg0 · input',
    ])
    expect(list.map((l) => l.querySelector('.pre')?.textContent)).toEqual([
      'D|drop-everyth|',
      'D|drop-from-wa|',
      'A|port-forward|',
      'D|guest-to-lan|',
      'D|wg0-to-input|',
    ])
    expect(boxes().every((b) => b.checked && !b.disabled)).toBe(true)
    expect(copyButton().textContent?.trim()).toBe('Copy — 5 rules')
    expect(copyButton().disabled).toBe(false)
    expect(screen.getByText('Safe to paste again; it sets, never adds.')).toBeTruthy()
    expect(document.querySelector('.obs.quiet')?.textContent).toBe('Nothing to wait for until you paste — the rule table is already here.')
    const block = screen.getByLabelText('Tagging block').textContent ?? ''
    expect(block.split('\n')).toHaveLength(5)
    expect(block).toContain('/ip firewall filter set [find comment="drop from WAN"] log=yes log-prefix="D|drop-from-wa|"')
    expect(block).not.toContain('add ')
  })

  it('unticking a rule takes it out of the block and the count; nothing ticked prints as such', async () => {
    render(StepTune)
    await waitFor(() => expect(rows()).toHaveLength(5))
    await fireEvent.click(boxes()[0])
    await tick()
    expect(copyButton().textContent?.trim()).toBe('Copy — 4 rules')
    expect(screen.getByLabelText('Tagging block').textContent).not.toContain('drop everything else')
    for (const b of boxes().slice(1)) await fireEvent.click(b)
    await tick()
    expect(screen.getByLabelText('Tagging block').textContent).toBe('# nothing ticked')
    expect(copyButton().disabled).toBe(true)
    await fireEvent.click(boxes()[4])
    await tick()
    expect(copyButton().textContent?.trim()).toBe('Copy — 1 rule')
  })

  it('Copy puts the block on the clipboard, fixes the ticks, and waits; the first new prefix flips it to counting', async () => {
    render(StepTune)
    await waitFor(() => expect(rows()).toHaveLength(5))
    await fireEvent.click(boxes()[2])
    await tick()
    await fireEvent.click(copyButton())
    await tick()
    expect(written).toHaveLength(1)
    expect(written[0].split('\n')).toHaveLength(4)
    expect(written[0]).not.toContain('port-forward')
    expect(copyButton().textContent?.trim()).toBe('Copied')
    expect(copyButton().disabled).toBe(true)
    expect(boxes().every((b) => b.disabled)).toBe(true)
    expect(wizardRun.tuneCopied).toBe(true)
    expect(wizardRun.chosenCount).toBe(4)
    expect(wizardRun.tuneChosen.map((r) => r.ordinal)).toEqual([3, 4, 6, 7])
    expect(document.querySelector('.obs.waiting')?.textContent).toBe('Waiting for the first line carrying a new prefix.')
    expect(document.querySelector('.rules label.lit')).toBeNull()

    // Lines that decoded before Copy do not count; the twelve already
    // there stay twelve until a new prefix lands.
    wizardRun.poll()
    await tick()
    expect(document.querySelector('.obs.waiting')).not.toBeNull()

    wizardState.status = status(13)
    wizardRun.poll()
    await tick()
    const counting = document.querySelector('.obs.counting')
    expect(counting).not.toBeNull()
    expect(counting?.textContent).toMatch(/^4 rules logging · first new line \d\d:\d\d:\d\d1,204 lines$/)
    // The ticked rows light; the unticked one does not.
    expect(rows().map((l) => l.classList.contains('lit'))).toEqual([true, true, false, true, true])
  })

  it('reads "needs the push" when the router was told not to push, or has pushed no rule table', async () => {
    wizardRun.push = false
    render(StepTune)
    await tick()
    expect(screen.getByText('Needs the push — the rule table is what proposes the rules to tag.')).toBeTruthy()
    expect(fetchRouterRules).toHaveBeenCalled()
  })

  it('reads "needs the push" when the store holds no rule table for this router', async () => {
    vi.mocked(fetchRouterRules).mockResolvedValue({ available: false, rules: [] })
    render(StepTune)
    await waitFor(() => expect(screen.getByText(/^Needs the push/)).toBeTruthy())
    expect(document.querySelector('.rules')).toBeNull()
  })
})

describe('StepTune in the shell: Skip and the receipt', () => {
  beforeEach(() => {
    vi.mocked(fetchSetupStatus).mockResolvedValue(status(12))
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
    vi.mocked(fetchRouterRules).mockResolvedValue({ available: true, rules: TABLE })
    wizardState.reset()
    wizardRun.reset()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
    vi.clearAllMocks()
  })

  function tuneRow(): HTMLButtonElement {
    const found = Array.from(document.querySelectorAll<HTMLButtonElement>('.rail .step-row')).find(
      (b) => b.querySelector('.step-title')?.textContent === 'Tag firewall rules',
    )
    if (!found) throw new Error('no rail row titled Tag firewall rules')
    return found
  }

  it('is proposed from the push while open, holds Next until a new prefix lands, and Skip leaves them dark', async () => {
    render(Wizard)
    await tick()
    landOnTune()
    await waitFor(() => expect(screen.getByRole('heading', { level: 3 }).textContent).toBe('5 rules log nothing.'))
    expect(tuneRow().getAttribute('aria-current')).toBe('step')
    expect(tuneRow().querySelector('.step-receipt')?.textContent).toBe('proposed from the push')
    const next = screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement
    expect(next.disabled).toBe(true)
    expect(screen.getByText('Next checks what has arrived')).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Skip this step' }))
    await tick()
    expect(wizardRun.stage).toBe('done')
    expect(tuneRow().classList.contains('chosen')).toBe(true)
    expect(tuneRow().querySelector('.step-receipt')?.textContent).toBe('left dark')
  })

  it('once tagged, the receipt counts the rules in the rules ink and the bar wears an "N rules" chip', async () => {
    render(Wizard)
    await tick()
    landOnTune()
    await waitFor(() => expect(document.querySelectorAll('.rules input[type=checkbox]')).toHaveLength(5))
    await fireEvent.click(screen.getByRole('button', { name: 'Copy — 5 rules' }))
    await tick()
    wizardState.status = status(20)
    wizardRun.poll()
    await tick()
    expect(tuneRow().classList.contains('done')).toBe(true)
    expect(tuneRow().querySelector('.step-receipt')?.textContent).toMatch(/^5 rules tagged · \d\d:\d\d:\d\d$/)
    expect(tuneRow().style.getPropertyValue('--ink')).toBe('var(--ink-rules)')
    expect(document.querySelector('.bar .chips .att.rules')?.textContent).toBe('5 rules')
    expect((screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement).disabled).toBe(false)
  })
})
