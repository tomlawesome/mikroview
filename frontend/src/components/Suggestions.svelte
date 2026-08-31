<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Suggestions (#243 slice 5): watchlist entries suggested from data
  // RouterOS has already pushed (named DHCP leases, ports an existing
  // rule already blocks) -- so an operator doesn't have to already know
  // what to watch before this feature is useful. Rendered inside
  // Watchlist.svelte's "Suggestions" tab (#547, per the ratified
  // navigation record: "Suggestions is a tab of Watchlist") -- briefly
  // its own page after #544 dropped its rail row, now folded back in.
  // Watchlist.svelte's own nav entry stays admin-gated for now (#657's
  // job to widen, not this file's) -- but accept/hide/unhide are #653's
  // normal operational actions (user tier, not owner-level), so this
  // page gates those three itself rather than inheriting whatever tier
  // its host page ends up reachable at. Reset-everything below stays
  // admin-only, same tier as before -- it is destructive across the
  // whole watchlist, not a single candidate's own decision.
  //
  // Every candidate is one of three states, never a binary accept/reject:
  //
  //  - Off -- undecided. The default for every newly generated
  //    candidate, and the default view here.
  //  - On -- accepted; a real watchlist entry now exists for it (see
  //    Watchlist.svelte).
  //  - Hide -- explicitly declined, but reversible only by deliberately
  //    switching to this view and flipping it back. Never reappears on
  //    its own.
  //
  // Candidates are kept in sync with the router automatically in the
  // background (internal/suggest.Store.RunPeriodicSync) -- there is
  // deliberately no manual "refresh" button here, see that method's own
  // doc comment for why a separate soft-refresh control would be
  // redundant.
  //
  // Accept/hide/unhide need can-edit (user or admin, #653); Reset
  // everything below stays admin-only.
  import { onMount } from 'svelte'
  import { authState } from '../lib/auth.svelte'
  import { suggestState } from '../lib/suggest.svelte'
  import type { Suggestion, SuggestionStatus } from '../lib/types'

  onMount(() => {
    suggestState.refresh()
  })

  // See the doc comment above: accept/hide/unhide need can-edit,
  // reset-everything stays admin-only.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)
  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  let filter = $state<SuggestionStatus>('off')

  let visible = $derived(suggestState.candidates.filter((c) => c.status === filter))

  let acceptingId = $state<string | null>(null)
  let hidingId = $state<string | null>(null)
  let unhidingId = $state<string | null>(null)
  let actionError = $state<string | null>(null)

  async function accept(c: Suggestion) {
    acceptingId = c.id
    actionError = null
    try {
      const result = await suggestState.accept(c.id)
      if (typeof result === 'string') actionError = result
    } finally {
      acceptingId = null
    }
  }

  async function hide(c: Suggestion) {
    hidingId = c.id
    actionError = null
    try {
      const err = await suggestState.hide(c.id)
      if (err) actionError = err
    } finally {
      hidingId = null
    }
  }

  async function unhide(c: Suggestion) {
    unhidingId = c.id
    actionError = null
    try {
      const err = await suggestState.unhide(c.id)
      if (err) actionError = err
    } finally {
      unhidingId = null
    }
  }

  // --- Nuke -----------------------------------------------------------
  // Deliberately the most alarming control on this page: it destroys
  // every watchlist entry, not just suggestion-tracking state, and
  // cannot be undone (#243 slice 5 design: "gated behind a real confirm
  // step and an unmistakably serious warning"). A native confirm()
  // dialog forces a synchronous, explicit choice the same way
  // Watchlist.svelte's own entry-removal confirm does, but with wording
  // proportionate to what is actually at stake here.
  let resetting = $state(false)

  async function resetEverything() {
    const entryCount = suggestState.countByStatus('on')
    const warning =
      `This permanently deletes every watchlist entry` +
      (entryCount > 0 ? ` -- including the ${entryCount} you've already accepted` : '') +
      `, and cannot be undone. A fresh set of suggestions will be generated from what your router reports right now.\n\n` +
      `Type OK only if you are certain.`
    if (!confirm(warning)) return
    resetting = true
    actionError = null
    try {
      const err = await suggestState.reset()
      if (err) actionError = err
      else filter = 'off'
    } finally {
      resetting = false
    }
  }

  function kindLabel(c: Suggestion): string {
    switch (c.kind) {
      case 'device':
        return 'device'
      case 'port':
        return 'port'
      default:
        return c.kind
    }
  }

  function detailLabel(c: Suggestion): string {
    if (c.kind === 'device') {
      return c.source?.mac || c.source?.ip || 'unknown device'
    }
    if (c.kind === 'port') {
      return `port ${(c.ports ?? []).join(', ')}`
    }
    return c.addressList ?? ''
  }
</script>

<div class="page scrollbar">
  <p class="intro">
    Suggested watchlist entries, generated from what your router has already reported: named devices (from DHCP
    leases), and ports an existing firewall rule already drops or rejects. Nothing here watches anything on its own
    -- <strong>accept</strong> a suggestion to create a real watchlist entry, or <strong>hide</strong> it if it's not
    useful. New suggestions appear automatically as your router's data changes; hiding one is remembered until you
    deliberately come back to this Hidden view and undo it.
  </p>

  <div class="toolbar">
    <div class="filters">
      {#each [['off', 'Undecided'], ['on', 'Accepted'], ['hide', 'Hidden']] as [status, label] (status)}
        <button
          class="filter"
          class:active={filter === status}
          onclick={() => (filter = status as SuggestionStatus)}
        >
          {label}
          <span class="count">{suggestState.countByStatus(status as SuggestionStatus)}</span>
        </button>
      {/each}
    </div>
    {#if isAdmin}
      <button class="nuke" disabled={resetting} onclick={resetEverything}>
        {resetting ? 'Resetting…' : 'Reset everything (cannot be undone)'}
      </button>
    {/if}
  </div>

  {#if actionError}
    <p class="error">{actionError}</p>
  {/if}

  <section class="section">
    {#if visible.length === 0}
      <p class="empty">
        {#if filter === 'off'}
          No undecided suggestions right now -- check back after your router next pushes its data, or nothing new
          has appeared since you last reviewed.
        {:else if filter === 'on'}
          No accepted suggestions yet.
        {:else}
          Nothing hidden.
        {/if}
      </p>
    {:else}
      <ul class="list">
        {#each visible as c (c.id)}
          <li class="card" class:stale={c.stale}>
            <div class="card-main">
              <span class="name">{c.name || '(unnamed)'}</span>
              <span class="badge kind">{kindLabel(c)}</span>
              {#if c.stale}
                <span class="badge stale-badge">stale -- reason no longer holds</span>
              {/if}
              <span class="detail">{detailLabel(c)}</span>
              <span class="justification">{c.justification}</span>
              <span class="device">via {c.routerDevice}</span>
            </div>
            <span class="row-actions">
              {#if canEdit && filter === 'off'}
                <button class="accept" disabled={acceptingId === c.id} onclick={() => accept(c)}>
                  {acceptingId === c.id ? 'Accepting…' : 'Accept'}
                </button>
                <button class="hide" disabled={hidingId === c.id} onclick={() => hide(c)}>
                  {hidingId === c.id ? 'Hiding…' : 'Hide'}
                </button>
              {:else if canEdit && filter === 'hide'}
                <button class="unhide" disabled={unhidingId === c.id} onclick={() => unhide(c)}>
                  {unhidingId === c.id ? 'Unhiding…' : 'Unhide'}
                </button>
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
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

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }

  .filters {
    display: flex;
    gap: 6px;
  }

  .filter {
    display: flex;
    align-items: center;
    gap: 6px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 999px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .filter:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .filter.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  .count {
    font-size: 11px;
    color: inherit;
    opacity: 0.75;
  }

  .nuke {
    background: var(--reject-bg);
    border: 1px solid var(--reject);
    color: var(--reject);
    font-weight: 600;
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 12px;
  }

  .nuke:hover:not(:disabled) {
    background: var(--reject);
    color: var(--bg);
  }

  .nuke:disabled {
    opacity: 0.6;
  }

  .error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
    max-width: 70ch;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px;
  }

  /* The stale highlight is deliberately loud (#243 slice 5 design: "a
     bright, hard-to-miss highlight") -- this is the one visual state on
     this page meant to interrupt rather than blend in, since it means an
     already-accepted entry's original justification has quietly stopped
     holding. */
  .card.stale {
    border-color: var(--reject);
    background: var(--reject-bg);
  }

  .card-main {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
    flex: 1 1 auto;
  }

  .name {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .badge {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 7px;
    border-radius: 999px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .badge.kind {
    background: var(--accent-bg);
    color: var(--accent);
  }

  .badge.stale-badge {
    background: var(--reject);
    color: var(--bg);
  }

  .detail {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-muted);
  }

  .justification {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .device {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .row-actions {
    display: flex;
    gap: 8px;
    flex: none;
  }

  .accept,
  .hide,
  .unhide {
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
  }

  .accept {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .hide,
  .unhide {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .hide:hover:not(:disabled),
  .unhide:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button:disabled {
    opacity: 0.6;
  }
</style>
