// SPDX-License-Identifier: AGPL-3.0-only

import {
  createDecommissionWatch,
  deleteDecommissionWatch,
  dismissDecommissionOffer,
  fetchDecommission,
  forceRemoveDecommissionGhost,
  undoDecommissionRetirement,
} from './api'
import type { DecommissionOffer, DecommissionWatch } from './types'

// The decommission surface's state (#460): what is being offered, and
// what is already being watched.
//
// One list, one refresh, mirroring watchlist.svelte.ts: a thin reactive
// wrapper over the API that re-reads the whole answer after every
// mutation rather than patching locally. That matters more here than
// elsewhere -- an offer and a ghost are the same object one decision
// apart, and the server is the only thing that knows which a segment is
// at any instant, so a locally patched list could draw a ghost the
// server has already retired.
class DecommissionsState {
  offers = $state<DecommissionOffer[]>([])
  watches = $state<DecommissionWatch[]>([])
  // #367's caveat, carried through from the API: false means at least
  // one router feeds events but never pushed its filter table, so a
  // broken ghost may be broken only because mikroview cannot see rather
  // than because nothing logs the range.
  evidenceComplete = $state(true)
  // Whether refresh() has ever completed. An empty list before this is
  // true means "not asked yet", never "nothing is retiring" -- the same
  // distinction watchlist.svelte.ts's `loaded` exists to keep.
  loaded = $state(false)
  error = $state<string | null>(null)

  // What the map paints as a ghost: watches that have not retired and
  // have not been force-removed. Force-removed watches are deliberately
  // absent -- that is the 2026-08-17 ruling's own sentence, that the
  // segment leaves the map now and the watch finishes the same job from
  // the watchlist, and the server's Store.Ghosts() says the same thing.
  ghosts = $derived(this.watches.filter((w) => !w.detached && w.state !== 'retired'))

  // Watches that retired by themselves and can still be taken back --
  // round 55's note line with its undo. The deadline is the server's,
  // read rather than recomputed, so the hour cannot come to mean two
  // different things on the two surfaces.
  retired = $derived(this.watches.filter((w) => w.state === 'retired' && !!w.undoableUntil))

  async refresh() {
    try {
      const { offers, watches, evidenceComplete } = await fetchDecommission()
      this.offers = offers
      this.watches = watches
      this.evidenceComplete = evidenceComplete
      this.error = null
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
    }
    this.loaded = true
  }

  // watchFor finds the ghost drawn for a boundary. Keyed on the
  // interface rather than the range, because that is the identity the
  // topography keys a zone on: an accepted offer's ghost has to land in
  // the lane its zone occupied, and the lane is the boundary.
  watchFor(device: string, iface: string): DecommissionWatch | null {
    return this.ghosts.find((w) => w.device === device && w.interface === iface) ?? null
  }

  offerFor(device: string, iface: string): DecommissionOffer | null {
    return this.offers.find((o) => o.device === device && o.interface === iface) ?? null
  }

  // The "yes" answer.
  async accept(offer: DecommissionOffer, cleanWindow?: string): Promise<string | null> {
    const result = await createDecommissionWatch(offer.device, offer.cidr, cleanWindow)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // The "no" answer: the zone leaves at once and no watch is created.
  async dismiss(offer: DecommissionOffer): Promise<string | null> {
    const err = await dismissDecommissionOffer(offer.device, offer.cidr)
    if (!err) await this.refresh()
    return err
  }

  // Force-remove: off the map now, still watched from the watchlist.
  async force(id: string, reason: string): Promise<string | null> {
    const result = await forceRemoveDecommissionGhost(id, reason)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  async undo(id: string): Promise<string | null> {
    const result = await undoDecommissionRetirement(id)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // The watchlist's own "forget" (#1069): the watch force-remove's
  // warning says can only be ended from here. reason mirrors force's own
  // #385 pattern -- the caller (Watchlist.svelte) requires one before
  // this is ever called, matching the card's own gate on its reason
  // field.
  async forget(id: string, reason: string): Promise<string | null> {
    const err = await deleteDecommissionWatch(id, reason)
    if (!err) await this.refresh()
    return err
  }
}

export const decommissionsState = new DecommissionsState()
