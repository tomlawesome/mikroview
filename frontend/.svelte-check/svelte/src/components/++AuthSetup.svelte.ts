///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Shown while zero accounts exist (AuthSession.setupRequired) --
// whoever completes this becomes the super-admin. See
// docs/configuration.md's "Authentication" section for why this is a
// one-time, self-service path rather than open registration.

import { authState } from '../lib/auth.svelte'
import AuthScreen from './AuthScreen.svelte';
function $$render() {

  
  
  
  
  
  
  
;
async () => {

 { const $$_neercShtuA0C = __sveltets_2_ensureComponent(AuthScreen); new $$_neercShtuA0C({ target: __sveltets_2_any(), props: {            "title":`Create the admin account`,"subtitle":`No account exists yet. Whoever completes this form becomes the admin.`,"submitLabel":`Create account`,"confirmPassword":true,"onsubmit":(username, password) => authState.register(username, password),"ssoAvailable":authState.ssoAvailable,}});}
};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const AuthSetup__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type AuthSetup__SvelteComponent_ = InstanceType<typeof AuthSetup__SvelteComponent_>;
/*Ωignore_endΩ*/export default AuthSetup__SvelteComponent_;