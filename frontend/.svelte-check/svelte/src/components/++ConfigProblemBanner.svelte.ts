///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
//
// Tells an admin that mikroview is running with a value different from
// the one they configured.
//
// Deliberately dismissable only for the current page view, not
// persisted: the condition is still true after a reload, and a
// permanently dismissable warning is a permanently dismissed one. The
// measured clickthrough on browser security warnings is around 70%,
// so a banner people can make go away forever is a banner that does
// nothing.

import { configProblemsState } from '../lib/configProblems.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  

  configProblemsState.ensureLoaded()
;
async () => {

if(configProblemsState.hasProblems && !configProblemsState.dismissed){
   { svelteHTML.createElement("div", {   "class":`banner`,"role":`status`,});
     { svelteHTML.createElement("div", { "class":`content`,});
       { svelteHTML.createElement("strong", {});
        configProblemsState.problems.length === 1
          ? 'A setting in your configuration is being ignored'
          : `${configProblemsState.problems.length} settings in your configuration are being ignored`;
       }
       { svelteHTML.createElement("ul", {});
           for(let p of __sveltets_2_ensureArray(configProblemsState.problems)){p.code + p.key;
           { svelteHTML.createElement("li", {});
             { svelteHTML.createElement("code", {});p.key; }  p.message;
            if(p.applied){
               { svelteHTML.createElement("span", { "class":`applied`,});  { svelteHTML.createElement("code", {});p.applied; }  }
            }
            if(p.remediation){
               { svelteHTML.createElement("span", { "class":`fix`,});p.remediation; }
            }
           }
        }
       }
     }
     { svelteHTML.createElement("button", {           "type":`button`,"class":`dismiss`,"onclick":() => (configProblemsState.dismissed = true),"aria-label":`Hide until reload`,"title":`Hide until reload -- the problem is still there`,});
      
     }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: "", slots: {}, events: {} }}
const ConfigProblemBanner__SvelteComponent_ = __sveltets_2_isomorphic_component(__sveltets_2_with_any_event($$render()));
/*Ωignore_startΩ*/type ConfigProblemBanner__SvelteComponent_ = InstanceType<typeof ConfigProblemBanner__SvelteComponent_>;
/*Ωignore_endΩ*/export default ConfigProblemBanner__SvelteComponent_;