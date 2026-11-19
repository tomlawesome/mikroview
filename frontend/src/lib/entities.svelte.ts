// SPDX-License-Identifier: AGPL-3.0-only

import { deleteEntity, fetchEntities, upsertEntity } from './api'
import type { Entity } from './types'

// Live, admin-managed (type, key) -> label/tags records (see
// internal/entities.Entity) -- admin-only, mirrors
// detectorSettings.svelte.ts's shape: a thin reactive wrapper over the
// API calls, refreshing the full list after every mutation rather than
// patching state locally, since the list is small and this keeps it
// always exactly what the server has.
class EntitiesState {
  list = $state<Entity[]>([])
  // False until the first fetch lands, so the page can show ghost rows
  // rather than an empty table that reads as "no names yet" (#1540).
  loaded = $state(false)
  // Why the last refresh failed, or null: a failed load must not read as
  // an empty table of "no names yet" (#1540).
  loadError = $state<string | null>(null)
  // True once any fetch has succeeded: a later failed refresh then keeps
  // the names already held rather than hiding them (#1540).
  hasLoaded = $state(false)

  async refresh() {
    try {
      this.list = await fetchEntities()
      this.loadError = null
      this.hasLoaded = true
    } catch (err) {
      this.loadError = err instanceof Error ? err.message : String(err)
      throw err
    } finally {
      this.loaded = true
    }
  }

  async upsert(entity: Entity): Promise<string | null> {
    const err = await upsertEntity(entity)
    if (!err) await this.refresh()
    return err
  }

  async remove(type: string, key: string): Promise<string | null> {
    const err = await deleteEntity(type, key)
    if (!err) await this.refresh()
    return err
  }
}

export const entitiesState = new EntitiesState()
