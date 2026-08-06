<script lang="ts">
  // Multi-line time-series chart of newly-raised flag episodes by type,
  // over the last hour at 1-minute resolution (see
  // internal/flags/store.go's minuteBuckets/Store.TimeSeries) -- the
  // flags-equivalent of EventsChart.svelte, whose structure/conventions
  // this deliberately mirrors closely (hand-rolled SVG, no charting
  // library, chart/table toggle, hover-crosshair). "Newly-raised episode"
  // (isNew from flags.Store.Add) matches how the rest of the app already
  // treats flag "activity" (e.g. Flags.svelte's activeCount badges), not
  // a fresh convention: a plain re-fire of an already-active flag does
  // not bump a bucket, so this reads as "how many distinct incidents
  // started," not "how many times did a detector re-check."
  //
  // Colors extend the app's existing fixed accept/drop/reject/log/
  // unknown semantic colors to the 14 flag types (see app.css's
  // --flag-* custom properties) -- past the ~8 series an OKLab-CVD-safe
  // palette can fully guarantee (dataviz skill), so identity leans on
  // the same mitigation EventsChart already applies: a persistent
  // legend with text labels, never color alone.
  import { flagsState } from '../lib/flags.svelte'
  import { formatHM } from '../lib/format'
  import type { FlagTimeBucket, FlagType } from '../lib/types'

  // Same declared order app.css's --flag-* variables use -- also the
  // order flags appear in the legend/table when more than one type is
  // present.
  const TYPE_ORDER: FlagType[] = [
    'port_scan',
    'activity_spike',
    'critical_port',
    'global_spike',
    'distributed_brute_force',
    'outbound_anomaly',
    'internal_recon',
    'rule_spike',
    'repeated_drops',
    'low_slow_scan',
    'off_hours_activity',
    'device_silence',
    'new_device',
    'stale_rule',
    'unexpected_mail_sender',
  ]

  // Same labels Flags.svelte uses -- duplicated rather than shared,
  // matching how ACTION_LABELS is already independently duplicated in
  // both EventsChart.svelte and Dashboard.svelte in this codebase.
  const TYPE_LABELS: Record<FlagType, string> = {
    port_scan: 'Port scan',
    activity_spike: 'Activity spike',
    critical_port: 'Critical-port attempts',
    global_spike: 'Network-wide volume spike',
    distributed_brute_force: 'Distributed brute-force',
    outbound_anomaly: 'Outbound anomaly',
    internal_recon: 'Internal reconnaissance',
    rule_spike: 'Rule hit-rate spike',
    repeated_drops: 'Repeated drops on a port',
    low_slow_scan: 'Low-and-slow port scan',
    off_hours_activity: 'Off-hours activity',
    device_silence: 'Device gone quiet',
    new_device: 'New device',
    stale_rule: 'Stale firewall rule',
    unexpected_mail_sender: 'Unexpected mail sender',
  }

  const W = 460
  const H = 180
  const MARGIN = { top: 10, right: 10, bottom: 22, left: 34 }
  const plotW = W - MARGIN.left - MARGIN.right
  const plotH = H - MARGIN.top - MARGIN.bottom

  let view = $state<'chart' | 'table'>('chart')
  let hoverIndex = $state<number | null>(null)
  let svgEl: SVGSVGElement | undefined = $state()

  const points = $derived(flagsState.timeSeries)

  const seriesTypes = $derived(
    TYPE_ORDER.filter((t) => points.some((p) => (p.byType[t] ?? 0) > 0)),
  )

  const maxValue = $derived(
    niceCeil(points.reduce((m, p) => Math.max(m, ...seriesTypes.map((t) => p.byType[t] ?? 0)), 0)),
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

  function pathFor(type: FlagType): string {
    return points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i).toFixed(1)},${y(p.byType[type] ?? 0).toFixed(1)}`).join(' ')
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

  function tableRows(): FlagTimeBucket[] {
    return points
  }
</script>

<div class="flags-chart">
  <div class="header">
    <span class="title">Flags raised — last hour</span>
    <button class="toggle" onclick={() => (view = view === 'chart' ? 'table' : 'chart')}>
      {view === 'chart' ? 'Table view' : 'Chart view'}
    </button>
  </div>

  {#if points.length === 0 || seriesTypes.length === 0}
    <div class="empty">No flags raised in the last hour</div>
  {:else if view === 'chart'}
    <svg
      bind:this={svgEl}
      viewBox="0 0 {W} {H}"
      role="img"
      aria-label="Newly-raised flag episodes by type over the last hour"
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

      {#each seriesTypes as type (type)}
        <path d={pathFor(type)} class="line" style="stroke: var(--flag-{type})" fill="none" />
        <circle
          cx={x(points.length - 1)}
          cy={y(points[points.length - 1].byType[type] ?? 0)}
          r="4"
          class="end-dot"
          style="fill: var(--flag-{type})"
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
        {#each seriesTypes as type (type)}
          <circle
            cx={x(hoverIndex)}
            cy={y(points[hoverIndex].byType[type] ?? 0)}
            r="4"
            class="end-dot"
            style="fill: var(--flag-{type})"
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
        {#each seriesTypes as type (type)}
          <div class="tooltip-row">
            <span class="dot" style="background: var(--flag-{type})"></span>
            <span class="label">{TYPE_LABELS[type]}</span>
            <span class="value">{points[hoverIndex].byType[type] ?? 0}</span>
          </div>
        {/each}
      </div>
    {/if}

    {#if seriesTypes.length > 1}
      <div class="legend">
        {#each seriesTypes as type (type)}
          <span class="legend-item">
            <span class="dot" style="background: var(--flag-{type})"></span>
            {TYPE_LABELS[type]}
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
            {#each seriesTypes as type (type)}
              <th>{TYPE_LABELS[type]}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each tableRows() as p (p.time)}
            <tr>
              <td>{formatHM(p.time)}</td>
              {#each seriesTypes as type (type)}
                <td>{p.byType[type] ?? 0}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .flags-chart {
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

  .end-dot {
    stroke: var(--bg-elevated);
    stroke-width: 2;
  }

  .crosshair {
    stroke: var(--accent);
    stroke-width: 1;
    stroke-dasharray: 2 2;
  }

  .tooltip {
    position: relative;
    width: max-content;
    max-width: 220px;
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
