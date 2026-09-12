// SPDX-License-Identifier: AGPL-3.0-only
//
// Log every rule (#435; "Tune logging" until #1134) -- these exercise
// the real component markup
// against a mocked network boundary, the same convention
// SetupWizard.svelte.test.ts uses: only fetch is faked, so what is
// tested here is what the operator can and cannot see and do.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchRouterRules: vi.fn(),
  fetchCoverageDeclarations: vi.fn(),
  fetchTuneLoggingAnalyse: vi.fn(),
  fetchTuneLoggingRender: vi.fn(),
}))

// The Clipboard API and Blob/URL.createObjectURL are unreliable in
// jsdom -- faked at the module boundary so tests exercise this
// component's own wiring (which button does what, when the guard is
// set) rather than jsdom's approximation of the browser.
vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
}))
vi.mock('../lib/export', () => ({
  downloadText: vi.fn(),
}))

import { fetchCoverageDeclarations, fetchRouterRules, fetchTuneLoggingAnalyse, fetchTuneLoggingRender } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import { downloadText } from '../lib/export'
import { appState } from '../lib/state.svelte'
import { policyState } from '../lib/policy.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { logEveryRuleNavState } from '../lib/logEveryRuleNav.svelte'
import type { Device, TuneLoggingAnalyseResponse, TuneLoggingRenderResponse } from '../lib/types'
import LogEveryRule from './LogEveryRule.svelte'

function device(over: Partial<Device> = {}): Device {
  return {
    id: 'edge-1',
    name: 'edge-1',
    sourceIp: '192.0.2.1',
    configured: true,
    firstSeen: '2026-08-01T00:00:00Z',
    lastSeen: '2026-09-03T00:00:00Z',
    eventCount: 100,
    status: 'live',
    ...over,
  }
}

// The contract's own §3 sample, plus a second, non-dark rule to exercise
// the collapsed group.
function analyseResponse(over: Partial<TuneLoggingAnalyseResponse> = {}): TuneLoggingAnalyseResponse {
  return {
    ready: true,
    observing: { since: '2026-09-01T10:00:00Z', hours: 51 },
    routeros: { version: '7.24.1', standing: 'reviewed', dialect: 'a' },
    rules: [
      {
        id: 3,
        chain: 'forward',
        action: 'accept',
        comment: 'lan to wan',
        inInterface: 'bridge',
        outInterface: 'ether1',
        inInterfaceList: '',
        outInterfaceList: '',
        boundary: 'bridge|ether1',
        crossesDark: true,
        log: false,
        logPrefix: '',
        packets: 41230,
        bytes: 8817212,
        countersKnown: true,
        line: 41,
      },
      {
        id: 7,
        chain: 'forward',
        action: 'drop',
        comment: 'block guest to lan',
        inInterface: 'guest',
        outInterface: 'bridge',
        inInterfaceList: '',
        outInterfaceList: '',
        boundary: 'guest|bridge',
        crossesDark: false,
        log: true,
        logPrefix: 'D|drop|',
        packets: 0,
        bytes: 0,
        countersKnown: false,
        line: 55,
      },
    ],
    rejected: null,
    ...over,
  }
}

function renderResponse(over: Partial<TuneLoggingRenderResponse> = {}): TuneLoggingRenderResponse {
  return {
    annotated: 'ANNOTATED EXPORT TEXT',
    commands: '/ip firewall filter set [find comment="lan to wan"] log=yes log-prefix="A|accept|"',
    changed: 1,
    routeros: { version: '7.24.1', standing: 'reviewed', dialect: 'a' },
    ...over,
  }
}

// A miniature export with two `add` lines under /ip firewall filter --
// enough for the zone's own rule count to be a number worth asserting.
const EXPORT_TEXT = [
  '# 2026/09/01 10:00:00 by RouterOS 7.24.1',
  '/ip firewall filter',
  'add action=accept chain=forward comment="lan to wan"',
  'add action=drop chain=forward comment="block guest to lan"',
  '',
].join('\n')

// ClipboardEvent and DataTransfer are not in jsdom, so both are built
// by hand here rather than through fireEvent.paste/fireEvent.drop --
// the component only ever reads `clipboardData.getData` and
// `dataTransfer.files`, which is exactly what these carry.
function zoneOf(container: HTMLElement): HTMLElement {
  return container.querySelector('.drop') as HTMLElement
}

async function pasteExport(container: HTMLElement, text = EXPORT_TEXT) {
  const evt = new Event('paste', { bubbles: true, cancelable: true })
  Object.defineProperty(evt, 'clipboardData', { value: { getData: () => text } })
  await fireEvent(zoneOf(container), evt)
}

async function dropFile(container: HTMLElement, name = 'edge-1.rsc', text = EXPORT_TEXT) {
  const evt = new Event('drop', { bubbles: true, cancelable: true })
  const file = new File([text], name, { type: 'text/plain' })
  Object.defineProperty(evt, 'dataTransfer', { value: { files: [file], getData: () => '' } })
  await fireEvent(zoneOf(container), evt)
}

// The tests below that are about something other than the intake keep
// using a paste, since #1134 left the page no other way to put an
// export into it.
async function typeExport(container: HTMLElement, text = EXPORT_TEXT) {
  await pasteExport(container, text)
  await waitFor(() => expect(zoneOf(container).classList.contains('filled')).toBe(true))
}

async function clickAnalyse() {
  await fireEvent.click(screen.getByRole('button', { name: 'Analyse' }))
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(fetchRouterRules).mockResolvedValue({ available: false, rules: [] })
  vi.mocked(fetchCoverageDeclarations).mockResolvedValue([])
  // resetAllMocks clears the mockResolvedValue set at module-definition
  // time above, same as every other vi.mock in this codebase's tests --
  // re-armed here rather than dropping the reset, so a test that
  // forgets to configure a return explicitly gets an obvious `undefined`
  // rather than a stale value from a previous test.
  vi.mocked(copyToClipboard).mockResolvedValue(true)
  appState.devices = [device()]
  policyState.pushed = []
  policyState.byDevice = {}
  policyState.anyPushed = false
  coverageState.declarations = []
  logEveryRuleNavState.pending = null
})

describe('LogEveryRule ephemerality', () => {
  it('states the issue\'s own ephemerality sentence, verbatim', () => {
    const { container } = render(LogEveryRule)
    expect(container.textContent).toContain(
      'Your config is never stored — it runs through memory, and once you leave this page it is gone.',
    )
  })
})

describe('LogEveryRule says what it is for (#1134)', () => {
  it('leads with the ruling\'s own sentence, before any control', () => {
    const { container } = render(LogEveryRule)
    const lead = container.querySelector('.lead') as HTMLElement
    expect(lead.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      "Drop in your router's export (/export hide-sensitive). You get it back with logging switched on for every " +
        'firewall rule that is not logging yet, ready to paste into the router. Nothing you paste is stored.',
    )
    // First on the page, and ahead of the drop zone it describes.
    expect(lead.compareDocumentPosition(zoneOf(container)) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('names the page "log every rule", not "tune logging"', () => {
    const { container } = render(LogEveryRule)
    expect(container.querySelector('.og h3')?.textContent).toBe('log every rule')
  })
})

describe('LogEveryRule drop zone (#1134)', () => {
  it('is one control, with the browser\'s own file input kept but never shown', () => {
    const { container } = render(LogEveryRule)
    // The raw "Browse… No file selected" control the owner rejected is
    // still there -- it is the only way to open a file chooser -- but
    // it is out of the page's own reading and tab order.
    const input = container.querySelector('input[type="file"]') as HTMLInputElement
    expect(input.classList.contains('sr-only')).toBe(true)
    expect(input.getAttribute('aria-hidden')).toBe('true')
    expect(input.tabIndex).toBe(-1)
    expect(container.querySelector('textarea')).toBeNull()
  })

  it('takes a dropped file, and says its name and how many rules are in it', async () => {
    const { container } = render(LogEveryRule)
    await dropFile(container, 'edge-1.rsc')

    await waitFor(() => expect(container.querySelector('.drop-picked')?.textContent).toBe('edge-1.rsc'))
    expect(container.querySelector('.drop-sub')?.textContent).toContain('2 firewall rules in it')
    expect(zoneOf(container).classList.contains('filled')).toBe(true)
  })

  it('takes a paste, which has no file name of its own to show', async () => {
    const { container } = render(LogEveryRule)
    await pasteExport(container)

    await waitFor(() => expect(container.querySelector('.drop-picked')?.textContent).toBe('pasted export'))
    expect(container.querySelector('.drop-sub')?.textContent).toContain('2 firewall rules in it')
  })

  it('sends what was dropped to analyse, so the zone really is the input', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    const { container } = render(LogEveryRule)
    await dropFile(container)
    await waitFor(() => expect(zoneOf(container).classList.contains('filled')).toBe(true))
    await clickAnalyse()

    await waitFor(() => expect(fetchTuneLoggingAnalyse).toHaveBeenCalled())
    expect(vi.mocked(fetchTuneLoggingAnalyse).mock.calls[0][0].export).toBe(EXPORT_TEXT)
  })

  it('leaves a paste aimed at a real field alone', async () => {
    appState.devices = [device({ id: 'edge-1' }), device({ id: 'edge-2', name: 'edge-2' })]
    const { container } = render(LogEveryRule)
    const select = container.querySelector('#ler-device') as HTMLSelectElement
    const evt = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(evt, 'clipboardData', { value: { getData: () => 'not an export' } })
    await fireEvent(select, evt)

    expect(zoneOf(container).classList.contains('filled')).toBe(false)
  })
})

// #1186: junk pasted in came back as "Watching for 0 hours", which
// reads as "nothing yet" rather than "that was not an export" -- and
// the 24-hour gate itself was never mentioned until after the operator
// had done the work of finding and pasting a config.
describe('LogEveryRule bad input and the 24-hour gate (#1186)', () => {
  it('says the pasted text is not an export, and holds Analyse', async () => {
    const { container } = render(LogEveryRule)
    await pasteExport(container, 'the quick brown fox jumps over the lazy dog')

    await waitFor(() => expect(container.querySelector('.load-error')).toBeTruthy())
    expect(container.querySelector('.load-error')?.textContent).toContain('no /ip firewall filter section')
    expect((screen.getByRole('button', { name: 'Analyse' }) as HTMLButtonElement).disabled).toBe(true)

    await clickAnalyse()
    expect(fetchTuneLoggingAnalyse).not.toHaveBeenCalled()
  })

  it('says nothing about a real export, and leaves Analyse free', async () => {
    const { container } = render(LogEveryRule)
    await typeExport(container)

    expect(container.querySelector('.load-error')).toBeNull()
    expect((screen.getByRole('button', { name: 'Analyse' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('states the 24-hour gate before anything has been pasted', () => {
    const { container } = render(LogEveryRule)
    expect(container.textContent).toContain('24 hours of watching the router')
  })
})

describe('LogEveryRule under 24 hours (#435 decision 5)', () => {
  it('shows the waiting message and no rule list', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(
      analyseResponse({ ready: false, rules: [], observing: { since: '2026-09-02T12:00:00Z', hours: 9 } }),
    )
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()

    await waitFor(() => expect(container.querySelector('.observation.waiting')).toBeTruthy())
    expect(container.textContent).toContain('Watching for 9 hours; suggestions arrive at 24 hours.')
    expect(container.querySelectorAll('.rule-row').length).toBe(0)
  })
})

describe('LogEveryRule rejected export (#435 §5)', () => {
  it('shows the rejection reason and no rule list', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(
      analyseResponse({ rejected: { reason: 'a value for "password" was found on line 12 -- not hide-sensitive' } }),
    )
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()

    await waitFor(() =>
      expect(container.textContent).toContain('a value for "password" was found on line 12 -- not hide-sensitive'),
    )
    expect(container.querySelectorAll('.rule-row').length).toBe(0)
  })
})

describe('LogEveryRule rule selection defaults (#435 decision 3)', () => {
  it('ticks every crosses-dark rule and shows it open; the rest stay collapsed and unticked', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()

    await waitFor(() => expect(container.querySelectorAll('.rule-row').length).toBe(1))
    const darkCheckbox = container.querySelector('.rule-row input') as HTMLInputElement
    expect(darkCheckbox.checked).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: /Show the other 1 rule/ }))
    const rows = container.querySelectorAll('.rule-row')
    expect(rows.length).toBe(2)
    const otherCheckbox = rows[1].querySelector('input') as HTMLInputElement
    expect(otherCheckbox.checked).toBe(false)
  })

  it('renders counters as "fired N times / M bytes since <date>" only when countersKnown', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()

    await waitFor(() => expect(container.querySelector('.rule-counter')).toBeTruthy())
    const since = new Date('2026-09-01T10:00:00Z').toLocaleString()
    expect(container.querySelector('.rule-counter')?.textContent).toBe(
      `fired 41,230 times / 8,817,212 bytes since ${since}`,
    )

    await fireEvent.click(screen.getByRole('button', { name: /Show the other 1 rule/ }))
    const rows = container.querySelectorAll('.rule-row')
    expect(rows[1].querySelector('.rule-counter')).toBeNull()
  })
})

describe('LogEveryRule render (#435 §4/§6)', () => {
  async function renderToResult() {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    vi.mocked(fetchTuneLoggingRender).mockResolvedValue(renderResponse())
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()
    await waitFor(() => expect(container.querySelectorAll('.rule-row').length).toBe(1))
    await fireEvent.click(screen.getByRole('button', { name: /^Render/ }))
    await waitFor(() => expect(container.querySelector('.render-result')).toBeTruthy())
    return container
  }

  it('sends the selected rule ids from the ticked defaults', async () => {
    await renderToResult()
    expect(fetchTuneLoggingRender).toHaveBeenCalledWith(
      expect.objectContaining({ device: 'edge-1', selected: [3] }),
    )
  })

  it('draws download first, copy second, then the commands block with its own copy', async () => {
    const container = await renderToResult()
    const buttons = [...container.querySelectorAll('.render-result button')].map((b) => b.textContent?.trim())
    expect(buttons[0]).toContain('Download')
    expect(buttons[1]).toContain('Copy the annotated export')
    expect(buttons[2]).toBe('Copy')
    expect(container.querySelector('.render-result pre')?.textContent).toBe(renderResponse().commands)
  })

  it('download calls downloadText with the device-named file and the annotated export', async () => {
    const container = await renderToResult()
    await fireEvent.click(screen.getByRole('button', { name: /Download/ }))
    expect(downloadText).toHaveBeenCalledWith('edge-1-logging.rsc', 'ANNOTATED EXPORT TEXT')
  })
})

describe('LogEveryRule beforeunload guard', () => {
  function dispatchBeforeUnload(): Event {
    const evt = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(evt)
    return evt
  }

  it('is set only while a rendered result exists that was neither downloaded nor copied', async () => {
    // No result yet: nothing to guard.
    render(LogEveryRule)
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })

  it('is set once a render lands, and cleared by downloading it', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    vi.mocked(fetchTuneLoggingRender).mockResolvedValue(renderResponse())
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()
    await waitFor(() => expect(container.querySelectorAll('.rule-row').length).toBe(1))
    await fireEvent.click(screen.getByRole('button', { name: /^Render/ }))
    await waitFor(() => expect(container.querySelector('.render-result')).toBeTruthy())

    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: /Download/ }))
    await waitFor(() => expect(dispatchBeforeUnload().defaultPrevented).toBe(false))
  })

  it('is also cleared by copying instead of downloading', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    vi.mocked(fetchTuneLoggingRender).mockResolvedValue(renderResponse())
    const { container } = render(LogEveryRule)
    await typeExport(container)
    await clickAnalyse()
    await waitFor(() => expect(container.querySelectorAll('.rule-row').length).toBe(1))
    await fireEvent.click(screen.getByRole('button', { name: /^Render/ }))
    await waitFor(() => expect(container.querySelector('.render-result')).toBeTruthy())
    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: 'Copy the annotated export' }))
    await waitFor(() => expect(dispatchBeforeUnload().defaultPrevented).toBe(false))
  })
})

describe('LogEveryRule device pick', () => {
  it('auto-picks the only known device, with no picker shown', async () => {
    vi.mocked(fetchTuneLoggingAnalyse).mockResolvedValue(analyseResponse())
    const { container } = render(LogEveryRule)
    expect(container.querySelector('#ler-device')).toBeNull()
    await typeExport(container)
    await clickAnalyse()
    await waitFor(() => expect(fetchTuneLoggingAnalyse).toHaveBeenCalled())
    expect(vi.mocked(fetchTuneLoggingAnalyse).mock.calls[0][0].device).toBe('edge-1')
  })

  it('shows a picker, pre-selected from the topography\'s dark-pair handoff', async () => {
    appState.devices = [device({ id: 'edge-1' }), device({ id: 'edge-2', name: 'edge-2' })]
    logEveryRuleNavState.request('edge-2', 'bridge|ether1')
    const { container } = render(LogEveryRule)
    await waitFor(() => expect(container.querySelector('#ler-device')).toBeTruthy())
    const select = container.querySelector('#ler-device') as HTMLSelectElement
    expect(select.value).toBe('edge-2')
  })
})
