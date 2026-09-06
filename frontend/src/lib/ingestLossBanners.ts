// SPDX-License-Identifier: AGPL-3.0-only

// #995: the ingest-loss banner family. MikroView can lose log data four
// distinct ways, plus a fifth condition (wsDropped) that is not a loss
// at all -- just one browser tab falling behind a live feed it already
// stored. This module turns the raw counters into the banners the
// owner ratified at docs/design/concepts/ingest-loss-995 (see that
// mockup's README for the drawing this mirrors field-for-field), kept
// separate from ConnectionBanner.svelte so the selection and collapse
// logic can be unit tested without a component harness.
//
// Severity scale (owner-ratified, 2026-09-06): red critical, orange
// warning, yellow below warning, cyan information. SEVERITY_ORDER below
// is both the sort key (worst leads the collapsed bar) and the
// expanded stack's order.

export type IngestLossSeverity = 'critical' | 'warn' | 'caution' | 'info'

const SEVERITY_ORDER: readonly IngestLossSeverity[] = ['critical', 'warn', 'caution', 'info']

export type IngestLossBannerId = 'dropped' | 'rejectedConfigured' | 'rejectedUndeclared' | 'oversized' | 'wsDropped'

export interface IngestLossBanner {
  id: IngestLossBannerId
  severity: IngestLossSeverity
  // The owner's ratified copy, verbatim, with numbers and the
  // router/IP already substituted -- nothing downstream reformats
  // either half further. Split in two so the component can set them
  // as the ratified two-tier hierarchy (label bold and dominant,
  // detail at text weight) instead of one flat string -- see
  // docs/design/concepts/ingest-loss-995's <strong>/<span class="dt">
  // markup, which this mirrors field-for-field.
  label: string
  detail: string
}

// The five raw counters the banners are built from. rejectedUndeclared
// is already the "undeclared sources" subset (total Rejected minus the
// RejectedConfigured slice), computed by the caller from
// SyslogListenerStats -- see toIngestLossInputs.
export interface IngestLossInputs {
  dropped: number
  rejectedConfigured: number
  rejectedConfiguredHosts: string[]
  rejectedUndeclared: number
  oversized: number
  oversizedHost: string
  wsDropped: number
}

// Mirrors internal/syslog.ListenerStats' two totals (Rejected counts
// every refusal, RejectedConfigured only declared-router ones) so
// callers pass the type frontend/src/lib/types.ts already has rather
// than pre-computing the subtraction themselves.
export function toIngestLossInputs(syslog: {
  rejected: number
  rejectedConfigured: number
  rejectedConfiguredHosts: string[]
  dropped: number
  oversized: number
  oversizedHost: string
}, wsDropped: number): IngestLossInputs {
  return {
    dropped: syslog.dropped,
    rejectedConfigured: syslog.rejectedConfigured,
    rejectedConfiguredHosts: syslog.rejectedConfiguredHosts,
    rejectedUndeclared: Math.max(syslog.rejected - syslog.rejectedConfigured, 0),
    oversized: syslog.oversized,
    oversizedHost: syslog.oversizedHost,
    wsDropped,
  }
}

// noun picks the singular or plural form by count -- the drawing's
// "singular when a count is 1" rule, applied consistently rather than
// left for each call site to remember.
function noun(n: number, singular: string, plural = `${singular}s`): string {
  return n === 1 ? singular : plural
}

// buildIngestLossBanners returns one banner per signal currently above
// zero, in severity order (worst first). Never returns a banner for a
// signal at zero -- a healthy counter is silent, not a "0" banner.
export function buildIngestLossBanners(input: IngestLossInputs): IngestLossBanner[] {
  const banners: IngestLossBanner[] = []

  if (input.dropped > 0) {
    banners.push({
      id: 'dropped',
      severity: 'critical',
      label: 'Ingest queue full',
      detail: `${input.dropped.toLocaleString()} log ${noun(input.dropped, 'line')} lost`,
    })
  }

  if (input.rejectedConfigured > 0) {
    const host = input.rejectedConfiguredHosts[0]
    const lockout = host ? ` (${host} locked out)` : ''
    banners.push({
      id: 'rejectedConfigured',
      severity: 'warn',
      label: 'Syslog slots full',
      detail: `${input.rejectedConfigured.toLocaleString()} refused${lockout}`,
    })
  }

  if (input.rejectedUndeclared > 0) {
    banners.push({
      id: 'rejectedUndeclared',
      severity: 'caution',
      label: 'Undeclared sources',
      detail: `${input.rejectedUndeclared.toLocaleString()} ${noun(input.rejectedUndeclared, 'connection')} refused`,
    })
  }

  if (input.oversized > 0) {
    const from = input.oversizedHost ? ` received from ${input.oversizedHost}` : ' received'
    const verb = input.oversized === 1 ? 'was' : 'were'
    banners.push({
      id: 'oversized',
      severity: 'caution',
      label: 'Non-RouterOS sender',
      detail: `${input.oversized.toLocaleString()} oversized ${noun(input.oversized, 'message')}${from} ${verb} truncated`,
    })
  }

  if (input.wsDropped > 0) {
    banners.push({
      id: 'wsDropped',
      severity: 'info',
      label: 'Slow browser tab',
      detail: `${input.wsDropped.toLocaleString()} ${noun(input.wsDropped, 'event')} not shown (but still logged)`,
    })
  }

  return banners.sort((a, b) => SEVERITY_ORDER.indexOf(a.severity) - SEVERITY_ORDER.indexOf(b.severity))
}

// IngestLossBar is what ConnectionBanner.svelte actually renders: the
// collapse-to-one-bar behaviour the owner ratified (several signals
// firing at once never push the app down more than one line; the
// worst leads; a "+N more" chip expands the rest). `banners` is the
// full severity-ordered stack for when the caller's own `expanded`
// state is true.
export interface IngestLossBar {
  banners: IngestLossBanner[]
  lead: IngestLossBanner | null
  moreCount: number
}

export function selectIngestLossBar(input: IngestLossInputs): IngestLossBar {
  const banners = buildIngestLossBanners(input)
  return {
    banners,
    lead: banners[0] ?? null,
    moreCount: Math.max(banners.length - 1, 0),
  }
}
