///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Always-visible glance-and-go strip -- see Fleet.svelte (issue #98)
// for the richer dedicated view this deliberately doesn't try to
// replace. `status` is server-computed by GET /api/devices (live/
// stale/never_seen, see internal/api/rest.go's deviceStatus) against
// the operator-configured deviceStaleAfter threshold -- reused here
// rather than this component keeping its own separate, shorter,
// hardcoded staleness heuristic, which used to disagree with both the
// actual TypeDeviceSilence flag and Fleet.svelte's own status column.

import { appState } from '../lib/state.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
;
async () => {

 { svelteHTML.createElement("div", { "class":`devices`,});
  if(appState.devices.length === 0){
     { svelteHTML.createElement("span", { "class":`none`,});     }
  }
     for(let d of __sveltets_2_ensureArray(appState.devices)){d.id;
     { svelteHTML.createElement("span", {    "class":`device`,"title":`${d.eventCount} events · ${d.sourceIp}`,});d.status !== 'live';
       { svelteHTML.createElement("span", { "class":`dot`,}); }
      d.name;
      if(!d.configured){ { svelteHTML.createElement("span", { "class":`unregistered`,});  }}
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const DeviceStatus__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type DeviceStatus__SvelteComponent_ = InstanceType<typeof DeviceStatus__SvelteComponent_>;
/*Ωignore_endΩ*/export default DeviceStatus__SvelteComponent_;