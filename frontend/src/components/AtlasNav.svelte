<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The atlas (owner ratification, 2026-08-29): the app's navigator,
  // replacing the persistent rail and toolbar chrome wholesale. One
  // gesture -- the wordmark on any scene, or `m` -- opens this
  // full-screen overlay: the operator's network drawn as zones on
  // dotted orbits around the router (the Atlas II refined identity's
  // own vocabulary), fully clickable. Clicking a zone is the reach
  // gesture: Stream opens filtered to that boundary. The app's few
  // destinations sit beneath the chart as plain ports of call.
  //
  // The zones come from the same boundary model the fall renders
  // (lib/fall.svelte.ts), so the chart is honest today; #485's Map
  // replaces this chart with the real topography when it lands -- this
  // overlay is the socket, not the Map.
  //
  // AGPL 5(d)/13: "About & licence" (AboutOverlay) is reachable here,
  // the only navigator, for every auth state and role.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import { atlasNav } from '../lib/atlasNav.svelte'
  import { fallState, laneColors, openBoundaryInStream, type FallBoundary } from '../lib/fall.svelte'
  import { visibleGroups, type NavItem } from '../lib/navGroups'
  import AboutOverlay from './AboutOverlay.svelte'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import UptimeBadge from './UptimeBadge.svelte'
  import DeviceStatus from './DeviceStatus.svelte'
  import ThemeMenu from './ThemeMenu.svelte'

  const W = 1200
  const H = 600
  const CX = W / 2
  const CY = H / 2 + 10

  let showAbout = $state(false)
  let closeBtn = $state<HTMLButtonElement | null>(null)
  let logoutError = $state<string | null>(null)

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  const groups = $derived(visibleGroups(isAdmin))
  const lanes = $derived(laneColors(fallState.boundaries))

  interface ZoneNode {
    b: FallBoundary
    x: number
    y: number
    lane: string
    anchor: 'start' | 'middle' | 'end'
  }

  // Observed zones ride the inner orbit, dark and unknown ones the
  // outer -- the guarded edge of the chart. Ellipses, not circles: the
  // canvas is wide, and the mockup's orbits are.
  const zones = $derived.by((): { inner: ZoneNode[]; outer: ZoneNode[] } => {
    const obs = fallState.boundaries.filter((b) => b.coverage === 'observed')
    const rest = fallState.boundaries.filter((b) => b.coverage !== 'observed')
    // The outer ring starts phase-shifted so its zones never stack
    // directly above an inner-ring zone at the same clock position.
    function place(list: FallBoundary[], rx: number, ry: number, phase = 0): ZoneNode[] {
      return list.map((b, i) => {
        const a = -Math.PI / 2 + phase + (i * 2 * Math.PI) / Math.max(list.length, 1)
        const x = CX + rx * Math.cos(a)
        const y = CY + ry * Math.sin(a)
        const anchor = Math.cos(a) > 0.25 ? 'start' : Math.cos(a) < -0.25 ? 'end' : 'middle'
        return { b, x, y, lane: lanes.get(b.key) ?? '', anchor }
      })
    }
    return { inner: place(obs, 340, 170), outer: place(rest, 480, 240, 0.6) }
  })

  function reach(b: FallBoundary) {
    openBoundaryInStream(b)
    atlasNav.open = false
  }

  function go(item: NavItem) {
    if (item.action === 'run-setup') wizardState.launch()
    else if (item.view) appState.view = item.view
    atlasNav.open = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (atlasNav.open && e.key === 'Escape') {
      e.preventDefault()
      atlasNav.open = false
      return
    }
    if (e.key !== 'm' || e.metaKey || e.ctrlKey || e.altKey) return
    const t = e.target as HTMLElement | null
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
    e.preventDefault()
    atlasNav.toggle()
  }

  $effect(() => {
    if (atlasNav.open) closeBtn?.focus()
  })

  function zoneSummary(b: FallBoundary): string {
    const parts = [b.label]
    if (b.epithet) parts.push(b.epithet)
    if (b.coverage === 'dark') parts.push('dark -- blank because nothing is logged, not because nothing is sent')
    else if (b.coverage === 'unknown') parts.push('coverage unknown')
    parts.push('activate to reach into it: Stream, filtered to this boundary')
    return parts.join('. ')
  }

  async function signOut() {
    logoutError = await authState.logout()
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if atlasNav.open}
  <div class="atlas" role="dialog" aria-modal="true" aria-label="The atlas — your network, and everywhere the app goes">
    <div class="bar">
      <span class="wm">MIKRO<em>VIEW</em></span>
      <h1>the atlas</h1>
      <span class="status"><ConnectionIndicator /><UptimeBadge /><DeviceStatus /></span>
      <ThemeMenu />
      <button class="close" bind:this={closeBtn} onclick={() => (atlasNav.open = false)}>esc · close</button>
    </div>

    {#if fallState.boundaries.length === 0}
      <p class="empty">No zones yet — waiting for a router to push its filter rules. Run setup… below configures the push.</p>
    {:else}
      <svg viewBox="0 0 {W} {H}" aria-label="Zones on orbit around the router; activate one to reach into it">
        <ellipse class="orbit" cx={CX} cy={CY} rx="340" ry="170" />
        <ellipse class="orbit" cx={CX} cy={CY} rx="480" ry="240" />
        {#each [...zones.inner, ...zones.outer] as z (z.b.key)}
          <line class="rib" x1={CX} y1={CY} x2={z.x} y2={z.y} />
        {/each}
        <g class="station">
          <circle cx={CX} cy={CY} r="30" class="station-ring" />
          <circle cx={CX} cy={CY} r="3" class="station-core" />
          <text x={CX} y={CY + 48} text-anchor="middle" class="station-label">the router</text>
        </g>
        {#each [...zones.inner, ...zones.outer] as z (z.b.key)}
          {@const lx = z.anchor === 'start' ? z.x + 16 : z.anchor === 'end' ? z.x - 16 : z.x}
          {@const lines = 1 + (z.b.epithet ? 1 : 0) + (z.b.coverage !== 'observed' ? 1 : 0)}
          {@const ly = z.y < CY - 8 ? z.y - 18 - (lines - 1) * 15 : z.y + 24}
          <g
            class="zone"
            class:dark={z.b.coverage === 'dark'}
            role="button"
            tabindex="0"
            aria-label={zoneSummary(z.b)}
            onclick={() => reach(z.b)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                reach(z.b)
              }
            }}
          >
            <circle cx={z.x} cy={z.y} r="22" class="zone-hit" />
            <circle cx={z.x} cy={z.y} r="9" class="zone-dot" style="stroke: {z.lane || 'var(--fg-dim)'}" />
            <text x={lx} y={ly} text-anchor={z.anchor} class="zone-label">{z.b.label}</text>
            {#if z.b.epithet}<text x={lx} y={ly + 15} text-anchor={z.anchor} class="zone-epithet">{z.b.epithet}</text>{/if}
            {#if z.b.coverage === 'dark'}
              <text x={lx} y={ly + (z.b.epithet ? 30 : 15)} text-anchor={z.anchor} class="zone-state bad">DARK — NO LOG RULE</text>
            {:else if z.b.coverage === 'unknown'}
              <text x={lx} y={ly + (z.b.epithet ? 30 : 15)} text-anchor={z.anchor} class="zone-state">COVERAGE UNKNOWN</text>
            {/if}
          </g>
        {/each}
      </svg>
      <p class="hint">click a zone to reach into it — its traffic, filtered · esc closes</p>
    {/if}

    <nav class="ports" aria-label="Destinations">
      {#each groups as g (g.name)}
        <div class="pgroup">
          <span class="pname">{g.name}</span>
          {#each g.items as item (item.label)}
            <button class="port" class:on={item.view === appState.view} title={item.title} onclick={() => go(item)}>
              {item.label}
              {#if item.badge && flagsState.activeCount > 0}<span class="badge">{flagsState.activeCount}</span>{/if}
              {#if item.ring && watchlistState.brokenCount > 0}<span class="ring" title="A watch is broken"></span>{/if}
            </button>
          {/each}
        </div>
      {/each}
      <div class="pgroup">
        <span class="pname">{authState.username}</span>
        {#if authState.hasLocalPassword}
          <button class="port" onclick={() => ((authState.showChangePassword = true), (atlasNav.open = false))}>Change password</button>
        {/if}
        {#if authState.ssoAvailable && authState.hasLocalPassword}
          <button class="port" onclick={() => ((authState.showSSOLink = true), (atlasNav.open = false))}>Use single sign-on</button>
        {/if}
        <button class="port" onclick={signOut}>Sign out</button>
        <button class="port" onclick={() => (showAbout = true)}>About &amp; licence</button>
      </div>
      {#if logoutError}<p class="err" role="alert">{logoutError}</p>{/if}
    </nav>
  </div>
{/if}

<AboutOverlay bind:open={showAbout} />

<style>
  .atlas {
    position: fixed;
    inset: 0;
    z-index: 80;
    display: flex;
    flex-direction: column;
    background:
      radial-gradient(1100px 600px at 75% -15%, rgba(80, 115, 205, 0.07), transparent 60%),
      var(--bg);
    overflow-y: auto;
  }

  .bar {
    display: flex;
    align-items: baseline;
    gap: 20px;
    padding: 14px 24px 6px;
  }
  .wm {
    font-size: 13px;
    font-weight: 800;
    letter-spacing: 0.22em;
    color: var(--fg-dim);
  }
  .wm em {
    color: var(--accent);
    font-style: normal;
  }
  .bar h1 {
    margin: 0;
    font-size: 22px;
    font-weight: 600;
    color: var(--fg);
  }
  .status {
    display: inline-flex;
    gap: 12px;
    align-items: baseline;
    margin-left: auto;
  }
  .close {
    background: transparent;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    color: var(--fg-muted);
    font-size: 12.5px;
    padding: 4px 13px;
  }
  .close:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  svg {
    width: min(100%, 1400px);
    margin: 0 auto;
    display: block;
    flex: 1;
    min-height: 0;
  }
  .orbit {
    fill: none;
    stroke: var(--border);
    stroke-dasharray: 2 7;
  }
  .rib {
    stroke: var(--border);
    stroke-width: 1;
  }
  .station-ring {
    fill: var(--glass);
    stroke: var(--accent);
    stroke-width: 1.4;
  }
  .station-core {
    fill: var(--accent);
  }
  .station-label {
    fill: var(--fg-muted);
    font-size: 12.5px;
    font-family: var(--font-mono);
  }

  .zone {
    cursor: pointer;
  }
  .zone-hit {
    fill: transparent;
  }
  .zone-dot {
    fill: var(--glass);
    stroke-width: 1.8;
  }
  .zone.dark .zone-dot {
    stroke-dasharray: 2 3;
  }
  .zone:hover .zone-dot,
  .zone:focus-visible .zone-dot {
    stroke-width: 3;
  }
  .zone:focus-visible {
    outline: none;
  }
  .zone:focus-visible .zone-hit {
    stroke: var(--accent);
    stroke-width: 1.5;
  }
  .zone-label {
    fill: var(--fg);
    font-size: 14px;
    font-weight: 650;
  }
  .zone-epithet {
    fill: var(--fg-dim);
    font-size: 11px;
  }
  .zone-state {
    fill: var(--fg-dim);
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.07em;
  }
  .zone-state.bad {
    fill: var(--fall-drop);
  }

  .hint,
  .empty {
    margin: 0;
    text-align: center;
    font-size: 12.5px;
    color: var(--fg-dim);
  }
  .empty {
    padding: 60px 0;
    font-size: 13px;
  }

  .ports {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 28px;
    align-items: baseline;
    justify-content: center;
    padding: 14px 24px 22px;
  }
  .pgroup {
    display: inline-flex;
    gap: 6px;
    align-items: baseline;
  }
  .pname {
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    margin-right: 4px;
  }
  .port {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--fg-muted);
    font-size: 13px;
    padding: 4px 12px;
    display: inline-flex;
    gap: 6px;
    align-items: center;
  }
  .port:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }
  .port.on {
    color: var(--fg);
    border-color: var(--accent);
  }
  .badge {
    background: var(--alarm);
    color: var(--bg);
    border-radius: 999px;
    font-size: 10px;
    font-weight: 700;
    padding: 0 6px;
    line-height: 15px;
  }
  .ring {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 2px solid var(--alarm);
    display: inline-block;
  }
  .err {
    width: 100%;
    text-align: center;
    color: var(--alarm);
    font-size: 11.5px;
    margin: 0;
  }
</style>
