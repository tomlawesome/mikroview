///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Single instance mounted once at the app root (see App.svelte) -- same
// fixed-position-from-trigger-coordinates approach as IpLookupPopover.svelte,
// driven by lib/portLookup.svelte.ts's shared singleton.

import { portLookupState } from '../lib/portLookup.svelte';
function $$render() {

  
  
  
  
  

  const POPOVER_WIDTH = 260

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) portLookupState.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') portLookupState.close()
  }

  $effect(() => {
    if (!portLookupState.anchor) return
    // Deferred past the current click's bubble phase, same reasoning as
    // IpLookupPopover.svelte -- the opening click would otherwise still be
    // bubbling up to `document` when this listener attaches.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  const style = $derived.by(() => {
    const a = portLookupState.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 60)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(portLookupState.anchor){
   { const $$_div0 = svelteHTML.createElement("div", {         "class":`popover`,style,"role":`dialog`,"aria-label":`Port lookup: ${portLookupState.anchor.port}`,});popoverEl = $$_div0;
     { svelteHTML.createElement("div", { "class":`header`,});
       { svelteHTML.createElement("span", { "class":`port`,}); portLookupState.anchor.port; }
       { svelteHTML.createElement("button", {     "class":`close`,"onclick":() => portLookupState.close(),"aria-label":`Close`,});  }
     }

    if(portLookupState.results.length === 0){
       { svelteHTML.createElement("div", { "class":`status`,});       }
    }else{
       { svelteHTML.createElement("div", { "class":`entries`,});
           for(let r of __sveltets_2_ensureArray(portLookupState.results)){r.name;
           { svelteHTML.createElement("div", { "class":`entry`,});
             { svelteHTML.createElement("div", { "class":`entry-header`,});
               { svelteHTML.createElement("span", { "class":`name`,});r.name; }
               { svelteHTML.createElement("span", { "class":`badge cat-${r.category}`,});r.category; }
             }
             { svelteHTML.createElement("div", { "class":`proto`,});r.protocol; }
             { svelteHTML.createElement("div", { "class":`desc`,});r.description; }
           }
        }
       }
    }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const PortLookupPopover__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type PortLookupPopover__SvelteComponent_ = ReturnType<typeof PortLookupPopover__SvelteComponent_>;
/*Ωignore_endΩ*/export default PortLookupPopover__SvelteComponent_;