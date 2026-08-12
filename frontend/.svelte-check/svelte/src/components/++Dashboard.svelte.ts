///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Full-page dashboard tab: consolidates the charts that used to live in
// toolbar popovers (event volume, top rules) plus new ones an admin
// scanning traffic would want (action/protocol mix, top talkers,
// per-device volume), all on one screen instead of one-at-a-time
// popouts. Reads from the same reactive state the live view uses, so it
// stays in sync with whatever filters/retention are active there.

import { appState } from '../lib/state.svelte'
import type { Action } from '../lib/types'
import { topNBy } from '../lib/topN'
import EventsChart from './EventsChart.svelte'
import FlagsChart from './FlagsChart.svelte'
import BarList from './BarList.svelte'
import CustomTopTalkers from './CustomTopTalkers.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  

  const ACTION_LABELS: Record<Action, string> = {
    accept: 'Accept',
    drop: 'Drop',
    reject: 'Reject',
    log: 'Log',
    unknown: 'Unknown',
  }

  const TOP_N = 10

  const topRuleRows = $derived(
    (appState.stats?.topRules ?? []).map((r) => ({ label: r.rule, count: r.count })),
  )

  const actionRows = $derived(
    Object.entries(appState.stats?.byAction ?? {})
      .map(([action, count]) => ({
        label: ACTION_LABELS[action as Action],
        count: count ?? 0,
        colorVar: `var(--${action})`,
      }))
      .sort((a, b) => b.count - a.count),
  )

  const protocolRows = $derived(
    topNBy(appState.filteredEvents, (e) => e.protocol?.toUpperCase(), TOP_N),
  )

  const topTalkerRows = $derived(
    topNBy(appState.filteredEvents, (e) => e.srcIp, TOP_N),
  )

  const deviceRows = $derived(
    [...appState.devices]
      .sort((a, b) => b.eventCount - a.eventCount)
      .map((d) => ({ label: d.name, count: d.eventCount })),
  )
;
async () => {

 { svelteHTML.createElement("div", { "class":`dashboard scrollbar`,});
   { svelteHTML.createElement("div", { "class":`panel wide`,});
     { const $$_trahCstnevE2C = __sveltets_2_ensureComponent(EventsChart); new $$_trahCstnevE2C({ target: __sveltets_2_any(), props: {}});}
   }
   { svelteHTML.createElement("div", { "class":`panel wide`,});
     { const $$_trahCsgalF2C = __sveltets_2_ensureComponent(FlagsChart); new $$_trahCsgalF2C({ target: __sveltets_2_any(), props: {}});}
   }
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {      "title":`Top rules`,"rows":topRuleRows,"emptyMessage":`No labeled rules seen yet`,}});}
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {    "title":`Action breakdown`,"rows":actionRows,}});}
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {    "title":`Protocol breakdown`,"rows":protocolRows,}});}
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {    "title":`Top talkers (source IP)`,"rows":topTalkerRows,}});}
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {      "title":`Event volume by device`,"rows":deviceRows,"emptyMessage":`No devices seen yet`,}});}
   { const $$_sreklaTpoTmotsuC1C = __sveltets_2_ensureComponent(CustomTopTalkers); new $$_sreklaTpoTmotsuC1C({ target: __sveltets_2_any(), props: {}});}
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const Dashboard__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type Dashboard__SvelteComponent_ = ReturnType<typeof Dashboard__SvelteComponent_>;
/*Ωignore_endΩ*/export default Dashboard__SvelteComponent_;