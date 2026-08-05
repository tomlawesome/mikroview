import { emptyFilters, type Filters } from './types'
import type { GroupByField } from './groupBy'

export interface TopTalkerWidget {
  id: string
  title: string
  groupBy: GroupByField
  filters: Filters
}

const STORAGE_KEY = 'mikroview-top-talker-widgets'
const DEFAULT_GROUP_BY: GroupByField = 'srcIp'

function loadInitial(): TopTalkerWidget[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    // Merge each stored widget's filters over a fresh emptyFilters(), same
    // reasoning as lib/presets.svelte.ts: a widget saved before a new
    // filter field existed shouldn't end up missing keys.
    return parsed
      .filter((w): w is TopTalkerWidget => typeof w?.id === 'string' && typeof w?.title === 'string')
      .map((w) => ({
        id: w.id,
        title: w.title,
        groupBy: (w.groupBy as GroupByField) || DEFAULT_GROUP_BY,
        filters: { ...emptyFilters(), ...w.filters },
      }))
  } catch {
    return []
  }
}

function makeId(): string {
  return `tt-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

class TopTalkerWidgetsState {
  widgets = $state<TopTalkerWidget[]>(loadInitial())

  add(title: string, groupBy: GroupByField, filters: Filters) {
    const trimmed = title.trim()
    if (!trimmed) return
    this.widgets = [...this.widgets, { id: makeId(), title: trimmed, groupBy, filters: { ...filters } }]
    this.persist()
  }

  remove(id: string) {
    this.widgets = this.widgets.filter((w) => w.id !== id)
    this.persist()
  }

  private persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.widgets))
    } catch {
      // storage unavailable -- widgets just won't persist across reloads
    }
  }
}

export const topTalkerWidgetsState = new TopTalkerWidgetsState()
