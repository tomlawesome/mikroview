// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PortsResponse, TraceResponse } from './api'

vi.mock('./api', () => ({
  fetchPorts: vi.fn(),
  fetchTrace: vi.fn(),
}))

import { fetchPorts, fetchTrace } from './api'
import { portFilterState } from './portFilter.svelte'
import { mapTraceState } from './mapTrace.svelte'

function answer(overrides: Partial<PortsResponse> = {}): PortsResponse {
  return {
    generatedAt: 1,
    windowSeconds: 3600,
    candidates: [
      { port: 445, proto: 'tcp', count: 16, named: true },
      { port: 22, proto: 'tcp', count: 1, named: false },
      { port: 3389, proto: 'tcp', count: 0, named: true },
    ],
    events: 16,
    accepts: 2,
    drops: 14,
    lines: 2,
    ribs: [{ in: 'bridge1', out: 'ether3', events: 2, accepts: 2, drops: 0 }],
    hosts: [{ ip: '10.0.10.21', name: 'tom-desktop', events: 2, accepts: 2, drops: 0 }],
    doors: [
      {
        device: 'core',
        label: '#12',
        ordinal: 12,
        action: 'accept',
        chain: 'forward',
        in: 'bridge1',
        out: 'ether3',
        dstPort: '445',
        who: 'bridge1 → ether3 accept',
      },
    ],
    ...overrides,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  portFilterState.clear()
  portFilterState.proto = 'tcp'
  mapTraceState.clear()
})

describe('the port pill (#1018)', () => {
  it('is inactive with nothing selected, and asks only for the picker list when opened', async () => {
    vi.mocked(fetchPorts).mockResolvedValue(answer({ selection: undefined }))

    expect(portFilterState.active).toBe(false)
    await portFilterState.openPicker()

    expect(portFilterState.open).toBe(true)
    expect(fetchPorts).toHaveBeenCalledWith([], 'tcp')
    expect(portFilterState.candidates.map((c) => c.port)).toEqual([445, 22, 3389])
    // The map is not filtered while nothing is selected: the picker's
    // own list is not an answer about a port.
    expect(portFilterState.active).toBe(false)
  })

  it('filters as the selection changes, with no Show button in between', async () => {
    vi.mocked(fetchPorts).mockResolvedValue(answer())

    await portFilterState.togglePort(445)

    expect(fetchPorts).toHaveBeenCalledWith([445], 'tcp')
    expect(portFilterState.active).toBe(true)
    expect(portFilterState.label).toBe('445/tcp')
    expect(portFilterState.summary).toBe('2 lines seen · 1 door')
    expect([...portFilterState.hostIps]).toEqual(['10.0.10.21'])
  })

  it('takes several ports at once and drops one on a second click', async () => {
    vi.mocked(fetchPorts).mockResolvedValue(answer())

    await portFilterState.togglePort(445)
    await portFilterState.togglePort(22)
    expect(portFilterState.ports).toEqual([22, 445])
    expect(portFilterState.label).toBe('22,445/tcp')

    await portFilterState.togglePort(445)
    expect(portFilterState.ports).toEqual([22])
  })

  // A slower earlier request must not land on top of a later answer:
  // the picker fires one per click, and the map would otherwise settle
  // on whichever finished last rather than on what is selected.
  it('ignores an answer that arrives after the selection has moved on', async () => {
    let releaseFirst: (v: PortsResponse) => void = () => {}
    vi.mocked(fetchPorts)
      .mockImplementationOnce(() => new Promise<PortsResponse>((r) => (releaseFirst = r)))
      .mockResolvedValueOnce(answer({ lines: 9 }))

    const slow = portFilterState.togglePort(445)
    await portFilterState.togglePort(22)
    releaseFirst(answer({ lines: 1 }))
    await slow

    expect(portFilterState.ports).toEqual([22, 445])
    // The stale answer is discarded rather than drawn under the new
    // label: `settled` is what the map reads, and it is false until the
    // answer in hand is about what is selected.
    expect(portFilterState.answer.lines).toBe(9)
  })

  it('draws nothing filtered while the answer in hand is about another selection', async () => {
    let release: (v: PortsResponse) => void = () => {}
    vi.mocked(fetchPorts).mockImplementation(() => new Promise<PortsResponse>((r) => (release = r)))

    const pending = portFilterState.togglePort(445)
    expect(portFilterState.settled).toBe(false)
    expect(portFilterState.ribs).toEqual([])
    expect(portFilterState.doors).toEqual([])

    release(answer())
    await pending
    expect(portFilterState.settled).toBe(true)
    expect(portFilterState.ribs.length).toBe(1)
  })

  it('reports nothing seen only once the answer says so', async () => {
    vi.mocked(fetchPorts).mockResolvedValue(answer({ events: 0, lines: 0, ribs: [], hosts: [] }))

    await portFilterState.togglePort(3389)
    expect(portFilterState.nothingSeen).toBe(true)
    // The door is still there: the map says "one rule names it" and
    // draws it, rather than looking broken.
    expect(portFilterState.doors.length).toBe(1)
  })

  // An unread answer is not an answer of zero. Settling on a failure
  // would put "no logged traffic on 445/tcp in the window" and "0 of 12
  // hosts" on the map -- a positive claim about the network made out of
  // a dropped connection.
  it('does not settle on a failed read, so the map never states a zero it never read', async () => {
    vi.mocked(fetchPorts).mockResolvedValueOnce(answer())
    await portFilterState.togglePort(445)
    expect(portFilterState.ribs.length).toBe(1)
    expect(portFilterState.settled).toBe(true)

    vi.mocked(fetchPorts).mockRejectedValueOnce(new Error('nope'))
    await portFilterState.setProto('udp')
    expect(portFilterState.settled).toBe(false)
    expect(portFilterState.nothingSeen).toBe(false)
    expect(portFilterState.ribs).toEqual([])
    expect(portFilterState.doors).toEqual([])
  })

  it('clears everything on the ✕', async () => {
    vi.mocked(fetchPorts).mockResolvedValue(answer())
    await portFilterState.openPicker()
    await portFilterState.togglePort(445)

    portFilterState.clear()
    expect(portFilterState.active).toBe(false)
    expect(portFilterState.open).toBe(false)
    expect(portFilterState.ports).toEqual([])
    expect(portFilterState.doors).toEqual([])
  })
})

function trace(overrides: Partial<TraceResponse> = {}): TraceResponse {
  return {
    found: true,
    verdict: 'refused',
    event: {
      id: 7,
      time: '2026-09-08T22:04:00Z',
      deviceId: 'core',
      sourceIp: '192.168.1.1',
      action: 'drop',
      ruleLabel: 'default drop',
      chain: 'forward',
      inInterface: 'ether4',
      srcIp: '10.0.30.14',
      dstIp: '10.0.10.21',
      dstPort: 445,
      protocol: 'tcp',
      raw: 'x',
    },
    like: 13,
    srcSeen: 14,
    dstReached: 0,
    ...overrides,
  }
}

describe('the event trace (#1018)', () => {
  it('holds the one hop it was told about', async () => {
    vi.mocked(fetchTrace).mockResolvedValue(trace())

    await mapTraceState.open({ event: 7 })

    expect(mapTraceState.active).toBe(true)
    expect(mapTraceState.verdict).toBe('refused')
    expect(mapTraceState.inIface).toBe('ether4')
    // A refusal never reached an out-interface, and the map draws that
    // rather than inferring where it would have gone.
    expect(mapTraceState.outIface).toBe('')
    expect(mapTraceState.like).toBe(13)
    expect(mapTraceState.dstReached).toBe(0)
  })

  it('is active on an honest miss too, so the map can say so in words', async () => {
    vi.mocked(fetchTrace).mockResolvedValue({ found: false, like: 0, srcSeen: 0, dstReached: 0 })

    await mapTraceState.open({ in: 'ether4', port: 445 })
    expect(mapTraceState.active).toBe(true)
    expect(mapTraceState.event).toBeNull()
    expect(mapTraceState.verdict).toBeNull()
  })

  it('treats a failed read as a miss rather than leaving the last line up', async () => {
    vi.mocked(fetchTrace).mockResolvedValueOnce(trace())
    await mapTraceState.open({ event: 7 })
    expect(mapTraceState.event).not.toBeNull()

    vi.mocked(fetchTrace).mockRejectedValueOnce(new Error('nope'))
    await mapTraceState.open({ event: 8 })
    expect(mapTraceState.event).toBeNull()
    expect(mapTraceState.result?.found).toBe(false)
  })

  it('clears on Esc', async () => {
    vi.mocked(fetchTrace).mockResolvedValue(trace())
    await mapTraceState.open({ event: 7 })

    mapTraceState.clear()
    expect(mapTraceState.active).toBe(false)
    expect(mapTraceState.event).toBeNull()
  })
})
