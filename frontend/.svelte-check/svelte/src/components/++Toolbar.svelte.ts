///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from '../lib/state.svelte'
import { formatEps, formatBufferDepth } from '../lib/format'
import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
import { viewportState } from '../lib/viewport.svelte'
import ConnectionIndicator from './ConnectionIndicator.svelte'
import DeviceStatus from './DeviceStatus.svelte'
import LogoLockup from './LogoLockup.svelte'
import NavMenu from './NavMenu.svelte'
import ThemeMenu from './ThemeMenu.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  

  function onMaxAgeChange(e: Event) {
    const raw = (e.target as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }
;
async () => {

 { svelteHTML.createElement("header", { "class":`toolbar`,});
   { svelteHTML.createElement("div", { "class":`brand`,});
     { svelteHTML.createElement("button", {         "class":`logo-button`,"onclick":() => (appState.view = 'live'),"title":`Back to live view`,"aria-label":`Back to live view`,});
       { const $$_pukcoLogoL3C = __sveltets_2_ensureComponent(LogoLockup); new $$_pukcoLogoL3C({ target: __sveltets_2_any(), props: {  "size":21,}});}
     }
     { const $$_rotacidnInoitcennoC2C = __sveltets_2_ensureComponent(ConnectionIndicator); new $$_rotacidnInoitcennoC2C({ target: __sveltets_2_any(), props: {}});}
   }

   { const $$_sutatSeciveD1C = __sveltets_2_ensureComponent(DeviceStatus); new $$_sutatSeciveD1C({ target: __sveltets_2_any(), props: {}});}

   { svelteHTML.createElement("div", { "class":`controls`,});
    if(appState.view === 'live'){
      if(appState.stats){
         { svelteHTML.createElement("span", {   "class":`eps`,"title":`Events per second (10s rolling average)`,});
          formatEps(appState.stats.eventsPerSecond);
         }
         { svelteHTML.createElement("span", {     "class":`buffer-depth`,"title":`The server's event buffer holds up to ${appState.stats.capacity.toLocaleString()} events. Once full, each new event overwrites the oldest -- this is how far back it actually reaches at the current rate, not the configured retention window.`,});
          formatBufferDepth(appState.stats.capacity, appState.stats.count, appState.stats.eventsPerSecond);
         }
        if(appState.stats.syslog && appState.stats.syslog.rejectedConfigured > 0){
          
           { svelteHTML.createElement("span", {     "class":`syslog-blocked`,"title":`MikroView has turned away ${appState.stats.syslog.rejectedConfigured} connection attempt(s) from a router listed in your config, because its syslog connection slots were full (${appState.stats.syslog.inUse} of ${appState.stats.syslog.capacity} in use). Those log lines never arrived. This usually means something is opening a lot of connections to the syslog port.`,});
              
           }
        }
      }

      if(!viewportState.isMobile){
         { svelteHTML.createElement("select", {         "value":retentionState.maxAgeSeconds === null ? 'null' : String(retentionState.maxAgeSeconds),"onchange":onMaxAgeChange,"title":`How long events stay visible in the live view`,"aria-label":`Display duration`,});
             for(let opt of __sveltets_2_ensureArray(MAX_AGE_OPTIONS)){opt.value;
             { svelteHTML.createElement("option", { "value":opt.value === null ? 'null' : String(opt.value),});opt.label; }
          }
         }
      }

       { svelteHTML.createElement("button", {      "onclick":() => (appState.autoscroll = !appState.autoscroll),"title":appState.autoscroll
          ? 'Auto-scroll to newest events'
          : 'Hold the current view -- new events keep arriving but the table stays put',});appState.autoscroll;
        
       }

       { svelteHTML.createElement("button", {      "onclick":() => appState.togglePause(),"title":appState.paused ? 'Resume live updates' : 'Pause live updates',});appState.paused;
        appState.paused ? `Resume${appState.pendingCount ? ` (${appState.pendingCount})` : ''}` : 'Pause';
       }

       { svelteHTML.createElement("button", {   "onclick":() => appState.clearBuffer(),"title":`Clear the local event buffer`,});
        
       }
    }

    
     { const $$_uneMemehT2C = __sveltets_2_ensureComponent(ThemeMenu); new $$_uneMemehT2C({ target: __sveltets_2_any(), props: {}});}
     { const $$_uneMvaN2C = __sveltets_2_ensureComponent(NavMenu); new $$_uneMvaN2C({ target: __sveltets_2_any(), props: {}});}
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const Toolbar__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type Toolbar__SvelteComponent_ = InstanceType<typeof Toolbar__SvelteComponent_>;
/*Ωignore_endΩ*/export default Toolbar__SvelteComponent_;