<script lang="ts">
  // Small per-IP trigger sitting next to the click-to-filter address cell
  // (see EventRow.svelte) -- only rendered for public IPs, since the
  // backend's own isPublic check (internal/reputation) would just reject
  // anything else with ErrNotPublic.
  import { ipLookupState } from '../lib/ipLookup.svelte'

  let { ip }: { ip: string } = $props()
  let btnEl: HTMLButtonElement | undefined = $state()

  function onClick() {
    if (!btnEl) return
    ipLookupState.open(ip, btnEl.getBoundingClientRect())
  }
</script>

<button
  bind:this={btnEl}
  class="investigate"
  onclick={onClick}
  title="Investigate {ip}"
  aria-label="Investigate {ip}"
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
