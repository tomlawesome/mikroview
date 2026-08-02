<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import { MAX_RENDERED_ROWS } from '../lib/constants'
  import EventRow from './EventRow.svelte'

  let bodyEl: HTMLDivElement | undefined = $state()

  const deviceNames = $derived.by(() => {
    const map = new Map<string, string>()
    for (const d of appState.devices) map.set(d.id, d.name)
    return map
  })

  const rendered = $derived(appState.filteredEvents.slice(-MAX_RENDERED_ROWS))

  function deviceName(id: string): string {
    return deviceNames.get(id) ?? id
  }

  $effect(() => {
    rendered.length // re-run this effect whenever the rendered set changes
    if (appState.autoscroll && !appState.paused && bodyEl) {
      requestAnimationFrame(() => {
        if (bodyEl) bodyEl.scrollTop = bodyEl.scrollHeight
      })
    }
  })
</script>

<div class="table-wrap">
  <div class="header">
    <span>Time</span>
    <span>Device</span>
    <span>Action</span>
    <span>Chain</span>
    <span>Source</span>
    <span>Destination</span>
    <span>Proto</span>
    <span>Interfaces</span>
    <span>Rule</span>
  </div>
  <div class="body scrollbar" bind:this={bodyEl}>
    <div class="grid">
      {#each rendered as event (event.id)}
        <EventRow {event} deviceName={deviceName(event.deviceId)} />
      {/each}
    </div>
    {#if rendered.length === 0}
      <div class="empty">
        {appState.events.length === 0
          ? 'Waiting for events…'
          : 'No events match the current filters.'}
      </div>
    {/if}
  </div>
</div>

<style>
  .table-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }

  .header,
  .grid {
    display: grid;
    grid-template-columns: 88px 130px 78px 74px 1fr 1fr 62px 140px 1fr;
  }

  .header {
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
  }

  .header span {
    padding: 8px 10px;
    font-size: 10.5px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--fg-dim);
    font-weight: 600;
  }

  .body {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
  }

  .empty {
    padding: 48px 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 13px;
  }
</style>
