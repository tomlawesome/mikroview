<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The engine room (#490): settings live on the machine. One page,
  // in the rail's Admin group, visible to a viewer as well as an admin
  // (see docs/design/screens/settings/DESIGN.md, the ratified record --
  // where this file and the round-2 mockup disagree, the record wins).
  //
  // The signal path (5 stations, top to bottom) on the left; the two
  // side doors (who/what may come in) on the right. Absorbs three former
  // pages wholesale -- Users.svelte, Tokens.svelte, Detectors.svelte --
  // whose *logic* lives on here (usersState/tokensState/
  // detectorSettingsState, EngineRoomDoors.svelte, EngineRoomWatchers.svelte)
  // even though their pages do not.
  //
  // Opening a station zooms rather than navigates: the opened station
  // unfolds in place, the others collapse to a slim title+number bar and
  // dim, and the path itself never leaves the screen (see
  // docs/design/screens/settings/round-2/direction-ac-engineroom.html
  // Scene 2). expandedStation is the whole of that mechanic -- null is
  // "the room at rest" (every station shows its full body), anything
  // else is "this one is open, the rest are slim bars".
  import { onMount } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { fetchSetupStatus } from '../lib/api'
  import { formatEps } from '../lib/format'
  import { portOf } from '../lib/setupsteps'
  import { scopeSummary } from '../lib/detectorCopy'
  import type { SetupStatus } from '../lib/types'
  import PageHeader from './PageHeader.svelte'
  import EngineRoomWatchers from './EngineRoomWatchers.svelte'
  import EngineRoomDoors from './EngineRoomDoors.svelte'

  type StationId = 'door' | 'store' | 'watchers' | 'flags' | 'heralds'

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  let status = $state<SetupStatus | null>(null)
  let expandedStation = $state<StationId | null>(null)

  onMount(() => {
    fetchSetupStatus()
      .then((s) => (status = s))
      .catch(() => {
        // The door/store facts that come from config.yaml simply show
        // nothing until this resolves -- see the `{#if status}` guards
        // below -- rather than a page-wide error for one station's worth
        // of context.
      })
    // Nothing else in the app calls this today (Detectors.svelte, which
    // this replaces, never did either -- it relied on an edit's own
    // update() to populate the list as a side effect). The watchers
    // station needs a real "N of M running" number the moment the room
    // is opened, not only after the first edit.
    detectorSettingsState.refresh().catch(() => {})
  })

  function toggleStation(id: StationId) {
    expandedStation = expandedStation === id ? null : id
  }

  function stationState(id: StationId): 'rest' | 'open' | 'collapsed' {
    if (expandedStation === null) return 'rest'
    return expandedStation === id ? 'open' : 'collapsed'
  }

  // The caption names the device actually driving the numbers below it:
  // whichever configured device has spoken most recently, falling back
  // to any device at all. Purely a caption convenience -- the door
  // station's own events/s figure is the network-wide total regardless
  // of which single device is named here.
  const primaryDevice = $derived.by(() => {
    const list = appState.devices
    if (list.length === 0) return null
    const configured = list.filter((d) => d.configured)
    const pool = configured.length > 0 ? configured : list
    return [...pool].sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0]
  })

  const epsText = $derived(appState.stats ? formatEps(appState.stats.eventsPerSecond) : null)

  // The store's retention fact -- read from GET /api/stats' windowSeconds
  // (the server's own store.retention window), not amended here: there is
  // no PUT endpoint for it. Per the ratified record, the knob is drawn
  // "if and only if the server exposes it" -- it does not, so this is a
  // fact with no knob, not a knob left unwired.
  const retentionHours = $derived(
    appState.stats ? Math.max(1, Math.round(appState.stats.windowSeconds / 3600)) : null,
  )

  const watchersRunning = $derived(detectorSettingsState.list.filter((d) => d.enabled).length)
  const watchersTotal = $derived(detectorSettingsState.list.length)

  // A couple of detectors and their scope, for the watchers station's
  // prose while it is not the open one -- "Open the station for the full
  // bench" is the door to everything else. Prefers detectors that
  // actually restrict something (the more interesting fact) before
  // falling back to whatever else is available, so an unconfigured
  // deployment still shows two real rows rather than nothing.
  const watcherHighlights = $derived.by(() => {
    const list = detectorSettingsState.list
    const scoped = list.filter((d) => Object.keys(d.scope ?? {}).length > 0)
    const rest = list.filter((d) => !scoped.includes(d))
    return [...scoped, ...rest].slice(0, 2)
  })

  function watcherFact(d: (typeof detectorSettingsState.list)[number]): string {
    return d.enabled ? `${d.label} ${scopeSummary(d.scope)}` : `${d.label} paused`
  }
</script>

<div class="page scrollbar">
  <PageHeader title="The engine room" readOnly={!isAdmin} />

  {#if appState.stats}
    <p class="arrives">
      the router speaks —
      {#if primaryDevice}<span class="mono">{primaryDevice.name}</span> pushes its log,{/if}
      <span class="mono">{epsText} events/s</span> this minute · mikroview never speaks back
    </p>
  {/if}

  <div class="room">
    <ul class="path" aria-label="The signal path, top to bottom">
      <!-- The door -->
      <li class="station st-{stationState('door')}">
        <button
          type="button"
          class="shead"
          aria-expanded={expandedStation === 'door'}
          aria-controls="station-door-body"
          onclick={() => toggleStation('door')}
        >
          <span class="nm">The door</span>
          {#if stationState('door') !== 'collapsed'}<span class="what">syslog listener</span>{/if}
          <span class="live"><span class="dot"></span>{epsText ?? '—'}/s in</span>
        </button>
        {#if stationState('door') !== 'collapsed'}
          <div class="sbody" id="station-door-body">
            {#if status}
              <p>
                Syslog listener on <span class="mono">{portOf(status.instance.syslogPort)}</span>
                {status.instance.tlsEnabled ? '(TLS)' : ''} —
                <span class="yaml">set in config.yaml, read at start; the room shows what is, the file decides.</span>
                Only holders of an <strong>ingest key</strong> may speak here (the machines' door, right).
              </p>
            {:else}
              <p class="yaml">set in config.yaml — the room shows what is, the file decides.</p>
            {/if}
          </div>
        {/if}
      </li>

      <!-- The store -->
      <li class="station st-{stationState('store')}">
        <button
          type="button"
          class="shead"
          aria-expanded={expandedStation === 'store'}
          aria-controls="station-store-body"
          onclick={() => toggleStation('store')}
        >
          <span class="nm">The store</span>
          {#if stationState('store') !== 'collapsed'}<span class="what">what is kept</span>{/if}
          <span class="live"><span class="dot"></span>{appState.stats ? appState.stats.count.toLocaleString() : '—'} events held</span>
        </button>
        {#if stationState('store') !== 'collapsed'}
          <div class="sbody" id="station-store-body">
            <p>
              Keeps <strong>{retentionHours !== null ? `${retentionHours} hours` : '—'}</strong> and lets the oldest fall
              off the end. Everything below reads from here; nothing anywhere probes.
            </p>
          </div>
        {/if}
      </li>

      <!-- The watchers -->
      <li class="station st-{stationState('watchers')}">
        <button
          type="button"
          class="shead"
          aria-expanded={expandedStation === 'watchers'}
          aria-controls="station-watchers-body"
          onclick={() => toggleStation('watchers')}
        >
          <span class="nm">The watchers</span>
          {#if stationState('watchers') !== 'collapsed'}<span class="what">detectors</span>{/if}
          <span class="live"><span class="dot"></span>{watchersRunning} of {watchersTotal} running</span>
        </button>
        {#if stationState('watchers') === 'open'}
          <div class="sbody" id="station-watchers-body">
            <EngineRoomWatchers {isAdmin} />
          </div>
        {:else if stationState('watchers') === 'rest'}
          <div class="sbody" id="station-watchers-body">
            <p>
              {#each watcherHighlights as d, i (d.name)}{i > 0 ? '. ' : ''}{watcherFact(d)}{/each}{watcherHighlights.length > 0 ? '. ' : ''}
              <button type="button" class="open-link" onclick={() => toggleStation('watchers')}>Open the station</button>
              for the full bench.
            </p>
          </div>
        {/if}
      </li>

      <!-- The flags desk -->
      <li class="station st-{stationState('flags')}">
        <button
          type="button"
          class="shead"
          aria-expanded={expandedStation === 'flags'}
          aria-controls="station-flags-body"
          onclick={() => toggleStation('flags')}
        >
          <span class="nm">The flags desk</span>
          {#if stationState('flags') !== 'collapsed'}<span class="what">what they raised</span>{/if}
          <span class="live"><span class="dot"></span>{flagsState.activeCount} open</span>
        </button>
        {#if stationState('flags') !== 'collapsed'}
          <div class="sbody" id="station-flags-body">
            <p>
              Raised flags wait here for a human; permanent clears become <strong>exclusions</strong>, kept on the
              desk where anyone can read why something stays quiet.
            </p>
          </div>
        {/if}
      </li>

      <!-- The heralds -->
      <li class="station st-{stationState('heralds')}">
        <button
          type="button"
          class="shead"
          aria-expanded={expandedStation === 'heralds'}
          aria-controls="station-heralds-body"
          onclick={() => toggleStation('heralds')}
        >
          <span class="nm">The heralds</span>
          {#if stationState('heralds') !== 'collapsed'}<span class="what">how word goes out</span>{/if}
          <span class="live yaml">config.yaml</span>
        </button>
        {#if stationState('heralds') !== 'collapsed'}
          <div class="sbody" id="station-heralds-body">
            <p class="yaml">
              Configured in config.yaml — stated here, amended there. The room never pretends a knob it does not
              hold.
            </p>
          </div>
        {/if}
      </li>
    </ul>

    <EngineRoomDoors {isAdmin} />
  </div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .arrives {
    margin: 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .mono {
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-style: normal;
    color: var(--fg-muted);
  }

  .room {
    display: flex;
    gap: 24px;
    align-items: flex-start;
    margin-top: 6px;
  }

  .path {
    flex: 1.3;
    min-width: 0;
    list-style: none;
    margin: 0;
    padding: 0 0 0 20px;
    position: relative;
  }

  /* The vertical spine and a node per station -- chrome, not a knob, so
     it stays a neutral border colour for both roles rather than the
     admin's accent ink (#490's colour grammar reserves accent for
     amendable/interactive ink specifically). */
  .path::before {
    content: '';
    position: absolute;
    left: 6px;
    top: 8px;
    bottom: 8px;
    width: 2px;
    background: var(--border);
  }

  .station {
    position: relative;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    margin-bottom: 12px;
    transition: opacity 0.15s ease;
  }

  .station::before {
    content: '';
    position: absolute;
    left: -20px;
    top: 16px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--bg);
    border: 2px solid var(--border);
  }

  .station.st-collapsed {
    opacity: 0.5;
  }

  .shead {
    width: 100%;
    display: flex;
    align-items: baseline;
    gap: 10px;
    padding: 10px 14px;
    background: transparent;
    border: none;
    text-align: left;
    color: var(--fg);
    cursor: pointer;
  }

  .shead:hover {
    background: var(--bg-hover);
  }

  .shead .nm {
    font-size: 13px;
    font-weight: 650;
  }

  .shead .what {
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .shead .live {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11px;
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .shead .live.yaml {
    font-style: italic;
    color: var(--fg-dim);
  }

  .shead .live .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--now);
  }

  .sbody {
    padding: 0 14px 12px;
    font-size: 12.5px;
    color: var(--fg-muted);
    line-height: 1.6;
  }

  .sbody p {
    margin: 0;
  }

  .sbody strong {
    color: var(--fg);
    font-weight: 650;
  }

  .sbody .yaml {
    color: var(--fg-dim);
    font-style: italic;
  }

  /* A solid underline, not the dashed knob ink -- "Open the station" is
     a read affordance available to a viewer too (zooming, not editing),
     so it must not borrow the dashed underline #490 reserves for the
     admin's amendable values. Identical for both roles, same as the
     rest of the room at rest. */
  .open-link {
    color: var(--fg);
    font-weight: 650;
    border-bottom: 1px solid var(--border);
    background: transparent;
    padding: 0;
    font-size: inherit;
  }

  .open-link:hover {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  @media (max-width: 900px) {
    .room {
      flex-direction: column;
    }
  }
</style>
