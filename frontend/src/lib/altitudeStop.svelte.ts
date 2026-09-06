// SPDX-License-Identifier: AGPL-3.0-only
//
// Which altitude stop the slider remembers across visits (#869): a
// standing preference like cityImportance.svelte.ts's reading -- its
// own small module rather than growing appState.
import { ALTITUDE_LABELS, type AltitudeLabel } from './altitude'

const STORAGE_KEY = 'mikroview:topography-altitude'
// #979: one stop further out than 'city' -- the axis' own centre and
// former default (#869) -- so the card opens with the whole estate in
// view. 'city' is already the widest of City's own stops (the whole
// estate, isometric); the only stop further out is 'zones', the 2D
// map's own widest -- so this default now opens on the flat map, not
// the isometric city.
const DEFAULT_STOP: AltitudeLabel = 'zones'

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
