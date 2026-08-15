<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  import { uptimeState } from '../lib/uptime.svelte'
  import { formatDurationShort } from '../lib/format'

  uptimeState.start()
</script>

{#if uptimeState.seconds !== null}
  <span
    class="uptime"
    title="How long the mikroview server has been running (since its last restart)"
  >
    up {formatDurationShort(uptimeState.seconds)}
  </span>
{/if}

<style>
  .uptime {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-dim);
    white-space: nowrap;
  }
</style>
