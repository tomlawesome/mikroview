// SPDX-License-Identifier: AGPL-3.0-only
//
// #995: the ingest-loss banner family. These are the component-level
// tests that prove two things lib/ingestLossBanners.test.ts cannot
// reach on its own: what actually lands in the DOM (the CSS class a
// screen reader / visual regression cares about, not just the severity
// string), and the +N-more collapse/expand click behaviour.

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { appState } from '../lib/state.svelte'
import type { SyslogListenerStats } from '../lib/types'
import ConnectionBanner from './ConnectionBanner.svelte'

function syslogStats(overrides: Partial<SyslogListenerStats> = {}): SyslogListenerStats {
  return {
    inUse: 0,
    capacity: 256,
    reservedForConfigured: 64,
    rejected: 0,
    rejectedConfigured: 0,
    dropped: 0,
    oversized: 0,
    rejectedConfiguredHosts: [],
    oversizedHost: '',
    ...overrides,
  }
}

beforeEach(() => {
  appState.connState = 'open'
  appState.wsDropped = 0
  appState.stats = null
})

afterEach(() => {
  cleanup()
})

describe('ConnectionBanner', () => {
  it('renders nothing when every counter is healthy', () => {
    render(ConnectionBanner)
    flushSync()
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('demotes the feed-drop notice to the cyan info tier, not a warning', () => {
    // This is the regression test for the issue's core ask: wsDropped
    // used to render with class="banner banner-warning" (--drop
    // amber), styled identically to a real data-loss banner. #995
    // demotes it to banner-info (--log cyan) with the reassurance that
    // nothing was actually lost.
    appState.wsDropped = 156
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    expect(banner.className).toContain('banner-info')
    expect(banner.className).not.toContain('banner-warning')
    expect(banner.className).not.toContain('banner-warn')
    expect(banner.textContent).toContain('Slow browser tab: 156 events not shown (but still logged)')
  })

  it('sets the label as its own bold element, not part of a flat string (owner ruling, 2026-09-06)', () => {
    // The two-tier hierarchy the owner asked for: the label is the
    // heading, bold and read first; the detail is subordinate, at text
    // weight. Ported from docs/design/concepts/ingest-loss-995's
    // <strong>/<span class="dt"> markup, so this proves the label is a
    // distinct emphasised element rather than a substring of one flat
    // banner line.
    appState.wsDropped = 156
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    const strong = banner.querySelector('strong')
    expect(strong).not.toBeNull()
    expect(strong?.textContent).toBe('Slow browser tab:')
    expect(banner.textContent).toContain('156 events not shown (but still logged)')
  })

  it('renders the single firing signal alone, with no "+more" chip', () => {
    appState.stats = { syslog: syslogStats({ rejected: 2867 }) } as never
    render(ConnectionBanner)
    flushSync()

    const banner = screen.getByRole('status')
    expect(banner.className).toContain('banner-caution')
    expect(banner.textContent).toContain('Undeclared sources: 2,867 connections refused')
    expect(screen.queryByText(/more/)).toBeNull()
  })

  it('collapses several firing signals to one bar led by the worst severity, with a working +N more chip', async () => {
    appState.stats = {
      syslog: syslogStats({
        dropped: 812,
        rejectedConfigured: 43,
        rejectedConfiguredHosts: ['branch-e4a1'],
        rejected: 2910,
      }),
    } as never
    appState.wsDropped = 156
    render(ConnectionBanner)
    flushSync()

    // Only one bar's height paid, even with four signals firing.
    expect(screen.getAllByRole('status')).toHaveLength(1)
    const lead = screen.getByRole('status')
    expect(lead.className).toContain('banner-critical')
    expect(lead.textContent).toContain('Ingest queue full: 812 log lines lost')

    const chip = screen.getByRole('button', { name: '+3 more' })
    await fireEvent.click(chip)
    flushSync()

    const expanded = screen.getAllByRole('status')
    expect(expanded).toHaveLength(4)
    expect(expanded[1].textContent).toContain('Syslog slots full: 43 refused (branch-e4a1 locked out)')
    expect(expanded[1].className).toContain('banner-warn')

    await fireEvent.click(screen.getByRole('button', { name: 'less' }))
    flushSync()
    expect(screen.getAllByRole('status')).toHaveLength(1)
  })
})
