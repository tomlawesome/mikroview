// SPDX-License-Identifier: AGPL-3.0-only

import { preferencesState } from './preferences.svelte'

export type Colorway = 'signal' | 'pulse' | 'nebula' | 'frequency' | 'mono'

export const COLORWAYS: { id: Colorway; label: string; swatch: string }[] = [
  { id: 'signal', label: 'Signal', swatch: '#4d9fff' },
  { id: 'pulse', label: 'Pulse', swatch: '#f472b6' },
  { id: 'nebula', label: 'Nebula', swatch: '#a78bfa' },
  { id: 'frequency', label: 'Frequency', swatch: '#22d3ee' },
  { id: 'mono', label: 'Mono', swatch: '#94a3b8' },
]

const DEFAULT_COLORWAY: Colorway = 'signal'
// #1283: was its own localStorage key ('mikroview-colorway').
const PREFS_KEY = 'colorway'

// The accent color -- see the [data-colorway] rules in app.css. This is
// the only theme axis left: the light and system themes were removed
// wholesale in #708, since round 30 is dark throughout.
class ColorwayState {
  pref = $state<Colorway>(DEFAULT_COLORWAY)

  constructor() {
    // Hydrated once the shared record has loaded -- until then (and for
    // a signed-out visitor) this stays the default, applied by
    // App.svelte's own $effect the moment it changes.
    preferencesState.register(PREFS_KEY, (value) => {
      this.pref = COLORWAYS.some((c) => c.id === value) ? (value as Colorway) : DEFAULT_COLORWAY
    })
  }

  apply() {
    document.documentElement.setAttribute('data-colorway', this.pref)
  }

  set(id: Colorway) {
    this.pref = id
    this.apply()
    preferencesState.set(PREFS_KEY, id)
  }
}

export const colorwayState = new ColorwayState()
