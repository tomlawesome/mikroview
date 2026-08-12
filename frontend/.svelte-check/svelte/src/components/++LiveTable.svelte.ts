///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

import { appState, applyFilters } from '../lib/state.svelte'
import { MAX_RENDERED_ROWS } from '../lib/constants'
import { COLUMNS, columnState } from '../lib/columns.svelte'
import { viewportState } from '../lib/viewport.svelte'
import type { ClientEvent, FirewallEvent } from '../lib/types'
import EventRow from './EventRow.svelte'
import EventCardMobile from './EventCardMobile.svelte'
import EventDetailSheet from './EventDetailSheet.svelte';

;type $$ComponentProps =  { events?: ClientEvent[]; emptyMessage?: string; honorAutoscroll?: boolean };function $$render() {

  
  
  
  
  
  
  
  
  

  // Both optional -- default to the live view's own state, so the
  // existing `<LiveTable />` call site (App.svelte's 'live' branch)
  // needs no change. A caller with its own independent event set,
  // filtered by some criteria the global FilterBar can't express, still
  // gets the same columns, resize handles, and EventRow click-to-filter
  // behavior for free. No current production caller does this (the old
  // Control Ports tab did, before #243 replaced it with Watchlist.svelte,
  // which shows watched destinations rather than a filtered event table)
  // -- kept for a future caller with the same shape, and exercised
  // directly by this component's own tests below.
  //
  // honorAutoscroll defaults true (the live view's own toolbar toggle
  // applies here) -- a caller with its own event set and no Autoscroll
  // control of its own would pass false, since the global toggle
  // freezing an unrelated table it doesn't render a control for would be
  // surprising, not helpful. Kept separate from "was an `events` prop
  // passed" so tests can supply a fixture `events` array while still
  // exercising the live view's freeze behavior.
  let {
    events,
    emptyMessage,
    honorAutoscroll = true,
  }:/*Ωignore_startΩ*/$$ComponentProps/*Ωignore_endΩ*/ = $props()

  let bodyEl: HTMLDivElement | undefined = $state()
  let gridEl: HTMLDivElement | undefined = $state()
  // Which event's EventDetailSheet.svelte is open (issue #85's mobile
  // card layout) -- null means none. Desktop never sets this. Typed as
  // FirewallEvent, not ClientEvent, to match EventRow/EventCardMobile's
  // own prop type -- applyFilters's declared return type is
  // FirewallEvent[] even though the real objects flowing through it are
  // ClientEvents (see state.svelte.ts), so `rendered` below is typed
  // FirewallEvent[] too.
  let selectedEvent: FirewallEvent | null = $state(null)
  let headerEls: (HTMLDivElement | undefined)[] = $state([])
  let dragIndex = $state<number | null>(null)
  let dragStartX = 0
  let dragStartWidth = 0

  // Absolutely-positioned children can't rely on percentage/inset-based
  // height (top:0;bottom:0) inside a grid item whose own height comes
  // from `align-self: stretch` -- that height isn't "definite" enough for
  // the browser to resolve the children against, so it collapses to 0.
  // Measuring the real header row height in JS sidesteps that entirely.
  let headerHeight = $state(38)

  // Resize handles are positioned from *measured* column boundaries
  // (getBoundingClientRect), not computed from columnState.widths --
  // several columns are `minmax(0, 1fr)` by default so they fill the
  // available width, and there's no way to know a flexible column's
  // actual rendered size without asking the DOM. Recomputed whenever the
  // grid's own size or template changes.
  let handleOffsets = $state<number[]>([])

  function measureOffsets() {
    if (!gridEl) return
    const gridLeft = gridEl.getBoundingClientRect().left
    handleOffsets = headerEls.slice(0, -1).map((el) => {
      if (!el) return 0
      const r = el.getBoundingClientRect()
      return r.left - gridLeft + r.width
    })
  }

  const deviceNames = $derived.by(() => {
    const map = new Map<string, string>()
    for (const d of appState.devices) map.set(d.id, d.name)
    return map
  })

  // A caller-supplied `events` array is used as-is, never re-filtered by
  // the global FilterBar -- such a caller applies its own, independent
  // match criteria before this ever sees it.
  const liveFiltered = $derived(
    events !== undefined ? events : applyFilters(appState.ageFilteredEvents, appState.filters, appState.ruleMatches),
  )
  const liveRendered = $derived(liveFiltered.slice(-MAX_RENDERED_ROWS))

  // Autoscroll off (issue #232) means "don't move the view", not just
  // "don't force-jump to the bottom" -- liveRendered is a sliding window
  // over MAX_RENDERED_ROWS, so once the total exceeds that cap, rows keep
  // falling off the top as new ones arrive at the bottom regardless of
  // autoscroll, which reads as the page scrolling itself out from under
  // you.
  //
  // frozenPool captures the RAW pool (pre-filter, post-age-cutoff) once,
  // the moment autoscroll turns off -- not the already-filtered/sliced
  // liveRendered. rendered then re-applies the *current* filters to that
  // frozen pool on every filter change, so narrowing/widening the filter
  // while frozen still works, but purely within what was already frozen:
  // an event that arrives after the freeze began can never appear, no
  // matter what the filter does afterward. Releases (frozenPool = null)
  // once autoscroll turns back on. Distinct from Pause, which also halts
  // the age-based display-duration cutoff and detection-adjacent
  // bookkeeping; this only stops what's on screen from moving.
  //
  // Scoped by honorAutoscroll to the live view's own table -- a
  // caller-supplied `events` array with no Autoscroll control of its own
  // passes honorAutoscroll={false}, so the global toggle never freezes
  // it. When it IS in scope and `events` was still explicitly supplied
  // (only test fixtures do this), there is no separate global filter to
  // re-apply -- the frozen pool is used as given.
  //
  // The pool itself lives on appState, not here: this component unmounts
  // when you switch views, and a local snapshot would be lost and
  // re-taken on return. See appState.frozenPool. An out-of-scope instance
  // returns early rather than clearing it -- otherwise merely mounting an
  // unrelated view would release the live view's freeze.
  $effect(() => {
    if (!honorAutoscroll) return
    if (appState.autoscroll) {
      appState.frozenPool = null
    } else if (appState.frozenPool === null) {
      appState.frozenPool = events ?? appState.ageFilteredEvents
    }
  })

  const frozenRendered = $derived(
    appState.frozenPool === null
      ? null
      : (events !== undefined
          ? appState.frozenPool
          : applyFilters(appState.frozenPool, appState.filters, appState.ruleMatches)
        ).slice(-MAX_RENDERED_ROWS),
  )

  const rendered = $derived(
    honorAutoscroll && !appState.autoscroll ? (frozenRendered ?? liveRendered) : liveRendered,
  )

  function deviceName(id: string): string {
    return deviceNames.get(id) ?? id
  }

  function startResize(index: number, e: PointerEvent) {
    dragIndex = index
    dragStartX = e.clientX
    dragStartWidth = headerEls[index]?.getBoundingClientRect().width ?? 120
    window.addEventListener('pointermove', onResizeMove)
    window.addEventListener('pointerup', endResize, { once: true })
    e.preventDefault()
  }

  function onResizeMove(e: PointerEvent) {
    if (dragIndex === null) return
    columnState.setWidth(dragIndex, dragStartWidth + (e.clientX - dragStartX))
  }

  function endResize() {
    dragIndex = null
    window.removeEventListener('pointermove', onResizeMove)
  }

  $effect(() => {
    // re-measure whenever the column template changes (resize, reset) or
    // the header row's own height/content changes
    columnState.gridTemplate
    headerHeight
    requestAnimationFrame(measureOffsets)
  })

  $effect(() => {
    if (!gridEl) return
    const ro = new ResizeObserver(() => measureOffsets())
    ro.observe(gridEl)
    return () => ro.disconnect()
  })

  $effect(() => {
    rendered.length // re-run this effect whenever the rendered set changes
    if (appState.autoscroll && !appState.paused && bodyEl) {
      requestAnimationFrame(() => {
        if (bodyEl) bodyEl.scrollTop = bodyEl.scrollHeight
      })
    }
  })
;
async () => {

 { svelteHTML.createElement("div", { "class":`table-wrap`,});
  if(viewportState.isMobile){
     { svelteHTML.createElement("div", { "class":`body scrollbar`,});
         for(let event of __sveltets_2_ensureArray(rendered)){event.id;
         { const $$_eliboMdraCtnevE2C = __sveltets_2_ensureComponent(EventCardMobile); new $$_eliboMdraCtnevE2C({ target: __sveltets_2_any(), props: {     event,"deviceName":deviceName(event.deviceId),"onOpen":() => (selectedEvent = event),}});}
      }
      if(rendered.length === 0){
         { svelteHTML.createElement("div", { "class":`empty`,});
          emptyMessage ??
            (appState.events.length === 0
              ? 'Waiting for events…'
              : 'No events match the current filters.');
         }
      }
     }
  }else{
     { const $$_div1 = svelteHTML.createElement("div", {  "class":`body scrollbar`,});bodyEl = $$_div1;
       { const $$_div2 = svelteHTML.createElement("div", {    "class":`grid`,"style":`grid-template-columns: ${columnState.gridTemplate}`,});gridEl = $$_div2;
            for(let col of __sveltets_2_ensureArray(COLUMNS)){let i = 1;col.key;
           { const $$_div3 = svelteHTML.createElement("div", {      "class":`header-cell`,});col.key === 'time';headerEls[i] = $$_div3;headerHeight= $$_div3.clientHeight;
             { svelteHTML.createElement("span", { "class":`label-text`,});col.label; }
           }
        }

         { svelteHTML.createElement("div", {   "class":`resize-overlay`,"style":`height: ${headerHeight}px`,});
              for(let col of __sveltets_2_ensureArray((COLUMNS.slice(0, -1)))){let i = 1;col.key;
             { svelteHTML.createElement("span", {                "class":`resizer`,"style":`left: ${(handleOffsets[i] ?? 0) - 5}px`,"onpointerdown":(e) => startResize(i, e),"ondblclick":() => columnState.reset(),"role":`separator`,"aria-orientation":`vertical`,"aria-label":`Resize ${col.label} column`,});dragIndex === i; }
          }
         }

           for(let event of __sveltets_2_ensureArray(rendered)){event.id;
           { const $$_woRtnevE3C = __sveltets_2_ensureComponent(EventRow); new $$_woRtnevE3C({ target: __sveltets_2_any(), props: {   event,"deviceName":deviceName(event.deviceId),}});}
        }
       }
      if(rendered.length === 0){
         { svelteHTML.createElement("div", { "class":`empty`,});
          emptyMessage ??
            (appState.events.length === 0
              ? 'Waiting for events…'
              : 'No events match the current filters.');
         }
      }
     }
  }
 }

if(selectedEvent){
   { const $$_teehSliateDtnevE0C = __sveltets_2_ensureComponent(EventDetailSheet); new $$_teehSliateDtnevE0C({ target: __sveltets_2_any(), props: {       "event":selectedEvent,"deviceName":deviceName(selectedEvent.deviceId),"onClose":() => (selectedEvent = null),}});}
}


};
return { props: {} as any as $$ComponentProps, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const LiveTable__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type LiveTable__SvelteComponent_ = ReturnType<typeof LiveTable__SvelteComponent_>;
/*Ωignore_endΩ*/export default LiveTable__SvelteComponent_;