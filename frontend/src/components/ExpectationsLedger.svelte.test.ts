// SPDX-License-Identifier: AGPL-3.0-only
//
// #640 part C: the expectations ledger under the bench on the watchers
// station.
//
// What is worth pinning here:
//
//   - the empty state says what would fill it, rather than just being
//     blank. A thin ledger is the normal early state of a deployment,
//     so it is the state most operators see first.
//   - a size-less expectation reads "any size", never "up to 0". Those
//     are opposite meanings and the difference is one absent field.
//   - Forget removes the row *and* re-reads the server, so absorbed
//     counts are the server's rather than the client's guess.
//   - a refused Forget shows the server's own words beside the row it
//     came from, and leaves the row in place.
//   - a viewer gets the facts and no button -- hidden, never disabled,
//     the grammar the bench above already set.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return { ...actual, fetchExpectations: vi.fn(), forgetExpectation: vi.fn() }
})

import ExpectationsLedger from './ExpectationsLedger.svelte'
import { fetchExpectations, forgetExpectation } from '../lib/api'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { formatDayMonth } from '../lib/format'
import type { Exclusion } from '../lib/types'

const settle = () => new Promise((r) => setTimeout(r, 0))

const PORT_SCAN: Exclusion = {
  id: 'port_scan|192.168.1.50',
  type: 'port_scan',
  target: '192.168.1.50',
  size: 30,
  absorbed: 7,
  since: '2026-09-02T08:15:00Z',
}

const SIZELESS: Exclusion = {
  id: 'global_spike|all',
  type: 'global_spike',
  target: 'all',
  absorbed: 0,
  since: '2026-08-20T08:15:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
  // The bench's own list, which the ledger reads for display names
  // rather than fetching definitions a second time. Only the two fields
  // it uses are relevant here.
  detectorSettingsState.list = [
    { name: 'port_scan', label: 'Port scan', enabled: true, scope: {} },
  ] as never
})

describe('an empty ledger', () => {
  it('says what would fill it rather than showing nothing', async () => {
    vi.mocked(fetchExpectations).mockResolvedValue([])
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    expect(screen.getByRole('heading', { name: 'What it has been told to expect' })).toBeTruthy()
    expect(
      screen.getByText('Nothing yet — every Expected verdict on the Flags card records one here.'),
    ).toBeTruthy()
  })

  it('states a failure to read the list instead of rendering it as empty', async () => {
    vi.mocked(fetchExpectations).mockRejectedValue(new Error('fetchExpectations: 503'))
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    expect(screen.getByText('fetchExpectations: 503')).toBeTruthy()
    expect(screen.queryByText(/Nothing yet/)).toBeNull()
  })
})

describe('a populated ledger', () => {
  beforeEach(() => {
    vi.mocked(fetchExpectations).mockResolvedValue([PORT_SCAN, SIZELESS])
  })

  it('gives each expectation its detector name, host, size, absorbed count and age', async () => {
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    expect(screen.getByText('Port scan')).toBeTruthy()
    expect(screen.getByText('192.168.1.50')).toBeTruthy()
    expect(screen.getByText('up to 30')).toBeTruthy()
    expect(screen.getByText('absorbed 7')).toBeTruthy()
    // Against formatDayMonth rather than a hardcoded "2 Sep": the
    // string is locale-dependent, and pinning one locale's spelling
    // would fail on a runner configured for another without anything
    // being wrong. What is pinned is that the row carries the day and
    // month and not the clock time the server also sent.
    expect(screen.getByText(`since ${formatDayMonth(PORT_SCAN.since!)}`)).toBeTruthy()
    expect(screen.queryByText(/08:15/)).toBeNull()
  })

  it('falls back to the definition id when the bench has no display name for it', async () => {
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    // global_spike is deliberately absent from detectorSettingsState.list
    // above: the id is still a true answer, and blanking the column is
    // not.
    expect(screen.getByText('global_spike')).toBeTruthy()
  })

  it('reads a size-less expectation as "any size", never "up to 0"', async () => {
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    expect(screen.getByText('any size')).toBeTruthy()
    expect(screen.queryByText('up to 0')).toBeNull()
  })

  it('offers a viewer every fact and no Forget button', async () => {
    render(ExpectationsLedger, { canEdit: false })
    await settle()

    expect(screen.getByText('up to 30')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /^Forget/ })).toBeNull()
  })
})

describe('forgetting an expectation', () => {
  beforeEach(() => {
    vi.mocked(fetchExpectations).mockResolvedValue([PORT_SCAN, SIZELESS])
  })

  it('removes the row and re-reads the list from the server', async () => {
    vi.mocked(forgetExpectation).mockResolvedValue(null)
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    // The second read is what the row disappearing has to come from --
    // an optimistic splice would pass a "row is gone" assertion without
    // the server ever having been asked.
    vi.mocked(fetchExpectations).mockResolvedValue([SIZELESS])
    await fireEvent.click(screen.getByRole('button', { name: 'Forget the expectation for 192.168.1.50' }))
    await settle()

    expect(vi.mocked(forgetExpectation)).toHaveBeenCalledWith('port_scan|192.168.1.50')
    expect(vi.mocked(fetchExpectations)).toHaveBeenCalledTimes(2)
    expect(screen.queryByText('192.168.1.50')).toBeNull()
    expect(screen.getByText('any size')).toBeTruthy()
  })

  it("shows the server's own refusal beside the row, and keeps the row", async () => {
    vi.mocked(forgetExpectation).mockResolvedValue('user role required')
    render(ExpectationsLedger, { canEdit: true })
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'Forget the expectation for 192.168.1.50' }))
    await settle()

    expect(screen.getByText('user role required')).toBeTruthy()
    expect(screen.getByText('192.168.1.50')).toBeTruthy()
    // A refused forget must not have re-read the list: the ledger would
    // then look refreshed when nothing changed.
    expect(vi.mocked(fetchExpectations)).toHaveBeenCalledTimes(1)
  })
})
