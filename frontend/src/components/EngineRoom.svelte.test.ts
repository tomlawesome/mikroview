// SPDX-License-Identifier: AGPL-3.0-only
//
// #490: the engine room absorbs Users.svelte, Tokens.svelte and
// Detectors.svelte wholesale -- see git history for their own retired
// test files. This covers the room's own new behaviour: the five
// stations, the zoom (opening one collapses the rest), and the
// viewer/admin split the design record asks for (chip once, verbs
// gated, facts identical).

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  fetchSetupStatus: vi.fn(async () => ({
    instance: { tlsEnabled: true, hosts: [], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: [],
  })),
  fetchDefinitions: vi.fn(async () => ({
    definitions: [
      {
        id: 'port_scan',
        name: 'Port scan',
        intent: 'detection',
        kind: 'declarative',
        enabled: true,
        scope: { hosts: ['203.0.113.9'], hostsMode: 'deny' },
        provenance: { origin: 'shipped' },
        available: true,
        replay: { known: true, capable: true },
      },
      {
        id: 'device_silence',
        name: 'Device gone quiet',
        intent: 'detection',
        kind: 'declarative',
        enabled: false,
        provenance: { origin: 'shipped' },
        available: true,
        replay: { known: true, capable: true },
      },
    ],
    coverageEvidence: { complete: true },
  })),
  updateDefinition: vi.fn(),
  fetchUsers: vi.fn(async () => [
    { id: 'u1', username: 'tom', role: 'admin', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T13:41:00Z', hasLocalPassword: true, sso: false },
    { id: 'u2', username: 'kai', role: 'user', createdAt: '2026-08-01T00:00:00Z', lastLogin: '2026-08-24T12:07:00Z', hasLocalPassword: true, sso: false },
  ]),
  createUser: vi.fn(),
  deleteUser: vi.fn(),
  fetchTokens: vi.fn(async () => [
    { id: 't1', name: 'rb5009-ingest', kind: 'ingest', device: 'rb5009', createdAt: '2026-08-01T00:00:00Z', lastUsedAt: '2026-08-24T14:02:00Z' },
  ]),
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  fetchDevices: vi.fn(async () => []),
}))

import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { flagsState } from '../lib/flags.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { usersState } from '../lib/users.svelte'
import { tokensState } from '../lib/tokens.svelte'
import type { Stats } from '../lib/types'
import EngineRoom from './EngineRoom.svelte'

function stats(overrides: Partial<Stats> = {}): Stats {
  return {
    total: 0,
    byAction: {},
    topRules: [],
    timeSeries: [],
    eventsPerSecond: 7.4,
    capacity: 100000,
    count: 41208,
    windowSeconds: 72 * 3600,
    connectedClients: 1,
    ...overrides,
  }
}

async function settle() {
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

beforeEach(() => {
  vi.clearAllMocks()
  appState.stats = stats()
  appState.devices = []
  flagsState.list = []
  detectorSettingsState.list = []
  usersState.list = []
  tokensState.list = []
  tokensState.justCreated = null
})

describe('The engine room (#490)', () => {
  it('renders its five stations', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    for (const name of ['The door', 'The store', 'The watchers', 'The flags desk', 'The heralds']) {
      expect(screen.getByText(name)).toBeTruthy()
    }
  })

  it('opening a station collapses the others', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    // At rest, the store's subtitle is visible.
    expect(screen.getByText('what is kept')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /The door/ }))
    flushSync()

    // Opening the door collapses the store to a slim title+number bar --
    // its subtitle (only shown outside the collapsed state) disappears.
    expect(screen.queryByText('what is kept')).toBeNull()
    expect(screen.getByRole('button', { name: /The door/ }).getAttribute('aria-expanded')).toBe('true')

    // Closing it again (same toggle) returns the room to rest.
    await fireEvent.click(screen.getByRole('button', { name: /The door/ }))
    flushSync()
    expect(screen.getByText('what is kept')).toBeTruthy()
  })

  it('a viewer sees the chip, no verbs, and no admin-only users door', async () => {
    authState.state = 'authenticated'
    authState.role = 'user'
    render(EngineRoom)
    await settle()

    expect(screen.getByText('READ-ONLY — ADMINS EDIT')).toBeTruthy()

    // Tokens door is viewer-readable but its verbs are gated.
    expect(screen.getByText('rb5009-ingest')).toBeTruthy()

    // An ingest key names the device it speaks for, not just its kind:
    // with two routers pushing, "ingest" alone does not say which key
    // belongs to which, and that is the fact an admin revokes on. The
    // old Tokens page carried it and the door has to as well.
    expect(screen.getByText(/ingest: rb5009/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Revoke' })).toBeNull()
    expect(screen.queryByRole('button', { name: '+ Mint a key' })).toBeNull()

    // The users door stayed admin-only (mid-build owner override) --
    // absent entirely for a viewer, not shown empty or explained.
    expect(screen.queryByText('Who may look in')).toBeNull()
    expect(screen.queryByRole('button', { name: '+ Let someone in' })).toBeNull()
  })

  it('an admin sees the verbs', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    render(EngineRoom)
    await settle()

    expect(screen.queryByText('READ-ONLY — ADMINS EDIT')).toBeNull()
    expect(screen.getByText('Who may look in')).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ Let someone in' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '+ Mint a key' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Revoke' })).toBeTruthy()
  })

  it('the mint banner appears once', async () => {
    authState.state = 'authenticated'
    authState.role = 'admin'
    tokensState.justCreated = {
      id: 't2',
      name: 'nas-read',
      kind: 'api',
      createdAt: '2026-08-24T14:05:00Z',
      value: 'mv1_4c21secret9b0d',
    }
    render(EngineRoom)
    await settle()

    expect(screen.getAllByText('mv1_4c21secret9b0d')).toHaveLength(1)
    expect(screen.getAllByText(/Copy it now/)).toHaveLength(1)
  })
})
