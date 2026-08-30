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

  // Half the drum's own drawable band: how far the outer stroke reaches
  // above/below the midline for a minute at the hour's own scale.
  const DRUM_HALF = 130
  // The floor that keeps a near-silent minute a visible mark rather than
  // a gap in the paper -- one stroke per minute means every minute draws
  // something, the same "honest thread" rule the record puts on a
  // series that whispered all hour.
  const MIN_HALF = 3
  const FLAG_ROW_H = 26
  const GROUP_GAP = 22
  const TOP = 34
  const BOTTOM = 26
  const RIGHT = 18
  const MIN_WIDTH = 420

  // Measured, not assumed: the drum is full-bleed, so its width is
  // whatever the content column gives it after the rail's own state.
  let boxWidth = $state(0)

  // Only the flag column needs a left gutter now that per-action strips
  // are gone -- the same widths the lane build used for its own flag
  // names/totals, unchanged by the rewrite.
  const gutter = $derived(boxWidth < 620 ? 112 : 132)
  const width = $derived(Math.max(MIN_WIDTH, boxWidth || MIN_WIDTH))
  const dpr = $derived(dprState.value)

  const n = $derived(hour.axis.length)
  const plotX0 = $derived(gutter)
  const plotX1 = $derived(width - RIGHT)

  const midlineY = TOP + DRUM_HALF
  const drumBottom = TOP + DRUM_HALF * 2
  const flagsTop = $derived(drumBottom + GROUP_GAP + 16)
  const height = $derived(flagsTop + hour.flags.length * FLAG_ROW_H + BOTTOM)

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
    return Math.max(MIN_HALF, (value / scale) * DRUM_HALF)
  }

  function xOf(i: number): number {
    if (n <= 1) return plotX1
    return plotX0 + (i / (n - 1)) * (plotX1 - plotX0)
  }

  function flagY(i: number): number {
    return flagsTop + i * FLAG_ROW_H + FLAG_ROW_H / 2
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
      `traffic, ${hour.flags.length} flag types below, newest at the right`,
  )
</script>

<div class="drum" bind:clientWidth={boxWidth}>
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

      <!-- flags: episodes are ticks, silence is a labelled hairline -->
      <text class="group" x="8" y={flagsTop - 14}>FLAG EPISODES</text>
      {#each hour.flags as series, i (series.key)}
        {@const y = flagY(i)}
        <line
          class="hairline"
          class:quiet={!series.spoke}
          x1={plotX0}
          x2={plotX1}
          y1={snapLine(y, dpr)}
          y2={snapLine(y, dpr)}
        />
        <text class="f-name" class:quiet={!series.spoke} x="8" y={y + 3.5}>{series.short}</text>
        <text class="f-total" class:quiet={!series.spoke} x={gutter - 10} y={y + 3.5} text-anchor="end"
          >{series.total}</text
        >
        {#each series.values as v, mi (mi)}
          {#if v > 0}
            <line class="tick" x1={snapLine(xOf(mi), dpr)} x2={snapLine(xOf(mi), dpr)} y1={y - 8} y2={y + 8} />
            {#if v > 1}
              <text class="tick-n" x={snapFill(xOf(mi), dpr) + 5} y={y - 6}>×{v}</text>
            {/if}
          {/if}
        {/each}
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
    min-width: 0;
    overflow-x: auto;
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

  /* "The silent types keep a labelled hairline each -- running and
     quiet, visibly." --border is too close to the page on the dark
     theme for that to be true, so a flag row's rule is drawn from the
     text ramp instead, washed rather than faded out of existence. */
  .hairline {
    stroke: var(--fg-dim);
    stroke-width: 1;
    opacity: 0.55;
    shape-rendering: crispEdges;
  }

  .hairline.quiet {
    opacity: 0.3;
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

  .group {
    fill: var(--fg-dim);
    font-size: 9px;
    font-weight: 650;
    letter-spacing: 0.16em;
  }

  .f-name {
    fill: var(--fg-muted);
    font-size: 10px;
  }

  .f-name.quiet,
  .f-total.quiet {
    fill: var(--fg-dim);
    opacity: 0.75;
  }

  .f-total {
    fill: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 10px;
  }

  .tick {
    stroke: var(--chart-traffic);
    stroke-width: 3;
    stroke-linecap: round;
  }

  .tick-n {
    fill: var(--fg-dim);
    font-size: 9px;
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
