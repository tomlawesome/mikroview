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
  //
  // In 'nat' mode the button carries the event with it (#445). The popup
  // has to answer a question about *this translation*, not about the
  // table in the abstract: which rule the operator's log-prefix names,
  // or -- when there is no prefix -- which rules this event rules out.
  // Both need the event, so the trigger passes it rather than the popup
  // reaching back for "the current row", which in a stream that is still
  // moving is not a thing that exists.
  import { routerLookupState, type NatEvidence } from '../lib/routerLookup.svelte'
  import { natFactsFromEvent } from '../lib/natMatch'
  import type { FirewallEvent } from '../lib/types'

  let {
    mode,
    device,
    ruleLabel = '',
    event,
    evidence = 'row',
  }: {
    mode: 'rule' | 'nat'
    device: string
    ruleLabel?: string
    event?: FirewallEvent
    evidence?: NatEvidence
  } = $props()

  let btnEl: HTMLButtonElement | undefined = $state()

  // The NAT label promises only what the popup will actually deliver.
  // "Look up the NAT rule" on an untagged translation would be a
  // promise the product cannot keep -- there is no rule to look up, only
  // a table to narrow -- so the untagged wording says narrow, and the
  // popup then says the same thing again in its header and chip.
  const label = $derived(
    mode === 'rule'
      ? `Look up rule for prefix ${ruleLabel}`
      : ruleLabel
        ? `Look up the NAT rule logged as ${ruleLabel}`
        : 'Narrow down which NAT rule did this',
  )

  function onClick() {
    if (!btnEl) return
    const rect = btnEl.getBoundingClientRect()
    if (mode === 'rule' || !event) {
      routerLookupState.openRule(device, ruleLabel, rect)
      return
    }
    routerLookupState.openNat(device, rect, {
      ruleLabel,
      facts: natFactsFromEvent(event),
      evidence,
    })
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
