// SPDX-License-Identifier: AGPL-3.0-only

import { emptyFilters, type Filters } from './types'

export interface FilterPreset {
  name: string
  filters: Filters
}

const STORAGE_KEY = 'mikroview-filter-presets'

function loadInitial(): FilterPreset[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    // Merge each stored preset over a fresh emptyFilters() so a preset
    // saved before a new filter field existed doesn't end up missing keys.
    return parsed
      .filter((p): p is FilterPreset => typeof p?.name === 'string' && typeof p?.filters === 'object')
      .map((p) => ({ name: p.name, filters: { ...emptyFilters(), ...p.filters } }))
  } catch {
    return []
  }
}

class PresetState {
  presets = $state<FilterPreset[]>(loadInitial())

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
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.presets))
    } catch {
      // storage unavailable -- presets just won't persist across reloads
    }
  }
}

export const presetState = new PresetState()
