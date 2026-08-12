///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { authState } from '../lib/auth.svelte'
import AuthScreen from './AuthScreen.svelte';
function $$render() {

  
  
  
;
async () => {

 { const $$_neercShtuA0C = __sveltets_2_ensureComponent(AuthScreen); new $$_neercShtuA0C({ target: __sveltets_2_any(), props: {           "title":`Sign in`,"subtitle":`Sign in to continue.`,"submitLabel":`Sign in`,"onsubmit":(username, password) => authState.login(username, password),"ssoAvailable":authState.ssoAvailable,}});}
};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const AuthLogin__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type AuthLogin__SvelteComponent_ = InstanceType<typeof AuthLogin__SvelteComponent_>;
/*Ωignore_endΩ*/export default AuthLogin__SvelteComponent_;