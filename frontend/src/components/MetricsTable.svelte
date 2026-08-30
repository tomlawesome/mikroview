<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The table: metrics' third view (#488,
  // docs/design/screens/metrics/DESIGN.md).
  //
  // "A peer, not a fallback: the same hour and the same totals, sortable
  // and copyable, refused columns in refused ink, the cursor's minute
  // highlighted amber and selected across view switches."
  //
  // It is also the record's identity-without-colour proof: every column
  // is named in words, so the page still answers every question with the
  // ink switched off entirely.
  import type { MetricsHour } from '../lib/metricsSeries'
  import { formatHM } from '../lib/format'
  import MetricsTotals from './MetricsTotals.svelte'
  import CustomTopTalkers from './CustomTopTalkers.svelte'
  import { copyToClipboard } from '../lib/clipboard'
  import { toastState } from '../lib/toast.svelte'

  let { hour, cursor, onselect }: { hour: MetricsHour; cursor: number; onselect: (index: number) => void } = $props()

  // 'minute' plus one key per series, so a column header sorts the thing
  // it names rather than an index into a parallel array.
  type SortKey = string
  let sortKey = $state<SortKey>('minute')
  // Newest first, matching the fall (#363) and both drawn views' brink.
  let sortDir = $state<'asc' | 'desc'>('desc')

  // natted gets its own ink (#634 round 21: "natted (teal)") rather than
  // riding the traffic/refused pair -- it's neither an accept nor a
  // drop, and the app already has a token for exactly this fact
  // (app.css's --natted, the same one the row backgrounds use).
  const columns = $derived(
    hour.traffic.map((s) => ({ key: s.key as string, label: s.label, ink: s.ink, natted: s.key === 'natted' })),
  )

  function valueAt(index: number, key: SortKey): number {
    if (key === 'flags') return hour.episodesPerMinute[index] ?? 0
    const series = hour.traffic.find((s) => s.key === key)
    return series ? series.values[index] : 0
  }

  // Top port/top talker (#644 round 21) aren't sortable columns: they're
  // a label per minute, not a count, so there is nothing for a numeric
  // sort to compare -- same reason 'minute' itself sorts on index rather
  // than through valueAt. An em dash means "unknown," not "zero": either
  // GET /api/stats/tops hasn't answered for this minute yet, or the ring
  // buffer no longer holds every event from it (MinuteTop.complete is
  // false) -- see lib/metricsSeries.ts's own doc comment on MinuteTop.
  function topCell(index: number, field: 'talker' | 'port'): string {
    const top = hour.tops[index]
    if (!top || !top.complete) return '—'
    const v = top[field]
    return v && v.length > 0 ? v : '—'
  }

  const order = $derived(
    Array.from({ length: hour.axis.length }, (_, i) => i).sort((a, b) => {
      const d = sortKey === 'minute' ? a - b : valueAt(a, sortKey) - valueAt(b, sortKey)
      // A stable tie-break on the minute, so equal counts stay in time
      // order instead of shuffling when the data refreshes.
      return (sortDir === 'asc' ? d : -d) || a - b
    }),
  )

  function sortBy(key: SortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = 'desc'
    }
  }

  function ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
    if (sortKey !== key) return 'none'
    return sortDir === 'asc' ? 'ascending' : 'descending'
  }

  const episodesTotal = $derived(hour.episodesPerMinute.reduce((a, b) => a + b, 0))

  // Copyable, per the record: the whole hour as tab-separated figures,
  // in the order currently on screen -- what someone sorted to find is
  // what they get, not a differently-ordered second answer.
  const copyText = $derived(
    [
      ['minute', ...columns.map((c) => c.label), 'flag episodes', 'top port', 'top talker'].join('\t'),
      ...order.map((i) =>
        [
          formatHM(hour.axis[i]),
          ...hour.traffic.map((s) => s.values[i]),
          hour.episodesPerMinute[i] ?? 0,
          topCell(i, 'port'),
          topCell(i, 'talker'),
        ].join('\t'),
      ),
      // The hour-total row's own top port/talker would need the whole
      // hour's raw counts, not just each minute's already-decided
      // winner (the two are not the same statistic -- see
      // internal/store/ring.go's HourTops doc comment) -- left blank
      // rather than answering a different question than the column
      // header claims.
      ['hour total', ...hour.traffic.map((s) => s.total), episodesTotal, '—', '—'].join('\t'),
    ].join('\n'),
  )

  async function copyFigures() {
    toastState.show((await copyToClipboard(copyText)) ? 'Copied' : 'Copy failed')
  }
</script>

<div class="table-view">
  <section class="totals">
    <h3>The hour in totals <span>· magnitude, not time — where the old cards' answers live</span></h3>
    <MetricsTotals {hour} />
  </section>

  <section class="figures">
    <div class="figures-head">
      <h3>Every minute <span>· click a minute to put the cursor on it</span></h3>
      <button class="copy" onclick={copyFigures}>Copy the hour</button>
    </div>

    {#if hour.axis.length === 0}
      <p class="empty">No minutes recorded yet.</p>
    {:else}
      <div class="table-wrap scrollbar">
        <table>
          <thead>
            <tr>
              <th scope="col" aria-sort={ariaSort('minute')}>
                <button onclick={() => sortBy('minute')}>Minute</button>
              </th>
              {#each columns as column (column.key)}
                <th scope="col" class:refused={column.ink === 'refused'} class:natted={column.natted} aria-sort={ariaSort(column.key)}>
                  <button onclick={() => sortBy(column.key)}>{column.label}</button>
                </th>
              {/each}
              <th scope="col" aria-sort={ariaSort('flags')}>
                <button onclick={() => sortBy('flags')}>Flag episodes</button>
              </th>
              <!-- Not sortable -- a label per minute, not a count. See
                   topCell's own comment. -->
              <th scope="col">Top port</th>
              <th scope="col">Top talker</th>
            </tr>
          </thead>
          <tbody>
            {#each order as i (hour.axis[i])}
              <tr class:selected={i === cursor} aria-selected={i === cursor}>
                <th scope="row">
                  <button class="minute" onclick={() => onselect(i)}>{formatHM(hour.axis[i])}</button>
                </th>
                {#each hour.traffic as series (series.key)}
                  <td class:refused={series.ink === 'refused'} class:natted={series.key === 'natted'}>{series.values[i]}</td>
                {/each}
                <td>{hour.episodesPerMinute[i] ?? 0}</td>
                <td class="top">{topCell(i, 'port')}</td>
                <td class="top">{topCell(i, 'talker')}</td>
              </tr>
            {/each}
          </tbody>
          <tfoot>
            <tr>
              <th scope="row">Hour total</th>
              {#each hour.traffic as series (series.key)}
                <td class:refused={series.ink === 'refused'} class:natted={series.key === 'natted'}>{series.total}</td>
              {/each}
              <td>{episodesTotal}</td>
              <!-- Deliberately blank: see copyText's own comment on why
                   the hour total isn't "whichever port/talker won the
                   most individual minutes". -->
              <td class="top">—</td>
              <td class="top">—</td>
            </tr>
          </tfoot>
        </table>
      </div>
    {/if}
  </section>

  <section class="saved">
    <h3>Your saved breakdowns <span>· each one its own filter, kept in this browser</span></h3>
    <div class="widgets">
      <CustomTopTalkers />
    </div>
  </section>
</div>

<style>
  .table-view {
    display: flex;
    flex-direction: column;
    gap: 18px;
    min-width: 0;
  }

  h3 {
    margin: 0 0 10px;
    font-size: 11px;
    font-weight: 650;
    color: var(--fg-muted);
  }

  h3 span {
    font-weight: 400;
    color: var(--fg-dim);
  }

  .figures-head {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .figures-head h3 {
    margin-bottom: 10px;
  }

  .copy {
    margin-left: auto;
    margin-bottom: 10px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--fg-muted);
    font-size: 11px;
    padding: 3px 9px;
  }

  .copy:hover {
    color: var(--fg);
    border-color: var(--fg-dim);
  }

  .empty {
    margin: 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .table-wrap {
    overflow: auto;
    max-height: 60vh;
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  th,
  td {
    padding: 4px 10px;
    text-align: right;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  td {
    color: var(--fg);
    font-family: var(--font-mono);
  }

  thead th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--bg-elevated);
    border-bottom: 1px solid var(--border);
    padding: 0;
  }

  thead th button {
    width: 100%;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    text-align: right;
    padding: 6px 10px;
  }

  thead th button:hover {
    color: var(--fg);
  }

  thead th.refused button {
    color: var(--chart-refused);
  }

  thead th:first-child button,
  tbody th button {
    text-align: left;
  }

  tbody th {
    padding: 0;
    text-align: left;
    font-weight: 400;
  }

  tbody th button.minute {
    background: transparent;
    border: none;
    color: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 12px;
    padding: 4px 10px;
    width: 100%;
    text-align: left;
  }

  tbody tr:nth-child(even) {
    background: var(--bg-hover);
  }

  /* Amber is time: the cursor's minute, the same colour it wears on the
     drum and the register, so the three views agree about which minute
     is selected. */
  tbody tr.selected,
  tbody tr.selected:nth-child(even) {
    background: color-mix(in srgb, var(--now) 16%, transparent);
    box-shadow: inset 2px 0 0 var(--now);
  }

  tbody tr.selected th button.minute {
    color: var(--fg);
    font-weight: 600;
  }

  td.refused {
    color: var(--chart-refused);
  }

  /* natted's own ink (#634 round 21) -- neither accepted nor refused,
     so it gets the app's existing NAT token rather than riding either. */
  td.natted {
    color: var(--natted);
  }

  thead th.natted button {
    color: var(--natted);
  }

  tfoot th,
  tfoot td {
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
    font-weight: 600;
    color: var(--fg);
  }

  tfoot th {
    text-align: left;
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .widgets {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
    align-items: start;
  }
</style>
