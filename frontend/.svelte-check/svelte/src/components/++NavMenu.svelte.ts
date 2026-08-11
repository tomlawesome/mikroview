///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState, type View } from '../lib/state.svelte'
import { flagsState } from '../lib/flags.svelte'
import { detectorSettingsState } from '../lib/detectorSettings.svelte'
import { entitiesState } from '../lib/entities.svelte'
import { auditState } from '../lib/audit.svelte'
import { exclusionsState } from '../lib/exclusions.svelte'
import { authState } from '../lib/auth.svelte'
import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
import { downloadEventsCsv } from '../lib/export'
import { viewportState } from '../lib/viewport.svelte'
import { versionState } from '../lib/version.svelte'
import AboutOverlay from './AboutOverlay.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  

  versionState.ensureLoaded()

  let showAbout = $state(false)

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })

  // Every view toggle here follows the same "click again to return to
  // live" behavior the old inline toolbar buttons had.
  function toggleView(v: Exclude<View, 'live' | 'detectors'>) {
    appState.view = appState.view === v ? 'live' : v
    open = false
  }

  function toggleDetectors() {
    if (appState.view === 'detectors') {
      appState.view = 'live'
    } else {
      appState.view = 'detectors'
      detectorSettingsState.refresh()
    }
    open = false
  }

  function toggleEntities() {
    if (appState.view === 'entities') {
      appState.view = 'live'
    } else {
      appState.view = 'entities'
      entitiesState.refresh()
    }
    open = false
  }

  function toggleAudit() {
    if (appState.view === 'audit') {
      appState.view = 'live'
    } else {
      appState.view = 'audit'
      auditState.refresh()
    }
    open = false
  }

  function toggleExclusions() {
    if (appState.view === 'exclusions') {
      appState.view = 'live'
    } else {
      appState.view = 'exclusions'
      exclusionsState.refresh()
    }
    open = false
  }

  function onMaxAgeChange(e: Event) {
    const raw = (e.target as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }
;
async () => {

 { const $$_div0 = svelteHTML.createElement("div", {  "class":`nav-menu`,});rootEl = $$_div0;
   { svelteHTML.createElement("button", {           "class":`trigger`,"onclick":() => (open = !open),"aria-haspopup":`true`,"aria-expanded":open,"title":`Views, export, and account`,});
     { svelteHTML.createElement("span", {   "class":`hamburger`,"aria-hidden":`true`,});
       { svelteHTML.createElement("span", {}); } { svelteHTML.createElement("span", {}); } { svelteHTML.createElement("span", {}); }
     }
    
    if(flagsState.activeCount > 0){
       { svelteHTML.createElement("span", { "class":`flags-badge`,});flagsState.activeCount; }
    }
   }

  if(open){
    if(viewportState.isMobile){
      
       { svelteHTML.createElement("div", {     "class":`scrim`,"onclick":() => (open = false),"role":`presentation`,}); }
    }
     { svelteHTML.createElement("div", {    "class":`menu`,"role":`menu`,});viewportState.isMobile;
      if(viewportState.isMobile){
         { svelteHTML.createElement("div", { "class":`handle`,}); }
      }
       { svelteHTML.createElement("div", { "class":`section`,});
         { svelteHTML.createElement("div", { "class":`section-label`,});  }

         { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => {
            appState.view = 'live'
            open = false
          },"title":`Back to the live view`,});appState.view === 'live';
           
         }

         { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => toggleView('metrics'),"title":`Event charts and traffic breakdowns`,});appState.view === 'metrics';
          
         }

         { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => toggleView('fleet'),"title":`Every known RouterOS device: live/stale/never-seen status, last-seen, and event counts`,});appState.view === 'fleet';
          
         }

         { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => toggleView('flags'),"title":`Behavioral flags: port scans, activity spikes, critical-port attempts, and volume spikes`,});appState.view === 'flags';
          
          if(flagsState.activeCount > 0){
             { svelteHTML.createElement("span", { "class":`flags-badge inline`,});flagsState.activeCount; }
          }
         }

        if(authState.state === 'authenticated' && authState.role === 'admin'){
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":toggleDetectors,"title":`Toggle behavioral detectors on/off and restrict their scope`,});appState.view === 'detectors';
            
           }
        }

        if(authState.state === 'authenticated' && authState.role === 'admin'){
          
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":toggleEntities,"title":`Manage persisted host/rule labels and tags`,});appState.view === 'entities';
            
           }

          
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":toggleAudit,"title":`Review admin-privileged actions: user/token/entity/detector changes`,});appState.view === 'audit';
             
           }

          
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":toggleExclusions,"title":`Review and remove permanently-excluded (detector, target) pairs`,});appState.view === 'exclusions';
            
           }

          
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => toggleView('watchlist'),"title":`Watch ports or watch a device's own destinations, observe before enforcing`,});appState.view === 'watchlist';
            
           }

          
           { svelteHTML.createElement("button", {        "class":`option`,"onclick":() => toggleView('suggestions'),"title":`Review watchlist entries suggested from data your router has already pushed`,});appState.view === 'suggestions';
            
           }
        }
       }

      if(appState.view === 'live'){
        
         { svelteHTML.createElement("div", { "class":`divider`,}); }

         { svelteHTML.createElement("div", { "class":`section`,});
           { svelteHTML.createElement("div", { "class":`section-label`,});  }

          if(viewportState.isMobile){
             { svelteHTML.createElement("label", { "class":`option select-option`,});
               
               { svelteHTML.createElement("select", {       "value":retentionState.maxAgeSeconds === null ? 'null' : String(retentionState.maxAgeSeconds),"onchange":onMaxAgeChange,"aria-label":`Display duration`,});
                   for(let opt of __sveltets_2_ensureArray(MAX_AGE_OPTIONS)){opt.value;
                   { svelteHTML.createElement("option", { "value":opt.value === null ? 'null' : String(opt.value),});opt.label; }
                }
               }
             }
          }

          
           { svelteHTML.createElement("button", {         "class":`option`,"onclick":() => {
              downloadEventsCsv(appState.filteredEvents)
              open = false
            },"disabled":appState.filteredEvents.length === 0,"title":`Export the currently shown/filtered events to a CSV file`,});
              
           }
         }
      }

      if(authState.state === 'authenticated'){
         { svelteHTML.createElement("div", { "class":`divider`,}); }

         { svelteHTML.createElement("div", { "class":`section`,});
           { svelteHTML.createElement("div", { "class":`section-label`,});  }

          if(authState.role === 'admin'){
             { svelteHTML.createElement("button", {       "class":`option`,"onclick":() => {
                authState.showUsers = true
                open = false
              },"title":`Add or remove accounts`,});
              
             }
             { svelteHTML.createElement("button", {       "class":`option`,"onclick":() => {
                authState.showTokens = true
                open = false
              },"title":`Create/revoke read-only API bearer tokens for scripted access`,});
               
             }
             { svelteHTML.createElement("div", { "class":`divider`,}); }
          }

          if(authState.ssoAvailable && authState.hasLocalPassword){
            
             { svelteHTML.createElement("button", {       "class":`option`,"onclick":() => {
                authState.showSSOLink = true
                open = false
              },"title":`Sign in through your identity provider instead of a MikroView password`,});
               
             }
          }

           { svelteHTML.createElement("button", {       "class":`option`,"onclick":() => {
              authState.logout()
              open = false
            },"title":`Sign out ${authState.username}`,});
              authState.username;
           }
         }
      }

       { svelteHTML.createElement("div", { "class":`divider`,}); }
      
       { svelteHTML.createElement("button", {       "class":`option`,"onclick":() => {
          showAbout = true
          open = false
        },"title":`Version, copyright, licence and source code`,});
          
       }
      if(versionState.version){
         { svelteHTML.createElement("div", {   "class":`version`,"title":"Build version -- also available via GET /api/healthz or `mikroview -version`",});
          versionState.version;
         }
      }
     }
  }
 }

 { const $$_yalrevOtuobA0C = __sveltets_2_ensureComponent(AboutOverlay); const $$_yalrevOtuobA0 = new $$_yalrevOtuobA0C({ target: __sveltets_2_any(), props: {   open:showAbout,}});/*Ωignore_startΩ*/() => showAbout = __sveltets_2_any(null);/*Ωignore_endΩ*/$$_yalrevOtuobA0.$$bindings = 'open';}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const NavMenu__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type NavMenu__SvelteComponent_ = ReturnType<typeof NavMenu__SvelteComponent_>;
/*Ωignore_endΩ*/export default NavMenu__SvelteComponent_;