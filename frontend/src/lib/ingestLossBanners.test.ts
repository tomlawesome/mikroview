// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  buildIngestLossBanners,
  selectIngestLossBar,
  toIngestLossInputs,
  type IngestLossInputs,
} from './ingestLossBanners'

function inputs(overrides: Partial<IngestLossInputs> = {}): IngestLossInputs {
  return {
    dropped: 0,
    rejectedConfigured: 0,
    rejectedConfiguredHosts: [],
    rejectedUndeclared: 0,
    oversized: 0,
    oversizedHost: '',
    wsDropped: 0,
    ...overrides,
  }
}

describe('buildIngestLossBanners', () => {
  it('is empty when every counter is zero -- healthy is silent', () => {
    expect(buildIngestLossBanners(inputs())).toEqual([])
  })

  it('renders the owner-ratified copy verbatim for each signal', () => {
    const banners = buildIngestLossBanners(
      inputs({
        dropped: 812,
        rejectedConfigured: 43,
        rejectedConfiguredHosts: ['branch-e4a1'],
        rejectedUndeclared: 2867,
        oversized: 7,
        oversizedHost: '10.20.3.9',
        wsDropped: 156,
      }),
    )

    const byId = Object.fromEntries(banners.map((b) => [b.id, b]))
    expect(byId.dropped.text).toBe('Ingest queue full: 812 log lines lost')
    expect(byId.rejectedConfigured.text).toBe('Syslog slots full: 43 refused (branch-e4a1 locked out)')
    expect(byId.rejectedUndeclared.text).toBe('Undeclared sources: 2,867 connections refused')
    expect(byId.oversized.text).toBe('Non-RouterOS sender: 7 oversized messages received from 10.20.3.9 were truncated')
    expect(byId.wsDropped.text).toBe('Slow browser tab: 156 events not shown (but still logged)')
  })

  it('uses the singular form and verb when a count is exactly 1', () => {
    const banners = buildIngestLossBanners(
      inputs({
        dropped: 1,
        rejectedUndeclared: 1,
        oversized: 1,
        oversizedHost: '10.20.3.9',
        wsDropped: 1,
      }),
    )
    const byId = Object.fromEntries(banners.map((b) => [b.id, b]))
    expect(byId.dropped.text).toBe('Ingest queue full: 1 log line lost')
    expect(byId.rejectedUndeclared.text).toBe('Undeclared sources: 1 connection refused')
    expect(byId.oversized.text).toBe('Non-RouterOS sender: 1 oversized message received from 10.20.3.9 was truncated')
    expect(byId.wsDropped.text).toBe('Slow browser tab: 1 event not shown (but still logged)')
  })

  it('omits the locked-out router name when no host is retained', () => {
    const [banner] = buildIngestLossBanners(inputs({ rejectedConfigured: 43, rejectedConfiguredHosts: [] }))
    expect(banner.text).toBe('Syslog slots full: 43 refused')
  })

  it('omits "received from" when no oversized sender host is known yet', () => {
    const [banner] = buildIngestLossBanners(inputs({ oversized: 7, oversizedHost: '' }))
    expect(banner.text).toBe('Non-RouterOS sender: 7 oversized messages received were truncated')
  })

  it('assigns the owner-ratified severity per signal', () => {
    const banners = buildIngestLossBanners(
      inputs({
        dropped: 1,
        rejectedConfigured: 1,
        rejectedUndeclared: 1,
        oversized: 1,
        wsDropped: 1,
      }),
    )
    const severityById = Object.fromEntries(banners.map((b) => [b.id, b.severity]))
    expect(severityById.dropped).toBe('critical')
    expect(severityById.rejectedConfigured).toBe('warn')
    expect(severityById.rejectedUndeclared).toBe('caution')
    expect(severityById.oversized).toBe('caution')
    expect(severityById.wsDropped).toBe('info')
  })

  it('demotes the feed-drop notice: wsDropped is info, never warn', () => {
    // This is the regression test for the issue's core demotion --
    // ConnectionBanner.svelte used to render wsDropped as
    // banner-warning, styled identically to a real data-loss banner.
    // #995 demotes it to the cyan info tier.
    const [banner] = buildIngestLossBanners(inputs({ wsDropped: 5 }))
    expect(banner.severity).toBe('info')
    expect(banner.severity).not.toBe('warn')
  })

  it('orders every combination worst-severity-first regardless of input order', () => {
    const banners = buildIngestLossBanners(
      inputs({
        wsDropped: 1,
        oversized: 1,
        rejectedUndeclared: 1,
        rejectedConfigured: 1,
        dropped: 1,
      }),
    )
    expect(banners.map((b) => b.severity)).toEqual(['critical', 'warn', 'caution', 'caution', 'info'])
  })
})

describe('selectIngestLossBar', () => {
  it('has no lead and no banners when healthy', () => {
    const bar = selectIngestLossBar(inputs())
    expect(bar.lead).toBeNull()
    expect(bar.banners).toEqual([])
    expect(bar.moreCount).toBe(0)
  })

  it('leads with the single firing signal and reports no "more" when only one fires', () => {
    const bar = selectIngestLossBar(inputs({ rejectedUndeclared: 3 }))
    expect(bar.lead?.id).toBe('rejectedUndeclared')
    expect(bar.moreCount).toBe(0)
    expect(bar.banners).toHaveLength(1)
  })

  it('collapses several firing signals to one lead, worst severity, plus a +N more count', () => {
    const bar = selectIngestLossBar(
      inputs({
        dropped: 812,
        rejectedConfigured: 43,
        rejectedConfiguredHosts: ['branch-e4a1'],
        rejectedUndeclared: 2867,
        wsDropped: 156,
      }),
    )
    // Scene 4 of the ratified drawing: four signals firing, one bar,
    // the critical one leads, "+3 more" covers the rest.
    expect(bar.lead?.id).toBe('dropped')
    expect(bar.moreCount).toBe(3)
    expect(bar.banners.map((b) => b.id)).toEqual(['dropped', 'rejectedConfigured', 'rejectedUndeclared', 'wsDropped'])
  })

  it('re-collapses to the new worst lead once the previous worst clears', () => {
    // The ratified behaviour ("re-collapses when counters go quiet")
    // is a property of this being a pure function of current counts,
    // not state carried between renders -- once `dropped` returns to
    // 0 the lead simply becomes the next-worst signal on the next call.
    const stillFiring = selectIngestLossBar(inputs({ rejectedConfigured: 1, rejectedUndeclared: 1 }))
    expect(stillFiring.lead?.id).toBe('rejectedConfigured')
    expect(stillFiring.moreCount).toBe(1)
  })
})

describe('toIngestLossInputs', () => {
  it('derives the undeclared-only count from the two backend totals', () => {
    const result = toIngestLossInputs(
      {
        rejected: 2910,
        rejectedConfigured: 43,
        rejectedConfiguredHosts: ['branch-e4a1'],
        dropped: 812,
        oversized: 7,
        oversizedHost: '10.20.3.9',
      },
      156,
    )
    expect(result.rejectedUndeclared).toBe(2867)
    expect(result.rejectedConfigured).toBe(43)
    expect(result.wsDropped).toBe(156)
  })

  it('never goes negative if the two backend totals are inconsistent', () => {
    const result = toIngestLossInputs(
      {
        rejected: 5,
        rejectedConfigured: 9,
        rejectedConfiguredHosts: [],
        dropped: 0,
        oversized: 0,
        oversizedHost: '',
      },
      0,
    )
    expect(result.rejectedUndeclared).toBe(0)
  })
})
