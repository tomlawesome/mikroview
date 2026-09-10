// SPDX-License-Identifier: AGPL-3.0-only

// #995/#1015: the ingest-loss banner family. MikroView can lose log data
// four distinct ways, plus a fifth condition (wsDropped) that is not a
// loss at all -- just one browser tab falling behind a live feed it
// already stored. This module turns the raw counters into the rows
// IngestLossDrawer.svelte renders, kept separate from that component so
// selection and the reopen rule can be unit tested without a component
// harness.
//
// #1015 split what used to be one totals-based read into two: the
// counters are process-lifetime monotonic, so "above zero" cannot mean
// "still happening" -- selectIngestLossRows below reads the server's
// per-counter `active` freshness signal instead (see SyslogIngestLoss in
// types.ts) and shows a row only while its condition is still true.
// toIngestLossInputs/IngestLossInputs below are the older totals-based
// path, kept because EngineRoom.svelte's Settings readout deliberately
// still shows the totals ("the total lives in Settings" -- issue #1015).
//
// Severity scale (owner-ratified, 2026-09-06): red critical, orange
// warning, yellow below warning, cyan information. SEVERITY_ORDER below
// is the sort key both paths use, worst first.

import type { SectionId } from './sectionLink'
import type { SyslogIngestLoss } from './types'

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
  // #1001: where this banner's `details` link goes, absent when it has
  // none. Set here rather than in the component so which banners carry
  // the link stays unit-testable -- and it is a real loss that carries
  // it: wsDropped lost nothing, so the drawing ends it with the
  // reassurance instead (docs/design/concepts/ingest-loss-995, scene 2's
  // note).
  details?: SectionId
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

// --- #1015: freshness-based row selection, IngestLossDrawer.svelte's own path ---

// wsDropped's client-side counterpart to the server's per-counter
// window -- it is a browser-side total (ws.ts), so it needs the same
// episode/lastAt/active treatment the server gives its own four
// counters, computed here instead of on a server that has never seen
// this number. 60s: same window the server gives `rejected`/`oversized`,
// the two other signals about something happening right now rather
// than a slow multi-minute drain.
export const WS_DROPPED_WINDOW_MS = 60_000

export interface WsDroppedEpisode {
  // Cumulative total, as the socket reports it (appState.wsDropped).
  total: number
  // How much of that total arrived in the current episode.
  recent: number
  // Epoch ms of the last increase, or null if the counter has never moved.
  lastAt: number | null
}

export const EMPTY_WS_DROPPED_EPISODE: WsDroppedEpisode = { total: 0, recent: 0, lastAt: null }

// noteWsDropped folds one new cumulative total into the episode, the
// same rule tcp_listener.go applies server-side: if the counter moved
// and the gap since its last move exceeds the window, the episode
// restarts at the size of this move rather than accumulating across the
// gap; otherwise it adds to the running episode. A total that went
// backwards (a fresh WebSocket connection resetting to 0 -- see ws.ts's
// onopen) starts a clean episode rather than reading as a negative move.
export function noteWsDropped(episode: WsDroppedEpisode, total: number, now: number): WsDroppedEpisode {
  if (total === episode.total) return episode
  if (total < episode.total) return { total, recent: 0, lastAt: null }

  const delta = total - episode.total
  const withinWindow = episode.lastAt !== null && now - episode.lastAt <= WS_DROPPED_WINDOW_MS
  return { total, recent: withinWindow ? episode.recent + delta : delta, lastAt: now }
}

// wsDroppedActive mirrors the server's own `active` rule (now - lastAt
// <= window), recomputed against whatever `now` the caller supplies --
// state.svelte.ts already re-runs this against a periodically-updated
// clock (see AppState.now), so a tab that stops dropping events falls
// quiet again without needing a new WS message to notice.
export function wsDroppedActive(episode: WsDroppedEpisode, now: number): boolean {
  return episode.lastAt !== null && now - episode.lastAt <= WS_DROPPED_WINDOW_MS
}

// The inputs selectIngestLossRows needs: the server's own freshness
// block (absent when stats haven't loaded yet, or a test fixture
// predates it -- every row reads as inactive rather than throwing) plus
// the client-computed wsDropped activity above.
export interface IngestLossRowInputs {
  loss?: SyslogIngestLoss
  wsDropped: { recent: number; active: boolean }
}

// selectIngestLossRows returns one row per signal whose condition is
// still true right now (`active`), in severity order, worst first. Never
// returns a row for a signal that has gone quiet, however large its
// total -- "a row shows while its condition is still true" (#1015). The
// drawer never scrolls (five rows at most), so unlike the retired
// selectIngestLossBar there is no lead/more collapse: every active row
// renders.
export function selectIngestLossRows(input: IngestLossRowInputs): IngestLossBanner[] {
  const rows: IngestLossBanner[] = []
  const loss = input.loss

  if (loss?.dropped.active) {
    rows.push({
      id: 'dropped',
      severity: 'critical',
      label: 'Ingest queue full',
      details: 'engineroom/ingest',
      detail: `${loss.dropped.recent.toLocaleString()} log ${noun(loss.dropped.recent, 'line')} lost`,
    })
  }

  if (loss?.rejectedConfigured.active) {
    const host = loss.rejectedConfigured.hosts[0]
    const lockout = host ? ` (${host} locked out)` : ''
    rows.push({
      id: 'rejectedConfigured',
      severity: 'warn',
      label: 'Syslog slots full',
      details: 'engineroom/ingest',
      detail: `${loss.rejectedConfigured.recent.toLocaleString()} refused${lockout}`,
    })
  }

  if (loss?.rejected.active) {
    rows.push({
      id: 'rejectedUndeclared',
      severity: 'caution',
      label: 'Undeclared sources',
      details: 'engineroom/ingest',
      detail: `${loss.rejected.recent.toLocaleString()} ${noun(loss.rejected.recent, 'connection')} refused`,
    })
  }

  if (loss?.oversized.active) {
    const from = loss.oversized.host ? ` received from ${loss.oversized.host}` : ' received'
    const verb = loss.oversized.recent === 1 ? 'was' : 'were'
    rows.push({
      id: 'oversized',
      severity: 'caution',
      label: 'Non-RouterOS sender',
      details: 'engineroom/ingest',
      detail: `${loss.oversized.recent.toLocaleString()} oversized ${noun(loss.oversized.recent, 'message')}${from} ${verb} truncated`,
    })
  }

  if (input.wsDropped.active) {
    rows.push({
      id: 'wsDropped',
      severity: 'info',
      label: 'Slow browser tab',
      detail: `${input.wsDropped.recent.toLocaleString()} ${noun(input.wsDropped.recent, 'event')} not shown (but still logged)`,
    })
  }

  return rows.sort((a, b) => SEVERITY_ORDER.indexOf(a.severity) - SEVERITY_ORDER.indexOf(b.severity))
}

// shouldReopen is the drawer's reopen rule (#1015): hiding records which
// kinds were showing at the moment of the click ("I have seen these"),
// so a kind outside that set -- new, or one that cleared and came back
// -- has not been seen and reopens the drawer. Pure and independent of
// IngestLossDrawer.svelte's own $state so the rule is testable without a
// component harness, the same reasoning as everything else in this file.
export function shouldReopen(
  hiddenAt: ReadonlySet<IngestLossBannerId>,
  activeIds: readonly IngestLossBannerId[],
): boolean {
  return activeIds.some((id) => !hiddenAt.has(id))
}
