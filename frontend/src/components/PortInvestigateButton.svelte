<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Small per-port trigger sitting next to the click-to-filter port cell
  // (see EventRow.svelte) -- only rendered for ports with a known entry in
  // lib/commonPorts.ts, mirroring IpInvestigateButton.svelte's
  // only-render-for-public-IPs pattern.
  import { portLookupState } from '../lib/portLookup.svelte'

  let { port }: { port: number } = $props()
  let btnEl: HTMLButtonElement | undefined = $state()

  function onClick() {
    if (!btnEl) return
    portLookupState.open(port, btnEl.getBoundingClientRect())
  }
</script>

<button
  bind:this={btnEl}
  class="investigate"
  onclick={onClick}
  title="What is port {port}?"
  aria-label="What is port {port}?"
>
  i
</button>

<style>
  .investigate {
    flex: none;
    width: 15px;
    height: 15px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    font-family: var(--font-sans);
    font-size: 10px;
    font-weight: 700;
    font-style: italic;
    line-height: 1;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 50%;
    cursor: pointer;
  }

  .investigate:hover {
    background: var(--accent-bg-hover);
  }
</style>
