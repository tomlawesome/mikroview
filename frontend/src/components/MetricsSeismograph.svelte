<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The seismograph: metrics' default view (#488,
  // docs/design/screens/metrics/DESIGN.md).
  //
  // The drum (#634 round-13 verdict, owner: "let's move on with the
  // seismograph as it is in 13" -- one mirrored stroke per minute,
  // refused traffic as the inner ink, in the deck's clothes). This
  // supersedes the per-action horizon lanes this file used to draw;
  // per-action detail now lives only in the register and the table --
  // the cursor's aria-valuetext (Metrics.svelte) still carries every
  // action's own figure for a screen-reader user regardless of which
  // view is on screen.
  //
  // Flags carry the same way: no round of the ratified deck
  // (docs/design/concepts/round-{13,20..29}) draws a per-flag-type row
  // under the drum -- flag detail lives in the register's narrow flag
  // columns, the table's flag-episodes column, and the hourline
  // cursor's own "N flag episodes" fact (#644). This file used to keep
  // a FLAG EPISODES panel here from the pre-#644 build; removed rather
  // than restyled, since the ratified drum has none.
  //
  // Hand-rolled SVG, like every other chart this project has ever
  // shipped -- AGENTS.md's dependency rules make a charting library a
  // licence question before it is a size one, and nothing here needs
  // one. The SVG is sized in real CSS pixels from the measured box (see
  // lib/pixelGrid.svelte.ts) rather than stretched from a viewBox, which
  // is what the record's "sharp" clause asks for.
  import { scaleFor, type MetricsHour } from '../lib/metricsSeries'
  import { dprState, snapFill, snapLine } from '../lib/pixelGrid.svelte'
  import { formatHM } from '../lib/format'

  let { hour, cursor, onselect }: { hour: MetricsHour; cursor: number; onselect: (index: number) => void } = $props()

  // The floor that keeps a near-silent minute a visible mark rather than
  // a gap in the paper -- one stroke per minute means every minute draws
  // something, the same "honest thread" rule the record puts on a
  // series that whispered all hour.
  const MIN_HALF = 3
  const TOP = 34
  const BOTTOM = 26
  // Left and right plot margins are equal now that no flag column reads
  // off the left gutter (the ratified drum's own margins, round 20-29,
  // are symmetric too) -- just enough for the oldest tick's time label.
  const LEFT = 30
  // Wide enough for the foot's 'the brink' label, which is centred on
  // the brink edge itself and so needs half its own width of margin
  // beyond it (round 30 leaves the same room: x=1360 in a 1400 box).
  const RIGHT = 30
  const MIN_WIDTH = 420
  // This floor keeps a cramped viewport at a readable height rather than
  // squashing the trace unreadably thin.
  const MIN_HEIGHT = 320
  // #716: measuring the container's full height and filling it (as this
  // used to) overcorrected -- the owner's review called the result
  // "comically oversized". Round 30's own mockup (metrics-seismograph.png)
  // draws the trace as a band through the vertical middle of the scene,
  // width:height roughly 3:1, with generous room above and below it, not
  // stretched to every pixel the card happens to have. This ceiling keeps
  // the drum at that band's proportions even inside a tall card.
  const ASPECT = 3.05
  const MAX_HEIGHT = 620

  // Measured, not assumed: the drum's width still fills whatever the
  // content column gives it after the rail's own state; its height no
  // longer does (see ASPECT above) -- boxHeight now only clamps the ideal
  // height down for a container shorter than the band would otherwise be.
  //
  // Measured by our own ResizeObserver rather than by
  // `bind:clientWidth`/`bind:clientHeight` on the drum, which is what
  // this used to be. Svelte's size binding (bind_element_size) reads
  // `element.clientWidth` inside an effect as well as inside the
  // observer, and this element is the one the whole hour of strokes is
  // drawn into -- so every render pass forced a synchronous layout of
  // the entire chart to answer it. #690's 2026-09-09 profile put that
  // binding's runtime at 23% of self-time on a roll to metrics, the
  // deck with by far the highest LayoutDuration (403ms median against
  // under 90ms everywhere else). An observer entry's contentRect is
  // reported by the observation itself, so reading it forces no layout;
  // the drum carries no border or padding, so that box is the same box
  // clientWidth/clientHeight reported, rounded the same way. Same
  // numbers, same drum -- measured once per actual resize instead of
  // once per render.
  let drumEl = $state<HTMLDivElement | null>(null)
  let boxWidth = $state(0)
  let boxHeight = $state(0)

  // One measurement at mount, then the observer's own boxes. The mount
  // read is the one place clientWidth/clientHeight are still asked for:
  // the drum has to be drawn at its real width on the first frame, not
  // at MIN_WIDTH until the first observation lands. That is one forced
  // layout per mount, against one per render before.
  //
  // jsdom has no ResizeObserver (lib/cardAnchor.ts guards the same way):
  // nothing is watched, the box stays 0, and the drum draws at its own
  // minimum -- which is exactly what the binding did there too.
  $effect(() => {
    const el = drumEl
    if (!el) return
    boxWidth = el.clientWidth
    boxHeight = el.clientHeight
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const box = entries[entries.length - 1]?.contentRect
      if (!box) return
      boxWidth = Math.round(box.width)
      boxHeight = Math.round(box.height)
    })
    ro.observe(el)
    return () => ro.disconnect()
  })

  const width = $derived(Math.max(MIN_WIDTH, boxWidth || MIN_WIDTH))
  const idealHeight = $derived(width / ASPECT)
  const height = $derived(
    Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, Math.min(idealHeight, boxHeight || idealHeight))),
  )
  const dpr = $derived(dprState.value)

  const n = $derived(hour.axis.length)
  const plotX0 = $derived(LEFT)
  const plotX1 = $derived(width - RIGHT)

  // Half the drum's own drawable band: how far the outer stroke reaches
  // above/below the midline for a minute at the hour's own scale. Now
  // grows with the measured height, so the trace fills the scene
  // instead of stopping at a fixed band.
  const drumHalf = $derived((height - TOP - BOTTOM) / 2)
  const midlineY = $derived(TOP + drumHalf)

  // One shared scale for both halves of every stroke: refused traffic is
  // always a subset of the minute's total, so the inner half has to read
  // against the same ceiling the outer half does, never a scale of its
  // own -- otherwise a quiet minute's refused sliver could draw taller
  // than its own total.
  const totals = $derived(
    n === 0 ? [] : Array.from({ length: n }, (_, i) => hour.traffic.reduce((a, s) => a + s.values[i], 0)),
  )
  const refused = $derived(
    n === 0
      ? []
      : Array.from({ length: n }, (_, i) =>
          hour.traffic.filter((s) => s.ink === 'refused').reduce((a, s) => a + s.values[i], 0),
        ),
  )
  const scale = $derived(scaleFor(totals))

  function halfOf(value: number): number {
    return Math.max(MIN_HALF, (value / scale) * drumHalf)
  }

  function xOf(i: number): number {
    if (n <= 1) return plotX1
    return plotX0 + (i / (n - 1)) * (plotX1 - plotX0)
  }

  // The hour is placed at its two ends and nowhere else, as every
  // ratified round of the drum draws it (round 30's `#mv-seis`, carried
  // unchanged through rounds 36-39): the oldest minute written at the
  // bottom-left, the words "the brink" under the brink's own edge at
  // the bottom-right. This used to be a label every tenth minute along
  // the top with a full-height rule dropped under each one -- a ruled
  // grid the mockup draws nowhere, and the same fault the fall carried
  // until #700 ("round 30 draws no grid at all").
  const oldestLabel = $derived(n === 0 ? '' : formatHM(hour.axis[0]))

  const cursorX = $derived(cursor >= 0 ? snapLine(xOf(cursor), dpr) : 0)

  function selectFromPointer(event: PointerEvent) {
    if (n === 0) return
    const target = event.currentTarget as SVGSVGElement
    const rect = target.getBoundingClientRect()
    const pxScale = rect.width === 0 ? 1 : width / rect.width
    const x = (event.clientX - rect.left) * pxScale
    const frac = (x - plotX0) / Math.max(1, plotX1 - plotX0)
    onselect(Math.min(n - 1, Math.max(0, Math.round(frac * (n - 1)))))
  }

  const label = $derived(
    `Seismograph: one stroke per minute, mirrored about the midline, for the hour to ` +
      `${hour.brink ? formatHM(hour.brink) : 'now'} -- the outer half every event, the inner half refused ` +
      `traffic, newest at the right`,
  )
</script>

<div class="drum" bind:this={drumEl}>
  {#if n === 0}
    <p class="empty">No minutes recorded yet — the drum starts as soon as events arrive.</p>
  {:else}
    <svg
      {width}
      {height}
      viewBox="0 0 {width} {height}"
      role="img"
      aria-label={label}
      onpointerdown={selectFromPointer}
    >
      <!-- the hour, written at its two ends and nowhere between -->
      <text class="time" x={plotX0} y={height - BOTTOM + 18}>{oldestLabel}</text>
      <text class="time" x={snapFill(plotX1, dpr)} y={height - BOTTOM + 18} text-anchor="middle">the brink</text>

      <line class="midline" x1={plotX0} x2={plotX1} y1={snapLine(midlineY, dpr)} y2={snapLine(midlineY, dpr)} />

      <!-- one mirrored stroke per minute: the outer half every event
           that minute, the inner half its refused share, both centred
           on the shared midline -->
      {#each hour.axis as axisTime, i (axisTime)}
        {@const x = snapFill(xOf(i), dpr)}
        {@const outer = halfOf(totals[i])}
        {@const inner = halfOf(refused[i])}
        <line class="stroke outer" x1={x} x2={x} y1={midlineY - outer} y2={midlineY + outer} />
        <line class="stroke inner" x1={x} x2={x} y1={midlineY - inner} y2={midlineY + inner} />
      {/each}

      <!-- amber is time: the brink edge the paper feeds from -->
      <line class="brink-glow" x1={snapLine(plotX1, dpr)} x2={snapLine(plotX1, dpr)} y1={TOP - 6} y2={height - BOTTOM} />
      <line class="brink-edge" x1={snapLine(plotX1, dpr)} x2={snapLine(plotX1, dpr)} y1={TOP - 6} y2={height - BOTTOM} />

      <!-- amber is time: and the cursor, which lifts the whole minute at once -->
      {#if cursor >= 0}
        <rect class="cursor-band" x={cursorX - 4} y={TOP - 6} width="8" height={height - BOTTOM - TOP + 6} />
        <line class="cursor" x1={cursorX} x2={cursorX} y1={TOP - 6} y2={height - BOTTOM} />
        <!-- above its own line, as the drawing puts it: the cursor's
             minute is the one amber word at the top of the paper, clear
             of the two dim end-labels along the foot. -->
        <text class="time brink cursor-label" x={cursorX} y={TOP - 16} text-anchor="middle"
          >{formatHM(hour.axis[cursor])}</text
        >
      {/if}
    </svg>
  {/if}
</div>

<style>
  .drum {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    overflow: auto;
  }

  .drum svg {
    display: block;
    cursor: crosshair;
    touch-action: pan-y;
  }

  .empty {
    padding: 40px 0;
    text-align: center;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .midline {
    stroke: var(--border);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  .stroke {
    stroke-linecap: round;
    stroke-width: 4;
  }

  .stroke.outer {
    stroke: var(--chart-traffic);
    opacity: 0.55;
  }

  .stroke.inner {
    stroke: var(--chart-refused);
  }

  .time {
    fill: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 9.5px;
  }

  .time.brink {
    fill: var(--now);
  }

  /* Named so a live-check can find the cursor's own minute label rather
     than guessing at DOM order among the axis labels. */
  .time.cursor-label {
    font-weight: 700;
  }

  .brink-edge {
    stroke: var(--now);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  /* The paper's breath -- the record allows this and the arrival, and
     nothing else, and both become instant under reduced motion. */
  .brink-glow {
    stroke: var(--now);
    stroke-width: 3;
    opacity: 0.16;
    animation: breathe 4s ease-in-out infinite;
  }

  @keyframes breathe {
    0%,
    100% {
      opacity: 0.16;
    }
    50% {
      opacity: 0.05;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .brink-glow {
      animation: none;
    }
  }

  .cursor {
    stroke: var(--now);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  .cursor-band {
    fill: var(--now);
    opacity: 0.09;
  }
</style>
