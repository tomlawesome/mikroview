// SPDX-License-Identifier: AGPL-3.0-only

export type ConfigProblem = {
  code: string
  key: string
  message: string
  applied?: string
  remediation?: string
  /**
   * 'warn' for a live finding that is not a setting being ignored --
   * today only history held on disk while off (#1354). Drawn apart from
   * the startup list, whose heading says "being ignored".
   */
  severity?: 'warn'
}

/** The live entry's code (internal/api's HistoryHeldWhileOffCode). */
export const HISTORY_HELD_WHILE_OFF = 'history-held-while-off'

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
    await this.fetchProblems()
  }

  // refresh asks again, for a change that can add or clear the live
  // history entry (#1354): turning history off or on, or deleting its
  // files. Only once something has loaded -- a session that never asked
  // (a non-admin's) has nothing to refresh and must not start asking.
  async refresh() {
    if (!this.loaded) return
    await this.fetchProblems()
  }

  private async fetchProblems() {
    try {
      const res = await fetch('/api/config/problems')
      if (!res.ok) return // 403 for non-admins is expected and silent
      const data = await res.json()
      const next: ConfigProblem[] = Array.isArray(data.problems) ? data.problems : []
      // Something new since the banner was hidden is shown again: the
      // hide was for what the admin had already read.
      const seen = new Set(this.problems.map((p) => p.code + p.key))
      if (next.some((p) => !seen.has(p.code + p.key))) this.dismissed = false
      this.problems = next
    } catch {
      // A failed fetch must never break the page. The log and
      // -validate-config remain the operator's full-detail channels.
    }
  }

  get hasProblems() {
    return this.problems.length > 0
  }

  /** The startup list: settings being ignored. */
  get ignored() {
    return this.problems.filter((p) => p.severity !== 'warn')
  }

  /** Live findings that are not a setting being ignored. */
  get warnings() {
    return this.problems.filter((p) => p.severity === 'warn')
  }

  // #1083, v0.6.0 pre-release audit Security stage: configProblemsState
  // was missed from the original batch. `loaded` is private and never
  // reset on its own, so ConfigProblemBanner -- which has no role check
  // of its own -- kept showing the previous admin's config diagnostics
  // for whoever signed in next on this tab.
  reset() {
    this.problems = []
    this.dismissed = false
    this.loaded = false
  }
}

export const configProblemsState = new ConfigProblemsState()
