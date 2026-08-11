///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Collapsed by default (just a "+" trigger card, same footprint as the
// other dashboard panels); expands into a small filter form -- a
// purpose-built subset of FilterBar's fields bound to local draft state
// rather than appState.filters, since a widget's filter is independent
// of whatever's active in the live view.

import { appState } from '../lib/state.svelte'
import { emptyFilters, type Action, type Filters } from '../lib/types'
import { GROUP_BY_FIELDS, GROUP_BY_LABELS, type GroupByField } from '../lib/groupBy'
import { topTalkerWidgetsState } from '../lib/topTalkers.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  

  const actions: { value: Action | ''; label: string }[] = [
    { value: '', label: 'Any action' },
    { value: 'accept', label: 'Accept' },
    { value: 'drop', label: 'Drop' },
    { value: 'reject', label: 'Reject' },
    { value: 'log', label: 'Log' },
    { value: 'unknown', label: 'Unknown' },
  ]

  let expanded = $state(false)
  let title = $state('')
  let groupBy = $state<GroupByField>('srcIp')
  let filters = $state<Filters>(emptyFilters())

  function reset() {
    title = ''
    groupBy = 'srcIp'
    filters = emptyFilters()
    expanded = false
  }

  function save() {
    if (!title.trim()) return
    topTalkerWidgetsState.add(title, groupBy, filters)
    reset()
  }
;
async () => {

if(!expanded){
   { svelteHTML.createElement("button", {   "class":`trigger`,"onclick":() => (expanded = true),});
        
   }
}else{
   { svelteHTML.createElement("div", { "class":`form`,});
     { svelteHTML.createElement("div", { "class":`header`,});    }

     { svelteHTML.createElement("input", {         "type":`text`,"placeholder":`Title (e.g. SSH attempts)`,"bind:value":title,"aria-label":`Widget title`,});/*Ωignore_startΩ*/() => title = __sveltets_2_any(null);/*Ωignore_endΩ*/}

     { svelteHTML.createElement("label", { "class":`field`,});
       { svelteHTML.createElement("span", {});  }
       { svelteHTML.createElement("select", {   "bind:value":groupBy,"aria-label":`Group by`,});/*Ωignore_startΩ*/() => groupBy = __sveltets_2_any(null);/*Ωignore_endΩ*/
           for(let f of __sveltets_2_ensureArray(GROUP_BY_FIELDS)){f;
           { svelteHTML.createElement("option", { "value":f,});GROUP_BY_LABELS[f]; }
        }
       }
     }

     { svelteHTML.createElement("div", { "class":`filters`,});
       { svelteHTML.createElement("select", {   "bind:value":filters.device,"aria-label":`Device`,});/*Ωignore_startΩ*/() => filters.device = __sveltets_2_any(null);/*Ωignore_endΩ*/
         { svelteHTML.createElement("option", {"value":"",});  }
           for(let d of __sveltets_2_ensureArray(appState.devices)){d.id;
           { svelteHTML.createElement("option", { "value":d.id,});d.name; }
        }
       }

       { svelteHTML.createElement("select", {   "bind:value":filters.action,"aria-label":`Action`,});/*Ωignore_startΩ*/() => filters.action = __sveltets_2_any(null);/*Ωignore_endΩ*/
           for(let a of __sveltets_2_ensureArray(actions)){a.value;
           { svelteHTML.createElement("option", { "value":a.value,});a.label; }
        }
       }

       { svelteHTML.createElement("input", {        "type":`text`,"placeholder":`Protocol`,"bind:value":filters.protocol,"aria-label":`Protocol`,});/*Ωignore_startΩ*/() => filters.protocol = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       { svelteHTML.createElement("input", {        "type":`text`,"placeholder":`IP or CIDR`,"bind:value":filters.ip,"aria-label":`IP address or CIDR`,});/*Ωignore_startΩ*/() => filters.ip = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       { svelteHTML.createElement("input", {          "type":`text`,"inputmode":`numeric`,"placeholder":`Port`,"bind:value":filters.port,"aria-label":`Port`,});/*Ωignore_startΩ*/() => filters.port = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       { svelteHTML.createElement("input", {        "type":`text`,"placeholder":`Interface`,"bind:value":filters.interface,"aria-label":`Interface`,});/*Ωignore_startΩ*/() => filters.interface = __sveltets_2_any(null);/*Ωignore_endΩ*/}

       { svelteHTML.createElement("select", {   "bind:value":filters.srcScope,"aria-label":`Source scope`,});/*Ωignore_startΩ*/() => filters.srcScope = __sveltets_2_any(null);/*Ωignore_endΩ*/
         { svelteHTML.createElement("option", {"value":"",});  }
         { svelteHTML.createElement("option", { "value":`internal`,});  }
         { svelteHTML.createElement("option", { "value":`external`,});  }
       }

       { svelteHTML.createElement("select", {   "bind:value":filters.dstScope,"aria-label":`Destination scope`,});/*Ωignore_startΩ*/() => filters.dstScope = __sveltets_2_any(null);/*Ωignore_endΩ*/
         { svelteHTML.createElement("option", {"value":"",});  }
         { svelteHTML.createElement("option", { "value":`internal`,});  }
         { svelteHTML.createElement("option", { "value":`external`,});  }
       }

       { svelteHTML.createElement("div", { "class":`rule-group`,});
         { svelteHTML.createElement("input", {         "type":`text`,"placeholder":filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…',"bind:value":filters.rule,"aria-label":`Rule filter`,});/*Ωignore_startΩ*/() => filters.rule = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         { svelteHTML.createElement("button", {          "class":`regex-toggle`,"onclick":() => (filters.ruleRegex = !filters.ruleRegex),"title":`Treat the rule filter as a regular expression`,"aria-pressed":filters.ruleRegex,});filters.ruleRegex;
          
         }
       }
     }

     { svelteHTML.createElement("div", { "class":`actions`,});
       { svelteHTML.createElement("button", {   "class":`cancel`,"onclick":reset,});  }
       { svelteHTML.createElement("button", {     "class":`save`,"onclick":save,"disabled":!title.trim(),});  }
     }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const AddTopTalkerWidget__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type AddTopTalkerWidget__SvelteComponent_ = ReturnType<typeof AddTopTalkerWidget__SvelteComponent_>;
/*Ωignore_endΩ*/export default AddTopTalkerWidget__SvelteComponent_;