// SPDX-License-Identifier: AGPL-3.0-only
//
// The browser's view of the baseline line register (#1016, round 49):
// which lines are off the established pattern today.
//
// Same split as hosts.svelte.ts next door -- the pure half is
// baseline.ts, which is where the predicates and the document shape
// live and where the unit tests point. This half holds the fetched
// document as state and knows how to re-read it.
//
// Nothing here draws anything. The map's own use of this lands with the
// drawing round.
import {
  deleteBaselineExpected,
  fetchOffBaseline,
  putBaselineExpected,
  type OffBaselineResponse,
} from './api'
import { EMPTY_OFF_BASELINE, type OffBaseline, type OffBaselineLine } from './baseline'

/**
 * How long a host may be silent before the map calls it quiet, when the
 * server has not said otherwise yet.
 *
 * Kept in step with config.DefaultHostQuietAfter on the server and with
 * hosts.svelte.ts's own constant; the server's configured figure
 * replaces it as soon as the first fetch lands.
 */
const DEFAULT_HOST_QUIET_AFTER_MS = 24 * 60 * 60_000

class BaselineState {
  /**
   * Today's off-baseline lines. Empty until the first fetch lands, and
   * empty again if a fetch fails -- "nothing is unusual" is the honest
   * picture while the truth is unknown, and the alternative would
   * brighten the whole map on a transient error.
   */
  off = $state<OffBaseline>(EMPTY_OFF_BASELINE)

  /**
   * The server's configured host quiet window, in milliseconds.
   *
   * It arrives on the off-baseline document rather than on its own
   * request -- see OffBaselineResponse for why -- and is held here so
   * the map has one place to read it from, whichever surface is drawing.
   */
  hostQuietAfterMs = $state<number>(DEFAULT_HOST_QUIET_AFTER_MS)

  /** Last write's failure, shown inline wherever `expected` is offered. */
  error = $state<string | null>(null)

  /** How many lines are off-baseline today -- the header's `⟡ N`. */
  count = $derived(this.off.count)

  async refresh() {
    try {
      const res: OffBaselineResponse = await fetchOffBaseline()
      this.off = {
        config: res.config,
        generatedAt: res.generatedAt,
        count: res.count,
        lines: res.lines,
      }
      // Guarded rather than trusted: a zero or missing window would make
      // the map call every host quiet the moment it was heard from.
      if (typeof res.hostQuietAfterMs === 'number' && res.hostQuietAfterMs > 0) {
        this.hostQuietAfterMs = res.hostQuietAfterMs
      }
    } catch {
      // Absence reads as "nothing off-baseline", which is also the honest
      // state while the register cannot be read -- the same choice
      // hostsState.refresh and coverageState.refresh both make.
    }
  }

  /**
   * expected says a line is meant to be there. The reason is required;
   * the server refuses an empty one and the refusal lands in `error`.
   */
  async expected(key: string, reason: string): Promise<boolean> {
    this.error = null
    const res = await putBaselineExpected(key, reason)
    if (typeof res === 'string') {
      this.error = res
      return false
    }
    await this.refresh()
    return true
  }

  /** unexpected takes the statement back. */
  async unexpected(key: string): Promise<boolean> {
    this.error = null
    const err = await deleteBaselineExpected(key)
    if (err) {
      this.error = err
      return false
    }
    await this.refresh()
    return true
  }

  /** The lines behind one drawn element, for its card. */
  matching(pred: (l: OffBaselineLine) => boolean): OffBaselineLine[] {
    return this.off.lines.filter(pred)
  }
}

export const baselineState = new BaselineState()
