<script lang="ts">
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  import { flagsState } from '../lib/flags.svelte'
  import { appState } from '../lib/state.svelte'
  import { formatHM, countryFlag } from '../lib/format'
  import ReputationDetails from './ReputationDetails.svelte'
  import type { Flag, FlagType } from '../lib/types'

  let expandedId: string | null = $state(null)

  function toggleExpanded(id: string) {
    expandedId = expandedId === id ? null : id
  }

  // Only true when there's actually something beyond `detail` to show --
  // avoids a dead "Details" button on flags with nothing extra (most
  // global_spike/rule_spike flags, or any flag when no reputation key is
  // configured).
  function hasExpandableDetail(f: Flag): boolean {
    return (
      !!f.country ||
      !!f.reputation ||
      !!f.evidence?.ports?.length ||
      !!f.evidence?.hosts?.length ||
      !!f.evidence?.nat
    )
  }

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
  }

  const active = $derived(flagsState.list.filter((f) => !f.cleared))
  const cleared = $derived(flagsState.list.filter((f) => f.cleared).slice(0, 20))

  // What a flag's target actually *is* varies by detector -- most are a
  // plain source IP, but distributed_brute_force is keyed by port,
  // rule_spike by rule label, repeated_drops by "ip -> port N", and
  // global_spike has no filterable target at all. Filtering on the
  // right field (rather than always assuming "ip") is what makes this
  // click-through actually land on a sensible pre-filtered view.
  function isFilterable(f: Flag): boolean {
    return f.type !== 'global_spike'
  }

  function filterToTarget(f: Flag) {
    switch (f.type) {
      case 'port_scan':
      case 'activity_spike':
      case 'critical_port':
      case 'outbound_anomaly':
      case 'internal_recon':
        appState.setFilter('ip', f.target)
        break
      case 'distributed_brute_force':
        appState.setFilter('port', f.target.replace(/^port /, ''))
        break
      case 'rule_spike':
        appState.setFilter('rule', f.target)
        break
      case 'repeated_drops':
        appState.setFilter('ip', f.target.split(' -> ')[0])
        break
      case 'global_spike':
        return
    }
    flagsState.open = false
  }

  async function clear(id: string) {
    await flagsState.clear(id)
  }

  // Graded rather than a single color for every value -- a 12% confidence
  // score and a 95% one shouldn't read as equally worth attention at a
  // glance, mirroring the severity coloring ActionBadge already uses
  // elsewhere.
  function confidenceTier(c: number): 'low' | 'medium' | 'high' {
    if (c >= 70) return 'high'
    if (c >= 40) return 'medium'
    return 'low'
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
              {#if f.confidence != null}
                <span
                  class="confidence confidence-{confidenceTier(f.confidence)}"
                  title="How confident this specific flag is, based on how much history backs it and how far it deviates from normal -- not how confident mikroview is overall"
                >
                  {f.confidence}% confidence
                </span>
              {/if}
              {#if isFilterable(f)}
                <button class="target" onclick={() => filterToTarget(f)} title="Filter the live view to {f.target}">
                  {f.target}
                </button>
              {:else}
                <span class="target target-global">network-wide</span>
              {/if}
              {#if f.country}
                <span class="country" title={f.country}>{countryFlag(f.country)}</span>
              {/if}
            </div>
            <p class="detail">{f.detail}</p>
            <div class="meta">
              <span>first seen {formatHM(f.firstSeen)}</span>
              <span>last seen {formatHM(f.lastSeen)}</span>
              <span>fired {f.count}×</span>
              {#if hasExpandableDetail(f)}
                <button class="details-toggle" onclick={() => toggleExpanded(f.id)}>
                  {expandedId === f.id ? 'Hide details' : 'Details'}
                </button>
              {/if}
            </div>
            {#if expandedId === f.id}
              <div class="expanded">
                {#if f.evidence?.ports?.length}
                  <div class="ev-row">
                    <span class="ev-label">Ports touched</span>
                    <span class="ev-value">{f.evidence.ports.join(', ')}</span>
                  </div>
                {/if}
                {#if f.evidence?.hosts?.length}
                  <div class="ev-row">
                    <span class="ev-label">Hosts involved</span>
                    <span class="ev-value">{f.evidence.hosts.join(', ')}</span>
                  </div>
                {/if}
                {#if f.evidence?.nat}
                  <div class="ev-row">
                    <span class="ev-label">NAT</span>
                    <span class="ev-value">
                      {f.evidence.nat.ip}{f.evidence.nat.port ? `:${f.evidence.nat.port}` : ''}
                      {#if f.evidence.nat.raw}<br /><span class="ev-raw">{f.evidence.nat.raw}</span>{/if}
                    </span>
                  </div>
                {/if}
                {#if f.reputation}
                  <ReputationDetails result={f.reputation} />
                {/if}
              </div>
            {/if}
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

  .confidence {
    font-size: 11px;
    font-weight: 600;
    border-radius: 4px;
    padding: 2px 7px;
  }

  .confidence-low {
    color: var(--fg-muted);
    background: var(--bg-hover);
  }

  .confidence-medium {
    color: var(--drop);
    background: var(--drop-bg);
  }

  .confidence-high {
    color: var(--reject);
    background: var(--reject-bg);
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

  .country {
    font-size: 14px;
  }

  .meta {
    margin-top: 6px;
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .details-toggle {
    background: transparent;
    border: none;
    color: var(--accent);
    padding: 0;
    font-size: 12px;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .details-toggle:hover {
    text-decoration-color: currentColor;
  }

  .expanded {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .ev-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    font-size: 13px;
  }

  .ev-label {
    color: var(--fg-muted);
    flex: none;
  }

  .ev-value {
    color: var(--fg);
    text-align: right;
    overflow-wrap: anywhere;
    font-family: var(--font-mono);
  }

  .ev-raw {
    font-size: 11px;
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
