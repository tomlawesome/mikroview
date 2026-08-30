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
  import { formatEps } from '../lib/format'

  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']

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

  function hostsLine(hosts: string[], count: number): string {
    const shown = hosts.slice(0, 3)
    const more = count - shown.length
    return shown.join(' · ') + (more > 0 ? ` · +${more}` : '')
  }
</script>

<div class="topo">
  <div class="crumb">
    <div class="path">Network <span class="sep">▸</span> <span class="here">—</span></div>
    {#if primaryDevice}
      <div class="sub"><b>{primaryDevice.name}</b> pushes its log · mikroview never connects back</div>
    {/if}
  </div>
  <div class="lenses" role="tablist" aria-label="Map lenses">
    <span class="lens on" role="tab" aria-selected="true">Traffic</span>
  </div>

  <div class="stage">
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
            <text x="-90" y="52" class="n-hosts">{hostsLine(z.hosts, z.hostCount)}</text>
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

  {#if zonesState.degraded && zones.length > 0}
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
