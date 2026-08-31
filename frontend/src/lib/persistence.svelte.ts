// SPDX-License-Identifier: AGPL-3.0-only

import { fetchPersistence } from './api'
import type { PersistenceInfo } from './types'

// Which backend mikroview's persisted stores (flags, definitions,
// watchlist entries, entities, tokens/accounts) actually use right now
// (#677's settings persistence row) -- fetched once and cached, same
// "only changes on restart" shape as versionState.ensureLoaded.
//
// GET /api/persistence is admin-gated (a filesystem directory is
// infrastructure detail, same reasoning /api/config/problems already
// applies), so a non-admin's ensureLoaded() throws and info stays null
// -- EngineRoom.svelte's onMount swallows that the same way it already
// swallows fetchSetupStatus's failure, and the persistence row states
// only the role-independent half of its sentence for that caller.
class PersistenceState {
  info = $state<PersistenceInfo | null>(null)
  private loaded = false

  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    this.info = await fetchPersistence()
  }
}

export const persistenceState = new PersistenceState()
