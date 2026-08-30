<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Metrics (#488, docs/design/screens/metrics/DESIGN.md): "three views
  // of one data set, chosen in the page header" -- Seismograph
  // (default), Register, Table.
  //
  // This file owns the three things the views share and must not each
  // decide for themselves: the hour (lib/metricsSeries.ts builds it
  // once from both payloads), the cursor (one minute, held by ISO time
  // in lib/metrics.svelte.ts so it survives a view switch and a sliding
  // axis), and the keyboard, which is here rather than inside a view so
  // the same keys move the same cursor whichever view is on screen.
  //
  // It replaced the old dashboard wholesale: the overlay charts
  // (EventsChart/FlagsChart) and the ranked-count cards are gone, their
  // magnitude answers rehoused in MetricsTotals.svelte -- the Register's
  // ledger strip and the Table's opening section.
  import { appState } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { metricsPref, METRICS_VIEWS } from '../lib/metrics.svelte'
  import { buildHour, minuteIndexOf, readMinute } from '../lib/metricsSeries'
  import { fetchStatsTops } from '../lib/api'
  import { formatEps, formatHM } from '../lib/format'
  import PageHeader from './PageHeader.svelte'
  import MetricsSeismograph from './MetricsSeismograph.svelte'
  import MetricsRegister from './MetricsRegister.svelte'
  import MetricsTable from './MetricsTable.svelte'
  import type { HourTopBucket } from '../lib/types'

  // #644 round 21's top-port/top-talker columns: GET /api/stats/tops is
  // its own poll, scoped to this page rather than riding App.svelte's
  // global STATS_REFRESH_MS -- same reasoning, and the same POLL_MS
  // pattern, as Fall.svelte's own per-page fetch. See fetchStatsTops'
  // own doc comment for why this isn't folded into appState.stats.
  const TOPS_POLL_MS = 5000
  let tops = $state<HourTopBucket[]>([])

  $effect(() => {
    let cancelled = false
    async function refresh() {
      try {
        const result = await fetchStatsTops()
        if (!cancelled) tops = result
      } catch {
        // A failed poll leaves the previous tops in place -- the table
        // then shows slightly stale (rather than blank) top port/talker
        // columns until the next successful poll, matching how a failed
        // appState.refreshDevicesAndStats() leaves the rest of the page
        // showing its last-known figures instead of clearing them.
      }
    }
    refresh()
    const id = setInterval(refresh, TOPS_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  })

  const hour = $derived(buildHour(appState.stats?.timeSeries ?? [], flagsState.timeSeries, tops))
  const cursor = $derived(minuteIndexOf(hour.axis, metricsPref.minute))
  const reading = $derived(readMinute(hour, cursor))

  const perMinuteNow = $derived(hour.traffic.reduce((a, s) => a + s.now, 0))
  const refusedAtCursor = $derived(
    reading ? reading.traffic.filter((r) => r.ink === 'refused').reduce((a, r) => a + r.value, 0) : 0,
  )
  const eventsAtCursor = $derived(reading ? reading.traffic.reduce((a, r) => a + r.value, 0) : 0)

  function select(index: number) {
    if (index < 0 || index >= hour.axis.length) return
    metricsPref.select(hour.axis[index])
  }

  // Every move is announced with the minute and its figures, per the
  // record -- a cursor that moves silently is a cursor a screen-reader
  // user cannot use to read anything. It rides aria-valuetext rather
  // than a live region so it is spoken as the slider's value, not as a
  // second voice talking over the page.
  const valueText = $derived.by(() => {
    if (!reading) return 'No minute selected'
    const figures = reading.traffic.map((t) => `${t.label} ${t.value}`).join(', ')
    const episodes =
      reading.episodeTotal === 0
        ? 'no flag episodes'
        : `${reading.episodeTotal} flag episode${reading.episodeTotal === 1 ? '' : 's'}: ${reading.episodes
            .map((e) => `${e.label}${e.value > 1 ? ` times ${e.value}` : ''}`)
            .join(', ')}`
    return `${formatHM(reading.time)} — ${figures}; ${episodes}`
  })

  function move(delta: number) {
    const n = hour.axis.length
    if (n === 0) return
    const from = cursor >= 0 ? cursor : n - 1
    select(Math.min(n - 1, Math.max(0, from + delta)))
  }

  // Right/Up move toward the brink in both drawn views (the drum feeds
  // from the right, the register hangs from the top), so one mapping
  // means the same thing in all three.
  function onkeydown(event: KeyboardEvent) {
    const step = event.shiftKey ? 10 : 1
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        move(-step)
        break
      case 'ArrowRight':
      case 'ArrowUp':
        move(step)
        break
      case 'Home':
        select(0)
        break
      case 'End':
        select(hour.axis.length - 1)
        break
      case 'Escape':
        if (metricsPref.minute === null) return
        metricsPref.select(null)
        break
      default:
        return
    }
    event.preventDefault()
  }
</script>

<div class="metrics scrollbar" onkeydown={onkeydown} role="presentation">
  <PageHeader title="Metrics">
    <div class="views" role="group" aria-label="Metrics view">
      {#each METRICS_VIEWS as option (option.value)}
        <button
          class="view"
          class:on={metricsPref.view === option.value}
          aria-pressed={metricsPref.view === option.value}
          title={option.title}
          onclick={() => metricsPref.setView(option.value)}>{option.label}</button
        >
      {/each}
    </div>
  </PageHeader>

  <div class="hourline">
    {#if reading}
      <span class="big">{formatHM(reading.time)}<span class="unit">the minute under the cursor</span></span>
      <span class="sep">·</span>
      <span class="fact"><b>{refusedAtCursor}</b> refused of <b>{eventsAtCursor}</b> events</span>
      <span class="sep">·</span>
      <span class="fact"><b>{reading.episodeTotal}</b> flag episodes</span>
    {:else}
      <span class="big">{formatEps(appState.stats?.eventsPerSecond ?? 0)}<span class="unit">events/s now</span></span>
      <span class="sep">·</span>
      <span class="fact"><b>{perMinuteNow}</b>/min</span>
      <span class="sep">·</span>
      <span class="fact"><b>{hour.eventsInHour}</b> events in the hour</span>
      <span class="sep">·</span>
      <span class="fact"
        ><b>{hour.episodesInHour}</b> episodes raised, from <b>{hour.typesThatSpoke}</b> of {hour.flags.length} types</span
      >
    {/if}
    {#if hour.brink}
      <span class="brinkmark">the brink · {formatHM(hour.brink)}</span>
    {/if}
  </div>

  <!-- A slider is what this actually is: one value moving along the
       hour, with exactly the arrows/Shift/Home/End contract the record
       asks for. It also gets the record's "each move is announced with
       the minute and its figures" from aria-valuetext, which is read on
       change without a live region shouting over the page. -->
  <div
    class="surface"
    tabindex="0"
    role="slider"
    aria-label="The minute under the cursor"
    aria-orientation={metricsPref.view === 'seismograph' ? 'horizontal' : 'vertical'}
    aria-valuemin="0"
    aria-valuemax={Math.max(0, hour.axis.length - 1)}
    aria-valuenow={cursor >= 0 ? cursor : Math.max(0, hour.axis.length - 1)}
    aria-valuetext={valueText}
  >
    {#if metricsPref.view === 'seismograph'}
      <MetricsSeismograph {hour} {cursor} onselect={select} />
    {:else if metricsPref.view === 'register'}
      <MetricsRegister {hour} {cursor} {reading} onselect={select} />
    {:else}
      <MetricsTable {hour} {cursor} onselect={select} />
    {/if}
  </div>

  <p class="keys">
    Click a minute to read it across every series · <kbd>←</kbd><kbd>→</kbd> a minute · <kbd>Shift</kbd> ten ·
    <kbd>Home</kbd>/<kbd>End</kbd> the ends of the hour · <kbd>Esc</kbd> clears the cursor
  </p>

  <p class="sr-only" role="status">{metricsPref.announcement}</p>
</div>

<style>
  .metrics {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 12px;
    overflow-y: auto;
    padding-bottom: 10px;
  }

  .views {
    display: flex;
    gap: 6px;
    margin-left: auto;
  }

  .view {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--fg-muted);
    font-size: 11.5px;
    padding: 3px 10px;
  }

  .view:hover {
    color: var(--fg);
    border-color: var(--fg-dim);
  }

  .view.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  .hourline {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 10px;
    padding-bottom: 10px;
    border-bottom: 1px solid var(--border);
  }

  .big {
    font-family: var(--font-mono);
    font-size: 24px;
    font-weight: 650;
    color: var(--fg);
    line-height: 1;
  }

  .big .unit {
    font-family: var(--font-sans);
    font-size: 11.5px;
    font-weight: 500;
    color: var(--fg-dim);
    margin-left: 6px;
  }

  .sep {
    color: var(--fg-dim);
    opacity: 0.6;
  }

  .fact {
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .fact b {
    color: var(--fg);
    font-family: var(--font-mono);
    font-weight: 600;
  }

  .brinkmark {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.08em;
    color: var(--now);
  }

  .surface {
    min-width: 0;
  }

  .surface:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 4px;
    border-radius: 4px;
  }

  .keys {
    margin: 0;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: 10px;
    border: 1px solid var(--border);
    border-bottom-width: 2px;
    border-radius: 4px;
    padding: 0 4px;
    margin: 0 1px;
  }

  /* Clipped rather than hidden -- display:none would remove the live
     region from the accessibility tree and silence every announcement. */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
