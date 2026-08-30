<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The docket (#633, rounds 17-19/25/28-29): what was flagged, what
  // you watch, what changed -- flags, watchlist and audit log as one
  // deck card's tabs. The tab row carries the flags tab's one control,
  // the clear-all bubble (round 29, owner-ratified): an outlined amber
  // bubble; one click arms it alarm-red 'confirm'; the second click
  // clears every open flag; clicking anywhere else disarms it, so an
  // armed bubble cannot ambush a stray click.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import Flags from './Flags.svelte'
  import Watchlist from './Watchlist.svelte'
  import AuditLog from './AuditLog.svelte'

  const isAdmin = $derived(authState.role === 'admin')

  // The docket's tab follows the app view, so a deep link (the scene
  // bar's flag badge, the broken ring, the menu's Audit log row) lands
  // on the right tab; clicking a tab is just a view change.
  type Tab = 'flags' | 'watchlist' | 'audit'
  const tab = $derived.by((): Tab => {
    if (appState.view === 'watchlist' && isAdmin) return 'watchlist'
    if (appState.view === 'audit' && isAdmin) return 'audit'
    return 'flags'
  })

  let armed = $state(false)

  function onClearAll(e: MouseEvent) {
    e.stopPropagation()
    if (!armed) {
      armed = true
      return
    }
    armed = false
    flagsState.clearAll()
  }

  function onWindowClick() {
    armed = false
  }
</script>

<svelte:window onclick={onWindowClick} />

<div class="docket">
  <div class="tab-row" role="tablist" aria-label="The docket">
    <button
      class="tab"
      class:on={tab === 'flags'}
      role="tab"
      aria-selected={tab === 'flags'}
      onclick={() => (appState.view = 'flags')}
    >
      flags
      <span class="count">⚑ {flagsState.activeCount}</span>
    </button>
    {#if isAdmin}
      <button
        class="tab"
        class:on={tab === 'watchlist'}
        role="tab"
        aria-selected={tab === 'watchlist'}
        onclick={() => (appState.view = 'watchlist')}
      >
        watchlist
        {#if watchlistState.brokenCount > 0}<span class="count broken">◉ {watchlistState.brokenCount} broken</span>{/if}
      </button>
      <button
        class="tab"
        class:on={tab === 'audit'}
        role="tab"
        aria-selected={tab === 'audit'}
        onclick={() => (appState.view = 'audit')}
      >
        audit log
      </button>
    {/if}

    {#if tab === 'flags' && flagsState.activeCount > 0}
      <button class="bubble" class:armed onclick={onClearAll} title="They keep their place in the audit log">
        {armed ? 'confirm' : `clear all ${flagsState.activeCount}`}
      </button>
    {/if}
  </div>

  <div class="pane">
    {#if tab === 'watchlist'}
      <Watchlist />
    {:else if tab === 'audit'}
      <AuditLog />
    {:else}
      <Flags />
    {/if}
  </div>
</div>

<style>
  .docket {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .tab-row {
    display: flex;
    align-items: baseline;
    gap: 4px;
    padding: 0 6px 6px;
  }

  .tab {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 4px 10px 6px;
    cursor: pointer;
  }

  .tab:hover {
    color: var(--fg-muted);
  }

  .tab.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  .count {
    margin-left: 6px;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--fg-dim);
  }

  .count.broken {
    color: var(--alarm);
  }

  /* The bubble (round 29): outlined, never filled. */
  .bubble {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--now);
    background: transparent;
    border: 1px solid var(--now);
    border-radius: 999px;
    padding: 4px 15px;
    cursor: pointer;
    transition:
      color 0.15s,
      border-color 0.15s;
  }

  .bubble:hover {
    background: color-mix(in srgb, var(--now) 8%, transparent);
  }

  .bubble.armed {
    color: var(--alarm);
    border-color: var(--alarm);
  }

  .bubble.armed:hover {
    background: color-mix(in srgb, var(--alarm) 8%, transparent);
  }

  @media (prefers-reduced-motion: reduce) {
    .bubble {
      transition: none;
    }
  }

  .pane {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
</style>
