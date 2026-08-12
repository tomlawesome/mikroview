///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Single instance mounted once at the app root (see App.svelte), driven
// entirely by lib/ipLookup.svelte.ts's shared singleton -- fixed-
// positioned from the trigger's own screen coordinates so it always
// renders above LiveTable's scrolling body instead of getting clipped
// by that container's overflow, which an absolutely-positioned popover
// anchored inside the table would be.

import { ipLookupState } from '../lib/ipLookup.svelte'
import ReputationDetails from './ReputationDetails.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  

  const POPOVER_WIDTH = 260

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) ipLookupState.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') ipLookupState.close()
  }

  $effect(() => {
    if (!ipLookupState.anchor) return
    // Deferred past the current click's bubble phase -- the click that
    // opened this (on IpInvestigateButton, a totally separate DOM subtree
    // from this popover) would otherwise still be bubbling up to
    // `document` when this listener attaches, immediately closing what it
    // just opened.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  const style = $derived.by(() => {
    const a = ipLookupState.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 60)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(ipLookupState.anchor){
   { const $$_div0 = svelteHTML.createElement("div", {         "class":`popover`,style,"role":`dialog`,"aria-label":`IP investigation: ${ipLookupState.anchor.ip}`,});popoverEl = $$_div0;
     { svelteHTML.createElement("div", { "class":`header`,});
       { svelteHTML.createElement("span", { "class":`ip`,});ipLookupState.anchor.ip; }
       { svelteHTML.createElement("button", {     "class":`close`,"onclick":() => ipLookupState.close(),"aria-label":`Close`,});  }
     }

    if(ipLookupState.loading){
       { svelteHTML.createElement("div", { "class":`status`,});  }
    } else if (ipLookupState.error){
       { svelteHTML.createElement("div", { "class":`status error`,});ipLookupState.error; }
    } else if (ipLookupState.result){
       { const $$_sliateDnoitatupeR1C = __sveltets_2_ensureComponent(ReputationDetails); new $$_sliateDnoitatupeR1C({ target: __sveltets_2_any(), props: {  "result":ipLookupState.result,}});}
    }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const IpLookupPopover__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type IpLookupPopover__SvelteComponent_ = ReturnType<typeof IpLookupPopover__SvelteComponent_>;
/*Ωignore_endΩ*/export default IpLookupPopover__SvelteComponent_;