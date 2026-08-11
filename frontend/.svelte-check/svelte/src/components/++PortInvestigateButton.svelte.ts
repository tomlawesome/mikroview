///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Small per-port trigger sitting next to the click-to-filter port cell
// (see EventRow.svelte) -- only rendered for ports with a known entry in
// lib/commonPorts.ts, mirroring IpInvestigateButton.svelte's
// only-render-for-public-IPs pattern.

import { portLookupState } from '../lib/portLookup.svelte';

;type $$ComponentProps =  { port: number };function $$render() {

  
  
  
  
  
  

  let { port }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()
  let btnEl: HTMLButtonElement | undefined = $state()

  function onClick() {
    if (!btnEl) return
    portLookupState.open(port, btnEl.getBoundingClientRect())
  }
;
async () => {

  { const $$_button0 = svelteHTML.createElement("button", {         "class":`investigate`,"onclick":onClick,"title":`What is port ${port}?`,"aria-label":`What is port ${port}?`,});btnEl = $$_button0;
  
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const PortInvestigateButton__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type PortInvestigateButton__SvelteComponent_ = ReturnType<typeof PortInvestigateButton__SvelteComponent_>;
/*Ωignore_endΩ*/export default PortInvestigateButton__SvelteComponent_;