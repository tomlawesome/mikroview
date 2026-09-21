// SPDX-License-Identifier: AGPL-3.0-only

import { emptyFilters, type Filters } from './types'
import type { GroupByField } from './groupBy'
import { preferencesState } from './preferences.svelte'

export interface TopTalkerWidget {
  id: string
  title: string
  groupBy: GroupByField
  filters: Filters
}

// #1283: was its own localStorage key ('mikroview-top-talker-widgets').
const PREFS_KEY = 'topTalkers'
const DEFAULT_GROUP_BY: GroupByField = 'srcIp'

// Same reasoning as lib/presets.svelte.ts: a widget saved before a new
// filter field existed shouldn't end up missing keys.
function sanitize(value: unknown): TopTalkerWidget[] {
  if (!Array.isArray(value)) return []
  return value
    .filter((w): w is TopTalkerWidget => typeof w?.id === 'string' && typeof w?.title === 'string')
    .map((w) => ({
      id: w.id,
      title: w.title,
      groupBy: (w.groupBy as GroupByField) || DEFAULT_GROUP_BY,
      filters: { ...emptyFilters(), ...w.filters },
    }))
}

function makeId(): string {
  return `tt-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

class TopTalkerWidgetsState {
  widgets = $state<TopTalkerWidget[]>([])

  constructor() {
    preferencesState.register(PREFS_KEY, (value) => {
      this.widgets = sanitize(value)
    })
  }

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
    preferencesState.set(PREFS_KEY, this.widgets)
  }
}

export const topTalkerWidgetsState = new TopTalkerWidgetsState()
