<script lang="ts">
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import { flagsState } from './lib/flags.svelte'
  import { authState } from './lib/auth.svelte'
  import { buildQuery, ApiError } from './lib/api'
  import { filtersFromSearchParams } from './lib/types'
  import Toolbar from './components/Toolbar.svelte'
  import ConnectionBanner from './components/ConnectionBanner.svelte'
  import FilterBar from './components/FilterBar.svelte'
  import LiveTable from './components/LiveTable.svelte'
  import Dashboard from './components/Dashboard.svelte'
  import ControlPorts from './components/ControlPorts.svelte'
  import Flags from './components/Flags.svelte'
  import Detectors from './components/Detectors.svelte'
  import IpLookupPopover from './components/IpLookupPopover.svelte'
  import PortLookupPopover from './components/PortLookupPopover.svelte'
  import AuthSetup from './components/AuthSetup.svelte'
  import AuthLogin from './components/AuthLogin.svelte'
  import AddUserOverlay from './components/AddUserOverlay.svelte'
  import TokensOverlay from './components/TokensOverlay.svelte'

  // Any polling call that fails with a 401 (an expired or reset-
  // invalidated session -- see internal/api's sessionUser) bounces to
  // the login view instead of failing silently forever.
  function handleApiError(err: unknown) {
    if (err instanceof ApiError && err.status === 401) {
      authState.handleUnauthorized()
    }
  }

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
  // Also runs once at startup, independent of the filter params above --
  // strips and surfaces a failed OIDC callback's ?ssoError= (see
  // internal/api/oidc.go's redirectWithSSOError).
  authState.consumeSSOErrorFromURL()

  $effect(() => {
    themeState.apply()
  })

  $effect(() => {
    colorwayState.apply()
  })

  // Runs once on mount, unconditionally -- everything else in this file
  // waits on its result (authState.state) before doing anything that
  // needs a session.
  $effect(() => {
    authState.check()
  })

  $effect(() => {
    // Reading authState.state here is what makes this effect re-run the
    // moment it flips to/from 'authenticated'/'auth-disabled' (login,
    // logout, a 401 bouncing back to the login view, or skipping auth
    // setup) -- same fine-grained reactivity the filter-sync effect
    // below relies on. Both states render the same main app shell (see
    // the template below), just like they both need live data.
    if (authState.state !== 'authenticated' && authState.state !== 'auth-disabled') return

    appState.loadInitial().catch(handleApiError)
    liveSocket.connect()
    flagsState.refresh().catch(handleApiError)

    const statsInterval = setInterval(() => {
      appState.refreshDevicesAndStats().catch(handleApiError)
      flagsState.refresh().catch(handleApiError)
    }, STATS_REFRESH_MS)

    const tickInterval = setInterval(() => appState.tick(), TICK_MS)

    return () => {
      liveSocket.disconnect()
      clearInterval(statsInterval)
      clearInterval(tickInterval)
    }
  })

  $effect(() => {
    if (authState.state !== 'authenticated' && authState.state !== 'auth-disabled') return

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
      appState.refetchWithFilters().catch(handleApiError)
    }, FILTER_DEBOUNCE_MS)
    return () => clearTimeout(timer)
  })
</script>

{#if authState.state === 'loading'}
  <!-- Deliberately blank rather than a spinner -- this resolves within
       one fetch round-trip on the same origin, not worth the flash. -->
{:else if authState.state === 'setup-required'}
  <AuthSetup />
{:else if authState.state === 'unauthenticated'}
  <AuthLogin />
{:else}
  <Toolbar />
  <ConnectionBanner />
  <main>
    {#if appState.view === 'live'}
      <FilterBar />
      <LiveTable />
    {:else if appState.view === 'control-ports'}
      <ControlPorts />
    {:else if appState.view === 'flags'}
      <Flags />
    {:else if appState.view === 'detectors'}
      <Detectors />
    {:else}
      <Dashboard />
    {/if}
  </main>
  <IpLookupPopover />
  <PortLookupPopover />
  <AddUserOverlay />
  <TokensOverlay />
{/if}

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
