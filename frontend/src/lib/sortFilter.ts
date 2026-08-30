// SPDX-License-Identifier: AGPL-3.0-only

// Shared plumbing for the docket's column sort/filter (#649): every
// column on Flags, Watchlist and the audit log sorts (click, reverse on
// a second click) and filters (a quiet dashed row beneath the heads),
// per docs/design/concepts/round-18/the-docket-opened.html. This module
// is the six lines that would otherwise be copied three times.

export type SortDir = 'asc' | 'desc'

// A blank filter matches everything -- an empty dashed input is "no
// opinion," not "match nothing."
export function matchesFilter(value: string, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  return value.toLowerCase().includes(q)
}

export function compareText(a: string, b: string, dir: SortDir): number {
  const cmp = a.localeCompare(b)
  return dir === 'asc' ? cmp : -cmp
}

export function compareNumeric(a: number, b: number, dir: SortDir): number {
  const cmp = a - b
  return dir === 'asc' ? cmp : -cmp
}
