const STORAGE_KEY = 'mikroview-max-age-minutes'

// How long an event stays visible in the live table after it's received,
// independent of the backend's Retention/MaxEvents config (which governs
// the whole store, not what's currently displayed). null means "no limit"
// -- the existing MAX_CLIENT_EVENTS/MAX_RENDERED_ROWS caps still apply.
export const MAX_AGE_OPTIONS: { value: number | null; label: string }[] = [
  { value: 1, label: 'Last 1 min' },
  { value: 5, label: 'Last 5 min' },
  { value: 15, label: 'Last 15 min' },
  { value: 60, label: 'Last hour' },
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
  maxAgeMinutes = $state<number | null>(loadInitial())

  set(value: number | null) {
    this.maxAgeMinutes = value
    try {
      localStorage.setItem(STORAGE_KEY, value === null ? 'null' : String(value))
    } catch {
      // storage unavailable -- setting just won't persist across reloads
    }
  }
}

export const retentionState = new RetentionState()
