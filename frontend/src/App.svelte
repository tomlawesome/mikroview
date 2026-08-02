<script lang="ts">
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import Toolbar from './components/Toolbar.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import LiveTable from './components/LiveTable.svelte'

  const STATS_REFRESH_MS = 5000

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
    }, STATS_REFRESH_MS)

    return () => {
      liveSocket.disconnect()
      clearInterval(interval)
    }
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
