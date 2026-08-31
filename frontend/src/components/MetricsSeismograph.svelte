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
  const RIGHT = 18
  const MIN_WIDTH = 420
  // Round 30's drum is full-bleed both ways (`.drumwrap { position:
  // absolute; inset: 108px 24px 30px }`, `svg { width: 100%; height:
  // 100% }`) -- it fills whatever vertical room the scene gives it
  // rather than the fixed short band this used to draw. This floor
  // only keeps a cramped viewport at that previous height rather than
  // squashing the trace unreadably thin.
  const MIN_HEIGHT = 320

  // Measured, not assumed: the drum is full-bleed, so its width and
  // height are whatever the content column gives it after the rail's
  // own state.
  let boxWidth = $state(0)
  let boxHeight = $state(0)

  const width = $derived(Math.max(MIN_WIDTH, boxWidth || MIN_WIDTH))
  const height = $derived(Math.max(MIN_HEIGHT, boxHeight || MIN_HEIGHT))
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

  // Every tenth minute, plus the brink -- enough to place the hour
  // without a ruled grid competing with the strokes.
  const timeTicks = $derived(
    n === 0 ? [] : Array.from({ length: n }, (_, i) => i).filter((i) => (n - 1 - i) % 10 === 0),
  )

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

<div class="drum" bind:clientWidth={boxWidth} bind:clientHeight={boxHeight}>
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
      <!-- the hour, placed once at the top -->
      {#each timeTicks as i (i)}
        <text
          class="time"
          class:brink={i === n - 1}
          x={snapFill(xOf(i), dpr)}
          y={TOP - 14}
          text-anchor={i === n - 1 ? 'end' : 'middle'}>{formatHM(hour.axis[i])}</text
        >
        <line
          class="decade"
          x1={snapLine(xOf(i), dpr)}
          x2={snapLine(xOf(i), dpr)}
          y1={TOP - 6}
          y2={height - BOTTOM + 4}
        />
      {/each}

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
        <text class="time brink cursor-label" x={cursorX} y={height - BOTTOM + 18} text-anchor="middle"
          >{formatHM(hour.axis[cursor])}</text
        >
      {/if}
    </svg>
  {/if}
</div>

<style>
  .drum {
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

  .decade {
    stroke: var(--border);
    stroke-width: 1;
    opacity: 0.45;
    shape-rendering: crispEdges;
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
