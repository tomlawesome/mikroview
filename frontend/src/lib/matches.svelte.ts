// SPDX-License-Identifier: AGPL-3.0-only

import { fetchRecentMatches } from './api'
import type { WatchlistMatch } from './types'

// One page of the merged list. 100 is matchlog's own DefaultLimit
// (internal/matchlog), and the ratified design for the Matches tab
// (#584) names it: "load the most recent 100, newest first, with load
// older as the way back in time". Sent explicitly rather than relying on
// the server's default, so the number the UI promises is the number the
// UI asks for.
export const MATCHES_PAGE_SIZE = 100

// The merged, reverse-chronological match list behind Watchlist's
// Matches tab (#584), fed by GET /api/matches?entries=all (#586).
//
// A class with its own state rather than a fetch inside the component,
// for watchlist.svelte.ts's own reason: the paging cursor and the
// already-seen set are real state with rules attached, and rules that
// live in a component are rules that get re-derived by the next
// component that needs them.
class MatchesState {
  // Newest first, by lastSeen -- the order the server returns and the
  // order the tab renders. Never re-sorted here: the server's ordering
  // is the contract (matchlog.RecentQuery), and a client-side sort would
  // quietly paper over a backend that stopped honouring it.
  records = $state<WatchlistMatch[]>([])
  // The first page is loading. Separate from loadingOlder so the empty
  // state can tell "still asking" from "asked, and the answer is
  // nothing" -- the distinction this tab's whole empty state exists for.
  loading = $state(false)
  loadingOlder = $state(false)
  // The reason, not a generic failure: a 503 here means the match log is
  // not available at all, which is a configuration answer rather than a
  // network blip, exactly as Watchlist.svelte's per-entry match panel
  // already argues.
  error = $state<string | null>(null)
  // A first load has completed (successfully or not). Without it an
  // empty list before the first fetch is indistinguishable from a
  // confirmed-empty one, which is the misreading this tab is built to
  // avoid.
  loaded = $state(false)
  // No older matches remain behind the ones already loaded.
  exhausted = $state(false)

  // Every id already held, so a page that overlaps the previous one adds
  // only what is new. See loadOlder for why overlap happens at all.
  #seen = new Set<string>()

  async load() {
    this.loading = true
    this.error = null
    try {
      const page = await fetchRecentMatches({ limit: MATCHES_PAGE_SIZE })
      this.records = page
      this.#seen = new Set(page.map((m) => m.id))
      // A short first page means the log holds no more than this, so
      // there is nothing older to ask for and the control that asks
      // should not be offered.
      this.exhausted = page.length < MATCHES_PAGE_SIZE
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
      // The buffer is left exactly as it was rather than emptied: a
      // failed refresh must not present itself as "nothing has broken",
      // which is the one sentence this surface must never say wrongly.
    } finally {
      this.loading = false
      this.loaded = true
    }
  }

  // loadOlder walks back in time from the oldest record already shown.
  //
  // The cursor is that record's lastSeen, because the query's `until`
  // filters on firstSeen (`first_seen < until`, both backends). That
  // pairing is deliberate:
  //
  //  - It cannot skip. Every record not yet loaded has lastSeen at or
  //    below the cursor, and firstSeen never exceeds its own lastSeen,
  //    so it survives `firstSeen < cursor` -- except a record that both
  //    began and ended at exactly the cursor instant, which a strict
  //    comparison excludes either way.
  //  - It can repeat. A long-running record that began before the cursor
  //    and is still being updated after it satisfies the filter and
  //    comes back a second time. Hence #seen: overlap is filtered on
  //    arrival rather than avoided, because avoiding it would mean
  //    skipping, and a missing violation is worse than a repeated fetch.
  //
  // A page that is entirely overlap therefore adds nothing, and is read
  // as the end of the list. That is exact for every ordinary case and
  // conservative in one: 100 records that all span the cursor would stop
  // the walk early. Reported as "nothing older" rather than pretended
  // otherwise, and the operator can reload the tab.
  async loadOlder() {
    const oldest = this.records[this.records.length - 1]
    if (!oldest || this.loadingOlder || this.exhausted) return
    this.loadingOlder = true
    this.error = null
    try {
      const page = await fetchRecentMatches({ until: oldest.lastSeen, limit: MATCHES_PAGE_SIZE })
      const fresh = page.filter((m) => !this.#seen.has(m.id))
      for (const m of fresh) this.#seen.add(m.id)
      // Appended, not merged: every record in a page has lastSeen at or
      // below the cursor, so the concatenation is still newest-first.
      this.records = [...this.records, ...fresh]
      this.exhausted = fresh.length === 0 || page.length < MATCHES_PAGE_SIZE
    } catch (err) {
      this.error = err instanceof Error ? err.message : String(err)
    } finally {
      this.loadingOlder = false
    }
  }

  // Back to the state before any load, for a test or a signed-out
  // session. Not called on tab switch -- see Watchlist.svelte.
  reset() {
    this.records = []
    this.#seen = new Set()
    this.loading = false
    this.loadingOlder = false
    this.error = null
    this.loaded = false
    this.exhausted = false
  }
}

export const matchesState = new MatchesState()
