// SPDX-License-Identifier: AGPL-3.0-only

import { emptyFilters, type Filters } from './types'
import { preferencesState } from './preferences.svelte'

export interface FilterPreset {
  name: string
  filters: Filters
}

// #1283: the key this module owns in the shared preferences record --
// was its own localStorage key ('mikroview-filter-presets') before.
const PREFS_KEY = 'presets'

// Merges each stored preset over a fresh emptyFilters() so a preset
// saved before a new filter field existed doesn't end up missing keys.
// Same shape of guard loadInitial() applied reading straight out of
// localStorage; now applied to whatever the server hands back instead.
function sanitize(value: unknown): FilterPreset[] {
  if (!Array.isArray(value)) return []
  return value
    .filter((p): p is FilterPreset => typeof p?.name === 'string' && typeof p?.filters === 'object')
    .map((p) => ({ name: p.name, filters: { ...emptyFilters(), ...p.filters } }))
}

class PresetState {
  presets = $state<FilterPreset[]>([])

  constructor() {
    // Registered at import time; hydrated once the shared record has
    // loaded (see preferences.svelte.ts's ensureLoaded(), hooked to
    // sign-in in auth.svelte.ts) -- undefined (no `presets` key saved
    // yet) sanitizes to this module's own default, [].
    preferencesState.register(PREFS_KEY, (value) => {
      this.presets = sanitize(value)
    })
  }

  save(name: string, filters: Filters) {
    const trimmed = name.trim()
    if (!trimmed) return
    const next = this.presets.filter((p) => p.name !== trimmed)
    next.push({ name: trimmed, filters: { ...filters } })
    next.sort((a, b) => a.name.localeCompare(b.name))
    this.presets = next
    this.persist()
  }

  remove(name: string) {
    this.presets = this.presets.filter((p) => p.name !== name)
    this.persist()
  }

  private persist() {
    preferencesState.set(PREFS_KEY, this.presets)
  }
}

export const presetState = new PresetState()
