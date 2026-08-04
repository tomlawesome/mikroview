<script lang="ts">
  // Full-page dashboard tab: consolidates the charts that used to live in
  // toolbar popovers (event volume, top rules) plus new ones an admin
  // scanning traffic would want (action/protocol mix, top talkers,
  // per-device volume), all on one screen instead of one-at-a-time
  // popouts. Reads from the same reactive state the live view uses, so it
  // stays in sync with whatever filters/retention are active there.
  import { appState } from '../lib/state.svelte'
  import type { Action } from '../lib/types'
  import EventsChart from './EventsChart.svelte'
  import BarList from './BarList.svelte'

  const ACTION_LABELS: Record<Action, string> = {
    accept: 'Accept',
    drop: 'Drop',
    reject: 'Reject',
    log: 'Log',
    unknown: 'Unknown',
  }

  const TOP_N = 10

  function topNBy<T>(items: T[], keyOf: (item: T) => string | undefined, n: number) {
    const counts = new Map<string, number>()
    for (const item of items) {
      const key = keyOf(item)
      if (!key) continue
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return [...counts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, n)
      .map(([label, count]) => ({ label, count }))
  }

  const topRuleRows = $derived(
    (appState.stats?.topRules ?? []).map((r) => ({ label: r.rule, count: r.count })),
  )

  const actionRows = $derived(
    Object.entries(appState.stats?.byAction ?? {})
      .map(([action, count]) => ({
        label: ACTION_LABELS[action as Action],
        count: count ?? 0,
        colorVar: `var(--${action})`,
      }))
      .sort((a, b) => b.count - a.count),
  )

  const protocolRows = $derived(
    topNBy(appState.filteredEvents, (e) => e.protocol?.toUpperCase(), TOP_N),
  )

  const topTalkerRows = $derived(
    topNBy(appState.filteredEvents, (e) => e.srcIp, TOP_N),
  )

  const deviceRows = $derived(
    [...appState.devices]
      .sort((a, b) => b.eventCount - a.eventCount)
      .map((d) => ({ label: d.name, count: d.eventCount })),
  )
</script>

<div class="dashboard scrollbar">
  <div class="panel wide">
    <EventsChart />
  </div>
  <BarList title="Top rules" rows={topRuleRows} emptyMessage="No labeled rules seen yet" />
  <BarList title="Action breakdown" rows={actionRows} />
  <BarList title="Protocol breakdown" rows={protocolRows} />
  <BarList title="Top talkers (source IP)" rows={topTalkerRows} />
  <BarList title="Event volume by device" rows={deviceRows} emptyMessage="No devices seen yet" />
</div>

<style>
  .dashboard {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(360px, 1fr));
    gap: 14px;
    padding: 14px;
    overflow-y: auto;
    align-content: start;
  }

  .panel.wide {
    grid-column: 1 / -1;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
  }

  /* 360px cards plus the grid's own 14px padding no longer fit under
     ~390px viewports, forcing a horizontal scrollbar on the modal for a
     single-column layout that doesn't need one. */
  @media (max-width: 420px) {
    .dashboard {
      grid-template-columns: 1fr;
      padding: 10px;
      gap: 10px;
    }
  }
</style>
