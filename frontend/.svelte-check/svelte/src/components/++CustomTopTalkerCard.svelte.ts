///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// One user-defined top-talkers widget: its own independent filter (not
// the live view's current FilterBar state -- see appState.filteredBy)
// grouped by whichever dimension the widget was saved with.

import { appState } from '../lib/state.svelte'
import { topNBy } from '../lib/topN'
import { groupByKey, GROUP_BY_LABELS } from '../lib/groupBy'
import { topTalkerWidgetsState, type TopTalkerWidget } from '../lib/topTalkers.svelte'
import BarList from './BarList.svelte';

;type $$ComponentProps =  { widget: TopTalkerWidget };function $$render() {

  
  
  
  
  
  
  
  
  

  let { widget }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  const TOP_N = 10

  const rows = $derived(
    topNBy(appState.filteredBy(widget.filters), (e) => groupByKey(widget.groupBy, e), TOP_N),
  )
;
async () => {

 { svelteHTML.createElement("div", { "class":`widget`,});
   { svelteHTML.createElement("button", {         "class":`remove`,"onclick":() => topTalkerWidgetsState.remove(widget.id),"title":`Remove this widget`,"aria-label":`Remove ${widget.title}`,});
    
   }
   { const $$_tsiLraB1C = __sveltets_2_ensureComponent(BarList); new $$_tsiLraB1C({ target: __sveltets_2_any(), props: {      "title":`${widget.title} — by ${GROUP_BY_LABELS[widget.groupBy]}`,rows,"emptyMessage":`No matching events yet`,}});}
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const CustomTopTalkerCard__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type CustomTopTalkerCard__SvelteComponent_ = ReturnType<typeof CustomTopTalkerCard__SvelteComponent_>;
/*Ωignore_endΩ*/export default CustomTopTalkerCard__SvelteComponent_;