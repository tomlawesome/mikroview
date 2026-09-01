<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The account menu (#633, slimmed by #647 round 23): the scene bar's
  // account chip carries theme switching, Run setup…, sign out, and
  // About & licence (AGPL 5(d)/13 -- the licence must stay reachable
  // from the running app). Settings, Entities and Audit log left the
  // menu once each had somewhere better to live -- Settings and
  // Entities joined the deck as cards of their own, and Audit log has
  // lived on the docket's own tab since rounds 17-19 -- so the menu's
  // only remaining page-shaped action is the one thing that opens a
  // modal rather than going anywhere: Run setup….
  import { appState, type View } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import ThemeMenu from './ThemeMenu.svelte'
  import AboutOverlay from './AboutOverlay.svelte'

  let open = $state(false)
  let showAbout = $state(false)
  let logoutError = $state<string | null>(null)
  let menuEl: HTMLElement | undefined

  const isAdmin = $derived(authState.role === 'admin')

  type Row = { label: string; view?: View; action?: 'run-setup'; admin?: boolean }
  const operate: Row[] = [{ label: 'Run setup…', action: 'run-setup', admin: true }]

  function go(row: Row) {
    if (row.action === 'run-setup') wizardState.launch()
    else if (row.view) appState.view = row.view
    open = false
  }

  async function signOut() {
    logoutError = await authState.logout()
  }

  function onWindowClick(e: MouseEvent) {
    if (open && menuEl && !menuEl.contains(e.target as Node)) open = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.preventDefault()
      open = false
    }
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

<div class="account" bind:this={menuEl}>
  <button
    class="chip"
    class:open
    onclick={() => (open = !open)}
    aria-haspopup="menu"
    aria-expanded={open}
    title="Account and operate pages"
  >
    {authState.username}{#if isAdmin}&nbsp;(admin){/if}
  </button>

  {#if open}
    <div class="menu" role="menu">
      <div class="row theme-row">
        <ThemeMenu />
      </div>
      <div class="rule"></div>
      {#each operate.filter((r) => !r.admin || isAdmin) as row (row.label)}
        <button class="row" role="menuitem" class:on={row.view === appState.view} onclick={() => go(row)}>
          {row.label}
        </button>
      {/each}
      <div class="rule"></div>
      {#if authState.hasLocalPassword}
        <button class="row" role="menuitem" onclick={() => ((authState.showChangePassword = true), (open = false))}>
          Change password
        </button>
      {/if}
      {#if authState.ssoAvailable && authState.hasLocalPassword}
        <button class="row" role="menuitem" onclick={() => ((authState.showSSOLink = true), (open = false))}>
          Use single sign-on
        </button>
      {/if}
      <button class="row" role="menuitem" onclick={signOut}>Sign out</button>
      <button class="row" role="menuitem" onclick={() => ((showAbout = true), (open = false))}>
        About &amp; licence
      </button>
      {#if logoutError}<p class="err" role="alert">{logoutError}</p>{/if}
    </div>
  {/if}
</div>

<AboutOverlay bind:open={showAbout} />

<style>
  .account {
    position: relative;
  }

  /* Round 30's `.scstatus .who`, ported field-for-field: plain text at
     rest -- no border, no fill -- with the pill's border and a
     brighter ink appearing only on hover (or while the menu the chip
     opens is itself open). The build used to draw the border at all
     times, turning a text label into a permanent button. */
  .chip {
    background: transparent;
    border: 1px solid transparent;
    border-radius: 999px;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 4px 12px;
    cursor: pointer;
  }

  .chip:hover,
  .chip.open {
    color: var(--fg-muted);
    border-color: var(--border);
  }

  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 6px);
    min-width: 200px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 6px;
    display: flex;
    flex-direction: column;
    z-index: 40;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: var(--fg-muted);
    font-size: 13px;
    text-align: left;
    padding: 7px 10px;
    cursor: pointer;
  }

  button.row:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .row.on {
    color: var(--fg);
  }

  .theme-row {
    cursor: default;
    justify-content: space-between;
  }

  .rule {
    height: 1px;
    background: var(--border);
    margin: 5px 4px;
  }

  .err {
    color: var(--alarm);
    font-size: 12px;
    margin: 4px 10px;
  }
</style>
