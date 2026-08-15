///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
//
// This overlay exists to satisfy the AGPL, not as a nicety.
//
// Section 0 defines "Appropriate Legal Notices" as a notice displaying
// (a) an appropriate copyright notice, (b) that there is no warranty,
// (c) that licensees may convey the work under the License, and (d)
// how to view a copy of the License. Section 5(d) requires an
// interactive interface to display them, and section 13 requires that
// anyone interacting over a network is offered the Corresponding
// Source.
//
// All four notices plus the source offer live here. Removing or
// emptying this component puts the project out of compliance -- if the
// UI is restructured, the notices move, they don't disappear.

import { versionState } from '../lib/version.svelte';

;type $$ComponentProps =  { open?: boolean };function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  let { open = $bindable(false) }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()/*Ωignore_startΩ*/;open;/*Ωignore_endΩ*/

  const SOURCE_URL = 'https://github.com/tomlawesome/mikroview'
  const LICENSE_URL = 'https://www.gnu.org/licenses/agpl-3.0.html'

  function close() {
    open = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') close()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) close()
  }
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(open){
   { svelteHTML.createElement("div", {     "class":`backdrop`,"onclick":onBackdropClick,"role":`presentation`,});
     { svelteHTML.createElement("div", {         "class":`modal`,"role":`dialog`,"aria-modal":`true`,"aria-label":`About MikroView`,"tabindex":-1,});
       { svelteHTML.createElement("div", { "class":`modal-header`,});
         { svelteHTML.createElement("span", { "class":`title`,});  }
         { svelteHTML.createElement("button", {       "type":`button`,"class":`close`,"onclick":close,"aria-label":`Close`,});  }
       }

       { svelteHTML.createElement("div", { "class":`body`,});
        if(versionState.version){
           { svelteHTML.createElement("p", { "class":`version`,}); versionState.version; }
        }

        
         { svelteHTML.createElement("p", {});     }

        
         { svelteHTML.createElement("p", {});
                    
              
           { svelteHTML.createElement("a", {     "href":LICENSE_URL,"target":`_blank`,"rel":`noopener noreferrer`,});
                  
           }
         }

        
         { svelteHTML.createElement("p", {});
                     
           { svelteHTML.createElement("strong", {});   }     
                   
             
         }

        
         { svelteHTML.createElement("p", {});
                   
           { svelteHTML.createElement("a", {     "href":SOURCE_URL,"target":`_blank`,"rel":`noopener noreferrer`,});
            SOURCE_URL;
           }
         }

         { svelteHTML.createElement("p", { "class":`commercial`,});
                      
                
           { svelteHTML.createElement("a", {     "href":`${SOURCE_URL}/blob/main/COMMERCIAL-LICENSE.md`,"target":`_blank`,"rel":`noopener noreferrer`,});
            
           }
         }

        
         { svelteHTML.createElement("p", { "class":`third-party`,});
                
              
           { svelteHTML.createElement("a", {     "href":`/api/third-party-notices`,"target":`_blank`,"rel":`noopener noreferrer`,});
             
           }
         }
       }
     }
   }
}


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings('open'), slots: {}, events: {} }}
const AboutOverlay__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type AboutOverlay__SvelteComponent_ = ReturnType<typeof AboutOverlay__SvelteComponent_>;
/*Ωignore_endΩ*/export default AboutOverlay__SvelteComponent_;