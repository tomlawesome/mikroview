// SPDX-License-Identifier: AGPL-3.0-only

import {
  createWatchlistEntry,
  deleteWatchlistEntry,
  fetchWatchlistEntries,
  fetchWatchlistMatches,
  promoteWatchlistDestinations,
  setWatchlistObserving,
  updateWatchlistEntry,
  type WatchlistEntryRequest,
} from './api'
import type { WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from './types'

// Live, admin-managed watchlist entries (#243) -- mirrors
// entities.svelte.ts's shape: a thin reactive wrapper over the API
// calls, refreshing the full list after every mutation rather than
// patching state locally, since the list is small and this keeps it
// always exactly what the server has.
class WatchlistState {
  entries = $state<WatchlistEntry[]>([])

  async refresh() {
    this.entries = await fetchWatchlistEntries()
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
