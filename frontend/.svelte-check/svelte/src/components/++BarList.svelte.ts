///<reference types="svelte" />
;;
  // SPDX-License-Identifier: AGPL-3.0-only
  // Generic horizontal bar-list panel, shared by every ranked-count chart
  // on the dashboard (top rules, action/protocol breakdown, top talkers,
  // per-device volume). Extracted from what was TopRulesMenu's row markup
  // so all five panels stay visually consistent instead of each hand-
  // rolling their own bar/track/count layout.
  interface Row {
    label: string
    count: number
    colorVar?: string
  };;

  interface Props {
    title: string
    rows: Row[]
    emptyMessage?: string
  };function $$render() {




  const { title, rows, emptyMessage = 'No data yet' }: Props = $props()

  const maxCount = $derived(rows[0]?.count ?? 0)
;
async () => {

 { svelteHTML.createElement("div", { "class":`bar-list`,});
   { svelteHTML.createElement("div", { "class":`header`,});title; }
  if(rows.length === 0){
     { svelteHTML.createElement("div", { "class":`empty`,});emptyMessage; }
  }else{
     { svelteHTML.createElement("div", { "class":`rows`,});
         for(let r of __sveltets_2_ensureArray(rows)){r.label;
         { svelteHTML.createElement("div", { "class":`row`,});
           { svelteHTML.createElement("span", { "class":`label`,});r.label; }
           { svelteHTML.createElement("span", { "class":`bar-track`,});
             { svelteHTML.createElement("span", {     "class":`bar`,"style":`width: ${maxCount ? (r.count / maxCount) * 100 : 0}%; background: ${r.colorVar ?? 'var(--accent)'}`,}); }
           }
           { svelteHTML.createElement("span", { "class":`count`,});r.count; }
         }
      }
     }
  }
 }


};
return { props: {} as any as Props, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const BarList__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type BarList__SvelteComponent_ = ReturnType<typeof BarList__SvelteComponent_>;
/*Ωignore_endΩ*/export default BarList__SvelteComponent_;