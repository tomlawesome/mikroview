<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // One user-defined top-talkers widget: its own independent filter (not
  // the live view's current FilterBar state -- see appState.filteredBy)
  // grouped by whichever dimension the widget was saved with.
  import { appState } from '../lib/state.svelte'
  import { topNBy } from '../lib/topN'
  import { groupByKey, GROUP_BY_LABELS } from '../lib/groupBy'
  import { topTalkerWidgetsState, type TopTalkerWidget } from '../lib/topTalkers.svelte'
  import BarList from './BarList.svelte'

  let { widget }: { widget: TopTalkerWidget } = $props()

  const TOP_N = 10

  const rows = $derived(
    topNBy(appState.filteredBy(widget.filters), (e) => groupByKey(widget.groupBy, e), TOP_N),
  )
</script>

<div class="widget">
  <button
    class="remove"
    onclick={() => topTalkerWidgetsState.remove(widget.id)}
    title="Remove this widget"
    aria-label="Remove {widget.title}"
  >
    ✕
  </button>
  <BarList
    title="{widget.title} — by {GROUP_BY_LABELS[widget.groupBy]}"
    {rows}
    emptyMessage="No matching events yet"
  />
</div>

<style>
  .widget {
    position: relative;
  }

  .remove {
    position: absolute;
    top: 12px;
    right: 14px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 20px;
    height: 20px;
    font-size: 11px;
    line-height: 1;
    z-index: 1;
  }

  .remove:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
