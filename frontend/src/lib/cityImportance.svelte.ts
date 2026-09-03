// SPDX-License-Identifier: AGPL-3.0-only

import { DEFAULT_IMPORTANCE_READING, type ImportanceReading } from './city/importance'

// Which of the city's two importance readings sets a building's plinth
// height (#867): depended-on (the default) or watched. Its own small
// module, matching how theme/colorway/retention/presets/group each get
// one rather than growing appState -- see groupMode.svelte.ts's own
// doc comment for the reasoning.
//
// Persisted per browser like those: which reading someone wants the
// skyline to answer is a standing preference, not something that
// should reset to the default on every reload.
const STORAGE_KEY = 'mikroview:city-importance'

function loadInitial(): ImportanceReading {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'depended-on' || v === 'watched') return v
  } catch {
    // Private browsing and blocked storage both throw here; the city
    // still draws, just unpersisted.
  }
  return DEFAULT_IMPORTANCE_READING
}

class CityImportanceState {
  reading = $state<ImportanceReading>(loadInitial())

  toggle() {
    this.set(this.reading === 'depended-on' ? 'watched' : 'depended-on')
  }

  set(value: ImportanceReading) {
    this.reading = value
    try {
      localStorage.setItem(STORAGE_KEY, value)
    } catch {
      // As above -- a preference that cannot be saved still applies for
      // this session.
    }
  }
}

export const cityImportanceState = new CityImportanceState()
