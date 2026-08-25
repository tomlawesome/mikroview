<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The Matches tab of Watchlist (#584, design ratified 2026-08-24):
  // one merged, reverse-chronological list of every entry's matches --
  // not an entry picker, and not grouped by entry.
  //
  // Why merged: only a Violation ever reaches the match log
  // (internal/watchlist's Outcome -- Observed candidates go to
  // RecordObservation, NoMatch records nothing), so every row here is
  // the same kind of event, an expectation being broken. There are no
  // confirmations mixed in to make one stream incoherent. Grouping, or
  // making the operator pick an entry first, would re-impose "know your
  // entry" as the reading order on the one surface that exists to
  // answer "what actually happened".
  //
  // Same grammar as the live view: one stream, newest first, repeats
  // collapsed into a count. `n×` is matchlog.Record's own Count, with
  // firstSeen/lastSeen -- not a new idea, the same collapsing the live
  // view's grouped rows do.
  //
  // No admin/read-only split lives here: Watchlist only ever mounts for
  // an admin (navGroups.ts's `admin: true` on the row), so a viewer
  // never reaches this tab at all -- the same argument #547 makes for
  // Suggestions, and what live-matches-tab.mjs proves.
  import { matchesState } from '../lib/matches.svelte'
  import ActionBadge from './ActionBadge.svelte'
  import type { WatchlistCoverage, WatchlistEntry, WatchlistMatch } from '../lib/types'

  let {
    entries,
    coverage,
    onopenentry,
  }: {
    entries: WatchlistEntry[]
    coverage: Record<string, WatchlistCoverage>
    // Follows the entry name back to the entry itself, on the Watchlist
    // tab. Owned by the parent because it is the parent that holds both
    // the tab selection and the expanded row.
    onopenentry: (entryId: string) => void
  } = $props()

  const byId = $derived(new Map(entries.map((e) => [e.id, e])))

  // Enabled entries that mikroview can say for certain nothing will ever
  // feed (#546's trigger, not a guess): no pushed firewall rule on any
  // connected router has logging on at all. 'unknown' and 'out-of-scope'
  // deliberately do not count, per the same honesty rule the broken ring
  // follows -- 'unknown' means mikroview has no answer, and asserting a
  // problem it cannot see is the failure this product keeps warning
  // about.
  const unfed = $derived(entries.filter((e) => e.enabled && coverage[e.id] === 'no-logging'))
  const enabledCount = $derived(entries.filter((e) => e.enabled).length)

  function entryName(m: WatchlistMatch): string {
    const e = byId.get(m.entryId)
    if (!e) return '(entry removed)'
    return e.name || '(unnamed)'
  }

  // The sentence to read differs by mode, and the entry name alone does
  // not say which -- so the row carries the mode, and the intro above
  // the list spells both out once rather than repeating a sentence on
  // every row.
  function modeLabel(m: WatchlistMatch): string {
    const e = byId.get(m.entryId)
    if (!e) return 'unknown mode'
    return e.invert ? 'egress policy' : 'watched port'
  }

  function modeSentence(m: WatchlistMatch): string {
    const e = byId.get(m.entryId)
    if (!e) return 'The entry this was recorded against no longer exists.'
    return e.invert
      ? 'This device went somewhere it should not.'
      : 'A port you watch was reached.'
  }

  // The identity the *event* carried, never the entry's own (possibly
  // unscoped) Source -- see matchlog.Tuple. An unscoped "any source"
  // entry's matches are exactly the ones no per-device query can reach,
  // and they are visible here for the first time (#586).
  function sourceLabel(m: WatchlistMatch): string {
    return m.tuple.source.mac || m.tuple.source.ip || 'unknown source'
  }

  // Date and time, not the live view's time-only formatting: this list
  // walks backwards through days as soon as "load older" is used, and a
  // bare clock time would make yesterday look like today.
  function formatWhen(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
  }

  function repeatTitle(m: WatchlistMatch): string {
    return m.count > 1
      ? `${m.count} occurrences collapsed into one record, first seen ${formatWhen(m.firstSeen)}`
      : 'seen once'
  }
</script>

<div class="page scrollbar">
  <p class="intro">
    Every match, newest first, across every entry -- the record of an expectation being broken. A
    <strong>watched port</strong> row means a port you watch was reached; an <strong>egress policy</strong> row means
    that device went somewhere it should not. <strong>n×</strong> is how many identical repeats collapsed into one
    record. The entry name takes you back to the entry.
  </p>

  {#if matchesState.error}
    <p class="error">Could not load matches: {matchesState.error}</p>
  {/if}

  {#if matchesState.loading}
    <p class="empty">Loading…</p>
  {:else if matchesState.records.length === 0 && matchesState.loaded && !matchesState.error}
    <!-- The empty state is the design, not a placeholder: "nothing
         matched" and "nothing could match" look identical, so this
         never reports a clean sheet it cannot stand behind. -->
    <div class="empty-state" role="status">
      {#if unfed.length > 0}
        <p class="empty-headline">Nothing has matched -- and nothing could.</p>
        <p class="empty-detail">
          {unfed.length === 1 ? 'One enabled watch cannot be checked at all' : `${unfed.length} enabled watches cannot be checked at all`}:
          <strong>{unfed.map((e) => e.name || '(unnamed)').join(', ')}</strong>. No firewall rule on any router you have
          connected has logging turned on, so no traffic is being reported for it. Set log=yes on the rules you want to
          see (see the RouterOS setup guide).
        </p>
      {:else if enabledCount === 0}
        <!-- Not one of the two states the design names, because it is
             the one where the question cannot arise: with no enabled
             entry there is nothing that could match, and saying
             "nothing has broken" here would be the exact false clean
             sheet the other branch exists to prevent. -->
        <p class="empty-headline">There is nothing to match yet.</p>
        <p class="empty-detail">No watchlist entry is enabled, so nothing is being watched. Add one on the Watchlist tab.</p>
      {:else}
        <p class="empty-headline">Nothing has broken.</p>
        <p class="empty-detail">
          No entry has recorded a match. Nothing here says a watch cannot be checked, so this is a real answer, not an
          empty page.
        </p>
      {/if}
    </div>
  {:else if matchesState.records.length > 0}
    <ul class="list">
      {#each matchesState.records as m (m.id)}
        <li class="match">
          <span class="when">{formatWhen(m.lastSeen)}</span>
          <button class="entry-link" onclick={() => onopenentry(m.entryId)}>{entryName(m)}</button>
          <span class="badge mode" class:egress={modeLabel(m) === 'egress policy'} title={modeSentence(m)}>
            {modeLabel(m)}
          </span>
          <span class="flow">
            <span class="addr">{sourceLabel(m)}</span>
            <span class="arrow" aria-hidden="true">→</span>
            <span class="addr">{m.tuple.destIp}:{m.tuple.port}</span>
          </span>
          <span class="count" title={repeatTitle(m)}>{m.count}×</span>
          <ActionBadge action={m.event.action} />
        </li>
      {/each}
    </ul>

    <div class="older">
      {#if matchesState.exhausted}
        <p class="empty small">Nothing older -- this is the whole match log.</p>
      {:else}
        <button class="load-older" disabled={matchesState.loadingOlder} onclick={() => matchesState.loadOlder()}>
          {matchesState.loadingOlder ? 'Loading…' : 'Load older'}
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .empty.small {
    padding: 4px 0;
    font-size: 12px;
  }

  .empty-state {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 16px 18px;
    max-width: 80ch;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .empty-headline {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .empty-detail {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .match {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 8px 12px;
  }

  .when {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .entry-link {
    background: transparent;
    border: none;
    padding: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--accent);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: pointer;
  }

  .entry-link:hover {
    color: var(--fg);
  }

  .badge.mode {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    background: var(--panel);
    color: var(--fg-muted);
  }

  .badge.mode.egress {
    background: var(--accent-bg);
    color: var(--accent);
  }

  .flow {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .addr {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
  }

  .arrow {
    color: var(--fg-dim);
  }

  .count {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 700;
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }

  .older {
    display: flex;
    justify-content: center;
    padding-bottom: 6px;
  }

  .load-older {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 12px;
  }

  .load-older:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .load-older:disabled {
    opacity: 0.6;
  }
</style>
