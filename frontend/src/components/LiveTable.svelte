<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState, applyFilters } from '../lib/state.svelte'
  import { MAX_RENDERED_ROWS } from '../lib/constants'
  import { COLUMNS, columnState } from '../lib/columns.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import type { ClientEvent, FirewallEvent } from '../lib/types'
  import EventRow from './EventRow.svelte'
  import EventCardMobile from './EventCardMobile.svelte'
  import EventDetailSheet from './EventDetailSheet.svelte'

  // Both optional -- default to the live view's own state, so the
  // existing `<LiveTable />` call site (App.svelte's 'live' branch)
  // needs no change. A caller with its own independent event set (e.g.
  // ControlPorts.svelte, filtered by control-port destination rather
  // than the global FilterBar) still gets the same columns, resize
  // handles, and EventRow click-to-filter behavior for free.
  //
  // honorAutoscroll defaults true (the live view's own toolbar toggle
  // applies here) -- a caller with its own event set and no Autoscroll
  // control of its own (ControlPorts.svelte) passes false, since the
  // global toggle freezing an unrelated table it doesn't render a
  // control for would be surprising, not helpful. Kept separate from
  // "was an `events` prop passed" so tests can supply a fixture `events`
  // array while still exercising the live view's freeze behavior.
  let {
    events,
    emptyMessage,
    honorAutoscroll = true,
  }: { events?: ClientEvent[]; emptyMessage?: string; honorAutoscroll?: boolean } = $props()

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
  // the global FilterBar -- ControlPorts.svelte already applies its own,
  // independent match criteria (see its own comment on why appState.
  // filters can't express an OR-of-ports match) before this ever sees it.
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
  // caller-supplied `events` array (ControlPorts.svelte) has no
  // Autoscroll control of its own and passes honorAutoscroll={false}, so
  // the global toggle never freezes it. When it IS in scope and `events`
  // was still explicitly supplied (only test fixtures do this), there is
  // no separate global filter to re-apply -- the frozen pool is used as
  // given.
  //
  // The pool itself lives on appState, not here: this component unmounts
  // when you switch views, and a local snapshot would be lost and
  // re-taken on return. See appState.frozenPool. An out-of-scope instance
  // returns early rather than clearing it -- otherwise merely mounting
  // ControlPorts would release the live view's freeze.
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
</script>

<div class="table-wrap">
  {#if viewportState.isMobile}
    <div class="body scrollbar">
      {#each rendered as event (event.id)}
        <EventCardMobile {event} deviceName={deviceName(event.deviceId)} onOpen={() => (selectedEvent = event)} />
      {/each}
      {#if rendered.length === 0}
        <div class="empty">
          {emptyMessage ??
            (appState.events.length === 0
              ? 'Waiting for events…'
              : 'No events match the current filters.')}
        </div>
      {/if}
    </div>
  {:else}
    <div class="body scrollbar" bind:this={bodyEl}>
      <div class="grid" bind:this={gridEl} style="grid-template-columns: {columnState.gridTemplate}">
        {#each COLUMNS as col, i (col.key)}
          <div
            class="header-cell"
            class:sticky-col={col.key === 'time'}
            bind:this={headerEls[i]}
            bind:clientHeight={headerHeight}
          >
            <span class="label-text">{col.label}</span>
          </div>
        {/each}

        <div class="resize-overlay" style="height: {headerHeight}px">
          {#each COLUMNS.slice(0, -1) as col, i (col.key)}
            <span
              class="resizer"
              class:active={dragIndex === i}
              style="left: {(handleOffsets[i] ?? 0) - 5}px"
              onpointerdown={(e) => startResize(i, e)}
              ondblclick={() => columnState.reset()}
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize {col.label} column"
            ></span>
          {/each}
        </div>

        {#each rendered as event (event.id)}
          <EventRow {event} deviceName={deviceName(event.deviceId)} />
        {/each}
      </div>
      {#if rendered.length === 0}
        <div class="empty">
          {emptyMessage ??
            (appState.events.length === 0
              ? 'Waiting for events…'
              : 'No events match the current filters.')}
        </div>
      {/if}
    </div>
  {/if}
</div>

{#if selectedEvent}
  <EventDetailSheet
    event={selectedEvent}
    deviceName={deviceName(selectedEvent.deviceId)}
    onClose={() => (selectedEvent = null)}
  />
{/if}

<style>
  .table-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    overflow: hidden;
  }

  .body {
    flex: 1;
    overflow: auto;
    -webkit-overflow-scrolling: touch;
  }

  .grid {
    display: grid;
    align-content: start;
    min-width: 100%;
  }

  .header-cell {
    position: sticky;
    top: 0;
    z-index: 2;
    background: var(--bg-elevated);
    padding: 10px;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--fg-dim);
    font-weight: 600;
    border-bottom: 1px solid var(--border);
  }

  .label-text {
    display: block;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* Mirrors EventRow's .time sticky positioning so the header stays
     aligned with the sticky timestamp column beneath it while the table
     scrolls horizontally. */
  .sticky-col {
    position: sticky;
    left: 0;
    z-index: 3;
  }

  /* A single overlay layer for all resize handles, rather than nesting a
     handle inside each header cell -- a sticky header cell's own z-index
     scopes its children's z-index, so a handle overlapping the *next*
     cell can't paint above that cell's content no matter how high its
     z-index is set. Height is set from JS (see headerHeight) rather than
     CSS stretch, since an absolutely-positioned child can't resolve
     top/bottom insets against a grid item whose own height comes from
     align-self: stretch. */
  .resize-overlay {
    grid-column: 1 / -1;
    grid-row: 1;
    position: sticky;
    top: 0;
    z-index: 4;
    pointer-events: none;
  }

  .resizer {
    position: absolute;
    top: 0;
    height: 100%;
    width: 10px;
    cursor: col-resize;
    pointer-events: auto;
    touch-action: none;
    display: flex;
    justify-content: center;
  }

  /* A clearly-visible divider line at rest, so the resize affordance is
     discoverable without having to hover the exact pixel boundary first
     -- brightens and widens further on hover/drag. */
  .resizer::after {
    content: '';
    width: 2px;
    height: 60%;
    border-radius: 1px;
    background: var(--fg-dim);
  }

  .resizer:hover::after,
  .resizer.active::after {
    width: 3px;
    height: 100%;
    background: var(--accent);
  }

  .empty {
    padding: 48px 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 15px;
  }
</style>
