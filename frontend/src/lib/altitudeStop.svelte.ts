// SPDX-License-Identifier: AGPL-3.0-only
//
// Which altitude stop the slider remembers across visits (#869): a
// standing preference kept in the shared per-user record (#1283), like
// retention -- its own small module rather than growing
// appState.
import { ALTITUDE_LABELS, type AltitudeLabel } from './altitude'
import { preferencesState } from './preferences.svelte'

// #1283: was its own localStorage key ('mikroview:topography-altitude').
const PREFS_KEY = 'altitudeStop'
const DEFAULT_STOP: AltitudeLabel = 'city'

function sanitize(value: unknown): AltitudeLabel {
  return typeof value === 'string' && (ALTITUDE_LABELS as readonly string[]).includes(value)
    ? (value as AltitudeLabel)
    : DEFAULT_STOP
}

class AltitudeStopState {
  stop = $state<AltitudeLabel>(DEFAULT_STOP)

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.stop = sanitize(value)
    })
  }

  set(value: AltitudeLabel) {
    this.stop = value
    preferencesState.set(PREFS_KEY, value)
  }
}

export const altitudeStopState = new AltitudeStopState()
