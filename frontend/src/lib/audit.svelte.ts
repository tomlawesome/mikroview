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

  async refresh() {
    const res = await fetchAuditLog()
    this.list = res.entries
    this.hasMore = res.hasMore
  }
}

export const auditState = new AuditState()
