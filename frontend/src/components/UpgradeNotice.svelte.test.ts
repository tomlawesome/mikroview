// SPDX-License-Identifier: AGPL-3.0-only
//
// #1240: after an upgrade the operator has to paste the certificate step again on each
// router. These pin the copy, the two controls, who sees the line, and
// the two rules that take it away -- `done`, and (#1241) a fleet that
// has caught up on its own.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, waitFor } from '@testing-library/svelte'

import { authState } from '../lib/auth.svelte'
import { upgradeState } from '../lib/upgrade.svelte'
import UpgradeNotice from './UpgradeNotice.svelte'

const fetchMock = vi.fn()

// upgrade builds one server answer, defaulting to the shape the notice
// shows for: a real crossing, unacknowledged, with no router having
// reported yet.
function upgrade(over: Record<string, unknown> = {}) {
  return {
    previous: 'v0.4.0',
    current: 'v0.5.0',
    noticedAt: '2026-09-15T10:00:00Z',
    acknowledged: false,
    routers: { behind: 0, total: 0, reported: 0 },
    ...over,
  }
}

function serve(body: unknown) {
  fetchMock.mockResolvedValue({ ok: true, json: async () => body })
}

beforeEach(() => {
  cleanup()
  fetchMock.mockReset()
  serve(upgrade())
  vi.stubGlobal('fetch', fetchMock)
  authState.role = 'admin'
  upgradeState.upgrade = null
  // ensureLoaded only ever fetches once per process, so its private
  // latch is reset between tests the same way ConfigProblemBanner's is.
  ;(upgradeState as unknown as { loaded: boolean }).loaded = false
})

describe('the upgrade notice', () => {
  it('says what happened and what to do, with both controls', async () => {
    const { container, getByText } = render(UpgradeNotice)

    await waitFor(() => expect(container.querySelector('.banner')).toBeTruthy())
    expect(container.querySelector('.line')?.textContent).toBe(
      'upgraded from v0.4.0 · paste step 1 of the setup again on each router',
    )
    expect(getByText('open setup')).toBeTruthy()
    expect(getByText('done')).toBeTruthy()
  })

  it('counts the routers still on the old setup once they report', async () => {
    serve(upgrade({ routers: { behind: 2, total: 3, reported: 3 } }))
    const { container } = render(UpgradeNotice)

    await waitFor(() =>
      expect(container.querySelector('.line')?.textContent).toBe(
        'upgraded from v0.4.0 · 2 of 3 routers still on the old setup · paste Trust the certificate again on each',
      ),
    )
    // Every router has reported, so the count is the truth and the line
    // will clear itself as they catch up: `done` would dismiss the work
    // rather than finish it, and is not offered.
    expect(container.querySelector('.done')).toBeNull()
  })

  it('still offers done while some router has never reported', async () => {
    serve(upgrade({ routers: { behind: 3, total: 3, reported: 1 } }))
    const { container } = render(UpgradeNotice)

    await waitFor(() => expect(container.querySelector('.done')).toBeTruthy())
  })

  it('done tells the server and takes the line away', async () => {
    const { container, getByText } = render(UpgradeNotice)
    await waitFor(() => expect(container.querySelector('.banner')).toBeTruthy())

    serve(upgrade({ acknowledged: true }))
    getByText('done').click()

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/upgrade/acknowledge', expect.objectContaining({ method: 'POST' })),
    )
    await waitFor(() => expect(container.querySelector('.banner')).toBeNull())
  })

  it('stays away once every router reports the current setup', async () => {
    serve(upgrade({ routers: { behind: 0, total: 3, reported: 3 } }))
    const { container } = render(UpgradeNotice)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(container.querySelector('.banner')).toBeNull()
  })

  it('says nothing on a first install', async () => {
    serve(upgrade({ previous: '' }))
    const { container } = render(UpgradeNotice)

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    expect(container.querySelector('.banner')).toBeNull()
  })

  it('is an admin’s line: a viewer sees nothing and asks for nothing', async () => {
    authState.role = 'viewer'
    const { container } = render(UpgradeNotice)

    await Promise.resolve()
    expect(fetchMock).not.toHaveBeenCalled()
    expect(container.querySelector('.banner')).toBeNull()
  })
})
