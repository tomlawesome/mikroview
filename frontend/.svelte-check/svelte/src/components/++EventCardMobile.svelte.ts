///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// One event, mobile card layout (issue #85) -- the desktop LiveTable's
// 12-column grid doesn't survive a phone-width squeeze without either
// tiny unreadable text or horizontal scrolling per row, so below the
// breakpoint LiveTable.svelte renders these instead. Design locked in
// from a mockup review (see issue #85's comments): primary triage
// info up front (time/action/NAT/rule, then the actual src->dst flow),
// secondary detail (device/proto/interfaces) small and dim, everything
// else pushed into EventDetailSheet.svelte on tap rather than shown
// inline or expanded in place.

import type { FirewallEvent } from '../lib/types'
import { formatTime, countryFlag, rawTooltip } from '../lib/format';

;type $$ComponentProps =  { event: FirewallEvent; deviceName: string; onOpen: () => void };function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  

  let { event, deviceName, onOpen }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ =
    $props()

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const ifaces = $derived([event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—')
;
async () => {

 { svelteHTML.createElement("button", {     "class":`card row-${event.action}`,"onclick":onOpen,"title":rawTooltip(event.raw, event.rawTruncated),});
   { svelteHTML.createElement("div", { "class":`line1`,});
     { svelteHTML.createElement("span", { "class":`time`,});formatTime(event.time); }
     { svelteHTML.createElement("span", { "class":`badge badge-${event.action}`,});event.action.toUpperCase(); }
    if(event.natIp){
       { svelteHTML.createElement("span", { "class":`badge badge-nat`,});  }
    }
    if(event.ruleName || event.ruleLabel){
       { svelteHTML.createElement("span", { "class":`rule`,});event.ruleName || event.ruleLabel; }
    }
   }
   { svelteHTML.createElement("div", { "class":`line2`,});
     { svelteHTML.createElement("span", { "class":`addr`,});
      if(event.srcIp){srcFlag ? `${srcFlag} ` : '';event.srcHostName || event.srcIp;if(event.srcPort){ { svelteHTML.createElement("span", { "class":`port`,}); event.srcPortName || event.srcPort; }}}else{ }
     }
     { svelteHTML.createElement("span", { "class":`arrow`,});  }
     { svelteHTML.createElement("span", { "class":`addr dst`,});
      if(event.dstIp){event.dstHostName || event.dstIp;if(event.dstPort){ { svelteHTML.createElement("span", { "class":`port`,}); event.dstPortName || event.dstPort; }}}else{ }
     }
   }
   { svelteHTML.createElement("div", { "class":`line3`,});
     { svelteHTML.createElement("span", {});deviceName; }
    if(event.protocol){ { svelteHTML.createElement("span", {});event.protocol; }}
     { svelteHTML.createElement("span", {});ifaces; }
   }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const EventCardMobile__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type EventCardMobile__SvelteComponent_ = ReturnType<typeof EventCardMobile__SvelteComponent_>;
/*Ωignore_endΩ*/export default EventCardMobile__SvelteComponent_;