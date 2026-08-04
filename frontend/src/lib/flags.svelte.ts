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

  async clear(id: string) {
    await clearFlag(id)
    await this.refresh()
  }
}

export const flagsState = new FlagsState()
