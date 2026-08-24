<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The ledger (#488, docs/design/screens/metrics/DESIGN.md): "the
  // cards' totals (top rules, top talkers, by device) open the Table
  // view, and the Register carries them as its ledger strip".
  //
  // One component for both, so the two views cannot answer the same
  // magnitude question differently. Deliberately *not* BarList: BarList
  // draws a bordered, elevated card, and cards are exactly what the
  // ratified design removed here -- "their answers live in the ledger
  // below, which owns magnitude and never pretends to own time". The
  // bars stay; the boxes do not.
  //
  // Protocol breakdown rides along with the three the record names: it
  // was a fourth answer on the removed dashboard, and dropping a
  // question the operator could previously ask is a removal nothing
  // asked for.
  import { appState } from '../lib/state.svelte'
  import { topNBy } from '../lib/topN'
  import type { MetricsHour } from '../lib/metricsSeries'

  let { hour }: { hour: MetricsHour } = $props()

  const TOP_N = 8

  type Row = { label: string; count: number; refused?: boolean }

  const topRules = $derived<Row[]>(
    (appState.stats?.topRules ?? []).slice(0, TOP_N).map((r) => ({ label: r.rule, count: r.count })),
  )

  const topTalkers = $derived<Row[]>(topNBy(appState.filteredEvents, (e) => e.srcIp, TOP_N))

  const byProtocol = $derived<Row[]>(topNBy(appState.filteredEvents, (e) => e.protocol?.toUpperCase(), TOP_N))

  const byDevice = $derived<Row[]>(
    [...appState.devices]
      .sort((a, b) => b.eventCount - a.eventCount)
      .slice(0, TOP_N)
      .map((d) => ({ label: d.name, count: d.eventCount })),
  )

  // The hour's own refused total, from the same series the drum drew --
  // not a second count of the same thing from a different source.
  const byAction = $derived<Row[]>(
    [...hour.traffic]
      .filter((s) => s.total > 0)
      .sort((a, b) => b.total - a.total)
      .map((s) => ({ label: s.label, count: s.total, refused: s.ink === 'refused' })),
  )

  const byFlagType = $derived<Row[]>(
    hour.flags.filter((s) => s.spoke).map((s) => ({ label: s.label, count: s.total })),
  )

  const columns = $derived([
    { title: 'Top rules', rows: topRules, empty: 'No labeled rules seen yet' },
    { title: 'Top talkers', rows: topTalkers, empty: 'No source addresses yet' },
    { title: 'By device', rows: byDevice, empty: 'No devices seen yet' },
    { title: 'By protocol', rows: byProtocol, empty: 'No protocols seen yet' },
    { title: 'The hour by action', rows: byAction, empty: 'No events in the hour' },
    { title: 'Episodes by flag type', rows: byFlagType, empty: 'Nothing raised this hour' },
  ])
</script>

<div class="ledger-strip">
  {#each columns as column (column.title)}
    {@const max = column.rows.reduce((m, r) => Math.max(m, r.count), 0)}
    <section class="column">
      <h4>{column.title}</h4>
      {#if column.rows.length === 0}
        <p class="empty">{column.empty}</p>
      {:else}
        {#each column.rows as row (row.label)}
          <div class="row">
            <span class="label" title={row.label}>{row.label}</span>
            <span class="track">
              <span class="bar" class:refused={row.refused} style="width: {max ? (row.count / max) * 100 : 0}%"></span>
            </span>
            <span class="count">{row.count}</span>
          </div>
        {/each}
      {/if}
    </section>
  {/each}
</div>

<style>
  .ledger-strip {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
    gap: 22px;
  }

  .column h4 {
    margin: 0 0 7px;
    font-size: 9px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .empty {
    margin: 0;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 1.5px 0;
  }

  .label {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 46%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    color: var(--fg-muted);
  }

  .track {
    flex: 1;
    min-width: 0;
    height: 5px;
    background: var(--bg-hover);
    border-radius: 2px;
    overflow: hidden;
  }

  .bar {
    display: block;
    height: 100%;
    background: var(--chart-traffic);
  }

  .bar.refused {
    background: var(--chart-refused);
  }

  .count {
    flex: none;
    font-family: var(--font-mono);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }
</style>
