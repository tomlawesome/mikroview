// SPDX-License-Identifier: AGPL-3.0-only
//
// #1015: the ingest-loss half of this component's old coverage moved to
// IngestLossDrawer.svelte.test.ts along with the banner family itself.
// What is left to prove here is just the connecting/disconnected line.

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import ConnectionBanner from './ConnectionBanner.svelte'

beforeEach(() => {
  appState.connState = 'open'
  appState.refreshError = null
})

afterEach(() => {
  cleanup()
})

describe('ConnectionBanner', () => {
  it('renders nothing while the socket is open', () => {
    render(ConnectionBanner)
    flushSync()
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('shows the connecting line while the socket is connecting', () => {
    appState.connState = 'connecting'
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    expect(banner.className).toContain('banner-connecting')
    expect(banner.textContent).toContain('Connecting to mikroview')
  })

  it('shows the disconnected line while the socket is closed', () => {
    appState.connState = 'closed'
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    expect(banner.className).toContain('banner-closed')
    expect(banner.textContent).toContain('Disconnected from server')
  })

  // #1089: the socket being open only says the live feed is up -- it says
  // nothing about the separate background HTTP polls (stats/flags/
  // watchlist). A failed one used to leave no on-screen signal at all.
  it('shows the stale-refresh line when the socket is open but a background refresh failed', () => {
    appState.connState = 'open'
    appState.refreshError = 'stats: fetchStats: 500'
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    expect(banner.className).toContain('banner-refresh-error')
    expect(banner.textContent).toContain(
      'Live feed is up, but the last background refresh failed: stats: fetchStats: 500. Numbers may be stale.',
    )
  })

  it('renders nothing when the socket is open and no refresh has failed', () => {
    appState.connState = 'open'
    appState.refreshError = null
    render(ConnectionBanner)
    flushSync()

    expect(screen.queryByRole('status')).toBeNull()
  })
})
