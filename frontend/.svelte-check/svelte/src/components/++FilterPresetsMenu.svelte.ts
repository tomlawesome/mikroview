///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from '../lib/state.svelte'
import { presetState } from '../lib/presets.svelte'
import type { Filters } from '../lib/types';
function $$render() {

  
  
  
  

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })

  function apply(filters: Filters) {
    appState.filters = { ...filters }
    open = false
  }

  function saveCurrent() {
    const name = window.prompt('Save current filters as:')
    if (name) presetState.save(name, appState.filters)
  }

  function remove(e: MouseEvent, name: string) {
    e.stopPropagation()
    presetState.remove(name)
  }
;
async () => {

 { const $$_div0 = svelteHTML.createElement("div", {  "class":`presets`,});rootEl = $$_div0;
   { svelteHTML.createElement("button", {           "class":`trigger`,"onclick":() => (open = !open),"aria-haspopup":`true`,"aria-expanded":open,"title":`Saved filter presets`,});
    
   }

  if(open){
     { svelteHTML.createElement("div", {   "class":`menu`,"role":`menu`,});
       { svelteHTML.createElement("button", {     "class":`save`,"onclick":saveCurrent,"disabled":!appState.hasActiveFilters,});
          
       }

      if(presetState.presets.length > 0){
         { svelteHTML.createElement("div", { "class":`divider`,}); }
           for(let p of __sveltets_2_ensureArray(presetState.presets)){p.name;
           { svelteHTML.createElement("div", { "class":`row`,});
             { svelteHTML.createElement("button", {     "class":`option`,"role":`menuitem`,"onclick":() => apply(p.filters),});
              p.name;
             }
             { svelteHTML.createElement("button", {       "class":`remove`,"onclick":(e) => remove(e, p.name),"title":`Delete preset`,"aria-label":`Delete preset ${p.name}`,});
              
             }
           }
        }
      }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const FilterPresetsMenu__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type FilterPresetsMenu__SvelteComponent_ = ReturnType<typeof FilterPresetsMenu__SvelteComponent_>;
/*Ωignore_endΩ*/export default FilterPresetsMenu__SvelteComponent_;