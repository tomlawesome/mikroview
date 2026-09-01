<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The docket (#633, rounds 17-19/25/28-29): what was flagged, what
  // you watch, what changed -- flags, watchlist and audit log as one
  // deck card's tabs -- the tabs themselves ride the scene bar since
  // #700. What stays on the page is the flags tab's one control, the
  // clear-all bubble (round 29, owner-ratified): an outlined amber
  // bubble; one click arms it alarm-red 'confirm'; the second click
  // clears every open flag; clicking anywhere else disarms it, so an
  // armed bubble cannot ambush a stray click.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import Flags from './Flags.svelte'
  import Watchlist from './Watchlist.svelte'
  import AuditLog from './AuditLog.svelte'

  const isAdmin = $derived(authState.role === 'admin')
  // #653: watchlist and clearing flags are user-tier; the audit log
  // stays owner-level. Absent rather than disabled, as everywhere else.
  const canEdit = $derived(authState.canEdit)

  // The docket's tab follows the app view, so a deep link (the scene
  // bar's flag badge, the broken ring, the menu's Audit log row) lands
  // on the right tab; clicking a tab is just a view change.
  type Tab = 'flags' | 'watchlist' | 'audit'
  const tab = $derived.by((): Tab => {
    if (appState.view === 'watchlist' && canEdit) return 'watchlist'
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
  <!-- The tabs moved to the scene bar (#700): round 30 rides them where
       the page heading used to be, beside the wordmark. What stays here
       is the flags tab's own control -- the clear-all bubble, which
       round 30 draws as its own row under the bar (its `.clearall`).
       Outlined, never filled; one click arms it 'confirm', the second
       clears, and clicking elsewhere disarms it. -->
  <div class="clear-row">
    {#if tab === 'flags' && flagsState.activeCount > 0 && canEdit}
      <button class="bubble" class:armed disabled={busy} onclick={onClearAll} title="They keep their place in the audit log">
        {armed ? 'confirm' : 'clear all'}
      </button>
    {/if}
    <!-- The watchlist tab's own panel-level action (#761, round 31):
         the same slot and pill `clear all` uses, the watch's own ink.
         Opening the draft is Watchlist.svelte's own private state --
         out of reach from here -- so this just fires the shared
         handoff (topologyNav.svelte.ts) the same way a flag's own
         `watch this pathway`/`watch this source` does. -->
    {#if tab === 'watchlist' && canEdit}
      <button
        class="bubble watch"
        onclick={(e) => {
          e.stopPropagation()
          topologyNavState.requestNewWatch()
        }}
        title="A new watch -- written in place, at the top of the table"
      >
        + watch
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

  .clear-row {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 0 6px 6px;
    min-height: 26px;
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

  /* `+ watch` (round 31, `.cabtn.wbtn` in the record): same pill, the
     watch's own ink -- --marked stands in for the record's #a78bfa, the
     watchers' purple every other watch-state chip already uses. */
  .bubble.watch {
    color: var(--marked);
    border-color: color-mix(in srgb, var(--marked) 55%, transparent);
  }

  .bubble.watch:hover {
    background: color-mix(in srgb, var(--marked) 8%, transparent);
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
