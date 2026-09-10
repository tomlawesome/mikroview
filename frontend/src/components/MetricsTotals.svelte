<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The ledger (#488, docs/design/screens/metrics/DESIGN.md): "the
  // cards' totals (top rules, top talkers, by device) open the Table
  // view".
  //
  // Rounds 36-37 (#803) gave it one home rather than two: the band above
  // the Table's minutes. The Register used to carry a copy as a strip
  // under its paper; it no longer does, so there is no second place the
  // same magnitude question can be answered differently.
  //
  // Deliberately *not* BarList: BarList
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
  import { zonesState } from '../lib/zones.svelte'
  import { familyOf } from '../lib/flagPalette'
  import type { MetricsHour } from '../lib/metricsSeries'

  let { hour }: { hour: MetricsHour } = $props()

  const TOP_N = 8

  type Row = { label: string; count: number; ink?: string }

  const topRules = $derived<Row[]>(
    (appState.stats?.topRules ?? []).slice(0, TOP_N).map((r) => ({ label: r.rule, count: r.count })),
  )
  // The server itself caps Stats.TopRules (internal/store/ring.go's
  // topRulesLimit), so this is what the client knows exists, not
  // necessarily the whole ruleset -- the honest floor for the "how much
  // of the whole" caption below, never inflated past what we were sent.
  const topRulesKnown = $derived(appState.stats?.topRules?.length ?? 0)

  const topTalkers = $derived<Row[]>(topNBy(appState.filteredEvents, (e) => e.srcIp, TOP_N))
  const talkersKnown = $derived(new Set(appState.filteredEvents.map((e) => e.srcIp).filter(Boolean)).size)

  const byProtocol = $derived<Row[]>(topNBy(appState.filteredEvents, (e) => e.protocol?.toUpperCase(), TOP_N))
  const protocolsKnown = $derived(
    new Set(appState.filteredEvents.map((e) => e.protocol?.toUpperCase()).filter(Boolean)).size,
  )

  // Devices take their lane colours (#732): the same rank-ordered lane
  // ink Topography.svelte and Entities.svelte already assign each
  // observed zone, read from the shared zonesState rather than a second
  // classification of the same boundaries. Read-only, like Flags.svelte's
  // own use of zonesState -- whichever scene loads first populates it,
  // and a metrics-first session just shows the plain default ink until
  // one does, the same degrade every other zonesState reader accepts.
  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']
  const laneByIp = $derived.by(() => {
    const m = new Map<string, string>()
    zonesState.zones.forEach((z, rank) => {
      for (const h of z.hosts) m.set(h.ip, LANE_INKS[rank % LANE_INKS.length])
    })
    return m
  })

  const byDevice = $derived<Row[]>(
    [...appState.devices]
      .sort((a, b) => b.eventCount - a.eventCount)
      .slice(0, TOP_N)
      .map((d) => ({ label: d.name, count: d.eventCount, ink: laneByIp.get(d.sourceIp) })),
  )

  // The hour's own refused total, from the same series the drum drew --
  // not a second count of the same thing from a different source.
  // Action keeps accept-green and drop-red (#732): --accept is the same
  // green every accept badge already wears; --chart-refused is the red
  // this card already used for the refused half before this change.
  const byAction = $derived<Row[]>(
    [...hour.traffic]
      .filter((s) => s.total > 0)
      .sort((a, b) => b.total - a.total)
      .map((s) => ({ label: s.label, count: s.total, ink: s.ink === 'refused' ? 'var(--chart-refused)' : 'var(--accept)' })),
  )

  // Flag types take the flag ink (#732): the same family ink the
  // docket, Flags.svelte and Entities.svelte already stripe each type
  // with (lib/flagPalette.ts), keyed by the same FlagType the series
  // already carries.
  const byFlagType = $derived<Row[]>(
    hour.flags.filter((s) => s.spoke).map((s) => ({ label: s.label, count: s.total, ink: familyOf(s.key).ink })),
  )

  // Rankings (rules, talkers, devices, flag types) stay ranked bars.
  // Proportions (action, protocol) draw as one split bar instead of a
  // list of bars sized against their own max -- a single-row ranking is
  // always 100% of itself, which says nothing; a single-row proportion
  // is honestly 100% of the whole (#732).
  const columns = $derived([
    { title: 'Top rules', rows: topRules, empty: 'No labeled rules seen yet', kind: 'ranked' as const, known: topRulesKnown },
    { title: 'Top talkers', rows: topTalkers, empty: 'No source addresses yet', kind: 'ranked' as const, known: talkersKnown },
    {
      title: 'By device',
      rows: byDevice,
      empty: 'No devices seen yet',
      kind: 'ranked' as const,
      known: appState.devices.length,
    },
    { title: 'By protocol', rows: byProtocol, empty: 'No protocols seen yet', kind: 'split' as const, known: protocolsKnown },
    { title: 'The hour by action', rows: byAction, empty: 'No events in the hour', kind: 'split' as const, known: undefined },
    { title: 'Episodes by flag type', rows: byFlagType, empty: 'Nothing raised this hour', kind: 'ranked' as const, known: undefined },
  ])
</script>

<div class="ledger-strip">
  {#each columns as column (column.title)}
    <section class="column">
      <h4>
        {column.title}{column.known !== undefined && column.known > column.rows.length
          ? ` · top ${column.rows.length} of ${column.known}`
          : ''}
      </h4>
      {#if column.rows.length === 0}
        <p class="empty">{column.empty}</p>
      {:else if column.kind === 'split'}
        {@const sum = column.rows.reduce((s, r) => s + r.count, 0)}
        <div class="split-track">
          {#each column.rows as row (row.label)}
            <span class="segment" style="flex: {sum ? row.count : 0} 0 0%; background: {row.ink ?? 'var(--chart-traffic)'}"
            ></span>
          {/each}
        </div>
        {#each column.rows as row (row.label)}
          <div class="row split-row">
            <span class="label" title={row.label}>{row.label}</span>
            <span class="count">{row.count}</span>
          </div>
        {/each}
      {:else}
        {@const max = column.rows.reduce((m, r) => Math.max(m, r.count), 0)}
        {#each column.rows as row (row.label)}
          <div class="row">
            <span class="label" title={row.label}>{row.label}</span>
            <span class="track">
              <span
                class="bar"
                style="width: {max ? (row.count / max) * 100 : 0}%; {row.ink ? `background: ${row.ink};` : ''}"
              ></span>
            </span>
            <span class="count">{row.count}</span>
          </div>
        {/each}
      {/if}
    </section>
  {/each}
</div>

<style>
  /* Round 37's `.ledger` (#803): six columns across the head of the table
     view, gap 28. The six are a fixed set, not a variable one, so they
     are declared as six tracks rather than auto-fitted to a minimum
     width -- which is what put five across a wide band and left the
     sixth stranded on its own row. `minmax(0, 1fr)` rather than `1fr` so
     a long rule name ellipsises inside its track instead of widening it
     and pushing the rest out of true. */
  .ledger-strip {
    display: grid;
    grid-template-columns: repeat(6, minmax(0, 1fr));
    gap: 28px;
  }

  /* Narrower than the band the six were drawn for: fold to three, then
     two, rather than crushing six tracks past the point a label or a bar
     can be read. */
  @media (max-width: 1180px) {
    .ledger-strip {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 22px;
    }
  }

  @media (max-width: 640px) {
    .ledger-strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  /* Bars, no boxes (#803, and this file's own opening comment): #716's
     bordered elevated card is gone. The ledger is one band of six ranked
     answers under the table view's head rule, and six boxes inside a
     ruled band is a border drawn twice. The bars carry the magnitude;
     the h4 names the question. */
  .column {
    min-width: 0;
  }

  /* #716: 9px at --fg-dim was too faint to find at a glance. Bumped to
     the same size/weight/colour as this scene's other section
     headings (MetricsTable.svelte's h3), keeping the small-caps
     treatment but with less crushed letter-spacing. */
  .column h4 {
    margin: 0 0 7px;
    font-size: 11px;
    font-weight: 650;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-muted);
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

  /* Round 37's `.lrow .c`: the figure is the answer, so it carries the
     weight the label does not. */
  .count {
    flex: none;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }

  /* The proportion mark (#732): one track shared by every row instead
     of one track per row, each segment's width its share of the
     column's own total -- "two totals in proportion", not two rankings
     each measured against itself. */
  .split-track {
    display: flex;
    gap: 2px;
    height: 6px;
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-hover);
    margin-bottom: 6px;
  }

  .segment {
    display: block;
    height: 100%;
  }

  .row.split-row {
    justify-content: space-between;
  }

  .row.split-row .label {
    max-width: 70%;
  }
</style>
