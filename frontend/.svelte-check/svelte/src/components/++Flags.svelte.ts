///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Behavioral flags raised by internal/detect (see docs/configuration.md's
// "Behavioral flags" section) -- an interrogation aid, not an IPS: every
// action here is a human reviewing and clearing a flag, never mikroview
// acting on traffic itself.

import { flagsState, extractSourceIp } from '../lib/flags.svelte'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { formatHM, countryFlag, isPublicIp } from '../lib/format'
import { flagLayoutState, type FlagColumns } from '../lib/flagLayout.svelte'
import { viewportState } from '../lib/viewport.svelte'
import ReputationDetails from './ReputationDetails.svelte'
import BarList from './BarList.svelte'
import IpInvestigateButton from './IpInvestigateButton.svelte'
import type { Flag, FlagType } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  // Same gate NavMenu uses for the Detectors view.
  const isAdminOrOpen = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // The stored preference collapses to 1 below the shared mobile
  // breakpoint regardless of what's selected (issue #199's responsive
  // floor) -- computed here in JS rather than as a CSS media query, so
  // it reuses viewportState's one 700px breakpoint (the same value
  // NavMenu/Toolbar/ThemeMenu already switch on) instead of a second
  // hardcoded copy of it, and so the *card* content also reverts to its
  // full, non-compact detail at exactly the width the grid itself
  // renders as one column. A CSS-only floor would narrow the grid but
  // leave the compact card styling active, which is the "unusably
  // narrow card" the floor exists to prevent, just moved one level down.
  const effectiveColumns = $derived<FlagColumns>(viewportState.isMobile ? 1 : flagLayoutState.columns)
  const compact = $derived(effectiveColumns > 1)

  // Which flag's split-Clear dropdown is open, if any -- one shared id
  // rather than per-card state, since at most one can be open at a time
  // and this list can be long. Closed on an outside click, Escape, or
  // picking the menu item (issue #198).
  let openClearMenuFor: string | null = $state(null)

  function toggleClearMenu(id: string) {
    openClearMenuFor = openClearMenuFor === id ? null : id
  }

  function onDocClickCloseClearMenu(e: MouseEvent) {
    if (!(e.target as HTMLElement).closest('.split-clear')) openClearMenuFor = null
  }

  function onKeydownCloseClearMenu(e: KeyboardEvent) {
    if (e.key === 'Escape') openClearMenuFor = null
  }

  $effect(() => {
    if (!openClearMenuFor) return
    document.addEventListener('click', onDocClickCloseClearMenu)
    document.addEventListener('keydown', onKeydownCloseClearMenu)
    return () => {
      document.removeEventListener('click', onDocClickCloseClearMenu)
      document.removeEventListener('keydown', onKeydownCloseClearMenu)
    }
  })

  // "Clear all" (issue #198): first click arms it (red, "Confirm"); the
  // second click on that same now-red button is the confirmation -- no
  // modal, because the second click *is* the deliberate second action.
  // Disarms itself after CLEAR_ALL_ARM_MS or when the pointer/focus
  // leaves, so an armed-but-abandoned state can't be triggered later by
  // an unrelated click landing back on the button.
  const CLEAR_ALL_ARM_MS = 4000
  let clearAllArmed = $state(false)
  let clearAllArmTimer: ReturnType<typeof setTimeout> | null = null
  let clearAllBusy = $state(false)

  function disarmClearAll() {
    clearAllArmed = false
    if (clearAllArmTimer) {
      clearTimeout(clearAllArmTimer)
      clearAllArmTimer = null
    }
  }

  async function onClearAllClick() {
    if (!clearAllArmed) {
      clearAllArmed = true
      clearAllArmTimer = setTimeout(disarmClearAll, CLEAR_ALL_ARM_MS)
      return
    }
    disarmClearAll()
    clearAllBusy = true
    try {
      await flagsState.clearAll()
    } finally {
      clearAllBusy = false
    }
  }

  let expandedId: string | null = $state(null)

  function toggleExpanded(id: string) {
    expandedId = expandedId === id ? null : id
  }

  // Which source IP's campaign card (see below) is currently expanded to
  // show its individual member flags -- null means every campaign card
  // is collapsed to just its summary row.
  let expandedGroup: string | null = $state(null)

  function toggleGroup(sourceIp: string) {
    expandedGroup = expandedGroup === sourceIp ? null : sourceIp
  }

  // Only true when there's actually something beyond `detail` to show --
  // avoids a dead "Details" button on flags with nothing extra (most
  // global_spike/rule_spike flags, or any flag when no reputation key is
  // configured).
  function hasExpandableDetail(f: Flag): boolean {
    return (
      !!f.country ||
      !!f.reputation ||
      !!f.evidence?.ports?.length ||
      !!f.evidence?.hosts?.length ||
      !!f.evidence?.nat
    )
  }

  // Same labels FlagsChart.svelte/Exclusions.svelte use -- duplicated
  // rather than shared, matching how ACTION_LABELS is already
  // independently duplicated in both EventsChart.svelte and
  // Dashboard.svelte in this codebase.
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

  // Sorted by firstSeen (not the fetch response's lastSeen-desc order --
  // see internal/flags.Store.List()) so a flag's position is fixed the
  // moment it first appears. lastSeen updates on every re-fire, not just
  // creation, so sorting by it made an already-visible flag you're
  // reading jump to the top of the list the instant it (or anything
  // else) re-fired on the next 5s poll -- jarring for something you're
  // mid-read on. Only a genuinely new flag entering the active set now
  // changes the ordering, which is the expected kind of layout change.
  const active = $derived(
    flagsState.list
      .filter((f) => !f.cleared)
      .sort((a, b) => new Date(b.firstSeen).getTime() - new Date(a.firstSeen).getTime()),
  )
  const cleared = $derived(flagsState.list.filter((f) => f.cleared).slice(0, 20))

  // "One actor, several signals" (issue #106): active flags sharing a
  // normalized source IP (flagsState.groupedBySource -- see that
  // derived's own doc comment for exactly which target shapes qualify)
  // collapse into a single campaign card instead of N separate cards,
  // in the same firstSeen-desc order `active` already uses. Each source
  // IP is represented once, at the position of its most-recent flag;
  // everything ungroupable (a lone flag from that source, or a target
  // with no single source IP to correlate on at all) renders exactly as
  // before.
  type ActiveItem = { kind: 'single'; flag: Flag } | { kind: 'group'; sourceIp: string; flags: Flag[] }

  const activeItems = $derived.by((): ActiveItem[] => {
    const seen = new Set<string>()
    const items: ActiveItem[] = []
    for (const f of active) {
      const ip = extractSourceIp(f.target)
      const group = ip ? flagsState.groupedBySource.get(ip) : undefined
      if (ip && group) {
        if (seen.has(ip)) continue
        seen.add(ip)
        items.push({ kind: 'group', sourceIp: ip, flags: group })
      } else {
        items.push({ kind: 'single', flag: f })
      }
    }
    return items
  })

  function groupTypeLabels(flags: Flag[]): string {
    return [...new Set(flags.map((f) => TYPE_LABELS[f.type]))].join(' · ')
  }

  function groupFirstSeen(flags: Flag[]): string {
    return flags.reduce((min, f) => (new Date(f.firstSeen) < new Date(min) ? f.firstSeen : min), flags[0].firstSeen)
  }

  function groupLastSeen(flags: Flag[]): string {
    return flags.reduce((max, f) => (new Date(f.lastSeen) > new Date(max) ? f.lastSeen : max), flags[0].lastSeen)
  }

  function filterToSource(sourceIp: string) {
    appState.setFilter('ip', sourceIp)
    appState.view = 'live'
  }

  // "Active flags by type" summary panel -- only types with at least one
  // active flag, ranked by count like every other BarList panel.
  const typeBreakdown = $derived(
    Object.entries(
      active.reduce<Partial<Record<FlagType, number>>>((counts, f) => {
        counts[f.type] = (counts[f.type] ?? 0) + 1
        return counts
      }, {}),
    )
      .map(([type, count]) => ({ label: TYPE_LABELS[type as FlagType], count: count ?? 0 }))
      .sort((a, b) => b.count - a.count),
  )

  // What a flag's target actually *is* varies by detector -- most are a
  // plain source IP, but distributed_brute_force is keyed by port,
  // rule_spike/stale_rule by rule label, repeated_drops by
  // "ip -> port N", device_silence by a device ID, and global_spike has
  // no filterable target at all. new_device's target is a MAC address
  // (see internal/flags.TypeNewDevice) -- the live view's Filters has no
  // MAC field to filter on, so it's not filterable either, same as
  // global_spike. Filtering on the right field (rather than always
  // assuming "ip") is what makes this click-through actually land on a
  // sensible pre-filtered view.
  function isFilterable(f: Flag): boolean {
    return f.type !== 'global_spike' && f.type !== 'new_device'
  }

  // The IP for a live abuse-check button on this card (issue #213), or
  // null if there is none worth checking. extractSourceIp already
  // screens out every target shape that isn't a bare IP (a rule label,
  // "port N", "global", a MAC) -- see its own doc comment -- so most
  // exclusions fall out of that for free rather than needing a second
  // type-by-type list to keep in step with filterToTarget's.
  //
  // device_silence is the one type that needs an explicit exclusion on
  // top of the shape check: an auto-discovered device's ID defaults to
  // its source IP (internal/device.Registry.Resolve), so its target can
  // be IP-shaped too -- but it identifies the device that went quiet,
  // not a source worth threat-checking, and #213 excludes it by name.
  function investigateIp(f: Flag): string | null {
    if (f.type === 'device_silence') return null
    const ip = extractSourceIp(f.target)
    return ip && isPublicIp(ip) ? ip : null
  }

  function filterToTarget(f: Flag) {
    switch (f.type) {
      case 'port_scan':
      case 'activity_spike':
      case 'critical_port':
      case 'outbound_anomaly':
      case 'internal_recon':
      case 'low_slow_scan':
      case 'off_hours_activity':
      case 'unexpected_mail_sender':
      case 'known_bad_ip':
        appState.setFilter('ip', f.target)
        break
      case 'distributed_brute_force':
        appState.setFilter('port', f.target.replace(/^port /, ''))
        break
      case 'rule_spike':
      case 'stale_rule':
        appState.setFilter('rule', f.target)
        break
      case 'repeated_drops':
        appState.setFilter('ip', f.target.split(' -> ')[0])
        break
      case 'device_silence':
        appState.setFilter('device', f.target)
        break
      case 'global_spike':
      case 'new_device':
        return
    }
    appState.view = 'live'
  }

  async function clear(id: string) {
    await flagsState.clear(id)
  }

  // "Clear and never flag this again" -- permanently excludes this
  // flag's exact (Type, Target) going forward (see internal/flags.
  // Store.Exclude's doc comment for why this is a deliberate permanent
  // suppression, not a timed snooze). Reviewing/undoing an exclusion
  // made by mistake is the admin-only "Manage exclusions" panel below,
  // not a confirmation dialog here.
  async function clearPermanent(id: string) {
    await flagsState.clearPermanent(id)
  }

  // Graded rather than a single color for every value -- a 12% confidence
  // score and a 95% one shouldn't read as equally worth attention at a
  // glance, mirroring the severity coloring ActionBadge already uses
  // elsewhere.
  function confidenceTier(c: number): 'low' | 'medium' | 'high' {
    if (c >= 70) return 'high'
    if (c >= 40) return 'medium'
    return 'low'
  }
;
async () => {

 { svelteHTML.createElement("div", { "class":`flags scrollbar`,});
   const flagCard/*Ωignore_positionΩ*/ = (f: Flag, compactCard: boolean = false)/*Ωignore_startΩ*/: ReturnType<import('svelte').Snippet>/*Ωignore_endΩ*/ => { async ()/*Ωignore_positionΩ*/ => {
    const investigate = investigateIp(f);
     { svelteHTML.createElement("li", {  "class":`card`,});compactCard;
       { svelteHTML.createElement("div", { "class":`card-main`,});
         { svelteHTML.createElement("span", { "class":`type`,});TYPE_LABELS[f.type]; }
        if(f.confidence != null){
           { svelteHTML.createElement("span", {     "class":`confidence confidence-${confidenceTier(f.confidence)}`,"title":`How confident this specific flag is, based on how much history backs it and how far it deviates from normal -- not how confident mikroview is overall`,});
            f.confidence; 
           }
        }
        if(isFilterable(f)){
           { svelteHTML.createElement("button", {     "class":`target`,"onclick":() => filterToTarget(f),"title":`Filter the live view to ${f.target}`,});
            f.target;
           }
        }else{
           { svelteHTML.createElement("span", { "class":`target target-global`,});  }
        }
        if(investigate){
          
           { const $$_nottuBetagitsevnIpI2C = __sveltets_2_ensureComponent(IpInvestigateButton); new $$_nottuBetagitsevnIpI2C({ target: __sveltets_2_any(), props: {  "ip":investigate,}});}
        }
        if(f.country){
           { svelteHTML.createElement("span", {   "class":`country`,"title":f.country,});countryFlag(f.country); }
        }
       }
      
       { svelteHTML.createElement("p", {   "class":`detail`,"title":compactCard ? f.detail : undefined,});f.detail; }
       { svelteHTML.createElement("div", { "class":`meta`,});
        if(!compactCard){
           { svelteHTML.createElement("span", {});  formatHM(f.firstSeen); }
        }
         { svelteHTML.createElement("span", {});  formatHM(f.lastSeen); }
         { svelteHTML.createElement("span", {}); f.count;  }
        if(hasExpandableDetail(f)){
           { svelteHTML.createElement("button", {   "class":`details-toggle`,"onclick":() => toggleExpanded(f.id),});
            expandedId === f.id ? 'Hide details' : 'Details';
           }
        }
       }
      if(expandedId === f.id){
         { svelteHTML.createElement("div", { "class":`expanded`,});
          if(f.evidence?.ports?.length){
             { svelteHTML.createElement("div", { "class":`ev-row`,});
               { svelteHTML.createElement("span", { "class":`ev-label`,});  }
               { svelteHTML.createElement("span", { "class":`ev-value`,});f.evidence.ports.join(', '); }
             }
          }
          if(f.evidence?.hosts?.length){
             { svelteHTML.createElement("div", { "class":`ev-row`,});
               { svelteHTML.createElement("span", { "class":`ev-label`,});  }
               { svelteHTML.createElement("span", { "class":`ev-value`,});f.evidence.hosts.join(', '); }
             }
          }
          if(f.evidence?.nat){
             { svelteHTML.createElement("div", { "class":`ev-row`,});
               { svelteHTML.createElement("span", { "class":`ev-label`,});  }
               { svelteHTML.createElement("span", { "class":`ev-value`,});
                f.evidence.nat.ip;f.evidence.nat.port ? `:${f.evidence.nat.port}` : '';
                if(f.evidence.nat.raw){ { svelteHTML.createElement("br", {});} { svelteHTML.createElement("span", { "class":`ev-raw`,});f.evidence.nat.raw; }}
               }
             }
          }
          if(f.reputation){
             { const $$_sliateDnoitatupeR2C = __sveltets_2_ensureComponent(ReputationDetails); new $$_sliateDnoitatupeR2C({ target: __sveltets_2_any(), props: {  "result":f.reputation,}});}
          }
         }
      }
       { svelteHTML.createElement("div", { "class":`actions`,});
        if(isAdminOrOpen){
          
           { svelteHTML.createElement("div", {  "class":`split-clear`,});openClearMenuFor === f.id;
             { svelteHTML.createElement("button", {   "class":`clear split-main`,"onclick":() => clear(f.id),});  }
             { svelteHTML.createElement("button", {           "class":`clear split-arrow`,"aria-haspopup":`true`,"aria-expanded":openClearMenuFor === f.id,"aria-label":`More clear options for this flag`,"onclick":() => toggleClearMenu(f.id),});
              
             }
            if(openClearMenuFor === f.id){
               { svelteHTML.createElement("div", {   "class":`split-menu`,"role":`menu`,});
                 { svelteHTML.createElement("button", {         "class":`split-menu-item`,"role":`menuitem`,"title":`Clear this flag and permanently stop ${TYPE_LABELS[f.type]} from ever raising again for ${f.target} -- reversible from the Exclusions page (see the menu).`,"onclick":() => {
                    openClearMenuFor = null
                    clearPermanent(f.id)
                  },});
                   
                 }
               }
            }
           }
        }else{
           { svelteHTML.createElement("button", {   "class":`clear`,"onclick":() => clear(f.id),});  }
        }
       }
     }
  };return __sveltets_2_any(0)}; { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {      "title":`Active flags by type`,"rows":typeBreakdown,"emptyMessage":`Nothing flagged right now.`,}});}

  

   { svelteHTML.createElement("section", { "aria-labelledby":`active-heading`,});
     { svelteHTML.createElement("div", { "class":`active-header`,});
       { svelteHTML.createElement("h2", { "id":`active-heading`,}); active.length;  }
       { svelteHTML.createElement("div", { "class":`header-controls`,});
        
         { svelteHTML.createElement("div", {     "class":`layout-select`,"role":`radiogroup`,"aria-label":`Card layout columns`,});
             for(let n of __sveltets_2_ensureArray(([1, 2, 3] as const))){n;
             { svelteHTML.createElement("button", {            "class":`layout-option`,"role":`radio`,"aria-checked":flagLayoutState.columns === n,"onclick":() => flagLayoutState.set(n),"title":`${n} column${n > 1 ? 's' : ''}`,});flagLayoutState.columns === n;
              n;
             }
          }
         }
        if(active.length > 0){
          
           { svelteHTML.createElement("button", {              "class":`clear-all`,"disabled":clearAllBusy,"onclick":onClearAllClick,"onblur":disarmClearAll,"onpointerleave":disarmClearAll,"title":clearAllArmed
              ? 'Click again to clear every active flag'
              : 'Clear every active flag -- regular clears only, click again to confirm',});clearAllArmed;
            clearAllArmed ? 'Confirm' : 'Clear all';
           }
        }
       }
     }
    if(active.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});    }
    }else{
       { svelteHTML.createElement("ul", {   "class":`list card-grid`,"style":`--flag-columns: ${effectiveColumns}`,});
           for(let item of __sveltets_2_ensureArray(activeItems)){item.kind === 'group' ? `group:${item.sourceIp}` : item.flag.id;
          if(item.kind === 'single'){
            ;__sveltets_2_ensureSnippet(flagCard(item.flag, compact));
          }else{
             { svelteHTML.createElement("li", { "class":`card campaign-card`,});
               { svelteHTML.createElement("div", { "class":`campaign-header`,});
                 { svelteHTML.createElement("button", {       "class":`campaign-toggle`,"onclick":() => toggleGroup(item.sourceIp),"aria-expanded":expandedGroup === item.sourceIp,});
                   { svelteHTML.createElement("span", { "class":`campaign-caret`,});expandedGroup === item.sourceIp ? '▾' : '▸'; }
                   { svelteHTML.createElement("span", { "class":`campaign-count`,});item.flags.length;      }
                 }
                 { svelteHTML.createElement("button", {       "class":`target campaign-source`,"onclick":() => filterToSource(item.sourceIp),"title":`Filter the live view to ${item.sourceIp}`,});
                  item.sourceIp;
                 }
               }
               { svelteHTML.createElement("div", { "class":`campaign-summary`,});
                 { svelteHTML.createElement("span", { "class":`campaign-types`,});groupTypeLabels(item.flags); }
                 { svelteHTML.createElement("span", {});  formatHM(groupFirstSeen(item.flags)); }
                 { svelteHTML.createElement("span", {});  formatHM(groupLastSeen(item.flags)); }
               }
              if(expandedGroup === item.sourceIp){
                 { svelteHTML.createElement("ul", { "class":`list campaign-members`,});
                     for(let f of __sveltets_2_ensureArray(item.flags)){f.id;
                    ;__sveltets_2_ensureSnippet(flagCard(f, compact));
                  }
                 }
              }
             }
          }
        }
       }
    }
   }

   { svelteHTML.createElement("section", { "aria-labelledby":`cleared-heading`,});
     { svelteHTML.createElement("h2", { "id":`cleared-heading`,});  }
    if(cleared.length === 0){
       { svelteHTML.createElement("p", { "class":`empty`,});    }
    }else{
      
       { svelteHTML.createElement("ul", {   "class":`list card-grid`,"style":`--flag-columns: ${effectiveColumns}`,});
           for(let f of __sveltets_2_ensureArray(cleared)){f.id;
           { svelteHTML.createElement("li", {  "class":`card cleared-card`,});compact;
             { svelteHTML.createElement("div", { "class":`card-main`,});
               { svelteHTML.createElement("span", { "class":`type`,});TYPE_LABELS[f.type]; }
               { svelteHTML.createElement("span", { "class":`target`,});f.target === 'global' ? 'network-wide' : f.target; }
             }
             { svelteHTML.createElement("p", { "class":`detail`,});f.detail; }
             { svelteHTML.createElement("div", { "class":`meta`,});
               { svelteHTML.createElement("span", {}); f.clearedAt ? formatHM(f.clearedAt) : ''; }
             }
           }
        }
       }
    }
   }

  if(isAdminOrOpen){
    
     { svelteHTML.createElement("p", { "class":`exclusions-pointer`,});
             
       { svelteHTML.createElement("button", {   "class":`link`,"onclick":() => (appState.view = 'exclusions'),});  }
          
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Flags__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Flags__SvelteComponent_ = ReturnType<typeof Flags__SvelteComponent_>;
/*Ωignore_endΩ*/export default Flags__SvelteComponent_;