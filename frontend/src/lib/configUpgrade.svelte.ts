// SPDX-License-Identifier: AGPL-3.0-only

import { dismissConfigUpgrade, fetchConfigUpgrade } from './api'
import type { ConfigUpgradeSetting } from './configUpgrade'

// #1218's "N new settings are available" notice -- its own small state
// module, matching auditState's pattern: a thin reactive wrapper over
// one read-only fetch, plus (unlike auditState) the one mutation this
// screen has, dismissing the notice for whatever version the server
// reports itself running.
class ConfigUpgradeState {
  settings = $state<ConfigUpgradeSetting[]>([])
  dismissed = $state(false)
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
      this.dismissed = res.dismissed
      this.version = res.version
      this.error = null
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
    } finally {
      this.loaded = true
    }
  }

  async dismiss() {
    try {
      const res = await dismissConfigUpgrade()
      this.dismissed = res.dismissed
      this.version = res.version
      this.error = null
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
    }
  }
}

export const configUpgradeState = new ConfigUpgradeState()
