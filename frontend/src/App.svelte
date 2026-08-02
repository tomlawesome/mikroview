<script lang="ts">
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import { buildQuery } from './lib/api'
  import { filtersFromSearchParams } from './lib/types'
  import Toolbar from './components/Toolbar.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import LiveTable from './components/LiveTable.svelte'

  const STATS_REFRESH_MS = 5000
  const FILTER_DEBOUNCE_MS = 300

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

    const interval = setInterval(() => {
      appState.refreshDevicesAndStats().catch(() => {})
      // Also drives the age-based display cutoff in filteredEvents -- it
      // needs *something* to re-trigger it periodically, since "an entry
      // aged past the cutoff" isn't itself a change to any $state value.
      appState.tick()
    }, STATS_REFRESH_MS)

    return () => {
      liveSocket.disconnect()
      clearInterval(interval)
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
<main>
  <FilterBar />
  <LiveTable />
</main>

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
