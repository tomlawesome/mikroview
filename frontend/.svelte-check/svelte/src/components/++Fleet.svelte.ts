///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Multi-router-fleet health view (issue #98): every known device (both
// configured, from config.yaml's `devices` list, and auto-discovered --
// seen on the wire but not yet added there) in one table, with the
// server-computed live/stale/never-seen status GET /api/devices now
// reports (see internal/api/rest.go's deviceView/deviceStatus). This is
// the richer, dedicated view the toolbar's small DeviceStatus dot-strip
// was never meant to replace -- that one stays a glance-and-go
// indicator (see its own doc comment); this one is where you'd actually
// come to check on a whole fleet.

import { appState } from '../lib/state.svelte'
import { flagsState } from '../lib/flags.svelte'
import { formatRelative, formatHM } from '../lib/format'
import type { Device } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  

  // How far back "recent activity" looks, client-side, from the live
  // event buffer -- a rough per-device rate to complement the lifetime
  // eventCount GET /api/devices already reports, without needing a new
  // backend endpoint. 5 minutes mirrors globalSpikeCheckInterval's
  // neighborhood of "recent enough to mean something, not so short it's
  // noisy between polls."
  const RECENT_WINDOW_MS = 5 * 60 * 1000

  const STATUS_LABEL: Record<Device['status'], string> = {
    live: 'Live',
    stale: 'Stale',
    never_seen: 'Never seen',
  }

  const rows = $derived(
    [...appState.devices].sort((a, b) => {
      // Configured devices first (an auto-discovered source is secondary
      // information, not something you set out to monitor), then by
      // status severity (stale/never-seen surfaced above live -- the
      // whole point of a fleet view is spotting the ones that need a
      // look), then alphabetically so the order is otherwise stable.
      if (a.configured !== b.configured) return a.configured ? -1 : 1
      const severity: Record<Device['status'], number> = { stale: 0, never_seen: 1, live: 2 }
      if (severity[a.status] !== severity[b.status]) return severity[a.status] - severity[b.status]
      return a.name.localeCompare(b.name)
    }),
  )

  function recentCount(deviceId: string): number {
    const cutoff = appState.now - RECENT_WINDOW_MS
    let n = 0
    for (const e of appState.events) {
      if (e.deviceId === deviceId && e.receivedAt >= cutoff) n++
    }
    return n
  }

  // True when this device has an active (unacknowledged) device_silence
  // flag -- distinct from status === 'stale': the flag only exists for a
  // *configured* device that was active and went quiet past
  // deviceStaleAfter, while `status` also covers auto-discovered devices
  // and a shorter/different threshold could in principle apply (today
  // they share deviceStaleAfter, but the API doesn't guarantee that).
  function hasActiveSilenceFlag(deviceId: string): boolean {
    return flagsState.list.some((f) => f.type === 'device_silence' && f.target === deviceId && !f.cleared)
  }
;
async () => {

 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
                 { svelteHTML.createElement("code", {});  }  
         { svelteHTML.createElement("strong", {});  }          
                        
               
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
             { svelteHTML.createElement("th", { "class":`num`,});  }
             { svelteHTML.createElement("th", { "class":`num`,});  }
           }
         }
         { svelteHTML.createElement("tbody", {});
             for(let d of __sveltets_2_ensureArray(rows)){d.id;
             { svelteHTML.createElement("tr", {  });d.status === 'stale';d.status === 'never_seen';
               { svelteHTML.createElement("td", { "class":`name-cell`,});
                 { svelteHTML.createElement("span", { "class":`name`,});d.name; }
                if(!d.configured){ { svelteHTML.createElement("span", { "class":`badge badge-unregistered`,});  }}
                if(hasActiveSilenceFlag(d.id)){ { svelteHTML.createElement("span", { "class":`badge badge-flag`,});  }}
               }
               { svelteHTML.createElement("td", {});
                 { svelteHTML.createElement("span", { "class":`status status-${d.status}`,});
                   { svelteHTML.createElement("span", { "class":`dot`,}); }
                  STATUS_LABEL[d.status];
                 }
               }
               { svelteHTML.createElement("td", {   "class":`mono`,"title":d.lastSeen ? formatHM(d.lastSeen) : '—',});
                d.status === 'never_seen' ? '—' : formatRelative(d.lastSeen, appState.now);
               }
               { svelteHTML.createElement("td", { "class":`mono dim`,});d.sourceIp; }
               { svelteHTML.createElement("td", { "class":`num mono`,});d.eventCount; }
               { svelteHTML.createElement("td", { "class":`num mono`,});recentCount(d.id); }
             }
          }
         }
       }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Fleet__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Fleet__SvelteComponent_ = ReturnType<typeof Fleet__SvelteComponent_>;
/*Ωignore_endΩ*/export default Fleet__SvelteComponent_;