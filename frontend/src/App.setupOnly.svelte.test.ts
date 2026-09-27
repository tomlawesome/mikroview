// SPDX-License-Identifier: AGPL-3.0-only
//
// #1347: setup-only mode. When /api/healthz reports mode "setup-only",
// a signed-in session gets the config editor and its banner and nothing
// else: no deck, no navigation, and none of the start-up fetches, polls
// or socket that would each only meet a 503.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('./lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./lib/api')>()
  return {
    ...actual,
    fetchHealthz: vi.fn(async () => ({
      status: 'ok',
      time: '',
      uptime: '',
      uptimeSeconds: 0,
      version: 'v0.7.0',
      geoip: false,
      mode: 'setup-only',
    })),
    fetchAuthSession: vi.fn(async () => ({
      authenticated: true,
      setupRequired: false,
      ssoAvailable: false,
      username: 'tom',
      role: 'admin',
    })),
    fetchMyPreferences: vi.fn(async () => ({ version: 1, prefs: {} })),
    fetchEvents: vi.fn(async () => ({ events: [] })),
    fetchDevices: vi.fn(async () => []),
    fetchStats: vi.fn(async () => ({})),
    fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
    fetchWatchlistEntries: vi.fn(async () => ({ entries: [], coverage: {} })),
    openConfigEditor: vi.fn(),
    validateConfig: vi.fn(async () => ({ problems: [] })),
    fetchConfigSnapshots: vi.fn(async () => ({ snapshots: [], keep: 5 })),
  }
})

vi.mock('./lib/ws', () => ({
  liveSocket: { connect: vi.fn(), disconnect: vi.fn(), onChange: vi.fn(() => vi.fn()) },
}))

// viewportState reads matchMedia at module load; jsdom has none.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

const { default: App } = await import('./App.svelte')
const { authState } = await import('./lib/auth.svelte')
const { liveSocket } = await import('./lib/ws')
const api = await import('./lib/api')

const BANNER =
  'MikroView started in setup-only mode because the config file was refused. Fix it here, download it, put it in place and restart.'

describe('setup-only mode (#1347)', () => {
  beforeEach(() => {
    authState.state = 'loading'
    authState.role = ''
  })

  it('goes straight to the editor with the banner, and starts nothing else', async () => {
    render(App)

    await vi.waitFor(() => {
      expect(screen.getByText(BANNER)).toBeTruthy()
    })
    // The editor asks for the password before it shows the text.
    expect(screen.getByLabelText('Password')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()

    // No shell: no deck, no skip link, no navigation.
    expect(document.querySelector('.deck')).toBeNull()
    expect(document.querySelector('#main-content')).toBeNull()

    // Give any stray start-up fetch a chance to fire before asserting it did not.
    await new Promise((r) => setTimeout(r, 20))
    expect(api.fetchEvents).not.toHaveBeenCalled()
    expect(api.fetchStats).not.toHaveBeenCalled()
    expect(api.fetchFlags).not.toHaveBeenCalled()
    expect(api.fetchWatchlistEntries).not.toHaveBeenCalled()
    expect(api.fetchMyPreferences).not.toHaveBeenCalled()
    expect(liveSocket.connect).not.toHaveBeenCalled()
  })

  it('opens the editor with the password, still without the shell', async () => {
    vi.mocked(api.openConfigEditor).mockResolvedValue({
      text: 'listen:\n  bogus: 1\n',
      path: '/etc/mikroview/config.yaml',
      changedSinceStart: false,
      header: null,
      schemaGuess: 6,
      runningVersion: 'v0.7.0',
      runningSchema: 8,
    })
    render(App)
    await vi.waitFor(() => {
      expect(screen.getByLabelText('Password')).toBeTruthy()
    })
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Open' }))

    await vi.waitFor(() => {
      expect((screen.getByLabelText('config.yaml') as HTMLTextAreaElement).value).toBe('listen:\n  bogus: 1\n')
    })
    expect(api.openConfigEditor).toHaveBeenCalledWith('hunter2')
    expect(screen.getByText(BANNER)).toBeTruthy()
    expect(document.querySelector('#main-content')).toBeNull()
    expect(liveSocket.connect).not.toHaveBeenCalled()
  })
})
