<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import { formatEps } from '../lib/format'
  import { themeState } from '../lib/theme.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import DeviceStatus from './DeviceStatus.svelte'
  import LogoLockup from './LogoLockup.svelte'
  import ThemeMenu from './ThemeMenu.svelte'

  const modeLabels = { system: 'Auto', light: 'Light', dark: 'Dark' }
</script>

<header class="toolbar">
  <div class="brand">
    <LogoLockup size={21} />
    <ConnectionIndicator />
  </div>

  <DeviceStatus />

  <div class="controls">
    {#if appState.stats}
      <span class="eps" title="Events per second (10s rolling average)">
        {formatEps(appState.stats.eventsPerSecond)}/s
      </span>
    {/if}

    <button
      class:active={appState.autoscroll}
      onclick={() => (appState.autoscroll = !appState.autoscroll)}
      title="Auto-scroll to newest events"
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

    <ThemeMenu />

    <button
      onclick={() => themeState.cycle()}
      title="Cycle light/dark mode: system → light → dark"
    >
      {modeLabels[themeState.pref]}
    </button>
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

  button {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
  }

  button:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.active {
    color: var(--accent);
    border-color: var(--accent);
  }
</style>
