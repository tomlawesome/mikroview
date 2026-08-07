<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Generic horizontal bar-list panel, shared by every ranked-count chart
  // on the dashboard (top rules, action/protocol breakdown, top talkers,
  // per-device volume). Extracted from what was TopRulesMenu's row markup
  // so all five panels stay visually consistent instead of each hand-
  // rolling their own bar/track/count layout.
  interface Row {
    label: string
    count: number
    colorVar?: string
  }

  interface Props {
    title: string
    rows: Row[]
    emptyMessage?: string
  }

  const { title, rows, emptyMessage = 'No data yet' }: Props = $props()

  const maxCount = $derived(rows[0]?.count ?? 0)
</script>

<div class="bar-list">
  <div class="header">{title}</div>
  {#if rows.length === 0}
    <div class="empty">{emptyMessage}</div>
  {:else}
    <div class="rows">
      {#each rows as r (r.label)}
        <div class="row">
          <span class="label">{r.label}</span>
          <span class="bar-track">
            <span
              class="bar"
              style="width: {maxCount ? (r.count / maxCount) * 100 : 0}%; background: {r.colorVar ?? 'var(--accent)'}"
            ></span>
          </span>
          <span class="count">{r.count}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .bar-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 14px 16px;
  }

  .header {
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .empty {
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 80px auto;
    align-items: center;
    gap: 10px;
    font-size: 13px;
  }

  .label {
    font-family: var(--font-mono);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .bar-track {
    height: 7px;
    border-radius: 3px;
    background: var(--bg-hover);
    overflow: hidden;
  }

  .bar {
    display: block;
    height: 100%;
    border-radius: 3px;
  }

  .count {
    min-width: 40px;
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
    text-align: right;
  }
</style>
