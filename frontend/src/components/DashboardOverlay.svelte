<script lang="ts">
  // Large centered modal for the dashboard charts, layered over the live
  // view rather than replacing it -- keeps the live table's state (scroll
  // position, pause, filters) untouched underneath while the dashboard is
  // open, and closing it is just closing an overlay, not navigating back.
  import { appState } from '../lib/state.svelte'
  import Dashboard from './Dashboard.svelte'

  function close() {
    appState.dashboardOpen = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  // Only close when the backdrop itself is clicked, not a bubbled click
  // from inside the modal -- avoids needing a second click handler (and
  // the a11y warning that comes with attaching one) just to stop it.
  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if appState.dashboardOpen}
  <div class="backdrop" onclick={onBackdropClick} role="presentation">
    <div class="modal" role="dialog" aria-modal="true" aria-label="Dashboard" tabindex="-1">
      <div class="modal-header">
        <span class="title">Dashboard</span>
        <button class="close" onclick={close} aria-label="Close dashboard">✕</button>
      </div>
      <Dashboard />
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 5vh 4vw;
    z-index: 50;
  }

  .modal {
    width: 100%;
    height: 100%;
    max-width: 1400px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    min-height: 0;
    box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
    flex: none;
  }

  .title {
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }

  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 28px;
    height: 28px;
    font-size: 13px;
    line-height: 1;
  }

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
