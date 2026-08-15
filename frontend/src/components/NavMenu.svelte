<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { appState, type View } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { auditState } from '../lib/audit.svelte'
  import { exclusionsState } from '../lib/exclusions.svelte'
  import { authState } from '../lib/auth.svelte'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { downloadEventsCsv } from '../lib/export'
  import { viewportState } from '../lib/viewport.svelte'
  import { versionState } from '../lib/version.svelte'
  import AboutOverlay from './AboutOverlay.svelte'
  import ChangePasswordOverlay from './ChangePasswordOverlay.svelte'

  versionState.ensureLoaded()

  let showAbout = $state(false)

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()
  // Shown after signing out, since the menu itself is gone by then --
  // see the Sign out button below for why a failure still signs out.
  let logoutError = $state<string | null>(null)

  function onDocClick(e: MouseEvent) {
    if (rootEl && !rootEl.contains(e.target as Node)) open = false
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })

  // Every view toggle here follows the same "click again to return to
  // live" behavior the old inline toolbar buttons had.
  function toggleView(v: Exclude<View, 'live' | 'detectors'>) {
    appState.view = appState.view === v ? 'live' : v
    open = false
  }

  function toggleDetectors() {
    if (appState.view === 'detectors') {
      appState.view = 'live'
    } else {
      appState.view = 'detectors'
      detectorSettingsState.refresh()
    }
    open = false
  }

  function toggleEntities() {
    if (appState.view === 'entities') {
      appState.view = 'live'
    } else {
      appState.view = 'entities'
      entitiesState.refresh()
    }
    open = false
  }

  function toggleAudit() {
    if (appState.view === 'audit') {
      appState.view = 'live'
    } else {
      appState.view = 'audit'
      auditState.refresh()
    }
    open = false
  }

  function toggleExclusions() {
    if (appState.view === 'exclusions') {
      appState.view = 'live'
    } else {
      appState.view = 'exclusions'
      exclusionsState.refresh()
    }
    open = false
  }

  function onMaxAgeChange(e: Event) {
    const raw = (e.target as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }
</script>

<div class="nav-menu" bind:this={rootEl}>
  {#if logoutError}
    <p class="logout-error" role="alert">
      Signed out here, but the server did not confirm it: {logoutError}
    </p>
  {/if}

  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Views, export, and account"
  >
    <span class="hamburger" aria-hidden="true">
      <span></span><span></span><span></span>
    </span>
    Menu
    {#if flagsState.activeCount > 0}
      <span class="flags-badge">{flagsState.activeCount}</span>
    {/if}
  </button>

  {#if open}
    {#if viewportState.isMobile}
      <!-- Fixed dropdown positioning (right:0 relative to the trigger)
           assumes the trigger stays at the toolbar's right edge -- at
           mobile widths the toolbar's controls wrap onto their own
           lines (issue #85) and the trigger can end up on the left,
           which pushed a right-anchored dropdown mostly off-screen. A
           bottom sheet is viewport-anchored instead, immune to wherever
           the trigger lands, and matches the visual language every
           other mobile panel here already uses (EventDetailSheet,
           FilterBar's drawer). -->
      <div class="scrim" onclick={() => (open = false)} role="presentation"></div>
    {/if}
    <div class="menu" class:mobile-sheet={viewportState.isMobile} role="menu">
      {#if viewportState.isMobile}
        <div class="handle"></div>
      {/if}
      <div class="section">
        <div class="section-label">Views</div>

        <button
          class="option"
          class:active={appState.view === 'live'}
          onclick={() => {
            appState.view = 'live'
            open = false
          }}
          title="Back to the live view"
        >
          Live view
        </button>

        <button
          class="option"
          class:active={appState.view === 'metrics'}
          onclick={() => toggleView('metrics')}
          title="Event charts and traffic breakdowns"
        >
          Metrics
        </button>

        <button
          class="option"
          class:active={appState.view === 'fleet'}
          onclick={() => toggleView('fleet')}
          title="Every known RouterOS device: live/stale/never-seen status, last-seen, and event counts"
        >
          Fleet
        </button>

        <button
          class="option"
          class:active={appState.view === 'flags'}
          onclick={() => toggleView('flags')}
          title="Behavioral flags: port scans, activity spikes, critical-port attempts, and volume spikes"
        >
          Flags
          {#if flagsState.activeCount > 0}
            <span class="flags-badge inline">{flagsState.activeCount}</span>
          {/if}
        </button>

        {#if authState.state === 'authenticated' && authState.role === 'admin'}
          <button
            class="option"
            class:active={appState.view === 'detectors'}
            onclick={toggleDetectors}
            title="Toggle behavioral detectors on/off and restrict their scope"
          >
            Detectors
          </button>
        {/if}

        {#if authState.state === 'authenticated' && authState.role === 'admin'}
          <!-- Stricter than Detectors' gate above -- entities management
               mirrors internal/api's callerIsAdmin (the same check
               POST /api/auth/users uses), not callerIsAdminOrOpen, so
               there's deliberately no "also show while auth is
               disabled" branch here (see internal/api/entities.go's own
               doc comment for why). -->
          <button
            class="option"
            class:active={appState.view === 'entities'}
            onclick={toggleEntities}
            title="Manage persisted host/rule labels and tags"
          >
            Entities
          </button>

          <!-- Same strict gate as Entities above -- GET /api/audit uses
               callerIsAdmin, not callerIsAdminOrOpen (see
               internal/api/audit.go), so this stays hidden while auth
               is disabled too. -->
          <button
            class="option"
            class:active={appState.view === 'audit'}
            onclick={toggleAudit}
            title="Review admin-privileged actions: user/token/entity/detector changes"
          >
            Audit log
          </button>

          <!-- Same strict gate again -- GET /api/flags/exclusions is
               callerIsAdmin (issue #207, moved out of the bottom of the
               Flags page). -->
          <button
            class="option"
            class:active={appState.view === 'exclusions'}
            onclick={toggleExclusions}
            title="Review and remove permanently-excluded (detector, target) pairs"
          >
            Exclusions
          </button>

          <!-- Same strict gate again -- entry management under
               /api/watchlist/entries is callerIsAdmin (issue #243,
               successor to the old, unauthenticated-by-default Control
               Ports tab). The match query itself is a looser gate
               (accessUser, reachable via a read-only bearer token too)
               but this menu item is about managing entries, not just
               viewing matches. -->
          <button
            class="option"
            class:active={appState.view === 'watchlist'}
            onclick={() => toggleView('watchlist')}
            title="Watch ports or watch a device's own destinations, observe before enforcing"
          >
            Watchlist
          </button>

          <!-- Same strict gate again -- GET /api/suggestions is
               callerIsAdmin (#243 slice 5): watchlist entries suggested
               from data RouterOS has already pushed. -->
          <button
            class="option"
            class:active={appState.view === 'suggestions'}
            onclick={() => toggleView('suggestions')}
            title="Review watchlist entries suggested from data your router has already pushed"
          >
            Suggestions
          </button>

          <!-- Admin-only, matching GET /api/setup/status's own gate: it
               enumerates every device and every address that has
               connected (#320). -->
          <button
            class="option"
            class:active={appState.view === 'setup'}
            onclick={() => toggleView('setup')}
            title="Step-by-step help connecting a MikroTik router, with commands filled in for you"
          >
            Connect a router
          </button>
        {/if}
      </div>

      {#if appState.view === 'live'}
        <!-- Live-view actions that are occasional and deliberate rather
             than touched constantly (issue #137's split, correcting
             #73's): Export lives here on both breakpoints now, where
             mobile already had it (issue #85). The display-duration
             select stays menu-only at phone widths -- desktop keeps it
             inline in the toolbar. -->
        <div class="divider"></div>

        <div class="section">
          <div class="section-label">Live view</div>

          {#if viewportState.isMobile}
            <label class="option select-option">
              Display duration
              <select
                value={retentionState.maxAgeSeconds === null ? 'null' : String(retentionState.maxAgeSeconds)}
                onchange={onMaxAgeChange}
                aria-label="Display duration"
              >
                {#each MAX_AGE_OPTIONS as opt (opt.value)}
                  <option value={opt.value === null ? 'null' : String(opt.value)}>{opt.label}</option>
                {/each}
              </select>
            </label>
          {/if}

          <!-- Deliberately one entry, not one per format: #94 defers
               additional export formats, and when they land this becomes
               a submenu (pick a format) rather than a flat item each. -->
          <button
            class="option"
            onclick={() => {
              downloadEventsCsv(appState.filteredEvents)
              open = false
            }}
            disabled={appState.filteredEvents.length === 0}
            title="Export the currently shown/filtered events to a CSV file"
          >
            Export to CSV
          </button>
        </div>
      {/if}

      {#if authState.state === 'authenticated'}
        <div class="divider"></div>

        <div class="section">
          <div class="section-label">Account</div>

          {#if authState.role === 'admin'}
            <button
              class="option"
              onclick={() => {
                authState.showUsers = true
                open = false
              }}
              title="Add or remove accounts"
            >
              Users
            </button>
            <button
              class="option"
              onclick={() => {
                authState.showTokens = true
                open = false
              }}
              title="Create/revoke read-only API bearer tokens for scripted access"
            >
              API tokens
            </button>
            <div class="divider"></div>
          {/if}

          {#if authState.hasLocalPassword}
            <!-- Every user's, like Connect SSO below: the server takes
                 the account from the session, never from the request.
                 Hidden for an SSO-only account, which has no local
                 password to change -- the server answers 409 either
                 way. -->
            <button
              class="option"
              onclick={() => {
                authState.showChangePassword = true
                open = false
              }}
              title="Change your MikroView password, and sign out everywhere else"
            >
              Change password
            </button>
          {/if}

          {#if authState.ssoAvailable && authState.hasLocalPassword}
            <!-- Deliberately outside the admin gate above: this converts
                 your OWN account, and the server takes the target from
                 the session, so it is every user's to use. Hidden once
                 there is no local password left to convert -- the server
                 refuses that with a 409 either way. -->
            <button
              class="option"
              onclick={() => {
                authState.showSSOLink = true
                open = false
              }}
              title="Sign in through your identity provider instead of a MikroView password"
            >
              Connect SSO
            </button>
          {/if}

          <button
            class="option"
            onclick={() => {
              // Caught rather than fire-and-forget: authState.logout()
              // signs out locally either way, so the only thing worth
              // saying is that the server may still hold the session.
              // Unhandled, this was a bare rejection and the user was
              // told nothing at all.
              void authState.logout().then((err) => {
                if (err) logoutError = err
              })
              open = false
            }}
            title="Sign out {authState.username}"
          >
            Sign out ({authState.username})
          </button>
        </div>
      {/if}

      <div class="divider"></div>
      <!--
        Licence obligation, not decoration. AGPL section 5(d) requires an
        interactive interface to display the Appropriate Legal Notices,
        and a prominent menu item satisfies that; section 13 requires the
        source offer to network users. Both live in AboutOverlay. If the
        menu is restructured this item moves -- it doesn't disappear.
      -->
      <button
        class="option"
        onclick={() => {
          showAbout = true
          open = false
        }}
        title="Version, copyright, licence and source code"
      >
        About &amp; licence
      </button>
      {#if versionState.version}
        <div class="version" title="Build version -- also available via GET /api/healthz or `mikroview -version`">
          {versionState.version}
        </div>
      {/if}
    </div>
  {/if}
</div>

<AboutOverlay bind:open={showAbout} />
<ChangePasswordOverlay />

<style>
  .logout-error {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 40;
    margin: 4px 0 0;
    max-width: 280px;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--panel);
    border: 1px solid var(--reject);
    color: var(--reject);
    font-size: 12px;
  }

  .nav-menu {
    position: relative;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
    position: relative;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .hamburger {
    display: inline-flex;
    flex-direction: column;
    gap: 3px;
  }

  .hamburger span {
    display: block;
    width: 14px;
    height: 1.5px;
    background: currentColor;
  }

  .menu {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    min-width: 190px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 5px;
    display: flex;
    flex-direction: column;
    gap: 1px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 20;
  }

  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 30;
  }

  .menu.mobile-sheet {
    position: fixed;
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    min-width: 0;
    max-height: 80vh;
    overflow-y: auto;
    border-radius: 16px 16px 0 0;
    border-bottom: none;
    padding: 10px 10px calc(14px + env(safe-area-inset-bottom));
    z-index: 31;
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 8px;
    flex: none;
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .section-label {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
    padding: 6px 9px 4px;
  }

  .divider {
    height: 1px;
    background: var(--border);
    margin: 5px 3px;
  }

  .option {
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    border: none;
    color: var(--fg-muted);
    padding: 7px 9px;
    border-radius: 5px;
    font-size: 13px;
    text-align: left;
    width: 100%;
  }

  .option:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .option.active {
    color: var(--fg);
    font-weight: 600;
  }

  .flags-badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    border-radius: 8px;
    background: var(--reject);
    color: #fff;
    font-size: 11px;
    font-weight: 700;
    line-height: 1;
  }

  .flags-badge.inline {
    margin-left: auto;
  }

  .select-option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    cursor: default;
  }

  .select-option select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 5px 8px;
    font-size: 12px;
  }

  .version {
    padding: 4px 9px 2px;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--fg-dim);
    text-align: center;
  }

  /* 44px minimum touch target (issue #85) -- the trigger and every
     dropdown option are the primary way a phone-width user reaches
     every view/appearance/account action, so this is the single
     highest-value place for this pass. */
  @media (max-width: 700px) {
    .trigger,
    .option,
    .select-option select {
      min-height: 44px;
    }
  }
</style>
