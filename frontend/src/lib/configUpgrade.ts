// SPDX-License-Identifier: AGPL-3.0-only

// Types for GET/POST /api/config/upgrade (#1218) -- kept in their own
// module rather than lib/types.ts, which another change in flight also
// touches.

/** One optional setting this build understands that config.yaml does not set. */
export interface ConfigUpgradeSetting {
  key: string
  /** Ready-to-paste YAML, comment lines and all, already "#"-commented. */
  block: string
}

export interface ConfigUpgradeResponse {
  /** This build's own version string -- echoed back unchanged on dismiss. */
  version: string
  settings: ConfigUpgradeSetting[]
  /** True once an admin has dismissed the notice for `version` specifically. */
  dismissed: boolean
}
