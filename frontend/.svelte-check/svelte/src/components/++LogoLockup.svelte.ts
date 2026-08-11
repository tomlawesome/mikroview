///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import LogoMark from './LogoMark.svelte';

;type $$ComponentProps =  { size?: number };function $$render() {

  
  
  let { size = 22 }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()
;
async () => {

 { svelteHTML.createElement("span", {   "class":`lockup`,"style":`--logo-size: ${size}px`,});
   { const $$_kraMogoL1C = __sveltets_2_ensureComponent(LogoMark); new $$_kraMogoL1C({ target: __sveltets_2_any(), props: {  "size":size,}});}
   { svelteHTML.createElement("span", { "class":`wordmark`,});  { svelteHTML.createElement("span", { "class":`accent`,});  } }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const LogoLockup__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type LogoLockup__SvelteComponent_ = ReturnType<typeof LogoLockup__SvelteComponent_>;
/*Ωignore_endΩ*/export default LogoLockup__SvelteComponent_;