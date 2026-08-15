///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Admin-only: list, add and remove accounts (issue #133). Mirrors
// TokensOverlay's modal pattern/markup.
//
// Two things this deliberately cannot do. It never creates an admin --
// mikroview has exactly one, and the server refuses a request for a
// second. And it never removes the admin: moving that role is CLI-only
// and recovery-key gated (`mikroview -transfer-admin`), so a
// compromised admin session cannot hand ownership to an attacker or
// demote the real admin out of their own deployment.

import { authState } from '../lib/auth.svelte'
import { usersState } from '../lib/users.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  

  let username = $state('')
  let password = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)

  $effect(() => {
    if (authState.showUsers) {
      error = null
      usersState.refresh()
    }
  })

  function close() {
    authState.showUsers = false
    username = ''
    password = ''
    error = null
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
    submitting = true
    const result = await usersState.create(username, password)
    submitting = false
    if (result) {
      error = result
      return
    }
    username = ''
    password = ''
  }

  async function handleDelete(user: { id: string; username: string }) {
    // Names the consequences rather than asking "are you sure?": the
    // sessions and tokens going too is the part that isn't obvious from
    // the word "delete".
    const ok = confirm(
      `Delete "${user.username}"?\n\n` +
        `They will be signed out immediately, and any API tokens they created will stop working.`,
    )
    if (!ok) return
    const result = await usersState.remove(user.id)
    if (result) error = result
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

if(authState.showUsers){
   { svelteHTML.createElement("div", {     "class":`backdrop`,"onclick":onBackdropClick,"role":`presentation`,});
     { svelteHTML.createElement("div", {         "class":`modal`,"role":`dialog`,"aria-modal":`true`,"aria-label":`Users`,"tabindex":-1,});
       { svelteHTML.createElement("div", { "class":`modal-header`,});
         { svelteHTML.createElement("span", { "class":`title`,});  }
         { svelteHTML.createElement("button", {       "type":`button`,"class":`close`,"onclick":close,"aria-label":`Close`,});  }
       }

       { svelteHTML.createElement("div", { "class":`body`,});
         { svelteHTML.createElement("p", { "class":`hint`,});
                    
                     
                       
          
         }

         { svelteHTML.createElement("form", {   "class":`create-form`,"onsubmit":handleCreate,});
            { svelteHTML.createElement("input", {        "type":`text`,"placeholder":`Username`,"autocomplete":`off`,"bind:value":username,"required":true,});/*Ωignore_startΩ*/() => username = __sveltets_2_any(null);/*Ωignore_endΩ*/}
           { svelteHTML.createElement("input", {          "type":`password`,"placeholder":`Password`,"autocomplete":`new-password`,"bind:value":password,"required":true,});/*Ωignore_startΩ*/() => password = __sveltets_2_any(null);/*Ωignore_endΩ*/}
           { svelteHTML.createElement("button", {     "type":`submit`,"class":`save`,"disabled":submitting,});submitting ? 'Adding…' : 'Add'; }
         }

        if(error){
           { svelteHTML.createElement("p", { "class":`error`,});error; }
        }

         { svelteHTML.createElement("div", { "class":`list`,});
             for(let user of __sveltets_2_ensureArray(usersState.list)){user.id;
             { svelteHTML.createElement("div", { "class":`row`,});
               { svelteHTML.createElement("div", { "class":`row-main`,});
                 { svelteHTML.createElement("span", { "class":`row-name`,});
                  user.username;
                  if(user.role === 'admin'){ { svelteHTML.createElement("span", { "class":`badge admin`,});  }}
                  if(user.sso){ { svelteHTML.createElement("span", { "class":`badge sso`,});  }}
                 }
                 { svelteHTML.createElement("span", { "class":`row-meta`,});
                   formatDateTime(user.createdAt);     formatDateTime(user.lastLogin);
                 }
               }
              if(user.role === 'admin'){
                 { svelteHTML.createElement("span", {   "class":`row-note`,"title":`Transfer the admin role from the command line first`,});
                    
                 }
              }else{
                 { svelteHTML.createElement("button", {     "type":`button`,"class":`revoke`,"onclick":() => handleDelete(user),});  }
              }
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
const UsersOverlay__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type UsersOverlay__SvelteComponent_ = ReturnType<typeof UsersOverlay__SvelteComponent_>;
/*Ωignore_endΩ*/export default UsersOverlay__SvelteComponent_;