// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'

// NavRail itself makes no requests -- this only stops the stores it pulls
// in from reaching for the network when they initialise under jsdom.
vi.mock('../lib/api', () => ({
  fetchFlags: vi.fn(async () => ({ flags: [], timeSeries: [] })),
  clearFlag: vi.fn(),
  clearAllFlags: vi.fn(),
  clearFlagPermanent: vi.fn(),
  logout: vi.fn(),
}))

import { flagsState } from '../lib/flags.svelte'
import type { Flag } from '../lib/types'
import NavRail from './NavRail.svelte'

function flag(id: string, cleared: boolean): Flag {
  return {
    id,
    type: 'port_scan',
    target: `198.51.100.${id}`,
    detail: 'a port scan',
    count: 1,
    firstSeen: '2026-08-23T20:00:00Z',
    lastSeen: '2026-08-23T20:00:00Z',
    cleared,
  }
}

// The count and its wording, which is all this component decides. Where
// the number comes from is internal/flags' business (an excluded pair is
// always also cleared, so "open" is already "open unexcluded"), and
// whether the badge is drawn inside the 54px rail is a layout fact that
// only a real browser can answer -- frontend/scripts/live-nav-badge.mjs
// covers both against a running server.
describe('NavRail flag badge', () => {
  beforeEach(() => {
    flagsState.list = []
  })

  it('shows no badge when nothing is open', () => {
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags' })).toBeTruthy()
  })

  it('counts open flags and speaks the count in the label', () => {
    flagsState.list = [flag('1', false), flag('2', false)]
    render(NavRail)
    const row = screen.getByRole('button', { name: 'Flags — 2 open' })
    expect(row.textContent).toContain('2')
  })

  it('leaves cleared flags out of the count', () => {
    flagsState.list = [flag('1', false), flag('2', true), flag('3', true)]
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags — 1 open' })).toBeTruthy()
  })

  // A zero would otherwise sit permanently on the row: the record puts one
  // alarm-filled count in the chrome, and only when it has something to say.
  it('drops the badge again once everything is cleared', () => {
    flagsState.list = [flag('1', true)]
    render(NavRail)
    expect(screen.getByRole('button', { name: 'Flags' })).toBeTruthy()
  })
})
