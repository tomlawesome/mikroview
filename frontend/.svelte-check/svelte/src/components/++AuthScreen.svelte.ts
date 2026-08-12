///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Shared shell for the two pre-app states (setup/login) -- both are a
// centered card with a username/password form, differing only in
// title, submit label, and what happens on submit. Not a modal: this
// replaces App.svelte's entire main content, same as Metrics being an
// independent view rather than an overlay.

import LogoLockup from './LogoLockup.svelte'
import { authState } from '../lib/auth.svelte';

;type $$ComponentProps =  {
    title: string
    subtitle: string
    submitLabel: string
    onsubmit: (username: string, password: string) => Promise<string | null>
    confirmPassword?: boolean
    // ssoAvailable: whether the backend has OIDC/SSO configured (see
    // authState.ssoAvailable) -- renders a secondary "Sign in with SSO"
    // link below the form. A plain <a>, not a button/fetch call: this
    // has to be a real top-level browser navigation for the OAuth
    // redirect to work at all, so clicking it leaves the SPA entirely.
    ssoAvailable?: boolean
  };function $$render() {

  
  
  
  
  
  
  
  

  let {
    title,
    subtitle,
    submitLabel,
    onsubmit,
    confirmPassword = false,
    ssoAvailable = false,
  }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  let username = $state('')
  let password = $state('')
  let passwordConfirm = $state('')
  let error = $state<string | null>(null)
  let submitting = $state(false)
  // Requires clicking through to a separate warning screen rather than
  // reacting to one click on the form -- this is a permanent, only-
  // CLI-reversible decision (see the plan's "informed choice, not
  // accidental click" requirement).

  async function handleSubmit(e: Event) {
    e.preventDefault()
    error = null

    if (confirmPassword && password !== passwordConfirm) {
      error = 'Passwords do not match.'
      return
    }

    submitting = true
    const result = await onsubmit(username, password)
    submitting = false
    if (result) error = result
  }

;
async () => {

 { svelteHTML.createElement("div", { "class":`screen`,});
   { svelteHTML.createElement("div", { "class":`card`,});
     { const $$_pukcoLogoL2C = __sveltets_2_ensureComponent(LogoLockup); new $$_pukcoLogoL2C({ target: __sveltets_2_any(), props: {  "size":26,}});}

    if(authState.ssoError){
       { svelteHTML.createElement("p", { "class":`error`,});authState.ssoError; }
    }

     { svelteHTML.createElement("form", {   "class":`form-body`,"onsubmit":handleSubmit,});
       { svelteHTML.createElement("h1", {});title; }
       { svelteHTML.createElement("p", { "class":`subtitle`,});subtitle; }

       { svelteHTML.createElement("label", {});
         { svelteHTML.createElement("span", {});  }
          { svelteHTML.createElement("input", {      "type":`text`,"autocomplete":`username`,"bind:value":username,"required":true,});/*Ωignore_startΩ*/() => username = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }

       { svelteHTML.createElement("label", {});
         { svelteHTML.createElement("span", {});  }
          { svelteHTML.createElement("input", {      "type":`password`,"autocomplete":confirmPassword ? 'new-password' : 'current-password',"bind:value":password,"required":true,});/*Ωignore_startΩ*/() => password = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }

      if(confirmPassword){
         { svelteHTML.createElement("label", {});
           { svelteHTML.createElement("span", {});  }
            { svelteHTML.createElement("input", {      "type":`password`,"autocomplete":`new-password`,"bind:value":passwordConfirm,"required":true,});/*Ωignore_startΩ*/() => passwordConfirm = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         }
      }

      if(error){
         { svelteHTML.createElement("p", { "class":`error`,});error; }
      }

       { svelteHTML.createElement("button", {   "type":`submit`,"disabled":submitting,});submitting ? 'Please wait…' : submitLabel; }

      if(ssoAvailable){
         { svelteHTML.createElement("div", { "class":`divider`,}); { svelteHTML.createElement("span", {});  } }
         { svelteHTML.createElement("a", {   "class":`sso-link`,"href":`/api/auth/oidc/login`,});    }
      }
     }

   }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const AuthScreen__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type AuthScreen__SvelteComponent_ = ReturnType<typeof AuthScreen__SvelteComponent_>;
/*Ωignore_endΩ*/export default AuthScreen__SvelteComponent_;