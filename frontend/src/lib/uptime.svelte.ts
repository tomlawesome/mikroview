// SPDX-License-Identifier: AGPL-3.0-only

import { fetchHealthz } from './api'

// How long the server has been running, for the toolbar (owner request,
// 2026-08-15: easily visible, not in a menu).
//
// The server reports uptimeSeconds once; the client counts on from there
// locally rather than polling -- uptime advances at exactly one second
// per second, so a fetch per tick would buy nothing. The baseline is
// re-fetched every RESYNC_MS because local counting has one honest
// failure mode: a server restart resets the real uptime while the local
// counter keeps climbing from the old baseline. A minute of staleness
// after a restart is acceptable for a status readout; showing "4d 2h"
// against a server that restarted an hour ago indefinitely would not be.
const RESYNC_MS = 60_000
const TICK_MS = 1_000

class UptimeState {
  seconds = $state<number | null>(null)
  private baseSeconds = 0
  private baseAtMs = 0
  private started = false

  start() {
    if (this.started) return
    this.started = true
    void this.resync()
    setInterval(() => {
      if (this.baseAtMs === 0) return
      this.seconds = this.baseSeconds + Math.round((Date.now() - this.baseAtMs) / 1000)
    }, TICK_MS)
    setInterval(() => void this.resync(), RESYNC_MS)
  }

  private async resync() {
    try {
      const healthz = await fetchHealthz()
      this.baseSeconds = healthz.uptimeSeconds
      this.baseAtMs = Date.now()
      this.seconds = healthz.uptimeSeconds
    } catch {
      // Unreachable server: leave the last known value ticking. The
      // connection indicator beside this readout is the component whose
      // job is to say the server is gone; a second voice would be noise.
    }
  }
}

export const uptimeState = new UptimeState()
