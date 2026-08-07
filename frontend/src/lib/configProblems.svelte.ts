// SPDX-License-Identifier: AGPL-3.0-only

export type ConfigProblem = {
  code: string
  key: string
  message: string
  applied?: string
  remediation?: string
}

// Configuration problems found at startup, where mikroview substituted a
// safe default for a value the operator set.
//
// This is surfaced in the app rather than only in the log because a
// startup log line is seen once, by whoever ran `docker compose up`, and
// never again -- which is not good enough for a setting the operator
// believes is in effect. Clamping a value is only defensible because
// this banner exists; if it is ever removed, the backend rules should go
// back to refusing startup instead.
//
// The endpoint is admin-gated server-side and 403s for anyone else, so a
// non-admin simply gets an empty list here and renders nothing. The
// filtering is not this file's job and must not be moved here.
class ConfigProblemsState {
  problems = $state<ConfigProblem[]>([])
  dismissed = $state(false)
  private loaded = false

  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    try {
      const res = await fetch('/api/config/problems')
      if (!res.ok) return // 403 for non-admins is expected and silent
      const data = await res.json()
      this.problems = Array.isArray(data.problems) ? data.problems : []
    } catch {
      // A failed fetch must never break the page. The log and
      // -validate-config remain the operator's full-detail channels.
    }
  }

  get hasProblems() {
    return this.problems.length > 0
  }
}

export const configProblemsState = new ConfigProblemsState()
