import { fetchExclusions, removeExclusion } from './api'
import type { Exclusion } from './types'

// Permanently-excluded (Type, Target) pairs (see internal/flags.Store.
// Exclude) -- admin-only, mirrors detectorSettings.svelte.ts's shape.
// This is the "undo a mistake" surface for Flags.svelte's "Clear and
// never flag this again" action: excluding is one-click and immediate,
// but reviewing/reversing it lives here rather than being
// unrecoverable.
class ExclusionsState {
  list = $state<Exclusion[]>([])

  async refresh() {
    this.list = await fetchExclusions()
  }

  async remove(id: string) {
    const previous = this.list
    this.list = this.list.filter((e) => e.id !== id)
    try {
      await removeExclusion(id)
    } catch (err) {
      this.list = previous
      throw err
    }
  }
}

export const exclusionsState = new ExclusionsState()
