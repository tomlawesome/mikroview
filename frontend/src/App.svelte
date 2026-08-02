<script lang="ts">
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import Toolbar from './components/Toolbar.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import LiveTable from './components/LiveTable.svelte'

  const STATS_REFRESH_MS = 5000
  const FILTER_DEBOUNCE_MS = 300

  let firstFilterRun = true

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
    if (firstFilterRun) {
      // Skip the run that fires on mount -- loadInitial() above already
      // covers the unfiltered initial fetch.
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
