<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState } from './lib/state.svelte'
  import { subscribe as subscribeVisibility } from './lib/visibility'
  import { liveSocket } from './lib/ws'
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
  import IngestLossDrawer from './components/IngestLossDrawer.svelte'
  import ConfigProblemBanner from './components/ConfigProblemBanner.svelte'
  import Fleet from './components/Fleet.svelte'
  import IpLookupPopover from './components/IpLookupPopover.svelte'
  import PortLookupPopover from './components/PortLookupPopover.svelte'
  import RouterLookupPopover from './components/RouterLookupPopover.svelte'
  import NameEditorPopover from './components/NameEditorPopover.svelte'
  // The device dossier card (#410), one instance for the whole app --
  // every surface that opens one calls lib/dossier.svelte.ts.
  import HostDossier from './components/HostDossier.svelte'
  import AuthSetup from './components/AuthSetup.svelte'
  import AuthLogin from './components/AuthLogin.svelte'
  import SSOLinkOverlay from './components/SSOLinkOverlay.svelte'
  // The journey (#646): choreography over the shell below, not a page of
  // its own. journeyState.begin() (AuthSetup.svelte) is the only trigger;
  // outside it these three never render.
  import { journeyState } from './lib/journey.svelte'
  import JourneyAttach from './components/JourneyAttach.svelte'
  import JourneyGlass from './components/JourneyGlass.svelte'
  import JourneyTour from './components/JourneyTour.svelte'
  // Mounted here, not in the account menu that triggers it: the menu is
  // scoped to whichever scene's own bar renders it, and this overlay
  // outlives any one of them.
  import ChangePasswordOverlay from './components/ChangePasswordOverlay.svelte'
  // The setup wizard is a modal over the shell, not a page (#487) -- so
  // it is mounted here with the other overlays rather than reached
  // through appState.view. Its "Run setup…" row lives in the account
  // menu (desktop) and the bottom bar (mobile), both of which call
  // wizardState.launch() directly.
  import SetupWizard from './components/SetupWizard.svelte'
  // #439's "copied" confirmation -- see lib/toast.svelte.ts for why this
  // is new rather than reusing something that already existed.
  import Toast from './components/Toast.svelte'

  // The deck's scenes (#633). Entities and Settings joined the deck in
  // #647 (round 23), folding Fleet's own table into Entities' leading
  // section. #657 then ruled Entities and Settings out of a viewer's
  // navigation and Fleet back in as its own card (deckCards.ts), so
  // 'fleet' is a deck view too now -- reachable from the deck's roll
  // rail for a viewer, same as every other card, rather than only from
  // the phone-width bottom bar.
  //
  // #1134 brought 'tune-logging' (Log every rule) in for the same
  // reason. It rendered here, outside the deck, on the reading that it
  // was a workflow stepped into and left -- which left it the one page
  // in the app with no navigation on it at all. The owner's ruling is
  // the same shell as every other page, so it is a deck card now
  // (deckCards.ts's `log-every-rule`) and this branch is gone. Its
  // view key keeps the endpoints' own spelling; #1134 renamed the page,
  // not the two /api/tune-logging routes.
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
    'fleet',
    'tune-logging',
  ])
  const inDeck = $derived(DECK_VIEWS.has(appState.view))

  // Any polling call that fails with a 401 (an expired or reset-
  // invalidated session -- see internal/api's sessionUser) bounces to
  // the login view instead of failing silently forever.
  //
  // #1089: everything else used to be swallowed silently, so a backend
  // outage or 500 left stale numbers on screen with no indication. Every
  // non-initial poll now passes a `source` naming what it was refreshing
  // ("stats", "flags" or "watchlist"), which becomes appState.refreshError
  // -- cleared back to null by the next successful refresh of any kind
  // (see pollStats/pollFlags/pollWatchlist below). loadInitial()'s own
  // failure is left alone here: it already has its own on-screen signal
  // (appState.fetchFailed), so it calls handleApiError with no source.
  function handleApiError(err: unknown, source?: string) {
    if (err instanceof ApiError && err.status === 401) {
      authState.handleUnauthorized()
      return
    }
    if (!source) return
    const message = err instanceof Error ? err.message : String(err)
    appState.refreshError = `${source}: ${message}`
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

    // #1089: each poll clears appState.refreshError on success (whichever
    // kind refreshed -- the banner only ever claims "the last background
    // refresh failed", not which one) and tags its own failure with what
    // it was fetching so handleApiError's message says what went stale.
    function pollStats() {
      return appState
        .refreshDevicesAndStats()
        .then(() => {
          appState.refreshError = null
        })
        .catch((err) => handleApiError(err, 'stats'))
    }

    function pollFlags() {
      return flagsState
        .refresh()
        .then(() => {
          appState.refreshError = null
        })
        .catch((err) => handleApiError(err, 'flags'))
    }

    function pollWatchlist() {
      return watchlistState
        .refresh()
        .then(() => {
          appState.refreshError = null
        })
        .catch((err) => handleApiError(err, 'watchlist'))
    }

    pollFlags()
    // #546's broken ring needs a live coverage answer even when Watchlist
    // itself is never opened -- the rail is chrome, not a page, so it
    // cannot wait on that page's own onMount. Gated to canEdit because the
    // Watchlist row this feeds is visible to that tier (navGroups.ts's
    // `edit: true` on the row), not admin-only, and GET /api/definitions
    // (which the ring's coverage rides on) is accessViewer -- readable by
    // canEdit and below (#653; internal/api/authz_matrix_test.go). The
    // immediate call here is what makes the ring correct on first paint;
    // WATCHLIST_COVERAGE_REFRESH_MS above is what keeps it correct after.
    if (authState.canEdit) pollWatchlist()

    function refreshStats() {
      pollStats()
      pollFlags()
    }

    function refreshWatchlist() {
      pollWatchlist()
    }

    let statsInterval: ReturnType<typeof setInterval> | undefined
    // Its own slower interval rather than riding statsInterval -- see
    // WATCHLIST_COVERAGE_REFRESH_MS's own comment for why the two cadences
    // are deliberately different rather than an oversight.
    let watchlistInterval: ReturnType<typeof setInterval> | undefined
    let tickInterval: ReturnType<typeof setInterval> | undefined

    function startIntervals() {
      statsInterval = setInterval(refreshStats, STATS_REFRESH_MS)
      watchlistInterval = authState.canEdit
        ? setInterval(refreshWatchlist, WATCHLIST_COVERAGE_REFRESH_MS)
        : undefined
      tickInterval = setInterval(() => appState.tick(), TICK_MS)
    }

    function stopIntervals() {
      clearInterval(statsInterval)
      clearInterval(watchlistInterval)
      clearInterval(tickInterval)
    }

    startIntervals()

    // #1088: a backgrounded tab gains nothing from polling at full rate.
    // Pause statsInterval/watchlistInterval/tickInterval while hidden, and
    // on return do one immediate refresh (not just a resumed interval) so
    // the numbers aren't stale for up to a further full interval on top of
    // however long the tab was hidden.
    const stopWatchingVisibility = subscribeVisibility((hidden) => {
      if (hidden) {
        stopIntervals()
        return
      }
      refreshStats()
      if (authState.canEdit) refreshWatchlist()
      startIntervals()
    })

    // The interval above is the backstop, not the mechanism. Coverage is
    // an answer about pushed router tables and the definitions read
    // against them, so it changes at exactly two moments the server
    // already knows about -- a router pushing a table, and a definition
    // being written -- and the socket is already open. Without this, an
    // operator who switched logging on for a rule and watched it push
    // sat looking at a ring still claiming nothing could feed the watch,
    // for up to a minute, with no way to tell a slow poll from a change
    // that had not registered.
    //
    // Same canEdit gate and the same refresh call as the interval, so a
    // notice can never fetch something the poll would not have.
    const stopListeningForChanges = authState.canEdit
      ? liveSocket.onChange((change) => {
          if (change === 'router-state' || change === 'definitions') {
            pollWatchlist()
          }
        })
      : undefined

    return () => {
      stopWatchingVisibility()
      stopListeningForChanges?.()
      liveSocket.disconnect()
      stopIntervals()
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
  <!-- First in tab order: rendered ahead of BottomBar and every scene's
       own bar, so a keyboard user reaches it before any navigation
       chrome rather than having to tab past it. -->
  <a class="skip-link" href="#main-content">Skip to content</a>
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
    <!-- ConnectionBanner's connecting/disconnected line tops the content
         column and pushes content rather than overlaying it, per the
         ratified record. IngestLossDrawer sits directly under it and
         does the same (#1015): it is where the ingest-loss banners
         always were, and what it adds is folding away to a 3px line
         rather than holding that space open for good. It overlaid the
         column in a first cut, which covered each scene's own bar --
         see the drawer's own header comment. -->
    <div class="content">
      <ConnectionBanner />
      <IngestLossDrawer />
      <ConfigProblemBanner />
      <main id="main-content" class:bare={inDeck && journeyState.phase !== 'attach'}>
        {#if journeyState.phase === 'attach'}
          <JourneyAttach />
        {:else if inDeck}
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
  <HostDossier />
  <SSOLinkOverlay />
  <ChangePasswordOverlay />
  <SetupWizard />
  <!-- Beats 4/5 (connecting, then the glass) float over the live fall;
       beat 6 (the tour) rings the deck's own cards -- both stay mounted
       alongside the shell above rather than replacing it, since the
       whole point is that it plays out over the real, filling app. -->
  {#if journeyState.phase === 'connecting' || journeyState.phase === 'glass'}
    <JourneyGlass />
  {/if}
  {#if journeyState.phase === 'touring'}
    <JourneyTour />
  {/if}
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
