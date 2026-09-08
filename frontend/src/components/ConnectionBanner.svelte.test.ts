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
})
