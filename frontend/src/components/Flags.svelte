<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { formatHM, countryFlag } from '../lib/format'
  import ReputationDetails from './ReputationDetails.svelte'
  import BarList from './BarList.svelte'
  import type { Flag, FlagType } from '../lib/types'

  // Same gate NavMenu uses for the Detectors view.
  const isAdminOrOpen = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // Which flag's split-Clear dropdown is open, if any -- one shared id
  // rather than per-card state, since at most one can be open at a time
  // and this list can be long. Closed on an outside click, Escape, or
  // picking the menu item (issue #198).
  let openClearMenuFor: string | null = $state(null)

  function toggleClearMenu(id: string) {
    openClearMenuFor = openClearMenuFor === id ? null : id
  }

  function onDocClickCloseClearMenu(e: MouseEvent) {
    if (!(e.target as HTMLElement).closest('.split-clear')) openClearMenuFor = null
  }

  function onKeydownCloseClearMenu(e: KeyboardEvent) {
    if (e.key === 'Escape') openClearMenuFor = null
  }

  $effect(() => {
    if (!openClearMenuFor) return
    document.addEventListener('click', onDocClickCloseClearMenu)
    document.addEventListener('keydown', onKeydownCloseClearMenu)
    return () => {
      document.removeEventListener('click', onDocClickCloseClearMenu)
      document.removeEventListener('keydown', onKeydownCloseClearMenu)
    }
  })

  // "Clear all" (issue #198): first click arms it (red, "Confirm"); the
  // second click on that same now-red button is the confirmation -- no
  // modal, because the second click *is* the deliberate second action.
  // Disarms itself after CLEAR_ALL_ARM_MS or when the pointer/focus
  // leaves, so an armed-but-abandoned state can't be triggered later by
  // an unrelated click landing back on the button.
  const CLEAR_ALL_ARM_MS = 4000
  let clearAllArmed = $state(false)
  let clearAllArmTimer: ReturnType<typeof setTimeout> | null = null
  let clearAllBusy = $state(false)

  function disarmClearAll() {
    clearAllArmed = false
    if (clearAllArmTimer) {
      clearTimeout(clearAllArmTimer)
      clearAllArmTimer = null
    }
  }

  async function onClearAllClick() {
    if (!clearAllArmed) {
      clearAllArmed = true
      clearAllArmTimer = setTimeout(disarmClearAll, CLEAR_ALL_ARM_MS)
      return
    }
    disarmClearAll()
    clearAllBusy = true
    try {
      await flagsState.clearAll()
    } finally {
      clearAllBusy = false
    }
  }

  let expandedId: string | null = $state(null)

  function toggleExpanded(id: string) {
    expandedId = expandedId === id ? null : id
  }

  // Which source IP's campaign card (see below) is currently expanded to
  // show its individual member flags -- null means every campaign card
  // is collapsed to just its summary row.
  let expandedGroup: string | null = $state(null)

  function toggleGroup(sourceIp: string) {
    expandedGroup = expandedGroup === sourceIp ? null : sourceIp
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

  // Same labels FlagsChart.svelte/Exclusions.svelte use -- duplicated
  // rather than shared, matching how ACTION_LABELS is already
  // independently duplicated in both EventsChart.svelte and
  // Dashboard.svelte in this codebase.
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
    known_bad_ip: 'Known-bad IP (blocklist match)',
  }

  // Sorted by firstSeen (not the fetch response's lastSeen-desc order --
  // see internal/flags.Store.List()) so a flag's position is fixed the
  // moment it first appears. lastSeen updates on every re-fire, not just
  // creation, so sorting by it made an already-visible flag you're
  // reading jump to the top of the list the instant it (or anything
  // else) re-fired on the next 5s poll -- jarring for something you're
  // mid-read on. Only a genuinely new flag entering the active set now
  // changes the ordering, which is the expected kind of layout change.
  const active = $derived(
    flagsState.list
      .filter((f) => !f.cleared)
      .sort((a, b) => new Date(b.firstSeen).getTime() - new Date(a.firstSeen).getTime()),
  )
  const cleared = $derived(flagsState.list.filter((f) => f.cleared).slice(0, 20))

  // "One actor, several signals" (issue #106): active flags sharing a
  // normalized source IP (flagsState.groupedBySource -- see that
  // derived's own doc comment for exactly which target shapes qualify)
  // collapse into a single campaign card instead of N separate cards,
  // in the same firstSeen-desc order `active` already uses. Each source
  // IP is represented once, at the position of its most-recent flag;
  // everything ungroupable (a lone flag from that source, or a target
  // with no single source IP to correlate on at all) renders exactly as
  // before.
  type ActiveItem = { kind: 'single'; flag: Flag } | { kind: 'group'; sourceIp: string; flags: Flag[] }

  const activeItems = $derived.by((): ActiveItem[] => {
    const seen = new Set<string>()
    const items: ActiveItem[] = []
    for (const f of active) {
      const ip = extractSourceIp(f.target)
      const group = ip ? flagsState.groupedBySource.get(ip) : undefined
      if (ip && group) {
        if (seen.has(ip)) continue
        seen.add(ip)
        items.push({ kind: 'group', sourceIp: ip, flags: group })
      } else {
        items.push({ kind: 'single', flag: f })
      }
    }
    return items
  })

  function groupTypeLabels(flags: Flag[]): string {
    return [...new Set(flags.map((f) => TYPE_LABELS[f.type]))].join(' · ')
  }

  function groupFirstSeen(flags: Flag[]): string {
    return flags.reduce((min, f) => (new Date(f.firstSeen) < new Date(min) ? f.firstSeen : min), flags[0].firstSeen)
  }

  function groupLastSeen(flags: Flag[]): string {
    return flags.reduce((max, f) => (new Date(f.lastSeen) > new Date(max) ? f.lastSeen : max), flags[0].lastSeen)
  }

  function filterToSource(sourceIp: string) {
    appState.setFilter('ip', sourceIp)
    appState.view = 'live'
  }

  // "Active flags by type" summary panel -- only types with at least one
  // active flag, ranked by count like every other BarList panel.
  const typeBreakdown = $derived(
    Object.entries(
      active.reduce<Partial<Record<FlagType, number>>>((counts, f) => {
        counts[f.type] = (counts[f.type] ?? 0) + 1
        return counts
      }, {}),
    )
      .map(([type, count]) => ({ label: TYPE_LABELS[type as FlagType], count: count ?? 0 }))
      .sort((a, b) => b.count - a.count),
  )

  // What a flag's target actually *is* varies by detector -- most are a
  // plain source IP, but distributed_brute_force is keyed by port,
  // rule_spike/stale_rule by rule label, repeated_drops by
  // "ip -> port N", device_silence by a device ID, and global_spike has
  // no filterable target at all. new_device's target is a MAC address
  // (see internal/flags.TypeNewDevice) -- the live view's Filters has no
  // MAC field to filter on, so it's not filterable either, same as
  // global_spike. Filtering on the right field (rather than always
  // assuming "ip") is what makes this click-through actually land on a
  // sensible pre-filtered view.
  function isFilterable(f: Flag): boolean {
    return f.type !== 'global_spike' && f.type !== 'new_device'
  }

  function filterToTarget(f: Flag) {
    switch (f.type) {
      case 'port_scan':
      case 'activity_spike':
      case 'critical_port':
      case 'outbound_anomaly':
      case 'internal_recon':
      case 'low_slow_scan':
      case 'off_hours_activity':
      case 'unexpected_mail_sender':
      case 'known_bad_ip':
        appState.setFilter('ip', f.target)
        break
      case 'distributed_brute_force':
        appState.setFilter('port', f.target.replace(/^port /, ''))
        break
      case 'rule_spike':
      case 'stale_rule':
        appState.setFilter('rule', f.target)
        break
      case 'repeated_drops':
        appState.setFilter('ip', f.target.split(' -> ')[0])
        break
      case 'device_silence':
        appState.setFilter('device', f.target)
        break
      case 'global_spike':
      case 'new_device':
        return
    }
    appState.view = 'live'
  }

  async function clear(id: string) {
    await flagsState.clear(id)
  }

  // "Clear and never flag this again" -- permanently excludes this
  // flag's exact (Type, Target) going forward (see internal/flags.
  // Store.Exclude's doc comment for why this is a deliberate permanent
  // suppression, not a timed snooze). Reviewing/undoing an exclusion
  // made by mistake is the admin-only "Manage exclusions" panel below,
  // not a confirmation dialog here.
  async function clearPermanent(id: string) {
    await flagsState.clearPermanent(id)
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
  <BarList title="Active flags by type" rows={typeBreakdown} emptyMessage="Nothing flagged right now." />

  {#snippet flagCard(f: Flag)}
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
      <div class="actions">
        {#if isAdminOrOpen}
          <!-- Split button: the main segment is exactly today's Clear.
               The arrow segment is admin-only, matching the backend's
               own gate on POST /api/flags/{id}/clear-permanent -- a
               permanent exclusion suppresses detection until someone
               undoes it, unlike the plain Clear beside it. A non-admin
               gets a plain Clear button with no arrow at all (below),
               rather than a disabled one that would just advertise an
               action they can't take (issue #198). -->
          <div class="split-clear" class:menu-open={openClearMenuFor === f.id}>
            <button class="clear split-main" onclick={() => clear(f.id)}>Clear</button>
            <button
              class="clear split-arrow"
              aria-haspopup="true"
              aria-expanded={openClearMenuFor === f.id}
              aria-label="More clear options for this flag"
              onclick={() => toggleClearMenu(f.id)}
            >
              ▾
            </button>
            {#if openClearMenuFor === f.id}
              <div class="split-menu" role="menu">
                <button
                  class="split-menu-item"
                  role="menuitem"
                  title="Clear this flag and permanently stop {TYPE_LABELS[f.type]} from ever raising again for {f.target} -- reversible from the Exclusions page (see the menu)."
                  onclick={() => {
                    openClearMenuFor = null
                    clearPermanent(f.id)
                  }}
                >
                  Permanently clear
                </button>
              </div>
            {/if}
          </div>
        {:else}
          <button class="clear" onclick={() => clear(f.id)}>Clear</button>
        {/if}
      </div>
    </li>
  {/snippet}

  <section aria-labelledby="active-heading">
    <div class="active-header">
      <h2 id="active-heading">Active ({active.length})</h2>
      {#if active.length > 0}
        <!-- Click-again confirm, not a modal: the second click on this
             same now-red button is the confirmation, which is what
             makes a single accidental click harmless while still
             asserting real intent for the second one (issue #198).
             Regular clears only -- see flagsState.clearAll's doc
             comment for why there is no permanent variant. -->
        <button
          class="clear-all"
          class:armed={clearAllArmed}
          disabled={clearAllBusy}
          onclick={onClearAllClick}
          onblur={disarmClearAll}
          onpointerleave={disarmClearAll}
          title={clearAllArmed
            ? 'Click again to clear every active flag'
            : 'Clear every active flag -- regular clears only, click again to confirm'}
        >
          {clearAllArmed ? 'Confirm' : 'Clear all'}
        </button>
      {/if}
    </div>
    {#if active.length === 0}
      <p class="empty">Nothing flagged right now.</p>
    {:else}
      <ul class="list">
        {#each activeItems as item (item.kind === 'group' ? `group:${item.sourceIp}` : item.flag.id)}
          {#if item.kind === 'single'}
            {@render flagCard(item.flag)}
          {:else}
            <li class="card campaign-card">
              <div class="campaign-header">
                <button
                  class="campaign-toggle"
                  onclick={() => toggleGroup(item.sourceIp)}
                  aria-expanded={expandedGroup === item.sourceIp}
                >
                  <span class="campaign-caret">{expandedGroup === item.sourceIp ? '▾' : '▸'}</span>
                  <span class="campaign-count">{item.flags.length} related flags from this source</span>
                </button>
                <button
                  class="target campaign-source"
                  onclick={() => filterToSource(item.sourceIp)}
                  title="Filter the live view to {item.sourceIp}"
                >
                  {item.sourceIp}
                </button>
              </div>
              <div class="campaign-summary">
                <span class="campaign-types">{groupTypeLabels(item.flags)}</span>
                <span>first seen {formatHM(groupFirstSeen(item.flags))}</span>
                <span>last seen {formatHM(groupLastSeen(item.flags))}</span>
              </div>
              {#if expandedGroup === item.sourceIp}
                <ul class="list campaign-members">
                  {#each item.flags as f (f.id)}
                    {@render flagCard(f)}
                  {/each}
                </ul>
              {/if}
            </li>
          {/if}
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

  {#if isAdminOrOpen}
    <!-- Moved to its own page (issue #207): reaching and reviewing
         exclusions underneath a potentially large active-flags list was
         a pain. Left as a pointer here rather than removed outright, so
         the path stays discoverable from where the permanent-clear
         action itself lives. -->
    <p class="exclusions-pointer">
      Permanently-excluded (detector, target) pairs are reviewed on the
      <button class="link" onclick={() => (appState.view = 'exclusions')}>Exclusions</button>
      page (also in the menu).
    </p>
  {/if}
</div>

<style>
  .flags {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px;
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
    padding: 10px 150px 10px 12px;
  }

  .cleared-card {
    opacity: 0.7;
    padding-right: 12px;
  }

  /* No single Clear action at the campaign level (clearing happens per
     member flag, inside the expanded list below), so unlike a plain
     .card it doesn't need to reserve .clear's right-hand padding. */
  .campaign-card {
    padding: 10px 12px;
  }

  .campaign-header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  .campaign-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: none;
    padding: 0;
    color: var(--fg);
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .campaign-caret {
    color: var(--fg-muted);
    font-size: 11px;
    width: 10px;
    display: inline-block;
  }

  .campaign-count {
    color: var(--fg);
  }

  .campaign-source.target {
    font-weight: 600;
  }

  .campaign-summary {
    margin-top: 6px;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    font-size: 12px;
    color: var(--fg-dim);
  }

  .campaign-types {
    color: var(--fg-muted);
  }

  .campaign-members {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
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

  .actions {
    position: absolute;
    top: 10px;
    right: 12px;
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 6px;
    width: 138px;
  }

  .clear {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 5px 10px;
    font-size: 12px;
    white-space: nowrap;
  }

  .clear:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .clear:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* Split button: .split-main is today's plain Clear, unchanged in
     behaviour and appearance. .split-arrow opens the dropdown holding
     the one permanent action -- kept visually distinct (the drop tint)
     so its warning colour, not just its position, marks it as the more
     deliberate one. */
  .split-clear {
    position: relative;
    display: flex;
  }

  .split-main {
    flex: 1;
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
    border-right: none;
  }

  .split-arrow {
    flex: none;
    width: 26px;
    padding: 5px 0;
    font-size: 10px;
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    color: var(--drop);
    border-color: var(--drop);
  }

  .split-arrow:hover,
  .split-clear.menu-open .split-arrow {
    background: var(--drop-bg);
  }

  .split-menu {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    min-width: 160px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 4px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 5;
  }

  .split-menu-item {
    display: block;
    width: 100%;
    text-align: left;
    background: transparent;
    border: none;
    color: var(--drop);
    padding: 7px 9px;
    border-radius: 5px;
    font-size: 12px;
    white-space: nowrap;
    cursor: pointer;
  }

  .split-menu-item:hover {
    background: var(--drop-bg);
  }

  .active-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 10px;
  }

  .active-header h2 {
    margin: 0;
  }

  .clear-all {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 6px 12px;
    font-size: 12px;
    white-space: nowrap;
  }

  .clear-all:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  /* Armed state: the button itself turns into the confirmation -- no
     modal, because this red/"Confirm" state IS the second, deliberate
     step (issue #198). */
  .clear-all.armed {
    background: var(--drop-bg);
    color: var(--drop);
    border-color: var(--drop);
    font-weight: 600;
  }

  .clear-all:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .exclusions-pointer {
    margin: 0;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--accent);
    text-decoration: underline;
    cursor: pointer;
  }
</style>
