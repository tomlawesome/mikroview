///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Full-detail bottom sheet for one event, opened by tapping a card in
// EventCardMobile.svelte -- chosen over expanding the card in place
// (see issue #85's locked design decision) since it scales better if
// more detail (e.g. the reputation/evidence data desktop's Flags view
// already shows) needs to live here later, without redesigning the
// card again.

import { appState } from '../lib/state.svelte'
import { formatTime, formatAddr, countryFlag, isPublicIp } from '../lib/format'
import { lookupPort } from '../lib/commonPorts'
import type { FirewallEvent } from '../lib/types'
import IpInvestigateButton from './IpInvestigateButton.svelte'
import PortInvestigateButton from './PortInvestigateButton.svelte';

;type $$ComponentProps =  { event: FirewallEvent; deviceName: string; onClose: () => void };function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  

  let { event, deviceName, onClose }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ =
    $props()

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const dstFlag = $derived(countryFlag(event.dstCountry))
  const ifaces = $derived([event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—')

  // Same click-to-filter convention every other cell in the app uses
  // (see EventRow.svelte, Flags.svelte's target button) -- closes the
  // sheet too, since the live view behind it is about to change under
  // it anyway.
  function filterAndClose<K extends keyof typeof appState.filters>(key: K, value: (typeof appState.filters)[K]) {
    appState.setFilter(key, value)
    onClose()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onClose()
  }
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

 { svelteHTML.createElement("div", {     "class":`scrim`,"onclick":onClose,"role":`presentation`,}); }
 { svelteHTML.createElement("div", {       "class":`sheet`,"role":`dialog`,"aria-modal":`true`,"aria-label":`Event detail`,});
   { svelteHTML.createElement("div", { "class":`handle`,}); }

   { svelteHTML.createElement("div", { "class":`header`,});
     { svelteHTML.createElement("span", { "class":`badge badge-${event.action}`,});event.action.toUpperCase(); }
     { svelteHTML.createElement("span", { "class":`time`,});formatTime(event.time); }
     { svelteHTML.createElement("button", {     "class":`close`,"onclick":onClose,"aria-label":`Close`,});  }
   }

   { svelteHTML.createElement("p", { "class":`title`,});
    srcFlag ? `${srcFlag} ` : '';formatAddr(event.srcIp, event.srcPort);
     { svelteHTML.createElement("span", { "class":`arrow`,});  }
    dstFlag ? `${dstFlag} ` : '';formatAddr(event.dstIp, event.dstPort);
   }

   { svelteHTML.createElement("div", { "class":`rows`,});
     { svelteHTML.createElement("div", { "class":`row`,});
       { svelteHTML.createElement("span", { "class":`k`,});  }
       { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('device', event.deviceId),});deviceName; }
     }
    if(event.chain){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('chain', event.chain),});event.chain; }
       }
    }
    if(event.srcIp){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("span", { "class":`v-group`,});
           { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('ip', event.srcIp ?? ''),});
            event.srcHostName || event.srcIp;
           }
          if(isPublicIp(event.srcIp)){ { const $$_nottuBetagitsevnIpI4C = __sveltets_2_ensureComponent(IpInvestigateButton); new $$_nottuBetagitsevnIpI4C({ target: __sveltets_2_any(), props: {  "ip":event.srcIp,}});}}
         }
       }
    }
    if(event.srcPort){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("span", { "class":`v-group`,});
           { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('port', String(event.srcPort)),});
            event.srcPortName || event.srcPort;
           }
          if(lookupPort(event.srcPort)){ { const $$_nottuBetagitsevnItroP4C = __sveltets_2_ensureComponent(PortInvestigateButton); new $$_nottuBetagitsevnItroP4C({ target: __sveltets_2_any(), props: {  "port":event.srcPort,}});}}
         }
       }
    }
    if(event.dstIp){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("span", { "class":`v-group`,});
           { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('ip', event.dstIp ?? ''),});
            event.dstHostName || event.dstIp;
           }
          if(isPublicIp(event.dstIp)){ { const $$_nottuBetagitsevnIpI4C = __sveltets_2_ensureComponent(IpInvestigateButton); new $$_nottuBetagitsevnIpI4C({ target: __sveltets_2_any(), props: {  "ip":event.dstIp,}});}}
         }
       }
    }
    if(event.dstPort){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("span", { "class":`v-group`,});
           { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('port', String(event.dstPort)),});
            event.dstPortName || event.dstPort;
           }
          if(lookupPort(event.dstPort)){ { const $$_nottuBetagitsevnItroP4C = __sveltets_2_ensureComponent(PortInvestigateButton); new $$_nottuBetagitsevnItroP4C({ target: __sveltets_2_any(), props: {  "port":event.dstPort,}});}}
         }
       }
    }
    if(event.natIp){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("span", { "class":`v accent`,}); formatAddr(event.natIp, event.natPort); }
       }
    }
    if(event.protocol){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("button", {   "class":`v link`,"onclick":() => filterAndClose('protocol', event.protocol ?? ''),});event.protocol; }
       }
    }
     { svelteHTML.createElement("div", { "class":`row`,});
       { svelteHTML.createElement("span", { "class":`k`,});  }
       { svelteHTML.createElement("span", { "class":`v`,});ifaces; }
     }
    if(event.ruleLabel){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`k`,});  }
         { svelteHTML.createElement("button", {     "class":`v link`,"onclick":() => {
            appState.filters = { ...appState.filters, rule: event.ruleLabel, ruleRegex: false }
            onClose()
          },});
          event.ruleName || event.ruleLabel;
         }
       }
    }
   }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const EventDetailSheet__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type EventDetailSheet__SvelteComponent_ = ReturnType<typeof EventDetailSheet__SvelteComponent_>;
/*Ωignore_endΩ*/export default EventDetailSheet__SvelteComponent_;