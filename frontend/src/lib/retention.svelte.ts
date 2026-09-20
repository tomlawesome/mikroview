// SPDX-License-Identifier: AGPL-3.0-only

import { preferencesState } from './preferences.svelte'

// #1283: was its own localStorage key ('mikroview-max-age-seconds').
const PREFS_KEY = 'retention'

// How long an event stays visible in the live table after it's received,
// independent of the backend's Retention/MaxEvents config (which governs
// the whole store, not what's currently displayed). null means "no limit"
// -- the existing MAX_CLIENT_EVENTS/MAX_RENDERED_ROWS caps still apply.
//
// Deliberately goes down to single-digit seconds: the point of this
// setting is to keep a fast-scrolling live view human-readable without
// having to hit Pause -- e.g. "keep the last 10s of connections visible,
// let anything older than that scroll off" -- so minute-level-only
// granularity would miss the actual use case.
export const MAX_AGE_OPTIONS: { value: number | null; label: string }[] = [
  { value: 5, label: 'Last 5s' },
  { value: 10, label: 'Last 10s' },
  { value: 30, label: 'Last 30s' },
  { value: 60, label: 'Last 1 min' },
  { value: 300, label: 'Last 5 min' },
  { value: 900, label: 'Last 15 min' },
  { value: 3600, label: 'Last hour' },
  { value: null, label: 'No limit' },
]

function sanitize(value: unknown): number | null {
  if (value === null || value === undefined) return null
  const n = Number(value)
  return Number.isFinite(n) && n > 0 ? n : null
}

class RetentionState {
  maxAgeSeconds = $state<number | null>(null)

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.maxAgeSeconds = sanitize(value)
    })
  }

  set(value: number | null) {
    this.maxAgeSeconds = value
    preferencesState.set(PREFS_KEY, value)
  }
}

export const retentionState = new RetentionState()
