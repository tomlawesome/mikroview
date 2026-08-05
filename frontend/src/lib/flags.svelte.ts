import { clearFlag, fetchFlags } from './api'
import type { Flag } from './types'

// Behavioral flags (port scans, activity spikes, critical-port attempts,
// global volume spikes -- see internal/detect) raised server-side and
// reviewed/cleared by a human here. Kept as its own small module rather
// than folded into appState, matching how theme/colorway/retention/
// presets each get their own state module in this codebase.
class FlagsState {
  list = $state<Flag[]>([])
  open = $state(false)

  activeCount = $derived(this.list.filter((f) => !f.cleared).length)

  async refresh() {
    this.list = await fetchFlags()
  }

  // Updates the flag locally the instant the user clicks Clear, rather
  // than waiting on a network round-trip before anything visible
  // happens (the old code awaited clearFlag then a second full refresh()
  // -- two serial round-trips per click, whose completion queued behind
  // whatever else the main thread was doing under a live-traffic flood).
  // App.svelte's existing 5s poll reconciles this against the server
  // regardless, so there's no correctness gap from skipping the extra
  // refetch here -- only a failed clearFlag call needs an explicit
  // revert, since otherwise the flag would sit incorrectly "cleared"
  // until that poll ran.
  async clear(id: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const wasCleared = flag.cleared
    const previousClearedAt = flag.clearedAt
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      await clearFlag(id)
    } catch (err) {
      flag.cleared = wasCleared
      flag.clearedAt = previousClearedAt
      throw err
    }
  }
}

export const flagsState = new FlagsState()
