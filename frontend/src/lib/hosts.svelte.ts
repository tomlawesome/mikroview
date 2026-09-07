// SPDX-License-Identifier: AGPL-3.0-only
//
// The host presence register (#1016): which hosts the syslog feed has
// shown, and what the operator has said about the quiet ones.
//
// The map derives its hosts from the live event buffer (zones.svelte.ts),
// which makes presence a property of the buffer rather than of the
// network -- a host that stops talking scrolls out and vanishes. The
// server keeps the record instead (internal/hosts), and this is the
// browser's view of it: "living means hosts come and go by what the
// syslog feed shows. A host that stops talking is not removed: it goes
// quiet and turns grey."
//
// Nothing here draws anything yet. The map's own use of this lands with
// the drawing round.
import {
  deleteHostMark,
  fetchHosts,
  putHostMark,
  type Host,
  type HostMarkKind,
} from './api'

/**
 * How long a host may be silent before the map calls it quiet, when the
 * server has not said otherwise.
 *
 * 24 hours, owner-ratified 2026-09-07. This was ten minutes, chosen
 * before anything drew it, on the reasoning that a machine switched off
 * before lunch should have greyed out by the time anybody looked. The
 * owner's correction was that ten minutes is not evidence of anything --
 * a laptop with its lid shut over lunch has not gone away -- so presence
 * is only worth drawing at the scale of a working day.
 *
 * It is a default, not the rule: the figure is configurable
 * (`baseline.hostQuietAfter`) and the configured one arrives with the
 * off-baseline document, on `baselineState.hostQuietAfterMs`. presenceOf
 * takes the window as an argument precisely so a caller can pass that
 * instead of this.
 */
export const HOST_QUIET_AFTER_MS = 24 * 60 * 60_000

/** What the map should render a host as. */
export type HostPresence = 'live' | 'quiet' | 'intended' | 'dismissed'

/**
 * presenceOf answers what one host is right now, from the host, the
 * current time and the quiet threshold -- pure, so the map can recompute
 * it on a clock tick without asking the server anything.
 *
 * The order is the point:
 *
 *  - `dismissed` first, unconditionally. A dismissal is "take this off
 *    my map", and the server takes it back by itself the moment the host
 *    speaks again (internal/hosts.Register.Observe), so a dismissed host
 *    that is still dismissed has genuinely not been heard from.
 *  - `intended` only while the host is *also* quiet. Saying a machine is
 *    off on purpose is a statement about its silence, not a permanent
 *    label: while it is talking it is simply live, and the explanation
 *    is waiting for the next silence rather than colouring a working
 *    host.
 *  - then `quiet` by the clock, else `live`.
 */
export function presenceOf(host: Host, now: number, quietAfterMs: number): HostPresence {
  if (host.mark?.kind === 'dismissed') return 'dismissed'
  const lastSeen = Date.parse(host.lastSeen)
  // An unparseable stamp is treated as quiet rather than live: claiming
  // a host is live is a positive claim, and a broken stamp is no
  // evidence for it.
  const quiet = Number.isNaN(lastSeen) || now - lastSeen > quietAfterMs
  if (host.mark?.kind === 'intended') return quiet ? 'intended' : 'live'
  return quiet ? 'quiet' : 'live'
}

class HostsState {
  hosts = $state<Host[]>([])
  /** Last write's failure, shown inline wherever the mark is offered. */
  error = $state<string | null>(null)

  async refresh() {
    try {
      this.hosts = await fetchHosts()
    } catch {
      // Absence reads as "nothing registered", which is also the honest
      // state while the register cannot be read -- dark stays dark, the
      // same way coverageState.refresh swallows its own failure.
    }
  }

  byKey = $derived.by(() => new Map(this.hosts.map((h) => [h.key, h])))

  /** mark says what a quiet host is. reason is required for 'intended'. */
  async mark(key: string, kind: HostMarkKind, reason = ''): Promise<boolean> {
    this.error = null
    const res = await putHostMark(key, kind, reason)
    if (typeof res === 'string') {
      this.error = res
      return false
    }
    await this.refresh()
    return true
  }

  /** unmark takes the statement back, whichever kind it was. */
  async unmark(key: string): Promise<boolean> {
    this.error = null
    const err = await deleteHostMark(key)
    if (err) {
      this.error = err
      return false
    }
    await this.refresh()
    return true
  }
}

export const hostsState = new HostsState()
