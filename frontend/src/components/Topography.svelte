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
  // naming the missing push. Lens row carries Traffic only: the other
  // lenses are unbuilt surfaces, absent rather than disabled.
  //
  // Deviation from #627's letter, declared on the issue: "the Map page
  // in the Live group's reserved slot" predates the deck -- topography
  // is a deck card (#633, rounds 20-29), and the reach (#626: a mode of
  // this scene, not a place) follows in its own change.
  import { appState } from '../lib/state.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { reachFor } from '../lib/reach'
  import { formatEps } from '../lib/format'

  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']

  // The pushed /ip address table names the zones; refreshed whenever
  // the device list itself changes (it loads after mount).
  $effect(() => {
    if (appState.devices.length > 0) zonesState.refresh()
  })

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
      <span class="lens" role="tab" aria-selected="false">Traffic</span>
    {:else}
      <span class="lens on" role="tab" aria-selected="true">Traffic</span>
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
      <!-- The one-way spine: internet into the waist. -->
      <path class="rib" d="M700 104 V 232" stroke="var(--accent)" stroke-width="3.5" />
      {#if eps > 0}
        <circle class="mote" r="2.5" fill="var(--accent)" />
      {/if}

      {#each zones as z, i (z.id)}
        <path class="rib" d={ribPath(i, zones.length)} stroke={LANE_INKS[i % LANE_INKS.length]} stroke-width="2.4" />
      {/each}

      <!-- Internet -->
      <g transform="translate(700 68)">
        <rect class="isl" x="-100" y="-30" width="200" height="60" rx="12" />
        <text x="-82" y="-3" class="n-name">Internet</text>
        {#if zonesState.wanInterface}
          <text x="-82" y="14" class="n-cidr">{zonesState.wanInterface}</text>
        {:else}
          <text x="-82" y="14" class="n-sub">no public traffic observed yet</text>
        {/if}
      </g>

      <!-- The waist -->
      <g transform="translate(700 268)">
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
          <text x="0" y="46" text-anchor="middle" class="n-sub">nothing observed yet</text>
          <text x="0" y="64" text-anchor="middle" class="n-sub">the map draws itself as traffic arrives</text>
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
