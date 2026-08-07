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

  async refresh() {
    this.list = await fetchEntities()
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
