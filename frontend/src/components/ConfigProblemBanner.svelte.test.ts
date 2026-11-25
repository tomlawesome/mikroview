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

// #1354: history off with files still on disk is a live `warn` entry,
// not a setting being ignored, and it links to the disk card.
describe('the history-held-while-off entry', () => {
  const held = {
    code: 'history-held-while-off',
    key: 'history.enabled',
    severity: 'warn',
    message: 'History is off, but 3 days (2026-09-01 → 2026-09-03, 12.0KiB) are still on disk. Nothing new is kept.',
    remediation: 'Turn history on with the same key to use them, or delete them in Settings.',
  }

  it('is drawn apart from the ignored settings, with a link to the disk card', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ problems: [held] }) })
    authState.role = 'admin'
    const { container, getByRole } = render(ConfigProblemBanner)

    await waitFor(() => expect(container.querySelector('.banner .warn')).toBeTruthy())
    expect(container.textContent).not.toContain('being ignored')
    expect(container.querySelector('.warn')?.textContent).toContain('History is off, but 3 days')
    expect(container.querySelector('.warn')?.textContent).toContain('Turn history on with the same key')
    expect(getByRole('button', { name: 'Go to the disk card' })).toBeTruthy()
  })

  it('appears and goes on refresh, without a reload', async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ problems: [] }) })
    authState.role = 'admin'
    const { container } = render(ConfigProblemBanner)
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    expect(container.querySelector('.banner')).toBeNull()

    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ problems: [held] }) })
    await configProblemsState.refresh()
    await waitFor(() => expect(container.querySelector('.banner .warn')).toBeTruthy())

    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ problems: [] }) })
    await configProblemsState.refresh()
    await waitFor(() => expect(container.querySelector('.banner')).toBeNull())
  })

  it('shows again after a hide when it is new', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ problems: [{ code: 'clamped', key: 'buffer.size', message: 'too large' }] }),
    })
    authState.role = 'admin'
    const { container } = render(ConfigProblemBanner)
    await waitFor(() => expect(container.querySelector('.banner')).toBeTruthy())
    configProblemsState.dismissed = true
    await waitFor(() => expect(container.querySelector('.banner')).toBeNull())

    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ problems: [{ code: 'clamped', key: 'buffer.size', message: 'too large' }, held] }),
    })
    await configProblemsState.refresh()
    await waitFor(() => expect(container.querySelector('.banner .warn')).toBeTruthy())
  })

  it('a session that never asked does not start asking on refresh', async () => {
    authState.role = 'viewer'
    render(ConfigProblemBanner)
    await configProblemsState.refresh()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
