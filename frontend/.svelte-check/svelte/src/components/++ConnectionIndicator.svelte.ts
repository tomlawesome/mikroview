///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from '../lib/state.svelte';
function $$render() {

  
  

  const labels = {
    connecting: 'Connecting…',
    open: 'Live',
    closed: 'Disconnected',
  }
;
async () => {

 { svelteHTML.createElement("span", { "class":`conn conn-${appState.connState}`,});
   { svelteHTML.createElement("span", { "class":`dot`,}); }
  labels[appState.connState];
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const ConnectionIndicator__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type ConnectionIndicator__SvelteComponent_ = InstanceType<typeof ConnectionIndicator__SvelteComponent_>;
/*Ωignore_endΩ*/export default ConnectionIndicator__SvelteComponent_;