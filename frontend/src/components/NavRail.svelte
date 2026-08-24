<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The ratified left rail (#544, under #486). Built from
  // docs/design/screens/navigation/DESIGN.md, which is the authoritative
  // record -- where it and a mockup disagree, the record wins.
  //
  // The open-flag count badge and the broken ring are both #546's; the
  // three persistent states, the icon set and the handle arrived with
  // #545. The ring waited on a separate ratified decision about what
  // "broken" means -- see the issue's "Ratified: what puts something in a
  // broken state" comment: Watchlist rings when an *enabled* expectation's
  // coverage is 'no-logging', and nothing else qualifies.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { railPref, describe, spokenLabel, type RailDensity } from '../lib/rail.svelte'
  import { visibleGroups, type NavItem } from '../lib/navGroups'
  import AboutOverlay from './AboutOverlay.svelte'
  import RailIcon from './RailIcon.svelte'

  // The geography itself -- the five groups, their pages, the
  // reserved-slot rule and the badge/ring markers -- lives in
  // lib/navGroups.ts, shared with #550's small-screen bottom bar and
  // half-sheet so the two surfaces cannot drift apart.
  type Item = NavItem

  // #490's grammar: admin-only rows are absent for viewers, never
  // disabled. A group whose every item is admin-only disappears with
  // them rather than rendering an empty heading.
  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  const visible = $derived(visibleGroups(isAdmin))

  // "Open unexcluded flags" is exactly flagsState.activeCount, with no
  // exclusion filter needed on top: internal/flags.Store keeps the two in
  // step deliberately. ClearAndExclude clears as it excludes, and Exclude
  // clears any pair that is already active -- its own comment calls an
  // excluded flag left sitting uncleared "a landmine for the next caller".
  // So an excluded pair can never be counted here as open.
  const flagCount = $derived(flagsState.activeCount)

  function showCount(item: Item): boolean {
    return item.badge === true && flagCount > 0
  }

  // #546's ring: fires only for the row marked `ring` and only while
  // watchlistState.brokenCount says something is actually broken. A live
  // reading of the store, not a record -- there is deliberately no
  // acknowledge/dismiss/snooze state to check here, so the ring clears
  // itself the instant the next poll's coverage answer does.
  function showRing(item: Item): boolean {
    return item.ring === true && watchlistState.brokenCount > 0
  }

  // The ring's whole meaning lives in this sentence -- it is a plain red
  // outline and nothing else. Plain operator language naming the count
  // and the cause, per the ratified wording: not "coverage is
  // no-logging", which is vocabulary the operator never chose.
  function ringReason(): string {
    const n = watchlistState.brokenCount
    return n === 1
      ? `1 watch can't be checked: the firewall rules it needs aren't being logged`
      : `${n} watches can't be checked: the firewall rules they need aren't being logged`
  }

  // What the row is called out loud. The count and the ring reason are
  // folded in because a badge or outline read on its own says nothing --
  // the record asks for "label+count in aria-labels", and the ratified
  // mockup words the count "Flags — 6 open". A count and a ring are
  // independent (a row could in principle carry both), so both are
  // gathered and composed by spokenLabel rather than one overwriting the
  // other.
  function spoken(item: Item): string {
    const bits: string[] = []
    if (showCount(item)) bits.push(`${flagCount} open`)
    if (showRing(item)) bits.push(ringReason())
    return spokenLabel(item.label, bits)
  }

  function activate(item: Item) {
    appState.view = item.view
  }

  function isCurrent(item: Item): boolean {
    return appState.view === item.view
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

  const iconsOnly = $derived(railPref.effective === 'icons')
  const nextDensity = $derived<RailDensity>(iconsOnly ? 'full' : 'icons')

  let railEl: HTMLElement | undefined = $state()
  const itemEls: Record<string, HTMLButtonElement | undefined> = $state({})

  // One shared tooltip, positioned fixed, rather than one absolutely
  // positioned inside each row. The rail scrolls (overflow-y: auto), and
  // a box with overflow on one axis clips the other regardless of what
  // overflow-x says -- so a tooltip living inside a row would be cut off
  // at 54px, which is exactly where it is needed.
  let tip = $state<{ text: string; top: number } | null>(null)

  function showTip(e: Event, text: string) {
    if (!iconsOnly) return
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    tip = { text, top: r.top + r.height / 2 }
  }

  const hideTip = () => (tip = null)

  // Restoring from docked owes the operator "same density, same scroll,
  // focus on the current page". Density is the preference's own job; the
  // other two are here because docking unmounts this component, so both
  // have to be re-applied when it comes back rather than merely left
  // alone. Guarded to the first run so an ordinary re-render never steals
  // focus -- and skipped entirely unless the handle was what brought the
  // rail back, so a plain page load leaves focus where it landed.
  let settled = false
  $effect(() => {
    if (!railEl || settled) return
    settled = true
    railEl.scrollTop = railPref.scrollTop
    if (railPref.restored) {
      const current = visible.flatMap((g) => g.items).find(isCurrent)
      if (current) itemEls[current.label]?.focus()
    }
  })
</script>

<!-- Window-level, not a div handler: the account row's wrapper div has
     no interactive role of its own, so a keydown listener on it would
     need one invented just to satisfy a11y linting. Guarded internally
     by accountOpen, same as AboutOverlay's own onkeydown. -->
<svelte:window onkeydown={onAccountKeydown} />

<nav
  class="rail"
  class:icons={iconsOnly}
  aria-label="Main"
  bind:this={railEl}
  onscroll={() => railEl && (railPref.scrollTop = railEl.scrollTop)}
>
  <!-- The rail-head dot (#549): "connection lost" turns the rail itself
       alarm, not just the banner -- see docs/design/screens/navigation/
       DESIGN.md's "States of the chrome". Purely visual (aria-hidden): the
       accessible text for a lost connection is ConnectionBanner's own
       role="status", which renders whether or not the rail is even
       mounted (see its own comment), so nothing here needs to repeat it.
       Docked, this element does not exist at all -- the handle is a
       one-job control and connection state is explicitly never its job
       (NavHandle.svelte), so the banner alone carries it there. -->
  <div class="rail-head" aria-hidden="true">
    <span class="rail-head-dot" class:alarm={appState.connState === 'closed'}></span>
  </div>
  <ul class="groups">
    {#each visible as group (group.name)}
      {@const headingId = `rail-group-${group.name.toLowerCase()}`}
      <li class="group">
        <!-- Headings are labels, never controls: no landing page, no
             accordion. The record is explicit about this. At icons
             density they stay in the accessibility tree and lose only
             their pixels -- a 54px rail has no room for the word, but
             dropping the heading would also drop the grouping. -->
        <h2 class="group-heading" id={headingId}>{group.name}</h2>
        <ul class="items" aria-labelledby={headingId}>
          {#each group.items as item (item.label)}
            <li>
              <button
                class="item"
                class:current={isCurrent(item)}
                class:broken={showRing(item)}
                aria-current={isCurrent(item) ? 'page' : undefined}
                aria-label={iconsOnly || showCount(item) || showRing(item) ? spoken(item) : undefined}
                title={iconsOnly ? undefined : item.title}
                bind:this={itemEls[item.label]}
                onclick={() => activate(item)}
                onmouseenter={(e) => showTip(e, spoken(item))}
                onmouseleave={hideTip}
                onfocus={(e) => showTip(e, spoken(item))}
                onblur={hideTip}
              >
                <RailIcon name={item.icon} />
                <span class="label">{item.label}</span>
                {#if showCount(item)}
                  <!-- aria-hidden because the button's own aria-label
                       already speaks the count in words ("Flags — 6
                       open"); left audible it would be announced twice,
                       the second time as a bare number. -->
                  <span class="count" aria-hidden="true">{flagCount}</span>
                {/if}
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
          aria-label={iconsOnly ? `Account: ${authState.username}` : undefined}
          title={iconsOnly ? undefined : 'Account'}
          onmouseenter={(e) => showTip(e, authState.username)}
          onmouseleave={hideTip}
          onfocus={(e) => showTip(e, authState.username)}
          onblur={hideTip}
        >
          <RailIcon name="account" />
          <span class="label">{authState.username}</span>
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
    <button
      class="footer-item"
      onclick={() => (showAbout = true)}
      aria-label={iconsOnly ? 'About & licence' : undefined}
      title={iconsOnly ? undefined : 'Version, copyright, licence and source code'}
      onmouseenter={(e) => showTip(e, 'About & licence')}
      onmouseleave={hideTip}
      onfocus={(e) => showTip(e, 'About & licence')}
      onblur={hideTip}
    >
      <RailIcon name="about" />
      <span class="label">About &amp; licence</span>
    </button>

    <!-- The only place a state is selected. The handle restores, but it
         never writes the preference, so these two buttons are the whole
         of the persistent choice. -->
    <div class="states">
      <button
        class="state-btn"
        onclick={() => railPref.setDensity(nextDensity)}
        aria-label="Show {describe(nextDensity)}"
        onmouseenter={(e) => showTip(e, `Show ${describe(nextDensity)}`)}
        onmouseleave={hideTip}
        onfocus={(e) => showTip(e, `Show ${describe(nextDensity)}`)}
        onblur={hideTip}
      >
        <RailIcon name={iconsOnly ? 'density-wide' : 'density-narrow'} />
      </button>
      <!-- The label teaches the way back at the moment of hiding: once
           docked there is no rail left to explain itself. -->
      <button
        class="state-btn"
        onclick={() => railPref.dock()}
        aria-label="Dock the navigation — reopen with the tab at the left edge"
        onmouseenter={(e) => showTip(e, 'Dock the navigation')}
        onmouseleave={hideTip}
        onfocus={(e) => showTip(e, 'Dock the navigation')}
        onblur={hideTip}
      >
        <RailIcon name="dock" />
      </button>
    </div>
  </div>
</nav>

<!-- Outside the rail, because the rail scrolls and would clip it. Purely
     visual: every row it describes already carries the same words in its
     aria-label, so announcing this too would just double up. -->
{#if tip}
  <div class="tip" style="top: {tip.top}px" aria-hidden="true">{tip.text}</div>
{/if}

<AboutOverlay bind:open={showAbout} />

<style>
  /* Two rendered densities: 216px icons+text, 54px icons. The third
     state (docked) is the absence of this element entirely -- App.svelte
     renders the handle instead -- rather than a 0px-wide rail, so nothing
     docked stays in the tab order. */
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

  .rail.icons {
    width: 54px;
    flex: 0 0 54px;
    padding: 10px 7px;
  }

  /* The rail-head dot: quiet by default (the same tone the rail's own
     muted rows use), alarm-red only once the connection is actually
     lost -- turning it on for 'connecting' too would make every ordinary
     reconnect flicker alarm-red, which is not what the record asks for. */
  .rail-head {
    display: flex;
    justify-content: center;
    padding: 0 0 8px;
  }

  .rail-head-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--fg-dim);
  }

  .rail-head-dot.alarm {
    background: var(--alarm);
    box-shadow: 0 0 6px var(--alarm);
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
    position: relative;
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

  .item:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  /* #546's broken ring: 2px alarm-red outline, 3px offset, per the
     record. Goes on .item itself -- already one element wrapping icon
     and label -- so .rail.icons .label { display: none } below tightens
     it to the icon alone at 54px with no extra rule needed. Declared
     before :focus-visible so tabbing to a broken row still shows the
     ordinary focus ring rather than fighting the alarm outline for the
     same CSS property. */
  .item.broken {
    outline: 2px solid var(--alarm);
    outline-offset: 3px;
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

  /* The rail's only alarm-filled count, per the record. Text is --bg
     rather than a fixed dark, so it stays legible against the bright red
     of the dark lane and the darker red of the light one. */
  .count {
    margin-left: auto;
    border-radius: 8px;
    padding: 1px 5px;
    background: var(--alarm);
    color: var(--bg);
    font-size: 0.68rem;
    font-weight: 700;
    /* So the row does not shift width as the count ticks between, say,
       9 and 10 while traffic arrives. */
    font-variant-numeric: tabular-nums;
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
    position: relative;
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

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The two state controls sit side by side rather than as two more full
     rows: they are chrome for the rail, not destinations in it. */
  .states {
    display: flex;
    gap: 2px;
    margin-top: 4px;
  }

  .state-btn {
    position: relative;
    display: flex;
    flex: 1;
    align-items: center;
    justify-content: center;
    padding: 6px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg-dim);
    cursor: pointer;
  }

  .state-btn:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .state-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  .rail.icons .states {
    flex-direction: column;
  }

  /* Hidden rather than removed: the grouping stays in the accessibility
     tree at 54px, where the word itself does not fit. */
  .rail.icons .group-heading {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .rail.icons .group + .group {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .rail.icons .item,
  .rail.icons .footer-item {
    justify-content: center;
    padding: 8px 6px;
  }

  .rail.icons .label {
    display: none;
  }

  /* At 54px there is no row left to push the count to, so it sits on the
     icon's top-right corner instead. The item is already
     position: relative for the same reason the tooltip is not. */
  .rail.icons .count {
    position: absolute;
    top: 1px;
    right: 6px;
    margin: 0;
  }

  /* The record asks for the label on hover *and* focus at icons density.
     A native `title` answers hover only, so this is a real element --
     fixed rather than absolute so the rail's own scrolling cannot clip
     it, and only ever rendered when showTip decides the density calls
     for one. */
  .tip {
    position: fixed;
    left: 60px;
    transform: translateY(-50%);
    z-index: 60;
    padding: 4px 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-elevated);
    color: var(--fg);
    font-size: 0.8rem;
    white-space: nowrap;
    pointer-events: none;
  }

  /* At 54px the rail is narrower than the menu's own contents, so it
     opens rightward from the edge instead of filling the rail's width. */
  .rail.icons .popover {
    right: auto;
    min-width: 200px;
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
