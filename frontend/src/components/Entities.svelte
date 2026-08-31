<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Entities, built to round 29's ratified scene (#675, superseding
  // #647's Fleet-folded-in page and the older #489/DESIGN.md survey
  // concept, which was ratified before round 29 restyled every "operate"
  // page onto the deck's own clothes and was never built against it).
  // The scene is two things under the strap "the routers that push
  // here, and the named things behind them":
  //
  //  1. Router cards -- one per device that pushes here (Fleet's old
  //     table, folded into cards: lib/fleet.ts's sort/status logic still
  //     backs it, so this and the standalone Fleet.svelte can't drift).
  //     A live card carries its current push rate; a quiet one states
  //     that fact without alarm. Adding a router used to be a further
  //     dashed card printed in the row, carrying the standing promise
  //     (mikroview only ever receives, never connects out) and the real
  //     RouterOS lines to paste as instructional prose on the page. The
  //     owner's round-30 review (#718) called that apparatus, not
  //     content -- it is now a single "add router" button that opens a
  //     small dialog with the same port, lines (lib/setupsteps.ts, the
  //     same generator the setup wizard uses) and assurance, on demand
  //     rather than always printed. The row's own bordered panel went
  //     the same way (#718's "boxes in boxes"): the label stays, the
  //     frame around already-bordered cards does not.
  //  2. A tab strip -- hosts / rules / ports (#681, a deliberate
  //     departure from the ratified round-29 scene, recorded on that
  //     issue rather than smuggled in) -- over one table per tab, the
  //     docket's own tab vocabulary (Docket.svelte) reused rather than
  //     inventing new furniture. hosts is the ratified table exactly as
  //     #675 built it and stays the default. rules and ports exist
  //     because naming in context has nowhere to happen: a rule that is
  //     in a router's pushed rule table but has never fired has no row
  //     anywhere else to click (#681's owner decision), so it needs its
  //     own surface, whether or not it has ever fired. The old page's
  //     separate add-entity form is still gone -- these tabs name what
  //     has actually arrived (fired) or been pushed (a rule), never a
  //     blank invented row.
  //
  // mac/first-seen/last-seen have no existing source (the entity store
  // only ever held label/tags, and the client event buffer is far too
  // short-lived for "412 d"): #675 added the minimal backend piece
  // (device.MACRegistry.NoteIP + GET /api/devices/macs) rather than
  // inventing the numbers, reusing the MAC registry that already existed
  // for the new-device detector. Lane reuses zones.svelte.ts unchanged
  // (the same boundary-derived zones the topography map draws).
  //
  // The rules tab's chain/action/last-fired columns are its own join,
  // not GET /api/rules alone: that endpoint (internal/rules.Store) only
  // ever holds a rule label once it has fired, so a never-fired rule is
  // simply absent from it, not present with a zero count -- the exact
  // case #681 exists for. The full pushed-rule table (fetchRouterRules,
  // already loaded per router below for the router cards' rule count)
  // carries chain/action/log-prefix for every rule regardless of firing,
  // keyed by lib/routerLookup.svelte.ts's own "<ACTION>|<slug>|"
  // log-prefix convention (ruleLabelFromLogPrefix, added for this tab as
  // that file's prefixMatchesLabel's inverse) -- the same convention
  // #445's router-lookup popup already decodes the other direction. A
  // rule with no log-prefix, or one that doesn't follow the convention,
  // can never produce a firing event carrying a label either (payload.go
  // -- "an unlogged rule must stay unnameable"), so it is left off this
  // tab rather than shown as an unnameable dead end.
  import { onMount } from 'svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { appState } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { familyOf } from '../lib/flagPalette'
  import {
    fetchDeviceMACs,
    fetchRouterRules,
    fetchRouterAddresses,
    fetchRules,
    fetchSetupStatus,
    type RouterFilterRule,
  } from '../lib/api'
  import { discoverHosts, discoverPorts } from '../lib/discoveredEntities'
  import { ruleLabelFromLogPrefix } from '../lib/routerLookup.svelte'
  import { formatRelative, formatHM } from '../lib/format'
  import { STATUS_LABEL, sortedDevices, recentCount as recentCountOf, RECENT_WINDOW_MS } from '../lib/fleet'
  import { syslogCommands, instanceAddress, portOf } from '../lib/setupsteps'
  import type { EntityType, MACRegistryEntry, RuleUsage, SetupStatus } from '../lib/types'

  // --- routers (folded in from Fleet, #647; cards since #675) ---------
  const routerRows = $derived(sortedDevices(appState.devices))

  let status = $state<SetupStatus | null>(null)

  // The "add a router" explanation and paste-able commands used to sit
  // printed on the page as a dashed card (issue #675's own "another
  // router?" invite) -- the owner's round-30 review (#718) called that
  // apparatus, not content: "just have an add router button that
  // displays the commands." showAddRouter gates a small dialog instead,
  // same backdrop-plus-modal shape AboutOverlay.svelte already uses
  // (mounted locally rather than at the app root, since nothing else
  // needs to open this one). The port, the paste lines and the "it
  // never connects to them" assurance all move inside it unchanged --
  // a relocation, not a deletion.
  let showAddRouter = $state(false)

  function closeAddRouter() {
    showAddRouter = false
  }

  function onAddRouterKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') closeAddRouter()
  }

  function onAddRouterBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) closeAddRouter()
  }

  // Per-router enrichment beyond what GET /api/devices already carries
  // (rule/zone counts, last push): the same pushed tables Topography's
  // zones and the setup wizard already read, fetched once per device
  // seen so far. A device that has never pushed either table simply
  // leaves these null -- absence is a fact, not a loading state to fake.
  interface RouterDetail {
    ruleCount: number | null
    zoneCount: number | null
    lastPush: string | null
  }
  let routerDetail = $state<Record<string, RouterDetail>>({})

  // The rules tab's source table (#681): kept alongside routerDetail's
  // ruleCount rather than re-fetched, since fetchRouterRules(deviceId)
  // already runs once per router below -- the count and the rows it is
  // counted from come from the same response.
  let routerRulesRaw = $state<Record<string, RouterFilterRule[]>>({})

  async function loadRouterDetail(deviceId: string) {
    const [rules, addrs] = await Promise.all([
      fetchRouterRules(deviceId).catch(() => null),
      fetchRouterAddresses(deviceId).catch(() => null),
    ])
    routerDetail[deviceId] = {
      ruleCount: rules?.available ? rules.rules.length : null,
      zoneCount: addrs?.available ? new Set(addrs.rules.map((a) => a.interface)).size : null,
      lastPush: rules?.updatedAt ?? addrs?.updatedAt ?? null,
    }
    routerRulesRaw[deviceId] = rules?.available ? rules.rules : []
  }

  // Fetches detail for any router this page hasn't asked about yet --
  // runs again whenever the device list gains one, e.g. a third router
  // just pointed its syslog here.
  $effect(() => {
    for (const d of routerRows) {
      if (!(d.id in routerDetail)) loadRouterDetail(d.id)
    }
  })

  function fstate(d: (typeof routerRows)[number]): { mark: string; cls: string; text: string } {
    if (d.status === 'live') return { mark: '●', cls: 'ok', text: 'LIVE' }
    if (d.status === 'never_seen') return { mark: '◌', cls: 'quiet', text: STATUS_LABEL.never_seen.toUpperCase() }
    const days = Math.floor((appState.now - new Date(d.lastSeen).getTime()) / 86_400_000)
    return { mark: '◌', cls: 'quiet', text: `QUIET${days >= 1 ? ` · ${days} d` : ''}` }
  }

  // Fixed to one decimal rather than lib/format's formatEps (whole
  // number at >=1 events/s, one decimal below it): two router cards
  // showing "1" and "1.0" side by side read as inconsistent even though
  // each is individually correct under formatEps's own rule (#718).
  // formatEps is shared by several other pages' own single-number
  // readouts, so the fix stays local to this page's per-card metric
  // rather than changing that shared rule.
  function routerRate(deviceId: string): string {
    const n = recentCountOf(appState.events, deviceId, appState.now)
    return (n / (RECENT_WINDOW_MS / 1000)).toFixed(1)
  }

  // --- named things: entity hosts + discovered-but-unnamed hosts, one
  // table (#675) ---------------------------------------------------------
  let macs = $state<MACRegistryEntry[]>([])
  const macByIp = $derived.by(() => {
    const m = new Map<string, MACRegistryEntry>()
    for (const e of macs) if (e.lastIp) m.set(e.lastIp, e)
    return m
  })

  const hostEntities = $derived(entitiesState.list.filter((e) => e.type === 'host'))
  const discoveredHosts = $derived(discoverHosts(appState.events, entitiesState.list))

  interface ThingRow {
    key: string // the IP address -- the entity key
    label: string // '' means not yet named
    fallbackLastSeen: string | null // from the client buffer, for a row with no MAC-registry entry
  }

  const thingRows = $derived.by((): ThingRow[] => {
    const rows: ThingRow[] = hostEntities.map((e) => ({ key: e.key, label: e.label ?? '', fallbackLastSeen: null }))
    for (const d of discoveredHosts) rows.push({ key: d.key, label: '', fallbackLastSeen: d.lastSeen })
    return rows
  })

  // The lanes: the same busiest-first order zones.svelte.ts already
  // ranks Topography's map by, reused here as a rank (not just a name)
  // so the table groups by lane the way the ratified scene's mockup
  // data does, and the dot wears the same colour Topography would give
  // that lane.
  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']
  const laneOf = $derived.by(() => {
    const m = new Map<string, { name: string; rank: number; ink: string }>()
    zonesState.zones.forEach((z, rank) => {
      for (const h of z.hosts) m.set(h.ip, { name: z.name, rank, ink: LANE_INKS[rank % LANE_INKS.length] })
    })
    return m
  })

  function elideMac(mac: string): string {
    const parts = mac.split(':')
    if (parts.length !== 6) return mac
    return `${parts[0]}:${parts[1]}:${parts[2]}:…:${parts[5]}`
  }

  // Marks, in the docket's own vocabulary (lib/flagPalette.ts,
  // watchlistState) -- never a new vocabulary invented for this page.
  interface Marks {
    newTalker: boolean
    watched: boolean
    ringBroken: boolean
    alarmCount: number
  }

  function marksFor(address: string, mac: string | undefined): Marks {
    const macLower = mac?.toLowerCase()
    let newTalker = false
    let alarmCount = 0
    for (const f of flagsState.list) {
      if (f.cleared) continue
      if (f.type === 'new_device') {
        if (macLower && f.target.toLowerCase() === macLower) newTalker = true
        continue
      }
      const addr = f.target.replace(/ -> port \d+$/, '')
      if (addr === address && familyOf(f.type).mark === '✱') alarmCount++
    }
    let watched = false
    let ringBroken = false
    for (const e of watchlistState.entries) {
      if (!e.enabled) continue
      const hit = e.destIp === address || e.source?.ip === address || (macLower && e.source?.mac?.toLowerCase() === macLower)
      if (!hit) continue
      watched = true
      if (watchlistState.coverage[e.id] === 'no-logging') ringBroken = true
    }
    return { newTalker, watched, ringBroken, alarmCount }
  }

  const NEW_TALKER_INK = familyOf('new_device').ink

  interface Row {
    key: string
    label: string
    lane: { name: string; ink: string } | null
    mac: MACRegistryEntry | null
    marks: Marks
  }

  const rows = $derived.by((): Row[] => {
    return thingRows
      .map((t): Row => {
        const mac = macByIp.get(t.key) ?? null
        const lane = laneOf.get(t.key) ?? null
        return { key: t.key, label: t.label, lane, mac, marks: marksFor(t.key, mac?.mac) }
      })
      .sort((a, b) => {
        const la = a.lane?.name ?? '￿'
        const lb = b.lane?.name ?? '￿'
        if (la !== lb) return la.localeCompare(lb)
        return (a.label || a.key).localeCompare(b.label || b.key)
      })
  })

  function lastSeenOf(row: Row): string {
    if (row.mac) return formatRelative(row.mac.lastSeen, appState.now)
    const t = thingRows.find((t) => t.key === row.key)
    return t?.fallbackLastSeen ? formatRelative(t.fallbackLastSeen, appState.now) : '—'
  }

  function firstSeenOf(row: Row): string {
    return row.mac ? formatRelative(row.mac.firstSeen, appState.now) : '—'
  }

  // --- rules tab: every pushed rule, named or not, fired or not (#681)
  // ---------------------------------------------------------------------
  let rulesUsage = $state<RuleUsage[]>([])
  const usageByRule = $derived.by(() => new Map(rulesUsage.map((u) => [u.rule, u])))
  const ruleEntities = $derived(entitiesState.list.filter((e) => e.type === 'rule'))

  // One row per distinct rule slug pushed by any router, deduped on the
  // slug itself: if two routers push a same-named rule (the operator's
  // own convention, not something mikroview enforces -- see
  // RulesForLogPrefix's doc), the later device in routerRows order wins
  // the displayed chain/action. A single-router fleet, by far the common
  // case, never hits this.
  const pushedRules = $derived.by(() => {
    const m = new Map<string, { chain: string; action: string }>()
    for (const d of routerRows) {
      for (const r of routerRulesRaw[d.id] ?? []) {
        const slug = ruleLabelFromLogPrefix(r.logPrefix)
        if (slug) m.set(slug, { chain: r.chain, action: r.action })
      }
    }
    return m
  })

  interface RuleRow {
    key: string // the rule slug -- the entity key
    label: string
    chain: string | null
    action: string | null
    lastFired: string | null // usage.lastSeen -- null means never fired
  }

  const ruleRows = $derived.by((): RuleRow[] => {
    const keys = new Set<string>([...pushedRules.keys(), ...ruleEntities.map((e) => e.key), ...rulesUsage.map((u) => u.rule)])
    return [...keys]
      .map((key): RuleRow => {
        const pushed = pushedRules.get(key) ?? null
        const usage = usageByRule.get(key) ?? null
        const entity = ruleEntities.find((e) => e.key === key)
        return {
          key,
          label: entity?.label ?? '',
          chain: pushed?.chain ?? null,
          action: pushed?.action ?? null,
          lastFired: usage?.lastSeen ?? null,
        }
      })
      .sort((a, b) => (a.label || a.key).localeCompare(b.label || b.key))
  })

  // --- ports tab: every port named, plus every port seen in traffic
  // that isn't yet (#681) --------------------------------------------
  const portEntities = $derived(entitiesState.list.filter((e) => e.type === 'port'))
  const discoveredPorts = $derived(discoverPorts(appState.events, entitiesState.list))

  interface PortRow {
    key: string
    label: string
    lastSeen: string | null
  }

  const portRows = $derived.by((): PortRow[] => {
    const rows: PortRow[] = portEntities.map((e) => ({ key: e.key, label: e.label ?? '', lastSeen: null }))
    for (const d of discoveredPorts) rows.push({ key: d.key, label: '', lastSeen: d.lastSeen })
    return rows.sort((a, b) => Number(a.key) - Number(b.key))
  })

  onMount(() => {
    entitiesState.refresh().catch(() => {
      // The table simply shows fewer named rows until this resolves.
    })
    fetchDeviceMACs()
      .then((m) => (macs = m))
      .catch(() => {
        // mac/first-seen/last-seen fall back to '—' until this resolves.
      })
    zonesState.refresh().catch(() => {
      // Lane names fall back to the raw boundary id until this resolves.
    })
    fetchSetupStatus()
      .then((s) => (status = s))
      .catch(() => {
        // The "add a third router" card's paste-lines disclosure simply
        // has nothing to show until this resolves.
      })
    fetchRules()
      .then((r) => (rulesUsage = r))
      .catch(() => {
        // The rules tab simply shows every pushed rule as never-fired
        // until this resolves -- true of what's loaded, not a guess.
      })
  })

  // --- tab strip (#681): hosts / rules / ports, the docket's own tab
  // vocabulary (Docket.svelte) over this page's one table, not a new
  // kind of furniture. hosts is the default -- the ratified scene's own
  // table, unchanged. -------------------------------------------------
  //
  // Off for round-30 fidelity: round 30's #ent draws the entities table
  // directly under the router cards, one table of named things, with no
  // tab strip -- the tabs are unmounted, not deleted (#700, #691). Typed
  // rather than inferred so the block stays reachable to the type
  // checker -- a bare `false` narrows to `never` and reports it as
  // unreachable. Same pattern as LiveTable's RESIZE_HANDLES_ENABLED,
  // MetricsRegister's LEDGER_ENABLED and Topography's
  // DEGRADED_NOTE_ENABLED. activeTab stays 'hosts' and is never changed
  // while the strip is unmounted, so the hosts table -- the ratified
  // round-29/round-30 table -- is what always renders; naming rules and
  // ports in context is real work tracked on #681, not lost, and
  // remounting the strip is all #691 needs to do to bring it back.
  type Tab = 'hosts' | 'rules' | 'ports'
  const TABS_ENABLED: boolean = false
  let activeTab = $state<Tab>('hosts')

  // ---- inline rename (issue #675: rename lives in the table, not a
  // separate form; #681 generalizes it across all three tabs -- same
  // store, same EntityType, same Enter-saves/Esc-cancels/blur-saves
  // behaviour, one rename path rather than three) ----------------------
  let renamingKey = $state<{ type: EntityType; key: string } | null>(null)
  let renameDraft = $state('')
  let renameSaving = $state(false)
  let renameError = $state<string | null>(null)

  function isRenaming(type: EntityType, key: string): boolean {
    return renamingKey?.type === type && renamingKey.key === key
  }

  function startRename(type: EntityType, key: string, label: string) {
    renamingKey = { type, key }
    renameDraft = label
    renameError = null
  }

  function cancelRename() {
    renamingKey = null
    renameError = null
  }

  async function saveRename(type: EntityType, key: string) {
    renameError = null
    renameSaving = true
    const existing = entitiesState.list.find((e) => e.type === type && e.key === key)
    const wasRenaming = renamingKey
    renamingKey = null
    const err = await entitiesState.upsert({
      type,
      key,
      label: renameDraft.trim(),
      tags: existing?.tags ?? [],
    })
    renameSaving = false
    if (err) {
      renamingKey = wasRenaming
      renameError = err
    }
  }

  function onRenameKeydown(e: KeyboardEvent, type: EntityType, key: string) {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveRename(type, key)
    } else if (e.key === 'Escape') {
      cancelRename()
    }
  }

  // Removing the input on Enter/Escape can itself fire a blur in a real
  // browser (losing focus because its element vanished, not because the
  // operator tabbed away) -- guarded on renamingKey still being this row
  // so that trailing blur never re-saves (Enter) or overrides a cancel
  // with the stale draft (Escape). jsdom doesn't reproduce this blur, so
  // nothing in the test suite would have caught it without the guard.
  function onRenameBlur(type: EntityType, key: string) {
    if (isRenaming(type, key)) saveRename(type, key)
  }

  function focusOnMount(node: HTMLInputElement) {
    node.focus()
    node.select()
  }
</script>

<svelte:window onkeydown={onAddRouterKeydown} />

<div class="page scrollbar op-page">
  <div class="opwrap"><div class="opanel">
    <div class="og">
      <h3>routers — every one that pushes here</h3>
        <div class="fcards">
          {#each routerRows as d (d.id)}
            {@const st = fstate(d)}
            {@const detail = routerDetail[d.id]}
            <div class="fcard" class:live={d.status === 'live'}>
              <div class="fhead"><b>{d.name}</b><span class="fstate {st.cls}">{st.mark} {st.text}</span></div>
              <div class="frow">
                {d.routerosVersion ? `RouterOS ${d.routerosVersion}` : 'RouterOS version not yet reported'}
                {#if detail?.ruleCount !== null && detail?.ruleCount !== undefined}
                  · {detail.ruleCount} rule{detail.ruleCount === 1 ? '' : 's'}
                {/if}
                {#if detail?.zoneCount !== null && detail?.zoneCount !== undefined}
                  · {detail.zoneCount} zone{detail.zoneCount === 1 ? '' : 's'}
                {/if}
              </div>
              {#if d.status === 'live'}
                <div class="frow">
                  {#if detail?.lastPush}last push {formatHM(detail.lastPush)} ·{/if} {routerRate(d.id)} events/s now
                </div>
              {:else if d.status === 'never_seen'}
                <div class="frow dim">never heard from yet</div>
              {:else}
                <div class="frow dim">last heard {formatRelative(d.lastSeen, appState.now)} — quiet is a fact, not a fault</div>
              {/if}
              <div class="frow dim">syslog{status?.instance.tlsEnabled ? ' TLS' : ''} · state pushed every 20 min</div>
            </div>
          {/each}
        </div>
        <button type="button" class="add-router-btn" onclick={() => (showAddRouter = true)}>+ add router</button>
    </div>

    {#if showAddRouter}
      <div class="backdrop" onclick={onAddRouterBackdropClick} role="presentation">
        <div class="modal" role="dialog" aria-modal="true" aria-label="Add a router" tabindex="-1">
          <div class="modal-header">
            <span class="title">Add a router</span>
            <button type="button" class="close" onclick={closeAddRouter} aria-label="Close">✕</button>
          </div>
          <div class="body">
            <p>
              Point its syslog at {status ? `:${portOf(status.instance.syslogPort)}` : 'mikroview’s syslog port'} and
              it appears here.
            </p>
            <p>Routers push to mikroview — it never connects to them.</p>
            {#if status}
              <pre class="paste">{syslogCommands(instanceAddress({ host: location.host }), status.instance.syslogPort)}</pre>
            {:else}
              <p class="dim">Loading the commands to paste…</p>
            {/if}
          </div>
        </div>
      </div>
    {/if}

    {#if TABS_ENABLED}
      <!-- Unmounted for round-30 fidelity -- see the comment on
           TABS_ENABLED above. Not deleted: tracked on #691/#681. -->
      <div class="tab-row" role="tablist" aria-label="Entities">
        <button class="tab" class:on={activeTab === 'hosts'} role="tab" aria-selected={activeTab === 'hosts'} onclick={() => (activeTab = 'hosts')}>
          hosts
        </button>
        <button class="tab" class:on={activeTab === 'rules'} role="tab" aria-selected={activeTab === 'rules'} onclick={() => (activeTab = 'rules')}>
          rules
        </button>
        <button class="tab" class:on={activeTab === 'ports'} role="tab" aria-selected={activeTab === 'ports'} onclick={() => (activeTab = 'ports')}>
          ports
        </button>
      </div>
    {/if}

    {#if activeTab === 'hosts'}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>lane</th>
            <th>address</th>
            <th>mac</th>
            <th>first seen</th>
            <th>last seen</th>
            <th>marks</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.key)}
            <tr class:warn={row.marks.alarmCount > 0}>
              <td class="k">
                {#if isRenaming('host', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'host', row.key)}
                    onblur={() => onRenameBlur('host', row.key)}
                    disabled={renameSaving}
                  />
                {:else}
                  <button type="button" class="rename-btn" onclick={() => startRename('host', row.key, row.label)} title="Click to rename">
                    {row.label || '— click to name —'}
                  </button>
                {/if}
              </td>
              <td>
                {#if row.lane}<i class="lz" style="background:{row.lane.ink}"></i>{row.lane.name}{:else}<span class="dim">—</span>{/if}
              </td>
              <td>{row.key}</td>
              <td class="dim">{row.mac ? elideMac(row.mac.mac) : 'private'}</td>
              <td class="dim">{firstSeenOf(row)}</td>
              <td>{lastSeenOf(row)}</td>
              <td>
                {#if row.marks.newTalker}<span class="mk" style="color:{NEW_TALKER_INK}">▲ new talker</span>{/if}
                {#if row.marks.watched}<span class="mk mk-watched"
                  >◉ watched{row.marks.ringBroken ? ' · ○ ring broken' : ''}</span
                >{/if}
                {#if row.marks.alarmCount > 0}<span class="mk mk-flagged">✱ flagged</span>{/if}
              </td>
            </tr>
            {#if isRenaming('host', row.key) && renameError}
              <tr><td colspan="7" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if rows.length === 0}
            <tr><td colspan="7" class="dim">Nothing seen yet.</td></tr>
          {/if}
        </tbody>
      </table>
      <p class="oghint table-hint">
        a name is yours to give — click one to rename it; the router's own names arrive with its pushes
      </p>
    {:else if activeTab === 'rules'}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>chain</th>
            <th>action</th>
            <th>last fired</th>
          </tr>
        </thead>
        <tbody>
          {#each ruleRows as row (row.key)}
            <tr>
              <td class="k">
                {#if isRenaming('rule', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'rule', row.key)}
                    onblur={() => onRenameBlur('rule', row.key)}
                    disabled={renameSaving}
                  />
                {:else}
                  <button type="button" class="rename-btn" onclick={() => startRename('rule', row.key, row.label)} title="Click to rename">
                    {row.label || row.key}
                  </button>
                {/if}
              </td>
              <td class="dim">{row.chain ?? '—'}</td>
              <td class="dim">{row.action ?? '—'}</td>
              <td class={row.lastFired ? '' : 'dim'}>
                {row.lastFired ? formatRelative(row.lastFired, appState.now) : 'has not fired'}
              </td>
            </tr>
            {#if isRenaming('rule', row.key) && renameError}
              <tr><td colspan="4" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if ruleRows.length === 0}
            <tr><td colspan="4" class="dim">No router has pushed a rule table yet — once one does, every rule it carries appears here, fired or not.</td></tr>
          {/if}
        </tbody>
      </table>
      <p class="oghint table-hint">
        a name is yours to give — click one to rename it; a rule that has never fired still gets a row, not silence
      </p>
    {:else}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>port</th>
            <th>last seen</th>
          </tr>
        </thead>
        <tbody>
          {#each portRows as row (row.key)}
            <tr>
              <td class="k">
                {#if isRenaming('port', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'port', row.key)}
                    onblur={() => onRenameBlur('port', row.key)}
                    disabled={renameSaving}
                  />
                {:else}
                  <button type="button" class="rename-btn" onclick={() => startRename('port', row.key, row.label)} title="Click to rename">
                    {row.label || '— click to name —'}
                  </button>
                {/if}
              </td>
              <td>{row.key}</td>
              <td class={row.lastSeen ? '' : 'dim'}>{row.lastSeen ? formatRelative(row.lastSeen, appState.now) : '—'}</td>
            </tr>
            {#if isRenaming('port', row.key) && renameError}
              <tr><td colspan="3" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if portRows.length === 0}
            <tr><td colspan="3" class="dim">No port has shown up in traffic yet, and none has been named ahead of time.</td></tr>
          {/if}
        </tbody>
      </table>
      <p class="oghint table-hint">
        a name is yours to give — click one to rename it; a port earns its row by showing up in traffic
      </p>
    {/if}
  </div></div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
  }

  .op-page .opwrap {
    display: flex;
    justify-content: center;
  }

  .op-page .opanel {
    width: 100%;
    max-width: 1500px;
  }

  /* No border/background here (#718): a bordered panel around a row of
     already-bordered router cards was a box inside a box. The label
     stays -- round 30 still names the row -- the frame around it does
     not. */
  .og {
    margin-bottom: 20px;
  }

  .og h3 {
    margin: 0 0 6px;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  /* --- the tab strip (#681): the docket's own tab vocabulary
     (Docket.svelte's .tab-row/.tab), reused rather than reinvented. --- */
  .tab-row {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-bottom: 10px;
  }

  .tab {
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 4px 10px 6px;
    cursor: pointer;
  }

  .tab:hover {
    color: var(--fg-muted);
  }

  .tab.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  .oghint {
    margin: 8px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .dim {
    color: var(--fg-dim);
  }

  /* --- router cards -------------------------------------------------- */
  .fcards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
  }

  .fcard {
    background: var(--glass);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 16px 20px;
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .fcard.live {
    border-color: var(--hair-2);
  }

  /* The add-router trigger (#718): a plain button, not a dashed card --
     the point of this round of feedback was one fewer box, not another
     one shaped like a button. */
  .add-router-btn {
    margin-top: 14px;
    background: none;
    border: 1px solid var(--accent);
    border-radius: 8px;
    padding: 7px 14px;
    font: inherit;
    font-size: 12.5px;
    color: var(--accent);
    cursor: pointer;
  }

  .add-router-btn:hover {
    background: var(--accent-bg);
  }

  .fhead {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 8px;
    gap: 10px;
  }

  .fhead b {
    font-size: 15px;
    color: var(--fg);
  }

  .fstate {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    white-space: nowrap;
  }

  .fstate.ok {
    color: var(--accept);
  }

  .fstate.quiet {
    color: var(--fg-dim);
  }

  .fcard .frow {
    padding: 3px 0;
  }

  /* --- the add-router dialog (#718): same backdrop-plus-modal shape as
     AboutOverlay.svelte, reused rather than a new kind of surface --
     mounted locally here since nothing else needs to open it. --- */
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }

  .modal {
    background: var(--bg-elevated, var(--bg));
    border: 1px solid var(--border);
    border-radius: 8px;
    max-width: 30rem;
    width: calc(100% - 2rem);
    max-height: calc(100vh - 4rem);
    overflow-y: auto;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .modal-header .title {
    font-weight: 600;
  }

  .modal .close {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 1rem;
    padding: 0.25rem;
  }

  .modal .close:hover {
    color: var(--fg);
  }

  .modal .body {
    padding: 1rem;
    font-size: 13px;
    line-height: 1.5;
  }

  .modal .body p {
    margin: 0 0 0.75rem;
  }

  .modal .body p:last-of-type {
    margin-bottom: 0;
  }

  .paste {
    margin: 8px 0 0;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-muted);
    white-space: pre-wrap;
    word-break: break-all;
  }

  /* --- the named-things table ------------------------------------------ */
  .etable {
    border-collapse: collapse;
    font-family: var(--font-mono);
    font-size: 12px;
    width: 100%;
  }

  .etable th {
    text-align: left;
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    padding: 6px 14px;
    border-bottom: 1px solid var(--hair-2);
  }

  .etable td {
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .etable tr.warn td {
    background: rgba(255, 84, 112, 0.04);
  }

  .etable td.k {
    color: var(--fg);
  }

  .rename-btn {
    background: none;
    border: 1px dashed transparent;
    border-radius: 4px;
    padding: 2px 4px;
    margin: -2px -4px;
    font: inherit;
    color: inherit;
    cursor: text;
    text-align: left;
  }

  .rename-btn:hover {
    border-color: var(--border);
    color: var(--accent);
  }

  .rename-input {
    font: inherit;
    background: var(--bg);
    border: 1px solid var(--accent);
    border-radius: 4px;
    padding: 2px 6px;
    color: var(--fg);
    width: 100%;
    min-width: 120px;
  }

  .rename-error {
    color: var(--reject);
    font-family: var(--font-sans, inherit);
    font-size: 12px;
  }

  .lz {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    margin-right: 7px;
    vertical-align: 0;
  }

  .mk {
    white-space: nowrap;
    margin-right: 8px;
  }

  .mk:last-child {
    margin-right: 0;
  }

  .mk-watched {
    color: var(--marked);
  }

  .mk-flagged {
    color: var(--alarm);
  }
</style>
