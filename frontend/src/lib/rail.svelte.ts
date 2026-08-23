// SPDX-License-Identifier: AGPL-3.0-only

export type RailState = 'full' | 'icons' | 'docked'
export type RailDensity = 'full' | 'icons'

const STORAGE_KEY = 'mikroview-rail'

// Full at >=1280px, icons below -- the record's defaults. Docked is never
// a default, only ever something the operator chose.
const FULL_FROM_PX = 1280

function defaultState(): RailState {
  try {
    return window.matchMedia(`(min-width: ${FULL_FROM_PX}px)`).matches ? 'full' : 'icons'
  } catch {
    return 'full'
  }
}

type Stored = { state: RailState; density: RailDensity }

// The wording the record uses for both the density control's aria-label
// ("Show icons only" / "Show icons and text") and the announcements
// ("navigation restored -- icons only"), kept in one place so the two
// cannot drift apart.
export function describe(d: RailDensity): string {
  return d === 'icons' ? 'icons only' : 'icons and text'
}

// One preference, three values (full - icons - docked), per the record.
// `density` is what that record calls "the undocked half of it": the
// density to come back to, kept alongside so a docked rail still knows
// which state restoring means. It is not a second preference -- nothing
// selects it independently of `state`.
function loadInitial(): Stored {
  const fallback = defaultState()
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { state: fallback, density: fallback === 'docked' ? 'full' : fallback }
    const parsed = JSON.parse(raw) as Partial<Stored>
    const state: RailState =
      parsed.state === 'full' || parsed.state === 'icons' || parsed.state === 'docked' ? parsed.state : fallback
    const density: RailDensity =
      parsed.density === 'full' || parsed.density === 'icons'
        ? parsed.density
        : state === 'docked'
          ? 'full'
          : state
    return { state, density }
  } catch {
    return { state: fallback, density: fallback === 'docked' ? 'full' : fallback }
  }
}

const initial = loadInitial()

class RailPref {
  // Read synchronously at module load, before the first render, so the
  // rail never paints in one state and then jumps to another.
  state = $state<RailState>(initial.state)
  density = $state<RailDensity>(initial.density)

  // Transient by design. The handle "never writes the preference -- it
  // only re-applies the undocked half of it", so a restored rail is
  // undocked for this session while the stored state stays `docked`, and
  // a reload docks again. Only the footer selects a state; that is the
  // owner's verdict from round 2 ("the handle restores the persistent
  // state; states are selected in the footer only"), which replaced an
  // earlier drawer-and-pin model.
  restored = $state(false)

  // The rail's own scrollTop across a dock/restore round trip, so
  // restoring returns "same density, same scroll". In memory only: it
  // describes this session's scroll position, not a preference.
  scrollTop = 0

  // Read by the live region in App.svelte rather than one inside the
  // rail: docking unmounts the rail, and a live region that disappears
  // in the same tick as the change it is announcing announces nothing.
  announcement = $state('')

  // "Docking returns focus to the handle" -- but the handle also mounts
  // on an ordinary page load when the stored state is docked, and
  // focusing it then would take focus off the page for no reason. This
  // marks the one case that earns it, and the handle clears it on use.
  justDocked = $state(false)

  // What the rail actually renders as right now, as opposed to what is
  // stored. Everything in the UI keys off this.
  get effective(): RailState {
    if (this.state === 'docked' && !this.restored) return 'docked'
    return this.state === 'docked' ? this.density : this.state
  }

  get isDocked(): boolean {
    return this.effective === 'docked'
  }

  private persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ state: this.state, density: this.density }))
    } catch {
      // storage unavailable (private browsing, etc.) -- the rail still
      // works, it just forgets between sessions
    }
  }

  /** Footer: switch between the two undocked densities. Writes. */
  setDensity(next: RailDensity) {
    this.density = next
    this.state = next
    this.restored = false
    this.persist()
    this.announcement = `Navigation — ${describe(next)}`
  }

  /** Footer: hide the rail to the handle. Writes. */
  dock() {
    if (this.state !== 'docked') this.density = this.state
    this.state = 'docked'
    this.restored = false
    this.justDocked = true
    this.persist()
    this.announcement = 'Navigation docked'
  }

  /** The handle: re-apply the undocked half. Deliberately does not write. */
  restore() {
    this.restored = true
    this.announcement = `Navigation restored — ${describe(this.density)}`
  }
}

export const railPref = new RailPref()
