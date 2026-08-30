<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Topography, layer 1 of #485 (#627): the map alone, from what
  // mikroview already knows. Internet above, the router as the waist,
  // subnet lanes below -- fixed, deliberate positions, hand-rolled SVG,
  // never force-directed physics. One saturated colour stays reserved
  // for the alarm, which this layer never draws (that is layer 3's job).
  //
  // States per #626's ratified record: the place renders before the
  // data (frames first, traffic arrives into them), the empty state is
  // honest ("the map draws itself as traffic arrives" -- round 26's
  // first-hour beat), and while the /ip address table has not been
  // pushed the zones degrade to boundary-derived names with a caption
  // naming the missing push. The lens row carries Traffic and Policy
  // (#628, layer 2); the remaining lenses are unbuilt surfaces, absent
  // rather than disabled. One fixed picture, tabs repaint it: the
  // Policy lens keeps every island where Traffic put it and swaps the
  // observed ribs for what the pushed rule table intends.
  //
  // Deviation from #627's letter, declared on the issue: "the Map page
  // in the Live group's reserved slot" predates the deck -- topography
  // is a deck card (#633, rounds 20-29), and the reach (#626: a mode of
  // this scene, not a place) follows in its own change.
  import { appState } from '../lib/state.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { policyState, type PolicyEdge } from '../lib/policy.svelte'
  import { realityEdges, unexercisedIntents, type RealityEdge } from '../lib/reality'
  import { reachFor } from '../lib/reach'
  import { formatEps } from '../lib/format'

  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']

  // The pushed /ip address table names the zones, the pushed rule
  // table draws the policy edges; both refreshed whenever the device
  // list itself changes (it loads after mount).
  $effect(() => {
    if (appState.devices.length > 0) {
      zonesState.refresh()
      policyState.refresh()
    }
  })

  // Which lens repaints the fixed picture. Reach layers on top of
  // either (#626: a mode, not a place).
  let lens = $state<'traffic' | 'policy'>('traffic')

  const zones = $derived(zonesState.zones)
  const eps = $derived(appState.stats?.eventsPerSecond ?? 0)
  const epsText = $derived(appState.stats ? formatEps(appState.stats.eventsPerSecond) : null)

  const primaryDevice = $derived.by(() => {
    const list = appState.devices
    if (list.length === 0) return null
    const configured = list.filter((d) => d.configured)
    const pool = configured.length > 0 ? configured : list
    return [...pool].sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0]
  })

  // Lane geometry: N zones spread across the stage, ribs curving from
  // the waist. The mockup's positions for four lanes generalise to a
  // linear spread; a single lane sits centre.
  function laneX(i: number, n: number): number {
    if (n === 1) return 700
    const left = 285
    const right = 1116
    return left + ((right - left) / (n - 1)) * i
  }

  function ribPath(i: number, n: number): string {
    const x = laneX(i, n)
    const spread = n === 1 ? 0 : -55 + (110 / (n - 1)) * i
    return `M ${700 + spread} 302 C ${700 + spread * 2.2} 380, ${x + (700 - x) * 0.25} 420, ${x} 480`
  }

  // Click-through per the shaped surface: a zone lands on the live view
  // filtered to its boundary; the whole map never navigates on a miss.
  function openZone(id: string) {
    appState.setFilter('interface', id)
    appState.view = 'live'
  }

  // --- the policy lens (#628: layer 2, intended-policy edges) --------------
  // Every crossing passes the router, so every edge routes through the
  // waist -- and an intended refusal dies there, ⊣, the same grammar the
  // reach's membrane already taught. Calm ink throughout: an intended
  // block is policy, not the alarm.
  const WAIST = { x: 700, y: 312 }
  const EDGE_CAP = 12

  type EdgeAnchor = { x: number; y: number; kind: 'zone' | 'internet' | 'any' }

  function anchorOf(iface: string): EdgeAnchor | null {
    if (iface === '') return { ...WAIST, kind: 'any' }
    if (iface === zonesState.wanInterface) return { x: 700, y: 104, kind: 'internet' }
    const i = zones.findIndex((z) => z.id === iface)
    if (i === -1) return null
    return { x: laneX(i, zones.length), y: 484, kind: 'zone' }
  }

  // A line between two anchors, shared by both lenses: the Policy lens
  // draws intent along it, the Traffic lens draws what actually
  // happened (#629). `crosses` is whether it arrives or dies at the
  // waist.
  interface Line {
    from: EdgeAnchor
    to: EdgeAnchor
    /** Perpendicular offset splitting the two directions of a pair. */
    off: { x: number; y: number }
    crosses: boolean
  }

  function lineFor(fromIface: string, toIface: string, crosses: boolean): Line | null {
    const from = anchorOf(fromIface)
    const to = anchorOf(toIface)
    // A pair whose boundary the map has no island for (no address push
    // named it, nothing spoke on it) cannot be drawn honestly.
    if (!from || !to || (from.kind === 'any' && to.kind === 'any')) return null
    const dx = to.x - from.x
    const dy = to.y - from.y
    const len = Math.hypot(dx, dy) || 1
    // A→B and B→A split to either side of the pair's shared line.
    return { from, to, off: { x: (-dy / len) * 7, y: (dx / len) * 7 }, crosses }
  }

  interface DrawnEdge {
    edge: PolicyEdge
    line: Line
  }

  const drawnEdges = $derived.by((): { drawn: DrawnEdge[]; undrawn: number } => {
    const drawn: DrawnEdge[] = []
    let undrawn = 0
    for (const e of policyState.edges) {
      const line = drawn.length < EDGE_CAP ? lineFor(e.from, e.to, e.accepted) : null
      // Counted and said, never silently dropped.
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ edge: e, line })
    }
    return { drawn, undrawn }
  })

  // A refusal dies on the waist's near side, so its bar is never behind
  // the island: arriving from the internet it dies at the top edge,
  // from a lane at the bottom.
  function deathPoint(l: Line): { x: number; y: number } {
    return { x: WAIST.x + l.off.x, y: (l.from.y < 268 ? 226 : WAIST.y) + l.off.y }
  }

  // The visible line: a cubic pulled through the waist, or dying there.
  function edgePath(l: Line): string {
    const { from, to, off } = l
    const w = { x: WAIST.x + off.x, y: WAIST.y + off.y }
    if (l.crosses) {
      return `M ${from.x + off.x} ${from.y + off.y} C ${w.x} ${w.y}, ${w.x} ${w.y}, ${to.x + off.x} ${to.y + off.y}`
    }
    const dp = deathPoint(l)
    return `M ${from.x + off.x} ${from.y + off.y} Q ${(from.x + dp.x) / 2 + off.x} ${(from.y + dp.y) / 2 + off.y}, ${dp.x} ${dp.y}`
  }

  // Where the ⊣ bar and the badges sit for a line.
  function edgeBarAt(l: Line): { x: number; y: number; angle: number } {
    const dp = deathPoint(l)
    return { x: dp.x, y: dp.y, angle: (Math.atan2(dp.y - l.from.y, dp.x - l.from.x) * 180) / Math.PI + 90 }
  }

  // Badges near the internet corridor stack when several edges share
  // it, so crossing badges stagger along their line by draw order.
  const BADGE_FRACS = [0.55, 0.72, 0.42, 0.64, 0.48]

  function edgeBadgeAt(l: Line, i = 0): { x: number; y: number } {
    const { from, to, off } = l
    if (!l.crosses) {
      // Beside the bar, pushed back toward the source and clear of the
      // opposite direction's line.
      const dp = deathPoint(l)
      const dx = from.x - dp.x
      const dy = from.y - dp.y
      const len = Math.hypot(dx, dy) || 1
      return { x: dp.x + (dx / len) * 26 + off.x * 4.2, y: dp.y + (dy / len) * 26 + off.y * 4.2 }
    }
    // Past the waist, toward the destination, on its own side.
    const f = BADGE_FRACS[i % BADGE_FRACS.length]
    return { x: WAIST.x + (to.x - WAIST.x) * f + off.x * 4.2, y: WAIST.y + (to.y - WAIST.y) * f + off.y * 4.2 }
  }

  function badgeLine(e: PolicyEdge): string {
    const ports = e.accepted ? e.acceptPorts : e.refusePorts
    const shown = ports.slice(0, 3).join(' ')
    const more = ports.length > 3 ? ` +${ports.length - 3}` : ''
    const mark = e.accepted ? (e.refused ? '→ ⊣' : '→') : '⊣'
    return ports.length > 0 ? `${mark} ${shown}${more}` : mark
  }

  function edgeLabel(e: PolicyEdge): string {
    const name = (i: string, kind: string) => (kind === 'internet' ? 'the internet' : i === '' ? 'any lane' : i)
    const from = anchorOf(e.from)
    const to = anchorOf(e.to)
    const what = e.accepted ? 'may reach' : 'is refused toward'
    return `${name(e.from, from?.kind ?? 'zone')} ${what} ${name(e.to, to?.kind ?? 'zone')}${e.comment ? ` — ${e.comment}` : ''}`
  }

  // Click-through per the shaped surface: the pair and its direction,
  // said in the filters the live view already speaks -- the zones' own
  // CIDRs where the address push named them, scope for the internet
  // side, the boundary name as the fallback. A single-port edge narrows
  // to it; a port *set* stays unnarrowed (the port filter takes one
  // query), declared on #628.
  function openPair(fromIface: string, toIface: string, ports: string[]) {
    const from = anchorOf(fromIface)
    const to = anchorOf(toIface)
    appState.resetFilters()
    const fromZone = zones.find((z) => z.id === fromIface)
    const toZone = zones.find((z) => z.id === toIface)
    if (from?.kind === 'internet') appState.setFilter('srcScope', 'external')
    else if (fromZone?.cidr) appState.setFilter('srcQuery', fromZone.cidr)
    else if (fromIface) appState.setFilter('interface', fromIface)
    if (to?.kind === 'internet') appState.setFilter('dstScope', 'external')
    else if (toZone?.cidr) appState.setFilter('dstQuery', toZone.cidr)
    else if (toIface && !appState.filters.interface) appState.setFilter('interface', toIface)
    if (ports.length === 1 && /^:\d+$/.test(ports[0])) appState.setFilter('port', ports[0].slice(1))
    appState.view = 'live'
  }

  function openEdge(e: PolicyEdge) {
    openPair(e.from, e.to, e.accepted ? e.acceptPorts : e.refusePorts)
  }

  // --- the reality overlay (#629: layer 3, observed on intended) -----------
  // The Traffic lens draws what actually happened, pair by pair, judged
  // against the intended edges: unplanned traffic finally spends the
  // reserved saturated colour. What happened is also the geometry --
  // accepted traffic crosses, drops die at the waist whatever the
  // verdict, and a pair carrying both draws the crossing with a ⊣ tick
  // for its refused share.
  const observed = $derived.by(() => realityEdges(appState.events, policyState.edges, policyState.anyPushed))

  interface DrawnReality {
    r: RealityEdge
    line: Line
  }

  const drawnReality = $derived.by((): { drawn: DrawnReality[]; undrawn: number } => {
    const drawn: DrawnReality[] = []
    let undrawn = 0
    for (const r of observed) {
      const line = drawn.length < EDGE_CAP ? lineFor(r.from, r.to, r.accepts > 0) : null
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ r, line })
    }
    return { drawn, undrawn }
  })

  // The second delta: accepting rules no packet has exercised, drawn as
  // ghosts of the intent nothing arrived to fill.
  const ghostIntents = $derived.by((): DrawnEdge[] => {
    if (!policyState.anyPushed) return []
    return unexercisedIntents(observed, policyState.edges)
      .map((edge) => {
        const line = lineFor(edge.from, edge.to, true)
        return line ? { edge, line } : null
      })
      .filter((g): g is DrawnEdge => g !== null)
      .slice(0, 4)
  })

  // Volume speaks through weight: 1 event is a hairline, thousands a
  // firm stroke, never a shout.
  function realityWidth(r: RealityEdge): number {
    return Math.min(4.4, 1.3 + Math.log10(Math.max(1, r.events)))
  }

  function realityBadge(r: RealityEdge): string {
    const ports = r.topPorts.slice(0, 3).join(' ')
    const n = r.events.toLocaleString()
    if (r.accepts === 0) return `⊣ ${n}× ${r.verdict === 'holding' ? 'held' : 'dropped'}`
    const mark = r.verdict === 'unplanned' ? 'unplanned ·' : '→'
    return `${mark} ${ports ? `${ports} · ` : ''}${n}×`
  }

  function realityLabel(r: RealityEdge): string {
    const name = (i: string) => (i === zonesState.wanInterface ? 'the internet' : i)
    const what =
      r.verdict === 'unplanned'
        ? 'unplanned by any pushed rule'
        : r.verdict === 'holding'
          ? 'held by policy'
          : r.verdict === 'planned'
            ? 'as intended'
            : 'unjudged — no rule table pushed'
    return `${name(r.from)} → ${name(r.to)}: ${r.events.toLocaleString()} events, ${what}`
  }

  // --- the reach (#626: a mode of this scene, not a place) -----------------
  // Recentring folds the map into the membrane view; Esc, the
  // breadcrumb or clicking off anywhere surfaces exactly where you
  // were. The map stays beneath, blurred, at the level you left
  // (round 24); zoom and pan sleep while descended (none exist yet, so
  // there is nothing to put to sleep -- recorded for when they do).
  let reach = $state<{ zoneId: string; host: string; ip: string } | null>(null)

  function descend(zoneId: string, host: string, ip: string) {
    reach = { zoneId, host, ip }
  }

  function surface() {
    reach = null
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && reach) surface()
  }

  const reachSummary = $derived(reach ? reachFor(reach.ip, zonesState.wanInterface, appState.events) : null)

  // The zone a strand's counterpart names, for its lane ink and label.
  function zoneIndex(id: string): number {
    return zones.findIndex((z) => z.id === id)
  }

  // Counterpart slots around the membrane, mockup-placed: first
  // top-left, second right, third lower-right; the internet is always
  // the band along the foot.
  const SLOTS = [
    { x: 240, y: 26, w: 330 },
    { x: 1010, y: 224, w: 270 },
    { x: 1000, y: 430, w: 270 },
  ]

  const reachCounterparts = $derived.by(() => {
    if (!reachSummary) return []
    const seen: string[] = []
    for (const s of reachSummary.strands) {
      if (s.counterpart !== 'internet' && !seen.includes(s.counterpart)) seen.push(s.counterpart)
    }
    return seen.slice(0, SLOTS.length)
  })

  const reachHasInternet = $derived(reachSummary?.strands.some((s) => s.counterpart === 'internet') ?? false)

  // Strand geometry: from the host's edge toward the counterpart. An
  // accepted strand passes the membrane; a blocked one dies at it.
  const MX = 560
  const MY = 330
  const MR = 152

  function strandTarget(counterpart: string): { x: number; y: number } {
    if (counterpart === 'internet') return { x: 700, y: 596 }
    const i = reachCounterparts.indexOf(counterpart)
    const s = SLOTS[i] ?? SLOTS[0]
    return { x: s.x + s.w / 2, y: s.y + 48 }
  }

  // Parallel offset so every strand to one counterpart reads as its own
  // line: direction splits the pair wide, outcome nudges within it.
  function strandOffset(outcome: string, direction: string): number {
    const dir = direction === 'out' ? 1 : -1
    return dir * (outcome === 'blocked' ? 18 : 8)
  }

  function strandPath(counterpart: string, outcome: string, direction: string): string {
    const t = strandTarget(counterpart)
    const dx = t.x - MX
    const dy = t.y - MY
    const len = Math.hypot(dx, dy) || 1
    const off = strandOffset(outcome, direction)
    const ox = (-dy / len) * off
    const oy = (dx / len) * off
    const from = { x: MX + (dx / len) * 48 + ox, y: MY + (dy / len) * 48 + oy }
    const to =
      outcome === 'blocked'
        ? { x: MX + (dx / len) * MR + ox, y: MY + (dy / len) * MR + oy }
        : { x: t.x - (dx / len) * 40 + ox, y: t.y - (dy / len) * 40 + oy }
    const mid = { x: (from.x + to.x) / 2 + ox * 0.6, y: (from.y + to.y) / 2 + oy * 0.6 }
    return `M ${from.x} ${from.y} Q ${mid.x} ${mid.y}, ${to.x} ${to.y}`
  }

  function membranePoint(counterpart: string, outcome: string, direction: string): { x: number; y: number; angle: number } {
    const t = strandTarget(counterpart)
    const dx = t.x - MX
    const dy = t.y - MY
    const len = Math.hypot(dx, dy) || 1
    const off = strandOffset(outcome, direction)
    return {
      x: MX + (dx / len) * MR + (-dy / len) * off,
      y: MY + (dy / len) * MR + (dx / len) * off,
      angle: (Math.atan2(dy, dx) * 180) / Math.PI + 90,
    }
  }

  function portsLine(ports: number[]): string {
    return ports
      .slice(0, 3)
      .map((p) => `:${p}`)
      .join(' ')
  }

  const reachZoneInk = $derived(reach ? LANE_INKS[Math.max(0, zoneIndex(reach.zoneId)) % LANE_INKS.length] : 'var(--accent)')

  const siblings = $derived.by(() => {
    if (!reach) return []
    const z = zones.find((zz) => zz.id === reach!.zoneId)
    return (z?.hosts ?? []).filter((h) => h.ip !== reach!.ip).slice(0, 2)
  })
</script>

<svelte:window onkeydown={onKeydown} />

<div class="topo">
  <div class="crumb">
    {#if reach}
      <div class="path">
        <button class="crumb-link" onclick={surface}>Network</button>
        <span class="sep">▸</span>
        <button class="crumb-link" onclick={surface}>{zones.find((z) => z.id === reach?.zoneId)?.name ?? reach.zoneId}</button>
        <span class="sep">▸</span>
        <span class="here">{reach.host}</span>
      </div>
      {#if reachSummary}
        <div class="sub">
          reaches <b>{reachSummary.reaches}</b> · reached by <b>{reachSummary.reachedBy}</b>
          {#if reachSummary.topBlocked}
            {@const b = reachSummary.topBlocked}
            {@const far = b.counterpart === 'internet' ? 'the internet' : b.counterpart}
            · <b class="alarm">{b.direction === 'out' ? `blocked toward ${far}` : `knocked from ${far}, refused`} — {b.count}×</b>
          {/if}
        </div>
      {/if}
    {:else}
      <div class="path">Network <span class="sep">▸</span> <span class="here">—</span></div>
      {#if primaryDevice}
        <div class="sub"><b>{primaryDevice.name}</b> pushes its log · mikroview never connects back</div>
      {/if}
    {/if}
  </div>
  <div class="lenses" role="tablist" aria-label="Map lenses">
    {#if reach}
      <span class="lens on" role="tab" aria-selected="true">Reach</span>
      <span class="lens" role="tab" aria-selected="false">{lens === 'policy' ? 'Policy' : 'Traffic'}</span>
    {:else}
      <button class="lens" class:on={lens === 'traffic'} role="tab" aria-selected={lens === 'traffic'} onclick={() => (lens = 'traffic')}>
        Traffic
      </button>
      <button class="lens" class:on={lens === 'policy'} role="tab" aria-selected={lens === 'policy'} onclick={() => (lens = 'policy')}>
        Policy
      </button>
    {/if}
  </div>
  {#if reach}
    <button class="ascend" onclick={surface}>⌃ surface — the map, as you left it</button>
  {/if}

  <!-- While descended, the map stays beneath as the reach's backdrop —
       blurred, at the level you left (round 24); clicking it surfaces
       exactly there. -->
  <div
    class="stage"
    class:backdrop={reach !== null}
    onclick={reach ? surface : undefined}
    role={reach ? 'button' : undefined}
    aria-label={reach ? 'Surface — back to the map as you left it' : undefined}
  >
    <svg
      viewBox="0 0 1400 620"
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label="The network map: internet above, the router at the waist, observed lanes below"
    >
      {#if lens === 'traffic'}
        <!-- The one-way spine: internet into the waist. -->
        <path class="rib" d="M700 104 V 232" stroke="var(--accent)" stroke-width="3.5" />
        {#if eps > 0}
          <circle class="mote" r="2.5" fill="var(--accent)" />
        {/if}

        {#if drawnReality.drawn.length === 0}
          <!-- Before pair-carrying traffic arrives, the lanes' simple
               volume ribs keep the place alive. -->
          {#each zones as z, i (z.id)}
            <path class="rib" d={ribPath(i, zones.length)} stroke={LANE_INKS[i % LANE_INKS.length]} stroke-width="2.4" />
          {/each}
        {/if}

        <!-- The reality overlay (#629): what actually happened, pair by
             pair. Accepted traffic crosses; drops die at the waist
             whatever the verdict; unplanned traffic spends the reserved
             saturated colour. -->
        {#each drawnReality.drawn as d, di (d.r.key)}
          {@const badge = edgeBadgeAt(d.line, di)}
          <g
            class="edge-g"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to this pair: {realityLabel(d.r)}"
            onclick={() => openPair(d.r.from, d.r.to, d.r.topPorts)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openPair(d.r.from, d.r.to, d.r.topPorts)
              }
            }}
          >
            <title>{realityLabel(d.r)}</title>
            <path class="edge-hit" d={edgePath(d.line)} />
            <path
              class="redge"
              class:alarm={d.r.verdict === 'unplanned'}
              d={edgePath(d.line)}
              style:stroke-width="{realityWidth(d.r)}px"
            />
            {#if d.r.drops > 0}
              {@const bar = edgeBarAt(d.line)}
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar" class:alarm-bar={d.r.verdict === 'unplanned'} x1="-7" y1="0" x2="7" y2="0" />
              </g>
            {/if}
            <text class="edge-badge" class:alarm-t={d.r.verdict === 'unplanned'} x={badge.x} y={badge.y} text-anchor="middle">
              {realityBadge(d.r)}
            </text>
          </g>
        {/each}

        <!-- The second delta: intent nothing arrived to fill. -->
        {#each ghostIntents as g, gi (g.edge.key)}
          {@const badge = edgeBadgeAt(g.line, gi + 2)}
          <path class="gedge" d={edgePath(g.line)} />
          <text class="edge-badge ghost-t" x={badge.x} y={badge.y - 12} text-anchor="middle">never exercised</text>
        {/each}

        {#if drawnReality.drawn.length > 0 && !policyState.anyPushed}
          <text x="700" y="596" text-anchor="middle" class="n-sub">unjudged — push the rule table and this lens says which of it was intended</text>
        {/if}
        {#if drawnReality.undrawn > 0}
          <text x="1370" y="608" text-anchor="end" class="n-sub">+{drawnReality.undrawn} pair{drawnReality.undrawn === 1 ? '' : 's'} not drawn — off this map's islands, or beyond its {EDGE_CAP}-edge calm</text>
        {/if}
      {:else}
        <!-- Intended-policy edges (#628): what the pushed table says
             may cross, refused where it says it may not. Drawn beneath
             the islands, like the ribs they replace. -->
        {#each drawnEdges.drawn as d, di (d.edge.key)}
          {@const bar = edgeBarAt(d.line)}
          {@const badge = edgeBadgeAt(d.line, di)}
          <g
            class="edge-g"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to this pair: {edgeLabel(d.edge)}"
            onclick={() => openEdge(d.edge)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openEdge(d.edge)
              }
            }}
          >
            <title>{edgeLabel(d.edge)}</title>
            <path class="edge-hit" d={edgePath(d.line)} />
            <path class="edge" class:refused={!d.edge.accepted} d={edgePath(d.line)} />
            {#if !d.edge.accepted}
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar" x1="-7" y1="0" x2="7" y2="0" />
              </g>
            {:else if d.edge.refused}
              <!-- The pair also carries refusals: the crossing line
                   stands, and the ⊣ tick beside the waist says some of
                   it is turned away. -->
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar dim" x1="-5" y1="0" x2="5" y2="0" />
              </g>
            {/if}
            {#if d.edge.accepted || d.edge.refusePorts.length > 0}
              <!-- A port-less refusal's badge would only repeat the bar. -->
              <text class="edge-badge" x={badge.x} y={badge.y} text-anchor="middle">{badgeLine(d.edge)}</text>
            {/if}
          </g>
        {/each}

        {#if !policyState.anyPushed}
          <!-- Waiting for data is a state, not a fault -- say so. -->
          <g transform="translate(700 400)">
            <text y="0" text-anchor="middle" class="n-sub">no rule table has been pushed yet — nothing is broken, this lens is waiting for data</text>
            <text y="20" text-anchor="middle" class="n-sub">the policy layer draws what your router intends; Settings → Run setup… prints the push script</text>
          </g>
        {:else if drawnEdges.drawn.length === 0}
          <text x="700" y="400" text-anchor="middle" class="n-sub">the pushed table has no forward rules — nothing crosses between lanes by intent</text>
        {/if}
        {#if drawnEdges.undrawn > 0}
          <text x="1370" y="608" text-anchor="end" class="n-sub">+{drawnEdges.undrawn} pair{drawnEdges.undrawn === 1 ? '' : 's'} not drawn — off this map's islands, or beyond its {EDGE_CAP}-edge calm</text>
        {/if}
      {/if}

      <!-- Internet. Not interactive, and passive to the pointer, so a
           policy edge arriving beneath it stays clickable. -->
      <g transform="translate(700 68)" class="passive">
        <rect class="isl" x="-100" y="-30" width="200" height="60" rx="12" />
        <text x="-82" y="-3" class="n-name">Internet</text>
        {#if zonesState.wanInterface}
          <text x="-82" y="14" class="n-cidr">{zonesState.wanInterface}</text>
        {:else}
          <text x="-82" y="14" class="n-sub">no public traffic observed yet</text>
        {/if}
      </g>

      <!-- The waist. Passive like the internet: every policy edge
           routes through here, and the island must not eat their clicks. -->
      <g transform="translate(700 268)" class="passive">
        <rect class="isl waist" x="-128" y="-34" width="256" height="68" rx="12" />
        <text x="-110" y="-6" class="n-name">{primaryDevice?.name ?? 'your router'}</text>
        <text x="-110" y="12" class="n-sub">
          the waist{epsText ? ` · ${epsText} events/s` : ''}
        </text>
      </g>

      <!-- The lanes -->
      {#each zones as z, i (z.id)}
        <g
          transform="translate({laneX(i, zones.length)} 490)"
          class="zone"
          role="button"
          tabindex="0"
          aria-label="Open the stream filtered to {z.name}"
          onclick={() => openZone(z.id)}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              openZone(z.id)
            }
          }}
        >
          <rect class="isl" x="-108" y="0" width="216" height="106" rx="12" />
          <circle cx="-90" cy="22" r="3.5" fill={LANE_INKS[i % LANE_INKS.length]} />
          <text x="-79" y="26" class="n-name">{z.name}</text>
          {#if z.cidr}
            <text x="30" y="26" class="n-cidr">{z.cidr}</text>
          {/if}
          {#if z.hosts.length > 0}
            <!-- Each host is the reach's door (#626): clicking one
                 recentres on that node rather than opening the zone. -->
            <text x="-90" y="52" class="n-hosts">
              {#each z.hosts.slice(0, 3) as h, hi (h.ip)}
                {#if hi > 0}<tspan> · </tspan>{/if}
                <tspan
                  class="host-link"
                  role="button"
                  tabindex="0"
                  onclick={(e) => {
                    e.stopPropagation()
                    descend(z.id, h.label, h.ip)
                  }}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') {
                      e.stopPropagation()
                      descend(z.id, h.label, h.ip)
                    }
                  }}>{h.label}</tspan>
              {/each}
              {#if z.hostCount > 3}<tspan> · +{z.hostCount - 3}</tspan>{/if}
            </text>
          {/if}
          <text x="-90" y="86" class="n-sub">{z.eventCount.toLocaleString()} events this window</text>
        </g>
      {/each}

      {#if zones.length === 0}
        <!-- The honest empty state: the place before the data. -->
        <g transform="translate(700 500)">
          <rect class="isl ghost" x="-108" y="0" width="216" height="106" rx="12" />
          <text x="0" y="40" text-anchor="middle" class="n-sub">nothing has arrived yet — waiting for data, not broken</text>
          <text x="0" y="58" text-anchor="middle" class="n-sub">the map draws itself as traffic arrives; mikroview never draws a guess</text>
        </g>
      {/if}
    </svg>
  </div>

  {#if reach && reachSummary}
    <!-- The membrane view. pointer-events pass through everywhere
         except its own content, so clicking off anywhere surfaces. -->
    <div class="membrane-layer" aria-label="The reach: {reach.host} and what it talks to">
      <svg viewBox="0 0 1400 620" preserveAspectRatio="xMidYMid meet">
        <circle cx={MX} cy={MY} r={MR} class="membrane" />
        <text x={MX} y="502" text-anchor="middle" class="n-sub">
          the membrane — lane-mates inside talk freely; every crossing needs a rule, per direction
        </text>

        {#each reachSummary.strands as s (s.key)}
          {@const p = membranePoint(s.counterpart, s.outcome, s.direction)}
          <path
            class="strand"
            d={strandPath(s.counterpart, s.outcome, s.direction)}
            stroke={s.outcome === 'accepted' ? 'var(--accept)' : 'var(--alarm)'}
            stroke-width={s.outcome === 'accepted' ? 2.2 : 2}
          />
          {#if s.outcome === 'blocked'}
            <g transform="translate({p.x} {p.y}) rotate({p.angle})">
              <line x1="-8" y1="0" x2="8" y2="0" stroke="var(--alarm)" stroke-width="3" />
            </g>
            <!-- Labels stagger by direction and outcome so no two
                 strands of one crossing ever overprint. -->
            <text x={p.x + 14} y={p.y + (s.direction === 'in' ? 30 : -22)} class="chip-t alarm-t">
              ⊣ {s.direction === 'out' ? 'dies at the membrane' : 'refused at the membrane'} · {portsLine(s.ports)} · {s.count}×{s.refusedBy
                ? ` · ${s.refusedBy}`
                : ''}
            </text>
          {:else}
            <text x={p.x + 14} y={p.y + (s.direction === 'in' ? 14 : -6)} class="chip-t ok-t">
              {s.direction === 'out' ? '→' : '→ in'} {portsLine(s.ports)} · {s.count}×
            </text>
          {/if}
        {/each}

        <!-- the host, centred, with its lane-mates inside -->
        <g transform="translate({MX} {MY})">
          <circle r="46" class="host-circle" style:stroke={reachZoneInk} />
          {#if reach.host !== reach.ip}
            <text y="-5" text-anchor="middle" class="n-name">{reach.host}</text>
            <text y="12" text-anchor="middle" class="n-cidr small">{reach.ip}</text>
          {:else}
            <text y="4" text-anchor="middle" class="n-cidr small">{reach.ip}</text>
          {/if}
        </g>
        {#each siblings as sib, i (sib.ip)}
          <g
            transform="translate({i === 0 ? 478 : 646} {i === 0 ? 408 : 412})"
            class="sibling"
            role="button"
            tabindex="0"
            aria-label="Recentre on {sib.label}"
            onclick={(e) => {
              e.stopPropagation()
              if (reach) descend(reach.zoneId, sib.label, sib.ip)
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' && reach) {
                e.stopPropagation()
                descend(reach.zoneId, sib.label, sib.ip)
              }
            }}
          >
            <circle r="20" class="sibling-circle" style:stroke={reachZoneInk} />
            <text y="34" text-anchor="middle" class="n-sub">{sib.label}</text>
          </g>
        {/each}

        <!-- counterpart clusters -->
        {#each reachCounterparts as c, i (c)}
          {@const slot = SLOTS[i]}
          {@const strandsFor = reachSummary.strands.filter((s) => s.counterpart === c)}
          <g transform="translate({slot.x} {slot.y})" class="cluster-g">
            <rect class="cluster" x="0" y="0" width={slot.w} height="88" rx="12" />
            <circle cx="20" cy="22" r="4" fill={LANE_INKS[Math.max(0, zoneIndex(c)) % LANE_INKS.length]} />
            <text x="32" y="27" class="n-name cluster-name">{c}</text>
            {#each strandsFor.slice(0, 2) as s (s.key)}
              {@const si = strandsFor.indexOf(s)}
              <text x="20" y={52 + si * 20} class="chiprow" fill={s.outcome === 'accepted' ? 'var(--accept)' : 'var(--alarm)'}>
                {s.outcome === 'accepted' ? (s.direction === 'out' ? '✓→' : '✓→in') : '⊣'}
                {s.peers.slice(0, 2).join(' · ')} {portsLine(s.ports)} · {s.count}×
              </text>
            {/each}
          </g>
        {/each}

        {#if reachHasInternet}
          <path d="M 180 574 C 460 622, 940 622, 1220 574" fill="none" stroke="var(--hair-2)" stroke-width="1.1" />
          <text x="1250" y="580" class="n-sub">INTERNET</text>
          {#each reachSummary.strands.filter((s) => s.counterpart === 'internet').slice(0, 3) as s, si (s.key)}
            <text
              x={280 + si * 300}
              y={si % 2 === 0 ? 566 : 588}
              class="n-sub"
              fill={s.outcome === 'accepted' ? 'var(--fg-muted)' : 'var(--alarm)'}
            >
              {s.peers.slice(0, 1).join('')} {portsLine(s.ports)}
            </text>
          {/each}
        {/if}

        {#if reachSummary.strands.length === 0}
          <!-- #626's honest empty: never a spinner standing in for an answer. -->
          <text x={MX} y={MY + 80} text-anchor="middle" class="n-sub">nothing observed this window</text>
        {/if}
      </svg>
    </div>
  {/if}

  {#if zonesState.degraded && zones.length > 0 && !reach}
    <p class="degraded">
      zones are boundary-derived — no <span class="mono">/ip address</span> table has been pushed; Run setup… adds it
    </p>
  {/if}
</div>

<style>
  .topo {
    flex: 1;
    min-height: 0;
    position: relative;
    display: flex;
    flex-direction: column;
  }

  .crumb {
    position: absolute;
    top: 14px;
    left: 24px;
    z-index: 2;
  }

  .crumb .path {
    font-size: 18px;
    font-weight: 550;
    letter-spacing: -0.01em;
    color: var(--fg);
  }

  .crumb .sep {
    color: var(--fg-dim);
    font-weight: 300;
    padding: 0 8px;
  }

  .crumb .here {
    color: var(--accent);
  }

  .crumb .sub {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 3px;
  }

  .crumb .sub b {
    color: var(--fg-muted);
    font-weight: 550;
  }

  .lenses {
    position: absolute;
    top: 18px;
    right: 24px;
    z-index: 2;
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 6px;
  }

  .lens {
    font-size: 12px;
    font-weight: 550;
    color: var(--fg-dim);
    padding: 4px 13px;
    letter-spacing: 0.02em;
    background: none;
    border: none;
    font-family: inherit;
    cursor: pointer;
  }

  span.lens {
    cursor: default;
  }

  button.lens:hover {
    color: var(--fg-muted);
  }

  .lens.on {
    color: var(--fg);
    border-bottom: 2px solid var(--accent);
    margin-bottom: -7px;
  }

  .stage {
    flex: 1;
    min-height: 0;
  }

  .stage svg {
    width: 100%;
    height: 100%;
    display: block;
  }

  .stage svg text {
    font-family: inherit;
  }

  .n-name {
    fill: var(--fg);
    font-size: 15px;
    font-weight: 600;
  }

  .n-sub {
    fill: var(--fg-dim);
    font-size: 9.5px;
  }

  .n-hosts {
    fill: var(--fg-muted);
    font-size: 10px;
  }

  .n-cidr {
    fill: var(--fg-muted);
    font-size: 10px;
    font-family: var(--font-mono);
  }

  .n-cidr.small {
    font-size: 9.5px;
  }

  .cluster-name {
    font-size: 13.5px;
  }

  .isl {
    fill: var(--bg-elevated);
    stroke: var(--border);
    stroke-width: 1;
  }

  .isl.waist {
    stroke: var(--hair-2);
  }

  .isl.ghost {
    fill: transparent;
    stroke-dasharray: 4 6;
  }

  .passive {
    pointer-events: none;
  }

  .zone {
    cursor: pointer;
  }

  .zone:hover .isl,
  .zone:focus-visible .isl {
    stroke: var(--accent);
  }

  .zone:focus-visible {
    outline: none;
  }

  .rib {
    fill: none;
    stroke-linecap: round;
    opacity: 0.55;
  }

  /* --- the policy lens (#628) -------------------------------------------- */
  .edge {
    fill: none;
    stroke: var(--fg-muted);
    stroke-width: 1.8;
    stroke-linecap: round;
    opacity: 0.6;
  }

  .edge.refused {
    stroke-dasharray: 5 5;
  }

  /* The invisible hit area a 1.8px line cannot be. */
  .edge-hit {
    fill: none;
    stroke: transparent;
    stroke-width: 14;
  }

  .edge-g {
    cursor: pointer;
  }

  .edge-g:hover .edge,
  .edge-g:focus-visible .edge {
    stroke: var(--fg);
    opacity: 0.95;
  }

  .edge-g:focus-visible {
    outline: none;
  }

  .edge-bar {
    stroke: var(--fg-muted);
    stroke-width: 2.6;
  }

  /* --- the reality overlay (#629) ---------------------------------------- */
  .redge {
    fill: none;
    stroke: var(--fg-muted);
    stroke-linecap: round;
    opacity: 0.65;
  }

  .redge.alarm {
    stroke: var(--alarm);
    opacity: 0.85;
  }

  .edge-bar.alarm-bar {
    stroke: var(--alarm);
  }

  /* Intent nothing arrived to fill: fainter than any observed line. */
  .gedge {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1.1;
    stroke-dasharray: 2 7;
    stroke-linecap: round;
    opacity: 0.55;
  }

  .ghost-t {
    opacity: 0.75;
    font-style: italic;
  }

  .edge-bar.dim {
    opacity: 0.55;
  }

  .edge-badge {
    fill: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 9.5px;
  }

  .edge-g:hover .edge-badge {
    fill: var(--fg-muted);
  }

  .mote {
    opacity: 0.9;
    offset-path: path('M700 104 V 232');
    animation: travel 1.8s linear infinite;
  }

  @keyframes travel {
    from {
      offset-distance: 0%;
    }
    to {
      offset-distance: 100%;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .mote {
      display: none;
    }
  }

  /* --- the reach ----------------------------------------------------------- */
  /* Round 24: the backdrop is the map at the level you left, blurred,
     at 0.38 -- readable as the place beneath, never mistakable for the
     live layer. */
  .stage.backdrop {
    filter: blur(7px);
    opacity: 0.38;
    transition:
      filter 0.25s,
      opacity 0.25s;
    cursor: pointer;
  }

  .membrane-layer {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }

  .membrane-layer svg {
    width: 100%;
    height: 100%;
    display: block;
  }

  .membrane {
    fill: color-mix(in srgb, var(--accent) 3%, transparent);
    stroke: var(--hair-2);
    stroke-width: 1.3;
  }

  .host-circle {
    fill: var(--bg-elevated);
    stroke-width: 1.5;
  }

  .sibling {
    pointer-events: auto;
    cursor: pointer;
  }

  .sibling-circle {
    fill: var(--bg-elevated);
    stroke-opacity: 0.35;
  }

  .sibling:hover .sibling-circle,
  .sibling:focus-visible .sibling-circle {
    stroke-opacity: 0.9;
  }

  .sibling:focus-visible {
    outline: none;
  }

  .strand {
    fill: none;
    stroke-linecap: round;
    opacity: 0.8;
  }

  .cluster {
    fill: var(--glass);
    stroke: var(--hair-2);
  }

  .chiprow {
    font-family: var(--font-mono);
    font-size: 10px;
  }

  .chip-t {
    font-family: var(--font-mono);
    font-size: 9px;
  }

  .alarm-t {
    fill: var(--alarm);
  }

  .ok-t {
    fill: var(--accept);
  }

  .crumb-link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--fg);
    cursor: pointer;
  }

  .crumb-link:hover {
    color: var(--accent);
  }

  .crumb .alarm {
    color: var(--alarm);
  }

  .ascend {
    position: absolute;
    top: 64px;
    right: 24px;
    z-index: 3;
    background: none;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 14px;
    font-size: 11px;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .ascend:hover {
    color: var(--fg);
    border-color: var(--hair-2);
  }

  .host-link {
    cursor: pointer;
  }

  .host-link:hover,
  .host-link:focus-visible {
    fill: var(--accent);
  }

  .degraded {
    position: absolute;
    bottom: 10px;
    left: 24px;
    margin: 0;
    font-size: 11px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .degraded .mono {
    font-family: var(--font-mono);
    font-style: normal;
  }
</style>
