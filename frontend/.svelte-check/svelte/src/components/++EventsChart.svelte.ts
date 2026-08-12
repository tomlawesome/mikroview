///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only
// Multi-line time-series chart of event volume by action, over the last
// hour at 1-minute resolution (see internal/store/ring.go's
// Stats.TimeSeries). Built by hand in SVG rather than pulling in a
// charting library, consistent with the rest of this app's lean stack.
//
// Colors deliberately reuse the app's existing fixed accept/drop/reject/
// log/unknown semantic colors (see app.css) rather than a fresh
// categorical palette, so a line here means the same thing as the same
// color everywhere else (ActionBadge, row tinting). Validating that set
// as a *chart* palette (dataviz skill's validator) flags low chroma on
// "unknown" and marginal light-mode contrast on a few slots -- the
// mitigation applied per the skill's own guidance is a persistent
// legend with text labels, so identity never depends on color alone.

import { appState } from '../lib/state.svelte'
import { formatHM } from '../lib/format'
import type { Action, TimeBucket } from '../lib/types';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

  const ACTION_ORDER: Action[] = ['accept', 'drop', 'reject', 'log', 'unknown']
  const ACTION_LABELS: Record<Action, string> = {
    accept: 'Accept',
    drop: 'Drop',
    reject: 'Reject',
    log: 'Log',
    unknown: 'Unknown',
  }

  const W = 460
  const H = 180
  const MARGIN = { top: 10, right: 10, bottom: 22, left: 34 }
  const plotW = W - MARGIN.left - MARGIN.right
  const plotH = H - MARGIN.top - MARGIN.bottom

  let view = $state<'chart' | 'table'>('chart')
  let hoverIndex = $state<number | null>(null)
  let svgEl: SVGSVGElement | undefined = $state()

  const points = $derived(appState.stats?.timeSeries ?? [])

  const seriesActions = $derived(
    ACTION_ORDER.filter((a) => points.some((p) => (p.byAction[a] ?? 0) > 0)),
  )

  const maxValue = $derived(
    niceCeil(points.reduce((m, p) => Math.max(m, ...seriesActions.map((a) => p.byAction[a] ?? 0)), 0)),
  )

  function niceCeil(v: number): number {
    if (v <= 0) return 1
    const pow = Math.pow(10, Math.floor(Math.log10(v)))
    for (const step of [1, 2, 5, 10]) {
      if (v <= step * pow) return step * pow
    }
    return 10 * pow
  }

  function x(i: number): number {
    if (points.length <= 1) return MARGIN.left
    return MARGIN.left + (i / (points.length - 1)) * plotW
  }

  function y(v: number): number {
    return MARGIN.top + plotH - (v / maxValue) * plotH
  }

  function pathFor(action: Action): string {
    return points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i).toFixed(1)},${y(p.byAction[action] ?? 0).toFixed(1)}`).join(' ')
  }

  const yTicks = $derived([0, maxValue / 2, maxValue])

  const xTickIndices = $derived(
    points.length === 0
      ? []
      : [0, Math.round((points.length - 1) / 2), points.length - 1].filter(
          (v, i, arr) => arr.indexOf(v) === i,
        ),
  )

  function onMove(e: PointerEvent) {
    if (!svgEl || points.length === 0) return
    const rect = svgEl.getBoundingClientRect()
    const relX = ((e.clientX - rect.left) / rect.width) * W
    const frac = (relX - MARGIN.left) / plotW
    const idx = Math.round(frac * (points.length - 1))
    hoverIndex = Math.min(points.length - 1, Math.max(0, idx))
  }

  function tableRows(): TimeBucket[] {
    return points
  }
;
async () => {

 { svelteHTML.createElement("div", { "class":`events-chart`,});
   { svelteHTML.createElement("div", { "class":`header`,});
     { svelteHTML.createElement("span", { "class":`title`,});     }
     { svelteHTML.createElement("button", {   "class":`toggle`,"onclick":() => (view = view === 'chart' ? 'table' : 'chart'),});
      view === 'chart' ? 'Table view' : 'Chart view';
     }
   }

  if(points.length === 0 || seriesActions.length === 0){
     { svelteHTML.createElement("div", { "class":`empty`,});   }
  } else if (view === 'chart'){
     { const $$_svg1 = svelteHTML.createElement("svg", {            "viewBox":`0 0 ${W} ${H}`,"role":`img`,"aria-label":`Event volume by action over the last hour`,"onpointermove":onMove,"onpointerleave":() => (hoverIndex = null),});svgEl = $$_svg1;
      
         for(let t of __sveltets_2_ensureArray(yTicks)){t;
         { svelteHTML.createElement("line", {          "x1":MARGIN.left,"x2":W - MARGIN.right,"y1":y(t),"y2":y(t),"class":`grid`,});}
         { svelteHTML.createElement("text", {          "x":MARGIN.left - 6,"y":y(t),"class":`axis-label`,"text-anchor":`end`,"dominant-baseline":`middle`,});Math.round(t); }
      }

         for(let i of __sveltets_2_ensureArray(xTickIndices)){i;
         { svelteHTML.createElement("text", {       "x":x(i),"y":H - 4,"class":`axis-label`,"text-anchor":`middle`,});formatHM(points[i].time); }
      }

         for(let action of __sveltets_2_ensureArray(seriesActions)){action;
         { svelteHTML.createElement("path", {      "d":pathFor(action),"class":`line line-${action}`,"fill":`none`,});}
         { svelteHTML.createElement("circle", {         "cx":x(points.length - 1),"cy":y(points[points.length - 1].byAction[action] ?? 0),"r":`4`,"class":`end-dot end-dot-${action}`,});}
      }

      if(hoverIndex !== null){
         { svelteHTML.createElement("line", {           "x1":x(hoverIndex),"x2":x(hoverIndex),"y1":MARGIN.top,"y2":MARGIN.top + plotH,"class":`crosshair`,});}
           for(let action of __sveltets_2_ensureArray(seriesActions)){action;
           { svelteHTML.createElement("circle", {         "cx":x(hoverIndex),"cy":y(points[hoverIndex].byAction[action] ?? 0),"r":`4`,"class":`end-dot end-dot-${action}`,});}
        }
      }
     }

    if(hoverIndex !== null){
       { svelteHTML.createElement("div", {     "class":`tooltip`,"style":`left: ${Math.min(78, Math.max(0, (x(hoverIndex) / W) * 100))}%`,});
         { svelteHTML.createElement("div", { "class":`tooltip-time`,});formatHM(points[hoverIndex].time); }
           for(let action of __sveltets_2_ensureArray(seriesActions)){action;
           { svelteHTML.createElement("div", { "class":`tooltip-row`,});
             { svelteHTML.createElement("span", { "class":`dot dot-${action}`,}); }
             { svelteHTML.createElement("span", { "class":`label`,});ACTION_LABELS[action]; }
             { svelteHTML.createElement("span", { "class":`value`,});points[hoverIndex].byAction[action] ?? 0; }
           }
        }
       }
    }

    if(seriesActions.length > 1){
       { svelteHTML.createElement("div", { "class":`legend`,});
           for(let action of __sveltets_2_ensureArray(seriesActions)){action;
           { svelteHTML.createElement("span", { "class":`legend-item`,});
             { svelteHTML.createElement("span", { "class":`dot dot-${action}`,}); }
            ACTION_LABELS[action];
           }
        }
       }
    }
  }else{
     { svelteHTML.createElement("div", { "class":`table-wrap scrollbar`,});
       { svelteHTML.createElement("table", {});
         { svelteHTML.createElement("thead", {});
           { svelteHTML.createElement("tr", {});
             { svelteHTML.createElement("th", {});  }
               for(let action of __sveltets_2_ensureArray(seriesActions)){action;
               { svelteHTML.createElement("th", {});ACTION_LABELS[action]; }
            }
           }
         }
         { svelteHTML.createElement("tbody", {});
             for(let p of __sveltets_2_ensureArray(tableRows())){p.time;
             { svelteHTML.createElement("tr", {});
               { svelteHTML.createElement("td", {});formatHM(p.time); }
                 for(let action of __sveltets_2_ensureArray(seriesActions)){action;
                 { svelteHTML.createElement("td", {});p.byAction[action] ?? 0; }
              }
             }
          }
         }
       }
     }
  }
 }


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const EventsChart__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type EventsChart__SvelteComponent_ = ReturnType<typeof EventsChart__SvelteComponent_>;
/*Ωignore_endΩ*/export default EventsChart__SvelteComponent_;