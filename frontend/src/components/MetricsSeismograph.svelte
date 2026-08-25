<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The seismograph: metrics' default view (#488,
  // docs/design/screens/metrics/DESIGN.md).
  //
  // "One full-bleed drum, no tiles, no cards, no boxes. Each series is a
  // horizon strip (~56px): its scale folds into three opacity bands of
  // its one ink, deepest band carrying the shape. Time runs left-right;
  // the amber brink is the right edge where the paper feeds."
  //
  // Hand-rolled SVG, like every other chart this project has ever
  // shipped -- AGENTS.md's dependency rules make a charting library a
  // licence question before it is a size one, and nothing here needs
  // one. The SVG is sized in real CSS pixels from the measured box (see
  // lib/pixelGrid.svelte.ts) rather than stretched from a viewBox, which
  // is what the record's "sharp" clause asks for.
  //
  // Flags share this surface rather than sitting behind a second
  // control: the record puts them in "the traffic coordinate space" and
  // asks the cursor to read "a whole minute at once across every
  // series", and neither is true of two separate charts. Episode ticks
  // wear the traffic ink -- the refused ink stays reserved for the
  // drop/reject series the record names as its meaning, and an episode
  // carries no verdict of its own.
  import type { MetricsHour } from '../lib/metricsSeries'
  import { dprState, snapFill, snapLine } from '../lib/pixelGrid.svelte'
  import { formatHM } from '../lib/format'

  let { hour, cursor, onselect }: { hour: MetricsHour; cursor: number; onselect: (index: number) => void } = $props()

  const STRIP_H = 56
  const STRIP_GAP = 6
  const FLAG_ROW_H = 26
  const GROUP_GAP = 22
  // The REFUSED bracket is printed in the gap above its first strip, and
  // the strip above it prints its own declared scale on its last line --
  // 6px of ordinary strip gap puts one on top of the other.
  const GROUP_SPLIT = 20
  const TOP = 30
  const BOTTOM = 26
  const RIGHT = 18
  const MIN_WIDTH = 420

  // Measured, not assumed: the drum is full-bleed, so its width is
  // whatever the content column gives it after the rail's own state.
  let boxWidth = $state(0)

  const gutter = $derived(boxWidth < 620 ? 112 : 132)
  const width = $derived(Math.max(MIN_WIDTH, boxWidth || MIN_WIDTH))
  const dpr = $derived(dprState.value)

  const n = $derived(hour.axis.length)
  const plotX0 = $derived(gutter)
  const plotX1 = $derived(width - RIGHT)

  const trafficTop = $derived(TOP)
  const trafficHeight = $derived(hour.traffic.length * (STRIP_H + STRIP_GAP) - STRIP_GAP + GROUP_SPLIT)
  const flagsTop = $derived(trafficTop + trafficHeight + GROUP_GAP + 16)
  const height = $derived(flagsTop + hour.flags.length * FLAG_ROW_H + BOTTOM)

  // The first refused series: where the REFUSED group bracket starts.
  const refusedFrom = $derived(hour.traffic.findIndex((s) => s.ink === 'refused'))

  function xOf(i: number): number {
    if (n <= 1) return plotX1
    return plotX0 + (i / (n - 1)) * (plotX1 - plotX0)
  }

  function stripY(i: number): number {
    return trafficTop + i * (STRIP_H + STRIP_GAP) + (refusedFrom >= 0 && i >= refusedFrom ? GROUP_SPLIT : 0)
  }

  function flagY(i: number): number {
    return flagsTop + i * FLAG_ROW_H + FLAG_ROW_H / 2
  }

  // One horizon band: the part of each minute's value that falls inside
  // this third of the series' own declared scale, as a filled area from
  // the strip's baseline. Three of these stacked is the fold -- the
  // deepest band only appears where the hour ran near its own ceiling.
  function bandPath(values: number[], scale: number, band: number, y0: number): string {
    if (n === 0) return ''
    const slice = scale / 3
    const lo = band * slice
    const base = snapFill(y0 + STRIP_H, dpr)
    let d = `M ${snapFill(xOf(0), dpr)},${base}`
    for (let i = 0; i < n; i++) {
      const frac = Math.max(0, Math.min(1, (values[i] - lo) / slice))
      d += ` L ${snapFill(xOf(i), dpr)},${snapFill(y0 + STRIP_H - frac * STRIP_H, dpr)}`
    }
    d += ` L ${snapFill(xOf(n - 1), dpr)},${base} Z`
    return d
  }

  // Every tenth minute, plus the brink -- enough to place the hour
  // without a ruled grid competing with the traces.
  const timeTicks = $derived(
    n === 0 ? [] : Array.from({ length: n }, (_, i) => i).filter((i) => (n - 1 - i) % 10 === 0),
  )

  const cursorX = $derived(cursor >= 0 ? snapLine(xOf(cursor), dpr) : 0)
  // A reading printed to the right of the cursor runs off the paper near
  // the brink, so it flips to the other side rather than being clipped.
  const readingFlipped = $derived(cursorX > plotX1 - 78)

  function selectFromPointer(event: PointerEvent) {
    if (n === 0) return
    const target = event.currentTarget as SVGSVGElement
    const rect = target.getBoundingClientRect()
    const scale = rect.width === 0 ? 1 : width / rect.width
    const x = (event.clientX - rect.left) * scale
    const frac = (x - plotX0) / Math.max(1, plotX1 - plotX0)
    onselect(Math.min(n - 1, Math.max(0, Math.round(frac * (n - 1)))))
  }

  const label = $derived(
    `Seismograph: ${hour.traffic.length} traffic series and ${hour.flags.length} flag types for the hour to ` +
      `${hour.brink ? formatHM(hour.brink) : 'now'}, horizon strips on one shared time axis, newest at the right`,
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
      <!-- the hour, placed once at the top and once under the flags -->
      {#each timeTicks as i (i)}
        <text
          class="time"
          class:brink={i === n - 1}
          x={snapFill(xOf(i), dpr)}
          y={TOP - 12}
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

      <!-- traffic: one horizon strip per action, each on its own scale -->
      {#each hour.traffic as series, i (series.key)}
        {@const y0 = stripY(i)}
        {#if i === 0 || i === refusedFrom}
          <text class="group" x="8" y={y0 - 8}>{i === 0 ? 'TRAFFIC' : 'REFUSED'}</text>
        {/if}
        <line
          class="baseline"
          x1={plotX0}
          x2={plotX1}
          y1={snapLine(y0 + STRIP_H, dpr)}
          y2={snapLine(y0 + STRIP_H, dpr)}
        />
        {#each [0, 1, 2] as band (band)}
          <path class="band band-{band} ink-{series.ink}" d={bandPath(series.values, series.scale, band, y0)} />
        {/each}
        <text class="s-name" x="8" y={y0 + 20}>{series.label}</text>
        <text class="s-now ink-text-{series.ink}" x="8" y={y0 + 38}
          >{series.now}<tspan class="s-unit"> /min</tspan></text
        >
        <text class="s-scale" x="8" y={y0 + 51}>scale {series.scale}</text>
        {#if cursor >= 0}
          <text
            class="reading ink-text-{series.ink}"
            x={readingFlipped ? cursorX - 8 : cursorX + 8}
            y={y0 + 14}
            text-anchor={readingFlipped ? 'end' : 'start'}>{series.values[cursor]}</text
          >
        {/if}
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

      <!-- amber is time: and the cursor, which lifts every needle at once -->
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

  .band {
    stroke: none;
  }

  .band-0 {
    opacity: 0.22;
  }

  .band-1 {
    opacity: 0.5;
  }

  .band-2 {
    opacity: 0.85;
  }

  .ink-traffic {
    fill: var(--chart-traffic);
  }

  .ink-refused {
    fill: var(--chart-refused);
  }

  .ink-text-traffic {
    fill: var(--fg);
  }

  .ink-text-refused {
    fill: var(--chart-refused);
  }

  .baseline {
    stroke: var(--border);
    stroke-width: 1;
    shape-rendering: crispEdges;
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

  .s-name {
    fill: var(--fg-muted);
    font-size: 11px;
    font-weight: 600;
  }

  .s-now {
    font-family: var(--font-mono);
    font-size: 14px;
    font-weight: 600;
  }

  .s-unit,
  .s-scale {
    fill: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 8.5px;
    font-weight: 400;
  }

  .reading {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
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
