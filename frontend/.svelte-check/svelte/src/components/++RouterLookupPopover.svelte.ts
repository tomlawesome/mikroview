///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Single instance mounted once at the app root (see App.svelte), same
// fixed-position-from-trigger-coordinates approach as IpLookupPopover /
// PortLookupPopover, driven by lib/routerLookup.svelte.ts's singleton.
//
// Renders the pushed rule/NAT data from mikroview's own store (issue
// #186 step 4). Three honest states beyond loading/error, and they are
// deliberately distinct: the device never pushed a table ("no data
// yet" -- with a pointer at what enables it), the table exists but no
// rule carries this prefix (prefix resolution is the operator's
// convention, see #186 step 4c), and one-or-more matches (a shared
// prefix legitimately resolves to several rules).

import { routerLookupState as st } from '../lib/routerLookup.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  

  const POPOVER_WIDTH = 320

  let popoverEl: HTMLDivElement | undefined = $state()

  function onDocClick(e: MouseEvent) {
    if (popoverEl && !popoverEl.contains(e.target as Node)) st.close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') st.close()
  }

  $effect(() => {
    if (!st.anchor) return
    // Deferred past the current click's bubble phase -- same reasoning
    // as IpLookupPopover.svelte.
    const timer = setTimeout(() => document.addEventListener('click', onDocClick))
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', onDocClick)
    }
  })

  const style = $derived.by(() => {
    const a = st.anchor
    if (!a) return ''
    const x = Math.min(a.x, window.innerWidth - POPOVER_WIDTH - 12)
    const y = Math.min(a.y + 6, window.innerHeight - 80)
    return `left: ${Math.max(8, x)}px; top: ${y}px`
  })

  const title = $derived(
    st.mode === 'rule' ? `Rules with log-prefix “${st.ruleLabel}”` : `NAT table — ${st.device}`,
  )
;
async () => {

 { svelteHTML.createElement("svelte:window", {  "onkeydown":onKeydown,});}

if(st.anchor){
   { const $$_div0 = svelteHTML.createElement("div", {        "class":`popover`,style,"role":`dialog`,"aria-label":title,});popoverEl = $$_div0;
     { svelteHTML.createElement("div", { "class":`header`,});
       { svelteHTML.createElement("span", { "class":`title`,});title; }
       { svelteHTML.createElement("button", {     "class":`close`,"onclick":() => st.close(),"aria-label":`Close`,});  }
     }

    if(st.loading){
       { svelteHTML.createElement("div", { "class":`status`,});  }
    } else if (st.error){
       { svelteHTML.createElement("div", { "class":`status error`,});st.error; }
    } else if (!st.available){
       { svelteHTML.createElement("div", { "class":`status`,});
         st.mode === 'rule' ? 'rule' : 'NAT';    st.device;    
               
       }
    } else if (st.mode === 'rule' && st.rules.length === 0){
       { svelteHTML.createElement("div", { "class":`status`,});
              st.tableSize;     st.ruleLabel;
                { svelteHTML.createElement("code", {});  }
       }
    } else if (st.mode === 'rule'){
       { svelteHTML.createElement("div", { "class":`entries`,});
           for(let r of __sveltets_2_ensureArray(st.rules)){r.ordinal;
           { svelteHTML.createElement("div", { "class":`entry`,});
             { svelteHTML.createElement("div", { "class":`entry-header`,});
               { svelteHTML.createElement("span", { "class":`ordinal`,}); r.ordinal; }
               { svelteHTML.createElement("span", { "class":`chain`,});r.chain; }
               { svelteHTML.createElement("span", { "class":`badge action-${r.action}`,});r.action; }
             }
            if(r.comment){
               { svelteHTML.createElement("div", { "class":`comment`,});r.comment; }
            }else{
               { svelteHTML.createElement("div", { "class":`comment dim`,});      }
            }
            if(r.srcAddressList){
               { svelteHTML.createElement("div", { "class":`detail`,}); r.srcAddressList; }
            }
           }
        }
       }
       { svelteHTML.createElement("div", { "class":`footnote`,});
                  st.rules[0].ordinal;  
       }
    }else{
       { svelteHTML.createElement("div", { "class":`entries nat`,});
           for(let r of __sveltets_2_ensureArray(st.natRules)){r.ordinal;
           { svelteHTML.createElement("div", { "class":`entry`,});
             { svelteHTML.createElement("div", { "class":`entry-header`,});
               { svelteHTML.createElement("span", { "class":`ordinal`,}); r.ordinal; }
               { svelteHTML.createElement("span", { "class":`chain`,});r.chain; }
               { svelteHTML.createElement("span", { "class":`badge`,});r.action; }
             }
            if(r.comment){
               { svelteHTML.createElement("div", { "class":`comment`,});r.comment; }
            }
           }
        }
       }
       { svelteHTML.createElement("div", { "class":`footnote`,});
                       
               
       }
    }
   }
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const RouterLookupPopover__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type RouterLookupPopover__SvelteComponent_ = ReturnType<typeof RouterLookupPopover__SvelteComponent_>;
/*Ωignore_endΩ*/export default RouterLookupPopover__SvelteComponent_;