///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from '../lib/state.svelte';
function $$render() {

  
  
;
async () => {

if(appState.connState !== 'open'){
   { svelteHTML.createElement("div", {   "class":`banner banner-${appState.connState}`,"role":`status`,});
    appState.connState === 'connecting'
      ? 'Connecting to mikroview…'
      : 'Disconnected from server — attempting to reconnect…';
   }
} else if (appState.wsDropped > 0){
   { svelteHTML.createElement("div", {   "class":`banner banner-warning`,"role":`status`,});
           appState.wsDropped;
    appState.wsDropped === 1 ? 'event has' : 'events have';        
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const ConnectionBanner__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type ConnectionBanner__SvelteComponent_ = InstanceType<typeof ConnectionBanner__SvelteComponent_>;
/*Ωignore_endΩ*/export default ConnectionBanner__SvelteComponent_;