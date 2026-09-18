// SPDX-License-Identifier: AGPL-3.0-only

import { fetchConfigUpgrade } from './api'
import type { ConfigUpgradeSetting } from './configUpgrade'

// #1218's "N new settings are available" notice -- its own small state
// module, matching auditState's pattern: a thin reactive wrapper over
// one read-only fetch. No dismiss mutation: the panel's close is a
// plain per-visit local toggle in ConfigUpgrade.svelte itself, not
// state worth persisting here (owner ruling: "Just have a close
// button. It's simple.").
class ConfigUpgradeState {
  settings = $state<ConfigUpgradeSetting[]>([])
  version = $state('')
  // False until a fetch resolves (success or failure) -- same
  // distinction auditState.loaded draws, so a failed fetch cannot read
  // as "nothing new", and a real "nothing new" cannot read as "still
  // loading".
  loaded = $state(false)
  error = $state<string | null>(null)

  async refresh() {
    try {
      const res = await fetchConfigUpgrade()
      this.settings = res.settings
      this.version = res.version
      this.error = null
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
    } finally {
      this.loaded = true
    }
  }
}

export const configUpgradeState = new ConfigUpgradeState()
