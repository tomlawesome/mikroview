// SPDX-License-Identifier: AGPL-3.0-only

const STORAGE_KEY = 'mikroview-max-age-seconds'

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

function loadInitial(): number | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === null) return null
    if (v === 'null') return null
    const n = Number(v)
    return Number.isFinite(n) && n > 0 ? n : null
  } catch {
    return null
  }
}

class RetentionState {
  maxAgeSeconds = $state<number | null>(loadInitial())

  set(value: number | null) {
    this.maxAgeSeconds = value
    try {
      localStorage.setItem(STORAGE_KEY, value === null ? 'null' : String(value))
    } catch {
      // storage unavailable -- setting just won't persist across reloads
    }
  }
}

export const retentionState = new RetentionState()
