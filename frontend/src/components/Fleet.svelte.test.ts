// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import Fleet from './Fleet.svelte'

// Fleet reads appState.devices directly -- no request of its own to mock.
vi.mock('../lib/api', () => ({}))

// #549's Loading and first-run chrome states, applied to Fleet the same
// way LiveTable.svelte.test.ts covers them for the live view: a
// zero-device table is either "the app's one loadInitial() call hasn't
// come back yet" (ghost rows) or "it has, and mikroview has genuinely
// never seen a device" (the first-run pointer). Fleet has no admin gate
// of its own in the rail (unlike Watchlist/the engine room's watchers
// station/etc.), so the viewer-vs-admin wording split matters here too.
describe('Fleet Loading and first-run empty states (#549)', () => {
  beforeEach(() => {
    appState.devices = []
    appState.initialLoadDone = true
    authState.role = ''
  })

  it('shows ghost rows while the initial fetch has not settled yet', () => {
    appState.initialLoadDone = false
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.ghost-rows')).not.toBeNull()
    expect(container.querySelector('.empty')).toBeNull()
  })

  it('points an admin at your account menu ▸ Run setup… once settled with no devices ever seen', () => {
    authState.role = 'admin'
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.empty')?.textContent).toMatch(/your account menu ▸ Run setup…/)
  })

  it('tells a viewer to ask an administrator instead', () => {
    authState.role = 'user'
    const { container } = render(Fleet)
    flushSync()

    const text = container.querySelector('.empty')?.textContent ?? ''
    expect(text).not.toMatch(/Run setup…/)
    expect(text.toLowerCase()).toMatch(/administrator/)
  })

  it('renders the table instead once at least one device exists, regardless of role', () => {
    appState.devices = [
      {
        id: 'r1',
        name: 'router1',
        configured: true,
        status: 'live',
        lastSeen: '2026-08-24T00:00:00Z',
        sourceIp: '10.0.0.1',
        eventCount: 3,
      },
    ] as unknown as (typeof appState)['devices']
    const { container } = render(Fleet)
    flushSync()

    expect(container.querySelector('.empty')).toBeNull()
    expect(container.querySelector('table')).not.toBeNull()
  })
})
