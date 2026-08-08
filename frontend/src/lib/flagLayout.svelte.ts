// SPDX-License-Identifier: AGPL-3.0-only

// How many columns the active-flags card grid renders as (issue #199).
// Same small-module-per-preference shape as colorway.svelte.ts/
// theme.svelte.ts -- persisted per browser, not synced anywhere.
export type FlagColumns = 1 | 2 | 3

const STORAGE_KEY = 'mikroview-flag-columns'

function loadInitial(): FlagColumns {
  try {
    const v = Number(localStorage.getItem(STORAGE_KEY))
    if (v === 1 || v === 2 || v === 3) return v
  } catch {
    // ignore
  }
  return 1
}

class FlagLayoutState {
  columns = $state<FlagColumns>(loadInitial())

  set(n: FlagColumns) {
    this.columns = n
    try {
      localStorage.setItem(STORAGE_KEY, String(n))
    } catch {
      // storage unavailable -- won't persist across reloads
    }
  }
}

export const flagLayoutState = new FlagLayoutState()
