<script lang="ts">
  import { appState } from '../lib/state.svelte'
</script>

{#if appState.connState !== 'open'}
  <div class="banner banner-{appState.connState}" role="status">
    {appState.connState === 'connecting'
      ? 'Connecting to mikroview…'
      : 'Disconnected from server — attempting to reconnect…'}
  </div>
{:else if appState.wsDropped > 0}
  <div class="banner banner-warning" role="status">
    Server is dropping events under load — {appState.wsDropped}
    {appState.wsDropped === 1 ? 'event has' : 'events have'} been lost from the live feed this connection.
  </div>
{/if}

<style>
  .banner {
    padding: 8px 16px;
    font-size: 13px;
    text-align: center;
    border-bottom: 1px solid var(--border);
  }

  .banner-connecting {
    background: var(--row-log-bg);
    color: var(--log);
  }

  .banner-closed {
    background: var(--row-reject-bg);
    color: var(--reject);
  }

  .banner-warning {
    background: var(--row-drop-bg);
    color: var(--drop);
  }
</style>
