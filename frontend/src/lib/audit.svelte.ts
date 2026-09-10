// SPDX-License-Identifier: AGPL-3.0-only

import { fetchAuditLog } from './api'
import type { AuditEntry } from './types'

// Admin-only view of the admin-action accountability log (issue #112) --
// its own small state module, matching entitiesState/tokensState's
// pattern: a thin reactive wrapper over the one read-only API call, with
// no client-side mutation methods since this log is never edited from
// the UI.
class AuditState {
  list = $state<AuditEntry[]>([])
  hasMore = $state(false)
  // #1089: false until a fetch resolves (success or failure) -- an empty
  // `list` before this is true is "not fetched yet", not "no admin
  // actions recorded", same distinction flagsState.loaded draws for
  // flags. AuditLog.svelte's empty message reads this so a failed fetch
  // can no longer render as a quietly empty log.
  loaded = $state(false)
  error = $state<string | null>(null)

  async refresh() {
    try {
      const res = await fetchAuditLog()
      this.list = res.entries
      this.hasMore = res.hasMore
      this.error = null
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
    } finally {
      this.loaded = true
    }
  }
}

export const auditState = new AuditState()
