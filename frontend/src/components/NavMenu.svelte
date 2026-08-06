<script lang="ts">
  import { appState, type View } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { authState } from '../lib/auth.svelte'
  import { themeState, type ThemePref } from '../lib/theme.svelte'
  import { COLORWAYS, colorwayState } from '../lib/colorway.svelte'

  let open = $state(false)
  let rootEl: HTMLDivElement | undefined = $state()

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

  const modeLabels: Record<ThemePref, string> = { system: 'Auto', light: 'Light', dark: 'Dark' }
  const modeOptions: ThemePref[] = ['system', 'light', 'dark']
</script>

<div class="nav-menu" bind:this={rootEl}>
  <button
    class="trigger"
    onclick={() => (open = !open)}
    aria-haspopup="true"
    aria-expanded={open}
    title="Views, appearance, and account"
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
    <div class="menu" role="menu">
      <div class="section">
        <div class="section-label">Views</div>

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
          class:active={appState.view === 'control-ports'}
          onclick={() => toggleView('control-ports')}
          title="SSH/Telnet/control-port attempts, accepted and denied"
        >
          Control ports
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

        {#if (authState.state === 'authenticated' && authState.role === 'admin') || authState.state === 'auth-disabled'}
          <!-- The backend's callerIsAdminOrOpen treats Count()==0 (true in
               auth-disabled, since Disable only succeeds pre-account) as
               admin-equivalent for this endpoint -- matching that here
               rather than hiding a control that would actually work. -->
          <button
            class="option"
            class:active={appState.view === 'detectors'}
            onclick={toggleDetectors}
            title="Toggle behavioral detectors on/off and restrict their scope"
          >
            Detectors
          </button>
        {/if}
      </div>

      <div class="divider"></div>

      <div class="section">
        <div class="section-label">Appearance</div>

        {#each COLORWAYS as c (c.id)}
          <button
            class="option"
            class:active={c.id === colorwayState.pref}
            role="menuitemradio"
            aria-checked={c.id === colorwayState.pref}
            onclick={() => {
              colorwayState.set(c.id)
              open = false
            }}
          >
            <span class="swatch" style="background: {c.swatch}"></span>
            {c.label}
          </button>
        {/each}

        {#each modeOptions as m (m)}
          <button
            class="option"
            class:active={m === themeState.pref}
            role="menuitemradio"
            aria-checked={m === themeState.pref}
            onclick={() => {
              themeState.pref = m
              themeState.apply()
              open = false
            }}
          >
            {modeLabels[m]}
          </button>
        {/each}
      </div>

      {#if authState.state === 'authenticated'}
        <div class="divider"></div>

        <div class="section">
          <div class="section-label">Account</div>

          {#if authState.role === 'admin'}
            <button
              class="option"
              onclick={() => {
                authState.showAddUser = true
                open = false
              }}
              title="Create an additional account"
            >
              Add user
            </button>
            <div class="divider"></div>
          {/if}

          <button
            class="option"
            onclick={() => {
              authState.logout()
              open = false
            }}
            title="Sign out {authState.username}"
          >
            Sign out ({authState.username})
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
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

  .swatch {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    flex: none;
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
</style>
