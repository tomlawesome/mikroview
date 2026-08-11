///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Per-row trigger for the pushed rule/NAT table lookup (issue #186
// step 4c -- the owner's "lookup button like the abuse IP stuff"
// proposal), sitting next to the click-to-filter rule and NAT cells in
// EventRow.svelte, same shape as IpInvestigateButton/
// PortInvestigateButton. Rendered unconditionally where the cell has a
// value: whether the device has pushed a table is only known
// server-side, and the popover itself says "no data pushed yet" -- a
// button that silently vanished for un-pushed devices would make the
// feature look broken rather than unconfigured.

import { routerLookupState } from '../lib/routerLookup.svelte';

;type $$ComponentProps =  { mode: 'rule' | 'nat'; device: string; ruleLabel?: string };function $$render() {

  
  
  
  
  
  
  
  
  
  
  

  let {
    mode,
    device,
    ruleLabel = '',
  }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

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
;
async () => {

 { const $$_button0 = svelteHTML.createElement("button", {         "class":`investigate`,"onclick":onClick,"title":label,"aria-label":label,});btnEl = $$_button0;
  
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const RouterRuleButton__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type RouterRuleButton__SvelteComponent_ = ReturnType<typeof RouterRuleButton__SvelteComponent_>;
/*Ωignore_endΩ*/export default RouterRuleButton__SvelteComponent_;