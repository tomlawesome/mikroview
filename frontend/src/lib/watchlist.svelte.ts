// SPDX-License-Identifier: AGPL-3.0-only

import {
  createWatchlistEntry,
  deleteWatchlistEntry,
  fetchWatchlistEntries,
  fetchWatchlistMatches,
  promoteWatchlistDestinations,
  setWatchlistEnabled,
  setWatchlistObserving,
  updateWatchlistEntry,
  type WatchlistEntryRequest,
} from './api'
import type { WatchlistCoverage, WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from './types'

// Live, admin-managed watchlist entries (#243) -- mirrors
// entities.svelte.ts's shape: a thin reactive wrapper over the API
// calls, refreshing the full list after every mutation rather than
// patching state locally, since the list is small and this keeps it
// always exactly what the server has.
class WatchlistState {
  entries = $state<WatchlistEntry[]>([])
  // What can be said about whether anything is able to feed each entry
  // (#274), keyed by entry id. Refreshed with the entries, since it is
  // derived from what routers have pushed rather than stored.
  coverage = $state<Record<string, WatchlistCoverage>>({})

  // #546's broken ring: how many *enabled* expectations currently answer
  // 'no-logging' -- the operator declared a watch and no pushed firewall
  // rule can ever produce an event it would match. 'unknown' and
  // 'out-of-scope' deliberately do not count (see the ratified decision
  // on #546): 'unknown' means mikroview has no answer at all, and ringing
  // on it would assert a problem it cannot see; 'out-of-scope' is a
  // scoping fact, not a failure. A disabled entry does not count either
  // -- switching a watch off is not promising mikroview can see it.
  // #367's evidence-completeness guard already downgrades an
  // under-evidenced 'no-logging'/'out-of-scope' to 'unknown' server-side
  // (definitionCoverage, internal/api/definitions.go), so this inherits
  // that honesty guarantee for free rather than needing to reimplement it.
  brokenCount = $derived.by(() => this.entries.filter((e) => e.enabled && this.coverage[e.id] === 'no-logging').length)

  // The scene bar's "◉ 7 ○ 1" (#683, ratified round 29): watchers
  // actually holding, i.e. enabled and not ring-broken -- the same
  // predicate Watchlist.svelte's own class:watching already uses, so
  // the bar's count and the page's own per-row marker never disagree.
  heldCount = $derived.by(
    () => this.entries.filter((e) => e.enabled && this.coverage[e.id] !== 'no-logging').length,
  )

  async refresh() {
    const { entries, coverage } = await fetchWatchlistEntries()
    this.entries = entries
    this.coverage = coverage
  }

  async create(req: WatchlistEntryRequest): Promise<string | null> {
    const result = await createWatchlistEntry(req)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  async update(id: string, req: WatchlistEntryRequest): Promise<string | null> {
    const result = await updateWatchlistEntry(id, req)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  async remove(id: string): Promise<string | null> {
    const err = await deleteWatchlistEntry(id)
    if (!err) await this.refresh()
    return err
  }

  async promote(id: string, destinations: WatchlistPermittedDest[]): Promise<string | null> {
    const result = await promoteWatchlistDestinations(id, destinations)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  async setObserving(id: string, observing: boolean): Promise<string | null> {
    const result = await setWatchlistObserving(id, observing)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // Pause/resume (#676's ratified "pause watch"/"resume watch"): the
  // same enabled flag the broken-ring predicate and stateLabel already
  // read, flipped from the drawer rather than only from the add/edit
  // form (which never exposed a plain toggle for it).
  async setEnabled(id: string, enabled: boolean): Promise<string | null> {
    const result = await setWatchlistEnabled(id, enabled)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // matchesFor is not cached on this class -- unlike entries, matches
  // can be numerous and are viewed per-entry on demand (see
  // Watchlist.svelte), not held as one always-fresh list. mac/ip come
  // from the entry's own Source, so this only makes sense for a scoped
  // entry -- an unscoped entry's matches carry whichever device actually
  // triggered them, which a single mac/ip query cannot enumerate.
  async matchesFor(mac?: string, ip?: string): Promise<WatchlistMatch[]> {
    return fetchWatchlistMatches({ mac, ip, limit: 50 })
  }
}

export const watchlistState = new WatchlistState()
