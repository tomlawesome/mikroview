///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState } from '../lib/state.svelte'
import type { Action } from '../lib/types'
import FilterPresetsMenu from './FilterPresetsMenu.svelte'
import { viewportState } from '../lib/viewport.svelte';
function $$render() {

  
  
  
  
  

  const actions: { value: Action | ''; label: string }[] = [
    { value: '', label: 'Any action' },
    { value: 'accept', label: 'Accept' },
    { value: 'drop', label: 'Drop' },
    { value: 'reject', label: 'Reject' },
    { value: 'log', label: 'Log' },
    { value: 'unknown', label: 'Unknown' },
  ]

  // Below the breakpoint, the ~9 fields below move into a slide-up
  // drawer behind a trigger (issue #85) rather than staying always-
  // visible -- a horizontally-wrapping strip of selects/inputs doesn't
  // fit a phone-width screen usefully even stacked one-per-line, and a
  // human scanning the live view rarely needs every filter visible at
  // once the way FilterPresetsMenu's saved presets do.
  let drawerOpen = $state(false)

  function onKeydown(e: KeyboardEvent) {
    if (drawerOpen && e.key === 'Escape') drawerOpen = false
  }
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(viewportState.isMobile){
   { svelteHTML.createElement("div", { "class":`mobile-row`,});
     { svelteHTML.createElement("button", {         "class":`trigger`,"onclick":() => (drawerOpen = true),"aria-haspopup":`true`,"aria-expanded":drawerOpen,});
      
      if(appState.hasActiveFilters){ { svelteHTML.createElement("span", {   "class":`dot`,"aria-label":`Filters active`,}); }}
     }
    if(appState.hasActiveFilters){
       { svelteHTML.createElement("button", {   "class":`clear`,"onclick":() => appState.resetFilters(),});  }
    }
   }
}

if(!viewportState.isMobile || drawerOpen){
  if(viewportState.isMobile){
     { svelteHTML.createElement("div", {     "class":`scrim`,"onclick":() => (drawerOpen = false),"role":`presentation`,}); }
  }
   { svelteHTML.createElement("div", {  "class":`bar`,});viewportState.isMobile;
    if(viewportState.isMobile){
       { svelteHTML.createElement("div", { "class":`handle`,}); }
       { svelteHTML.createElement("div", { "class":`drawer-header`,});
         { svelteHTML.createElement("span", { "class":`drawer-title`,});  }
         { svelteHTML.createElement("button", {   "class":`done`,"onclick":() => (drawerOpen = false),});  }
       }
    }

     { const $$_uneMsteserPretliF1C = __sveltets_2_ensureComponent(FilterPresetsMenu); new $$_uneMsteserPretliF1C({ target: __sveltets_2_any(), props: {}});}

     { svelteHTML.createElement("select", {   "bind:value":appState.filters.device,"aria-label":`Device`,});/*Ωignore_startΩ*/() => appState.filters.device = __sveltets_2_any(null);/*Ωignore_endΩ*/
       { svelteHTML.createElement("option", {"value":"",});  }
         for(let d of __sveltets_2_ensureArray(appState.devices)){d.id;
         { svelteHTML.createElement("option", { "value":d.id,});d.name; }
      }
     }

     { svelteHTML.createElement("select", {   "bind:value":appState.filters.action,"aria-label":`Action`,});/*Ωignore_startΩ*/() => appState.filters.action = __sveltets_2_any(null);/*Ωignore_endΩ*/
         for(let a of __sveltets_2_ensureArray(actions)){a.value;
         { svelteHTML.createElement("option", { "value":a.value,});a.label; }
      }
     }

     { svelteHTML.createElement("input", {         "type":`text`,"placeholder":`Protocol (tcp, udp, icmp…)`,"bind:value":appState.filters.protocol,"aria-label":`Protocol`,});/*Ωignore_startΩ*/() => appState.filters.protocol = __sveltets_2_any(null);/*Ωignore_endΩ*/}

     { svelteHTML.createElement("input", {         "type":`text`,"placeholder":`IP or CIDR`,"bind:value":appState.filters.ip,"aria-label":`IP address or CIDR`,});/*Ωignore_startΩ*/() => appState.filters.ip = __sveltets_2_any(null);/*Ωignore_endΩ*/}

     { svelteHTML.createElement("input", {           "type":`text`,"inputmode":`numeric`,"placeholder":`Port`,"bind:value":appState.filters.port,"aria-label":`Port`,});/*Ωignore_startΩ*/() => appState.filters.port = __sveltets_2_any(null);/*Ωignore_endΩ*/}

     { svelteHTML.createElement("select", {     "bind:value":appState.filters.srcScope,"aria-label":`Source scope`,"title":`Restrict by whether the source is on your LAN`,});/*Ωignore_startΩ*/() => appState.filters.srcScope = __sveltets_2_any(null);/*Ωignore_endΩ*/
       { svelteHTML.createElement("option", {"value":"",});  }
       { svelteHTML.createElement("option", { "value":`internal`,});  }
       { svelteHTML.createElement("option", { "value":`external`,});  }
     }

     { svelteHTML.createElement("select", {     "bind:value":appState.filters.dstScope,"aria-label":`Destination scope`,"title":`Restrict by whether the destination is on your LAN`,});/*Ωignore_startΩ*/() => appState.filters.dstScope = __sveltets_2_any(null);/*Ωignore_endΩ*/
       { svelteHTML.createElement("option", {"value":"",});  }
       { svelteHTML.createElement("option", { "value":`internal`,});  }
       { svelteHTML.createElement("option", { "value":`external`,});  }
     }

     { svelteHTML.createElement("input", {         "type":`text`,"placeholder":`Interface`,"bind:value":appState.filters.interface,"aria-label":`Interface`,});/*Ωignore_startΩ*/() => appState.filters.interface = __sveltets_2_any(null);/*Ωignore_endΩ*/}

     { svelteHTML.createElement("div", { "class":`rule-group`,});
       { svelteHTML.createElement("input", {           "type":`text`,"placeholder":appState.filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…',"bind:value":appState.filters.rule,"class":`rule`,"aria-label":appState.filters.ruleRegex ? 'Rule/raw line regex search' : 'Rule label search',});/*Ωignore_startΩ*/() => appState.filters.rule = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       { svelteHTML.createElement("button", {           "class":`regex-toggle`,"onclick":() => (appState.filters.ruleRegex = !appState.filters.ruleRegex),"title":appState.ruleMatchStatus === 'too-slow'
          ? 'That pattern took too long to evaluate and was stopped, so the rule filter is inactive. Try a simpler one.'
          : appState.ruleMatchStatus === 'invalid'
            ? 'That is not a valid regular expression, so the rule filter is inactive.'
            : 'Treat the rule search above as a regular expression (matches rule label or raw log line)',"aria-pressed":appState.filters.ruleRegex,});appState.filters.ruleRegex;appState.ruleMatchStatus === 'too-slow' || appState.ruleMatchStatus === 'invalid';
        
       }
     }

    if(appState.hasActiveFilters && !viewportState.isMobile){
       { svelteHTML.createElement("button", {   "class":`clear`,"onclick":() => appState.resetFilters(),});  }
    }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const FilterBar__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type FilterBar__SvelteComponent_ = ReturnType<typeof FilterBar__SvelteComponent_>;
/*Ωignore_endΩ*/export default FilterBar__SvelteComponent_;