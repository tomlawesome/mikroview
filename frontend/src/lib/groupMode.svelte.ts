// SPDX-License-Identifier: AGPL-3.0-only

import { preferencesState } from './preferences.svelte'

// Whether the live view groups repeats of the same connection into one
// row (#341). Its own small module, matching how theme/colorway/
// retention/presets each get one rather than growing appState.
//
// Named groupMode rather than group to stay distinct from grouping.ts,
// which holds the grouping itself: that file decides what counts as the
// same connection, this one only records whether the operator has it
// switched on.
//
// Kept in the shared per-user preferences record (#1283), like the
// column widths and the retention window: which way an operator prefers
// to read their own traffic is a preference, not session state, and
// having it reset on every reload would make it feel like a mode rather
// than a setting.
//
// Off by default. The live view's job is one row per event; this is an
// option on top of it, not a new mode it starts in.

// #1283: was its own localStorage key ('mikroview:group', '1'/'0').
const PREFS_KEY = 'groupMode'

class GroupModeState {
  enabled = $state(false)

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.enabled = value === true
    })
  }

  toggle() {
    this.set(!this.enabled)
  }

  set(value: boolean) {
    this.enabled = value
    preferencesState.set(PREFS_KEY, value)
  }
}

export const groupModeState = new GroupModeState()
