// SPDX-License-Identifier: AGPL-3.0-only

export type Colorway = 'signal' | 'pulse' | 'nebula' | 'frequency' | 'mono'

export const COLORWAYS: { id: Colorway; label: string; swatch: string }[] = [
  { id: 'signal', label: 'Signal', swatch: '#4d9fff' },
  { id: 'pulse', label: 'Pulse', swatch: '#f472b6' },
  { id: 'nebula', label: 'Nebula', swatch: '#a78bfa' },
  { id: 'frequency', label: 'Frequency', swatch: '#22d3ee' },
  { id: 'mono', label: 'Mono', swatch: '#94a3b8' },
]

const STORAGE_KEY = 'mikroview-colorway'

function loadInitial(): Colorway {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (COLORWAYS.some((c) => c.id === v)) return v as Colorway
  } catch {
    // ignore
  }
  return 'signal'
}

// The accent color -- see the [data-colorway] rules in app.css. This is
// the only theme axis left: the light and system themes were removed
// wholesale in #708, since round 30 is dark throughout.
class ColorwayState {
  pref = $state<Colorway>(loadInitial())

  apply() {
    document.documentElement.setAttribute('data-colorway', this.pref)
    try {
      localStorage.setItem(STORAGE_KEY, this.pref)
    } catch {
      // storage unavailable -- won't persist across reloads
    }
  }

  set(id: Colorway) {
    this.pref = id
    this.apply()
  }
}

export const colorwayState = new ColorwayState()
