// SPDX-License-Identifier: AGPL-3.0-only

// Shared table-header sort logic (#1086). MetricsTable, AuditLog and
// Watchlist each click-a-header-to-sort, click-again-to-reverse, with an
// aria-sort value for screen readers -- three copies of the same three
// functions, differing only in what direction a freshly-clicked column
// should default to (AuditLog's 'time' defaults to newest-first/desc,
// its other columns to asc; MetricsTable always defaults to desc;
// Watchlist always defaults to asc). That default is the one thing
// callers still supply.

import type { SortDir } from './sortFilter'

export type SortState<K> = { key: K; dir: SortDir }

// Clicking the already-active key reverses its direction. Clicking a
// different key switches to it at `defaultDir`, the caller's own rule
// for that key.
export function nextSort<K>(current: SortState<K>, key: K, defaultDir: SortDir): SortState<K> {
  if (current.key === key) {
    return { key, dir: current.dir === 'asc' ? 'desc' : 'asc' }
  }
  return { key, dir: defaultDir }
}

export function ariaSort<K>(current: SortState<K>, key: K): 'ascending' | 'descending' | 'none' {
  if (current.key !== key) return 'none'
  return current.dir === 'asc' ? 'ascending' : 'descending'
}

// The ▲/▼ shown beside the active column's label in AuditLog and
// Watchlist (MetricsTable relies on aria-sort alone and has no glyph).
export function sortGlyph<K>(current: SortState<K>, key: K): string {
  if (current.key !== key) return ''
  return current.dir === 'asc' ? '▲' : '▼'
}
