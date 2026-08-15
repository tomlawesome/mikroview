<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from '../lib/state.svelte'
  import { groupModeState } from '../lib/groupMode.svelte'
  import { formatEps, formatBufferDepth } from '../lib/format'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import DeviceStatus from './DeviceStatus.svelte'
  import UptimeBadge from './UptimeBadge.svelte'
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
    <button
      class="logo-button"
      onclick={() => (appState.view = 'live')}
      title="Back to live view"
      aria-label="Back to live view"
    >
      <LogoLockup size={21} />
    </button>
    <ConnectionIndicator />
    <UptimeBadge />
  </div>

  <DeviceStatus />

  <div class="controls">
    {#if appState.view === 'live'}
      {#if appState.stats}
        <span class="eps" title="Events per second (10s rolling average)">
          {formatEps(appState.stats.eventsPerSecond)}/s
        </span>
        <span
          class="buffer-depth"
          title="The server's event buffer holds up to {appState.stats.capacity.toLocaleString()} events. Once full, each new event overwrites the oldest -- this is how far back it actually reaches at the current rate, not the configured retention window."
        >
          {formatBufferDepth(appState.stats.capacity, appState.stats.count, appState.stats.eventsPerSecond)}
        </span>
        {#if appState.stats.syslog && appState.stats.syslog.rejectedConfigured > 0}
          <!-- Only shown when one of YOUR routers was turned away. The
               listener being busy is not itself a problem; a device you
               told MikroView to watch not getting through is. -->
          <span
            class="syslog-blocked"
            title="MikroView has turned away {appState.stats.syslog.rejectedConfigured} connection attempt(s) from a router listed in your config, because its syslog connection slots were full ({appState.stats.syslog.inUse} of {appState.stats.syslog.capacity} in use). Those log lines never arrived. This usually means something is opening a lot of connections to the syslog port."
          >
            ⚠ syslog full
          </span>
        {/if}
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

      <!-- Grouping (#341): collapse repeats of the same connection into
           one row with a count, so a host retrying the same thing four
           hundred times costs one line. An option on the live view, not
           a different view -- every event is still there, and the row
           opens to show them. -->
      <button
        class:active={groupModeState.enabled}
        onclick={() => groupModeState.toggle()}
        title={groupModeState.enabled
          ? 'Show every event on its own row'
          : 'Collapse repeats of the same connection into one row with a count'}
      >
        Group
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

  .logo-button {
    display: inline-flex;
    background: none;
    border: none;
    padding: 0;
    margin: 0;
    cursor: pointer;
    border-radius: 5px;
  }

  .logo-button:hover {
    opacity: 0.85;
  }

  .logo-button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
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
  }

  .buffer-depth {
    font-size: 12px;
    color: var(--fg-muted);
    padding-right: 10px;
    border-right: 1px solid var(--border);
  }

  /* Deliberately not muted: this is the one item here that means
     something is wrong right now, rather than reporting a rate. */
  .syslog-blocked {
    font-size: 12px;
    font-weight: 600;
    color: var(--danger, #c0392b);
    padding-right: 10px;
    border-right: 1px solid var(--border);
    cursor: help;
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
