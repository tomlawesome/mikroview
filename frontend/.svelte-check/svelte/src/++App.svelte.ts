///<reference types="svelte" />
;// SPDX-License-Identifier: AGPL-3.0-only

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
import ConfigProblemBanner from './components/ConfigProblemBanner.svelte'
import FilterBar from './components/FilterBar.svelte'
import LiveTable from './components/LiveTable.svelte'
import Dashboard from './components/Dashboard.svelte'
import Watchlist from './components/Watchlist.svelte'
import Suggestions from './components/Suggestions.svelte'
import Flags from './components/Flags.svelte'
import Detectors from './components/Detectors.svelte'
import Entities from './components/Entities.svelte'
import Fleet from './components/Fleet.svelte'
import AuditLog from './components/AuditLog.svelte'
import Exclusions from './components/Exclusions.svelte'
import IpLookupPopover from './components/IpLookupPopover.svelte'
import PortLookupPopover from './components/PortLookupPopover.svelte'
import RouterLookupPopover from './components/RouterLookupPopover.svelte'
import AuthSetup from './components/AuthSetup.svelte'
import AuthLogin from './components/AuthLogin.svelte'
import UsersOverlay from './components/UsersOverlay.svelte'
import SSOLinkOverlay from './components/SSOLinkOverlay.svelte'
import TokensOverlay from './components/TokensOverlay.svelte';
function $$render() {

  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  
  

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
;
async () => {

if(authState.state === 'loading'){
  
} else if (authState.state === 'setup-required'){
   { const $$_puteShtuA0C = __sveltets_2_ensureComponent(AuthSetup); new $$_puteShtuA0C({ target: __sveltets_2_any(), props: {}});}
} else if (authState.state === 'unauthenticated'){
   { const $$_nigoLhtuA0C = __sveltets_2_ensureComponent(AuthLogin); new $$_nigoLhtuA0C({ target: __sveltets_2_any(), props: {}});}
}else{
   { const $$_rablooT0C = __sveltets_2_ensureComponent(Toolbar); new $$_rablooT0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_rennaBnoitcennoC0C = __sveltets_2_ensureComponent(ConnectionBanner); new $$_rennaBnoitcennoC0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_rennaBmelborPgifnoC0C = __sveltets_2_ensureComponent(ConfigProblemBanner); new $$_rennaBmelborPgifnoC0C({ target: __sveltets_2_any(), props: {}});}
   { svelteHTML.createElement("main", {});
    if(appState.view === 'live'){
       { const $$_raBretliF1C = __sveltets_2_ensureComponent(FilterBar); new $$_raBretliF1C({ target: __sveltets_2_any(), props: {}});}
       { const $$_elbaTeviL1C = __sveltets_2_ensureComponent(LiveTable); new $$_elbaTeviL1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'watchlist'){
       { const $$_tsilhctaW1C = __sveltets_2_ensureComponent(Watchlist); new $$_tsilhctaW1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'suggestions'){
       { const $$_snoitsegguS1C = __sveltets_2_ensureComponent(Suggestions); new $$_snoitsegguS1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'flags'){
       { const $$_sgalF1C = __sveltets_2_ensureComponent(Flags); new $$_sgalF1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'detectors'){
       { const $$_srotceteD1C = __sveltets_2_ensureComponent(Detectors); new $$_srotceteD1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'entities'){
       { const $$_seititnE1C = __sveltets_2_ensureComponent(Entities); new $$_seititnE1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'fleet'){
       { const $$_teelF1C = __sveltets_2_ensureComponent(Fleet); new $$_teelF1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'audit'){
       { const $$_goLtiduA1C = __sveltets_2_ensureComponent(AuditLog); new $$_goLtiduA1C({ target: __sveltets_2_any(), props: {}});}
    } else if (appState.view === 'exclusions'){
       { const $$_snoisulcxE1C = __sveltets_2_ensureComponent(Exclusions); new $$_snoisulcxE1C({ target: __sveltets_2_any(), props: {}});}
    }else{
       { const $$_draobhsaD1C = __sveltets_2_ensureComponent(Dashboard); new $$_draobhsaD1C({ target: __sveltets_2_any(), props: {}});}
    }
   }
   { const $$_revopoPpukooLpI0C = __sveltets_2_ensureComponent(IpLookupPopover); new $$_revopoPpukooLpI0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_revopoPpukooLtroP0C = __sveltets_2_ensureComponent(PortLookupPopover); new $$_revopoPpukooLtroP0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_revopoPpukooLretuoR0C = __sveltets_2_ensureComponent(RouterLookupPopover); new $$_revopoPpukooLretuoR0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_yalrevOsresU0C = __sveltets_2_ensureComponent(UsersOverlay); new $$_yalrevOsresU0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_yalrevOkniLOSS0C = __sveltets_2_ensureComponent(SSOLinkOverlay); new $$_yalrevOkniLOSS0C({ target: __sveltets_2_any(), props: {}});}
   { const $$_yalrevOsnekoT0C = __sveltets_2_ensureComponent(TokensOverlay); new $$_yalrevOsnekoT0C({ target: __sveltets_2_any(), props: {}});}
}


};
return { props: {} as Record<string, never>, exports: {}, bindings: __sveltets_$$bindings(''), slots: {}, events: {} }}
const App__SvelteComponent_ = __sveltets_2_fn_component($$render());
/*Ωignore_startΩ*/type App__SvelteComponent_ = ReturnType<typeof App__SvelteComponent_>;
/*Ωignore_endΩ*/export default App__SvelteComponent_;