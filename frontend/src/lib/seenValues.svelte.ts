// SPDX-License-Identifier: AGPL-3.0-only
//
// What the stream's Proto and Interface filters offer (#1226): the
// values this instance has actually observed, read from
// GET /api/seen-values.
//
// These were the only two filter controls in the strip with no list
// behind them -- every other one is a picker over something known. The
// options that were put to the owner were a hardcoded list of well-known
// protocols or a list scraped from whatever happened to be on screen;
// both were rejected. The first is a guess about somebody else's
// network, the second is thrown away on reload and lies after a quiet
// hour. The server grows the real list instead and persists it.
//
// Suggestions only, exactly like scopeSuggestions.svelte.ts next door:
// both boxes still accept a value that is not in the list. That is not a
// nicety -- a filter you cannot set up until the traffic appears is
// useless for watching for traffic that has not appeared yet, which is
// most of what an operator wants a filter for.
//
// Fetched once per page load and cached, the same shape geoip.svelte.ts
// uses: the list only grows as events arrive, and a menu that is one
// session behind is a missing suggestion, never a wrong filter.

import { fetchSeenValues } from './api'

class SeenValuesState {
  // Plain value strings in the order the server served them, which is
  // most recently seen first -- what the network is doing now belongs at
  // the top of the menu. The seen times are deliberately dropped here:
  // a <datalist> option renders a value and an optional label, and
  // "last seen 4 minutes ago" in a menu is a fact about the past that
  // would go stale on screen.
  proto = $state<string[]>([])
  interfaces = $state<string[]>([])
  loaded = $state(false)
  private started = false

  // Best-effort on purpose, the same reasoning scopeSuggestionsState
  // documents: a failed fetch means the two boxes suggest nothing and
  // still accept anything typed into them, which is exactly what they
  // did before this existed. An operator setting a filter should not be
  // shown an error about a menu.
  async ensureLoaded(): Promise<void> {
    if (this.started) return
    this.started = true
    try {
      const values = await fetchSeenValues()
      this.proto = values.proto.map((v) => v.value)
      this.interfaces = values.interface.map((v) => v.value)
    } catch {
      // Deliberately leaves the lists as they were rather than blanking
      // them: on a first load they are already empty, and blanking a
      // list that did load would turn one failed request into a menu
      // that silently lost its options.
    } finally {
      this.loaded = true
    }
  }

  // reset drops the cache so the next ensureLoaded fetches again. For
  // tests, and for a sign-out that should not leave one account's
  // interface names suggested to the next.
  reset(): void {
    this.started = false
    this.loaded = false
    this.proto = []
    this.interfaces = []
  }
}

export const seenValuesState = new SeenValuesState()
