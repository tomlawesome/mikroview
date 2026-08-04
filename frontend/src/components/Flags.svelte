<script lang="ts">
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  import { flagsState } from '../lib/flags.svelte'
  import { appState } from '../lib/state.svelte'
  import { formatHM } from '../lib/format'
  import type { Flag, FlagType } from '../lib/types'

  const TYPE_LABELS: Record<FlagType, string> = {
    port_scan: 'Port scan',
    activity_spike: 'Activity spike',
    critical_port: 'Critical-port attempts',
    global_spike: 'Network-wide volume spike',
  }

  const active = $derived(flagsState.list.filter((f) => !f.cleared))
  const cleared = $derived(flagsState.list.filter((f) => f.cleared).slice(0, 20))

  function filterToTarget(f: Flag) {
    if (f.target === 'global') return
    appState.setFilter('ip', f.target)
    flagsState.open = false
  }

  async function clear(id: string) {
    await flagsState.clear(id)
  }
</script>

<div class="flags scrollbar">
  <section aria-labelledby="active-heading">
    <h2 id="active-heading">Active ({active.length})</h2>
    {#if active.length === 0}
      <p class="empty">Nothing flagged right now.</p>
    {:else}
      <ul class="list">
        {#each active as f (f.id)}
          <li class="card">
            <div class="card-main">
              <span class="type">{TYPE_LABELS[f.type]}</span>
              {#if f.target !== 'global'}
                <button class="target" onclick={() => filterToTarget(f)} title="Filter the live view to {f.target}">
                  {f.target}
                </button>
              {:else}
                <span class="target target-global">network-wide</span>
              {/if}
            </div>
            <p class="detail">{f.detail}</p>
            <div class="meta">
              <span>first seen {formatHM(f.firstSeen)}</span>
              <span>last seen {formatHM(f.lastSeen)}</span>
              <span>fired {f.count}×</span>
            </div>
            <button class="clear" onclick={() => clear(f.id)}>Clear</button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section aria-labelledby="cleared-heading">
    <h2 id="cleared-heading">Recently cleared</h2>
    {#if cleared.length === 0}
      <p class="empty">No cleared flags yet.</p>
    {:else}
      <ul class="list">
        {#each cleared as f (f.id)}
          <li class="card cleared-card">
            <div class="card-main">
              <span class="type">{TYPE_LABELS[f.type]}</span>
              <span class="target">{f.target === 'global' ? 'network-wide' : f.target}</span>
            </div>
            <p class="detail">{f.detail}</p>
            <div class="meta">
              <span>cleared {f.clearedAt ? formatHM(f.clearedAt) : ''}</span>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .flags {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  h2 {
    margin: 0 0 10px;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .card {
    position: relative;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 90px 10px 12px;
  }

  .cleared-card {
    opacity: 0.7;
    padding-right: 12px;
  }

  .card-main {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .type {
    font-size: 12px;
    font-weight: 600;
    color: var(--accent);
    background: var(--accent-bg);
    border-radius: 4px;
    padding: 2px 7px;
  }

  .target {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg);
  }

  button.target {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  button.target:hover {
    text-decoration-color: currentColor;
  }

  .target-global {
    color: var(--fg-muted);
  }

  .detail {
    margin: 6px 0 0;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .meta {
    margin-top: 6px;
    display: flex;
    gap: 12px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .clear {
    position: absolute;
    top: 10px;
    right: 12px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 5px 10px;
    font-size: 12px;
  }

  .clear:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
</style>
