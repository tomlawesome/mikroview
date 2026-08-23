<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { formatHM, countryFlag, isPublicIp } from '../lib/format'
  import { flagLayoutState, type FlagColumns } from '../lib/flagLayout.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import ReputationDetails from './ReputationDetails.svelte'
  import BarList from './BarList.svelte'
  import IpInvestigateButton from './IpInvestigateButton.svelte'
  import type { Flag, FlagType } from '../lib/types'

  // Same gate the rail uses for the Detectors view.
  const isAdminOrOpen = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // lib/flags.svelte.ts's clear/clearAll/clearPermanent optimistically
  // update, then roll back and *rethrow* on failure. None of the call
  // sites below caught that, so a transient 500 or an expired session
  // became an unhandled rejection: the flag reappeared with no
  // explanation, which reads as the button not having worked rather
  // than as an error. Reported the same way Watchlist and Entities
  // report theirs.
  let error = $state<string | null>(null)

  function reportFailure(action: string, err: unknown) {
    error = err instanceof Error ? `${action}: ${err.message}` : `${action} failed`
  }

  // The stored preference collapses to 1 below the shared mobile
  // breakpoint regardless of what's selected (issue #199's responsive
  // floor) -- computed here in JS rather than as a CSS media query, so
  // it reuses viewportState's one 700px breakpoint (the same value
  // Toolbar/ThemeMenu already switch on) instead of a second
  // hardcoded copy of it, and so the *card* content also reverts to its
  // full, non-compact detail at exactly the width the grid itself
  // renders as one column. A CSS-only floor would narrow the grid but
  // leave the compact card styling active, which is the "unusably
  // narrow card" the floor exists to prevent, just moved one level down.
  const effectiveColumns = $derived<FlagColumns>(viewportState.isMobile ? 1 : flagLayoutState.columns)
  const compact = $derived(effectiveColumns > 1)

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
    error = null
    try {
      await flagsState.clearAll()
    } catch (err) {
      reportFailure('Could not clear all flags', err)
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

  // The IP for a live abuse-check button on this card (issue #213), or
  // null if there is none worth checking. extractSourceIp already
  // screens out every target shape that isn't a bare IP (a rule label,
  // "port N", "global", a MAC) -- see its own doc comment -- so most
  // exclusions fall out of that for free rather than needing a second
  // type-by-type list to keep in step with filterToTarget's.
  //
  // device_silence is the one type that needs an explicit exclusion on
  // top of the shape check: an auto-discovered device's ID defaults to
  // its source IP (internal/device.Registry.Resolve), so its target can
  // be IP-shaped too -- but it identifies the device that went quiet,
  // not a source worth threat-checking, and #213 excludes it by name.
  function investigateIp(f: Flag): string | null {
    if (f.type === 'device_silence') return null
    const ip = extractSourceIp(f.target)
    return ip && isPublicIp(ip) ? ip : null
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
    error = null
    try {
      await flagsState.clear(id)
    } catch (err) {
      reportFailure('Could not clear this flag', err)
    }
  }

  // "Clear and never flag this again" -- permanently excludes this
  // flag's exact (Type, Target) going forward (see internal/flags.
  // Store.Exclude's doc comment for why this is a deliberate permanent
  // suppression, not a timed snooze). Reviewing/undoing an exclusion
  // made by mistake is the admin-only "Manage exclusions" panel below,
  // not a confirmation dialog here.
  async function clearPermanent(id: string) {
    error = null
    try {
      await flagsState.clearPermanent(id)
    } catch (err) {
      reportFailure('Could not permanently clear this flag', err)
    }
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
  {#if error}
    <p class="mutation-error" role="alert">{error}</p>
  {/if}

  <BarList title="Active flags by type" rows={typeBreakdown} emptyMessage="Nothing flagged right now." />

  {#snippet flagCard(f: Flag, compactCard: boolean = false)}
    {@const investigate = investigateIp(f)}
    <li class="card" class:compact={compactCard}>
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
        {#if investigate}
          <!-- A fresh check, not the frozen raise-time snapshot below
               (issue #213): raw events aren't persisted, so an old or
               cleared flag often has nothing left in the live view to
               click into -- this is what makes "what does it look like
               now" reachable without leaving the page. Reuses the exact
               component/lookup path EventRow/EventDetailSheet already
               use; the snapshot in Details stays as-is and answers a
               different question ("what did it look like when it
               fired"). -->
          <IpInvestigateButton ip={investigate} />
        {/if}
        {#if f.country}
          <span class="country" title={f.country}>{countryFlag(f.country)}</span>
        {/if}
      </div>
      <!-- Compact (2/3 columns, issue #199): the detail line truncates to
           one line rather than wrapping and pushing the card taller than
           its neighbours in the same grid row -- the type/target above
           and the expand affordance below stay fully visible either way,
           so nothing identifying is lost, only the free-text summary. -->
      <p class="detail" title={compactCard ? f.detail : undefined}>{f.detail}</p>
      <div class="meta">
        {#if !compactCard}
          <span>first seen {formatHM(f.firstSeen)}</span>
        {/if}
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
      <div class="header-controls">
        <!-- 1/2/3-column density (issue #199), persisted per browser.
             Below the shared mobile breakpoint this stays selectable but
             stops changing the render -- see effectiveColumns' own
             comment for why the floor lives there rather than only in a
             media query. -->
        <div class="layout-select" role="radiogroup" aria-label="Card layout columns">
          {#each [1, 2, 3] as const as n (n)}
            <button
              class="layout-option"
              class:active={flagLayoutState.columns === n}
              role="radio"
              aria-checked={flagLayoutState.columns === n}
              onclick={() => flagLayoutState.set(n)}
              title="{n} column{n > 1 ? 's' : ''}"
            >
              {n}
            </button>
          {/each}
        </div>
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
    </div>
    {#if active.length === 0}
      <p class="empty">Nothing flagged right now.</p>
    {:else}
      <ul class="list card-grid" style="--flag-columns: {effectiveColumns}">
        {#each activeItems as item (item.kind === 'group' ? `group:${item.sourceIp}` : item.flag.id)}
          {#if item.kind === 'single'}
            {@render flagCard(item.flag, compact)}
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
                    {@render flagCard(f, compact)}
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
      <!-- Same column setting as the active list above (issue #199's
           "secondary" note) -- no independent control here, one
           preference for the whole page reads simpler than two. -->
      <ul class="list card-grid" style="--flag-columns: {effectiveColumns}">
        {#each cleared as f (f.id)}
          <li class="card cleared-card" class:compact>
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

  /* Same treatment Watchlist and Entities give their own mutation
     errors, so a failed clear reads the same way everywhere. */
  .mutation-error {
    margin: 0;
    color: var(--reject);
    font-size: 12px;
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

  /* 1/2/3-column density (issue #199). Only the two top-level lists
     (active, cleared) get this -- .campaign-members (a campaign's
     expanded member list, nested one level inside a single grid cell)
     stays the plain flex column above regardless of the page's column
     setting, since it's already-indented content, not another row of
     the same grid. minmax(0, 1fr), not 1fr alone, so a long unbroken
     target/detail string can't force a column wider than its share and
     blow out the grid -- a bare 1fr lets content overflow its track. */
  .card-grid {
    display: grid;
    grid-template-columns: repeat(var(--flag-columns, 1), minmax(0, 1fr));
  }

  .card {
    position: relative;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 150px 10px 12px;
  }

  /* Compact (2/3 columns, issue #199). The 150px right-reserve above
     exists only to make room for .actions floating in the corner --
     narrower cards don't have that much spare width to give up, so
     .actions moves into normal flow at the bottom instead (see below)
     and the reserve is dropped along with it. */
  .card.compact {
    padding: 8px 10px;
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

  /* Flows below the card content instead of floating in the corner --
     a fixed-width floating box is what the 150px reserve above was
     for, and a compact card doesn't have that to spare. Full-width
     here also means the split button (see .split-clear) always has
     room regardless of how narrow the column gets, rather than needing
     its own per-density size math. */
  .card.compact .actions {
    position: static;
    width: auto;
    flex-direction: row;
    margin-top: 8px;
  }

  .card.compact .actions .split-clear,
  .card.compact .actions > .clear {
    flex: 1;
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
    flex-wrap: wrap;
  }

  .active-header h2 {
    margin: 0;
  }

  .header-controls {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .layout-select {
    display: flex;
    border: 1px solid var(--border);
    border-radius: 5px;
    overflow: hidden;
  }

  .layout-option {
    background: transparent;
    border: none;
    border-left: 1px solid var(--border);
    color: var(--fg-muted);
    padding: 5px 11px;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    cursor: pointer;
  }

  .layout-option:first-child {
    border-left: none;
  }

  .layout-option:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .layout-option.active {
    color: var(--accent);
    background: var(--accent-bg);
    font-weight: 600;
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
