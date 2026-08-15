///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Admin-only entity management (see internal/entities.Entity, issue
// #107) -- persisted (type, key) -> label/tags records covering hosts,
// rules, and (issue #109) ports. Two halves:
//
//  1. "Named entities": every persisted record, with the type/key/tags
//     add-or-edit form #107 shipped, plus inline label editing (click
//     a label to rename it in place without opening the form).
//  2. "Discovered": hosts/rules/ports seen in live data that have no
//     entity yet -- mirroring internal/device.Registry's own
//     "auto-discovered, shown even before configured" pattern, so a
//     user can name something without already knowing its raw IP/
//     rule-label/port number. See lib/discoveredEntities.ts for how
//     each of the three is derived.

import { onMount } from 'svelte'
import { entitiesState } from '../lib/entities.svelte'
import { appState } from '../lib/state.svelte'
import { fetchRules } from '../lib/api'
import { discoverHosts, discoverPorts, discoverRules } from '../lib/discoveredEntities'
import { formatRelative } from '../lib/format'
import type { Entity, RuleUsage } from '../lib/types';
function $$render() {
 const discoveredRow/*Ωignore_positionΩ*/ = (type: string, item: { key: string; lastSeen: string })/*Ωignore_startΩ*/: ReturnType<import('svelte').Snippet>/*Ωignore_endΩ*/ => { async ()/*Ωignore_positionΩ*/ => {
  const rk = rowKey(type, item.key);
   { svelteHTML.createElement("li", { "class":`row discovered`,});
     { svelteHTML.createElement("span", { "class":`type`,});type; }
     { svelteHTML.createElement("span", { "class":`key`,});item.key; }
    if(inlineKey === rk){
       {const $$action_0 = __sveltets_2_ensureAction(focusOnMount(svelteHTML.mapElementTag('input')));{ svelteHTML.createElement("input", __sveltets_2_union($$action_0), {            "class":`inline-input`,"type":`text`,"placeholder":`friendly name`,"bind:value":inlineDraft,"onkeydown":(e) => onInlineKeydown(e, type, item.key),});/*Ωignore_startΩ*/() => inlineDraft = __sveltets_2_any(null);/*Ωignore_endΩ*/}}
       { svelteHTML.createElement("span", { "class":`row-actions`,});
         { svelteHTML.createElement("button", {   "class":`cancel`,"onclick":cancelInline,});  }
         { svelteHTML.createElement("button", {     "class":`save`,"disabled":inlineSaving,"onclick":() => saveInline(type, item.key),});
          inlineSaving ? 'Saving…' : 'Save';
         }
       }
    }else{
       { svelteHTML.createElement("span", { "class":`label unnamed`,});   }
       { svelteHTML.createElement("span", { "class":`row-actions`,});
         { svelteHTML.createElement("button", {   "class":`name-it`,"onclick":() => startInline(type, item.key, ''),});  }
       }
    }
     { svelteHTML.createElement("span", { "class":`seen`,});  formatRelative(item.lastSeen, appState.now); }
   }
  if(inlineKey === rk && inlineError){
     { svelteHTML.createElement("p", { "class":`error`,});inlineError; }
  }
};return __sveltets_2_any(0)};
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  // '' means "not currently editing" -- the add form and the edit form
  // are the same form (Upsert already treats create/replace as one
  // operation), so there's only ever one draft in flight.
  let editingKey = $state<{ type: string; key: string } | null>(null)

  let draftType = $state('host')
  let draftKey = $state('')
  let draftLabel = $state('')
  let draftTags = $state('')

  let error = $state<string | null>(null)
  let saving = $state(false)
  let deletingKey = $state<string | null>(null)

  // rulesUsage backs the "discovered rules" section -- GET /api/rules'
  // full history (issue #103's internal/rules.Store), fetched once per
  // panel open the same way entitiesState.refresh() is triggered by
  // NavMenu's toggleEntities (this component is unmounted/remounted on
  // every view toggle, so onMount firing once per open is exactly right).
  let rulesUsage = $state<RuleUsage[]>([])
  let rulesError = $state(false)

  onMount(() => {
    fetchRules()
      .then((r) => (rulesUsage = r))
      .catch(() => (rulesError = true))
  })

  const discoveredRules = $derived(discoverRules(rulesUsage, entitiesState.list))
  const discoveredHosts = $derived(discoverHosts(appState.events, entitiesState.list))
  const discoveredPorts = $derived(discoverPorts(appState.events, entitiesState.list))

  function resetDraft() {
    editingKey = null
    draftType = 'host'
    draftKey = ''
    draftLabel = ''
    draftTags = ''
    error = null
  }

  function startEdit(e: Entity) {
    cancelInline()
    editingKey = { type: e.type, key: e.key }
    draftType = e.type
    draftKey = e.key
    draftLabel = e.label ?? ''
    draftTags = (e.tags ?? []).join(', ')
    error = null
  }

  function parseTags(v: string): string[] {
    return v
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
  }

  async function submit(e: Event) {
    e.preventDefault()
    error = null
    saving = true
    const err = await entitiesState.upsert({
      type: draftType.trim(),
      key: draftKey.trim(),
      label: draftLabel.trim(),
      tags: parseTags(draftTags),
    })
    saving = false
    if (err) {
      error = err
      return
    }
    resetDraft()
  }

  async function remove(t: Entity) {
    deletingKey = t.type + ':' + t.key
    await entitiesState.remove(t.type, t.key)
    deletingKey = null
    if (editingKey?.type === t.type && editingKey?.key === t.key) resetDraft()
  }

  // ---- Inline label editing -----------------------------------------
  // Shared by both halves of this view: a named entity's label cell
  // (rename in place), and a discovered row's "Name it" affordance
  // (create a new entity with just a label, no tags). Only one row can
  // be mid-edit at a time -- same single-draft-in-flight reasoning
  // editingKey above already follows for the full form.
  let inlineKey = $state<string | null>(null)
  let inlineDraft = $state('')
  let inlineSaving = $state(false)
  let inlineError = $state<string | null>(null)

  function rowKey(type: string, key: string): string {
    return type + ':' + key
  }

  function startInline(type: string, key: string, currentLabel: string) {
    resetDraft() // mutually exclusive with the full add/edit form
    inlineKey = rowKey(type, key)
    inlineDraft = currentLabel
    inlineError = null
  }

  function cancelInline() {
    inlineKey = null
    inlineError = null
  }

  async function saveInline(type: string, key: string, tags: string[] = []) {
    inlineError = null
    inlineSaving = true
    const err = await entitiesState.upsert({ type, key, label: inlineDraft.trim(), tags })
    inlineSaving = false
    if (err) {
      inlineError = err
      return
    }
    inlineKey = null
  }

  function onInlineKeydown(e: KeyboardEvent, type: string, key: string, tags: string[] = []) {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveInline(type, key, tags)
    } else if (e.key === 'Escape') {
      cancelInline()
    }
  }

  // A plain HTML `autofocus` attribute trips svelte's a11y-autofocus
  // warning; this action gets the same "ready to type immediately"
  // behavior without it, and re-runs correctly since the input this is
  // attached to only exists (is (re)mounted) while inlineKey matches.
  function focusOnMount(node: HTMLInputElement) {
    node.focus()
  }
;
async () => {



 { svelteHTML.createElement("div", { "class":`page scrollbar`,});
   { svelteHTML.createElement("p", { "class":`intro`,});
                    
           { svelteHTML.createElement("strong", {});  }      
                          
     
   }

   { svelteHTML.createElement("form", {   "class":`form`,"onsubmit":submit,});
     { svelteHTML.createElement("div", { "class":`form-title`,});editingKey ? `Editing ${editingKey.type}:${editingKey.key}` : 'Add entity'; }
     { svelteHTML.createElement("div", { "class":`form-row`,});
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {});  }
         { svelteHTML.createElement("input", {          "type":`text`,"list":`entity-types`,"placeholder":`host`,"bind:value":draftType,"required":true,"disabled":!!editingKey,});/*Ωignore_startΩ*/() => draftType = __sveltets_2_any(null);/*Ωignore_endΩ*/}
         { svelteHTML.createElement("datalist", { "id":`entity-types`,});
           { svelteHTML.createElement("option", { "value":`host`,}); }
           { svelteHTML.createElement("option", { "value":`rule`,}); }
           { svelteHTML.createElement("option", { "value":`port`,}); }
         }
       }
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {});  }
         { svelteHTML.createElement("input", {          "type":`text`,"placeholder":`192.168.1.50, r13, or 8291`,"bind:value":draftKey,"required":true,"disabled":!!editingKey,});/*Ωignore_startΩ*/() => draftKey = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
       { svelteHTML.createElement("label", { "class":`field`,});
         { svelteHTML.createElement("span", {});  }
         { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`friendly name`,"bind:value":draftLabel,});/*Ωignore_startΩ*/() => draftLabel = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
       { svelteHTML.createElement("label", { "class":`field grow`,});
         { svelteHTML.createElement("span", {});  }
         { svelteHTML.createElement("input", {      "type":`text`,"placeholder":`trusted-mail-sender`,"bind:value":draftTags,});/*Ωignore_startΩ*/() => draftTags = __sveltets_2_any(null);/*Ωignore_endΩ*/}
       }
     }
    if(error){
       { svelteHTML.createElement("p", { "class":`error`,});error; }
    }
     { svelteHTML.createElement("div", { "class":`form-actions`,});
      if(editingKey){
         { svelteHTML.createElement("button", {     "type":`button`,"class":`cancel`,"onclick":resetDraft,});  }
      }
       { svelteHTML.createElement("button", {     "type":`submit`,"class":`save`,"disabled":saving,});
        saving ? 'Saving…' : editingKey ? 'Save changes' : 'Add entity';
       }
     }
   }

   { svelteHTML.createElement("section", { "class":`section`,});
     { svelteHTML.createElement("h3", { "class":`section-title`,});  }
    if(entitiesState.list.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});            }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let e of __sveltets_2_ensureArray(entitiesState.list)){e.type + ':' + e.key;
          const rk = rowKey(e.type, e.key);
           { svelteHTML.createElement("li", { "class":`row`,});
             { svelteHTML.createElement("span", { "class":`type`,});e.type; }
             { svelteHTML.createElement("span", { "class":`key`,});e.key; }
            if(inlineKey === rk){
               {const $$action_0 = __sveltets_2_ensureAction(focusOnMount(svelteHTML.mapElementTag('input')));{ svelteHTML.createElement("input", __sveltets_2_union($$action_0), {            "class":`inline-input`,"type":`text`,"placeholder":`friendly name`,"bind:value":inlineDraft,"onkeydown":(ev) => onInlineKeydown(ev, e.type, e.key, e.tags ?? []),});/*Ωignore_startΩ*/() => inlineDraft = __sveltets_2_any(null);/*Ωignore_endΩ*/}}
            }else{
               { svelteHTML.createElement("button", {       "class":`label label-btn`,"onclick":() => startInline(e.type, e.key, e.label ?? ''),"title":`Click to edit label`,});
                e.label || '— click to name —';
               }
            }
             { svelteHTML.createElement("span", { "class":`tags`,});
                 for(let tag of __sveltets_2_ensureArray(e.tags ?? [])){tag;
                 { svelteHTML.createElement("span", { "class":`tag`,});tag; }
              }
             }
             { svelteHTML.createElement("span", { "class":`row-actions`,});
              if(inlineKey === rk){
                 { svelteHTML.createElement("button", {   "class":`cancel`,"onclick":cancelInline,});  }
                 { svelteHTML.createElement("button", {     "class":`save`,"disabled":inlineSaving,"onclick":() => saveInline(e.type, e.key, e.tags ?? []),});
                  inlineSaving ? 'Saving…' : 'Save';
                 }
              }else{
                 { svelteHTML.createElement("button", {   "class":`edit`,"onclick":() => startEdit(e),});  }
                 { svelteHTML.createElement("button", {     "class":`delete`,"disabled":deletingKey === rk,"onclick":() => remove(e),});
                  deletingKey === rk ? 'Removing…' : 'Remove';
                 }
              }
             }
           }
          if(inlineKey === rk && inlineError){
             { svelteHTML.createElement("p", { "class":`error`,});inlineError; }
          }
        }
       }
    }
   }

   { svelteHTML.createElement("section", { "class":`section`,});
     { svelteHTML.createElement("h3", { "class":`section-title`,});  }
     { svelteHTML.createElement("p", { "class":`section-intro`,});
                   
      if(rulesError){ { svelteHTML.createElement("span", { "class":`fetch-error`,});    }}
     }
    if(discoveredRules.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});   }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let item of __sveltets_2_ensureArray(discoveredRules)){item.key;
          ;__sveltets_2_ensureSnippet(discoveredRow('rule', item));
        }
       }
    }
   }

   { svelteHTML.createElement("section", { "class":`section`,});
     { svelteHTML.createElement("h3", { "class":`section-title`,});  }
     { svelteHTML.createElement("p", { "class":`section-intro`,});
                      
         
     }
    if(discoveredHosts.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});   }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let item of __sveltets_2_ensureArray(discoveredHosts)){item.key;
          ;__sveltets_2_ensureSnippet(discoveredRow('host', item));
        }
       }
    }
   }

   { svelteHTML.createElement("section", { "class":`section`,});
     { svelteHTML.createElement("h3", { "class":`section-title`,});  }
     { svelteHTML.createElement("p", { "class":`section-intro`,});
                      
         
     }
    if(discoveredPorts.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});   }
    }else{
       { svelteHTML.createElement("ul", { "class":`list`,});
           for(let item of __sveltets_2_ensureArray(discoveredPorts)){item.key;
          ;__sveltets_2_ensureSnippet(discoveredRow('port', item));
        }
       }
    }
   }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Entities__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Entities__SvelteComponent_ = ReturnType<typeof Entities__SvelteComponent_>;
/*Ωignore_endΩ*/export default Entities__SvelteComponent_;