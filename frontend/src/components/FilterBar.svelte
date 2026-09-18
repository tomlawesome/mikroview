<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Round 30 shipped a "bar ▸"/"◂ bar" toggle welded to the filter box's
  // left edge (#697). Owner correction, 2026-08-31: "Remove the bar
  // button entirely, and the filter bar instead folds out of the search
  // box as a drawer" -- "clicking in the box opens it, clicking away
  // from the box closes it." That toggle is gone; there is no button.
  // The box (`.fbox`) is itself the drawer's disclosure: a click inside
  // it opens the strip below, a click away from both the box and the
  // open strip closes it again (Escape does too, for a keyboard user
  // with no "click away" to press -- see onWindowClick/onWindowKeydown
  // below). "One filter, two hands" stands as before: the box carries
  // the full typed grammar and is ALWAYS on screen -- clear every term
  // and it says so instead of vanishing -- while the strip is the same
  // filter as named fields. Editing either writes the same
  // appState.filters; clicking a value in a row (EventRow's own
  // gesture) writes both. The span pills (15 m/1 h/24 h/14 d, #703) and
  // the "holding N" reach words ride the right end of this same filter
  // line -- moved here from SceneBar's top chrome, not duplicated (see
  // SceneBar.svelte's own comment).
  import { appState } from '../lib/state.svelte'
  import { ACTION_FILTER_OPTIONS } from '../lib/actions'
  import { viewportState } from '../lib/viewport.svelte'
  import { retentionState, MAX_AGE_OPTIONS } from '../lib/retention.svelte'
  import { buildFilterChips, type FilterChip } from '../lib/filterChips'
  import { SPANS, describeReach, reachSeconds, spanAvailable, unavailableReason } from '../lib/spans'
  import { geoipState } from '../lib/geoip.svelte'
  import { seenValuesState } from '../lib/seenValues.svelte'
  import {
    acceptsTypedValue,
    backspaceAction,
    fieldItems,
    lastTokenKey,
    tokenPatch,
    typedValueHint,
    valueItems,
    type TokenField,
    type TokenMenuItem,
    type TokenSources,
  } from '../lib/tokenBar'
  import type { Filters } from '../lib/types'
  import FilterPresetsMenu from './FilterPresetsMenu.svelte'
  import ColumnToggles from './ColumnToggles.svelte'
  import { onMount } from 'svelte'

  // #1198: country flags go blank with no GeoIP database configured, and
  // a blank was previously indistinguishable from "no public traffic
  // yet". Loaded once here so both country selects below can show the
  // explainer row the moment it's known to be off.
  onMount(() => {
    geoipState.ensureLoaded().catch(() => {})
    // #1226: the values this instance has actually seen, so Proto and
    // Interface below are pickers rather than boxes an operator guesses
    // into. Best-effort -- a failed fetch leaves both suggesting nothing
    // and still accepting anything typed, which is what they did before.
    seenValuesState.ensureLoaded().catch(() => {})
  })

  // Saved filters have a drawn home now (round 37: "saved filters are
  // the box's business"), so FilterPresetsMenu is mounted inside the box
  // below rather than left as the gap #683 recorded. Export went the
  // other way and is not here: round 37 draws `csv ↓` as a verb on the
  // lines held on screen, beside `wipe` on the whisper's own line, so
  // lib/export.ts is mounted from Whisper.svelte.

  // Phone-width only, mirroring what the retired hamburger did (#544):
  // Toolbar.svelte:64 already carries this control at desktop width, so
  // duplicating it here unconditionally would put two of them on screen.
  function onMaxAgeChange(e: Event) {
    const raw = (e.currentTarget as HTMLSelectElement).value
    retentionState.set(raw === 'null' ? null : Number(raw))
  }

  const actions = ACTION_FILTER_OPTIONS

  // Below the breakpoint, the ~9 fields below move into a slide-up
  // drawer behind a trigger (issue #85) rather than staying always-
  // visible -- a horizontally-wrapping strip of selects/inputs doesn't
  // fit a phone-width screen usefully even stacked one-per-line, and a
  // human scanning the live view rarely needs every filter visible at
  // once the way FilterPresetsMenu's saved presets do.
  let drawerOpen = $state(false)

  // Desktop's own fold state (#697, owner 2026-08-31: "the bar goes,
  // the filter folds out of the search box"). Opens on a click inside
  // `.fbox` (or Enter/Space while it holds keyboard focus, since a
  // keyboard user has no "click inside" to press); closes on a click
  // away from both the box and the open strip, or Escape. Not tied to
  // whether a filter is active -- it opens and closes from that gesture
  // alone. A separate flag from drawerOpen: the two breakpoints render
  // different chrome (a bottom-sheet drawer vs. an inline thin row) and
  // must be independently togglable.
  let expanded = $state(false)

  // DOM refs for the outside-click close below: a click only counts as
  // "away from the box" once it lands outside both the trigger
  // (`.fbox`) and the strip it opens (`.bar.thin`), so picking a value
  // inside the open drawer never closes the thing being used.
  let fboxEl: HTMLDivElement | undefined = $state()
  let barEl: HTMLDivElement | undefined = $state()
  // The hint inside the box is the real input (see its comment in the
  // markup), so it -- not the box div, which has no tabstop -- is where
  // keyboard focus goes back to on close.
  let hintEl: HTMLInputElement | undefined = $state()

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    // #1246: one Escape closes whatever the box has opened -- the token
    // menu and the strip together, rather than making an operator press
    // it twice to get back to a plain box.
    if (menuOpen) closeMenu({ suppress: true })
    if (drawerOpen) drawerOpen = false
    if (expanded) {
      expanded = false
      // Keyboard close returns focus to the control that opened it,
      // same as any other disclosure widget in this app.
      hintEl?.focus()
    }
  }

  // The box is a click-to-open target for the pointer; the keyboard's
  // way in is the real input inside it, which takes text natively. See
  // the markup comment on .fbox for why the role does not sit on the
  // box itself.
  // "Clicking away from the box closes it" (owner, 2026-08-31) -- away
  // from the open strip too, not just the box itself. Bound via
  // <svelte:window> below, which Svelte itself adds on mount and
  // removes on destroy, so there is nothing here to tear down by hand.
  //
  // composedPath(), not e.target -- a click on a control that removes
  // *itself* from the DOM as part of its own handler (the strip's own
  // "× clear", once resetFilters() makes hasActiveFilters false) leaves
  // e.target detached by the time this runs on the bubble, so
  // `barEl.contains(e.target)` reads false for a click that never left
  // the strip and folds it as a side effect of clearing. composedPath()
  // is captured at dispatch, before any handler had a chance to mutate
  // the tree, so it still names the strip regardless of what that click
  // going on to unmount.
  function onWindowClick(e: MouseEvent) {
    const path = e.composedPath()
    if (viewportState.isMobile || (!expanded && !menuOpen)) return
    const inBox = fboxEl && path.includes(fboxEl)
    const inBar = barEl && path.includes(barEl)
    // #1246: the token menu lives inside the box and hangs over the strip
    // below it, so anything outside the box closes it -- reaching for the
    // strip's named fields is not using the menu, and leaving it open over
    // them would cover the field being reached for.
    if (!inBox && menuOpen) closeMenu()
    if (inBox || inBar) return
    expanded = false
  }

  // The box's own chip summary (ported from SceneBar's retired `.search`,
  // #697/#700): the same appState.filters, read as chips, always on
  // screen rather than only while a filter existed -- see the comment on
  // FILTERS_TRIGGER_ENABLED below for what that replaced.
  //
  // Minus the rule chip since #1246: the rule search is the free text in
  // this same box, so round 57 draws it once, as the text, never also as
  // a token beside it -- `rule:iot ✕ | iot` said the same thing twice and
  // offered a remover for something Backspace already edits. The drawer
  // and the strip still show the Rule field itself.
  const filterChips = $derived(
    buildFilterChips(appState.filters, appState.devices).filter((c) => c.key !== 'rule'),
  )

  // Removes one chip's own term(s), leaving the rest of the filter
  // untouched -- the mockup's own per-chip ⌫ ("drop this term"), not one
  // combined clear-everything glyph. A compound chip (source/destination)
  // clears every field it summarises together, so the chip and the field
  // group it mirrors always agree on being empty or not.
  function clearChip(chip: FilterChip) {
    switch (chip.key) {
      case 'device':
        appState.setFilter('device', '')
        break
      case 'action':
        appState.setFilter('action', '')
        break
      case 'chain':
        appState.setFilter('chain', '')
        break
      case 'proto':
        appState.setFilter('protocol', '')
        break
      case 'source':
        appState.setFilter('srcQuery', '')
        appState.setFilter('srcScope', '')
        appState.setFilter('srcCountry', '')
        break
      case 'destination':
        appState.setFilter('dstQuery', '')
        appState.setFilter('dstScope', '')
        appState.setFilter('dstCountry', '')
        break
      case 'port':
        appState.setFilter('port', '')
        break
      case 'interface':
        appState.setFilter('interface', '')
        break
      case 'rule':
        appState.setFilter('rule', '')
        break
    }
  }

  // ---------------------------------------------------------------
  // #1246 (round 57): the box's third face, the token bar.
  //
  // Face one is the named-field strip below, face two the chip summary
  // above; this is the third -- a field menu on focus, then that field's
  // own values, committing to the very same appState.filters both other
  // faces read. Nothing below holds a filter of its own: pendingField
  // and pendingValue are the half-made token between picking a field and
  // choosing its value, and they are gone the moment it commits. Which
  // list belongs to which field, and what a pick means, live in
  // lib/tokenBar.ts.
  let menuOpen = $state(false)
  let pendingField = $state<TokenField | null>(null)
  let pendingValue = $state('')
  // Which item the keyboard is on; -1 is "the caret is still in the box"
  // -- ArrowDown enters the list on the first item (round 57's scene 03).
  let menuIndex = $state(-1)
  // Escape closes the menu with the caret still in the box, so the focus
  // handler must not re-open it under the operator's hands. Cleared by
  // the next click in the box, or by focus arriving afresh.
  let menuSuppressed = false

  const tokenSources = $derived<TokenSources>({
    devices: appState.devices,
    chains: appState.chainOptions,
    // Verdict 1 on round 57 ("it should grow a real list of what it has
    // seen over time, and be persisted"): these two are #1226's
    // persisted register, never a scrape of what happens to be on screen.
    protos: seenValuesState.proto,
    interfaces: seenValuesState.interfaces,
    srcCountries: appState.srcCountryOptions,
    dstCountries: appState.dstCountryOptions,
  })

  const menuItems = $derived(pendingField ? valueItems(pendingField, tokenSources) : fieldItems(tokenSources))
  // The line under a value menu for the fields that also take typing
  // (port, and either side's address); '' for the ones that only take a
  // pick, which renders nothing.
  const menuTypedHint = $derived(pendingField ? typedValueHint(pendingField) : '')

  // The one input serves both jobs: the free-text rule search, and the
  // value of whichever field is pending. Which one it is showing is
  // pendingField, and nothing else -- so plain typing with no pending
  // field always lands in free text, and is never swallowed by the menu.
  const boxValue = $derived(pendingField ? pendingValue : appState.filters.rule)

  function openMenu() {
    menuSuppressed = false
    menuOpen = true
    menuIndex = -1
  }

  function closeMenu({ suppress = false } = {}) {
    menuOpen = false
    pendingField = null
    pendingValue = ''
    menuIndex = -1
    menuSuppressed = suppress
  }

  function onBoxFocus() {
    if (menuOpen || menuSuppressed) return
    openMenu()
  }

  // Focus leaving the box closes the menu, the same as a click away
  // does (onWindowClick above): round 57 opens it on focus, so it goes
  // with the focus. Without this, tabbing out -- or anything else that
  // blurs the input with no click -- left the menu hanging over the
  // first columns of the table underneath, covering rows nobody was
  // filtering (found by live-token-copy, whose row hover it blocked).
  // relatedTarget is where focus went: the menu's own items are
  // buttons, so a pointer pick lands inside the box and is left alone.
  function onBoxFocusOut(e: FocusEvent) {
    if (!menuOpen) return
    const to = e.relatedTarget
    if (to instanceof Node && fboxEl?.contains(to)) return
    closeMenu()
  }

  function pickField(field: TokenField) {
    pendingField = field
    pendingValue = ''
    menuIndex = -1
    menuOpen = true
    hintEl?.focus()
  }

  // Writes the token through setFilter, one field at a time, exactly as
  // the strip and EventRow's click-to-filter do -- the chip the box then
  // draws is buildFilterChips' own, not a second rendering of the same
  // term. Closes suppressed: the click that committed took focus off the
  // input, and putting it back must not re-open the field menu.
  function commitToken(field: TokenField, value: string) {
    for (const [key, v] of Object.entries(tokenPatch(field, value)) as [keyof Filters, never][]) {
      appState.setFilter(key, v)
    }
    closeMenu({ suppress: true })
    hintEl?.focus()
  }

  function chooseMenuItem(item: TokenMenuItem) {
    if (pendingField) commitToken(pendingField, item.value)
    else pickField(item.value as TokenField)
  }

  // Enter on a field that takes typing. A side's typed text is its
  // address sub-field, so it joins the same composite token any scope or
  // country already picked is in.
  function commitTypedValue() {
    const text = pendingValue.trim()
    if (!pendingField || !text) return
    commitToken(pendingField, pendingField === 'port' ? text : `query:${text}`)
  }

  function removeLastToken() {
    const key = lastTokenKey(filterChips)
    const chip = filterChips.find((c) => c.key === key)
    if (chip) clearChip(chip)
  }

  function onBoxKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (!menuOpen) openMenu()
      menuIndex = Math.min(menuIndex + 1, menuItems.length - 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      // Back past the first item is back to the box itself, not a wrap
      // to the end -- the caret has somewhere to be here.
      menuIndex = Math.max(menuIndex - 1, -1)
      return
    }
    if (e.key === 'Backspace') {
      // Verdict 3 on round 57 ("character you just typed"): one key, one
      // meaning, in whichever box holds the caret.
      const action = backspaceAction(pendingField, boxValue)
      if (action === 'character') return
      e.preventDefault()
      if (action === 'cancel-pending') {
        pendingField = null
        menuIndex = -1
        menuOpen = true
        return
      }
      removeLastToken()
      return
    }
    if (e.key !== 'Enter') return
    if (menuOpen && menuIndex >= 0 && menuIndex < menuItems.length) {
      e.preventDefault()
      chooseMenuItem(menuItems[menuIndex])
      return
    }
    if (pendingField && acceptsTypedValue(pendingField) && pendingValue.trim() !== '') {
      e.preventDefault()
      commitTypedValue()
      return
    }
    // The button this replaces opened natively on Enter or Space; Space
    // must stay a literal space while typing a term, but Enter carries no
    // other meaning here (there is no form to submit), so it keeps the
    // keyboard's way into the strip.
    expanded = true
  }

  // The stream's SPAN control (#703). It sets the same display window
  // the mobile drawer's duration selector sets, so the two can never
  // disagree about what the table is showing; a span reads as active
  // only when the window matches it exactly, so a duration chosen in the
  // drawer leaves every pill quiet rather than lighting the nearest one.
  //
  // Availability comes from the buffer's own reach, never from the
  // configured retention: offering a fortnight over nine hours of buffer
  // would answer with nine hours and call thirteen days quiet.
  const reach = $derived(reachSeconds(appState.stats?.oldestHeld, appState.now))
  const reachWords = $derived(describeReach(reach))

  // The old standalone "Filters ▸" trigger this replaced (#697): round
  // 29's box only ever displayed a filter and offered no way to make
  // one, so the build kept this control mounted beside it as the only
  // way in. Round 30's box is always on screen (see `.filterline`
  // below), so there is always a way in and this second control has
  // nothing left to do -- unmounted, not deleted, per #700/#691.
  const FILTERS_TRIGGER_ENABLED: boolean = false
</script>

<svelte:window onkeydown={onKeydown} onclick={onWindowClick} />

{#if viewportState.isMobile}
  <div class="mobile-row">
    <button
      class="trigger"
      onclick={() => (drawerOpen = true)}
      aria-haspopup="true"
      aria-expanded={drawerOpen}
    >
      Filters
      {#if appState.hasActiveFilters}<span class="dot" aria-label="Filters active"></span>{/if}
    </button>
    {#if appState.hasActiveFilters}
      <button class="clear" onclick={() => appState.resetFilters()}>Clear filters</button>
    {/if}
  </div>
{:else}
  <!-- Round 30's filter line (#697, #s5 `.filterline`), rebuilt per the
       owner's 2026-08-31 correction: no separate button reaches the
       strip below any more -- the box itself (always on screen, never
       conditional on a filter existing) is the disclosure, and the span
       pills still ride the line's right end. FILTERS_TRIGGER_ENABLED's
       old second control stays retired, not duplicated beside this. -->
  <div class="filterline">
    <!-- A click anywhere inside opens the strip (owner: "Clicking in the
         box opens it. Clicking away from the box closes it"), and
         onWindowClick above closes it on a click away from both this box
         and the open strip.

         The box itself carries no ARIA role. It holds real nested
         buttons -- each chip's own ⌫ -- and a screen reader flattens the
         content of anything with role="button", which would have taken
         the chip removers out of reach. Instead the always-present
         .fbtype input below is the genuine control and carries the
         expanded state, so the keyboard path and the announcement live
         there while the whole box stays clickable by pointer -- a click
         anywhere in the box (including empty space or a chip's own
         text) focuses that input, same as clicking the input directly.
         Hence the two ignores: the keyboard route is that input, not
         this div. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
      class="fbox"
      class:empty={filterChips.length === 0}
      class:open={expanded}
      bind:this={fboxEl}
      onfocusout={onBoxFocusOut}
      onclick={() => {
        expanded = true
        // #1246: a click in the box is also the pointer's way into the
        // field menu -- focus alone opens it, but a click arriving at an
        // already-focused box fires no focus event of its own.
        menuSuppressed = false
        if (!menuOpen) openMenu()
        hintEl?.focus()
      }}
    >
      {#if filterChips.length > 0 || pendingField}
        <span class="fchips">
          {#each filterChips as chip (chip.key)}
            <span class="chip"
              >{chip.label}:<em>{chip.value}</em><button
                type="button"
                class="chip-x"
                onclick={(e) => {
                  // Removing a chip is not "click inside the box to open
                  // it" -- stop the click reaching the box's own
                  // onclick above so it doesn't also (re)open the strip.
                  e.stopPropagation()
                  clearChip(chip)
                }}
                title="drop this term"
                aria-label="Remove the {chip.label} filter"
              >
                ⌫
              </button></span
            >
          {/each}
          <!-- #1246: the half-made token -- a field picked, its value not
               chosen yet. Quiet and caret-tipped rather than a fourth
               committed chip, so the box never claims a filter that is
               not yet active (round 57, scene 03). -->
          {#if pendingField}
            <span class="chip pending">{pendingField}:<span class="tm-caret" aria-hidden="true"></span></span>
          {/if}
        </span>
      {/if}
      <!-- #734: the always-visible free-text term. It reads/writes the
           same appState.filters.rule the strip's own Rule field below
           does (label/raw/name substring search, lib/state.svelte.ts's
           applyFilters) -- "editing either writes the same
           appState.filters" per the top-of-file comment, not a second
           field. The old button's wording moves here unchanged, as the
           placeholder. -->
      <!-- aria-expanded/aria-haspopup aren't part of the textbox role's
           supported set (the linter is right to flag them), but there is
           no combobox listbox here to justify that role instead -- this
           is a plain text field that happens to also disclose the strip
           below, and dropping the state announcement would leave a
           screen reader user unable to tell the strip opened at all. -->
      <!-- svelte-ignore a11y_role_supports_aria_props_implicit -->
      <input
        type="text"
        class="fbtype"
        placeholder={filterChips.length > 0
          ? 'type a term, or click a value in a row'
          : 'no filter — every line, as it arrived. type a term, or click a value in a row'}
        aria-label={filterChips.length > 0
          ? 'type a term, or click a value in a row'
          : 'no filter — every line, as it arrived. type a term, or click a value in a row'}
        aria-haspopup="true"
        aria-expanded={expanded}
        aria-controls="filterbar-strip"
        bind:this={hintEl}
        value={boxValue}
        oninput={(e) => {
          // #1246: no longer a plain bind, because this one input is both
          // the free-text rule search and the value box of a pending
          // token. Which it is writing to is pendingField and nothing
          // else -- see boxValue above.
          const next = e.currentTarget.value
          if (pendingField) pendingValue = next
          else appState.filters.rule = next
        }}
        onfocus={onBoxFocus}
        onkeydown={onBoxKeydown}
      />
      <!-- Round 37's `saved ▾`, at the box's own right end (its
           `margin-left: auto` puts it there). Inside the box, not
           beside it: a saved filter is a filter. Its own clicks are
           contained so reaching for one does not also unfold the strip
           this box discloses -- see the component. -->
      <FilterPresetsMenu />

      <!-- #1246 (round 57): the field menu, and then the picked field's
           own value menu, in FilterPresetsMenu's floating dress (elevated
           panel, hairline border, radius, the same shadow) rather than a
           second kind of popup -- anchored to the box's left edge instead
           of the trigger's right. Each item stops its own click: picking
           a value is not "click inside the box", which would re-open the
           menu over the token just committed. -->
      {#if menuOpen}
        <div
          class="token-menu"
          class:tm-values={pendingField !== null}
          role="listbox"
          aria-label={pendingField ? `Pick a value for ${pendingField}` : 'Choose a field to filter on'}
        >
          {#if pendingField}
            <div class="tm-crumb">‹ {pendingField}</div>
          {/if}
          {#each menuItems as item, i (item.value)}
            <button
              type="button"
              class="tm-item"
              class:focused={i === menuIndex}
              role="option"
              aria-selected={i === menuIndex}
              onclick={(e) => {
                e.stopPropagation()
                chooseMenuItem(item)
              }}
            >
              <span class="tm-name">{item.label}</span>
              {#if item.hint}<span class="tm-hint">{item.hint}</span>{/if}
            </button>
          {/each}
          {#if menuTypedHint}
            <p class="tm-typed">{menuTypedHint}</p>
          {/if}
        </div>
      {/if}
    </div>
    <span class="spans" role="group" aria-label="How far back the stream shows — {reachWords}">
      {#each SPANS as span (span.key)}
        {@const available = spanAvailable(span, reach)}
        <button
          type="button"
          class="span"
          class:on={retentionState.maxAgeSeconds === span.seconds}
          disabled={!available}
          aria-pressed={retentionState.maxAgeSeconds === span.seconds}
          title={available ? `Show the last ${span.label}` : unavailableReason(span, reach)}
          onclick={() => retentionState.set(span.seconds)}>{span.label}</button
        >
      {/each}
      <!-- What the buffer really holds, beside the control it qualifies.
           Not a description of the interface (round 30 struck those) but
           the same fact the unavailable spans turn on, said once in
           words instead of only on hover. -->
      <span class="reach">{reachWords}</span>
    </span>
  </div>

  {#if FILTERS_TRIGGER_ENABLED && !expanded}
    <!-- The pre-round-30 folded box (#644): a quiet trigger standing in
         for the whole bar. Retired by #697 -- the box above is always on
         screen now, so this second way in is unmounted (see
         FILTERS_TRIGGER_ENABLED's own comment), never deleted. -->
    <button
      class="fold-trigger"
      onclick={() => (expanded = true)}
      aria-haspopup="true"
      aria-expanded={expanded}
    >
      Filters ▸
      {#if appState.hasActiveFilters}<span class="dot" aria-label="Filters active"></span>{/if}
    </button>
  {/if}
{/if}

{#if (viewportState.isMobile && drawerOpen) || (!viewportState.isMobile && expanded)}
  {#if viewportState.isMobile}
    <div class="scrim" onclick={() => (drawerOpen = false)} role="presentation"></div>
  {/if}
  <div
    class="bar"
    id="filterbar-strip"
    class:drawer={viewportState.isMobile}
    class:thin={!viewportState.isMobile}
    bind:this={barEl}
  >
    {#if viewportState.isMobile}
      <div class="handle"></div>
      <div class="drawer-header">
        <span class="drawer-title">Filters</span>
        <button class="done" onclick={() => (drawerOpen = false)}>Done</button>
      </div>
    {/if}

    <!-- fb-label spans below are visible on the desktop thin bar only
         (.thin .fb-label; hidden in the mobile drawer, which already
         names each field via its placeholder/aria-label -- see round 8's
         "dim micro-labels over hairline-underlined values, no boxes").
         The desktop thin row additionally drops the "Any X"/placeholder
         prose the mobile drawer still shows (#683, round 29: "no
         placeholder prose inside the fields") -- the fb-label above
         already names the field there, so the empty state is just the
         hairline with nothing on it. #1191 is the one ratified
         exception: the two address queries show their "name, IP or
         CIDR" hint on desktop as well as on phones, because that field
         was read as a bare underline with nothing to say what it
         takes. -->
    <div class="fb-field">
      <span class="fb-label">Device</span>
      <select bind:value={appState.filters.device} aria-label="Device">
        <option value="">{viewportState.isMobile ? 'Any device' : '—'}</option>
        {#each appState.devices as d (d.id)}
          <option value={d.id}>{d.name}</option>
        {/each}
      </select>
    </div>

    <div class="fb-field">
      <span class="fb-label">Action</span>
      <select bind:value={appState.filters.action} aria-label="Action">
        {#each actions as a (a.value)}
          <option value={a.value}>{viewportState.isMobile || a.value !== '' ? a.label : '—'}</option>
        {/each}
      </select>
    </div>

    <!-- #438: existed as a filter field (EventRow's chain cell already
         called setFilter('chain', …)) with no control here to show, edit
         or clear it -- the issue's own worked example of the bidirectional
         contract being one-way. A select, not free text: the built-in
         chains plus anything else observed appear in appState.chainOptions,
         so a custom chain can't be typo'd and shows up the moment it's seen. -->
    <div class="fb-field">
      <span class="fb-label">Chain</span>
      <select bind:value={appState.filters.chain} aria-label="Chain">
        <option value="">{viewportState.isMobile ? 'Any chain' : '—'}</option>
        {#each appState.chainOptions as c (c)}
          <option value={c}>{c}</option>
        {/each}
      </select>
    </div>

    <!-- #1226: a datalist combo, not a <select>. The list is what this
         instance has actually seen (seenValues.svelte.ts); typing a
         value that is not in it still works, which is what lets a filter
         be set up before the traffic it is waiting for arrives. Same
         idiom as the watchers station's scope boxes. -->
    <div class="fb-field">
      <span class="fb-label">Proto</span>
      <input
        type="text"
        list="fb-seen-protos"
        placeholder={viewportState.isMobile ? 'Protocol (tcp, udp, icmp…)' : ''}
        bind:value={appState.filters.protocol}
        aria-label="Protocol"
      />
    </div>

    <!-- #438: the single "IP or CIDR" box (matched src OR dst, raw address
         only) is replaced by side-scoped Source/Destination groups, each
         pairing its query box with the existing scope select and a new
         country select -- matching, label or IP or CIDR, lives in
         lib/addressMatch.ts. The swap button between them answers "clicked
         the wrong side" in two clicks instead of retyping. Ratified as
         "source ⇄ destination (scope + country)" -- the free-text query
         stays too (#683: "do not delete working features"), just folded
         into the same compact group rather than dropped. -->
    <!-- #1191: each of the three controls carries its own caption, in the
         same .fb-label small-caps the strip already uses for SOURCE and
         DESTINATION above them. They were three unlabelled controls in a
         row -- the aria-labels said scope/name-IP-or-CIDR/country all
         along, so this only makes visible what a screen reader was
         already told. -->
    <div class="fb-field">
      <span class="fb-label">Source</span>
      <div class="addr-group">
        <div class="fb-field">
          <span class="fb-label">Scope</span>
          <select bind:value={appState.filters.srcScope} aria-label="Source scope" title="Restrict by whether the source is on your LAN">
            <option value="">{viewportState.isMobile ? 'Any source' : '—'}</option>
            <option value="internal">Internal</option>
            <option value="external">External</option>
          </select>
        </div>
        <div class="fb-field">
          <span class="fb-label">Name, IP or CIDR</span>
          <input
            type="text"
            placeholder={viewportState.isMobile ? 'Source — name, IP or CIDR' : 'name, IP or CIDR'}
            bind:value={appState.filters.srcQuery}
            aria-label="Source — name, IP or CIDR"
          />
        </div>
        <div class="fb-field">
          <span class="fb-label">Country</span>
          <select bind:value={appState.filters.srcCountry} aria-label="Source country">
            <option value="">{viewportState.isMobile ? 'Any country' : '—'}</option>
            <!-- #1198: with no GeoIP database, every event's country is
                 unresolved and this list would otherwise just stay
                 empty -- indistinguishable from "no public traffic
                 yet". disabled: it exists to explain, not to be picked. -->
            {#if geoipState.enabled === false}
              <option value="__no_geoip__" disabled>no GeoIP database — see docs ▸</option>
            {/if}
            {#each appState.srcCountryOptions as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </div>
      </div>
    </div>

    <button
      class="swap"
      onclick={() => appState.swapSourceDestination()}
      aria-label="Swap source and destination filters"
      title="Swap source and destination filters"
    >
      ⇄
    </button>

    <div class="fb-field">
      <span class="fb-label">Destination</span>
      <div class="addr-group">
        <div class="fb-field">
          <span class="fb-label">Scope</span>
          <select bind:value={appState.filters.dstScope} aria-label="Destination scope" title="Restrict by whether the destination is on your LAN">
            <option value="">{viewportState.isMobile ? 'Any destination' : '—'}</option>
            <option value="internal">Internal</option>
            <option value="external">External</option>
          </select>
        </div>
        <div class="fb-field">
          <span class="fb-label">Name, IP or CIDR</span>
          <input
            type="text"
            placeholder={viewportState.isMobile ? 'Destination — name, IP or CIDR' : 'name, IP or CIDR'}
            bind:value={appState.filters.dstQuery}
            aria-label="Destination — name, IP or CIDR"
          />
        </div>
        <div class="fb-field">
          <span class="fb-label">Country</span>
          <select bind:value={appState.filters.dstCountry} aria-label="Destination country">
            <option value="">{viewportState.isMobile ? 'Any country' : '—'}</option>
            <!-- #1198: see the matching comment on the source select above. -->
            {#if geoipState.enabled === false}
              <option value="__no_geoip__" disabled>no GeoIP database — see docs ▸</option>
            {/if}
            {#each appState.dstCountryOptions as opt (opt.value)}
              <option value={opt.value}>{opt.label}</option>
            {/each}
          </select>
        </div>
      </div>
    </div>

    <!-- #438: text now, not numeric-only -- a bare integer is still an
         exact port match on either side, but anything else searches the
         displayed label (an operator name, or a well-known service name
         from lib/commonPorts.ts) via lib/portMatch.ts. -->
    <div class="fb-field">
      <span class="fb-label">Port</span>
      <input
        type="text"
        placeholder={viewportState.isMobile ? 'Port — number or service' : ''}
        bind:value={appState.filters.port}
        aria-label="Port — number or service"
      />
    </div>

    <!-- One list for both directions, because there is one filter: it
         matches an event whose in *or* out interface is the value
         picked. Typing an unseen name works here too. -->
    <div class="fb-field">
      <span class="fb-label">Interface</span>
      <input
        type="text"
        list="fb-seen-interfaces"
        placeholder={viewportState.isMobile ? 'Interface' : ''}
        bind:value={appState.filters.interface}
        aria-label="Interface"
      />
    </div>

    <div class="fb-field">
      <span class="fb-label">Rule</span>
      <div class="rule-group">
        <input
          type="text"
          placeholder={viewportState.isMobile ? (appState.filters.ruleRegex ? 'Rule / raw line regex…' : 'Rule / label contains…') : ''}
          bind:value={appState.filters.rule}
          class="rule"
          aria-label={appState.filters.ruleRegex ? 'Rule/raw line regex search' : 'Rule label search'}
        />
        <button
          class="regex-toggle"
          class:active={appState.filters.ruleRegex}
          onclick={() => (appState.filters.ruleRegex = !appState.filters.ruleRegex)}
          title={appState.ruleMatchStatus === 'too-slow'
            ? 'That pattern took too long to evaluate and was stopped, so the rule filter is inactive. Try a simpler one.'
            : appState.ruleMatchStatus === 'invalid'
              ? 'That is not a valid regular expression, so the rule filter is inactive.'
              : 'Treat the rule search above as a regular expression (matches rule label or raw log line)'}
          class:refused={appState.ruleMatchStatus === 'too-slow' || appState.ruleMatchStatus === 'invalid'}
          aria-pressed={appState.filters.ruleRegex}
        >
          .*
        </button>
      </div>
    </div>

    {#if viewportState.isMobile}
      <!-- The mobile drawer is already a vertical stack with room to
           spare (#85's 44px-row convention below), so it keeps its own
           always-open list -- unchanged by #1197's ruling, which only
           moved the desktop trigger (now Whisper.svelte's own "columns
           ▸", see that file's comment). -->
      <div class="fb-field columns-field">
        <span class="fb-label">Columns</span>
        <div class="col-toggles" role="group" aria-label="Choose which columns the stream shows">
          <ColumnToggles touch />
        </div>
      </div>
    {/if}

    {#if appState.hasActiveFilters && !viewportState.isMobile}
      <button class="tf-clear" onclick={() => appState.resetFilters()} aria-label="Clear all filters" title="Clear every term">× clear</button>
    {/if}

    <!-- Also moved off the retired hamburger (#544). Phone-width only:
         the desktop control is Toolbar's. -->
    {#if viewportState.isMobile}
      <label class="duration">
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

    {#if !viewportState.isMobile}
      <!-- Fold slides the bar back into the box (#644, round 8) -- the
           typed grammar/click model stays appState.filters either way,
           so nothing here is lost by folding, only hidden. -->
      <button
        class="tf-fold"
        onclick={() => {
          expanded = false
          // #1246: fold means put it away -- the focus this hands back to
          // the box must not pop the token menu open in the strip's place.
          closeMenu({ suppress: true })
          hintEl?.focus()
        }}
        aria-label="Fold filters back into the box"
        title="Fold filters back into the box">fold ▸</button
      >
    {/if}
  </div>
{/if}

<!-- #1226: one datalist per field for the whole strip, at the top level
     rather than beside each input, so they survive the strip folding and
     are not rebuilt every time it opens. A <datalist> whose input is not
     on screen is inert, and an empty one simply offers nothing -- which
     is what a fresh instance, or a failed fetch, correctly shows. -->
<datalist id="fb-seen-protos">
  {#each seenValuesState.proto as p (p)}
    <option value={p}></option>
  {/each}
</datalist>
<datalist id="fb-seen-interfaces">
  {#each seenValuesState.interfaces as iface (iface)}
    <option value={iface}></option>
  {/each}
</datalist>

<style>
  /* Deck.svelte mounts the stream's own card-body as `<Whisper />
     <FilterBar /> <LiveTable />`, in that document order (untouched
     here -- Deck.svelte is out of scope for this fidelity pass). Round
     30's own order is filter line -> bar -> whisper -> table (#697, "the
     top is a flow column"), so every top-level element this component
     renders sits ahead of Whisper's own root in the shared
     `.card-body` flex column via `order` -- Whisper (unordered, default
     0) then keeps its place ahead of LiveTable (also default 0) purely
     from DOM order, so nothing there needs to change either. */
  .filterline,
  .fold-trigger,
  .bar,
  .mobile-row {
    order: -1;
  }

  /* Round 30's filter line (#697, #s5 `.filterline`): the toggle, the
     always-on box, and the span pills, left-to-right on one row. */
  .filterline {
    display: flex;
    gap: 18px;
    align-items: center;
  }

  /* #697: no button is welded to this any more -- the box is its own
     disclosure. Full border-radius now that nothing sits flush against
     its left edge, plus the pointer/hover/focus affordance the removed
     `.fb-open` button used to carry. */
  .fbox {
    flex: 1;
    display: flex;
    gap: 12px;
    align-items: center;
    flex-wrap: wrap;
    min-height: 28px;
    padding: 4px 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
    cursor: pointer;
    /* #1246: the token menu hangs off the box's own left edge. */
    position: relative;
  }

  .fbox:hover {
    border-color: var(--fg-muted);
  }

  .fbox:focus-visible {
    outline: none;
    border-color: var(--accent);
  }

  /* While the strip is open, matching how `.fb-open.on` used to mark
     the toggle. */
  .fbox.open {
    border-color: var(--accent);
  }

  .fchips {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
  }

  .chip {
    display: inline-flex;
    gap: 5px;
    align-items: baseline;
    white-space: nowrap;
  }

  .chip em {
    font-style: normal;
    color: var(--accent);
  }

  .chip-x {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 12px;
    cursor: pointer;
    padding: 0;
  }

  .chip-x:hover {
    color: var(--alarm);
  }

  /* #1246: the pending token and its caret -- the committed chip's own
     markup, quieter, with no value and no ⌫ because there is nothing to
     remove yet. */
  .chip.pending {
    color: var(--fg-muted);
  }

  .tm-caret {
    display: inline-block;
    width: 1px;
    height: 12px;
    background: var(--accent);
    margin-left: 2px;
    vertical-align: -2px;
    animation: tm-blink 1s step-end infinite;
  }

  @keyframes tm-blink {
    50% {
      opacity: 0;
    }
  }

  /* #1246 (round 57): the field and value menus. FilterPresetsMenu's own
     floating-panel dress -- same elevated background, hairline border,
     radius, shadow and z-index -- so the box's two popups read as one
     kind of thing; anchored left, where the box's own content starts,
     and wide enough to carry a field name plus its one-line hint. */
  .token-menu {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    width: 360px;
    max-width: 92vw;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 6px 0;
    z-index: 40;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.35);
    cursor: default;
    animation: tm-in 0.16s ease-out;
  }

  /* A value is one short word; only the field menu carries hints wide
     enough to need the full panel. */
  .token-menu.tm-values {
    width: 220px;
  }

  @keyframes tm-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .token-menu {
      animation: none;
    }

    .tm-caret {
      animation: none;
    }
  }

  /* Which field's values these are -- the way back, said once at the top
     rather than repeated on every row. */
  .tm-crumb {
    padding: 4px 14px 6px;
    font: 500 9px var(--font-mono);
    letter-spacing: 0.08em;
    color: var(--fg-dim);
    border-bottom: 1px solid var(--border);
    margin-bottom: 4px;
  }

  .tm-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 100%;
    text-align: left;
    background: none;
    border: 0;
    padding: 7px 14px;
    cursor: pointer;
  }

  .tm-item:hover,
  .tm-item.focused {
    background: var(--bg-hover);
  }

  .tm-name {
    font: 12px var(--font-mono);
    color: var(--fg-muted);
  }

  .tm-item:hover .tm-name,
  .tm-item.focused .tm-name {
    color: var(--fg);
  }

  .tm-hint {
    font: 10.5px var(--font-sans);
    color: var(--fg-dim);
  }

  .tm-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  /* Where the keyboard is, distinct from where the pointer is hovering. */
  .tm-item.focused {
    box-shadow: inset 2px 0 0 var(--accent);
  }

  /* The fields that take typing as well as a pick (port, either side's
     address) say so at the foot of their own menu. */
  .tm-typed {
    margin: 0;
    padding: 6px 14px 2px;
    font: 10.5px var(--font-sans);
    color: var(--fg-dim);
  }

  /* The empty box still says what it is (#697) -- round 29's box only
     rendered once a filter existed, which is exactly why the build kept
     a second "Filters ▸" control alive beside it. */
  /* #734: a real text input, so it actually takes keystrokes -- reset to
     read as the quiet hint it looks like (no color of its own; the
     shared `input, select` rule below gives it the same ink every other
     field's typed text has, and `input::placeholder` dims the hint text
     until something is typed), but keeping the focus ring the app uses
     everywhere else, since this is the keyboard's way in. flex:1 lets it
     take up whatever width the chips don't, so the whole box stays one
     click/type target. */
  .fbtype {
    appearance: none;
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    text-align: left;
    cursor: text;
    flex: 1;
    min-width: 60px;
  }

  .fbtype:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* Round 30's `.spans`: quiet pills, the chosen one in full ink. A span
     the buffer cannot cover is dimmed and unclickable rather than
     hidden -- the operator should see that a fortnight exists and is
     not held, not wonder where it went. */
  .spans {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 10px;
  }

  .span {
    background: transparent;
    border: none;
    padding: 0;
    font: 500 11px var(--font-sans);
    color: var(--fg-dim);
    cursor: pointer;
  }

  .span:hover:not(:disabled) {
    color: var(--fg);
  }

  .span.on {
    color: var(--fg);
  }

  .span:disabled {
    color: var(--fg-dim);
    opacity: 0.4;
    cursor: not-allowed;
  }

  .reach {
    font: 400 10.5px var(--font-sans);
    color: var(--fg-dim);
    margin-left: 2px;
  }

  .mobile-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 9px 14px;
    font-size: 14px;
    /* 44px minimum touch target (issue #85). */
    min-height: 44px;
  }

  .trigger:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  /* The folded box (#644, round 8): a quieter pill than .trigger's --
     this is chrome sitting over the live table on every desktop visit,
     not a modal's one-shot entry point. */
  .fold-trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    border-radius: 999px;
    padding: 4px 14px;
    font-size: 12px;
    font-family: var(--font-mono);
    cursor: pointer;
  }

  .fold-trigger:hover {
    color: var(--fg-muted);
    border-color: var(--fg-muted);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
  }

  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 30;
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 10px 14px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  .bar.drawer {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 31;
    flex-direction: column;
    /* Round 57's "noticed, unverified", checked against this file's own
       rules and confirmed (#1246): the drawer's content is far taller
       than 80vh on any phone -- every input and select in here is
       min-height 44px and full width (issue #85's touch target), which is
       ~44px a row for nine fields, three rows each for the two address
       groups, and a wrapped column list of thirteen more. With `.bar`'s
       flex-wrap: wrap left on, a column that tall does not scroll: it
       opens a second column beside the first and overflow-y has nothing
       to do. nowrap is what makes max-height + overflow-y below mean
       what they say. */
    flex-wrap: nowrap;
    max-height: 80vh;
    overflow-y: auto;
    border-radius: 16px 16px 0 0;
    border-bottom: none;
    padding: 10px 18px calc(18px + env(safe-area-inset-bottom));
    box-shadow: 0 -20px 50px rgba(0, 0, 0, 0.4);
  }

  /* The thin bar (#644, round 8's correction of round 7's rejected fat
     panel: "the box slides out to the left as a thin bar... reminiscent
     of the old live view"). Overrides the boxed/elevated look .bar
     carries for .drawer above -- one quiet row, no border box, dim
     micro-labels over hairline-underlined values. */
  .bar.thin {
    background: color-mix(in srgb, var(--bg) 55%, transparent);
    backdrop-filter: blur(6px);
    border: none;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    padding: 6px 4px 8px;
    gap: 6px;
    align-items: flex-end;
    animation: unfurl 0.35s ease-out;
    transform-origin: right center;
  }

  @keyframes unfurl {
    from {
      transform: scaleX(0.05);
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .bar.thin {
      animation: none;
    }
  }

  .fb-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .fb-label {
    display: none;
    font: 500 8px var(--font-mono);
    letter-spacing: 0.14em;
    color: var(--fg-dim);
    text-transform: uppercase;
  }

  /* #1191: the strip used the left 1050px of a 1920px bar and left
     `fold ▸` marooned alone at the far edge, because every field sat at
     its natural width and the whole of the slack went to .tf-fold's own
     margin-left:auto. The fields share that slack out between
     themselves instead -- the controls inside keep the fixed widths the
     `.thin input`/`.thin select` rules give them, so this widens the
     gaps between groups and never the inputs. With no free space left
     to claim, fold's auto margin resolves to nothing and it sits beside
     `columns ▸`, which is where round 30 draws the pair. Direct
     children only: the captioned sub-fields inside an .addr-group must
     go on hugging their own control. */
  .bar.thin > .fb-field {
    flex-grow: 1;
  }

  /* Visible on the thin bar only -- the mobile drawer already names each
     field via its placeholder/aria-label, and showing this too would be
     a mobile visual change nothing here asked for. */
  .thin .fb-label {
    display: block;
  }

  .thin input,
  .thin select {
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--hair-2);
    border-radius: 0;
    padding: 1px 2px 3px;
    font: 12px var(--font-mono);
    /* Compact by design (#683, round 29's "one quiet row ... fits on one
       line at 1600px wide") -- a filled field shows its own short value
       (a device name, "drop", "DE"), not prose, so a value that's still
       too long to fit is clipped rather than allowed to reflow the row. */
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* #717: a bare `border: none` does not stop a real <select> drawing
     its own native dropdown arrow -- Chromium and friends render that
     glyph regardless of author CSS unless appearance is reset too, so
     without this the control still reads as "an unstyled browser form
     field" even though the box chrome above is already gone. Two small
     gradients stand in for the mockup's own dim inline "▾" character
     (the-whole.html #s5), sized and coloured to match it rather than
     the browser's bold default triangle. */
  .thin select {
    width: 72px;
    appearance: none;
    -webkit-appearance: none;
    -moz-appearance: none;
    cursor: pointer;
    padding-right: 13px;
    background-image:
      linear-gradient(45deg, transparent 50%, var(--fg-dim) 50%),
      linear-gradient(135deg, var(--fg-dim) 50%, transparent 50%);
    background-position:
      calc(100% - 8px) 55%,
      calc(100% - 4px) 55%;
    background-size: 4px 4px, 4px 4px;
    background-repeat: no-repeat;
  }

  .thin input[type='text'] {
    width: 80px;
  }

  .thin .addr-group select {
    width: 58px;
  }

  .thin .rule {
    width: 130px;
  }

  .thin input:focus,
  .thin select:focus {
    border-bottom-color: var(--accent);
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 10px;
    flex: none;
  }

  .drawer-header {
    display: flex;
    align-items: center;
    margin-bottom: 6px;
  }

  .drawer-title {
    font-size: 15px;
    font-weight: 600;
    color: var(--fg);
  }

  .done {
    margin-left: auto;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--accent);
    border-radius: 5px;
    padding: 7px 14px;
    font-size: 13px;
    min-height: 44px;
  }

  input,
  select {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 14px;
    min-width: 0;
    /* 44px minimum touch target (issue #85) -- desktop's tighter 8px
       vertical padding above is comfortable with a mouse but too
       cramped to reliably tap; the drawer-only override below restores
       enough height without changing the always-visible desktop bar. */
  }

  .drawer input,
  .drawer select,
  .drawer .regex-toggle {
    min-height: 44px;
  }

  input::placeholder {
    color: var(--fg-dim);
  }

  input:focus,
  select:focus {
    outline: none;
    border-color: var(--accent);
  }

  input[type='text'] {
    width: 145px;
  }

  .drawer input[type='text'],
  .drawer select {
    width: 100%;
  }

  /* #438: keeps a Source or Destination group's scope/query/country
     controls together while the bar's own flex-wrap moves whole groups
     around, rather than letting the three drift apart mid-wrap. */
  .addr-group {
    display: flex;
    gap: 8px;
  }

  .thin .addr-group {
    gap: 4px;
  }

  .drawer .addr-group {
    flex-direction: column;
  }

  .addr-group select {
    flex: none;
    width: auto;
  }

  .drawer .addr-group select {
    width: 100%;
  }

  .swap {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 10px;
    font-size: 16px;
    line-height: 1;
    flex: none;
    align-self: center;
  }

  .swap:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .drawer .swap {
    align-self: flex-start;
    min-height: 44px;
  }

  /* Matches round 8's own .fb-swap: a plain glyph, no button chrome. */
  .thin .swap {
    background: none;
    border: none;
    padding: 0 2px;
    font-size: 14px;
    align-self: flex-end;
  }

  .thin .swap:hover {
    color: var(--fg-muted);
  }

  /* A pattern that was invalid, or refused for overrunning its time
     budget (see lib/ruleMatcher.ts). The filter is inactive rather than
     silently matching nothing, so say so. */
  .regex-toggle.refused {
    border-color: var(--danger, #c0392b);
    color: var(--danger, #c0392b);
  }

  /* No flex-basis here: .fb-field is a column flex container (its own
     rule above), so a basis set on this its lone row-flex child would
     be read along .fb-field's main axis -- vertical, not horizontal --
     ballooning this field's height to ~200px and pushing it and
     everything after it (fold ▸) up out of the row. That is the "RULE
     and fold have come loose, floating above the row" glitch (#717).
     .rule below already carries its own horizontal flex-basis correctly
     (it is *its* row-flex parent, .rule-group, that is row-direction). */
  .rule-group {
    display: flex;
    gap: 4px;
  }

  .rule {
    width: 200px;
    flex: 1 1 200px;
  }

  .regex-toggle {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-dim);
    border-radius: 5px;
    padding: 0 10px;
    font-family: var(--font-mono);
    font-size: 13px;
    flex: none;
  }

  .regex-toggle:hover {
    color: var(--fg-muted);
    border-color: var(--fg-muted);
  }

  .regex-toggle.active {
    color: var(--accent);
    border-color: var(--accent);
    background: var(--accent-bg);
  }

  /* Matches .thin .swap's own rule above: a plain glyph riding the
     hairline row, no button-box chrome. */
  .thin .regex-toggle {
    background: none;
    border: none;
    border-bottom: 1px solid var(--hair-2);
    border-radius: 0;
    padding: 1px 2px 3px;
    font-size: 11px;
    width: auto;
  }

  .thin .regex-toggle.active {
    background: none;
    border-bottom-color: var(--accent);
  }

  .thin .regex-toggle.refused {
    border-bottom-color: var(--danger, #c0392b);
  }

  .clear {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    padding: 8px 14px;
    font-size: 14px;
  }

  .clear:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .mobile-row .clear {
    min-height: 44px;
  }

  /* The thin bar's own clear/fold -- round 8's "× clear" and "fold ▸",
     plain text rather than .clear's bordered button to stay quiet. */
  .tf-clear,
  .tf-fold {
    align-self: center;
    background: none;
    border: none;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }

  .tf-clear {
    color: var(--fg-dim);
    margin-left: auto;
  }

  .tf-clear:hover {
    color: var(--alarm);
  }

  /* Round 30 draws "× clear" and "fold ▸" together at the right end of
     the strip (`stream-bar-out.png`). `× clear` only mounts while a
     filter is set, so its own margin-left:auto could not carry fold
     across on an empty filter -- fold sat at the left of the row the
     column chooser's flex-basis:100% pushes it onto. Fold claims the
     free space itself, and gives it up again when clear is there to
     claim it first, so the pair stays adjacent rather than splitting
     the row between two auto margins. */
  .tf-fold {
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: 10.5px;
    margin-left: auto;
  }

  .tf-clear ~ .tf-fold {
    margin-left: 0;
  }

  /* #729: the column chooser, mobile drawer only now -- the desktop
     trigger and its popover moved to Whisper.svelte's own hand under
     #1197's ruling (see that file's comment). Same fb-field/fb-label
     shape every other control in the drawer already uses -- a row of
     checkboxes, not a new kind of control. The checkboxes themselves
     (.col-toggle/.col-group-heading) are ColumnToggles.svelte's own
     styles now, `touch` for the drawer's 44px rows (issue #85) --
     only this container and its flex layout stay here. */
  .columns-field {
    flex-basis: 100%;
  }

  .col-toggles {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
  }

  .duration {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--fg-muted);
    font-size: 14px;
    min-height: 44px;
  }

  /* Below this width the fixed input widths (145px/200px) leave several
     fields too narrow to comfortably type into once wrapped one per line
     -- let them fill the row instead. Only reachable today via a narrow
     desktop window (viewportState.isMobile's drawer already covers real
     phone widths, and already applies this same full-width treatment
     unconditionally). */
  @media (max-width: 520px) {
    .bar:not(.drawer) {
      flex-direction: column;
      align-items: stretch;
    }

    .bar:not(.drawer) input[type='text'],
    .bar:not(.drawer) select {
      width: 100%;
    }

    .bar:not(.drawer) .addr-group {
      flex-direction: column;
    }

    .bar:not(.drawer) .addr-group select {
      width: 100%;
    }

    .bar:not(.drawer) .swap {
      align-self: flex-start;
    }

    .rule-group {
      flex: 1 1 auto;
    }
  }
</style>
