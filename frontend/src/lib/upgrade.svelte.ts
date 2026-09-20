// SPDX-License-Identifier: AGPL-3.0-only

// The upgrade notice's state (#1240, GET/POST /api/upgrade).
//
// After an upgrade there is exactly one thing MikroView cannot do for
// the operator: the setup wizard's pasted script changes between
// versions and the router does not update itself. This holds what the
// server says about that -- which version this install came from, and
// how much of the fleet is still on the old setup -- and the two rules
// that decide whether the line shows and whether `done` is offered.
//
// The rules live here rather than in the component so they can be read
// in one place and tested without rendering: they are the design ruling
// on the issue, not styling.

import { TITLES } from './setupsteps'

/** How the declared fleet stands against the current wizard (#1241). */
export interface UpgradeRouters {
  /** Routers not reporting the current setup -- including any that have never reported. */
  behind: number
  total: number
  /** Routers that have sent their setup page at all, whatever it said. */
  reported: number
}

export interface Upgrade {
  /** The build this install came from. Empty on a first install. */
  previous: string
  current: string
  noticedAt?: string
  /** True once an admin has pressed `done` on this upgrade, server-side, for every session. */
  acknowledged: boolean
  routers: UpgradeRouters
}

class UpgradeState {
  upgrade = $state<Upgrade | null>(null)
  private loaded = false

  // Its own fetch rather than lib/api.ts's helpers, the same way
  // configProblems.svelte.ts does: this is one GET and one POST for one
  // banner, and another module in flight owns that file. The POST
  // carries the CSRF header every mutating request in this app sends
  // (internal/api's csrfHeaderName).
  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    try {
      const res = await fetch('/api/upgrade')
      if (!res.ok) return
      this.upgrade = await res.json()
    } catch {
      // A failed fetch must never break the page: the boot log still
      // says what this build upgraded from.
    }
  }

  // acknowledge is the notice's `done`. It replaces the local answer
  // with the server's own rather than setting a flag here: what hides
  // the line is what the server now holds, which is also what the next
  // session and the next reload will read.
  async acknowledge() {
    try {
      const res = await fetch('/api/upgrade/acknowledge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
        body: '{}',
      })
      if (!res.ok) return
      this.upgrade = await res.json()
    } catch {
      // Same as above -- the line stays up, which is the safe failure:
      // it is still true.
    }
  }

  // show is the design ruling's four silences. No upgrade to speak of
  // (a first install, or a plain restart), one the operator has already
  // settled, and -- once routers report their own setup (#1241) -- a
  // fleet that has caught up on its own, which clears the line without
  // anyone having to press anything.
  get show(): boolean {
    const u = this.upgrade
    if (!u || !u.previous || u.previous === u.current) return false
    if (u.acknowledged) return false
    if (u.routers.total > 0 && u.routers.behind === 0) return false
    return true
  }

  // offersDone is the ruling's last paragraph. Where every router has
  // reported, the count is the truth and the line clears itself as they
  // catch up, so `done` has nothing to add and is not offered -- an
  // operator dismissing a line that names three routers still on the old
  // setup would be dismissing the work, not finishing it. Where some
  // router has never reported at all, MikroView cannot tell, and the
  // operator's own `done` remains the only way to settle it.
  get offersDone(): boolean {
    const r = this.upgrade?.routers
    if (!r) return false
    return r.behind === 0 || r.reported < r.total
  }

  // line is the notice's whole text, in the app's voice: lower case,
  // middle dots, the previous version always named (the current one is
  // already in the header). The count appears only once there is a
  // count to give.
  get line(): string {
    const u = this.upgrade
    if (!u) return ''
    const r = u.routers
    if (r.total > 0 && r.behind > 0) {
      return `upgraded from ${u.previous} · ${r.behind} of ${r.total} routers still on the old setup · paste ${TITLES.ca} again on each`
    }
    return `upgraded from ${u.previous} · paste ${TITLES.ca} again on each router`
  }
}

export const upgradeState = new UpgradeState()
