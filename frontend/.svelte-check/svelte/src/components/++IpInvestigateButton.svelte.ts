///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Small per-IP trigger sitting next to the click-to-filter address cell
// (see EventRow.svelte) -- only rendered for public IPs, since the
// backend's own isPublic check (internal/reputation) would just reject
// anything else with ErrNotPublic.

import { ipLookupState } from '../lib/ipLookup.svelte';

;type $$ComponentProps =  { ip: string };function $$render() {

  
  
  
  
  
  

  let { ip }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()
  let btnEl: HTMLButtonElement | undefined = $state()

  function onClick() {
    if (!btnEl) return
    ipLookupState.open(ip, btnEl.getBoundingClientRect())
  }
;
async () => {

  { const $$_button0 = svelteHTML.createElement("button", {         "class":`investigate`,"onclick":onClick,"title":`Investigate ${ip}`,"aria-label":`Investigate ${ip}`,});btnEl = $$_button0;
  
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const IpInvestigateButton__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type IpInvestigateButton__SvelteComponent_ = ReturnType<typeof IpInvestigateButton__SvelteComponent_>;
/*Ωignore_endΩ*/export default IpInvestigateButton__SvelteComponent_;