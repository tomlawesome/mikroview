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
// #1160: the same suggestion, several times over. internal/suggest
// records one candidate per justification, so two drop rules covering
// port 445 -- or the same rule pushed by two routers -- arrive as two
// candidates that render as the same row, word for word ("port 445 →
// :445 · suggested — from drop rule port 445", three times).
//
// Nothing is discarded server-side: this collapses the list at the point
// it is read, keeping the first of each set (the earliest, since the API
// returns them in first-seen order) so the row that stays is the one
// whose id the operator's accept/set-aside has always acted on.
//
// The key is what the row is built from, not the rendered row: the
// component resolves a zone name for the boundary, which this module
// cannot see. `stale` is part of it because a stale candidate says so on
// its chip -- two rows that read differently are not duplicates.
export function suggestionIdentity(c: Suggestion): string {
  return JSON.stringify([
    c.kind,
    c.name,
    c.source?.mac ?? '',
    c.source?.ip ?? '',
    [...(c.ports ?? [])].sort((a, b) => a - b),
    c.addressList ?? '',
    c.stale === true,
  ])
}

export function dedupeSuggestions(candidates: readonly Suggestion[]): Suggestion[] {
  const seen = new Set<string>()
  const kept: Suggestion[] = []
  for (const c of candidates) {
    const key = suggestionIdentity(c)
    if (seen.has(key)) continue
    seen.add(key)
    kept.push(c)
  }
  return kept
}

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
