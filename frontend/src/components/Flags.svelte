<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Behavioral flags raised by internal/detect (see docs/configuration.md's
  // "Behavioral flags" section) -- an interrogation aid, not an IPS: every
  // action here is a human reviewing and clearing a flag, never mikroview
  // acting on traffic itself.
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { fetchFlagEpisode } from '../lib/api'
  import { familyOf } from '../lib/flagPalette'
  import { formatHM, formatTime, countryFlag, isPublicIp } from '../lib/format'
  import { flagLayoutState, type FlagColumns } from '../lib/flagLayout.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import { exclusionsState } from '../lib/exclusions.svelte'
  import { groupPairsByHost, pairsTruncated, pairsTruncationLabel } from '../lib/evidencePairs'
  import ReputationDetails from './ReputationDetails.svelte'
  import BarList from './BarList.svelte'
  import IpInvestigateButton from './IpInvestigateButton.svelte'
  import TabList from './TabList.svelte'
  import Exclusions from './Exclusions.svelte'
  import type { Flag, FlagType, FirewallEvent, Verdict } from '../lib/types'

  // Same gate the rail uses for the engine room's watchers station.
  const isAdminOrOpen = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  // #653: judging and clearing a flag are user-tier actions -- a viewer
  // may watch what mikroview is seeing but not change what it shows.
  // Absent rather than disabled, the same grammar the split button below
  // already uses for the admin-only permanent clear.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)

  // Exclusions is a tab of Flags (#547, per the ratified navigation
  // record) -- admin-only because GET/DELETE /api/flags/exclusions both
  // 403 a non-admin caller (see Exclusions.svelte's own doc comment), so
  // the tab itself is absent for a viewer rather than present-and-empty.
  // With no second tab to switch between, a viewer never sees any tab
  // chrome at all -- just the flags content, as before #547.
  type TabId = 'flags' | 'exclusions'
  let activeTab = $state<TabId>('flags')
  const tabs = $derived<{ id: string; label: string; count?: number }[]>(
    isAdminOrOpen
      ? [
          { id: 'flags', label: 'Flags' },
          // Quiet, outlined count -- never the rail's alarm-filled one,
          // which the record reserves for Flags' own open-count alone.
          // Omitted rather than shown as a permanent "0", same reasoning
          // as NavRail's own open-count badge: a count that never has
          // anything to say shouldn't sit on the tab forever.
          {
            id: 'exclusions',
            label: 'Exclusions',
            count: exclusionsState.list.length > 0 ? exclusionsState.list.length : undefined,
          },
        ]
      : [{ id: 'flags', label: 'Flags' }],
  )

  function selectTab(id: string) {
    activeTab = id as TabId
    // Exclusions.svelte stays mounted (just hidden) once switched away
    // from, unlike the standalone page it used to be, which remounted
    // -- and so refetched -- on every navigation to it. Refreshing on
    // each switch back keeps that same freshness rather than showing
    // whatever the list looked like the last time this tab was open.
    if (activeTab === 'exclusions') exclusionsState.refresh()
  }

  // Fetched here rather than centrally in App.svelte (unlike flagsState,
  // which every role needs): only an admin ever sees this tab, so only
  // an admin session should ever ask the admin-only endpoint for it.
  $effect(() => {
    if (isAdminOrOpen) exclusionsState.refresh()
  })

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

  let expandedId: string | null = $state(null)

  function toggleExpanded(f: Flag) {
    expandedId = expandedId === f.id ? null : f.id
    if (expandedId === f.id) loadEpisode(f)
  }

  // The drawer's episode (#633, rounds 18-19): the flag's own events,
  // fetched once per flag on first open via the #29 around+window
  // lookback centred on lastSeen. Cached for the component's lifetime
  // rather than refetched per open -- a drawer that redraws its episode
  // differently each time it opens reads as noise, not evidence.
  let episodes = $state<Record<string, FirewallEvent[] | 'loading' | 'error'>>({})

  // The same target mapping filterToTarget uses, as query params: which
  // server-side filter this flag's target actually is (see that
  // function's own comment). Empty for global_spike (the surge is the
  // whole window) and new_device (a MAC has no server-side match).
  function episodeParams(f: Flag): { ip?: string; port?: string; rule?: string; device?: string } {
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
        return { ip: f.target }
      case 'distributed_brute_force':
        return { port: f.target.replace(/^port /, '') }
      case 'rule_spike':
      case 'stale_rule':
        return { rule: f.target }
      case 'repeated_drops':
        return { ip: f.target.split(' -> ')[0] }
      case 'device_silence':
        return { device: f.target }
      case 'global_spike':
      case 'new_device':
        return {}
    }
  }

  async function loadEpisode(f: Flag) {
    if (episodes[f.id]) return
    episodes[f.id] = 'loading'
    try {
      const res = await fetchFlagEpisode({ ...episodeParams(f), around: f.lastSeen, window: '30m', limit: 120 })
      episodes[f.id] = res.events
    } catch {
      episodes[f.id] = 'error'
    }
  }

  // Tick positions for the episode strip, one per event, normalised
  // across the fetched span (the record's own geometry: 260-wide
  // viewBox, ticks inset 8px each side). A single event centres.
  function episodeTicks(events: FirewallEvent[]): number[] {
    const times = events
      .map((e) => new Date(e.time).getTime())
      .filter((t) => !Number.isNaN(t))
      .sort((a, b) => a - b)
    if (times.length === 0) return []
    const t0 = times[0]
    const span = times[times.length - 1] - t0
    return times.map((t) => (span === 0 ? 130 : 8 + ((t - t0) / span) * 244))
  }

  // One matched line, composed from the structured event the same way
  // the stream renders it -- raw lines are not retained, and composing
  // beats showing nothing.
  function eventLine(e: FirewallEvent): string {
    const io = [e.inInterface ? `in:${e.inInterface}` : '', e.outInterface ? `out:${e.outInterface}` : '']
      .filter(Boolean)
      .join(' ')
    const proto = e.protocol ? ` proto ${e.protocol.toUpperCase()}` : ''
    const src = e.srcIp ? `${e.srcIp}${e.srcPort ? `:${e.srcPort}` : ''}` : ''
    const dst = e.dstIp ? `${e.dstIp}${e.dstPort ? `:${e.dstPort}` : ''}` : ''
    const flow = src && dst ? `, ${src}->${dst}` : ''
    return `${formatTime(e.time)} ${e.action}|${e.ruleLabel}| ${e.chain}: ${io}${proto}${flow}`
  }

  // Which source IP's campaign card (see below) is currently expanded to
  // show its individual member flags -- null means every campaign card
  // is collapsed to just its summary row.
  let expandedGroup: string | null = $state(null)

  function toggleGroup(sourceIp: string) {
    expandedGroup = expandedGroup === sourceIp ? null : sourceIp
  }

  // Same labels Exclusions.svelte and lib/metricsSeries.ts use --
  // duplicated rather than shared, which is the long-standing convention
  // for these two tables in this codebase.
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

  // A custom detection's type is its author's own name for it -- the
  // honest label, not a key the sixteen-entry table above could know.
  const labelFor = (t: FlagType) => TYPE_LABELS[t] ?? t

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

  // The honest cleared state (round 26): when nothing is open, say when
  // the last clear happened rather than pretending nothing ever fired.
  // Null when no flag has ever been cleared -- then "nothing open" is
  // the whole truth and carries no timestamp.
  const lastClearedAt = $derived.by((): string | null => {
    let latest: string | null = null
    for (const f of cleared) {
      if (f.clearedAt && (!latest || new Date(f.clearedAt) > new Date(latest))) latest = f.clearedAt
    }
    return latest
  })

  function clearedWhen(iso: string): string {
    const d = new Date(iso)
    const now = new Date()
    const sameDay =
      d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
    return sameDay ? `today at ${formatHM(iso)}` : formatTime(iso)
  }

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
    return [...new Set(flags.map((f) => labelFor(f.type)))].join(' · ')
  }

  function groupFirstSeen(flags: Flag[]): string {
    return flags.reduce((min, f) => (new Date(f.firstSeen) < new Date(min) ? f.firstSeen : min), flags[0].firstSeen)
  }

  function groupLastSeen(flags: Flag[]): string {
    return flags.reduce((max, f) => (new Date(f.lastSeen) > new Date(max) ? f.lastSeen : max), flags[0].lastSeen)
  }

  function filterToSource(sourceIp: string) {
    appState.setFilter('srcQuery', sourceIp)
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
      .map(([type, count]) => ({ label: labelFor(type as FlagType), count: count ?? 0 }))
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
        appState.setFilter('srcQuery', f.target)
        break
      case 'distributed_brute_force':
        appState.setFilter('port', f.target.replace(/^port /, ''))
        break
      case 'rule_spike':
      case 'stale_rule':
        appState.setFilter('rule', f.target)
        break
      case 'repeated_drops':
        appState.setFilter('srcQuery', f.target.split(' -> ')[0])
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
  // made by mistake is the admin-only Exclusions tab, not a confirmation
  // dialog here.
  async function clearPermanent(id: string) {
    error = null
    try {
      await flagsState.clearPermanent(id)
      // This is what creates the exclusion the tab's own count and list
      // are reading -- before #547, that tab was a separate view that
      // remounted (and so refetched) every time you navigated to it.
      // Mounted-but-hidden doesn't get that for free, so it is refreshed
      // explicitly on the one action that changes it. selectTab below
      // covers the rest (switching to the tab, or a change made
      // elsewhere in the meantime).
      exclusionsState.refresh()
    } catch (err) {
      reportFailure('Could not permanently clear this flag', err)
    }
  }

  // Bare labels only (owner, 2026-08-30) -- no explanatory second line
  // under any of the three buttons or the badge that replaces them once
  // a flag is judged.
  const VERDICT_LABELS: Record<Verdict, string> = {
    expected: 'Expected',
    noise: 'Noise',
    real: 'Real',
  }

  // The three-button verdict row (issue #638). Every verdict posts at
  // once and is awaited here, reported the same way clear()/
  // clearPermanent() above report theirs -- 'expected'/'noise' additionally
  // clear the flag as part of that same request (flagsState.judgeAndClear),
  // 'real' does not (flagsState.judgeReal).
  async function judge(id: string, verdict: Verdict) {
    error = null
    try {
      if (verdict === 'real') {
        await flagsState.judgeReal(id, authState.username)
      } else {
        await flagsState.judgeAndClear(id, verdict)
      }
    } catch (err) {
      reportFailure('Could not record verdict', err)
    }
  }

  // Undo (issue #638) is a real DELETE now, not a cancelled timer -- see
  // flagsState.undoVerdict's own doc comment. Reported the same way as
  // every other awaited mutation on this page.
  async function undoVerdict(id: string) {
    error = null
    try {
      await flagsState.undoVerdict(id)
    } catch (err) {
      reportFailure('Could not undo this verdict', err)
    }
  }

  // "judged by X, HH:MM" -- the badge's own supporting line (issue
  // #638's "who judged it and when"). verdictBy/verdictAt are only ever
  // both present or both absent (see Flag's own doc comment), so falling
  // back to just the account name if a timestamp were somehow missing is
  // a defensive floor, not an expected path.
  function judgedByLine(f: Flag): string {
    if (!f.verdictBy) return ''
    return f.verdictAt ? `${f.verdictBy} · ${formatHM(f.verdictAt)}` : f.verdictBy
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

<div class="flags-page">
  {#if tabs.length > 1}
    <TabList {tabs} selected={activeTab} onselect={selectTab} label="Flags views" />
  {/if}
  <div
    class="flags scrollbar"
    role="tabpanel"
    id="panel-flags"
    aria-labelledby="tab-flags"
    tabindex="0"
    hidden={activeTab !== 'flags'}
  >
  {#if error}
    <p class="mutation-error" role="alert">{error}</p>
  {/if}

  <BarList title="Active flags by type" rows={typeBreakdown} emptyMessage="Nothing flagged right now." />

  {#snippet flagCard(f: Flag, compactCard: boolean = false)}
    {@const investigate = investigateIp(f)}
    {@const family = familyOf(f.type)}
    <li class="card" class:compact={compactCard} class:open={expandedId === f.id} style="--ft: {family.ink}">
      <div class="card-main">
        <span class="type">{family.mark} {labelFor(f.type)}</span>
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
      </div>
      <!-- The leading triage act (issue #638): judge, or the badge once
           judged -- on the card's face rather than in the drawer,
           because the verdict is the primary act and must not cost a
           click to reach. Clear rides the drawer's foot with the rest
           of the secondary actions. -->
      <div class="verdict-slot">
        {#if f.verdict}
          <div class="verdict-status">
            <span class="verdict-badge verdict-{f.verdict}">{VERDICT_LABELS[f.verdict]}</span>
            {#if judgedByLine(f)}
              <span class="verdict-judged-by">{judgedByLine(f)}</span>
            {/if}
          </div>
        {:else if canEdit}
          <!-- The three-button verdict row (#638) is the leading
               affordance; Clear demotes to secondary. Absent for a
               viewer (#653), never disabled. -->
          <div class="verdict-row" role="group" aria-label="Judge this flag">
            <button class="verdict-btn verdict-btn-expected" onclick={() => judge(f.id, 'expected')}>
              Expected
            </button>
            <button class="verdict-btn verdict-btn-noise" onclick={() => judge(f.id, 'noise')}>Noise</button>
            <button class="verdict-btn verdict-btn-real" onclick={() => judge(f.id, 'real')}>Real</button>
          </div>
        {/if}
      </div>
      <!-- The whole row's one affordance (rounds 18-19): the chevron
           opens the drawer; it rotates rather than swapping glyphs so
           the open state reads at a glance down a striped list. -->
      <button
        class="openc"
        aria-expanded={expandedId === f.id}
        aria-label="{expandedId === f.id ? 'Close' : 'Open'} the drawer for this flag"
        onclick={() => toggleExpanded(f)}
      >
        ▸
      </button>
      {#if expandedId === f.id}
        {@const ep = episodes[f.id]}
        <!-- The drawer (rounds 18-19): the evidence and matched lines on
             the left, the episode's shape on the right, the actions
             across the foot. The stripe runs through it unbroken -- the
             card's own left edge, not a second indented border. -->
        <div class="dwr">
          <div class="dcol">
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
            {#if f.evidence?.pairs?.length}
              <!-- #654: grouped by host -- one row per host with the
                   ports actually seen with it, never a flat host:port
                   list and never crossed against Hosts/Ports above,
                   which would silently claim combinations no event ever
                   produced. The cap is stated, not hidden: a truncated
                   sample says so rather than reading as complete. -->
              <div class="ev-row">
                <span class="ev-label">
                  Host:port pairs
                  {#if pairsTruncated(f.evidence.pairs, f.evidence.pairsTotal)}
                    <span class="ev-truncated"
                      >(showing {pairsTruncationLabel(
                        f.evidence.pairs.length,
                        f.evidence.pairsTotal ?? 0,
                        f.evidence.pairsTotalIsFloor,
                      )})</span
                    >
                  {/if}
                </span>
              </div>
              {#each groupPairsByHost(f.evidence.pairs) as g (g.host)}
                <div class="ev-row ev-pair-row">
                  <span class="ev-label">{g.host}</span>
                  <span class="ev-value">{g.ports.join(', ')}</span>
                </div>
              {/each}
            {/if}
            {#if f.evidence?.srcMac}
              <div class="ev-row">
                <span class="ev-label">Source MAC</span>
                <span class="ev-value">{f.evidence.srcMac}</span>
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
            {#if Array.isArray(ep) && ep.length > 0}
              <div class="lines">
                {#each ep.slice(0, 3) as e (e.id)}
                  <div>{eventLine(e)}</div>
                {/each}
              </div>
            {/if}
          </div>
          <div class="side">
            <span class="lab">the episode</span>
            {#if ep === 'loading'}
              <p class="ep-note">fetching the events…</p>
            {:else if ep === 'error'}
              <p class="ep-note">could not fetch the events</p>
            {:else if Array.isArray(ep) && ep.length === 0}
              <!-- Raw events are only retained in the buffer; an old
                   flag honestly says the window has moved on rather
                   than drawing an empty strip. -->
              <p class="ep-note">no matching events still buffered</p>
            {:else if Array.isArray(ep)}
              <svg
                viewBox="0 0 260 34"
                preserveAspectRatio="none"
                role="img"
                aria-label="{ep.length} events, drawn on a strip of the half hour around last seen"
              >
                {#each episodeTicks(ep) as x, i (i)}
                  <line x1={x} y1="6" x2={x} y2="28" stroke="var(--ft)" stroke-width="2.5" stroke-linecap="round" />
                {/each}
              </svg>
              <span class="span">{ep.length} events · ±30 m around last seen</span>
            {/if}
          </div>
          <div class="dwr-acts">
            {#if isFilterable(f)}
              <button class="act" onclick={() => filterToTarget(f)}>open in stream ▸</button>
            {/if}
            {#if isAdminOrOpen}
              <!-- Split button: the main segment is exactly today's Clear.
                   The arrow segment is admin-only, matching the backend's
                   own gate on POST /api/flags/{id}/clear-permanent -- a
                   permanent exclusion suppresses detection until someone
                   undoes it, unlike the plain Clear beside it. See the
                   user-tier branch below for what a user gets, and #653
                   for why a viewer gets nothing here. -->
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
                      title="Clear this flag and permanently stop {labelFor(f.type)} from ever raising again for {f.target} -- reversible from the Exclusions page (see the menu)."
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
            {:else if canEdit}
              <!-- A user (#653: below admin, above viewer) gets a plain
                   Clear with no arrow, rather than a disabled one that
                   advertises an action they cannot take (#198). A viewer
                   gets neither. -->
              <button class="clear" onclick={() => clear(f.id)}>Clear</button>
            {/if}
          </div>
        </div>
      {/if}
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
        <!-- Clear-all moved to the docket's tab row as the bubble
             (#633 round 29) -- one control, not two. Its click-again
             confirm interaction (#198) travelled with it. -->
      </div>
    </div>
    {#if active.length === 0}
      <!-- The honest cleared state (round 26): zero open is a fact with
           a history, not a blank. When something was cleared, say when,
           and stand by the audit-log promise the bubble makes. -->
      <div class="caempty">
        <span class="cae-mark">✓</span>
        <div>
          <b>Nothing open.</b>
          {#if lastClearedAt}
            Cleared {clearedWhen(lastClearedAt)} — cleared flags keep their place
            below{#if isAdminOrOpen}&nbsp;and in the
              <button class="olink" onclick={() => (appState.view = 'audit')}>audit log</button>{/if}.
          {:else}
            Nothing has been flagged yet.
          {/if}
        </div>
      </div>
    {:else}
      <ul class="list card-grid" style="--flag-columns: {effectiveColumns}">
        {#each activeItems as item (item.kind === 'group' ? `group:${item.sourceIp}` : item.flag.id)}
          {#if item.kind === 'single'}
            {@render flagCard(item.flag, compact)}
          {:else}
            <!-- The stripe wears the newest member's family ink -- a
                 campaign has no single type, and the newest member is
                 what put the card at this position in the list. -->
            <li class="card campaign-card" style="--ft: {familyOf(item.flags[0].type).ink}">
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
          <li class="card cleared-card" class:compact style="--ft: {familyOf(f.type).ink}">
            <div class="card-main">
              <span class="type">{familyOf(f.type).mark} {labelFor(f.type)}</span>
              <span class="target">{f.target === 'global' ? 'network-wide' : f.target}</span>
            </div>
            <p class="detail">{f.detail}</p>
            <div class="meta">
              {#if f.verdict}
                <!-- Judged before it cleared (#638's 'expected'/'noise')
                     -- the verdict is the more informative fact here, so
                     it replaces the plain "cleared HH:MM" line rather
                     than sitting alongside it. -->
                <span class="verdict-badge verdict-{f.verdict}">{VERDICT_LABELS[f.verdict]}</span>
                <span>{judgedByLine(f)}</span>
              {:else}
                <span>cleared {f.clearedAt ? formatHM(f.clearedAt) : ''}</span>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  </div>

  {#if isAdminOrOpen}
    <!-- The Exclusions tab (#547): permanently-excluded (detector,
         target) pairs, reviewed and undone here rather than from a
         pointer to a separate page -- see Exclusions.svelte's own doc
         comment for why this is admin-only. -->
    <div
      class="exclusions-panel"
      role="tabpanel"
      id="panel-exclusions"
      aria-labelledby="tab-exclusions"
      tabindex="0"
      hidden={activeTab !== 'exclusions'}
    >
      <Exclusions />
    </div>
  {/if}

  {#if flagsState.undoableVerdicts.length > 0}
    <!-- Issue #638's undo affordance: judgeAndClear() has already
         posted the verdict and cleared the card -- this is the window
         (VERDICT_UNDO_MS) during which undoVerdict() can still send a
         real DELETE to reverse it. Unlike Toast.svelte (a passive,
         single, fade-away confirmation with pointer-events: none) this
         needs to be clickable and to hold more than one at a time,
         since Expected and Noise can each be pressed on a different
         card before either window lapses. -->
    <div class="verdict-undo-stack" role="status">
      {#each flagsState.undoableVerdicts as u (u.id)}
        <div class="verdict-undo">
          <span>Cleared as {VERDICT_LABELS[u.verdict]}</span>
          <button class="verdict-undo-btn" onclick={() => undoVerdict(u.id)}>Undo</button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .flags-page {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  /* The explicit display rules above beat the browser's own [hidden]
     handling, so without this the inactive panel rendered *below* the
     active one -- the Exclusions blurb sat under the flags list.
     Watchlist.svelte carries the same guard for the same reason. */
  .flags[hidden],
  .exclusions-panel[hidden] {
    display: none;
  }

  .exclusions-panel {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

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

  /* The stripe (rounds 18-19): the flag's family ink as one unbroken
     3px line down the card's own left edge, running through the drawer
     when it opens -- an inset shadow rather than a border so it sits
     inside the rounded corner without its own radius math. --ft is set
     per-card from lib/flagPalette. The right padding reserves only the
     chevron's corner now that the actions live in the drawer. */
  .card {
    position: relative;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 40px 10px 14px;
    box-shadow: inset 3px 0 0 var(--ft, transparent);
  }

  /* Compact (2/3 columns, issue #199): tighter padding, same stripe. */
  .card.compact {
    padding: 8px 32px 8px 12px;
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

  /* The mark wears the family ink (rounds 18-19): ✱ an alarm, ▲ an
     advisory, coloured text rather than the old accent chip so the
     type and its stripe read as one thing. */
  .type {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--ft, var(--accent));
    white-space: nowrap;
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

  /* The row's one affordance: the chevron rotates open, per the
     record's own idiom. */
  .openc {
    position: absolute;
    top: 8px;
    right: 8px;
    background: transparent;
    border: none;
    color: var(--accent);
    font-size: 13px;
    padding: 4px 8px;
    cursor: pointer;
    transition: transform 0.2s;
  }

  .card.open > .openc {
    transform: rotate(90deg);
  }

  /* The drawer (rounds 18-19): evidence and matched lines left, the
     episode's shape right, actions across the foot. No indented border
     of its own -- the card's stripe is the one line (round 19). */
  .dwr {
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
    display: grid;
    grid-template-columns: 1.3fr 1fr;
    gap: 10px 32px;
  }

  .card.compact .dwr {
    grid-template-columns: 1fr;
    gap: 8px;
  }

  .dcol {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }

  .side {
    min-width: 0;
  }

  .side .lab {
    display: block;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .side svg {
    display: block;
    width: 100%;
    height: 34px;
    margin: 6px 0 2px;
  }

  .side .span,
  .ep-note {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .ep-note {
    margin: 8px 0 0;
  }

  .lines {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    white-space: pre;
    overflow: hidden;
  }

  .dwr-acts {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 2px;
  }

  .act {
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 16px;
    cursor: pointer;
  }

  .act:hover {
    border-color: var(--accent);
  }

  /* The honest cleared state (round 26). */
  .caempty {
    display: flex;
    align-items: baseline;
    gap: 10px;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .caempty b {
    color: var(--fg);
  }

  .cae-mark {
    color: var(--accept);
    font-weight: 700;
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover {
    text-decoration-color: currentColor;
  }

  @media (prefers-reduced-motion: reduce) {
    .openc {
      transition: none;
    }
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

  /* #654: the pair cap's truncation notice -- quiet (fg-dim, no icon)
     because it's a footnote on the header row above, not a warning; the
     point is only that it's never silent, not that it's loud. */
  .ev-truncated {
    font-size: 11px;
    color: var(--fg-dim);
    font-weight: normal;
  }

  /* One row per host group (#654) -- same ev-row/ev-label/ev-value
     shape as every other evidence line, just indented slightly so a
     multi-host pairs list visually nests under its own "Host:port
     pairs" header row rather than reading as a sibling of Ports
     touched/Hosts involved. */
  .ev-pair-row {
    padding-left: 10px;
  }

  /* The verdict lives on the card's face (issue #638: the primary
     triage act), inline under the meta line -- the drawer carries the
     clear actions. */
  .verdict-slot {
    margin-top: 8px;
    max-width: 280px;
  }

  /* Issue #638: the leading verdict row, three bare-labelled buttons in
     one line -- see VERDICT_LABELS' own doc comment for why there is no
     second line under any of them. Colour is reinforcement only; the
     label text is what carries the meaning (#616's "no meaning by
     colour alone"). */
  .verdict-row {
    display: flex;
    gap: 4px;
  }

  .verdict-btn {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 5px;
    padding: 5px 4px;
    font-size: 11px;
    font-weight: 600;
    white-space: nowrap;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .verdict-btn:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .verdict-btn-expected {
    color: var(--accept);
    border-color: color-mix(in srgb, var(--accept) 35%, var(--border));
  }

  .verdict-btn-expected:hover {
    background: var(--accept-bg);
    border-color: var(--accept);
  }

  .verdict-btn-real {
    color: var(--reject);
    border-color: color-mix(in srgb, var(--reject) 35%, var(--border));
  }

  .verdict-btn-real:hover {
    background: var(--reject-bg);
    border-color: var(--reject);
  }

  /* A judged flag's badge + judged-by/when (issue #638) -- replaces the
     verdict row entirely rather than sitting beside it, which is what
     keeps a judged flag from ever reading as an open question again. */
  .verdict-status {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .verdict-badge {
    align-self: flex-start;
    font-size: 11px;
    font-weight: 700;
    border-radius: 4px;
    padding: 3px 8px;
    white-space: nowrap;
  }

  .verdict-badge.verdict-expected {
    color: var(--accept);
    background: var(--accept-bg);
  }

  .verdict-badge.verdict-noise {
    color: var(--fg-muted);
    background: var(--bg-hover);
  }

  .verdict-badge.verdict-real {
    color: var(--reject);
    background: var(--reject-bg);
  }

  .verdict-judged-by {
    font-size: 11px;
    color: var(--fg-dim);
  }

  /* Clear/split-Clear demote to secondary (#638) -- still fully
     available, one press away, just no longer the card's leading
     affordance now that the verdict row is. Quieted with opacity alone
     (not a smaller control, which would cost the split button's own
     click target) and restored on hover/focus so the control never
     becomes hard to find, only quiet to glance past. */
  .clear.secondary,
  .split-clear.secondary {
    opacity: 0.72;
  }

  .clear.secondary:hover,
  .clear.secondary:focus-visible,
  .split-clear.secondary:hover,
  .split-clear.secondary:focus-within {
    opacity: 1;
  }

  /* The undo affordance for a just-judged 'expected'/'noise' verdict
     (issue #638) -- see flagsState.undoVerdict's own doc comment for why
     undoing is a real DELETE, not just cancelling a timer. Fixed rather
     than inline: the card it refers to has already left the active list
     by the time this renders, so there is nothing sensible to anchor it
     to in flow. */
  .verdict-undo-stack {
    position: fixed;
    left: 50%;
    bottom: 28px;
    transform: translateX(-50%);
    z-index: 50;
    display: flex;
    flex-direction: column;
    gap: 6px;
    align-items: center;
  }

  .verdict-undo {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px 10px 8px 16px;
    border-radius: 8px;
    background: var(--bg-elevated);
    color: var(--fg);
    border: 1px solid var(--border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    font-size: 13px;
  }

  .verdict-undo-btn {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent);
    border-radius: 5px;
    padding: 4px 10px;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }

  .verdict-undo-btn:hover {
    background: var(--accent-bg);
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


</style>
