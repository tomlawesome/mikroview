// SPDX-License-Identifier: AGPL-3.0-only
//
// Which altitude stop the slider remembers across visits (#869): a
// standing preference like cityImportance.svelte.ts's reading -- its
// own small module rather than growing appState.
import { ALTITUDE_LABELS, type AltitudeLabel } from './altitude'

const STORAGE_KEY = 'mikroview:topography-altitude'
const DEFAULT_STOP: AltitudeLabel = 'city'

function loadInitial(): AltitudeLabel {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v && (ALTITUDE_LABELS as readonly string[]).includes(v)) return v as AltitudeLabel
  } catch {
    // Private browsing and blocked storage both throw here; the slider
    // still works, just unpersisted.
  }
  return DEFAULT_STOP
}

class AltitudeStopState {
  stop = $state<AltitudeLabel>(loadInitial())

  set(value: AltitudeLabel) {
    this.stop = value
    try {
      localStorage.setItem(STORAGE_KEY, value)
    } catch {
      // As above.
    }
  }
}

export const altitudeStopState = new AltitudeStopState()
