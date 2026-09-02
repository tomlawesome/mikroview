<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The event buffer's size, as a control (#796). Round 39 draws it in
  // Settings' memory group (docs/design/concepts/round-39/the-whole.html,
  // `#set`): a track from 32 MiB to what this host can spare, on a
  // doubling scale, with the figure riding above the handle.
  //
  // One component rather than two copies, because #796 asks the setup
  // wizard to offer "the same control once, with the same sentence" --
  // and a slider that spends the host's memory and can destroy held
  // history is exactly the thing that must not quietly differ between
  // the two places it appears.
  //
  // Dragging only proposes. Nothing changes until apply, which is the
  // shape the owner asked about before ratifying round 39: a shrink
  // discards events, so a slipped mouse must not be able to spend them.
  //
  // A caller that also draws the hours bar (Settings does; the wizard
  // does not) binds `proposal` to learn where the shrink's cut falls,
  // so the bar and this sentence describe one proposal rather than two
  // computations of it.
  import { setStoreMaxMemory } from '../lib/api'
  import {
    TRACK_X0,
    TRACK_X1,
    bytesAtX,
    ceilingCaption,
    describeProposal,
    doublingTicks,
    formatSize,
    midLabel,
    pageStepBytes,
    stepBytes,
    trackX,
    type Proposal,
  } from '../lib/memory'
  import type { StoreMemory, Stats } from '../lib/types'

  let {
    mem,
    stats,
    canEdit,
    proposal = $bindable(null),
    onapplied,
  }: {
    mem: StoreMemory
    stats: Stats
    /** Whether this caller may move the handle -- admin only. */
    canEdit: boolean
    proposal?: Proposal | null
    /** Called after the server has accepted a new figure. */
    onapplied?: () => void
  } = $props()

  // null means no proposal is open and the handle sits on the figure in
  // effect; a number is what has been dragged to but not applied.
  let proposed = $state<number | null>(null)
  let applying = $state(false)
  let error = $state<string | null>(null)
  let dragging = $state(false)

  const shown = $derived(proposed ?? mem.maxMemory)

  const live = $derived.by(() => {
    if (proposed === null || proposed === mem.maxMemory) return null
    return describeProposal({
      proposed,
      current: mem.maxMemory,
      bytesPerEvent: mem.bytesPerEvent,
      eventsPerSecond: stats.eventsPerSecond,
      count: stats.count,
      now: Date.now(),
    })
  })

  // Published to the caller so a hours bar drawn beside this can mark
  // the same cut this sentence describes.
  $effect(() => {
    proposal = live
  })

  const ticks = $derived(doublingTicks(mem.min, mem.max))
  const mid = $derived(midLabel(mem.min, mem.max))
  const handleX = $derived(trackX(shown, mem.min, mem.max))
  const ghostX = $derived(trackX(mem.maxMemory, mem.min, mem.max))

  function proposeFromPointer(event: PointerEvent, svg: SVGSVGElement) {
    const box = svg.getBoundingClientRect()
    if (box.width <= 0) return
    // The viewBox is 520 units wide whatever the element's pixel width
    // turns out to be, so the pointer is mapped back into those units
    // before the scale reads it.
    proposed = bytesAtX(((event.clientX - box.left) / box.width) * 520, mem.min, mem.max)
    error = null
  }

  function onPointerDown(event: PointerEvent) {
    const svg = event.currentTarget as SVGSVGElement
    svg.setPointerCapture(event.pointerId)
    dragging = true
    proposeFromPointer(event, svg)
  }

  function onPointerMove(event: PointerEvent) {
    if (!dragging) return
    proposeFromPointer(event, event.currentTarget as SVGSVGElement)
  }

  function onPointerUp(event: PointerEvent) {
    const svg = event.currentTarget as SVGSVGElement
    if (svg.hasPointerCapture(event.pointerId)) svg.releasePointerCapture(event.pointerId)
    dragging = false
  }

  function onKeyDown(event: KeyboardEvent) {
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        proposed = stepBytes(shown, -1, mem.min, mem.max)
        break
      case 'ArrowRight':
      case 'ArrowUp':
        proposed = stepBytes(shown, 1, mem.min, mem.max)
        break
      case 'PageUp':
        proposed = pageStepBytes(shown, 1, mem.min, mem.max)
        break
      case 'PageDown':
        proposed = pageStepBytes(shown, -1, mem.min, mem.max)
        break
      case 'Home':
        proposed = mem.min
        break
      case 'End':
        proposed = mem.max
        break
      case 'Enter':
        if (live) apply()
        break
      case 'Escape':
        keep()
        break
      default:
        return
    }
    event.preventDefault()
    error = null
  }

  async function apply() {
    if (proposed === null || applying) return
    applying = true
    error = null
    const result = await setStoreMaxMemory(proposed)
    applying = false
    if (typeof result === 'string') {
      // The server's own words: which end of the range was hit, or that
      // nothing changed because the figure could not be stored. Either
      // is more use to an operator than "failed".
      error = result
      return
    }
    proposed = null
    onapplied?.()
  }

  function keep() {
    proposed = null
    error = null
  }
</script>

<!-- An admin drags it; everyone else reads it. Deliberately no lock icon
     and no disabled styling for the reader (#796): the same bar and the
     same figure, simply without the drag on offer -- the absent-not-
     disabled grammar the rest of Settings already follows. -->
{#snippet track()}
  <line x1={TRACK_X0} y1="24" x2={TRACK_X1} y2="24" class="mrail" />
  {#each ticks as tick (tick)}
    <line
      x1={trackX(tick, mem.min, mem.max)}
      y1="27"
      x2={trackX(tick, mem.min, mem.max)}
      y2="31"
      class="mrail"
    />
  {/each}
  <line x1={TRACK_X0} y1="24" x2={handleX} y2="24" class="mfill" />
  <!-- Where the handle was before the drag, so what is being given up
       is on screen beside what would be gained. -->
  {#if live}
    <circle class="mghost" cx={ghostX} cy="24" r="4.5" />
  {/if}
  <circle class="mh" cx={handleX} cy="24" r="6.5" />
  <text x={handleX} y="11" text-anchor="middle" class="sp-k">{formatSize(shown)}</text>
  <text x={TRACK_X0} y="46" class="sp-n">{formatSize(mem.min)}</text>
  {#if mid !== null}
    <text x={trackX(mid, mem.min, mem.max)} y="46" text-anchor="middle" class="sp-n quiet"
      >{formatSize(mid)}</text
    >
  {/if}
  <text x={TRACK_X1} y="46" text-anchor="end" class="sp-n">{ceilingCaption(mem.max, mem.hostTotal)}</text>
{/snippet}

{#if canEdit}
  <svg
    class="stmemctl"
    viewBox="0 0 520 54"
    role="slider"
    tabindex="0"
    aria-label="Event buffer size"
    aria-valuemin={mem.min}
    aria-valuemax={mem.max}
    aria-valuenow={shown}
    aria-valuetext="{formatSize(shown)} of the {formatSize(mem.max)} this host can spare"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onkeydown={onKeyDown}
  >
    {@render track()}
  </svg>
{:else}
  <svg
    class="stmemctl still"
    viewBox="0 0 520 54"
    role="img"
    aria-label="The event buffer is set to {formatSize(shown)}, of the {formatSize(
      mem.max,
    )} this host can spare"
  >
    {@render track()}
  </svg>
{/if}

{#if live}
  <p class="oghint memnote">
    {live.sentence} —
    <button class="olink" disabled={applying} onclick={apply}>{applying ? 'applying…' : 'apply'}</button>
    ·
    <button class="olink" disabled={applying} onclick={keep}>keep {formatSize(mem.maxMemory)}</button>
  </p>
{/if}
{#if error}
  <p class="oghint err" role="alert">{error}</p>
{/if}

<style>
  /* Ported from docs/design/concepts/round-39/the-whole.html's
     `.stmemctl`. The mockup's tokens land on this app's own custom
     properties the same way the rest of Settings already does
     (--hair-2 -> --border, --raised -> --bg, --ink-3 -> --fg-dim,
     --ink-2 -> --fg-muted). */
  .stmemctl {
    width: 100%;
    height: auto;
    display: block;
    margin: 10px 0 0;
  }

  .stmemctl:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .mrail {
    stroke: var(--border);
    stroke-width: 2;
    stroke-linecap: round;
  }

  .mfill {
    stroke: var(--accent);
    stroke-width: 2;
    stroke-linecap: round;
    opacity: 0.55;
  }

  .mh {
    fill: var(--bg);
    stroke: var(--accent);
    stroke-width: 1.6;
    cursor: grab;
  }

  /* A reader's copy: the handle is drawn, because they can see what the
     buffer is set to, but it is not something to take hold of. */
  .stmemctl.still .mh {
    cursor: default;
  }

  .mghost {
    fill: none;
    stroke: var(--fg-dim);
    stroke-dasharray: 2 2;
  }

  .sp-n {
    font-family: var(--font-mono);
    font-size: 9.5px;
    fill: var(--fg-dim);
  }

  .sp-n.quiet {
    opacity: 0.75;
  }

  .sp-k {
    font-family: var(--font-mono);
    font-size: 10px;
    fill: var(--fg-muted);
  }

  .oghint {
    font-size: 11px;
    color: var(--fg-dim);
    margin: 2px 0 8px;
  }

  .memnote {
    color: var(--fg-muted);
    margin-top: 2px;
  }

  .oghint.err {
    color: var(--reject);
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover {
    text-decoration-color: currentColor;
  }

  .olink:disabled {
    cursor: default;
    opacity: 0.6;
  }
</style>
