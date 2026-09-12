// SPDX-License-Identifier: AGPL-3.0-only
//
// #410: the dossier card's composition is the thing worth testing here
// -- that the seven sections the design fixes all land in the DOM from
// one response, that the three lines the card exists to be honest with
// (the locally-administered note, the absent list, the note-only
// identity) appear when the backend sets them, and that Esc closes.
// The assembly itself is Go's business (internal/dossier); this file
// only proves the card renders what it is given and claims nothing more.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  fetchHostDossier: vi.fn(async () => FIXTURE),
  fetchNameProvenance: vi.fn(async () => ({ editable: true, source: 'none', name: '', label: '' })),
  upsertEntity: vi.fn(async () => ''),
  deleteEntity: vi.fn(async () => ''),
}))

import { fetchHostDossier } from '../lib/api'
import { dossierState } from '../lib/dossier.svelte'
import { authState } from '../lib/auth.svelte'
import type { HostDossier as HostDossierResponse } from '../lib/types'
import HostDossier from './HostDossier.svelte'

// A host with something in every block: the card's full-house case.
let FIXTURE: HostDossierResponse

function baseFixture(): HostDossierResponse {
  return {
    ip: '10.20.0.31',
    generatedAt: '2026-09-09T12:00:00Z',
    seen: {
      known: true,
      firstSeen: '2026-09-01T09:00:00Z',
      firstSeenSource: 'the host presence register, which survives restarts',
      lastSeen: '2026-09-09T11:58:00Z',
      events: 4210,
      windowStart: '2026-09-08T12:00:00Z',
      interfaces: ['lan-iot'],
    },
    names: {
      known: true,
      name: 'kitchen-sensor',
      source: 'router-dhcp-lease',
      sourceNote: 'from a DHCP lease on gw-main',
    },
    mac: {
      known: true,
      address: 'b8:27:eb:4f:1a:22',
      source: "the host's own events",
      locallyAdministered: false,
      vendor: { known: true, name: 'Raspberry Pi Foundation', oui: 'B8:27:EB' },
      registry: { source: 'IEEE MA-L registry', loaded: true, entries: 34000 },
    },
    address: {
      assignment: 'lease',
      note: 'holds a DHCP lease on gw-main',
      device: 'gw-main',
      hostname: 'kitchen-sensor',
      leaseMac: 'b8:27:eb:4f:1a:22',
      consulted: ['gw-main'],
    },
    traffic: {
      known: true,
      destinations: [
        { ip: '52.10.4.9', name: 'broker.example', country: 'US', scope: 'internet', events: 900, ports: [8883], lastSeen: '2026-09-09T11:58:00Z' },
      ],
      talkers: [{ ip: '10.20.0.1', scope: 'local', events: 40, lastSeen: '2026-09-09T11:40:00Z' }],
      ports: [
        { port: 8883, protocol: 'tcp', name: 'mqtt-tls', direction: 'out', events: 900, peers: 1, lastSeen: '2026-09-09T11:58:00Z' },
      ],
      cadence: { known: true, shape: 'regular', medianGapSeconds: 60 },
    },
    firewall: {
      known: true,
      rules: [
        { label: 'iot-out', name: 'IoT egress', chain: 'forward', action: 'accept', device: 'gw-main', comment: 'let sensors reach the broker', commentKnown: true, events: 900, lastSeen: '2026-09-09T11:58:00Z' },
      ],
    },
    identity: {
      suggested: true,
      profile: 'iot',
      label: 'an IoT device',
      confidence: 'fair',
      because: 'it speaks MQTT and checks the time and talks to one cloud host',
      evidence: [
        { signal: 'MQTT', detail: 'port 8883 to broker.example, 900 events' },
        { signal: 'NTP', detail: 'port 123 out, hourly' },
      ],
      alternatives: ['a smart-home hub'],
      note: '',
    },
    suggestedProbe: {
      command: 'nmap -Pn -sV 10.20.0.31',
      note: 'run it yourself if you want the ports confirmed',
    },
    absent: ['ARP, DHCP leases — no push has covered this host'],
  }
}

async function open(ip = '10.20.0.31') {
  render(HostDossier)
  dossierState.open(ip)
  flushSync()
  await vi.waitFor(() => expect(screen.getByText('Reads as')).toBeTruthy())
}

beforeEach(() => {
  FIXTURE = baseFixture()
  authState.state = 'authenticated'
  authState.role = 'user'
  vi.mocked(fetchHostDossier).mockClear()
  vi.mocked(fetchHostDossier).mockImplementation(async () => FIXTURE)
})

afterEach(() => {
  dossierState.close()
  cleanup()
})

describe('HostDossier', () => {
  it('renders the seven sections, in the order the design fixes', async () => {
    await open()

    // 1 head, 3 field marks, 5 probe -- the headings that carry them.
    const headings = Array.from(document.querySelectorAll('h2, h3')).map((h) => h.textContent?.trim())
    expect(headings).toEqual([
      '10.20.0.31 kitchen-sensor',
      'Reads as',
      'Seen',
      'Names',
      'MAC',
      'Address',
      'Traffic',
      'Firewall',
      'Suggested probe',
    ])

    // 2. Reads as: the label and the confidence as a word, never a
    // number, with the evidence under it and the alternatives after.
    expect(screen.getByText(/reads as an IoT device/)).toBeTruthy()
    expect(screen.getByText(/· fair/)).toBeTruthy()
    expect(screen.getByText(/because it speaks MQTT/)).toBeTruthy()
    expect(screen.getByText(/port 8883 to broker.example/)).toBeTruthy()
    expect(screen.getByText(/could also be a smart-home hub/)).toBeTruthy()

    // 3. Field marks carry their own facts.
    expect(screen.getByText(/4210 events/)).toBeTruthy()
    expect(screen.getByText('Raspberry Pi Foundation · B8:27:EB')).toBeTruthy()
    expect(screen.getByText('holds a DHCP lease on gw-main')).toBeTruthy()
    expect(screen.getByText('broker.example')).toBeTruthy()
    expect(screen.getByText('IoT egress')).toBeTruthy()

    // 4, 5, 6, 7.
    expect(screen.getByTestId('dossier-absent').textContent).toContain('Not available:')
    expect(screen.getByText('nmap -Pn -sV 10.20.0.31')).toBeTruthy()
    expect(screen.getByText('MikroView never runs this; you would.')).toBeTruthy()
    expect(
      screen.getByText(/Nothing here was probed or looked up outside the router's own pushes/),
    ).toBeTruthy()
    expect(screen.getByText('Name this device')).toBeTruthy()
  })

  it('shows the locally-administered line, on its own, when the bit is set', async () => {
    FIXTURE.mac.locallyAdministered = true
    FIXTURE.mac.locallyAdministeredNote = 'no vendor exists: this is a VM, container or randomised MAC'
    FIXTURE.mac.vendor = { known: false, reason: 'the locally-administered bit is set' }
    await open()

    expect(screen.getByTestId('dossier-laa').textContent?.trim()).toBe(
      'no vendor exists: this is a VM, container or randomised MAC',
    )
    // And no vendor is claimed alongside it.
    expect(screen.queryByText('Raspberry Pi Foundation · B8:27:EB')).toBeNull()
  })

  it('shows the absent line verbatim, as the card’s honesty surface', async () => {
    FIXTURE.absent = ['ARP', 'DHCP leases — no push has covered this host']
    await open()

    expect(screen.getByTestId('dossier-absent').textContent).toBe(
      'Not available: ARP; DHCP leases — no push has covered this host',
    )
  })

  it('makes the note the whole section when nothing is suggested', async () => {
    FIXTURE.identity = {
      suggested: false,
      note: 'nothing this host does matches a profile closely enough to name one',
      evidence: [{ signal: 'NTP', detail: 'port 123 out, hourly' }],
    }
    await open()

    expect(
      screen.getByText('nothing this host does matches a profile closely enough to name one'),
    ).toBeTruthy()
    expect(screen.queryByText(/reads as/)).toBeNull()
    // The working still shows: a card that names nothing still says
    // what it saw.
    expect(screen.getByText(/port 123 out, hourly/)).toBeTruthy()
  })

  it('shows the ghost rows while it loads, and a try again on failure', async () => {
    let reject: (e: Error) => void = () => {}
    vi.mocked(fetchHostDossier).mockImplementation(
      () => new Promise((_, rej) => (reject = rej)) as Promise<HostDossierResponse>,
    )

    render(HostDossier)
    dossierState.open('10.20.0.31')
    flushSync()
    expect(screen.getByText('Assembling the dossier…')).toBeTruthy()

    reject(new Error('boom'))
    await vi.waitFor(() => expect(screen.getByRole('alert')).toBeTruthy())
    expect(screen.getByText('Could not assemble the dossier')).toBeTruthy()

    vi.mocked(fetchHostDossier).mockImplementation(async () => FIXTURE)
    await fireEvent.click(screen.getByText('Try again'))
    await vi.waitFor(() => expect(screen.getByText('Reads as')).toBeTruthy())
  })

  it('omits the probe section entirely when the backend gives none', async () => {
    FIXTURE.suggestedProbe = undefined
    await open()
    expect(screen.queryByTestId('dossier-probe')).toBeNull()
  })

  it('is one dialog spoken as a region with headings, and Esc closes it', async () => {
    await open()

    const sheet = screen.getByTestId('host-dossier')
    expect(sheet.getAttribute('role')).toBe('dialog')
    expect(sheet.getAttribute('aria-modal')).toBe('true')
    expect(sheet.getAttribute('aria-labelledby')).toBe('host-dossier-title')
    expect(document.querySelectorAll('section[aria-labelledby]').length).toBeGreaterThan(5)

    await fireEvent.keyDown(window, { key: 'Escape' })
    flushSync()
    expect(screen.queryByTestId('host-dossier')).toBeNull()
  })

  it('offers no naming action to a viewer', async () => {
    authState.role = 'viewer'
    await open()
    expect(screen.queryByText('Name this device')).toBeNull()
  })
})
