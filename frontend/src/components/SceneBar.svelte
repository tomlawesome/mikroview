<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Every scene's own bar, under the Atlas identity (owner, 2026-08-29:
  // pages are the site -- no persistent chrome). Carries the wordmark,
  // the scene's name, the live status cluster, the account chip (#633:
  // navigation to the operate pages lives on its menu; the deck and
  // roll rail cover the scenes), and -- on the stream -- the controls
  // the retired toolbar used to hold, unchanged in behaviour.
  //
  // Inside the deck every card carries its own bar, so the scene named
  // here is the card's own, passed as a prop; outside the deck (the
  // operate pages) it defaults to the current view.
  import { appState, type View } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { groupModeState } from '../lib/groupMode.svelte'
  import { formatEps, formatBufferDepth } from '../lib/format'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import DeviceStatus from './DeviceStatus.svelte'
  import UptimeBadge from './UptimeBadge.svelte'
  import AccountMenu from './AccountMenu.svelte'

  let { scene = null }: { scene?: View | null } = $props()
  const view = $derived(scene ?? appState.view)

  const TITLES: Record<string, string> = {
    live: 'Stream',
    metrics: 'Metrics',
    audit: 'The docket',
    flags: 'The docket',
    watchlist: 'The docket',
    engineroom: 'Settings',
    fleet: 'Fleet',
    entities: 'Entities',
  }

  function onMaxAgeChange(e: Event) {
    const raw = (e.target as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }
</script>

<div class="scene-bar">
  <span class="wm">MIKRO<em>VIEW</em></span>
  <h1>{TITLES[view] ?? ''}</h1>
  <ConnectionIndicator />
  <UptimeBadge />
  <DeviceStatus />
  <!-- The chrome's alarm pair, per the ratified navigation record: the
       open-flag count badge and the broken ring are the two signals an
       operator must see without opening anything. They lived on the
       rail; with the rail retired (#633) the scene bar is the chrome. -->
  {#if flagsState.activeCount > 0}
    <button class="flag-badge" onclick={() => (appState.view = 'flags')} title="Open flags">
      {flagsState.activeCount}
    </button>
  {/if}
  {#if watchlistState.brokenCount > 0}
    <button
      class="ring"
      onclick={() => (appState.view = 'watchlist')}
      title="A watch is broken"
      aria-label="A watch is broken — open the watchlist"
    ></button>
  {/if}

  <div class="controls">
    {#if view === 'live'}
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

      {#if !viewportState.isMobile}
        <button
          class:active={groupModeState.enabled}
          onclick={() => groupModeState.toggle()}
          title={groupModeState.enabled
            ? 'Show every event on its own row'
            : 'Collapse repeats of the same connection into one row with a count'}
        >
          Group
        </button>
      {/if}

      <button onclick={() => appState.clearBuffer()} title="Clear the local event buffer">
        Clear
      </button>
    {/if}
    <AccountMenu />
  </div>
</div>

<style>
  .scene-bar {
    display: flex;
    align-items: baseline;
    gap: 16px;
    padding: 12px 20px 4px;
    flex-wrap: wrap;
  }
  .wm {
    font-size: 13px;
    font-weight: 800;
    letter-spacing: 0.22em;
    color: var(--fg-dim);
  }
  .wm em {
    color: var(--accent);
    font-style: normal;
  }
  h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    color: var(--fg);
  }

  .controls {
    margin-left: auto;
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .eps,
  .buffer-depth {
    font-size: 12px;
    color: var(--fg-dim);
    font-family: var(--font-mono);
  }
  .syslog-blocked {
    font-size: 12px;
    color: var(--alarm);
  }
  .controls select,
  .controls button {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--fg-muted);
    font-size: 13px;
    padding: 4px 12px;
  }
  .controls select:hover,
  .controls button:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }
  .controls button.active {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--accent-bg);
  }
  .flag-badge {
    background: var(--alarm);
    color: var(--bg);
    border: none;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
    padding: 0 8px;
    line-height: 17px;
    cursor: pointer;
  }
  .ring {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 2px solid var(--alarm);
    background: transparent;
    padding: 0;
    cursor: pointer;
  }
</style>
