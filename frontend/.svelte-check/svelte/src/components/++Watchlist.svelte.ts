///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Watchlist (#243): what Control Ports grew into. Two modes per entry:
//
//  - Non-inverted -- "record attempts against these ports," the same
//    thing Control Ports did, generalised beyond SSH/Telnet and now
//    persisted server-side (internal/matchlog) instead of only ever
//    existing in the live view's own capped, volatile client buffer.
//  - Inverted -- "this device should only ever reach these
//    destinations." A new inverted entry starts Observing: it records
//    what the device actually touches without raising anything, so you
//    can review real evidence and promote what's expected before
//    anything is treated as a violation.
//
// Admin-only throughout, matching GET /api/watchlist/entries' own gate
// (see internal/api's authzMatrix) -- unlike the match query API
// (accessUser, and reachable via a read-only token for external
// correlation), entry management itself is administrative
// configuration about the network, the same tier as Entities/Audit/
// Exclusions.

import { onMount } from 'svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import type { WatchlistEntry, WatchlistMatch, WatchlistPermittedDest } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  onMount(() => {
    watchlistState.refresh()
  })

  // --- Add/edit form -----------------------------------------------

  let editingId = $state<string | null>(null)
  let draftName = $state('')
  let draftInvert = $state(false)
  let draftSourceMac = $state('')
  let draftSourceIp = $state('')
  let draftDestIp = $state('')
  let draftPorts = $state('')
  let draftIncludeStructuralNoise = $state(false)

  let error = $state<string | null>(null)
  let saving = $state(false)
  let deletingId = $state<string | null>(null)

  function resetDraft() {
    editingId = null
    draftName = ''
    draftInvert = false
    draftSourceMac = ''
    draftSourceIp = ''
    draftDestIp = ''
    draftPorts = ''
    draftIncludeStructuralNoise = false
    error = null
  }

  function startEdit(e: WatchlistEntry) {
    editingId = e.id
    draftName = e.name ?? ''
    draftInvert = !!e.invert
    draftSourceMac = e.source?.mac ?? ''
    draftSourceIp = e.source?.ip ?? ''
    draftDestIp = e.destIp ?? ''
    draftPorts = (e.ports ?? []).join(', ')
    draftIncludeStructuralNoise = !!e.includeStructuralNoise
    error = null
  }

  // Mirrors Entities.svelte's parseTags shape -- comma/whitespace
  // separated, blank entries dropped, non-numeric entries dropped rather
  // than rejecting the whole field (a stray comma or typo shouldn't lose
  // every other port already typed).
  function parsePorts(v: string): number[] {
    return v
      .split(/[,\s]+/)
      .map((s) => Number(s.trim()))
      .filter((n) => Number.isInteger(n) && n > 0)
  }

  async function submit(ev: Event) {
    ev.preventDefault()
    saving = true
    error = null
    try {
      const req = {
        name: draftName.trim() || undefined,
        invert: draftInvert,
        source:
          draftSourceMac.trim() || draftSourceIp.trim()
            ? { mac: draftSourceMac.trim() || undefined, ip: draftSourceIp.trim() || undefined }
            : undefined,
        destIp: draftInvert ? undefined : draftDestIp.trim() || undefined,
        ports: draftInvert ? undefined : parsePorts(draftPorts),
        includeStructuralNoise: draftInvert ? draftIncludeStructuralNoise : undefined,
      }
      const err = editingId ? await watchlistState.update(editingId, req) : await watchlistState.create(req)
      if (err) {
        error = err
      } else {
        resetDraft()
      }
    } finally {
      saving = false
    }
  }

  async function remove(e: WatchlistEntry) {
    if (!confirm(`Remove the watchlist entry "${e.name || e.id}"? This does not delete any matches it already recorded.`))
      return
    deletingId = e.id
    try {
      await watchlistState.remove(e.id)
    } finally {
      deletingId = null
    }
  }

  // --- Observe/promote/matches, expanded per entry ------------------

  let expandedId = $state<string | null>(null)
  let togglingObserve = $state<string | null>(null)
  let promoting = $state<string | null>(null)
  let matchesByEntry = $state<Record<string, WatchlistMatch[] | 'loading' | 'error'>>({})

  function toggleExpand(id: string) {
    expandedId = expandedId === id ? null : id
  }

  async function toggleObserving(e: WatchlistEntry) {
    togglingObserve = e.id
    try {
      await watchlistState.setObserving(e.id, !e.observing)
    } finally {
      togglingObserve = null
    }
  }

  async function promoteOne(e: WatchlistEntry, d: WatchlistPermittedDest) {
    promoting = e.id + d.destIp + d.port
    try {
      await watchlistState.promote(e.id, [d])
    } finally {
      promoting = null
    }
  }

  // loadMatches is called each time the matches panel is opened rather
  // than cached indefinitely -- a match log is append-only and can
  // change between views, and the volumes here (an entry's own recent
  // matches) are small enough that refetching on open is cheap.
  async function loadMatches(e: WatchlistEntry) {
    if (!e.source?.mac && !e.source?.ip) return
    matchesByEntry[e.id] = 'loading'
    try {
      matchesByEntry[e.id] = await watchlistState.matchesFor(e.source.mac, e.source.ip)
    } catch {
      matchesByEntry[e.id] = 'error'
    }
  }

  function formatTime(iso: string): string {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
  }

  function sourceLabel(e: WatchlistEntry): string {
    if (e.source?.mac) return e.source.mac
    if (e.source?.ip) return e.source.ip
    return 'any source'
  }
;
async () => {

 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
          { svelteHTML.createElement("strong", {});  }        
             { svelteHTML.createElement("strong", {});  }        
                { svelteHTML.createElement("strong", {});  }  
                     
               
   }

   { svelteHTML.createElement("form", {   "class":`form`,"onsubmit":submit,});
     { svelteHTML.createElement("div", { "class":`form-title`,});editingId ? 'Editing entry' : 'Add entry'; }
     { svelteHTML.createElement("div", { "class":`form-row`,});
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {});  }
         { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`SSH watch`,"bind:value":draftName,});/*Ωignore_startΩ*/() => draftName = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
       { svelteHTML.createElement("label", { "class":`field checkbox-field`,});
         { svelteHTML.createElement("span", {});
           { svelteHTML.createElement("input", {    "type":`checkbox`,"bind:checked":draftInvert,});/*Ωignore_startΩ*/() => draftInvert = __sveltets_2_any(null);/*Ωignore_endΩ*/}
                   
         }
       }
     }

     { svelteHTML.createElement("div", { "class":`form-row`,});
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {}); draftInvert ? ' (required)' : ' (optional)'; }
         { svelteHTML.createElement("input", {        "type":`text`,"placeholder":`aa:bb:cc:dd:ee:ff`,"bind:value":draftSourceMac,"required":draftInvert,});/*Ωignore_startΩ*/() => draftSourceMac = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {});             }
         { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`192.168.1.50`,"bind:value":draftSourceIp,});/*Ωignore_startΩ*/() => draftSourceIp = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
     }

    if(!draftInvert){
       { svelteHTML.createElement("div", { "class":`form-row`,});
         { svelteHTML.createElement("label", { "class":`field`,});
           { svelteHTML.createElement("span", {});   }
           { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`any destination`,"bind:value":draftDestIp,});/*Ωignore_startΩ*/() => draftDestIp = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         }
         { svelteHTML.createElement("label", { "class":`field grow`,});
           { svelteHTML.createElement("span", {});   }
            { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`22, 23, 3389`,"bind:value":draftPorts,"required":true,});/*Ωignore_startΩ*/() => draftPorts = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         }
       }
    }else{
       { svelteHTML.createElement("div", { "class":`form-row`,});
         { svelteHTML.createElement("label", { "class":`field checkbox-field`,});
           { svelteHTML.createElement("span", {});
             { svelteHTML.createElement("input", {    "type":`checkbox`,"bind:checked":draftIncludeStructuralNoise,});/*Ωignore_startΩ*/() => draftIncludeStructuralNoise = __sveltets_2_any(null);/*Ωignore_endΩ*/}
                      
           }
         }
       }
    }

    if(error){
       { svelteHTML.createElement("p", { "class":`error`,});error; }
    }
     { svelteHTML.createElement("div", { "class":`form-actions`,});
      if(editingId){
         { svelteHTML.createElement("button", {     "type":`button`,"class":`cancel`,"onclick":resetDraft,});  }
      }
       { svelteHTML.createElement("button", {     "type":`submit`,"class":`save`,"disabled":saving,});
        saving ? 'Saving…' : editingId ? 'Save changes' : 'Add entry';
       }
     }
   }

   { svelteHTML.createElement("section", { "class":`section`,});
     { svelteHTML.createElement("h3", { "class":`section-title`,});  }
    if(watchlistState.entries.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});        }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let e of __sveltets_2_ensureArray(watchlistState.entries)){e.id;
           { svelteHTML.createElement("li", { "class":`card`,});
             { svelteHTML.createElement("button", {   "class":`card-main`,"onclick":() => toggleExpand(e.id),});
               { svelteHTML.createElement("span", { "class":`name`,});e.name || '(unnamed)'; }
              if(e.invert){
                 { svelteHTML.createElement("span", { "class":`badge invert`,});  }
                if(e.observing){
                   { svelteHTML.createElement("span", { "class":`badge observing`,});  }
                }
              }
               { svelteHTML.createElement("span", { "class":`source`,});sourceLabel(e); }
              if(e.invert){
                 { svelteHTML.createElement("span", { "class":`detail`,});(e.permitted ?? []).length;  (e.observed ?? []).length;   }
              }else{
                 { svelteHTML.createElement("span", { "class":`detail`,}); (e.ports ?? []).join(', ');e.destIp ? ` → ${e.destIp}` : ''; }
              }
             }
             { svelteHTML.createElement("span", { "class":`row-actions`,});
               { svelteHTML.createElement("button", {   "class":`edit`,"onclick":() => startEdit(e),});  }
               { svelteHTML.createElement("button", {     "class":`delete`,"disabled":deletingId === e.id,"onclick":() => remove(e),});
                deletingId === e.id ? 'Removing…' : 'Remove';
               }
             }

            if(expandedId === e.id){
               { svelteHTML.createElement("div", { "class":`expanded`,});
                if(e.invert){
                   { svelteHTML.createElement("div", { "class":`expanded-row`,});
                     { svelteHTML.createElement("button", {     "class":`observe-toggle`,"disabled":togglingObserve === e.id,"onclick":() => toggleObserving(e),});
                      togglingObserve === e.id
                        ? 'Saving…'
                        : e.observing
                          ? 'Stop observing (start enforcing)'
                          : 'Resume observing';
                     }
                    if(e.observing){
                       { svelteHTML.createElement("span", { "class":`hint`,});            }
                    }else{
                       { svelteHTML.createElement("span", { "class":`hint`,});           }
                    }
                   }

                   { svelteHTML.createElement("div", { "class":`sub-section`,});
                     { svelteHTML.createElement("h4", {}); (e.permitted ?? []).length;  }
                    if((e.permitted ?? []).length === 0){
                       { svelteHTML.createElement("p", { "class":`empty small`,});   }
                    }else{
                       { svelteHTML.createElement("ul", { "class":`dest-list`,});
                           for(let p of __sveltets_2_ensureArray(e.permitted ?? [])){p.destIp + ':' + p.port;
                           { svelteHTML.createElement("li", {});p.destIp; p.port; }
                        }
                       }
                    }
                   }

                   { svelteHTML.createElement("div", { "class":`sub-section`,});
                     { svelteHTML.createElement("h4", {});  (e.observed ?? []).length;  }
                    if((e.observed ?? []).length === 0){
                       { svelteHTML.createElement("p", { "class":`empty small`,});                }
                    }else{
                       { svelteHTML.createElement("ul", { "class":`dest-list`,});
                           for(let o of __sveltets_2_ensureArray(e.observed ?? [])){o.destIp + ':' + o.port;
                           { svelteHTML.createElement("li", {});
                             { svelteHTML.createElement("span", { "class":`dest`,});o.destIp; o.port; }
                             { svelteHTML.createElement("span", { "class":`dest-meta`,});
                               o.count;   formatTime(o.lastSeen);
                             }
                             { svelteHTML.createElement("button", {       "class":`promote`,"disabled":promoting === e.id + o.destIp + o.port,"onclick":() => promoteOne(e, { destIp: o.destIp, port: o.port }),});
                              promoting === e.id + o.destIp + o.port ? 'Promoting…' : 'Promote';
                             }
                           }
                        }
                       }
                    }
                   }
                }

                if(e.source?.mac || e.source?.ip){
                   { svelteHTML.createElement("div", { "class":`sub-section`,});
                     { svelteHTML.createElement("h4", {});  }
                    if(!matchesByEntry[e.id]){
                       { svelteHTML.createElement("button", {   "class":`load-matches`,"onclick":() => loadMatches(e),});   }
                    } else if (matchesByEntry[e.id] === 'loading'){
                       { svelteHTML.createElement("p", { "class":`empty small`,});  }
                    } else if (matchesByEntry[e.id] === 'error'){
                       { svelteHTML.createElement("p", { "class":`error`,});    }
                    } else if ((matchesByEntry[e.id] as WatchlistMatch[]).length === 0){
                       { svelteHTML.createElement("p", { "class":`empty small`,});        }
                    }else{
                       { svelteHTML.createElement("ul", { "class":`match-list`,});
                           for(let m of __sveltets_2_ensureArray(matchesByEntry[e.id] as WatchlistMatch[])){m.id;
                           { svelteHTML.createElement("li", {});
                             { svelteHTML.createElement("span", { "class":`dest`,});m.tuple.destIp; m.tuple.port; }
                             { svelteHTML.createElement("span", { "class":`dest-meta`,});
                              m.count;   formatTime(m.lastSeen);   m.event.action;
                             }
                           }
                        }
                       }
                    }
                   }
                }
               }
            }
           }
        }
       }
    }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Watchlist__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Watchlist__SvelteComponent_ = ReturnType<typeof Watchlist__SvelteComponent_>;
/*Ωignore_endΩ*/export default Watchlist__SvelteComponent_;