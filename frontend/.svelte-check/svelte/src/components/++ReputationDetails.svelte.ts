///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// The reputation-info rows shared by IpLookupPopover (a live, on-demand
// lookup) and Flags.svelte's expanded detail (a snapshot captured at
// raise time) -- same ReputationResult shape either way, so the
// rendering only needs to live in one place.

import type { ReputationResult } from '../lib/types';

;type $$ComponentProps =  { result: ReputationResult };function $$render() {

  
  
  
  
  
  

  let { result }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  const hasIntel = $derived(
    result.abuseScore != null ||
      result.totalReports != null ||
      !!result.isp ||
      !!result.countryCode ||
      !!result.usageType ||
      !!result.isTor ||
      !!result.netClass ||
      !!result.ports?.length ||
      !!result.hostnames?.length ||
      !!result.vulns?.length,
  )

  // A human category label for the network-class row. The category comes
  // from a fixed enum server-side, so this map is exhaustive; an
  // unrecognised value falls back to the raw string rather than blanking.
  const categoryLabels: Record<string, string> = {
    tor: 'Tor exit',
    vpn: 'VPN',
    datacenter: 'Datacenter',
    'privacy-relay': 'Privacy relay',
  }
;
async () => {

if(!hasIntel){
   { svelteHTML.createElement("div", { "class":`status`,});      }
}else{
   { svelteHTML.createElement("div", { "class":`rows`,});
    if(result.abuseScore != null){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", {  "class":`value`,});result.abuseScore >= 50;result.abuseScore;  }
       }
    }
    if(result.totalReports != null){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.totalReports; }
       }
    }
    if(result.isp){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.isp; }
       }
    }
    if(result.countryCode){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.countryCode; }
       }
    }
    if(result.usageType){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.usageType; }
       }
    }
    if(result.netClass){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", {  "class":`value netclass`,});result.netClass.category === 'tor' || result.netClass.category === 'vpn';
          categoryLabels[result.netClass.category] ?? result.netClass.category;
          if(result.netClass.detail){
             { svelteHTML.createElement("span", { "class":`netclass-detail`,}); result.netClass.label; result.netClass.detail; }
          }else{
             { svelteHTML.createElement("span", { "class":`netclass-detail`,}); result.netClass.label; }
          }
         }
       }
    }
    if(result.isTor){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value high`,});  }
       }
    }
    if(result.ports?.length){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.ports.join(', '); }
       }
    }
    if(result.hostnames?.length){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.hostnames.join(', '); }
       }
    }
    if(result.vulns?.length){
       { svelteHTML.createElement("div", { "class":`row`,});
         { svelteHTML.createElement("span", { "class":`label`,});  }
         { svelteHTML.createElement("span", { "class":`value`,});result.vulns.join(', '); }
       }
    }
   }
}


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const ReputationDetails__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type ReputationDetails__SvelteComponent_ = ReturnType<typeof ReputationDetails__SvelteComponent_>;
/*Ωignore_endΩ*/export default ReputationDetails__SvelteComponent_;