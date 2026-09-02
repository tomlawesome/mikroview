<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState, applyFilters } from '../lib/state.svelte'
  import { whisperState } from '../lib/whisper.svelte'
  import { authState } from '../lib/auth.svelte'
  import { MAX_RENDERED_ROWS } from '../lib/constants'
  import { formatTime } from '../lib/format'
  import { columnState } from '../lib/columns.svelte'
  import { groupModeState } from '../lib/groupMode.svelte'
  import { flaggedSources, groupEvents, drawerEvents, hiddenInDrawer } from '../lib/grouping'
  import { flagsState } from '../lib/flags.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import type { ClientEvent, FirewallEvent } from '../lib/types'
  import EventRow from './EventRow.svelte'
  import EventCardMobile from './EventCardMobile.svelte'
  import EventDetailSheet from './EventDetailSheet.svelte'
  import GhostRows from './GhostRows.svelte'
  import { wizardState } from '../lib/wizard.svelte'

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
  }: { events?: ClientEvent[]; emptyMessage?: string; honorAutoscroll?: boolean } = $props()

  let bodyEl: HTMLDivElement | undefined = $state()
  let gridEl: HTMLDivElement | undefined = $state()
  // Which event's EventDetailSheet.svelte is open -- null means none.
  // Originally issue #85's mobile card layout only; #644's squared
  // columns made it every row's detail surface, since the sheet is the
  // one place a row's full detail lives -- raw line, MAC/NAT lookups,
  // and (#717 restored these as columns too, but the sheet still has
  // them) device, chain, interfaces, src port, NAT, MAC. Typed as
  // FirewallEvent, not ClientEvent, to match EventRow/EventCardMobile's
  // own prop type -- applyFilters's declared return type is
  // FirewallEvent[] even though the real objects flowing through it are
  // ClientEvents (see state.svelte.ts), so `rendered` below is typed
  // FirewallEvent[] too.
  let selectedEvent: FirewallEvent | null = $state(null)
  let headerEls: (HTMLDivElement | undefined)[] = $state([])

  // Column resize, mounted again by round 36 and drawing nothing at
  // rest: "the boundary reveals itself under the hand -- a hair on the
  // header's edge and a col-resize cursor -- and is otherwise
  // invisible." Round 30 had unmounted it because its own shots showed
  // no handle; what they showed no handle of was the *resting* header,
  // and the answer drawn here is a hover state, not an absent feature.
  // The visible half is entirely CSS (.header-cell::after and
  // .resizer's own hover/active rules below); the drag itself is
  // measureOffsets/handleOffsets/startResize/onResizeMove/endResize.
  // Which boundary is being dragged, twice over: the position (so the
  // handle under the pointer keeps its own `.active` look) and the
  // column's key (so the width lands on the right column whatever the
  // reader has hidden -- see columnState.setWidthForKey).
  let dragIndex = $state<number | null>(null)
  let dragKey: string | null = null
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
  // "don't force-jump to the newest row" -- liveRendered is a sliding
  // window over MAX_RENDERED_ROWS, so once the total exceeds that cap,
  // rows keep falling off one end of this array as new ones arrive at
  // the other, regardless of autoscroll, which reads as the page
  // scrolling itself out from under you. (This array itself stays
  // oldest-first -- see displayRendered below for where newest-at-top
  // is actually applied. "Falling off" here is about eviction from the
  // buffer, not about which end of the screen it happens on.)
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
  // appState.streamHeld is the transient hold an open row-anchored
  // surface takes (see appState.holdStream): it freezes on exactly the
  // same terms as Autoscroll-off, so the two compose as one condition
  // here rather than as a second freeze mechanism.
  $effect(() => {
    if (!honorAutoscroll) return
    // streamHeld, not autoscroll: #413 holds the stream transiently
    // while a row-anchored surface is open, without touching the
    // Autoscroll preference. See appState.streamHolds.
    if (!appState.streamHeld) {
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
    honorAutoscroll && appState.streamHeld ? (frozenRendered ?? liveRendered) : liveRendered,
  )

  // Grouping (#341): collapse repeats of the same connection. Built
  // from `rendered` rather than from the whole buffer, so grouping is a
  // lens over exactly what the view would have shown -- the counts
  // account for every row the ungrouped view has, and no more. Grouping
  // a wider set would quietly change what a filter means.
  //
  // `rendered` and `groups` both stay in arrival order (oldest first) --
  // that's what the freeze logic above and groupEvents' "head is the
  // first arrival" rule are written against. Newest-at-top (#363) is
  // purely a rendering concern, applied once, below, rather than
  // threaded back through either of those.
  const groups = $derived(groupModeState.enabled ? groupEvents(rendered) : [])

  // Newest-at-top (#363, decided 2026-08-13: "Most recent at the top,
  // older at the bottom is easy, simple design language"). `rendered`
  // and `groups` are oldest-first internally (see above); reversed only
  // here, at the last step before the template iterates them, so this is
  // the one place display order lives. A plain reverse of a `.slice()`
  // copy -- never the source arrays -- so it can't disturb the frozen
  // pool or the grouping key's "first arrival wins" rule.
  //
  // Groups still keep their position once display order is inverted:
  // "position" now means "how recently this group first appeared",
  // measured from the top instead of from the bottom, but a group being
  // hit again still doesn't move -- see groupEvents' head comment.
  const displayRendered = $derived([...rendered].reverse())
  const displayGroups = $derived([...groups].reverse())

  // What the empty-body area shows when `rendered` has nothing in it --
  // one derived rather than the inline ternary chain this used to be,
  // because #549 adds a fourth case (still loading) ahead of the three
  // #373 already distinguished (failed, confirmed-empty, filtered-empty),
  // and a fifth reading on top of that (confirmed-empty *because setup
  // has never run*). Order matters: fetchFailed is checked first because
  // a failure can happen with a non-empty buffer still on screen (a
  // refetch that failed leaves the pre-refetch buffer in place -- see
  // refetchWithFilters' own comment) as well as an empty one, and it
  // always outranks every other reading once true.
  const emptyState = $derived.by((): { kind: 'ghost' } | { kind: 'text'; text: string } => {
    if (emptyMessage) return { kind: 'text', text: emptyMessage }
    if (appState.fetchFailed) {
      return { kind: 'text', text: 'Could not load events from the server — this is not a confirmed empty result.' }
    }
    // A real, non-empty buffer that the *current filter* happens to
    // exclude -- nothing to do with loading or first run, checked ahead
    // of both so a narrow filter on a healthy, populated buffer never
    // reads as either.
    if (appState.events.length > 0) return { kind: 'text', text: 'No events match the current filters.' }
    // Empty because the reader emptied it (round 36's `wipe`). Said
    // before every other empty reading below, all of which would be
    // wrong here -- ghost rows promise lines that are not coming, and
    // "waiting for events" blames the estate for a silence the operator
    // caused. The second half is the half that is not on screen: the
    // wipe took this browser's copy and nothing else.
    if (appState.wipedAt !== null) {
      return {
        kind: 'text',
        text: `Nothing since ${formatTime(new Date(appState.wipedAt).toISOString())} — wiped here, by you · the server's ring still holds every line`,
      }
    }
    // The buffer is empty and nothing has failed -- either the app's one
    // loadInitial() call (App.svelte's mount effect) hasn't come back
    // yet, or it has and the server genuinely has nothing. Ghost rows,
    // not a spinner, per the record's Loading state, cover the former;
    // ghost rows are the wrong answer to the latter (there is nothing
    // coming to fill them), which is why this only fires while
    // initialLoadDone is still false.
    if (!appState.initialLoadDone) return { kind: 'ghost' }
    // Confirmed empty, and no device has ever sent anything -- the
    // sharpest first-run signal available client-side, since it is
    // exactly what running setup (pointing a RouterOS device at
    // mikroview) produces. #490's grammar already keeps "Run setup…"
    // absent for viewers, never disabled, so the pointer only names it
    // for an admin who can actually reach it; a viewer is told who can.
    if (appState.devices.length === 0) {
      const base =
        authState.role === 'admin'
          ? 'No devices have sent anything yet — your account menu ▸ Run setup… to point a RouterOS device at mikroview.'
          : 'No devices have sent anything yet. Ask an administrator to run setup.'
      // #487's "the record is the feature": where a setup step was
      // skipped or forced past, this silence has a recorded cause, and
      // the empty state names it rather than leaving the operator to
      // wonder. Null when the ledger explains nothing -- an empty
      // surface with no decision behind it is simply empty, and
      // inventing a cause would be the opposite of the point.
      const silence = wizardState.silence
      return { kind: 'text', text: silence ? `${base} ${silence}` : base }
    }
    return { kind: 'text', text: 'Waiting for events…' }
  })

  // Sources carrying an active flag, for the row marker. Recomputed from
  // the flag list rather than per row, so this is one pass rather than
  // one lookup per rendered row.
  const flagged = $derived(flaggedSources(flagsState.list))

  // The stream's foot band is gone (owner, round 36: "oh that thing, I
  // don't want that at all", closing #717's earlier "I hate it, remove
  // it"), and gone wholesale rather than gated off: round 37 removed it
  // from the drawing, so there is no round for it to come back in. Of
  // its three facts, a repeating refusal is already a flag and a dark
  // boundary is already on the fall and the topography; the `▲3.1×`
  // drops trend had no other home and is dropped with it -- if a rise
  // matters it is a watch that flags, not a strip. lib/footLine.ts and
  // its tests went with the band, and so did the on-mount read of
  // fallState's pushed rule tables that only the dark-boundary fact
  // needed -- this table never asked the server for a boundary for any
  // other reason.

  // Which groups are open. Keyed by the group key rather than by index,
  // so an open drawer stays with its group as new events arrive.
  let openGroups = $state(new Set<string>())

  function toggleGroup(key: string) {
    const next = new Set(openGroups)
    if (!next.delete(key)) next.add(key)
    openGroups = next
  }

  // An expansion does not outlive the group it expanded (#381, owner
  // decision 2026-08-22): when a group's key leaves the rendered window
  // -- its traffic aged out, a filter excluded it, or grouping was
  // turned off -- its open state goes with it, so the same connection
  // recurring later renders collapsed rather than silently pre-expanded
  // against a set of events the operator never chose to open. One
  // reactive prune covers every eviction path; the write is guarded so
  // an unchanged set does not re-trigger the effect.
  $effect(() => {
    const present = new Set(groups.map((g) => g.key))
    const kept = [...openGroups].filter((k) => present.has(k))
    if (kept.length !== openGroups.size) openGroups = new Set(kept)
  })

  function deviceName(id: string): string {
    return deviceNames.get(id) ?? id
  }

  // applyFilters is declared FirewallEvent[] even though the objects
  // flowing through it are always ClientEvents (see state.svelte.ts's
  // own comment on filteredEvents) -- this reads the receivedAt every
  // one of them actually carries, for the whisper's fence (#644).
  function isDimmed(event: FirewallEvent): boolean {
    return whisperState.dimmed((event as ClientEvent).receivedAt)
  }

  function startResize(index: number, key: string, e: PointerEvent) {
    dragIndex = index
    dragKey = key
    dragStartX = e.clientX
    dragStartWidth = headerEls[index]?.getBoundingClientRect().width ?? 120
    window.addEventListener('pointermove', onResizeMove)
    window.addEventListener('pointerup', endResize, { once: true })
    e.preventDefault()
  }

  function onResizeMove(e: PointerEvent) {
    if (dragKey === null) return
    columnState.setWidthForKey(dragKey, dragStartWidth + (e.clientX - dragStartX))
  }

  function endResize() {
    dragIndex = null
    dragKey = null
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
    if (appState.autoscroll && !appState.streamHeld && !appState.paused && bodyEl) {
      // Newest-at-top (#363): the newest row renders first now, so
      // "follow the newest event" means holding scrollTop at 0, not
      // chasing scrollHeight. Still an rAF, not a synchronous set --
      // the DOM has to actually contain the new row before scrolling to
      // it means anything.
      requestAnimationFrame(() => {
        if (bodyEl) bodyEl.scrollTop = 0
      })
    }
  })
</script>

<div class="table-wrap">
  {#if viewportState.isMobile}
    <div class="body scrollbar">
      {#each displayRendered as event (event.id)}
        <EventCardMobile
          {event}
          deviceName={deviceName(event.deviceId)}
          dimmed={isDimmed(event)}
          onOpen={() => (selectedEvent = event)}
        />
      {/each}
      {#if rendered.length === 0}
        {#if emptyState.kind === 'ghost'}
          <GhostRows label="Loading events…" rows={4} />
        {:else}
          <div class="empty">{emptyState.text}</div>
        {/if}
      {/if}
    </div>
  {:else}
    <div class="body scrollbar" bind:this={bodyEl}>
      <div class="grid" bind:this={gridEl} style="grid-template-columns: {columnState.gridTemplate}">
        <!-- #729: the reader's chosen subset, not the fixed fifteen --
             columnState.visibleColumns already carries Time and Rule
             (pinned, always in it) plus whatever else the chooser in
             FilterBar left on. EventRow's own cells are gated on the same
             columnState.isColumnVisible(key) calls, column by column, so
             the two can never disagree about which columns are showing. -->
        {#each columnState.visibleColumns as col, i (col.key)}
          <div
            class="header-cell"
            class:sticky-col={col.key === 'time'}
            bind:this={headerEls[i]}
            bind:clientHeight={headerHeight}
          >
            <span class="label-text">{col.label}</span>
          </div>
        {/each}

        <!-- The drag targets, one per column boundary. Over
             columnState.visibleColumns, not the fixed COLUMNS list:
             handleOffsets is measured from the header cells that are
             actually rendered (#729's chooser can leave any of them
             off), and startResize/setWidth index by the same position,
             so anything else silently drags the wrong column's edge.
             Invisible at rest -- see .resizer's own comment below. -->
        <div class="resize-overlay" style="height: {headerHeight}px">
          {#each columnState.visibleColumns.slice(0, -1) as col, i (col.key)}
            <span
              class="resizer"
              class:active={dragIndex === i}
              style="left: {(handleOffsets[i] ?? 0) - 5}px"
              onpointerdown={(e) => startResize(i, col.key, e)}
              ondblclick={() => columnState.reset()}
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize {col.label} column"
            ></span>
          {/each}
        </div>

        {#if groupModeState.enabled}
          {#each displayGroups as group, gi (group.key)}
            <EventRow
              event={group.head}
              deviceName={deviceName(group.head.deviceId)}
              count={group.count}
              flagged={flagged.has(group.head.srcIp ?? '')}
              dimmed={isDimmed(group.head)}
              banded={gi % 2 === 1}
              expandable={group.count > 1}
              expanded={openGroups.has(group.key)}
              onToggle={() => toggleGroup(group.key)}
              onOpen={() => (selectedEvent = group.head)}
            />
            <!-- Gated on group.count > 1 as well as the open flag, matching
                 the `expandable` predicate on the toggle above. The two used
                 to be written independently, and `groups` is $derived from
                 `rendered` while `openGroups` is not, so they diverged the
                 moment a group's count fell to 1 with its drawer open (a
                 filter narrowing to one member, or older members sliding out
                 of MAX_RENDERED_ROWS). The toggle disappeared while the
                 drawer stayed, rendering the one remaining event twice --
                 once as itself and once as a child of itself -- with no
                 control left to collapse it (#381). -->
            {#if group.count > 1 && openGroups.has(group.key)}
              {#each drawerEvents(group) as member (member.id)}
                <EventRow
                  event={member}
                  deviceName={deviceName(member.deviceId)}
                  flagged={flagged.has(member.srcIp ?? '')}
                  dimmed={isDimmed(member)}
                  member
                  onOpen={() => (selectedEvent = member)}
                />
              {/each}
              {#if hiddenInDrawer(group) > 0}
                <div class="drawer-note">
                  Showing the most recent {drawerEvents(group).length} of {group.count}; the other
                  {hiddenInDrawer(group)} are older than this list.
                </div>
              {/if}
              {#if group.rules.length > 1}
                <div class="drawer-note">
                  Matched more than one rule ({group.rules.join(', ')}) — usually a rule-ordering
                  surprise.
                </div>
              {/if}
            {/if}
          {/each}
        {:else}
          {#each displayRendered as event, i (event.id)}
            <EventRow
              {event}
              deviceName={deviceName(event.deviceId)}
              flagged={flagged.has(event.srcIp ?? '')}
              dimmed={isDimmed(event)}
              banded={i % 2 === 1}
              onOpen={() => (selectedEvent = event)}
            />
          {/each}
        {/if}
      </div>
      {#if rendered.length === 0}
        {#if emptyState.kind === 'ghost'}
          <GhostRows label="Loading events…" rows={6} />
        {:else}
          <div class="empty">{emptyState.text}</div>
        {/if}
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
  /* Spans every column: it is about the group, not about a field. */
  .drawer-note {
    grid-column: 1 / -1;
    padding: 4px 10px 6px 22px;
    font-size: 11px;
    color: var(--fg-muted);
    border-left: 2px solid var(--accent);
    border-bottom: 1px solid var(--border);
  }

  /* #733: the stream is the scene, not a card dropped on it -- no
     border, no corner radius, and the ground is the page's own
     (var(--bg)), not the elevated panel tint. The shared deck padding
     (Deck.svelte) already runs this flush to the scene's margins, so
     nothing here needs its own inset.

     No `overflow` here, deliberately, matching MetricsTable.svelte's
     own .table-wrap comment: .body below is the real, intentional
     scroll container (it needs its own scrollbar for the 1622px of
     fixed columns, #729), and .header-cell's `position: sticky` holds
     against .body's scrollport regardless of what this wrapper does --
     but giving this wrapper any overflow other than visible has no
     upside now that there's no border-radius left to clip, and it's
     one fewer ancestor to reason about if sticky ever moves. */
  .table-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg);
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
    /* For the boundary hairline below, which hangs on this cell's own
       right edge. */
    /* Opaque so rows scrolling underneath don't show through, but the
       scene's own ground (#733) now that .table-wrap carries no
       separate panel tint for this to stand apart from. */
    background: var(--bg);
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

  /* Round 36's column boundary, ported from the drawing's own
     `table.stream th::after`: nothing at rest, and under the hand a
     hairline on the header's edge with the cursor saying what it does.
     Drawn from the header cell rather than only from the drag handle so
     hovering anywhere in a column shows that column's edge -- the
     boundary should be findable without first landing on the six pixels
     it occupies.

     Inset to `right: 0` rather than the drawing's `-3px`: these header
     cells are opaque (they sit over scrolling rows), so a line
     overhanging into the next cell would be painted over by it. */
  .header-cell::after {
    content: '';
    position: absolute;
    right: 0;
    top: 5px;
    bottom: 5px;
    width: 6px;
    cursor: col-resize;
    border-right: 1px solid transparent;
    transition: border-color 0.15s;
  }

  .header-cell:last-child::after {
    /* No boundary past the last column: there is nothing on the other
       side of it to resize against. */
    display: none;
  }

  .header-cell:hover::after {
    border-right-color: var(--hair-2);
  }

  @media (prefers-reduced-motion: reduce) {
    .header-cell::after {
      transition: none;
    }
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
    /* #685: missing on purpose nowhere -- without it, a flex item with an
       explicit cross-size (::after's height: 60% below) does not stretch
       and falls back to flex-start (top), so the tick pinned itself to
       the header's top edge instead of centering in it. That read as an
       unexplained stroke hovering over the column label rather than a
       column-boundary divider, which is what it actually is: the drag
       handle for this column's resize. */
    align-items: center;
  }

  /* Nothing at rest (round 36). The handle used to draw a permanent
     tick so it could be found without hovering; the drawn answer to
     that is the header cell's own hover hairline above, which covers
     the whole column rather than the six pixels of the edge, so the
     handle itself now only marks the boundary while the hand is
     actually on it or dragging it.

     Matching the header's hairline exactly -- same width, same ink, same
     place -- so moving from the middle of a column onto its edge is one
     continuous line, not one mark replacing another. */
  .resizer::after {
    content: '';
    width: 1px;
    height: calc(100% - 10px);
    background: transparent;
  }

  .resizer:hover::after {
    background: var(--hair-2);
  }

  .resizer.active::after {
    background: var(--accent);
  }

  .empty {
    padding: 48px 16px;
    text-align: center;
    color: var(--fg-dim);
    font-size: 15px;
  }
</style>
