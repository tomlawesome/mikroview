<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Multi-line time-series chart of event volume by action, over the last
  // hour at 1-minute resolution (see internal/store/ring.go's
  // Stats.TimeSeries). Built by hand in SVG rather than pulling in a
  // charting library, consistent with the rest of this app's lean stack.
  //
  // Colors deliberately reuse the app's existing fixed per-action
  // semantic colors (see app.css) rather than a fresh categorical
  // palette, so a line here means the same thing as the same color
  // everywhere else (ActionBadge, row tinting). Validating that set
  // as a *chart* palette (dataviz skill's validator) flags low chroma on
  // "unknown" and marginal light-mode contrast on a few slots -- the
  // mitigation applied per the skill's own guidance is a persistent
  // legend with text labels, so identity never depends on color alone.
  import { appState } from '../lib/state.svelte'
  import { formatHM } from '../lib/format'
  import type { Action, TimeBucket } from '../lib/types'
  import { ACTIONS, ACTION_LABELS } from '../lib/actions'

  // Order is the legend order and therefore the adjacency the palette
  // was checked against -- see app.css's note on --marked/--natted.
  const ACTION_ORDER = ACTIONS

  const W = 460
  const H = 180
  const MARGIN = { top: 10, right: 10, bottom: 22, left: 34 }
  const plotW = W - MARGIN.left - MARGIN.right
  const plotH = H - MARGIN.top - MARGIN.bottom

  let view = $state<'chart' | 'table'>('chart')
  let hoverIndex = $state<number | null>(null)
  let svgEl: SVGSVGElement | undefined = $state()

  const points = $derived(appState.stats?.timeSeries ?? [])

  const seriesActions = $derived(
    ACTION_ORDER.filter((a) => points.some((p) => (p.byAction[a] ?? 0) > 0)),
  )

  const maxValue = $derived(
    niceCeil(points.reduce((m, p) => Math.max(m, ...seriesActions.map((a) => p.byAction[a] ?? 0)), 0)),
  )

  function niceCeil(v: number): number {
    if (v <= 0) return 1
    const pow = Math.pow(10, Math.floor(Math.log10(v)))
    for (const step of [1, 2, 5, 10]) {
      if (v <= step * pow) return step * pow
    }
    return 10 * pow
  }

  function x(i: number): number {
    if (points.length <= 1) return MARGIN.left
    return MARGIN.left + (i / (points.length - 1)) * plotW
  }

  function y(v: number): number {
    return MARGIN.top + plotH - (v / maxValue) * plotH
  }

  function pathFor(action: Action): string {
    return points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i).toFixed(1)},${y(p.byAction[action] ?? 0).toFixed(1)}`).join(' ')
  }

  const yTicks = $derived([0, maxValue / 2, maxValue])

  const xTickIndices = $derived(
    points.length === 0
      ? []
      : [0, Math.round((points.length - 1) / 2), points.length - 1].filter(
          (v, i, arr) => arr.indexOf(v) === i,
        ),
  )

  function onMove(e: PointerEvent) {
    if (!svgEl || points.length === 0) return
    const rect = svgEl.getBoundingClientRect()
    const relX = ((e.clientX - rect.left) / rect.width) * W
    const frac = (relX - MARGIN.left) / plotW
    const idx = Math.round(frac * (points.length - 1))
    hoverIndex = Math.min(points.length - 1, Math.max(0, idx))
  }

  function tableRows(): TimeBucket[] {
    return points
  }
</script>

<div class="events-chart">
  <div class="header">
    <span class="title">Event volume — last hour</span>
    <button class="toggle" onclick={() => (view = view === 'chart' ? 'table' : 'chart')}>
      {view === 'chart' ? 'Table view' : 'Chart view'}
    </button>
  </div>

  {#if points.length === 0 || seriesActions.length === 0}
    <div class="empty">No events yet</div>
  {:else if view === 'chart'}
    <svg
      bind:this={svgEl}
      viewBox="0 0 {W} {H}"
      role="img"
      aria-label="Event volume by action over the last hour"
      onpointermove={onMove}
      onpointerleave={() => (hoverIndex = null)}
    >
      <!-- gridlines: hairline, recessive, solid -->
      {#each yTicks as t (t)}
        <line x1={MARGIN.left} x2={W - MARGIN.right} y1={y(t)} y2={y(t)} class="grid" />
        <text x={MARGIN.left - 6} y={y(t)} class="axis-label" text-anchor="end" dominant-baseline="middle"
          >{Math.round(t)}</text
        >
      {/each}

      {#each xTickIndices as i (i)}
        <text x={x(i)} y={H - 4} class="axis-label" text-anchor="middle">{formatHM(points[i].time)}</text>
      {/each}

      {#each seriesActions as action (action)}
        <path d={pathFor(action)} class="line line-{action}" fill="none" />
        <circle
          cx={x(points.length - 1)}
          cy={y(points[points.length - 1].byAction[action] ?? 0)}
          r="4"
          class="end-dot end-dot-{action}"
        />
      {/each}

      {#if hoverIndex !== null}
        <line
          x1={x(hoverIndex)}
          x2={x(hoverIndex)}
          y1={MARGIN.top}
          y2={MARGIN.top + plotH}
          class="crosshair"
        />
        {#each seriesActions as action (action)}
          <circle
            cx={x(hoverIndex)}
            cy={y(points[hoverIndex].byAction[action] ?? 0)}
            r="4"
            class="end-dot end-dot-{action}"
          />
        {/each}
      {/if}
    </svg>

    {#if hoverIndex !== null}
      <div
        class="tooltip"
        style="left: {Math.min(78, Math.max(0, (x(hoverIndex) / W) * 100))}%"
      >
        <div class="tooltip-time">{formatHM(points[hoverIndex].time)}</div>
        {#each seriesActions as action (action)}
          <div class="tooltip-row">
            <span class="dot dot-{action}"></span>
            <span class="label">{ACTION_LABELS[action]}</span>
            <span class="value">{points[hoverIndex].byAction[action] ?? 0}</span>
          </div>
        {/each}
      </div>
    {/if}

    {#if seriesActions.length > 1}
      <div class="legend">
        {#each seriesActions as action (action)}
          <span class="legend-item">
            <span class="dot dot-{action}"></span>
            {ACTION_LABELS[action]}
          </span>
        {/each}
      </div>
    {/if}
  {:else}
    <div class="table-wrap scrollbar">
      <table>
        <thead>
          <tr>
            <th>Time</th>
            {#each seriesActions as action (action)}
              <th>{ACTION_LABELS[action]}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each tableRows() as p (p.time)}
            <tr>
              <td>{formatHM(p.time)}</td>
              {#each seriesActions as action (action)}
                <td>{p.byAction[action] ?? 0}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .events-chart {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  .title {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .toggle {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 4px 9px;
    font-size: 12px;
  }

  .toggle:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .empty {
    padding: 40px 0;
    text-align: center;
    color: var(--fg-dim);
    font-size: 13px;
  }

  svg {
    width: 100%;
    height: auto;
    touch-action: none;
  }

  .grid {
    stroke: var(--border);
    stroke-width: 1;
  }

  .axis-label {
    fill: var(--fg-dim);
    font-size: 9px;
    font-family: var(--font-mono);
  }

  .line {
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .line-accept {
    stroke: var(--accept);
  }
  .line-drop {
    stroke: var(--drop);
  }
  .line-reject {
    stroke: var(--reject);
  }
  .line-log {
    stroke: var(--log);
  }
  .line-marked {
    stroke: var(--marked);
  }
  .line-natted {
    stroke: var(--natted);
  }
  .line-unknown {
    stroke: var(--unknown);
  }

  .end-dot {
    stroke: var(--bg-elevated);
    stroke-width: 2;
  }

  .end-dot-accept {
    fill: var(--accept);
  }
  .end-dot-drop {
    fill: var(--drop);
  }
  .end-dot-reject {
    fill: var(--reject);
  }
  .end-dot-log {
    fill: var(--log);
  }
  .end-dot-marked {
    fill: var(--marked);
  }
  .end-dot-natted {
    fill: var(--natted);
  }
  .end-dot-unknown {
    fill: var(--unknown);
  }

  .crosshair {
    stroke: var(--accent);
    stroke-width: 1;
    stroke-dasharray: 2 2;
  }

  .tooltip {
    position: relative;
    width: max-content;
    max-width: 200px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 8px;
    font-size: 12px;
    pointer-events: none;
    box-shadow: 0 8px 20px -6px rgba(0, 0, 0, 0.4);
  }

  .tooltip-time {
    color: var(--fg-muted);
    font-family: var(--font-mono);
    margin-bottom: 3px;
  }

  .tooltip-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .tooltip-row .label {
    color: var(--fg-muted);
    flex: 1;
  }

  .tooltip-row .value {
    color: var(--fg);
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
  }

  .dot-accept {
    background: var(--accept);
  }
  .dot-drop {
    background: var(--drop);
  }
  .dot-reject {
    background: var(--reject);
  }
  .dot-log {
    background: var(--log);
  }
  .dot-marked {
    background: var(--marked);
  }
  .dot-natted {
    background: var(--natted);
  }
  .dot-unknown {
    background: var(--unknown);
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }

  .legend-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .table-wrap {
    max-height: 220px;
    overflow-y: auto;
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  th,
  td {
    padding: 5px 9px;
    text-align: right;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  th:first-child,
  td:first-child {
    text-align: left;
    font-family: var(--font-mono);
    color: var(--fg-muted);
  }

  th {
    position: sticky;
    top: 0;
    background: var(--bg-elevated);
    color: var(--fg-dim);
    font-weight: 600;
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.03em;
    border-bottom: 1px solid var(--border);
  }

  tbody tr:nth-child(even) {
    background: var(--bg-hover);
  }
</style>
