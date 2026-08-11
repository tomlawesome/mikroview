///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
//
// Appearance as a standalone, always-visible control (issue #137).
//
// This existed before #73 consolidated the toolbar into NavMenu, and
// burying it there was a regression rather than a tidy-up: theme and
// colorway switching is reached for often and wants to be one click
// away, not two. Reinstated in the pre-#73 shape, now also carrying
// the light/dark/auto mode picker that was split out separately.
//
// At mobile widths the dropdown becomes a bottom sheet, for the same
// reason NavMenu's does (issue #85): a right-anchored dropdown assumes
// the trigger stays at the toolbar's right edge, and a wrapped toolbar
// can put it anywhere.

import { COLORWAYS, colorwayState } from '../lib/colorway.svelte'
import { themeState, type ThemePref } from '../lib/theme.svelte'
import { viewportState } from '../lib/viewport.svelte';
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

  const current = $derived(COLORWAYS.find((c) => c.id === colorwayState.pref) ?? COLORWAYS[0])

  const modeLabels: Record<ThemePref, string> = { system: 'Auto', light: 'Light', dark: 'Dark' }
  const modeOptions: ThemePref[] = ['system', 'light', 'dark']
;
async () => {

 { const $$_div0 = svelteHTML.createElement("div", {  "class":`theme-menu`,});rootEl = $$_div0;
   { svelteHTML.createElement("button", {           "class":`trigger`,"onclick":() => (open = !open),"aria-haspopup":`true`,"aria-expanded":open,"title":`Colour theme and light/dark mode`,});
     { svelteHTML.createElement("span", {   "class":`swatch`,"style":`background: ${current.swatch}`,}); }
    
   }

  if(open){
    if(viewportState.isMobile){
       { svelteHTML.createElement("div", {     "class":`scrim`,"onclick":() => (open = false),"role":`presentation`,}); }
    }
     { svelteHTML.createElement("div", {    "class":`menu`,"role":`menu`,});viewportState.isMobile;
      if(viewportState.isMobile){
         { svelteHTML.createElement("div", { "class":`handle`,}); }
      }

         for(let c of __sveltets_2_ensureArray(COLORWAYS)){c.id;
         { svelteHTML.createElement("button", {          "class":`option`,"role":`menuitemradio`,"aria-checked":c.id === colorwayState.pref,"onclick":() => {
            colorwayState.set(c.id)
            open = false
          },});c.id === colorwayState.pref;
           { svelteHTML.createElement("span", {   "class":`swatch`,"style":`background: ${c.swatch}`,}); }
          c.label;
         }
      }

       { svelteHTML.createElement("div", { "class":`divider`,}); }

         for(let m of __sveltets_2_ensureArray(modeOptions)){m;
         { svelteHTML.createElement("button", {          "class":`option`,"role":`menuitemradio`,"aria-checked":m === themeState.pref,"onclick":() => {
            themeState.pref = m
            themeState.apply()
            open = false
          },});m === themeState.pref;
          modeLabels[m];
         }
      }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const ThemeMenu__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type ThemeMenu__SvelteComponent_ = ReturnType<typeof ThemeMenu__SvelteComponent_>;
/*Ωignore_endΩ*/export default ThemeMenu__SvelteComponent_;