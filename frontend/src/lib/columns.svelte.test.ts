// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { COLUMNS, PINNED_COLUMNS, columnState } from './columns.svelte'

// columnState is a module-level singleton (shared across every test file
// that imports it), so each test here restores it to the all-visible
// default rather than leaving a toggle to leak into whatever test runs
// next -- the same hygiene this file's neighbours already give
// appState/flagsState/etc. in their own beforeEach blocks.
function resetVisibility() {
  columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
  try {
    localStorage.removeItem('mikroview-column-visibility-v1')
  } catch {
    // unavailable storage -- nothing to clear
  }
}

beforeEach(resetVisibility)
afterEach(resetVisibility)

describe('column visibility (#729)', () => {
  it('defaults every column to visible -- the owner ruling keeps the shipped default at all fifteen', () => {
    for (const col of COLUMNS) {
      expect(columnState.isColumnVisible(col.key)).toBe(true)
    }
    expect(columnState.visibleColumns).toEqual(COLUMNS)
  })

  it('toggles an ordinary column off and back on', () => {
    columnState.toggleColumn('device')
    expect(columnState.isColumnVisible('device')).toBe(false)
    expect(columnState.visibleColumns.some((c) => c.key === 'device')).toBe(false)

    columnState.toggleColumn('device')
    expect(columnState.isColumnVisible('device')).toBe(true)
  })

  it('refuses to hide a pinned column (time, rule)', () => {
    for (const key of PINNED_COLUMNS) {
      columnState.toggleColumn(key)
      expect(columnState.isColumnVisible(key)).toBe(true)
    }
  })

  it('persists the choice across reloads the way column widths already do -- a fresh read from storage', () => {
    columnState.toggleColumn('mac')
    columnState.toggleColumn('nat')

    const raw = localStorage.getItem('mikroview-column-visibility-v1')
    expect(raw).toBeTruthy()
    const parsed = JSON.parse(raw as string)
    expect(parsed.mac).toBe(false)
    expect(parsed.nat).toBe(false)
  })

  it('ignores a stored value that tries to hide a pinned column -- loadInitialVisibility, exercised via a fresh module load', async () => {
    // columnState is constructed once, at module import, from whatever
    // storage held at that moment (loadInitialVisibility) -- writing to
    // storage after the fact (as the rest of this file's tests do, via
    // toggleColumn) never re-reads it. Proving the *load-time* guard
    // itself needs a genuinely fresh module instance, via
    // vi.resetModules() plus a new dynamic import, rather than the
    // already-constructed singleton every other test in this file shares.
    const stored: Record<string, boolean> = Object.fromEntries(COLUMNS.map((col) => [col.key, true]))
    stored.time = false
    stored.rule = false
    stored.device = false
    localStorage.setItem('mikroview-column-visibility-v1', JSON.stringify(stored))

    vi.resetModules()
    const fresh = await import('./columns.svelte')

    // A hand-edited (or pre-pinning) stored value marked Time and Rule
    // hidden -- the load guard must not trust that for a pinned key.
    expect(fresh.columnState.isColumnVisible('time')).toBe(true)
    expect(fresh.columnState.isColumnVisible('rule')).toBe(true)
    // Everything else in the stored value is honoured as saved.
    expect(fresh.columnState.isColumnVisible('device')).toBe(false)
  })

  // gridTemplate joins tokens with a single space, but a flexible column's
  // own token ("minmax(140px, 1fr)") also contains a space -- counting
  // tracks with a naive `.split(' ')` would overcount by one for each of
  // those, so this counts CSS tracks instead (a bare px length, or a whole
  // minmax(...) call).
  function trackCount(template: string): number {
    return (template.match(/\d+px|minmax\([^)]*\)/g) ?? []).length
  }

  it('keeps the grid template in step with only the visible columns', () => {
    columnState.toggleColumn('device')
    columnState.toggleColumn('chain')
    expect(trackCount(columnState.gridTemplate)).toBe(COLUMNS.length - 2)
  })

  it('leaves a usable table when every optional column is turned off -- only the two pinned remain', () => {
    for (const col of COLUMNS) {
      if (!PINNED_COLUMNS.has(col.key)) columnState.toggleColumn(col.key)
    }
    expect(columnState.visibleColumns.map((c) => c.key).sort()).toEqual([...PINNED_COLUMNS].sort())
    expect(trackCount(columnState.gridTemplate)).toBe(PINNED_COLUMNS.size)
  })
})
