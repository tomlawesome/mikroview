<script lang="ts">
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import { buildQuery } from './lib/api'
  import { filtersFromSearchParams } from './lib/types'
  import Toolbar from './components/Toolbar.svelte'
  import ConnectionBanner from './components/ConnectionBanner.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import LiveTable from './components/LiveTable.svelte'
  import DashboardOverlay from './components/DashboardOverlay.svelte'
  import IpLookupPopover from './components/IpLookupPopover.svelte'

  const STATS_REFRESH_MS = 5000
  const FILTER_DEBOUNCE_MS = 300
  // Drives the age-based display cutoff in appState.filteredEvents. Needs
  // its own fast interval, separate from STATS_REFRESH_MS: the shortest
  // display-duration option is 5s (see retention.svelte.ts), and pruning
  // only every 5s would make that setting nearly useless -- entries would
  // sit stale for up to a full extra cycle before disappearing.
  const TICK_MS = 250

  let firstFilterRun = true

  // Runs once at startup, before the effects below, so a bookmarked or
  // shared filtered link loads pre-filtered from the very first fetch.
  const initialParams = new URLSearchParams(location.search)
  if ([...initialParams.keys()].length > 0) {
    appState.filters = filtersFromSearchParams(initialParams)
  }

  $effect(() => {
    themeState.apply()
  })

  $effect(() => {
    colorwayState.apply()
  })

  $effect(() => {
    appState.loadInitial().catch(() => {
      // initial load failure is non-fatal: the WS tail will still populate
      // the table once it connects, and the next stats poll will retry
    })
    liveSocket.connect()

    const statsInterval = setInterval(() => {
      appState.refreshDevicesAndStats().catch(() => {})
    }, STATS_REFRESH_MS)

    const tickInterval = setInterval(() => appState.tick(), TICK_MS)

    return () => {
      liveSocket.disconnect()
      clearInterval(statsInterval)
      clearInterval(tickInterval)
    }
  })

  $effect(() => {
    // Reading every field off appState.filters here is what makes this
    // effect re-run on any filter change (Svelte 5's fine-grained
    // reactivity tracks each property access as its own dependency).
    const snapshot = { ...appState.filters }

    // Keep the URL in sync so the current filtered view is always a
    // shareable/bookmarkable link -- replaceState (not pushState) so
    // typing in a filter field doesn't spam browser history.
    const qs = buildQuery(snapshot)
    history.replaceState(null, '', location.pathname + qs)

    if (firstFilterRun) {
      // Skip the network refetch on mount -- loadInitial() above already
      // covers the (possibly URL-filtered) initial fetch.
      firstFilterRun = false
      return
    }
    const timer = setTimeout(() => {
      appState.refetchWithFilters().catch(() => {})
    }, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  })
</script>

<Toolbar />
<ConnectionBanner />
<main>
  <FilterBar />
  <LiveTable />
</main>
<DashboardOverlay />
<IpLookupPopover />

<style>
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 14px 14px;
    min-height: 0;
  }
</style>
