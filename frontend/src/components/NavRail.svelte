<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The ratified left rail (#544, under #486). Built from
  // docs/design/screens/navigation/DESIGN.md, which is the authoritative
  // record -- where it and a mockup disagree, the record wins.
  //
  // Only the full 216px density exists here. The icons/docked states and
  // the handle are #545's; badges and the broken ring are #546's.
  import { appState, type View } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import AboutOverlay from './AboutOverlay.svelte'

  type Item = {
    label: string
    // A view the app already renders, or an action for surfaces that are
    // not views yet (Users/Tokens are still overlays until #548).
    view?: View
    act?: () => void
    admin?: boolean
    title: string
  }

  type Group = { name: string; items: Item[] }

  // Fixed order, per the record. The reserved-slot rule lives here rather
  // than in the DOM: Map (v0.5.0) and Lookback (unbuilt) are deliberately
  // absent, not stubbed or disabled.
  //
  // Interim, per #544's body: the Live group carries Stream alone until
  // the fall ships, and Stream is the landing.
  const groups: Group[] = [
    {
      name: 'Live',
      items: [{ label: 'Stream', view: 'live', title: 'The live event stream' }],
    },
    {
      name: 'Investigate',
      items: [
        { label: 'Metrics', view: 'metrics', title: 'Event charts and traffic breakdowns' },
        { label: 'Audit log', view: 'audit', admin: true, title: 'Who changed what, and when' },
      ],
    },
    {
      name: 'Detect',
      items: [
        {
          label: 'Flags',
          view: 'flags',
          title: 'Behavioral flags: port scans, activity spikes, critical-port attempts, and volume spikes',
        },
        {
          label: 'Detectors',
          view: 'detectors',
          admin: true,
          title: 'Toggle behavioral detectors on/off and restrict their scope',
        },
      ],
    },
    {
      name: 'Expect',
      items: [
        { label: 'Watchlist', view: 'watchlist', admin: true, title: 'Hosts and ports you expect to see' },
      ],
    },
    {
      name: 'Admin',
      items: [
        {
          label: 'Users',
          act: () => (authState.showUsers = true),
          admin: true,
          title: 'Add or remove accounts',
        },
        {
          label: 'Tokens',
          act: () => (authState.showTokens = true),
          admin: true,
          title: 'Create/revoke read-only API bearer tokens for scripted access',
        },
        {
          label: 'Fleet',
          view: 'fleet',
          title: 'Every known RouterOS device: live/stale/never-seen status, last-seen, and event counts',
        },
        { label: 'Entities', view: 'entities', admin: true, title: 'Named hosts, ports and services' },
        {
          label: 'Run setup…',
          view: 'setup',
          admin: true,
          title: 'Re-run the setup wizard',
        },
      ],
    },
  ]

  // #490's grammar: admin-only rows are absent for viewers, never
  // disabled. A group whose every item is admin-only disappears with
  // them rather than rendering an empty heading.
  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  const visible = $derived(
    groups
      .map((g) => ({ ...g, items: g.items.filter((i) => !i.admin || isAdmin) }))
      .filter((g) => g.items.length > 0),
  )

  function activate(item: Item) {
    if (item.act) item.act()
    else if (item.view) appState.view = item.view
  }

  function isCurrent(item: Item): boolean {
    return item.view !== undefined && appState.view === item.view
  }

  // The footer's Account popover, per #544's design record ("homes for
  // the four non-navigation items", §1).
  let accountOpen = $state(false)
  let accountRootEl: HTMLDivElement | undefined = $state()
  let accountTriggerEl: HTMLButtonElement | undefined = $state()
  // Shown after signing out, since the popover itself is gone by then --
  // see the Sign out button below for why a failure still signs out.
  let logoutError = $state<string | null>(null)

  function onDocClick(e: MouseEvent) {
    if (accountRootEl && !accountRootEl.contains(e.target as Node)) accountOpen = false
  }

  $effect(() => {
    if (!accountOpen) return
    document.addEventListener('click', onDocClick)
    return () => document.removeEventListener('click', onDocClick)
  })

  // Esc returns focus to the row it came from: the popover hangs off the
  // footer, so without this, dismissing it drops focus to the document.
  function onAccountKeydown(e: KeyboardEvent) {
    if (!accountOpen || e.key !== 'Escape') return
    accountOpen = false
    accountTriggerEl?.focus()
  }

  // See AboutOverlay.svelte's own comment for why this exists at all
  // (AGPL 5(d)/13, not decoration).
  let showAbout = $state(false)
</script>

<!-- Window-level, not a div handler: the account row's wrapper div has
     no interactive role of its own, so a keydown listener on it would
     need one invented just to satisfy a11y linting. Guarded internally
     by accountOpen, same as AboutOverlay's own onkeydown. -->
<svelte:window onkeydown={onAccountKeydown} />

<nav class="rail" aria-label="Main">
  <ul class="groups">
    {#each visible as group (group.name)}
      {@const headingId = `rail-group-${group.name.toLowerCase()}`}
      <li class="group">
        <!-- Headings are labels, never controls: no landing page, no
             accordion. The record is explicit about this. -->
        <h2 class="group-heading" id={headingId}>{group.name}</h2>
        <ul class="items" aria-labelledby={headingId}>
          {#each group.items as item (item.label)}
            <li>
              <button
                class="item"
                class:current={isCurrent(item)}
                aria-current={isCurrent(item) ? 'page' : undefined}
                title={item.title}
                onclick={() => activate(item)}
              >
                {item.label}
              </button>
            </li>
          {/each}
        </ul>
      </li>
    {/each}
  </ul>

  <!-- Session actions + the licence notice, per #544's design record
       ("homes for the four non-navigation items"): not a sixth group,
       so the group-heading rule ("labels, never controls") stays
       untouched. #545 appends density/dock controls below this, in the
       same footer. -->
  <div class="footer">
    {#if logoutError}
      <p class="logout-error" role="alert">
        Signed out here, but the server did not confirm it: {logoutError}
      </p>
    {/if}

    {#if authState.state === 'authenticated'}
      <!-- Absent, never disabled, per #490's grammar -- same rule the
           admin-only items above already follow. -->
      <div class="account" bind:this={accountRootEl}>
        <button
          class="footer-item"
          bind:this={accountTriggerEl}
          onclick={() => (accountOpen = !accountOpen)}
          aria-haspopup="true"
          aria-expanded={accountOpen}
          title="Account"
        >
          <svg class="glyph" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <circle cx="8" cy="5.5" r="3" fill="currentColor" />
            <path d="M2 14.5c0-3.6 2.7-6.5 6-6.5s6 2.9 6 6.5" fill="currentColor" />
          </svg>
          {authState.username}
        </button>

        {#if accountOpen}
          <div class="popover" role="menu">
            <div class="popover-header">{authState.username}</div>

            {#if authState.hasLocalPassword}
              <!-- Hidden for an SSO-only account, which has no local
                   password to change. -->
              <button
                class="popover-item"
                onclick={() => {
                  authState.showChangePassword = true
                  accountOpen = false
                }}
                title="Change your MikroView password, and sign out everywhere else"
              >
                Change password
              </button>
            {/if}

            {#if authState.ssoAvailable && authState.hasLocalPassword}
              <!-- Hidden once there is no local password left to
                   convert. -->
              <button
                class="popover-item"
                onclick={() => {
                  authState.showSSOLink = true
                  accountOpen = false
                }}
                title="Sign in through your identity provider instead of a MikroView password"
              >
                Connect SSO
              </button>
            {/if}

            <button
              class="popover-item"
              onclick={() => {
                // Caught rather than fire-and-forget: authState.logout()
                // signs out locally either way, so the only thing worth
                // saying is that the server may still hold the session.
                void authState.logout().then((err) => {
                  if (err) logoutError = err
                })
                accountOpen = false
              }}
              title="Sign out {authState.username}"
            >
              Sign out ({authState.username})
            </button>
          </div>
        {/if}
      </div>
    {/if}

    <!--
      Licence obligation, not decoration. AGPL section 5(d) requires an
      interactive
      interface to display the Appropriate Legal Notices, and section 13
      requires the source offer to network users. Both live in
      AboutOverlay. Present regardless of auth state or role.
    -->
    <button class="footer-item" onclick={() => (showAbout = true)} title="Version, copyright, licence and source code">
      About &amp; licence
    </button>
  </div>
</nav>

<AboutOverlay bind:open={showAbout} />

<style>
  /* Full density only (216px, icons+text). #545 adds the 54px icons and
     0px docked states. */
  .rail {
    width: 216px;
    flex: 0 0 216px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 8px;
    border-right: 1px solid var(--border);
    background: var(--bg-elevated);
    overflow-y: auto;
  }

  .groups,
  .items {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .group + .group {
    margin-top: 14px;
  }

  .group-heading {
    margin: 0 0 4px;
    padding: 0 8px;
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .item {
    display: block;
    width: 100%;
    padding: 6px 8px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: 0.9rem;
    text-align: left;
    cursor: pointer;
  }

  .item:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  /* Never hover-only, per the record: focus is always visible. */
  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  .item.current {
    background: var(--accent-bg);
    color: var(--fg);
    font-weight: 600;
  }

  /* Pinned to the bottom of the rail's own flex column via margin-top:
     auto, rather than a second scroll region -- #545 appends density/
     dock controls as further rows here, below About & licence. */
  .footer {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin-top: auto;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .account {
    position: relative;
  }

  .footer-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 8px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: 0.9rem;
    text-align: left;
    cursor: pointer;
  }

  .footer-item:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .footer-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  .glyph {
    width: 14px;
    height: 14px;
    flex: none;
  }

  /* Opens upward: the row it hangs off sits at the bottom of the rail,
     so a menu opening downward would run off the viewport. */
  .popover {
    position: absolute;
    bottom: calc(100% + 4px);
    left: 0;
    right: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.4);
    z-index: 20;
  }

  .popover-header {
    padding: 6px 9px 4px;
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .popover-item {
    display: block;
    width: 100%;
    padding: 6px 9px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: 0.9rem;
    text-align: left;
    cursor: pointer;
  }

  .popover-item:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .popover-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  /* No new colour literal: the tokens named in #544's footer spec hold
     no alarm colour, so the alert leans on a bold weight and a marker
     glyph rather than red. */
  .logout-error {
    margin: 0 0 2px;
    padding: 6px 8px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-elevated);
    color: var(--fg);
    font-size: 0.78rem;
    font-weight: 600;
  }

  .logout-error::before {
    content: '⚠ ';
  }

</style>
