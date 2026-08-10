// SPDX-License-Identifier: AGPL-3.0-only

import {
  acceptSuggestion,
  fetchSuggestions,
  hideSuggestion,
  resetSuggestions,
  unhideSuggestion,
} from './api'
import type { Suggestion, SuggestionStatus, WatchlistEntry } from './types'

// Live, admin-managed suggestion candidates (#243 slice 5) -- mirrors
// watchlist.svelte.ts's shape: a thin reactive wrapper over the API
// calls, refreshing the full list after every mutation rather than
// patching state locally, since the candidate pool is small and this
// keeps it always exactly what the server has. Unlike watchlist entries,
// there is deliberately no create/update here -- a candidate only ever
// moves between Off/On/Hide, never edited (see internal/suggest's own
// package doc comment).
class SuggestState {
  candidates = $state<Suggestion[]>([])

  async refresh() {
    this.candidates = await fetchSuggestions()
  }

  async accept(id: string): Promise<{ entry: WatchlistEntry } | string> {
    const result = await acceptSuggestion(id)
    if (typeof result === 'string') return result
    await this.refresh()
    return { entry: result.entry }
  }

  async hide(id: string): Promise<string | null> {
    const result = await hideSuggestion(id)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  async unhide(id: string): Promise<string | null> {
    const result = await unhideSuggestion(id)
    if (typeof result === 'string') return result
    await this.refresh()
    return null
  }

  // reset is the "nuke" action -- see resetSuggestions' own doc comment.
  // Callers are responsible for their own confirm dialog; this class
  // adds no additional gate beyond what the server already enforces.
  async reset(): Promise<string | null> {
    const result = await resetSuggestions()
    if (typeof result === 'string') return result
    this.candidates = result
    return null
  }

  countByStatus(status: SuggestionStatus): number {
    return this.candidates.filter((c) => c.status === status).length
  }
}

export const suggestState = new SuggestState()
