///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Permanent flag exclusions, on their own page (issue #207) -- moved
// out of the bottom of Flags.svelte because reaching and reviewing
// exclusions underneath a list of hundreds of active flags was a pain.
//
// Every (detector, target) pair permanently silenced via Flags.svelte's
// "Permanently clear" action -- removing one here lets it raise
// normally again. This is the one undo path a permanent clear has; a
// regular clear has none, by design (see Flags.svelte).

import { onMount } from 'svelte'
import { exclusionsState } from '../lib/exclusions.svelte'
import type { FlagType } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  

  onMount(() => {
    exclusionsState.refresh()
  })

  // Same labels Flags.svelte/FlagsChart.svelte use -- duplicated rather
  // than shared, matching how ACTION_LABELS is already independently
  // duplicated in both EventsChart.svelte and Dashboard.svelte in this
  // codebase.
  const TYPE_LABELS: Record<FlagType, string> = {
    port_scan: 'Port scan',
    activity_spike: 'Activity spike',
    critical_port: 'Critical-port attempts',
    global_spike: 'Network-wide volume spike',
    distributed_brute_force: 'Distributed brute-force',
    outbound_anomaly: 'Outbound anomaly',
    internal_recon: 'Internal reconnaissance',
    rule_spike: 'Rule hit-rate spike',
    repeated_drops: 'Repeated drops on a port',
    low_slow_scan: 'Low-and-slow port scan',
    off_hours_activity: 'Off-hours activity',
    device_silence: 'Device gone quiet',
    new_device: 'New device',
    stale_rule: 'Stale firewall rule',
    unexpected_mail_sender: 'Unexpected mail sender',
    known_bad_ip: 'Known-bad IP (blocklist match)',
  }

  let removingId = $state<string | null>(null)
  let typeFilter = $state<FlagType | ''>('')
  let targetFilter = $state('')

  async function remove(id: string) {
    removingId = id
    try {
      await exclusionsState.remove(id)
    } finally {
      removingId = null
    }
  }

  // Sorted however the server returns them (ListExclusions sorts by ID,
  // which is stable but not chronological -- Exclusion carries no
  // timestamp, and this page is scoped to be a client-side filter over
  // the existing two endpoints, not a reason to add one). Filtering
  // client-side is enough at the sizes this list actually reaches.
  const filtered = $derived(
    exclusionsState.list.filter((e) => {
      if (typeFilter && e.type !== typeFilter) return false
      if (targetFilter && !e.target.toLowerCase().includes(targetFilter.trim().toLowerCase())) return false
      return true
    }),
  )

  const typeOptions = $derived(
    [...new Set(exclusionsState.list.map((e) => e.type))].sort((a, b) =>
      TYPE_LABELS[a].localeCompare(TYPE_LABELS[b]),
    ),
  )
;
async () => {

 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
                    
            
   }

  if(exclusionsState.list.length > 0){
     { svelteHTML.createElement("div", { "class":`toolbar`,});
       { svelteHTML.createElement("span", { "class":`count`,});
        filtered.length === exclusionsState.list.length
          ? `${exclusionsState.list.length} exclusion${exclusionsState.list.length === 1 ? '' : 's'}`
          : `${filtered.length} of ${exclusionsState.list.length}`;
       }

       { svelteHTML.createElement("label", { "class":`filter`,});
        
         { svelteHTML.createElement("select", { "bind:value":typeFilter,});/*Ωignore_startΩ*/() => typeFilter = __sveltets_2_any(null);/*Ωignore_endΩ*/
           { svelteHTML.createElement("option", {"value":"",});  }
             for(let t of __sveltets_2_ensureArray(typeOptions)){t;
             { svelteHTML.createElement("option", { "value":t,});TYPE_LABELS[t]; }
          }
         }
       }

       { svelteHTML.createElement("label", { "class":`filter`,});
        
         { svelteHTML.createElement("input", {      "type":`search`,"placeholder":`Filter by IP, host, or 'global'…`,"bind:value":targetFilter,});/*Ωignore_startΩ*/() => targetFilter = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
     }
  }

  if(exclusionsState.list.length === 0){
     { svelteHTML.createElement("p", { "class":`empty`,});   }
  } else if (filtered.length === 0){
     { svelteHTML.createElement("p", { "class":`empty`,});     }
  }else{
     { svelteHTML.createElement("ul", { "class":`list`,});
         for(let e of __sveltets_2_ensureArray(filtered)){e.id;
         { svelteHTML.createElement("li", { "class":`card`,});
           { svelteHTML.createElement("div", { "class":`card-main`,});
             { svelteHTML.createElement("span", { "class":`type`,});TYPE_LABELS[e.type]; }
             { svelteHTML.createElement("span", { "class":`target`,});e.target === 'global' ? 'network-wide' : e.target; }
           }
           { svelteHTML.createElement("button", {     "class":`remove`,"disabled":removingId === e.id,"onclick":() => remove(e.id),});
            removingId === e.id ? 'Removing…' : 'Remove exclusion';
           }
         }
      }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Exclusions__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Exclusions__SvelteComponent_ = ReturnType<typeof Exclusions__SvelteComponent_>;
/*Ωignore_endΩ*/export default Exclusions__SvelteComponent_;