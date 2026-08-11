///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import type { FirewallEvent } from '../lib/types'
import { countryFlag, formatAddr, formatTime, isPublicIp, rawTooltip } from '../lib/format'
import { appState } from '../lib/state.svelte'
import ActionBadge from './ActionBadge.svelte'
import IpInvestigateButton from './IpInvestigateButton.svelte'
import PortInvestigateButton from './PortInvestigateButton.svelte'
import RouterRuleButton from './RouterRuleButton.svelte'
import { lookupPort } from '../lib/commonPorts';

;type $$ComponentProps =  { event: FirewallEvent; deviceName: string };function $$render() {

  
  
  
  
  
  
  
  
  

  let { event, deviceName }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  const ifaces = $derived(
    [event.inInterface, event.outInterface].filter(Boolean).join(' → ') || '—',
  )

  const srcFlag = $derived(countryFlag(event.srcCountry))
  const dstFlag = $derived(countryFlag(event.dstCountry))
;
async () => {

 { svelteHTML.createElement("div", {   "class":`row row-${event.action}`,"title":rawTooltip(event.raw, event.rawTruncated),});
   { svelteHTML.createElement("span", { "class":`cell time`,});formatTime(event.time); }

   { svelteHTML.createElement("button", {       "class":`cell device cell-btn`,"onclick":() => appState.setFilter('device', event.deviceId),"title":`Filter to device: ${deviceName}`,});
    deviceName;
   }

   { svelteHTML.createElement("button", {       "class":`cell action cell-btn`,"onclick":() => appState.setFilter('action', event.action),"title":`Filter to action: ${event.action}`,});
     { const $$_egdaBnoitcA2C = __sveltets_2_ensureComponent(ActionBadge); new $$_egdaBnoitcA2C({ target: __sveltets_2_any(), props: {  "action":event.action,}});}
   }

  if(event.chain){
     { svelteHTML.createElement("button", {       "class":`cell chain cell-btn`,"onclick":() => appState.setFilter('chain', event.chain),"title":`Filter to chain: ${event.chain}`,});
      event.chain;
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell chain`,});  }
  }

  if(event.srcIp){
     { svelteHTML.createElement("span", { "class":`cell addr`,});
       { svelteHTML.createElement("button", {       "class":`cell-btn addr-btn`,"title":event.srcHostName ? `${event.srcHostName} — filter to IP: ${event.srcIp}` : `Filter to IP: ${event.srcIp}`,"onclick":() => appState.setFilter('ip', event.srcIp ?? ''),});
        srcFlag ? `${srcFlag} ` : '';event.srcHostName || event.srcIp;
       }
      if(isPublicIp(event.srcIp)){
         { const $$_nottuBetagitsevnIpI2C = __sveltets_2_ensureComponent(IpInvestigateButton); new $$_nottuBetagitsevnIpI2C({ target: __sveltets_2_any(), props: {  "ip":event.srcIp,}});}
      }
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell addr`,});  }
  }

  if(event.srcPort){
     { svelteHTML.createElement("span", { "class":`cell port`,});
       { svelteHTML.createElement("button", {       "class":`cell-btn port-btn`,"title":event.srcPortName
          ? `${event.srcPortName} — filter to port: ${event.srcPort}`
          : `Filter to port: ${event.srcPort}`,"onclick":() => appState.setFilter('port', String(event.srcPort)),});
        event.srcPortName || event.srcPort;
       }
      if(lookupPort(event.srcPort)){
         { const $$_nottuBetagitsevnItroP2C = __sveltets_2_ensureComponent(PortInvestigateButton); new $$_nottuBetagitsevnItroP2C({ target: __sveltets_2_any(), props: {  "port":event.srcPort,}});}
      }
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell port`,});  }
  }

  if(event.dstIp){
     { svelteHTML.createElement("span", { "class":`cell addr`,});
       { svelteHTML.createElement("button", {       "class":`cell-btn addr-btn`,"title":event.dstHostName ? `${event.dstHostName} — filter to IP: ${event.dstIp}` : `Filter to IP: ${event.dstIp}`,"onclick":() => appState.setFilter('ip', event.dstIp ?? ''),});
        dstFlag ? `${dstFlag} ` : '';event.dstHostName || event.dstIp;
       }
      if(isPublicIp(event.dstIp)){
         { const $$_nottuBetagitsevnIpI2C = __sveltets_2_ensureComponent(IpInvestigateButton); new $$_nottuBetagitsevnIpI2C({ target: __sveltets_2_any(), props: {  "ip":event.dstIp,}});}
      }
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell addr`,});  }
  }

  if(event.dstPort){
     { svelteHTML.createElement("span", { "class":`cell port`,});
       { svelteHTML.createElement("button", {       "class":`cell-btn port-btn`,"title":event.dstPortName
          ? `${event.dstPortName} — filter to port: ${event.dstPort}`
          : `Filter to port: ${event.dstPort}`,"onclick":() => appState.setFilter('port', String(event.dstPort)),});
        event.dstPortName || event.dstPort;
       }
      if(lookupPort(event.dstPort)){
         { const $$_nottuBetagitsevnItroP2C = __sveltets_2_ensureComponent(PortInvestigateButton); new $$_nottuBetagitsevnItroP2C({ target: __sveltets_2_any(), props: {  "port":event.dstPort,}});}
      }
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell port`,});  }
  }

   { svelteHTML.createElement("span", {    "class":`cell addr nat`,"title":event.natRaw,});!!event.natIp;
    if(event.natIp){
       { svelteHTML.createElement("span", { "class":`nat-value`,}); formatAddr(event.natIp, event.natPort); }
       { const $$_nottuBeluRretuoR2C = __sveltets_2_ensureComponent(RouterRuleButton); new $$_nottuBeluRretuoR2C({ target: __sveltets_2_any(), props: {    "mode":`nat`,"device":event.deviceId,}});}
    }else{ }
   }

  if(event.protocol){
     { svelteHTML.createElement("button", {       "class":`cell proto cell-btn`,"onclick":() => appState.setFilter('protocol', event.protocol ?? ''),"title":`Filter to protocol: ${event.protocol}`,});
      event.protocol;
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell proto`,});  }
  }

   { svelteHTML.createElement("span", { "class":`cell iface`,});ifaces; }

  if(event.ruleLabel){
     { svelteHTML.createElement("span", { "class":`cell rule`,});
       { svelteHTML.createElement("button", {       "class":`cell-btn rule-btn`,"onclick":() => (appState.filters = { ...appState.filters, rule: event.ruleLabel, ruleRegex: false }),"title":event.ruleName ? `${event.ruleName} — filter to rule: ${event.ruleLabel}` : `Filter to rule: ${event.ruleLabel}`,});
        event.ruleName || event.ruleLabel;
       }
       { const $$_nottuBeluRretuoR2C = __sveltets_2_ensureComponent(RouterRuleButton); new $$_nottuBeluRretuoR2C({ target: __sveltets_2_any(), props: {      "mode":`rule`,"device":event.deviceId,"ruleLabel":event.ruleLabel,}});}
     }
  }else{
     { svelteHTML.createElement("span", { "class":`cell rule`,});  }
  }
 }


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const EventRow__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type EventRow__SvelteComponent_ = ReturnType<typeof EventRow__SvelteComponent_>;
/*Ωignore_endΩ*/export default EventRow__SvelteComponent_;