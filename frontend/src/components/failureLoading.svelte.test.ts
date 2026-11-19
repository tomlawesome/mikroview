// SPDX-License-Identifier: AGPL-3.0-only
//
// #1540 (C4, C5): site-level behaviour. C4: a failed load announces itself
// as role="alert" with the "Could not <verb> <thing>: <server words>" lead-in
// on screens that used a bare class or the raw message. C5: a list shows
// GhostRows (a role="status" label plus placeholder rows) while its first
// load is in flight, not a blank table.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'

const fetchEntities = vi.fn(async (): Promise<unknown[]> => [])
const fetchAuditLog = vi.fn(async (): Promise<unknown> => ({ entries: [], hasMore: false }))
const lookupIp = vi.fn(async (_ip: string): Promise<unknown> => ({ ip: '192.0.2.2' }))
const fetchWatchlistEntries = vi.fn(async (): Promise<unknown> => ({ entries: [], coverage: {} }))

vi.mock('../lib/api', () => ({
  fetchEntities: () => fetchEntities(),
  upsertEntity: vi.fn(async () => null),
  deleteEntity: vi.fn(),
  deleteDevice: vi.fn(async () => null),
  fetchDevices: vi.fn(async () => []),
  fetchStats: vi.fn(async () => ({ total: 0, byAction: {}, topRules: [], timeline: [] })),
  fetchDeviceMACs: vi.fn(async () => []),
  fetchRouterRules: vi.fn(async () => ({ available: false, rules: [] })),
  fetchRouterAddresses: vi.fn(async () => ({ available: false, rules: [] })),
  fetchRules: vi.fn(async () => []),
  fetchUnattributedSources: vi.fn(async () => []),
  fetchRefusedSenders: vi.fn(async () => []),
  fetchSetupStatus: vi.fn(async () => ({ instance: { tlsEnabled: true, syslogPort: ':16893', hosts: [] } })),
  fetchSetupCommands: vi.fn(async () => ({})),
  fetchAuditLog: () => fetchAuditLog(),
  lookupIp: (ip: string) => lookupIp(ip),
  geoLookup: vi.fn(async () => ({ country: 'US', asn: null, asName: null })),
  fetchWatchlistEntries: () => fetchWatchlistEntries(),
  createWatchlistEntry: vi.fn(),
  updateWatchlistEntry: vi.fn(),
  deleteWatchlistEntry: vi.fn(),
  fetchWatchlistMatches: vi.fn(),
  fetchRecentMatches: vi.fn(async () => []),
  promoteWatchlistDestinations: vi.fn(),
  setWatchlistObserving: vi.fn(),
  setWatchlistEnabled: vi.fn(),
  fetchSuggestions: vi.fn(async () => []),
  acceptSuggestion: vi.fn(),
  hideSuggestion: vi.fn(),
  unhideSuggestion: vi.fn(),
  resetSuggestions: vi.fn(),
  fetchDecommission: vi.fn(async () => ({ offers: [], watches: [], evidenceComplete: true })),
  deleteDecommissionWatch: vi.fn(),
}))

import { appState } from '../lib/state.svelte'
import { entitiesState } from '../lib/entities.svelte'
import { auditState } from '../lib/audit.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { ipLookupState } from '../lib/ipLookup.svelte'
import AuditLog from './AuditLog.svelte'
import Entities from './Entities.svelte'
import Watchlist from './Watchlist.svelte'
import IpLookupPopover from './IpLookupPopover.svelte'

const never = () => new Promise<never>(() => {})

async function settle() {
  for (let i = 0; i < 5; i++) await Promise.resolve()
  flushSync()
}

beforeEach(() => {
  vi.clearAllMocks()
  appState.initialLoadDone = true
  appState.events = []
  appState.devices = []
  entitiesState.list = []
  entitiesState.loaded = false
  entitiesState.loadError = null
  auditState.reset()
  watchlistState.reset?.()
})

afterEach(() => {
  ipLookupState.close()
  cleanup()
})

describe('failure lines announce themselves (#1540 C4)', () => {
  it('AuditLog: a failed load is a role="alert" "Could not load the audit log: <words>"', async () => {
    fetchAuditLog.mockRejectedValue(new Error('network unreachable'))
    render(AuditLog)
    await settle()
    expect(screen.getByRole('alert').textContent?.trim()).toBe('Could not load the audit log: network unreachable')
  })

  it('IpLookupPopover: a failed lookup is a role="alert" "Could not look up …: <server words>"', async () => {
    lookupIp.mockRejectedValue(new Error('rate limited'))
    render(IpLookupPopover)
    ipLookupState.open('192.0.2.2', new DOMRect(10, 10, 20, 20))
    await settle()
    const line = screen.getByRole('alert')
    expect(line.textContent).toMatch(/^Could not look up .+: rate limited$/)
  })
})

describe('first load shows ghost rows, never a blank table (#1540 C5)', () => {
  it('AuditLog: while the first fetch is in flight, a Loading status and placeholder rows show', async () => {
    fetchAuditLog.mockImplementation(never)
    const { container } = render(AuditLog)
    await settle()
    expect(container.querySelector('.ghost-rows')).not.toBeNull()
    expect(screen.getByRole('status').textContent).toMatch(/Loading|Reading/)
  })

  it('Entities: while the first fetch is in flight, ghost rows show', async () => {
    fetchEntities.mockImplementation(never)
    const { container } = render(Entities)
    await settle()
    expect(container.querySelector('.ghost-rows')).not.toBeNull()
  })

  it('Watchlist: before the first load lands, ghost rows show', async () => {
    fetchWatchlistEntries.mockImplementation(never)
    const { container } = render(Watchlist)
    await settle()
    expect(container.querySelector('.ghost-rows')).not.toBeNull()
  })
})

describe('a failed first load says so, never a blank or endless ghost (#1540 review)', () => {
  it('Watchlist: a rejected first fetch clears the ghost rows and shows the failure line', async () => {
    fetchWatchlistEntries.mockRejectedValue(new Error('boom'))
    const { container } = render(Watchlist)
    await settle()
    expect(container.querySelector('.ghost-rows')).toBeNull()
    expect(screen.getByRole('alert').textContent).toContain('Could not load the watches: boom')
  })

  it('Entities: a rejected fetch shows the failure line, not an empty table', async () => {
    fetchEntities.mockRejectedValue(new Error('boom'))
    const { container } = render(Entities)
    await settle()
    expect(container.querySelector('.ghost-rows')).toBeNull()
    expect(screen.getByRole('alert').textContent).toContain('Could not load the names: boom')
    expect(container.querySelector('table.etable')).toBeNull()
  })
})
