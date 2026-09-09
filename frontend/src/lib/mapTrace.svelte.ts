// SPDX-License-Identifier: AGPL-3.0-only
//
// The event trace's own state (#1018, round 53): one logged line, drawn
// as the one hop the router knows about.
//
// The map draws only what is here. Nothing infers a second hop, and a
// refused line stops at the router -- what would have happened after it
// is not in any log, so the map says "would have reached" in words on a
// dashed ghost rather than drawing a path that never existed.
import { fetchTrace, type TraceRequest, type TraceResponse } from './api'
import type { FirewallEvent } from './types'

class MapTraceState {
  /** What was asked for, held so the crumb can say so on a miss. */
  request = $state<TraceRequest | null>(null)

  /** The server's answer; null while nothing is traced. */
  result = $state<TraceResponse | null>(null)

  /** True between the ask and the answer, so the map does not flicker. */
  loading = $state(false)

  /** True while a trace is on the map, found or honestly missed. */
  active = $derived(this.request !== null)

  event = $derived<FirewallEvent | null>(this.result?.event ?? null)

  /**
   * 'accepted' | 'refused' | null. Null covers a log, mark or NAT line,
   * which says which kind of rule logged the packet and not whether it
   * passed: the map draws no verdict colour rather than picking one.
   */
  verdict = $derived(this.result?.verdict ?? null)

  /** The half the line came in on, and the half it left on. */
  inIface = $derived(this.event?.inInterface ?? '')
  outIface = $derived(this.event?.outInterface ?? '')

  /** `and 13 more like it` -- already the count without this one. */
  like = $derived(this.result?.like ?? 0)
  srcSeen = $derived(this.result?.srcSeen ?? 0)
  dstReached = $derived(this.result?.dstReached ?? 0)

  async open(req: TraceRequest) {
    this.request = req
    this.result = null
    this.loading = true
    const asked = JSON.stringify(req)
    try {
      const res = await fetchTrace(req)
      // Two traces opened in quick succession: only the last one's
      // answer is allowed to land, so the crumb and the drawing are
      // never about different lines.
      if (asked !== JSON.stringify(this.request)) return
      this.result = res
    } catch {
      if (asked !== JSON.stringify(this.request)) return
      // A failed read is a miss, drawn as a miss. The alternative --
      // leaving the previous trace up -- would put one line's crumb over
      // another line's drawing.
      this.result = { found: false, like: 0, srcSeen: 0, dstReached: 0 }
    } finally {
      if (asked === JSON.stringify(this.request)) this.loading = false
    }
  }

  clear() {
    this.request = null
    this.result = null
    this.loading = false
  }
}

export const mapTraceState = new MapTraceState()
