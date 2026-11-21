// SPDX-License-Identifier: AGPL-3.0-only
//
// #1540 (C4), remaining sites: the stores that kept the raw error message
// give the shared "Could not <verb> <thing>: <server words>" lead-in, so a
// screen can show the store's text as it is. The server's words stay
// verbatim after the colon.
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchAuditLog: vi.fn(),
  fetchRecentMatches: vi.fn(),
  fetchDecommission: vi.fn(),
  fetchSetupStatus: vi.fn(),
  fetchSetupCommands: vi.fn(),
  saveSetupAddress: vi.fn(),
  saveSetupBackupTransport: vi.fn(),
  mintEnrolment: vi.fn(),
  rebindEnrolment: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  fetchRefusedSenders: vi.fn(),
}))

import {
  fetchAuditLog,
  fetchDecommission,
  fetchRecentMatches,
  fetchSetupStatus,
  saveSetupAddress,
  saveSetupBackupTransport,
} from './api'
import { auditState } from './audit.svelte'
import { matchesState } from './matches.svelte'
import { decommissionsState } from './decommission.svelte'
import { wizardState } from './wizard.svelte'

const LEAD = /^Could not [a-z]+ .+: boom$/

beforeEach(() => {
  vi.clearAllMocks()
  auditState.reset()
  matchesState.reset()
})

describe('stores give the failure lead-in (#1540 C4)', () => {
  it('audit: a failed refresh reads "Could not load the audit log: boom"', async () => {
    vi.mocked(fetchAuditLog).mockRejectedValue(new Error('boom'))
    await auditState.refresh()
    expect(auditState.error).toMatch(LEAD)
  })

  it('matches: a failed load and a failed older page both carry the lead-in', async () => {
    vi.mocked(fetchRecentMatches).mockRejectedValue(new Error('boom'))
    await matchesState.load()
    expect(matchesState.error).toMatch(LEAD)

    matchesState.reset()
    vi.mocked(fetchRecentMatches).mockResolvedValueOnce(
      Array.from({ length: 100 }, (_, i) => ({
        id: `m${i}`,
        entryId: 'e1',
        firstSeen: '2026-11-01T00:00:00Z',
        lastSeen: '2026-11-01T00:00:00Z',
        count: 1,
      })) as never,
    )
    await matchesState.load()
    vi.mocked(fetchRecentMatches).mockRejectedValue(new Error('boom'))
    await matchesState.loadOlder()
    expect(matchesState.error).toMatch(LEAD)
  })

  it('decommission: a failed refresh reads "Could not <verb> …: boom"', async () => {
    vi.mocked(fetchDecommission).mockRejectedValue(new Error('boom'))
    await decommissionsState.refresh()
    expect(decommissionsState.error).toMatch(LEAD)
  })

  it('wizard: a failed status read, address save and transport switch carry the lead-in', async () => {
    vi.mocked(fetchSetupStatus).mockRejectedValue(new Error('boom'))
    await wizardState.refresh()
    expect(wizardState.error).toMatch(LEAD)

    // A refusal arrives as the returned string, a dropped connection as a throw.
    vi.mocked(saveSetupAddress).mockResolvedValue('boom')
    wizardState.address = '192.0.2.10'
    await wizardState.saveAddress()
    expect(wizardState.addressSaveError).toMatch(LEAD)

    vi.mocked(saveSetupBackupTransport).mockRejectedValue(new Error('boom'))
    wizardState.backupTransport = 'sftp'
    await wizardState.setBackupTransport('https')
    expect(wizardState.backupTransportError).toMatch(LEAD)
  })
})
