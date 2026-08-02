<script lang="ts">
  import EventsChart from './EventsChart.svelte'

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })
</script>

<div class="chart-menu" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Event volume over time"
  >
    Chart
  </button>

  {#if open}
    <div class="panel" role="dialog" aria-label="Event volume chart">
      <EventsChart />
    </div>
  {/if}
</div>

<style>
  .chart-menu {
    position: relative;
  }

  .trigger {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .panel {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 14px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 20;
  }
</style>
