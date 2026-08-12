///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Connect the signed-in account to SSO (issue #133 Part 4).
//
// This exists because the operation is irreversible from inside
// mikroview and destroys a credential. The warning comes before the
// confirm, not after it, and the confirm button says what happens
// rather than "OK" -- the same reasoning SECURITY.md applies to the
// "skip auth" choice, which is likewise a permanent decision rather
// than a default someone falls into.

import { authState } from '../lib/auth.svelte'
import { startSSOLink } from '../lib/api';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  

  let error = $state<string | null>(null)
  let submitting = $state(false)

  function close() {
    authState.showSSOLink = false
    error = null
    submitting = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }

  async function confirm() {
    error = null
    submitting = true
    const result = await startSSOLink()
    if (typeof result === 'string') {
      error = result
      submitting = false
      return
    }
    // Full navigation, not fetch: the provider needs to see the browser
    // so it can show its own sign-in page and set its own cookies.
    // submitting stays true -- the page is on its way out.
    location.href = result.url
  }
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(authState.showSSOLink){
   { svelteHTML.createElement("div", {     "class":`backdrop`,"onclick":onBackdropClick,"role":`presentation`,});
     { svelteHTML.createElement("div", {         "class":`modal`,"role":`dialog`,"aria-modal":`true`,"aria-label":`Connect SSO`,"tabindex":-1,});
       { svelteHTML.createElement("div", { "class":`modal-header`,});
         { svelteHTML.createElement("span", { "class":`title`,});     }
         { svelteHTML.createElement("button", {       "type":`button`,"class":`close`,"onclick":close,"aria-label":`Close`,});  }
       }

       { svelteHTML.createElement("div", { "class":`body`,});
         { svelteHTML.createElement("p", {});
                       
                 
         }

         { svelteHTML.createElement("div", { "class":`warning`,});
           { svelteHTML.createElement("strong", {});      }
           { svelteHTML.createElement("p", {});
                      
                         
                  
           }
         }

         { svelteHTML.createElement("p", { "class":`muted`,});
                     
           
         }

        if(error){
           { svelteHTML.createElement("p", { "class":`error`,});error; }
        }
       }

       { svelteHTML.createElement("div", { "class":`actions`,});
         { svelteHTML.createElement("button", {       "type":`button`,"class":`cancel`,"onclick":close,"disabled":submitting,});  }
         { svelteHTML.createElement("button", {       "type":`button`,"class":`danger`,"onclick":confirm,"disabled":submitting,});
          submitting ? 'Redirecting…' : 'Delete my password and connect SSO';
         }
       }
     }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const SSOLinkOverlay__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type SSOLinkOverlay__SvelteComponent_ = ReturnType<typeof SSOLinkOverlay__SvelteComponent_>;
/*Ωignore_endΩ*/export default SSOLinkOverlay__SvelteComponent_;