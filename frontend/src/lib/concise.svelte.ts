// SPDX-License-Identifier: AGPL-3.0-only

// Whether the live view collapses repeats of the same connection into
// one row (#341). Its own small module, matching how theme/colorway/
// retention/presets each get one rather than growing appState.
//
// Persisted per browser, like the column widths and the retention
// window: which way an operator prefers to read their own traffic is a
// preference, not session state, and having it reset on every reload
// would make it feel like a mode rather than a setting.
//
// Off by default. The live view's job is one row per event; this is an
// option on top of it, not a new mode it starts in.

const STORAGE_KEY = 'mikroview:concise'

function loadInitial(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    // Private browsing and blocked storage both throw here; the feature
    // works fine unpersisted, so this is not worth surfacing.
    return false
  }
}

class ConciseState {
  enabled = $state(loadInitial())

  toggle() {
    this.set(!this.enabled)
  }

  set(value: boolean) {
    this.enabled = value
    try {
      localStorage.setItem(STORAGE_KEY, value ? '1' : '0')
    } catch {
      // As above -- a preference that cannot be saved still applies for
      // this session.
    }
  }
}

export const conciseState = new ConciseState()
