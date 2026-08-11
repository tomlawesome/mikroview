///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Admin-only audit log (issue #112): a read-only, most-recent-first
// table of every admin-privileged mutation mikroview has recorded --
// who created a user, changed a detector setting, upserted/deleted an
// entity, created/revoked an API token, or removed a permanent flag
// exclusion. See internal/audit.Entry -- nothing here is editable from
// the UI, mirroring Fleet.svelte's plain read-only table shape rather
// than Entities.svelte's form-backed CRUD one, since there's nothing
// to create/edit/delete about a historical log entry.

import { onMount } from 'svelte'
import { auditState } from '../lib/audit.svelte'
import { appState } from '../lib/state.svelte'
import { formatRelative, formatHM } from '../lib/format';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  

  onMount(() => {
    auditState.refresh()
  })

  const rows = $derived([...auditState.list].reverse())
;
async () => {

 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
                  
                   
              
    if(auditState.hasMore){
       { svelteHTML.createElement("span", { "class":`truncated`,});      }
    }
   }

  if(rows.length === 0){
     { svelteHTML.createElement("div", { "class":`empty`,});     }
  }else{
     { svelteHTML.createElement("div", { "class":`table-wrap`,});
       { svelteHTML.createElement("table", {});
         { svelteHTML.createElement("thead", {});
           { svelteHTML.createElement("tr", {});
             { svelteHTML.createElement("th", {});  }
             { svelteHTML.createElement("th", {});  }
             { svelteHTML.createElement("th", {});  }
             { svelteHTML.createElement("th", {});  }
             { svelteHTML.createElement("th", {});  }
           }
         }
         { svelteHTML.createElement("tbody", {});
             for(let e of __sveltets_2_ensureArray(rows)){e.id;
             { svelteHTML.createElement("tr", {});
               { svelteHTML.createElement("td", {   "class":`mono`,"title":formatHM(e.timestamp),});formatRelative(e.timestamp, appState.now); }
               { svelteHTML.createElement("td", { "class":`actor`,});e.actor; }
               { svelteHTML.createElement("td", { "class":`mono action`,});e.action; }
               { svelteHTML.createElement("td", { "class":`mono target`,});e.target; }
               { svelteHTML.createElement("td", { "class":`dim`,});e.detail || '—'; }
             }
          }
         }
       }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const AuditLog__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type AuditLog__SvelteComponent_ = ReturnType<typeof AuditLog__SvelteComponent_>;
/*Ωignore_endΩ*/export default AuditLog__SvelteComponent_;