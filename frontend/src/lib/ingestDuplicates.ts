// SPDX-License-Identifier: AGPL-3.0-only

import type { SyslogListenerStats } from './types'

// Issue #1234: a router whose mikroview logging block has been pasted
// more than once carries several identical logging rules, so every
// line arrives two or three times over -- inflating syslog volume,
// event rows, rule usage, coverage and baselines without anything
// downstream being able to tell. Detection is server-side
// (internal/syslog/duplicate.go, GET /api/stats' syslog.loss.duplicate
// field); this module reads that result and supplies the Settings ▸
// ingest wording, the same split EngineRoom already uses for #1205's
// setup-drift line (there, oversizedSetupDriftHost derives the flag
// and the template owns the words).

// DuplicateDrift is a source currently sending every line more than
// once, and the apparent number of copies.
export interface DuplicateDrift {
  host: string
  copyCount: number
}

// duplicateDrift reads #1234's result off a stats snapshot. undefined
// both before the first sighting and once the source has gone quiet
// long enough for the server's own window to stop reporting it active
// -- active is computed fresh server-side on every request, so this
// never needs its own staleness check.
export function duplicateDrift(syslog: SyslogListenerStats | undefined): DuplicateDrift | undefined {
  const d = syslog?.loss?.duplicate
  if (!d?.active || !d.host || !d.copyCount) return undefined
  return { host: d.host, copyCount: d.copyCount }
}

// duplicateCleanupCommand clears every mikroview logging rule a router
// carries, however many times the block was pasted, so the wizard's
// step 1 block can be re-pasted cleanly. Safe to run more than once
// since #1208 made that paste idempotent.
export const duplicateCleanupCommand = '/system logging remove [find action=mikroview]'

// duplicateDriftMessage is the one-line Settings ▸ ingest copy: names
// the source, the apparent copy count, and the fix. Plain text, never
// markup: the caller puts duplicateCleanupCommand inside its own
// <code> element rather than having a string here carry any tags --
// see guards/injection-sinks.test.ts, which fails the build on the
// escape opt-out this would otherwise need.
export function duplicateDriftMessage(drift: DuplicateDrift): string {
  const times = drift.copyCount === 2 ? 'twice' : `${drift.copyCount}×`
  return `${drift.host} appears to be sending every line ${times} over`
}
