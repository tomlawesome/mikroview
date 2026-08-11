///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Suggestions (#243 slice 5): watchlist entries suggested from data
// RouterOS has already pushed (named DHCP leases, ports an existing
// rule already blocks) -- so an operator doesn't have to already know
// what to watch before this feature is useful. Every candidate is one
// of three states, never a binary accept/reject:
//
//  - Off -- undecided. The default for every newly generated
//    candidate, and the default view here.
//  - On -- accepted; a real watchlist entry now exists for it (see
//    Watchlist.svelte).
//  - Hide -- explicitly declined, but reversible only by deliberately
//    switching to this view and flipping it back. Never reappears on
//    its own.
//
// Candidates are kept in sync with the router automatically in the
// background (internal/suggest.Store.RunPeriodicSync) -- there is
// deliberately no manual "refresh" button here, see that method's own
// doc comment for why a separate soft-refresh control would be
// redundant.
//
// Admin-only throughout, same tier as Watchlist.svelte itself -- a
// candidate's justification names a specific rule or device, the same
// class of administrative network information.

import { onMount } from 'svelte'
import { suggestState } from '../lib/suggest.svelte'
import type { Suggestion, SuggestionStatus } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  onMount(() => {
    suggestState.refresh()
  })

  let filter = $state<SuggestionStatus>('off')

  let visible = $derived(suggestState.candidates.filter((c) => c.status === filter))

  let acceptingId = $state<string | null>(null)
  let hidingId = $state<string | null>(null)
  let unhidingId = $state<string | null>(null)
  let actionError = $state<string | null>(null)

  async function accept(c: Suggestion) {
    acceptingId = c.id
    actionError = null
    try {
      const result = await suggestState.accept(c.id)
      if (typeof result === 'string') actionError = result
    } finally {
      acceptingId = null
    }
  }

  async function hide(c: Suggestion) {
    hidingId = c.id
    actionError = null
    try {
      const err = await suggestState.hide(c.id)
      if (err) actionError = err
    } finally {
      hidingId = null
    }
  }

  async function unhide(c: Suggestion) {
    unhidingId = c.id
    actionError = null
    try {
      const err = await suggestState.unhide(c.id)
      if (err) actionError = err
    } finally {
      unhidingId = null
    }
  }

  // --- Nuke -----------------------------------------------------------
  // Deliberately the most alarming control on this page: it destroys
  // every watchlist entry, not just suggestion-tracking state, and
  // cannot be undone (#243 slice 5 design: "gated behind a real confirm
  // step and an unmistakably serious warning"). A native confirm()
  // dialog forces a synchronous, explicit choice the same way
  // Watchlist.svelte's own entry-removal confirm does, but with wording
  // proportionate to what is actually at stake here.
  let resetting = $state(false)

  async function resetEverything() {
    const entryCount = suggestState.countByStatus('on')
    const warning =
      `This permanently deletes every watchlist entry` +
      (entryCount > 0 ? ` -- including the ${entryCount} you've already accepted` : '') +
      `, and cannot be undone. A fresh set of suggestions will be generated from what your router reports right now.\n\n` +
      `Type OK only if you are certain.`
    if (!confirm(warning)) return
    resetting = true
    actionError = null
    try {
      const err = await suggestState.reset()
      if (err) actionError = err
      else filter = 'off'
    } finally {
      resetting = false
    }
  }

  function kindLabel(c: Suggestion): string {
    switch (c.kind) {
      case 'device':
        return 'device'
      case 'port':
        return 'port'
      default:
        return c.kind
    }
  }

  function detailLabel(c: Suggestion): string {
    if (c.kind === 'device') {
      return c.source?.mac || c.source?.ip || 'unknown device'
    }
    if (c.kind === 'port') {
      return `port ${(c.ports ?? []).join(', ')}`
    }
    return c.addressList ?? ''
  }
;
async () => {

 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
                  
                     
      { svelteHTML.createElement("strong", {});  }           { svelteHTML.createElement("strong", {});  }    
                   
             
   }

   { svelteHTML.createElement("div", { "class":`toolbar`,});
     { svelteHTML.createElement("div", { "class":`filters`,});
         for(let [status, label] of __sveltets_2_ensureArray(([['off', 'Undecided'], ['on', 'Accepted'], ['hide', 'Hidden']]))){status;
         { svelteHTML.createElement("button", {      "class":`filter`,"onclick":() => (filter = status as SuggestionStatus),});filter === status;
          label;
           { svelteHTML.createElement("span", { "class":`count`,});suggestState.countByStatus(status as SuggestionStatus); }
         }
      }
     }
     { svelteHTML.createElement("button", {     "class":`nuke`,"disabled":resetting,"onclick":resetEverything,});
      resetting ? 'Resetting…' : 'Reset everything (cannot be undone)';
     }
   }

  if(actionError){
     { svelteHTML.createElement("p", { "class":`error`,});actionError; }
  }

   { svelteHTML.createElement("section", { "class":`section`,});
    if(visible.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});
        if(filter === 'off'){                 
               } else if (filter === 'on'){   }else{ }
       }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let c of __sveltets_2_ensureArray(visible)){c.id;
           { svelteHTML.createElement("li", {  "class":`card`,});c.stale;
             { svelteHTML.createElement("div", { "class":`card-main`,});
               { svelteHTML.createElement("span", { "class":`name`,});c.name || '(unnamed)'; }
               { svelteHTML.createElement("span", { "class":`badge kind`,});kindLabel(c); }
              if(c.stale){
                 { svelteHTML.createElement("span", { "class":`badge stale-badge`,});      }
              }
               { svelteHTML.createElement("span", { "class":`detail`,});detailLabel(c); }
               { svelteHTML.createElement("span", { "class":`justification`,});c.justification; }
               { svelteHTML.createElement("span", { "class":`device`,}); c.routerDevice; }
             }
             { svelteHTML.createElement("span", { "class":`row-actions`,});
              if(filter === 'off'){
                 { svelteHTML.createElement("button", {     "class":`accept`,"disabled":acceptingId === c.id,"onclick":() => accept(c),});
                  acceptingId === c.id ? 'Accepting…' : 'Accept';
                 }
                 { svelteHTML.createElement("button", {     "class":`hide`,"disabled":hidingId === c.id,"onclick":() => hide(c),});
                  hidingId === c.id ? 'Hiding…' : 'Hide';
                 }
              } else if (filter === 'hide'){
                 { svelteHTML.createElement("button", {     "class":`unhide`,"disabled":unhidingId === c.id,"onclick":() => unhide(c),});
                  unhidingId === c.id ? 'Unhiding…' : 'Unhide';
                 }
              }
             }
           }
        }
       }
    }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Suggestions__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Suggestions__SvelteComponent_ = ReturnType<typeof Suggestions__SvelteComponent_>;
/*Ωignore_endΩ*/export default Suggestions__SvelteComponent_;