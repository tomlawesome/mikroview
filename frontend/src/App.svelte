<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from './lib/state.svelte'
  import { liveSocket } from './lib/ws'
  import { themeState } from './lib/theme.svelte'
  import { colorwayState } from './lib/colorway.svelte'
  import { flagsState } from './lib/flags.svelte'
  import { watchlistState } from './lib/watchlist.svelte'
  import { authState } from './lib/auth.svelte'
  import { buildQuery, ApiError } from './lib/api'
  import { filtersFromSearchParams } from './lib/types'
  import SceneBar from './components/SceneBar.svelte'
  import Deck from './components/Deck.svelte'
  import BottomBar from './components/BottomBar.svelte'
  import { viewportState } from './lib/viewport.svelte'
  import ConnectionBanner from './components/ConnectionBanner.svelte'
  import ConfigProblemBanner from './components/ConfigProblemBanner.svelte'
  import Fleet from './components/Fleet.svelte'
  import IpLookupPopover from './components/IpLookupPopover.svelte'
  import PortLookupPopover from './components/PortLookupPopover.svelte'
  import RouterLookupPopover from './components/RouterLookupPopover.svelte'
  import NameEditorPopover from './components/NameEditorPopover.svelte'
  import AuthSetup from './components/AuthSetup.svelte'
  import AuthLogin from './components/AuthLogin.svelte'
  import SSOLinkOverlay from './components/SSOLinkOverlay.svelte'
  // Mounted here, not in the rail that triggers it: the rail is chrome
  // for authenticated pages, and this overlay outlives any one of them.
  import ChangePasswordOverlay from './components/ChangePasswordOverlay.svelte'
  // The setup wizard is a modal over the shell, not a page (#487) -- so
  // it is mounted here with the other overlays rather than reached
  // through appState.view. The rail's "Run setup…" opens it.
  import SetupWizard from './components/SetupWizard.svelte'
  // #439's "copied" confirmation -- see lib/toast.svelte.ts for why this
  // is new rather than reusing something that already existed.
  import Toast from './components/Toast.svelte'

  // The deck's scenes (#633). Entities and Settings joined the deck in
  // #647 (round 23); Fleet alone is left outside it, absorbed into the
  // Entities card and reached only from the phone-width bottom bar now.
  const DECK_VIEWS = new Set([
    'fall',
    'topography',
    'metrics',
    'live',
    'flags',
    'watchlist',
    'audit',
    'entities',
    'engineroom',
  ])
  const inDeck = $derived(DECK_VIEWS.has(appState.view))

  // Any polling call that fails with a 401 (an expired or reset-
  // invalidated session -- see internal/api's sessionUser) bounces to
  // the login view instead of failing silently forever.
  function handleApiError(err: unknown) {
    if (err instanceof ApiError && err.status === 401) {
      authState.handleUnauthorized()
    }
  }

  const STATS_REFRESH_MS = 5000
  // #546's broken ring rides the coverage answer in GET /api/definitions,
  // which is computed per request (definitionsCoverage in
  // internal/api/definitions.go) rather than cached -- not free to poll at
  // STATS_REFRESH_MS's cadence. What it is actually watching for is a
  // pushed filter table changing, and docs/routeros-setup.md's documented
  // push scheduler runs at interval=20m, so the answer this polls for
  // cannot move faster than that in a standard deployment. 60s keeps the
  // ring effectively live from the operator's point of view (still ~20x
  // faster than the fastest the answer can actually change) at a twelfth
  // of the request rate a shared STATS_REFRESH_MS interval would cost.
  const WATCHLIST_COVERAGE_REFRESH_MS = 60000
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
  authState.consumeSSOLinkedFromURL()

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
    // moment it flips to or from 'authenticated' (login, logout, or a
    // 401 bouncing back to the login view) -- same fine-grained
    // reactivity the filter-sync effect below relies on.
    if (authState.state !== 'authenticated') return

    appState.loadInitial().catch(handleApiError)
    liveSocket.connect()
    flagsState.refresh().catch(handleApiError)
    // #546's broken ring needs a live coverage answer even when Watchlist
    // itself is never opened -- the rail is chrome, not a page, so it
    // cannot wait on that page's own onMount. Gated to admin because
    // GET /api/definitions (which the ring's coverage rides on) is
    // admin-only throughout (internal/api/definitions.go), and the
    // Watchlist row this feeds is itself admin-only in the rail. The
    // immediate call here is what makes the ring correct on first paint;
    // WATCHLIST_COVERAGE_REFRESH_MS above is what keeps it correct after.
    if (authState.role === 'admin') watchlistState.refresh().catch(handleApiError)

    const statsInterval = setInterval(() => {
      appState.refreshDevicesAndStats().catch(handleApiError)
      flagsState.refresh().catch(handleApiError)
    }, STATS_REFRESH_MS)

    // Its own slower interval rather than riding statsInterval -- see
    // WATCHLIST_COVERAGE_REFRESH_MS's own comment for why the two cadences
    // are deliberately different rather than an oversight.
    const watchlistInterval =
      authState.role === 'admin'
        ? setInterval(() => watchlistState.refresh().catch(handleApiError), WATCHLIST_COVERAGE_REFRESH_MS)
        : undefined

    const tickInterval = setInterval(() => appState.tick(), TICK_MS)

    return () => {
      liveSocket.disconnect()
      clearInterval(statsInterval)
      clearInterval(watchlistInterval)
      clearInterval(tickInterval)
    }
  })

  // Reading both fields is what makes this re-run when either changes;
  // syncRuleMatches debounces, so a keystroke does not classify the
  // buffer four times while "drop" is being typed.
  $effect(() => {
    void appState.filters.rule
    void appState.filters.ruleRegex
    appState.syncRuleMatches()
  })

  $effect(() => {
    if (authState.state !== 'authenticated') return

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
  <!-- First in tab order, which is why it is here rather than in the rail
       that owns the rest of the navigation: the toolbar renders ahead of
       the rail, so a skip-link inside the rail would sit behind every
       toolbar control and skip nothing worth skipping. -->
  <a class="skip-link" href="#main-content">Skip to content</a>
  <!-- First in tab order after the skip-link, per the record: the handle
       is the only way back to a docked rail, so it cannot sit behind the
       page's own controls. -->
  <!-- Dock and density are pointer-width affordances (DESIGN.md's "Small
       screens"): at a small viewport the bottom bar is the whole of
       navigation, and neither NavRail nor NavHandle (which only ever
       restores a rail state) mounts at all. -->
  <!-- Pages are the site (owner, 2026-08-29): no persistent chrome.
       The toolbar and the desktop nav rail are retired wholesale; each
       scene carries its own bar. Navigation is the deck (#633, #647):
       the scenes are full-viewport snap cards with the roll rail as the
       jump control -- Entities and Settings among them since round 23,
       so Fleet (folded into Entities' own card) is the one page left
       outside it, reached only from the phone-width bottom bar. The
       BottomBar itself stays until the deck learns a small-screen
       shape. -->
  {#if viewportState.isMobile}
    <BottomBar />
  {/if}
  <div class="shell" class:with-bottom-bar={viewportState.isMobile}>
    <!-- The banner tops the content column and pushes content rather than
         overlaying it, per the ratified record; that is why the banners
         live inside this column and not above the deck. -->
    <div class="content">
      <ConnectionBanner />
      <ConfigProblemBanner />
      <main id="main-content" class:bare={inDeck}>
        {#if inDeck}
          <Deck />
        {:else}
          <SceneBar />
          <Fleet />
        {/if}
      </main>
    </div>
  </div>
  <IpLookupPopover />
  <PortLookupPopover />
  <RouterLookupPopover />
  <NameEditorPopover />
  <SSOLinkOverlay />
  <ChangePasswordOverlay />
  <SetupWizard />
  <Toast />
{/if}

<style>
  .skip-link {
    position: absolute;
    left: -9999px;
    top: 0;
    z-index: 100;
    padding: 8px 12px;
    background: var(--bg-elevated);
    color: var(--fg);
    border: 1px solid var(--accent);
    border-radius: 4px;
  }

  .skip-link:focus {
    left: 8px;
    top: 8px;
  }

  .shell {
    flex: 1;
    display: flex;
    min-height: 0;
  }

  /* Reserves room for BottomBar.svelte's fixed-position bar so it never
     covers the last row of content -- 52px matches .group-btn's
     min-height there. */
  .shell.with-bottom-bar {
    padding-bottom: calc(52px + env(safe-area-inset-bottom));
  }

  .content {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 14px 14px;
    min-height: 0;
  }

  /* The fall bleeds to the edges: no gutter, no card -- its canvas is
     the page ground. */
  main.bare {
    padding: 0;
    gap: 0;
  }
</style>
