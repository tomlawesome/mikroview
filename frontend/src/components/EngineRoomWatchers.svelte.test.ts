// SPDX-License-Identifier: AGPL-3.0-only
//
// #787: the detector bench as a full editor. A row expands in place into
// typed tuning fields built from the server's own param schema, scope as
// removable chips, and reset/clone/save/cancel at its foot.
//
// What is worth pinning here, rather than in the pure-helper tests next
// door (lib/definitionEditor.test.ts):
//
//   - the fields come from GET /api/definitions/schema, not from the
//     paramSchema copy riding on the definition in the list. Both carry
//     the same value in production, so a test that mocked them alike
//     could not tell which one the panel read -- these mocks make them
//     deliberately disagree, which is the only way the question has an
//     answer.
//   - one panel open at a time.
//   - a viewer sees every row and every fact, and no control at all:
//     hidden, never disabled, the grammar the run/pause tick already set.
//   - reset and clone reach the endpoints that exist for them, and a
//     server refusal is shown in the operator's words, not swallowed.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/svelte'

const PORT_SCAN_SCHEMA = [
  {
    name: 'threshold',
    type: 'int',
    description: 'Distinct destination ports before a source is flagged.',
    min: 2,
    max: 1000,
  },
  {
    name: 'window',
    type: 'duration',
    description: 'How long the distinct-port count is accumulated over.',
    min: 1e9,
    max: 3600e9,
  },
]

vi.mock('../lib/api', () => ({
  fetchDefinitions: vi.fn(async () => ({
    definitions: [
      {
        id: 'port_scan',
        name: 'Port scan',
        intent: 'detection',
        kind: 'declarative',
        enabled: true,
        scope: { hosts: ['192.168.1.20'], hostsMode: 'deny', ports: [22], portsMode: 'allow' },
        params: { threshold: 15, window: '1m0s' },
        // Deliberately a *different* schema from the one the schema
        // endpoint serves, so a panel reading this copy instead would
        // render a field named "stale" and this suite would say so.
        paramSchema: [{ name: 'stale', type: 'int', description: 'the row copy' }],
        provenance: { origin: 'shipped' },
        available: true,
        replay: { known: true, capable: true },
      },
      {
        id: 'rule_spike',
        name: 'Rule hit-rate spike',
        intent: 'detection',
        kind: 'declarative',
        enabled: false,
        scope: { rules: ['r13'], rulesMode: 'allow' },
        params: {},
        provenance: { origin: 'shipped' },
        available: true,
        distance: { threshold: { shipped: 5, current: 3 } },
        replay: { known: true, capable: true },
      },
      // An operator-authored detector: structure stored as data (#502),
      // which is what makes it the one row on this bench the server can
      // copy (#810). Paused, so the run/pause wording stays unambiguous
      // for the rows above.
      {
        id: 'ssh_hammering',
        name: 'SSH hammering',
        intent: 'detection',
        kind: 'declarative',
        enabled: false,
        scope: {},
        params: { threshold: 5, window: '1m0s' },
        provenance: { origin: 'custom' },
        detection: {
          conditions: [{ field: 'destinationPort', operator: 'equals', values: ['22'] }],
          key: 'perSource',
          counting: 'total',
          detailTemplate: '{Count} attempts against port 22 from {SourceAddress}',
        },
        available: true,
        replay: { known: true, capable: true },
      },
    ],
    coverageEvidence: { complete: true },
  })),
  fetchDefinitionSchema: vi.fn(async () => ({
    port_scan: PORT_SCAN_SCHEMA,
    ssh_hammering: PORT_SCAN_SCHEMA,
    'copy-1': PORT_SCAN_SCHEMA,
  })),
  updateDefinition: vi.fn(async () => ({ id: 'port_scan' })),
  replayDefinition: vi.fn(async () => ({
    receipt: {
      window: {
        start: '2026-09-02T08:00:00Z',
        end: '2026-09-02T12:12:00Z',
        duration: '4h12m0s',
        eventCount: 8421,
      },
      emissionCount: 3,
      sample: [
        { at: '2026-09-02T09:04:00Z', target: '203.0.113.9', detail: '', provisional: false },
        { at: '2026-09-02T10:20:00Z', target: '198.51.100.4', detail: '', provisional: false },
        // The same host twice: the sample is one entry per emission, the
        // slot names hosts.
        { at: '2026-09-02T11:31:00Z', target: '203.0.113.9', detail: '', provisional: false },
      ],
      sampleTruncated: false,
      corpusTruncated: false,
      anyProvisional: false,
    },
  })),
  resetDefinition: vi.fn(async () => ({ id: 'port_scan' })),
  cloneDefinition: vi.fn(async () => ({ id: 'copy-1' })),
  fetchEntities: vi.fn(async () => [
    { type: 'host', key: '192.168.1.50', label: 'nas' },
    { type: 'port', key: '22', label: 'ssh' },
  ]),
  fetchRouterRules: vi.fn(async () => ({
    available: true,
    rules: [{ logPrefix: 'r13' }, { logPrefix: 'wan-in' }, { logPrefix: '' }],
  })),
  // The station mounts #640's expectations ledger under the bench, and
  // this factory replaces the whole api module -- so these two have to
  // exist here or the ledger's own fetch throws inside every test in
  // this file. The ledger's behaviour is covered next door in
  // ExpectationsLedger.svelte.test.ts; empty is all this suite needs.
  fetchExpectations: vi.fn(async () => []),
  forgetExpectation: vi.fn(async () => null),
}))

import EngineRoomWatchers from './EngineRoomWatchers.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { scopeSuggestionsState } from '../lib/scopeSuggestions.svelte'
import { appState } from '../lib/state.svelte'
import * as api from '../lib/api'

// A tick that lets the component's own $effect-driven fetches settle.
const settle = () => new Promise((r) => setTimeout(r, 0))

async function open(name = 'Port scan') {
  await fireEvent.click(screen.getByRole('button', { expanded: false, name: new RegExp(name) }))
  await settle()
}

beforeEach(async () => {
  vi.clearAllMocks()
  detectorSettingsState.schema = {}
  scopeSuggestionsState.hosts = []
  scopeSuggestionsState.rules = []
  appState.devices = [
    {
      id: 'rb5009',
      name: 'rb5009',
      sourceIp: '10.0.0.1',
      configured: true,
      firstSeen: '',
      lastSeen: '',
      eventCount: 1,
      status: 'live',
    },
  ] as never
  await detectorSettingsState.refresh()
})

describe('the bench', () => {
  it('lists every available detection definition with its worded scope', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    expect(screen.getByText('port_scan')).toBeTruthy()
    expect(screen.getByText('rule_spike')).toBeTruthy()
    expect(screen.getByText(/hosts deny-listed \(1\)/)).toBeTruthy()
  })

  it('says which rows are no longer on their shipped numbers', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    // rule_spike is the one with a distance from stock; port_scan is not.
    expect(screen.getAllByText('tuned')).toHaveLength(1)
  })
})

describe('expanding a row', () => {
  it('opens the panel in place under the row that was clicked', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    expect(screen.queryByText('When it fires')).toBeNull()
    await open()
    expect(screen.getByText('When it fires')).toBeTruthy()
  })

  it('keeps one panel open at a time', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    expect(screen.getByLabelText('add a host')).toBeTruthy()
    await open('Rule hit-rate spike')
    // The rules axis is rule_spike's, the hosts axis is port_scan's --
    // both showing at once would mean two panels open.
    expect(screen.getByLabelText('add a rule label')).toBeTruthy()
    expect(screen.queryByLabelText('add a host')).toBeNull()
  })

  it('closes again when the same row is clicked twice', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { expanded: true }))
    expect(screen.queryByText('When it fires')).toBeNull()
  })
})

describe('the tuning fields', () => {
  it('builds them from the schema endpoint, not from the copy on the definition', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    expect(api.fetchDefinitionSchema).toHaveBeenCalled()
    expect(screen.getByText(/^Threshold/)).toBeTruthy()
    expect(screen.queryByText(/^Stale/)).toBeNull()
  })

  it('carries the schema’s type, bounds, unit and description onto the control', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const threshold = screen.getByRole('spinbutton', { name: /Threshold/ })
    expect(threshold.getAttribute('min')).toBe('2')
    expect(threshold.getAttribute('max')).toBe('1000')
    expect(screen.getByText(/Distinct destination ports before a source is flagged/)).toBeTruthy()
  })

  it('edits a duration in seconds, converting the schema’s nanosecond bounds', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const window = screen.getByRole('spinbutton', { name: /Window/ })
    expect((window as HTMLInputElement).value).toBe('60')
    expect(window.getAttribute('min')).toBe('1')
    expect(window.getAttribute('max')).toBe('3600')
  })

  it('shows no tuning group for a definition the server declares no schema for', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open('Rule hit-rate spike')
    expect(screen.queryByText('When it fires')).toBeNull()
    expect(screen.getByText('What it watches')).toBeTruthy()
  })

  it('saves an edited threshold and window in one write, in the server’s own units', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.input(screen.getByRole('spinbutton', { name: /Threshold/ }), {
      target: { value: '9' },
    })
    await fireEvent.input(screen.getByRole('spinbutton', { name: /Window/ }), {
      target: { value: '90' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await settle()
    expect(api.updateDefinition).toHaveBeenCalledTimes(1)
    const [id, body] = vi.mocked(api.updateDefinition).mock.calls[0]
    expect(id).toBe('port_scan')
    expect(body.params).toEqual({ threshold: 9, window: '90s' })
  })
})

describe('scope chips', () => {
  it('shows each host and port as its own removable chip', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    expect(screen.getByRole('button', { name: 'remove host 192.168.1.20' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'remove port 22' })).toBeTruthy()
  })

  it('drops a chip and saves the axis without it', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { name: 'remove host 192.168.1.20' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await settle()
    const [, body] = vi.mocked(api.updateDefinition).mock.calls[0]
    expect(body.scope?.hosts).toEqual([])
    expect(body.scope?.hostsMode).toBe('deny')
  })

  it('adds a host from the add box on Enter', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const box = screen.getByLabelText('add a host')
    await fireEvent.input(box, { target: { value: '203.0.113.0/24' } })
    await fireEvent.keyDown(box, { key: 'Enter' })
    expect(screen.getByRole('button', { name: 'remove host 203.0.113.0/24' })).toBeTruthy()
    expect((box as HTMLInputElement).value).toBe('')
  })

  it('expands a typed port range into one chip per port, and saves them as numbers', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const box = screen.getByLabelText('add a port')
    await fireEvent.input(box, { target: { value: '8000-8002' } })
    await fireEvent.keyDown(box, { key: 'Enter' })
    expect(screen.getByRole('button', { name: 'remove port 8000' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'remove port 8002' })).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await settle()
    const [, body] = vi.mocked(api.updateDefinition).mock.calls[0]
    expect(body.scope?.ports).toEqual([22, 8000, 8001, 8002])
  })

  it('refuses a port that is not one, saying why rather than adding nothing', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const box = screen.getByLabelText('add a port')
    await fireEvent.input(box, { target: { value: 'ssh' } })
    await fireEvent.keyDown(box, { key: 'Enter' })
    expect(screen.getByText(/is not a port number/)).toBeTruthy()
  })

  it('keeps the allow/deny/no-restriction select beside the chips', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const select = screen.getByLabelText('Hosts restriction') as HTMLSelectElement
    expect(select.value).toBe('deny')
    expect(within(select).getByText('no restriction')).toBeTruthy()
  })

  it('suggests hosts from Entities and rule labels from the pushed rule tables', async () => {
    const { container } = render(EngineRoomWatchers, { canEdit: true })
    await settle()
    const hosts = container.querySelector('#watchers-hosts')
    const rules = container.querySelector('#watchers-rules')
    expect([...(hosts?.querySelectorAll('option') ?? [])].map((o) => o.value)).toEqual([
      '192.168.1.50',
    ])
    // The empty log prefix on the third pushed rule is not a label and is
    // not offered as one.
    expect([...(rules?.querySelectorAll('option') ?? [])].map((o) => o.value)).toEqual([
      'r13',
      'wan-in',
    ])
  })
})

describe('reset', () => {
  it('asks the server to put the definition back to stock', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset to stock' }))
    await settle()
    expect(api.resetDefinition).toHaveBeenCalledWith('port_scan')
  })

  it('never writes scope as part of a reset', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset to stock' }))
    await settle()
    expect(api.updateDefinition).not.toHaveBeenCalled()
  })

  it('leaves the panel open on the freshly stock values', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset to stock' }))
    await settle()
    expect(screen.getByText('When it fires')).toBeTruthy()
  })

  it('shows the server’s refusal rather than reporting a reset that did not happen', async () => {
    vi.mocked(api.resetDefinition).mockResolvedValueOnce('no such definition')
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.click(screen.getByRole('button', { name: 'Reset to stock' }))
    await settle()
    expect(screen.getByText('no such definition')).toBeTruthy()
  })
})

// #810. Clone is offered on the rows where it can succeed and nowhere
// else: a custom detector is stored structure the server copies, a
// shipped one is Go keyed by its own id and always refuses.
describe('clone', () => {
  // The copy as the server hands it back on the refresh that follows:
  // a second custom detector, paused, under the "(copy)" name.
  const COPY = {
    id: 'copy-1',
    name: 'SSH hammering (copy)',
    intent: 'detection',
    kind: 'declarative',
    enabled: false,
    scope: {},
    params: { threshold: 5, window: '1m0s' },
    provenance: { origin: 'custom' },
    detection: {
      conditions: [{ field: 'destinationPort', operator: 'equals', values: ['22'] }],
      key: 'perSource',
      counting: 'total',
      detailTemplate: '{Count} attempts against port 22 from {SourceAddress}',
    },
    available: true,
    replay: { known: true, capable: true },
  }

  // Returns the copy alongside everything already listed, which is what
  // the bench re-reads after a successful clone.
  async function withCopyOnRefresh() {
    const { definitions, coverageEvidence } = await api.fetchDefinitions()
    vi.mocked(api.fetchDefinitions).mockResolvedValueOnce({
      definitions: [...definitions, COPY],
      coverageEvidence,
    } as never)
  }

  it('is offered on a custom detector', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open('SSH hammering')
    expect(screen.getByRole('button', { name: 'Clone' })).toBeTruthy()
  })

  it('is not offered on a shipped one, whose logic no copy could carry', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    expect(screen.queryByRole('button', { name: 'Clone' })).toBeNull()
    // The rest of the foot is untouched: this hides one button, not the
    // panel it sits in.
    expect(screen.getByRole('button', { name: 'Reset to stock' })).toBeTruthy()
  })

  it('creates the copy with no prompt in between, under the "(copy)" name', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open('SSH hammering')
    await fireEvent.click(screen.getByRole('button', { name: 'Clone' }))
    await settle()
    expect(api.cloneDefinition).toHaveBeenCalledWith('ssh_hammering', 'SSH hammering (copy)')
  })

  it('leaves the pause to the server rather than a second request of its own', async () => {
    // The server stores the copy disabled, so nothing here has to. A
    // follow-up PUT could fail on its own and leave a running duplicate
    // of a detector the operator is halfway through editing.
    render(EngineRoomWatchers, { canEdit: true })
    await open('SSH hammering')
    await fireEvent.click(screen.getByRole('button', { name: 'Clone' }))
    await settle()
    expect(api.updateDefinition).not.toHaveBeenCalled()
  })

  it('opens the copy expanded, with its name selected to be typed over', async () => {
    await withCopyOnRefresh()
    render(EngineRoomWatchers, { canEdit: true })
    await open('SSH hammering')
    await fireEvent.click(screen.getByRole('button', { name: 'Clone' }))
    await settle()

    const copyRow = screen.getByRole('button', { expanded: true, name: /SSH hammering \(copy\)/ })
    expect(copyRow).toBeTruthy()
    const name = screen.getByLabelText('Name') as HTMLInputElement
    expect(name.value).toBe('SSH hammering (copy)')
    expect(document.activeElement).toBe(name)
  })

  it('shows the server’s refusal in its own words when a definition cannot be cloned', async () => {
    const refusal =
      'a shipped definition cannot be cloned: its logic is compiled into this binary and keyed by its own id, so a copy would evaluate nothing. Override its params instead (PUT /api/definitions/{id}).'
    vi.mocked(api.cloneDefinition).mockResolvedValueOnce(refusal)
    render(EngineRoomWatchers, { canEdit: true })
    await open('SSH hammering')
    await fireEvent.click(screen.getByRole('button', { name: 'Clone' }))
    await settle()
    expect(screen.getByText(refusal)).toBeTruthy()
  })
})

// #786. What is worth pinning about the slot, rather than about the
// wrapper next door (lib/api.test.ts, which pins the two shapes coming
// back as values):
//
//   - the receipt and the decline land in the *same* slot, and the
//     decline is not dressed as an error -- it is an honest limit of the
//     traffic held, so it carries the panel's quiet ink, not .error's.
//   - a truncation flag reads as "at least", and the two flags are not
//     interchangeable: a cut-short corpus makes the *count* a floor, a
//     bounded sample makes only the *list* partial.
//   - Try never blocks Save, before or after either answer.
describe('try', () => {
  async function press(name = 'Try') {
    await fireEvent.click(screen.getByRole('button', { name }))
    await settle()
  }

  it('replays the candidate numbers as typed, without saving them', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await fireEvent.input(screen.getByRole('spinbutton', { name: /Threshold/ }), {
      target: { value: '9' },
    })
    await press()
    // window and threshold only: the engine's replay candidate is that
    // closed set (replayParamSchema), and a third param name is refused
    // outright rather than ignored.
    expect(api.replayDefinition).toHaveBeenCalledWith('port_scan', {
      threshold: 9,
      window: '60s',
    })
    // A trial is not an edit. Nothing about the definition the engine is
    // evaluating may change because someone asked a question about it.
    expect(api.updateDefinition).not.toHaveBeenCalled()
  })

  it('says what would have fired, over the window it was counted across', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('Would have fired 3 times in the last 4h 12m')).toBeTruthy()
  })

  it('lists the hosts that would have been flagged, each once', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('203.0.113.9')).toBeTruthy()
    expect(screen.getByText('198.51.100.4')).toBeTruthy()
    // The third sample entry is the first host again; the slot names
    // hosts, not sample rows.
    expect(screen.getAllByText('203.0.113.9')).toHaveLength(1)
  })

  it('reads a cut-short corpus as "at least", because the count is then a floor', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      receipt: {
        window: { start: '', end: '', duration: '4h12m0s', eventCount: 1_000_000 },
        emissionCount: 3,
        sample: [],
        sampleTruncated: false,
        corpusTruncated: true,
        anyProvisional: false,
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('Would have fired at least 3 times in the last 4h 12m')).toBeTruthy()
  })

  it('says "at least these" of a bounded sample, and leaves the count exact', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      receipt: {
        window: { start: '', end: '', duration: '4h12m0s', eventCount: 8421 },
        emissionCount: 40,
        sample: [{ at: '', target: '203.0.113.9', detail: '', provisional: false }],
        sampleTruncated: true,
        corpusTruncated: false,
        anyProvisional: false,
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('at least these')).toBeTruthy()
    // The sample being bounded says nothing about the total, so the
    // count line stays exact.
    expect(screen.getByText('Would have fired 40 times in the last 4h 12m')).toBeTruthy()
  })

  it('shows a decline in the same slot, in grey rather than in the error ink', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      decline: {
        reason: 'corpus covers 4h12m0s (8421 event(s)), shorter than this definition’s window',
        corpusSpan: '4h12m0s',
        definitionWindow: '24h0m0s',
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    // "1d 0h", not "24h": formatDurationShort drops to days at 24 hours,
    // and the decline uses the same helper every other duration on this
    // panel does rather than a second way of saying a length.
    const said = screen.getByText("Can't replay: needs a 1d 0h window, only 4h 12m held")
    expect(said.classList.contains('error')).toBe(false)
    expect(said.classList.contains('declined')).toBe(true)
  })

  // The candidate's count against the one it would replace (#786). The
  // server measures it by replaying the live params over the same corpus
  // in the same request; the slot's job is only to say it beside the
  // count, quietly, and to say nothing at all when there is nothing to
  // compare against.
  it('says what the detector counts as it stands, beside the candidate’s count', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      receipt: {
        window: { start: '', end: '', duration: '4h12m0s', eventCount: 8421 },
        emissionCount: 3,
        sample: [],
        sampleTruncated: false,
        corpusTruncated: false,
        anyProvisional: false,
      },
      current: {
        receipt: {
          window: { start: '', end: '', duration: '4h12m0s', eventCount: 8421 },
          emissionCount: 41,
          sample: [],
          sampleTruncated: false,
          corpusTruncated: false,
          anyProvisional: false,
        },
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText(/Would have fired 3 times in the last 4h 12m/)).toBeTruthy()
    const current = screen.getByText('currently: 41')
    // In the slot's own quiet ink, not a status colour: it is the thing
    // the candidate is measured against, not a second answer.
    expect(current.classList.contains('tried-current')).toBe(true)
    expect(current.classList.contains('error')).toBe(false)
  })

  it('reads a cut-short corpus as "at least" for the current count too', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      receipt: {
        window: { start: '', end: '', duration: '4h12m0s', eventCount: 1_000_000 },
        emissionCount: 3,
        sample: [],
        sampleTruncated: false,
        corpusTruncated: true,
        anyProvisional: false,
      },
      current: {
        receipt: {
          window: { start: '', end: '', duration: '4h12m0s', eventCount: 1_000_000 },
          emissionCount: 41,
          sample: [],
          sampleTruncated: false,
          corpusTruncated: true,
          anyProvisional: false,
        },
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('currently: at least 41')).toBeTruthy()
  })

  it('says a declining live-params answer in the same grey, and keeps the receipt', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce({
      receipt: {
        window: { start: '', end: '', duration: '4h12m0s', eventCount: 8421 },
        emissionCount: 3,
        sample: [],
        sampleTruncated: false,
        corpusTruncated: false,
        anyProvisional: false,
      },
      current: {
        decline: {
          reason: 'corpus covers 4h12m0s (8421 event(s)), shorter than this definition’s window',
          corpusSpan: '4h12m0s',
          definitionWindow: '24h0m0s',
        },
      },
    })
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    // The candidate was answerable and the comparison was not: what is
    // missing is the second number, not the receipt.
    expect(screen.getByText(/Would have fired 3 times in the last 4h 12m/)).toBeTruthy()
    const current = screen.getByText(/currently: not replayable over the traffic held/)
    expect(current.classList.contains('tried-current')).toBe(true)
    expect(current.textContent).toContain('shorter than this definition’s window')
  })

  it('says nothing about "currently" when the answer carries no current', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    // The default answer here carries none -- as the server's does for a
    // candidate that changed nothing, where the count *is* the current
    // one and repeating it beside itself would say nothing.
    expect(screen.getByText('Would have fired 3 times in the last 4h 12m')).toBeTruthy()
    expect(screen.queryByText(/currently:/)).toBeNull()
  })

  it('never blocks Save -- before, during or after either answer', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    const save = screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement
    expect(save.disabled).toBe(false)
    await press()
    expect(save.disabled).toBe(false)
  })

  it('shows a refusal in the server’s own words, without a slot answer', async () => {
    vi.mocked(api.replayDefinition).mockResolvedValueOnce(
      'no event corpus is available to replay against',
    )
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText('no event corpus is available to replay against')).toBeTruthy()
    expect(screen.queryByText(/Would have fired/)).toBeNull()
  })

  it('clears the slot when the panel is reopened, so no receipt outlives its numbers', async () => {
    render(EngineRoomWatchers, { canEdit: true })
    await open()
    await press()
    expect(screen.getByText(/Would have fired/)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { expanded: true }))
    await open()
    expect(screen.queryByText(/Would have fired/)).toBeNull()
  })
})

describe('a viewer', () => {
  it('sees every row and the same facts', async () => {
    render(EngineRoomWatchers, { canEdit: false })
    expect(screen.getByText('port_scan')).toBeTruthy()
    expect(screen.getByText(/hosts deny-listed \(1\)/)).toBeTruthy()
    expect(screen.getByText('running')).toBeTruthy()
  })

  it('gets no controls at all -- hidden, never disabled', async () => {
    render(EngineRoomWatchers, { canEdit: false })
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
    expect(screen.queryAllByRole('button')).toHaveLength(0)
  })

  it('asks the server for neither the schema nor the suggestions it cannot read', async () => {
    render(EngineRoomWatchers, { canEdit: false })
    await settle()
    expect(api.fetchDefinitionSchema).not.toHaveBeenCalled()
    expect(api.fetchEntities).not.toHaveBeenCalled()
  })
})
