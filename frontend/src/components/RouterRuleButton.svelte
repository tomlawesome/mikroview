<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Per-row trigger for the pushed rule/NAT table lookup (issue #186
  // step 4c -- the owner's "lookup button like the abuse IP stuff"
  // proposal), sitting next to the click-to-filter rule and NAT cells in
  // EventRow.svelte, same shape as IpInvestigateButton/
  // PortInvestigateButton. Rendered unconditionally where the cell has a
  // value: whether the device has pushed a table is only known
  // server-side, and the popover itself says "no data pushed yet" -- a
  // button that silently vanished for un-pushed devices would make the
  // feature look broken rather than unconfigured.
  import { routerLookupState } from '../lib/routerLookup.svelte'

  let {
    mode,
    device,
    ruleLabel = '',
  }: { mode: 'rule' | 'nat'; device: string; ruleLabel?: string } = $props()

  let btnEl: HTMLButtonElement | undefined = $state()

  const label = $derived(
    mode === 'rule' ? `Look up rule for prefix ${ruleLabel}` : 'Show this router’s NAT table',
  )

  function onClick() {
    if (!btnEl) return
    const rect = btnEl.getBoundingClientRect()
    if (mode === 'rule') routerLookupState.openRule(device, ruleLabel, rect)
    else routerLookupState.openNat(device, rect)
  }
</script>

<button bind:this={btnEl} class="investigate" onclick={onClick} title={label} aria-label={label}>
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
