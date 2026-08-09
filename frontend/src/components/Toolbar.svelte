<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from '../lib/state.svelte'
  import { formatEps } from '../lib/format'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import DeviceStatus from './DeviceStatus.svelte'
  import LogoLockup from './LogoLockup.svelte'
  import NavMenu from './NavMenu.svelte'
  import ThemeMenu from './ThemeMenu.svelte'

  function onMaxAgeChange(e: Event) {
    const raw = (e.target as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }
</script>

<header class="toolbar">
  <div class="brand">
    <LogoLockup size={21} />
    <ConnectionIndicator />
  </div>

  <DeviceStatus />

  <div class="controls">
    {#if appState.view === 'live'}
      {#if appState.stats}
        <span class="eps" title="Events per second (10s rolling average)">
          {formatEps(appState.stats.eventsPerSecond)}/s
        </span>
      {/if}

      {#if !viewportState.isMobile}
        <select
          value={retentionState.maxAgeSeconds === null ? 'null' : String(retentionState.maxAgeSeconds)}
          onchange={onMaxAgeChange}
          title="How long events stay visible in the live view"
          aria-label="Display duration"
        >
          {#each MAX_AGE_OPTIONS as opt (opt.value)}
            <option value={opt.value === null ? 'null' : String(opt.value)}>{opt.label}</option>
          {/each}
        </select>
      {/if}

      <button
        class:active={appState.autoscroll}
        onclick={() => (appState.autoscroll = !appState.autoscroll)}
        title={appState.autoscroll
          ? 'Auto-scroll to newest events'
          : 'Hold the current view -- new events keep arriving but the table stays put'}
      >
        Autoscroll
      </button>

      <button
        class:active={appState.paused}
        onclick={() => appState.togglePause()}
        title={appState.paused ? 'Resume live updates' : 'Pause live updates'}
      >
        {appState.paused ? `Resume${appState.pendingCount ? ` (${appState.pendingCount})` : ''}` : 'Pause'}
      </button>

      <button onclick={() => appState.clearBuffer()} title="Clear the local event buffer">
        Clear
      </button>
    {/if}

    <!-- Appearance stays standalone and always visible (issue #137):
         #73's inline-vs-menu split filed it under "everything else",
         but theme switching is reached for constantly and wants to be
         one click away. Export went the other way -- an occasional,
         deliberate action that was holding an inline slot -- and now
         lives in NavMenu on both breakpoints, where mobile already had
         it. -->
    <ThemeMenu />
    <NavMenu />
  </div>
</header>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    flex-wrap: wrap;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin-left: auto;
  }

  .eps {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-muted);
    padding-right: 10px;
    border-right: 1px solid var(--border);
  }

  button,
  select {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
  }

  select {
    background: var(--bg);
  }

  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  button:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  button.active:hover {
    background: var(--accent-bg-hover);
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  button:disabled:hover {
    color: var(--fg-muted);
    border-color: var(--border);
  }

  /* 44px minimum touch target (issue #85) for the toolbar's own
     always-inline live-view controls -- desktop's 7px vertical padding
     above is comfortable with a mouse but too cramped to tap
     reliably. Scoped to this component's own buttons/select only
     (Svelte's default style scoping), not a global rule that could
     collide with smaller fixed-size icon buttons elsewhere. */
  @media (max-width: 700px) {
    button,
    select {
      min-height: 44px;
    }
  }
</style>
