<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The register: metrics' second ratified view (#488,
  // docs/design/screens/metrics/DESIGN.md).
  //
  // "The hour read downward, the way the app already reads: the brink is
  // the top edge (#363's newest-first), minutes are shared rows, and
  // every series is a vertical ribbon with a calligraphic smoothed edge
  // -- one instant is one straight line across the page."
  //
  // Same data, same cursor and the same per-series scale as the
  // seismograph (lib/metricsSeries.ts owns all three); what differs is
  // the orientation and the ledger strip below, which carries the
  // magnitude answers the old cards used to hold.
  //
  // #634 round 21/22 verdicts: "the register needs to take up far more
  // of the available screen space" -- ribbons spread across the frame
  // and fill it, rather than sitting at a flat pixel width regardless of
  // the card's own size. The column width is measured from the box, not
  // assumed, the same way the drum measures its own.
  import { FLAG_TYPE_SHORT_LABELS, type MetricsHour, type MinuteReading } from '../lib/metricsSeries'
  import { dprState, snapFill, snapLine } from '../lib/pixelGrid.svelte'
  import { formatHM } from '../lib/format'
  import MetricsTotals from './MetricsTotals.svelte'

  let {
    hour,
    cursor,
    reading,
    onselect,
  }: {
    hour: MetricsHour
    cursor: number
    reading: MinuteReading | null
    onselect: (index: number) => void
  } = $props()

  const GUTTER = 66
  const ROW_H = 8
  // Floors, not the drawn size: a column never shrinks below these, so a
  // narrow card scrolls (the paper already does) rather than crushing a
  // ribbon or a rotated flag label unreadable.
  const COL_MIN = 90

  // #716 reverses 778203f: the owner ruled the flag-type labels' *rotation*
  // was never the fault -- they collided with the brink rule because the
  // anchor sat only 10px above the axis, so a rotated label's tail swept
  // down across the rule before it, further for a longer name. Restored to
  // -60deg / text-anchor="end", with the anchor point and header depth now
  // sized from the geometry instead of guessed -- checked against the
  // *longest* registered flag-type name, not whichever happens to fire this
  // hour, so a long name that only starts firing later never collides.
  const FLAG_ROTATE_DEG = 60
  // Average advance per character for the app's system-sans stack
  // (--font-sans) at .f-name's 10px size -- not measured per-glyph,
  // deliberately generous so the estimate only ever errs toward more
  // clearance, never less.
  const FLAG_LABEL_CHAR_W = 6
  const LONGEST_FLAG_LABEL = Object.values(FLAG_TYPE_SHORT_LABELS).reduce(
    (longest, name) => (name.length > longest.length ? name : longest),
    '',
  )
  const FLAG_LABEL_RAD = (FLAG_ROTATE_DEG * Math.PI) / 180
  const FLAG_LABEL_WIDTH = LONGEST_FLAG_LABEL.length * FLAG_LABEL_CHAR_W
  // How far the rotated label's tail swings below its own anchor (decides
  // the header's depth) and how far it reaches left of its own column's
  // centre (decides the flag columns' minimum width).
  const FLAG_LABEL_DROP = Math.sin(FLAG_LABEL_RAD) * FLAG_LABEL_WIDTH
  const FLAG_LABEL_REACH = Math.cos(FLAG_LABEL_RAD) * FLAG_LABEL_WIDTH
  // The anchor -- text-anchor="end", so the label's *last* character --
  // sits this far below the FLAG EPISODES group label: clear of it, high
  // enough in the header that the sweep below still clears the axis.
  const FLAG_LABEL_TOP = 34
  // The label's tail, its lowest/leftmost point once rotated, stops this
  // far above the brink/axis line rather than sweeping across it.
  const FLAG_LABEL_CLEARANCE = 8

  // Deep enough for the stacked traffic column header (group label, name,
  // value, scale) *and* for the longest rotated flag-type label's full
  // sweep to clear the axis, whichever needs more room.
  const HEADER = Math.max(118, Math.ceil(FLAG_LABEL_TOP + FLAG_LABEL_DROP + FLAG_LABEL_CLEARANCE))
  // Wide enough that one column's rotated label doesn't sweep into its
  // neighbour's -- the reach above, plus a margin.
  const FLAG_COL_MIN = Math.ceil(FLAG_LABEL_REACH) + 14
  const GROUP_GAP = 14
  const BOTTOM = 26

  const dpr = $derived(dprState.value)
  const n = $derived(hour.axis.length)

  // Measured, not assumed -- the register fills whatever the card gives
  // it (#634 round 22), the same technique the drum uses for its own
  // full-bleed width.
  let boxWidth = $state(0)

  // Round 30: only the flag types that actually fired this hour get a
  // column -- the mockup draws a column per fired type, not one for every
  // registered type (docs/design/concepts/round-30/the-whole.html #s4 draws
  // two flag columns, "unplanned" and "ring broken", not all sixteen).
  // hour.flags still carries every registered type (spoke-first, then
  // silent) so the cross-section and other views keep the full picture;
  // the register itself just doesn't draw a column for a silent one.
  const firedFlags = $derived(hour.flags.filter((s) => s.spoke))

  // Off for round-30 fidelity: the ledger strip below is unmounted, not
  // deleted (#700, #691). Typed rather than inferred so the block stays
  // reachable to the type checker.
  const LEDGER_ENABLED: boolean = false

  // Same treatment for the cross-section panel. Round 30's register draws
  // no side panel: the minute under the cursor is read from the scene's
  // own header line, not from an aside, and the panel's empty state
  // ("Pick a minute on the register to read it across every series") is
  // the printed instruction the round struck everywhere (README section
  // "No apparatus, anywhere") -- the same text the owner objected to on
  // the seismograph. Unmounted, not deleted; tracked on #691.
  const CROSS_SECTION_ENABLED: boolean = false
  // No flag columns at all -- 0 fired this hour -- means no group gap to
  // reserve either, so the traffic ribbons get the space back instead of
  // leaving a blank strip.
  const flagGap = $derived(firedFlags.length > 0 ? GROUP_GAP : 0)
  const flagsWidth = $derived(firedFlags.length * FLAG_COL_MIN)
  const dataMinWidth = $derived(GUTTER + hour.traffic.length * COL_MIN + flagGap + flagsWidth + 12)
  const width = $derived(Math.max(dataMinWidth, boxWidth || dataMinWidth))

  // Every traffic column grows to fill what the flag columns and gutter
  // leave behind -- "columns spaced across the frame" (round 21). Flag
  // columns stay narrow, as ratified; only the traffic ribbons spread.
  const COL_W = $derived(
    hour.traffic.length > 0 ? Math.max(COL_MIN, (width - GUTTER - flagGap - flagsWidth - 12) / hour.traffic.length) : COL_MIN,
  )
  const FLAG_COL_W = FLAG_COL_MIN
  // A ribbon at "full breadth" (round 22) fills nearly its whole column
  // at the series' own peak, with a hairline gutter left between
  // neighbours so two maxed-out ribbons never touch.
  const HALF = $derived(Math.max(30, COL_W / 2 - 8))

  const flagsX0 = $derived(GUTTER + hour.traffic.length * COL_W + flagGap)
  const height = $derived(HEADER + Math.max(1, n) * ROW_H + BOTTOM)

  const refusedFrom = $derived(hour.traffic.findIndex((s) => s.ink === 'refused'))

  // Row 0 is the brink: the newest minute is the top edge, the way the
  // fall reads (#363). Everything below indexes back through the hour.
  function rowOf(index: number): number {
    return n - 1 - index
  }

  function yOf(index: number): number {
    return HEADER + rowOf(index) * ROW_H
  }

  function colX(i: number): number {
    return GUTTER + i * COL_W + COL_W / 2
  }

  function flagX(i: number): number {
    return flagsX0 + i * FLAG_COL_W + FLAG_COL_W / 2
  }

  // A midpoint-quadratic edge rather than one straight segment per
  // minute: sixty hard corners read as noise, the smoothed edge reads as
  // a pen stroke, and neither moves a value off its own row.
  function edge(points: [number, number][]): string {
    let d = ''
    for (let i = 1; i < points.length; i++) {
      const q = points[i - 1]
      const p = points[i]
      d += ` Q${q[0].toFixed(1)},${q[1].toFixed(1)} ${((q[0] + p[0]) / 2).toFixed(1)},${((q[1] + p[1]) / 2).toFixed(1)}`
    }
    const last = points[points.length - 1]
    d += ` L${last[0].toFixed(1)},${last[1].toFixed(1)}`
    return d
  }

  function ribbon(values: number[], cx: number, scale: number): string {
    if (n === 0) return ''
    const left: [number, number][] = []
    const right: [number, number][] = []
    for (let i = n - 1; i >= 0; i--) {
      const w = Math.max(0.35, (values[i] / scale) * HALF)
      const y = snapFill(yOf(i), dpr)
      left.push([snapFill(cx - w, dpr), y])
      right.push([snapFill(cx + w, dpr), y])
    }
    right.reverse()
    return (
      `M${left[0][0].toFixed(1)},${left[0][1].toFixed(1)}` +
      edge(left) +
      ` L${right[0][0].toFixed(1)},${right[0][1].toFixed(1)}` +
      edge(right) +
      ' Z'
    )
  }

  const timeTicks = $derived(
    n === 0 ? [] : Array.from({ length: n }, (_, i) => i).filter((i) => (n - 1 - i) % 10 === 0),
  )

  function selectFromPointer(event: PointerEvent) {
    if (n === 0) return
    const target = event.currentTarget as SVGSVGElement
    const rect = target.getBoundingClientRect()
    const scale = rect.height === 0 ? 1 : height / rect.height
    const y = (event.clientY - rect.top) * scale
    const row = Math.round((y - HEADER) / ROW_H)
    onselect(Math.min(n - 1, Math.max(0, n - 1 - row)))
  }

  const label = $derived(
    `Register: ${hour.traffic.length} traffic series and ${firedFlags.length} flag types for the hour to ` +
      `${hour.brink ? formatHM(hour.brink) : 'now'}, vertical ribbons on shared minute-rows, newest at the top`,
  )
</script>

<div class="register">
  <div class="paper scrollbar" bind:clientWidth={boxWidth}>
    {#if n === 0}
      <p class="empty">No minutes recorded yet — the register starts as soon as events arrive.</p>
    {:else}
      <svg {width} {height} viewBox="0 0 {width} {height}" role="img" aria-label={label} onpointerdown={selectFromPointer}>
        <text class="group" x={GUTTER + 2} y="16">TRAFFIC</text>
        {#if refusedFrom >= 0}
          <text class="group" x={colX(refusedFrom) - HALF} y="16">REFUSED</text>
        {/if}
        {#if firedFlags.length > 0}
          <text class="group" x={flagsX0} y="16">FLAG EPISODES</text>
        {/if}

        {#each timeTicks as i (i)}
          <text class="time" class:brink={i === n - 1} x={GUTTER - 8} y={snapFill(yOf(i), dpr) + 3.5} text-anchor="end"
            >{formatHM(hour.axis[i])}</text
          >
          {#if i !== n - 1}
            <line class="decade" x1={GUTTER} x2={width - 8} y1={snapLine(yOf(i), dpr)} y2={snapLine(yOf(i), dpr)} />
          {/if}
        {/each}

        {#each hour.traffic as series, i (series.key)}
          {@const cx = colX(i)}
          <text class="c-name" x={cx - HALF} y="62">{series.label}</text>
          <text class="c-now ink-text-{series.ink}" x={cx - HALF} y="82"
            >{series.now}<tspan class="c-unit"> /min</tspan></text
          >
          <text class="c-scale" x={cx - HALF} y="98">width {series.scale}/min</text>
          <line class="axis" x1={snapLine(cx, dpr)} x2={snapLine(cx, dpr)} y1={HEADER} y2={height - BOTTOM} />
          <path class="ribbon ink-{series.ink}" d={ribbon(series.values, cx, series.scale)} />
        {/each}

        {#each firedFlags as series, i (series.key)}
          {@const cx = flagX(i)}
          <!-- Diagonal, per the owner's #716 reversal of 778203f: the
               anchor sits near the top of the header (FLAG_LABEL_TOP), and
               the label sweeps down-left from there, its tail clearing the
               axis by FLAG_LABEL_CLEARANCE -- see the constants above. -->
          <text
            class="f-name"
            class:quiet={!series.spoke}
            x={cx}
            y={FLAG_LABEL_TOP}
            text-anchor="end"
            transform="rotate(-{FLAG_ROTATE_DEG} {cx} {FLAG_LABEL_TOP})">{series.short}</text
          >
          <line
            class="axis"
            class:quiet={!series.spoke}
            x1={snapLine(cx, dpr)}
            x2={snapLine(cx, dpr)}
            y1={HEADER}
            y2={height - BOTTOM}
          />
          {#each series.values as v, mi (mi)}
            {#if v > 0}
              <line class="tick" x1={cx - 9} x2={cx + 9} y1={snapLine(yOf(mi), dpr)} y2={snapLine(yOf(mi), dpr)} />
              {#if v > 1}
                <text class="tick-n" x={cx + 11} y={snapFill(yOf(mi), dpr) + 3}>×{v}</text>
              {/if}
            {/if}
          {/each}
          <text class="f-total" class:quiet={!series.spoke} x={cx} y={height - BOTTOM + 14} text-anchor="middle"
            >{series.total}</text
          >
        {/each}

        <!-- amber is time: the brink is the top edge the hour hangs from -->
        <line class="brink-glow" x1={GUTTER - 12} x2={width - 8} y1={snapLine(HEADER, dpr)} y2={snapLine(HEADER, dpr)} />
        <line class="brink-edge" x1={GUTTER - 12} x2={width - 8} y1={snapLine(HEADER, dpr)} y2={snapLine(HEADER, dpr)} />

        {#if cursor >= 0}
          <rect class="cursor-band" x={GUTTER - 12} y={snapFill(yOf(cursor), dpr) - 4} width={width - GUTTER - 4} height="8" />
          <line class="cursor" x1={GUTTER - 12} x2={width - 8} y1={snapLine(yOf(cursor), dpr)} y2={snapLine(yOf(cursor), dpr)} />
          <text class="time brink" x={GUTTER - 8} y={snapFill(yOf(cursor), dpr) + 3.5} text-anchor="end"
            >{formatHM(hour.axis[cursor])}</text
          >
        {/if}
      </svg>
    {/if}
  </div>

  {#if CROSS_SECTION_ENABLED}
  <aside class="cross-section" aria-label="The selected minute">
    {#if reading}
      <h3>The minute {formatHM(reading.time)}</h3>
      <dl>
        {#each reading.traffic as row (row.key)}
          <div class="xs-row">
            <dt>{row.label}</dt>
            <dd class:refused={row.ink === 'refused'}>{row.value}</dd>
          </div>
        {/each}
        <div class="xs-row total">
          <dt>flag episodes</dt>
          <dd>{reading.episodeTotal}</dd>
        </div>
      </dl>
      {#if reading.episodes.length > 0}
        <p class="episodes">{reading.episodes.map((e) => `${e.label}${e.value > 1 ? ` ×${e.value}` : ''}`).join(' · ')}</p>
      {/if}
    {:else}
      <h3>The minute</h3>
      <p class="hint">Pick a minute on the register to read it across every series.</p>
    {/if}
  </aside>
  {/if}
</div>

<!-- ROUND 30 FIDELITY: none of the three ratified metrics views (seismograph,
     register, table -- docs/design/concepts/round-30/the-whole.html #s4)
     draws a ledger strip. Per the project's build-to-the-mockup-first
     policy (#700) this stays implemented rather than deleted; it is just
     unmounted here so nothing renders. Re-mounting it (or replacing it) is
     tracked as a gap on #691. Guarded by a flag rather than an HTML comment
     so MetricsTotals and its {hour} usage stay live code and the type check
     keeps covering them; a plain {#if false} does the same job but narrows
     to never, which reports the block as unreachable. Same pattern as
     LiveTable's RESIZE_HANDLES_ENABLED. -->
{#if LEDGER_ENABLED}
  <div class="ledger">
    <h3 class="ledger-head">The ledger <span>· the same hour in totals — magnitude, not time</span></h3>
    <MetricsTotals {hour} />
  </div>
{/if}

<style>
  .register {
    display: flex;
    gap: 14px;
    align-items: flex-start;
    min-width: 0;
  }

  .paper {
    flex: 1;
    min-width: 0;
    overflow-x: auto;
  }

  .paper svg {
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

  .cross-section {
    flex: none;
    width: 216px;
    border-left: 1px solid var(--border);
    padding-left: 14px;
  }

  .cross-section h3 {
    margin: 0 0 8px;
    font-size: 9px;
    font-weight: 650;
    letter-spacing: 0.16em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .cross-section dl {
    margin: 0;
  }

  .xs-row {
    display: flex;
    justify-content: space-between;
    gap: 10px;
    padding: 2px 0;
  }

  .xs-row.total {
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--border);
  }

  .xs-row dt {
    font-size: 11px;
    color: var(--fg-muted);
  }

  .xs-row dd {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 11.5px;
    font-weight: 600;
    color: var(--fg);
  }

  .xs-row dd.refused {
    color: var(--chart-refused);
  }

  .episodes {
    margin: 8px 0 0;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .hint {
    margin: 0;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .ledger {
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }

  .ledger-head {
    margin: 0 0 10px;
    font-size: 11px;
    font-weight: 650;
    color: var(--fg-muted);
  }

  .ledger-head span {
    font-weight: 400;
    color: var(--fg-dim);
  }

  .group {
    fill: var(--fg-dim);
    font-size: 9px;
    font-weight: 650;
    letter-spacing: 0.16em;
  }

  .time {
    fill: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 9.5px;
  }

  .time.brink {
    fill: var(--now);
  }

  .decade {
    stroke: var(--border);
    stroke-width: 1;
    opacity: 0.45;
    shape-rendering: crispEdges;
  }

  .axis {
    stroke: var(--border);
    stroke-width: 1;
    shape-rendering: crispEdges;
  }

  .axis.quiet {
    opacity: 0.55;
  }

  .c-name {
    fill: var(--fg-muted);
    font-size: 11px;
    font-weight: 600;
  }

  .c-now {
    font-family: var(--font-mono);
    font-size: 14px;
    font-weight: 600;
  }

  .c-unit,
  .c-scale {
    fill: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 8.5px;
    font-weight: 400;
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

  .ribbon {
    fill-opacity: 0.62;
    stroke: none;
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

  @media (max-width: 900px) {
    .register {
      flex-direction: column;
    }

    .cross-section {
      width: 100%;
      border-left: none;
      border-top: 1px solid var(--border);
      padding-left: 0;
      padding-top: 10px;
    }
  }
</style>
