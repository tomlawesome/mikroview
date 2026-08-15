///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// No wrapping element -- each widget card (and the trailing add-trigger)
// becomes its own sibling in Dashboard.svelte's grid, the same as the
// fixed BarList panels around it.

import { topTalkerWidgetsState } from '../lib/topTalkers.svelte'
import CustomTopTalkerCard from './CustomTopTalkerCard.svelte'
import AddTopTalkerWidget from './AddTopTalkerWidget.svelte';
function $$render() {

  
  
  
  
  
  
  
;
async () => {

   for(let widget of __sveltets_2_ensureArray(topTalkerWidgetsState.widgets)){widget.id;
   { const $$_draCreklaTpoTmotsuC0C = __sveltets_2_ensureComponent(CustomTopTalkerCard); new $$_draCreklaTpoTmotsuC0C({ target: __sveltets_2_any(), props: { widget,}});}
}
 { const $$_tegdiWreklaTpoTddA0C = __sveltets_2_ensureComponent(AddTopTalkerWidget); new $$_tegdiWreklaTpoTddA0C({ target: __sveltets_2_any(), props: {}});}
};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const CustomTopTalkers__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type CustomTopTalkers__SvelteComponent_ = InstanceType<typeof CustomTopTalkers__SvelteComponent_>;
/*Ωignore_endΩ*/export default CustomTopTalkers__SvelteComponent_;