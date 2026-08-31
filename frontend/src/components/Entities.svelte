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
  //     that fact without alarm; a final dashed "add a third router"
  //     card carries the standing promise -- mikroview only ever
  //     receives, never connects out -- and a disclosure with the real
  //     RouterOS lines to paste (lib/setupsteps.ts, the same generator
  //     the setup wizard uses).
  //  2. One table of named things: every host entity plus every
  //     discovered-but-unnamed host, one row each, lane/address/mac/
  //     first+last seen/marks, name renamed inline (click it, Enter
  //     saves, Esc cancels). The old page's separate add-entity form and
  //     discovered-rules/-ports sections are gone: the ratified scene
  //     has exactly one table, host-shaped (lane/mac make no sense for a
  //     rule or a port), and no add-entity affordance beyond naming what
  //     has actually arrived -- see #675's own report for this reading.
  //
  // mac/first-seen/last-seen have no existing source (the entity store
  // only ever held label/tags, and the client event buffer is far too
  // short-lived for "412 d"): #675 added the minimal backend piece
  // (device.MACRegistry.NoteIP + GET /api/devices/macs) rather than
  // inventing the numbers, reusing the MAC registry that already existed
  // for the new-device detector. Lane reuses zones.svelte.ts unchanged
  // (the same boundary-derived zones the topography map draws).
  import { onMount } from 'svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { appState } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { familyOf } from '../lib/flagPalette'
  import { fetchDeviceMACs, fetchRouterRules, fetchRouterAddresses, fetchSetupStatus } from '../lib/api'
  import { discoverHosts } from '../lib/discoveredEntities'
  import { formatRelative, formatHM, formatEps } from '../lib/format'
  import { STATUS_LABEL, sortedDevices, recentCount as recentCountOf, RECENT_WINDOW_MS } from '../lib/fleet'
  import { syslogCommands, instanceAddress, portOf } from '../lib/setupsteps'
  import type { MACRegistryEntry, SetupStatus } from '../lib/types'

  // --- routers (folded in from Fleet, #647; cards since #675) ---------
  const routerRows = $derived(sortedDevices(appState.devices))

  let status = $state<SetupStatus | null>(null)
  let showPasteLines = $state(false)

  // The invite card's own heading stays honest about which router it
  // would be -- the mockup's "a third router?" is the two-router
  // example's own count, not fixed copy.
  const ORDINAL = ['a', 'a second', 'a third'] as const
  const nextRouterInvite = $derived(
    routerRows.length < ORDINAL.length ? `${ORDINAL[routerRows.length]} router?` : 'another router?',
  )

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

  function routerRate(deviceId: string): string {
    const n = recentCountOf(appState.events, deviceId, appState.now)
    return formatEps(n / (RECENT_WINDOW_MS / 1000))
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
  })

  // ---- inline rename (issue #675: rename lives in the table, not a
  // separate form) -----------------------------------------------------
  let renamingKey = $state<string | null>(null)
  let renameDraft = $state('')
  let renameSaving = $state(false)
  let renameError = $state<string | null>(null)

  function startRename(row: Row) {
    renamingKey = row.key
    renameDraft = row.label
    renameError = null
  }

  function cancelRename() {
    renamingKey = null
    renameError = null
  }

  async function saveRename(key: string) {
    renameError = null
    renameSaving = true
    const existing = hostEntities.find((e) => e.key === key)
    const wasRenaming = renamingKey
    renamingKey = null
    const err = await entitiesState.upsert({
      type: 'host',
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

  function onRenameKeydown(e: KeyboardEvent, key: string) {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveRename(key)
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
  function onRenameBlur(key: string) {
    if (renamingKey === key) saveRename(key)
  }

  function focusOnMount(node: HTMLInputElement) {
    node.focus()
    node.select()
  }
</script>

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
                  {#if detail?.lastPush}last push {formatHM(detail.lastPush)} · {/if}{routerRate(d.id)} events/s now
                </div>
              {:else if d.status === 'never_seen'}
                <div class="frow dim">never heard from yet</div>
              {:else}
                <div class="frow dim">last heard {formatRelative(d.lastSeen, appState.now)} — quiet is a fact, not a fault</div>
              {/if}
              <div class="frow dim">syslog{status?.instance.tlsEnabled ? ' TLS' : ''} · state pushed every 20 min</div>
            </div>
          {/each}
          <div class="fcard add">
            <div class="fhead"><b>{nextRouterInvite}</b></div>
            <div class="frow dim">
              point its syslog at {status ? `:${portOf(status.instance.syslogPort)}` : 'mikroview’s syslog port'} and it appears
              here.<br />Routers push to mikroview — it never connects to them.
            </div>
            <div class="frow">
              <button type="button" class="olink" onclick={() => (showPasteLines = !showPasteLines)}>
                {showPasteLines ? 'hide the RouterOS lines' : 'show the RouterOS lines to paste'} ▸
              </button>
            </div>
            {#if showPasteLines && status}
              <pre class="paste">{syslogCommands(instanceAddress({ host: location.host }), status.instance.syslogPort)}</pre>
            {/if}
          </div>
        </div>
    </div>

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
              {#if renamingKey === row.key}
                <input
                  class="rename-input"
                  type="text"
                  placeholder="friendly name"
                  bind:value={renameDraft}
                  use:focusOnMount
                  onkeydown={(e) => onRenameKeydown(e, row.key)}
                  onblur={() => onRenameBlur(row.key)}
                  disabled={renameSaving}
                />
              {:else}
                <button type="button" class="rename-btn" onclick={() => startRename(row)} title="Click to rename">
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
              {#if row.marks.alarmCount > 0}<span class="mk mk-flagged"
                >{'✱'.repeat(Math.min(row.marks.alarmCount, 3))} flagged</span
              >{/if}
            </td>
          </tr>
          {#if renamingKey === row.key && renameError}
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

  .og {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
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

  .fcard.add {
    border-style: dashed;
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

  .fcard .olink {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .fcard .olink:hover {
    text-decoration-color: currentColor;
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
