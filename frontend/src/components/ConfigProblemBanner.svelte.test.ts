// SPDX-License-Identifier: AGPL-3.0-only
//
// #1188: GET /api/config/problems is admin-gated server-side, so a
// non-admin session asking for it on load only ever produced a 403 in
// two logs. The banner is an admin's, so only an admin's session asks.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, waitFor } from '@testing-library/svelte'

import { authState } from '../lib/auth.svelte'
import { configProblemsState } from '../lib/configProblems.svelte'
import ConfigProblemBanner from './ConfigProblemBanner.svelte'

const fetchMock = vi.fn()

beforeEach(() => {
  cleanup()
  fetchMock.mockReset()
  fetchMock.mockResolvedValue({
    ok: true,
    json: async () => ({ problems: [{ code: 'clamped', key: 'buffer.size', message: 'too large' }] }),
  })
  vi.stubGlobal('fetch', fetchMock)
  configProblemsState.problems = []
  configProblemsState.dismissed = false
  // ensureLoaded() only ever fetches once per process, so its private
  // latch is reset between tests the same way the state modules with a
  // documented one-shot load are elsewhere.
  ;(configProblemsState as unknown as { loaded: boolean }).loaded = false
})

describe('who asks for the config problems', () => {
  it('does not ask at all for a session that would be refused', async () => {
    authState.role = 'viewer'
    render(ConfigProblemBanner)
    await Promise.resolve()
    expect(fetchMock).not.toHaveBeenCalled()

    authState.role = 'user'
    render(ConfigProblemBanner)
    await Promise.resolve()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('asks once for an admin, and shows what came back', async () => {
    authState.role = 'admin'
    const { container } = render(ConfigProblemBanner)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith('/api/config/problems'))
    await waitFor(() => expect(container.querySelector('.banner')).toBeTruthy())
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
