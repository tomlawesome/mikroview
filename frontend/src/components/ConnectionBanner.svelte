<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1015: this used to also carry the #995 ingest-loss banner family --
  // that half moved to IngestLossDrawer.svelte, which overlays the top
  // of the content column instead of pushing it (App.svelte's `.content`
  // is the drawer's positioning context now). What is left here is the
  // one line that still belongs in flow: the socket's own connecting/
  // disconnected state, which is about this tab's link to the server,
  // not about data the server itself is losing.
  import { appState } from '../lib/state.svelte'
</script>

{#if appState.connState !== 'open'}
  <div class="banner banner-{appState.connState}" role="status">
    {appState.connState === 'connecting'
      ? 'Connecting to mikroview…'
      : 'Disconnected from server — attempting to reconnect…'}
  </div>
{/if}

<style>
  .banner {
    position: relative;
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
</style>
