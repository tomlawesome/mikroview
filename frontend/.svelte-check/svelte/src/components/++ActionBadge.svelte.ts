///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import type { Action } from '../lib/types';

;type $$ComponentProps =  { action: Action };function $$render() {

  
  

  let { action }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  const labels: Record<Action, string> = {
    accept: 'ACCEPT',
    drop: 'DROP',
    reject: 'REJECT',
    log: 'LOG',
    unknown: '?',
  }
;
async () => {

 { svelteHTML.createElement("span", { "class":`badge badge-${action}`,});labels[action] ?? action; }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const ActionBadge__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type ActionBadge__SvelteComponent_ = ReturnType<typeof ActionBadge__SvelteComponent_>;
/*Ωignore_endΩ*/export default ActionBadge__SvelteComponent_;