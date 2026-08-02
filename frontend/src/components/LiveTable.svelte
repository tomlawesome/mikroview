<script lang="ts">
  import { appState } from '../lib/state.svelte'
  import { MAX_RENDERED_ROWS } from '../lib/constants'
  import { COLUMNS, columnState } from '../lib/columns.svelte'
  import EventRow from './EventRow.svelte'

  let bodyEl: HTMLDivElement | undefined = $state()
  let dragIndex = $state<number | null>(null)
  let dragStartX = 0
  let dragStartWidth = 0
  // Absolutely-positioned children can't rely on percentage/inset-based
  // height (top:0;bottom:0) inside a grid item whose own height comes
  // from `align-self: stretch` -- that height isn't "definite" enough for
  // the browser to resolve the children against, so it collapses to 0.
  // Measuring the real header row height in JS sidesteps that entirely.
  let headerHeight = $state(38)

  const deviceNames = $derived.by(() => {
    const map = new Map<string, string>()
    for (const d of appState.devices) map.set(d.id, d.name)
    return map
  })

  const rendered = $derived(appState.filteredEvents.slice(-MAX_RENDERED_ROWS))

  function deviceName(id: string): string {
    return deviceNames.get(id) ?? id
  }

  function startResize(index: number, e: PointerEvent) {
    dragIndex = index
    dragStartX = e.clientX
    dragStartWidth = columnState.widths[index]
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
    rendered.length // re-run this effect whenever the rendered set changes
    if (appState.autoscroll && !appState.paused && bodyEl) {
      requestAnimationFrame(() => {
        if (bodyEl) bodyEl.scrollTop = bodyEl.scrollHeight
      })
    }
  })
</script>

<div class="table-wrap">
  <div class="body scrollbar" bind:this={bodyEl}>
    <div class="grid" style="grid-template-columns: {columnState.gridTemplate}">
      {#each COLUMNS as col, i (col.key)}
        <div class="header-cell" bind:clientHeight={headerHeight}>
          <span class="label-text">{col.label}</span>
        </div>
      {/each}

      <div class="resize-overlay" style="height: {headerHeight}px">
        {#each COLUMNS.slice(0, -1) as col, i (col.key)}
          <span
            class="resizer"
            class:active={dragIndex === i}
            style="left: {columnState.offsets[i] - 5}px"
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
        {appState.events.length === 0
          ? 'Waiting for events…'
          : 'No events match the current filters.'}
      </div>
    {/if}
  </div>
</div>

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
  }

  .grid {
    display: grid;
    align-content: start;
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

  /* A single overlay layer for all resize handles, rather than nesting a
     handle inside each header cell -- see the comment on
     lib/columns.svelte.ts's `offsets` for why. Placed explicitly into the
     header row (row 1) so it overlaps the header cells; height is set
     from JS (see headerHeight) rather than CSS stretch, since an
     absolutely-positioned child can't resolve top/bottom insets against a
     grid item whose own height comes from align-self: stretch. */
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
  }

  .resizer:hover,
  .resizer.active {
    background: var(--accent);
    opacity: 0.4;
  }

  .empty {
    padding: 48px 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 15px;
  }
</style>
