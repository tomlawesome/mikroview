///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Admin-only: create/name/revoke read-only API bearer tokens (issue
// #101) -- for a companion service (e.g. Birdcage) to pull event/flag
// data with no browser session. Mirrors UsersOverlay's modal
// pattern/markup.

import { authState } from '../lib/auth.svelte'
import { tokensState } from '../lib/tokens.svelte';
function $$render() {

  
  
  
  
  
  
  

  let name = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  let copied = $state(false)

  $effect(() => {
    if (authState.showTokens) {
      error = null
      tokensState.refresh()
    }
  })

  function close() {
    authState.showTokens = false
    name = ''
    error = null
    copied = false
    tokensState.clearJustCreated()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function handleCreate(e: Event) {
    e.preventDefault()
    error = null
    copied = false
    submitting = true
    const result = await tokensState.create(name)
    submitting = false
    if (result) {
      error = result
      return
    }
    name = ''
  }

  async function handleRevoke(id: string) {
    if (!confirm('Revoke this token? Anything using it will immediately lose access.')) return
    const result = await tokensState.revoke(id)
    if (result) error = result
  }

  async function copyValue(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      copied = true
    } catch {
      // Clipboard access can fail (permissions, non-secure context) --
      // the value stays selectable/visible in the banner either way, so
      // there's still a manual fallback; nothing more to do here.
    }
  }

  function formatDateTime(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return '—'
    return d.toLocaleString()
  }
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(authState.showTokens){
   { svelteHTML.createElement("div", {     "class":`backdrop`,"onclick":onBackdropClick,"role":`presentation`,});
     { svelteHTML.createElement("div", {         "class":`modal`,"role":`dialog`,"aria-modal":`true`,"aria-label":`API tokens`,"tabindex":-1,});
       { svelteHTML.createElement("div", { "class":`modal-header`,});
         { svelteHTML.createElement("span", { "class":`title`,});  }
         { svelteHTML.createElement("button", {       "type":`button`,"class":`close`,"onclick":close,"aria-label":`Close`,});  }
       }

       { svelteHTML.createElement("div", { "class":`body`,});
         { svelteHTML.createElement("p", { "class":`hint`,});
                  { svelteHTML.createElement("code", {});  }
           { svelteHTML.createElement("code", {});  }  { svelteHTML.createElement("code", {});  }   { svelteHTML.createElement("code", {});  }    
               
         }

        if(tokensState.justCreated){
           { svelteHTML.createElement("div", { "class":`created-banner`,});
             { svelteHTML.createElement("div", { "class":`created-label`,});
               tokensState.justCreated.name;         
             }
             { svelteHTML.createElement("div", { "class":`created-value-row`,});
               { svelteHTML.createElement("code", { "class":`created-value`,});tokensState.justCreated.value; }
               { svelteHTML.createElement("button", {       "type":`button`,"class":`copy`,"onclick":() => tokensState.justCreated && copyValue(tokensState.justCreated.value ?? ''),});
                copied ? 'Copied' : 'Copy';
               }
             }
           }
        }

         { svelteHTML.createElement("form", {   "class":`create-form`,"onsubmit":handleCreate,});
            { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`Token name (e.g. birdcage)`,"bind:value":name,"required":true,});/*Ωignore_startΩ*/() => name = __sveltets_2_any(null);/*Ωignore_endΩ*/}
           { svelteHTML.createElement("button", {     "type":`submit`,"class":`save`,"disabled":submitting,});submitting ? 'Creating…' : 'Create'; }
         }

        if(error){
           { svelteHTML.createElement("p", { "class":`error`,});error; }
        }

         { svelteHTML.createElement("div", { "class":`list`,});
          if(tokensState.list.length === 0){
             { svelteHTML.createElement("p", { "class":`empty`,});   }
          }
             for(let tok of __sveltets_2_ensureArray(tokensState.list)){tok.id;
             { svelteHTML.createElement("div", { "class":`row`,});
               { svelteHTML.createElement("div", { "class":`row-main`,});
                 { svelteHTML.createElement("span", { "class":`row-name`,});tok.name; }
                 { svelteHTML.createElement("span", { "class":`row-meta`,});
                   formatDateTime(tok.createdAt);    formatDateTime(tok.lastUsedAt);
                 }
               }
               { svelteHTML.createElement("button", {     "type":`button`,"class":`revoke`,"onclick":() => handleRevoke(tok.id),});  }
             }
          }
         }
       }

       { svelteHTML.createElement("div", { "class":`actions`,});
         { svelteHTML.createElement("button", {     "type":`button`,"class":`cancel`,"onclick":close,});  }
       }
     }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const TokensOverlay__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type TokensOverlay__SvelteComponent_ = ReturnType<typeof TokensOverlay__SvelteComponent_>;
/*Ωignore_endΩ*/export default TokensOverlay__SvelteComponent_;