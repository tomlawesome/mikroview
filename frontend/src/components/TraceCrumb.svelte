<!-- SPDX-License-Identifier: AGPL-3.0-only -->
<script lang="ts">
  // The traced line's own crumb (#1018, round 53) plus its list (#1050,
  // round 56, A1): the whole story in one line -- who → who, the port,
  // the verdict and the rule that made it, how many more lines like it
  // there are -- and, on request, the two columns of lines that back
  // that count. Lifted out of Topography.svelte so the same markup can
  // be mounted on the city too (round 56's own rule: "the same HTML on
  // both surfaces"); nothing here reaches for anything Topography-only.
  import { mapTraceState } from '../lib/mapTrace.svelte'
  import { appState } from '../lib/state.svelte'
  import { formatHM } from '../lib/format'
  import type { FirewallEvent } from '../lib/types'

  const uid = $props.id()

  const traceOn = $derived(mapTraceState.active)

  function verdictWord(e: FirewallEvent): 'accepted' | 'refused' | 'logged' {
    if (e.action === 'accept') return 'accepted'
    if (e.action === 'drop' || e.action === 'reject') return 'refused'
    return 'logged'
  }
  function verdictClass(e: FirewallEvent): string {
    const w = verdictWord(e)
    return w === 'accepted' ? 'ok' : w === 'refused' ? 'al' : ''
  }
  function verdictText(e: FirewallEvent): string {
    return `${verdictWord(e)} at ${e.ruleName || e.ruleLabel || 'no rule named'}`
  }

  interface RowVM {
    key: string
    ev: FirewallEvent
    time: string
    detail: string
    verdict: string
    verdictClass: string
    traced: boolean
  }

  const tracedId = $derived(mapTraceState.event?.id ?? null)

  const lineRows = $derived.by((): RowVM[] =>
    mapTraceState.sameLine.map((e) => ({
      key: `line-${e.id}`,
      ev: e,
      time: formatHM(e.time),
      detail: '',
      verdict: verdictText(e),
      verdictClass: verdictClass(e),
      traced: e.id === tracedId,
    })),
  )

  const minuteRows = $derived.by((): RowVM[] =>
    mapTraceState.sameMinute.map((e) => {
      const dst = e.dstHostName || e.dstIp || 'unknown'
      const proto = (e.protocol ?? '?').toLowerCase()
      return {
        key: `minute-${e.id}`,
        ev: e,
        time: formatHM(e.time),
        detail: e.dstPort ? `→ ${dst} · ${e.dstPort}/${proto}` : `→ ${dst}`,
        verdict: verdictText(e),
        verdictClass: verdictClass(e),
        traced: false,
      }
    }),
  )

  /** The column headers' own sub-lines: what SAME LINE and SAME MINUTE
   * are lines like, in the crumb's own wording. */
  const lineSub = $derived.by(() => {
    const e = mapTraceState.event
    if (!e) return ''
    const src = e.srcHostName || e.srcIp || 'unknown'
    const dst = e.dstHostName || e.dstIp || 'unknown'
    const proto = (e.protocol ?? '?').toLowerCase()
    return e.dstPort ? `${src} → ${dst} · ${e.dstPort}/${proto}` : `${src} → ${dst}`
  })
  const minuteSub = $derived.by(() => {
    const e = mapTraceState.event
    if (!e) return ''
    return `${e.srcHostName || e.srcIp || 'unknown'} · ${formatHM(e.time)}`
  })

  // What each footer says: the exact overflow past the eight shown, or
  // the plain link where there is none -- the same "+N more" grammar the
  // mockup's own footer uses.
  const lineMoreCount = $derived(Math.max(0, mapTraceState.like + 1 - lineRows.length))
  const minuteMoreCount = $derived(Math.max(0, mapTraceState.sameMinuteTotal - minuteRows.length))

  /** The same filter the crumb's own "and N more like it" has always
   * set: source, destination and port, into the stream. */
  function openStreamForLine() {
    const e = mapTraceState.event
    if (!e) return
    appState.resetFilters()
    if (e.srcIp) appState.setFilter('srcQuery', e.srcIp)
    if (e.dstIp) appState.setFilter('dstQuery', e.dstIp)
    if (e.dstPort) appState.setFilter('port', String(e.dstPort))
    appState.view = 'live'
  }

  /** The same-minute footer's own filter: the source alone. There is no
   * minute-window filter in the stream's own Filters model (lib/types.ts)
   * to add "that minute only" on top of it, so this sets the source and
   * leaves the rest of round 56's A1 wording unmet -- see the build's own
   * report for this gap rather than inventing a new filter field here. */
  function openStreamForMinute() {
    const e = mapTraceState.event
    if (!e) return
    appState.resetFilters()
    if (e.srcIp) appState.setFilter('srcQuery', e.srcIp)
    appState.view = 'live'
  }

  function openRow(e: FirewallEvent) {
    void mapTraceState.open({ event: e.id })
  }

  let toggleEl: HTMLButtonElement | undefined = $state()
  let wrapEl: HTMLDivElement | undefined = $state()
  let rowEls: (HTMLButtonElement | undefined)[] = $state([])

  function toggleList() {
    mapTraceState.listOpen = !mapTraceState.listOpen
  }

  function closeListToTrigger() {
    mapTraceState.listOpen = false
    toggleEl?.focus()
  }

  // Esc closes the list first, a second Esc (Topography's own ladder,
  // reading mapTraceState.listOpen) clears the trace -- this handler
  // only ever owns the first of those two rungs.
  function onListKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      closeListToTrigger()
      return
    }
    if (e.key === 'Tab') {
      mapTraceState.listOpen = false
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const rows = rowEls.filter((el): el is HTMLButtonElement => !!el)
      if (rows.length === 0) return
      e.preventDefault()
      const at = rows.indexOf(document.activeElement as HTMLButtonElement)
      const next = e.key === 'ArrowDown' ? (at + 1) % rows.length : (at - 1 + rows.length) % rows.length
      rows[next]?.focus()
    }
  }

  // Moves focus into the list exactly once, on the open transition --
  // the same guard Topography's own dial panel uses, so a live update to
  // the rows while the list is open does not yank focus back to row one.
  let focusedForList = false
  $effect(() => {
    if (mapTraceState.listOpen && !focusedForList) {
      rowEls.find((el): el is HTMLButtonElement => !!el)?.focus()
    }
    focusedForList = mapTraceState.listOpen
  })

  // Click-away: a capturing window listener closes the list before
  // whatever was under the click sees it, the same device Topography's
  // dial panel uses.
  $effect(() => {
    if (!mapTraceState.listOpen) return
    function onClickAway(e: MouseEvent) {
      if (wrapEl && e.target instanceof Node && wrapEl.contains(e.target)) return
      mapTraceState.listOpen = false
    }
    window.addEventListener('click', onClickAway, true)
    return () => window.removeEventListener('click', onClickAway, true)
  })
</script>

{#if traceOn}
  <div class="trace-wrap" bind:this={wrapEl}>
    <div class="crumb trace-crumb" aria-label="The traced line">
      <div class="path">
        {#if mapTraceState.loading}
          <span class="here">tracing…</span>
        {:else if mapTraceState.event}
          {@const e = mapTraceState.event}
          <span class="here">{e.srcHostName || e.srcIp || 'unknown'}</span>
          {#if e.srcHostName && e.srcIp}<span class="ip">{e.srcIp}</span>{/if}
          <span aria-hidden="true">→</span>
          <span class="here">{e.dstHostName || e.dstIp || 'unknown'}</span>
          {#if e.dstHostName && e.dstIp}<span class="ip">{e.dstIp}</span>{/if}
          <i class="bar"></i>
          <!-- RouterOS logs the protocol in caps; every other port label
               on this map (the pill, the door, the reach's card) reads
               `445/tcp`, so this one does too. -->
          {#if e.dstPort}<span>{e.dstPort}/{(e.protocol ?? '?').toLowerCase()}</span>{/if}
          <span class:alarm={mapTraceState.verdict === 'refused'}>
            {mapTraceState.verdict === 'refused' ? 'refused' : mapTraceState.verdict === 'accepted' ? 'accepted' : 'logged'}
            at {e.ruleName || e.ruleLabel || 'no rule named'}
          </span>
          <span>{formatHM(e.time)}</span>
          {#if mapTraceState.like > 0}
            <i class="bar"></i>
            <!-- The others are a click into the list (round 56, A1) --
                 "and 41 more like it", never a union on the map. -->
            <button
              class="crumb-link"
              bind:this={toggleEl}
              aria-expanded={mapTraceState.listOpen}
              aria-controls="{uid}-picker"
              onclick={toggleList}
              >and <b>{mapTraceState.like} more like it</b> {mapTraceState.listOpen ? '▾' : '▸'}</button
            >
          {/if}
        {:else}
          <!-- An honest miss: the window holds nothing matching, said in
               words rather than drawn as a path that went nowhere. -->
          <span class="here">nothing in the window matches that line</span>
        {/if}
        <i class="bar"></i>
        <button class="crumb-link esc" onclick={() => mapTraceState.clear()}>Esc ▸</button>
      </div>
    </div>
    {#if mapTraceState.listOpen && mapTraceState.event}
      <div
        id="{uid}-picker"
        class="picker"
        role="listbox"
        tabindex="-1"
        aria-label="Lines like the traced one: the same line, and the same minute"
        onkeydown={onListKeydown}
      >
        <div class="col">
          <h5>SAME LINE <b>{mapTraceState.like}</b><small>{lineSub}</small></h5>
          {#each lineRows as row, i (row.key)}
            <button
              type="button"
              class="row"
              class:on={row.traced}
              role="option"
              aria-selected={row.traced}
              bind:this={rowEls[i]}
              onclick={() => openRow(row.ev)}
            >
              <span class="t">{row.time}</span>
              <span class="v {row.verdictClass}" title={row.verdict}>{row.verdict}</span>
              {#if row.traced}<span class="now">TRACED</span>{/if}
            </button>
          {/each}
          <button type="button" class="foot" onclick={openStreamForLine}>
            {lineMoreCount > 0 ? `+${lineMoreCount} more in the stream ▸` : 'more in the stream ▸'}
          </button>
        </div>
        <div class="col">
          <h5>SAME MINUTE <b>{mapTraceState.sameMinuteTotal}</b><small>{minuteSub}</small></h5>
          {#each minuteRows as row, i (row.key)}
            <button
              type="button"
              class="row"
              role="option"
              aria-selected="false"
              bind:this={rowEls[lineRows.length + i]}
              onclick={() => openRow(row.ev)}
            >
              <span class="t">{row.time}</span>
              <span class="d" title={row.detail}>{row.detail}</span>
              <span class="v {row.verdictClass}" title={row.verdict}>{row.verdict}</span>
            </button>
          {/each}
          <button type="button" class="foot" onclick={openStreamForMinute}>
            {minuteMoreCount > 0 ? `+${minuteMoreCount} more in the stream ▸` : 'more in the stream ▸'}
          </button>
        </div>
        <div class="hint">↑↓ pick · ↵ redraws the trace here · Esc clears</div>
      </div>
    {/if}
  </div>
{/if}

<style>
  /* Ported from Topography.svelte's own `.crumb` -- the two crumbs
     (this one and the reach's) share the same look, and Svelte's
     component-scoped styles mean that has to be said twice rather than
     once, now that this one is its own component. */
  .trace-wrap {
    position: absolute;
    top: 14px;
    left: 24px;
    /* #1050 round 56 defect 2: the picker below is anchored off this
       element's own width (`left: calc(50% - 196px)`, the mockup's own
       formula), so this has to span the map rather than shrink to the
       crumb bar's content -- otherwise "50%" is 50% of the crumb text,
       not of the map, and the picker lands wherever the crumb happens
       to be wide enough to put it. The crumb itself is unaffected: it
       is a flex row that never asked for a background of its own. */
    right: 0;
    z-index: 2;
    /* Spanning the map (above) would otherwise let this wrapper's own
       empty space eat clicks meant for whatever is drawn under it --
       its two children opt back in below. */
    pointer-events: none;
  }

  .trace-wrap > * {
    pointer-events: auto;
  }

  .crumb {
    /* trace-wrap (above) now spans the map so the picker's own
       percentage anchor has something real to measure against; without
       this a plain block child would stretch to match it, and the
       empty space beside the actual crumb text would start capturing
       clicks meant for the map under it. */
    width: fit-content;
  }

  .crumb .path {
    display: flex;
    flex-wrap: wrap;
    gap: 11px;
    align-items: baseline;
    font-size: 18px;
    font-weight: 550;
    letter-spacing: -0.01em;
    color: var(--fg);
  }

  .crumb .path > span:not(.here) {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--fg-dim);
  }

  .crumb .path .ip {
    color: var(--fg-muted);
  }

  .crumb .path span.alarm {
    color: var(--alarm);
  }

  .crumb i.bar {
    width: 1px;
    height: 12px;
    background: var(--hair-2);
    align-self: center;
  }

  .crumb .esc {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--accent);
  }

  .crumb .here {
    color: var(--accent);
  }

  .crumb-link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--fg);
    cursor: pointer;
  }

  .crumb-link:hover {
    color: var(--accent);
  }

  /* Round 56's own list (A1), ported from
     docs/design/concepts/round-56/index.html's `.picker` block --
     mockup tokens mapped onto this app's own (--ink → --fg, --ink-2 →
     --fg-muted, --ink-3 → --fg-dim, --mono → --font-mono, --hair →
     --hair-2, the app has one hairline strength where the mockup has
     two). */
  .picker {
    position: absolute;
    top: 52px;
    /* #1050 round 56 defect 2: `left: 0` put this under the city's own
       ESTATE MAP mini-map (City.svelte, top-left) -- round-56's own
       mockup (index.html's `.picker`) anchors it off the crumb's centre
       instead, right-aligned under "and N more like it", clear of any
       top-left furniture on either surface. */
    left: calc(50% - 196px);
    z-index: 9;
    width: 574px;
    display: flex;
    flex-wrap: wrap;
    gap: 0;
    padding: 0;
    background: rgba(15, 20, 34, 0.985);
    border: 1px solid var(--hair-2);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
    font: 10.5px var(--font-mono);
    color: var(--fg-muted);
    backdrop-filter: blur(7px);
  }

  .picker::before {
    content: '';
    position: absolute;
    top: -6px;
    /* Ported with the panel's own anchor above: the mockup points this
       from the panel's right side (`right: 118px`), which is where "and
       N more like it" sits once the panel is centred under the crumb
       rather than pinned to its left edge. */
    right: 118px;
    width: 10px;
    height: 10px;
    background: rgba(15, 20, 34, 0.96);
    border-left: 1px solid var(--hair-2);
    border-top: 1px solid var(--hair-2);
    transform: rotate(45deg);
  }

  .picker .col {
    flex: 1;
    padding: 8px 10px 7px;
    min-width: 0;
  }

  .picker .col + .col {
    border-left: 1px solid var(--hair-2);
  }

  .picker h5 {
    font: 600 9px var(--font-mono);
    letter-spacing: 0.12em;
    color: var(--fg-dim);
    margin: 0 0 5px;
  }

  .picker h5 b {
    color: var(--fg-muted);
    font-weight: 600;
    letter-spacing: 0;
  }

  .picker h5 small {
    display: block;
    margin-top: 2px;
    font: 9.5px var(--font-mono);
    letter-spacing: 0;
    color: var(--fg-dim);
    font-weight: 400;
  }

  .picker .row {
    display: flex;
    gap: 8px;
    align-items: baseline;
    width: 100%;
    padding: 2px 6px;
    margin: 0 0 0 -6px;
    border-radius: 5px;
    cursor: pointer;
    white-space: nowrap;
    background: none;
    border: none;
    font: inherit;
    color: inherit;
    text-align: left;
    /* #1050 round 56 defect 1: without this, a row's own min-content
       width (its text, unwrapped) floors how far flexbox can shrink it,
       so a long verdict or detail pushed the row past the column and
       the column past the panel. `overflow: hidden` here sets the row's
       own used minimum width to 0 per the flexbox spec, so its children
       below can shrink and ellipsis instead. */
    overflow: hidden;
  }

  .picker .row .d,
  .picker .row .v {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .picker .row:hover {
    background: rgba(157, 184, 232, 0.08);
  }

  .picker .row.on {
    background: rgba(157, 184, 232, 0.14);
    color: var(--fg);
  }

  .picker .row .t {
    color: var(--fg);
    font-variant-numeric: tabular-nums;
  }

  .picker .row .t::before {
    content: '▸';
    display: inline-block;
    width: 9px;
    color: transparent;
  }

  .picker .row.on .t::before {
    color: var(--accent);
  }

  .picker .row .d {
    color: var(--fg-muted);
  }

  .picker .row .v {
    margin-left: auto;
  }

  .picker .row .v.al {
    color: var(--alarm);
  }

  .picker .row .v.ok {
    color: var(--accept);
  }

  .picker .row .now {
    font-size: 9px;
    color: var(--fg-dim);
    letter-spacing: 0.08em;
  }

  .picker .foot {
    display: block;
    width: 100%;
    margin-top: 5px;
    padding-top: 5px;
    border: none;
    border-top: 1px solid var(--hair-2);
    background: none;
    color: var(--accent);
    font: inherit;
    text-align: left;
    text-decoration: none;
    cursor: pointer;
  }

  .picker .foot:hover {
    text-decoration: underline;
  }

  .picker .hint {
    flex-basis: 100%;
    padding: 5px 10px 6px;
    border-top: 1px solid var(--hair-2);
    font: 9px var(--font-mono);
    color: var(--fg-dim);
    letter-spacing: 0.04em;
    text-align: right;
  }
</style>
