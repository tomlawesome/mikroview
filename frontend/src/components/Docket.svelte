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
  let busy = $state(false)
  let error = $state<string | null>(null)

  // flagsState.clearAll() optimistically updates, then rolls back and
  // rethrows on failure (see its doc comment) -- caught here so a
  // transient failure reads as an error, not a button that did nothing.
  async function onClearAll(e: MouseEvent) {
    e.stopPropagation()
    if (!armed) {
      armed = true
      return
    }
    armed = false
    busy = true
    error = null
    try {
      await flagsState.clearAll()
    } catch (err) {
      error = err instanceof Error ? `Could not clear all flags: ${err.message}` : 'Could not clear all flags'
    } finally {
      busy = false
    }
  }

  function onWindowClick() {
    armed = false
  }
</script>

<svelte:window onclick={onWindowClick} />

<div class="docket">
  <div class="tab-row" role="tablist" aria-label="The docket">
    <!-- Counts sit beneath the labels (round 19): bare ink, tiny, and
         only when they have something to say -- a permanent "0" is the
         failure, not the goal. The broken watch is the small red ○
         under "watchlist"; the healthy count wears the watchers'
         purple. The row keeps its height when every count is silent. -->
    <button
      class="tab"
      class:on={tab === 'flags'}
      role="tab"
      aria-selected={tab === 'flags'}
      onclick={() => (appState.view = 'flags')}
    >
      <span class="tlabel">flags</span>
      <span class="under">
        {#if flagsState.activeCount > 0}<b class="ct">⚑ {flagsState.activeCount}</b>{/if}
      </span>
    </button>
    {#if isAdmin}
      <button
        class="tab"
        class:on={tab === 'watchlist'}
        role="tab"
        aria-selected={tab === 'watchlist'}
        onclick={() => (appState.view = 'watchlist')}
      >
        <span class="tlabel">watchlist</span>
        <span class="under">
          {#if watchlistState.entries.length > 0}<b class="wct">◉ {watchlistState.entries.length}</b>{/if}
          {#if watchlistState.brokenCount > 0}<b class="bct">○ {watchlistState.brokenCount}</b>{/if}
        </span>
      </button>
      <button
        class="tab"
        class:on={tab === 'audit'}
        role="tab"
        aria-selected={tab === 'audit'}
        onclick={() => (appState.view = 'audit')}
      >
        <span class="tlabel">audit log</span>
        <span class="under"></span>
      </button>
    {/if}

    {#if tab === 'flags' && flagsState.activeCount > 0}
      <button class="bubble" class:armed disabled={busy} onclick={onClearAll} title="They keep their place in the audit log">
        {armed ? 'confirm' : `clear all ${flagsState.activeCount}`}
      </button>
    {/if}
    {#if error}<span class="err" role="alert">{error}</span>{/if}
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
    align-items: center;
    gap: 4px;
    padding: 0 6px 6px;
  }

  .tab {
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    gap: 1px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 4px 10px 4px;
    cursor: pointer;
  }

  .tab:hover {
    color: var(--fg-muted);
  }

  .tab.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  /* A fixed-height shelf under every label, so a count appearing or
     clearing never shifts the row. */
  .under {
    display: flex;
    align-items: baseline;
    gap: 7px;
    height: 13px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
  }

  .ct {
    color: var(--alarm);
  }

  .wct {
    color: var(--marked);
  }

  .bct {
    color: var(--alarm);
    font-size: 9px;
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

  .bubble:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .err {
    margin-left: 10px;
    font-size: 12px;
    color: var(--alarm);
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
