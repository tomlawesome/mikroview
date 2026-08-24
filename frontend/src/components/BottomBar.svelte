<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The small-screen counterpart to NavRail/NavHandle (#550, under #486).
  // Built from docs/design/screens/navigation/DESIGN.md's "Small screens"
  // section, which is authoritative: "Bottom bar of the five groups
  // (badge intact); tapping a group with more than one page raises a
  // half-sheet (house modal: focus trap, Esc/back closes); single-page
  // groups go straight to the page. Dock and density are pointer-width
  // affordances -- they do not exist on the bottom bar."
  //
  // The five groups, their order, each page's home and the reserved-slot
  // rule all come from lib/navGroups.ts, the same table NavRail.svelte
  // renders -- carried forward rather than redefined here, so the two
  // surfaces cannot disagree about the geography.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { spokenLabel } from '../lib/rail.svelte'
  import { visibleGroups, type NavGroup, type NavItem } from '../lib/navGroups'
  import { trapFocus } from '../lib/focusTrap'
  import RailIcon from './RailIcon.svelte'

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  const groups = $derived(visibleGroups(isAdmin))

  // Same store, same wording as NavRail's Flags row -- see that
  // component's comment for why activeCount is already "open
  // *unexcluded*" with no exclusion filter needed on top.
  const flagCount = $derived(flagsState.activeCount)

  function showCount(item: NavItem): boolean {
    return item.badge === true && flagCount > 0
  }

  // A group carries the badge if any of its pages does -- today that is
  // only Flags, inside Detect, but this does not assume it stays that
  // way.
  function groupCount(group: NavGroup): number {
    return group.items.some((i) => i.badge) && flagCount > 0 ? flagCount : 0
  }

  function isCurrent(item: NavItem): boolean {
    return appState.view === item.view
  }

  function groupHasCurrent(group: NavGroup): boolean {
    return group.items.some(isCurrent)
  }

  function spokenGroup(group: NavGroup): string {
    const n = groupCount(group)
    return spokenLabel(group.name, n > 0 ? [`${n} open`] : [])
  }

  function spokenItem(item: NavItem): string {
    return spokenLabel(item.label, showCount(item) ? [`${flagCount} open`] : [])
  }

  // The half-sheet's open group, or null when none is raised. Only a
  // group with more than one page ever sets this -- a single-page group
  // navigates straight to its page instead (see activateGroup below).
  let openGroupName = $state<string | null>(null)
  const openGroup = $derived(groups.find((g) => g.name === openGroupName) ?? null)

  // Browser back closes the sheet, per the record. One pushed history
  // entry per open sheet: opening a second group while one is already
  // open replaces that entry rather than stacking another, so one Back
  // press is always enough to leave the sheet, however many groups were
  // tapped along the way.
  function raiseSheet(name: string) {
    openGroupName = name
    if (history.state?.mvSheet) {
      history.replaceState({ mvSheet: name }, '')
    } else {
      history.pushState({ mvSheet: name }, '')
    }
  }

  // Called for every way the sheet closes except the browser's own Back
  // (onPopState below): Esc, the backdrop, the close button, or picking
  // an item. Consumes the history entry raiseSheet pushed so Back does
  // not need pressing twice, once to leave the sheet and once more to
  // leave the page it was raised from.
  function closeSheet() {
    if (openGroupName === null) return
    openGroupName = null
    if (history.state?.mvSheet) history.back()
  }

  // Fires either because closeSheet() above called history.back() itself,
  // or because the operator pressed the browser/hardware Back button
  // directly. Either way the sheet is already meant to be gone here, so
  // this only clears the state -- calling history.back() again would
  // consume a second, unrelated entry.
  function onPopState() {
    openGroupName = null
  }

  function activateGroup(group: NavGroup) {
    if (group.items.length === 1) {
      appState.view = group.items[0].view
      return
    }
    raiseSheet(group.name)
  }

  function selectItem(item: NavItem) {
    appState.view = item.view
    closeSheet()
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') closeSheet()
  }

  function onBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) closeSheet()
  }
</script>

<svelte:window onkeydown={onKeydown} onpopstate={onPopState} />

<nav class="bottom-bar" aria-label="Main">
  {#each groups as group (group.name)}
    <button
      class="group-btn"
      class:current={groupHasCurrent(group)}
      aria-current={group.items.length === 1 && isCurrent(group.items[0]) ? 'page' : undefined}
      aria-haspopup={group.items.length > 1 ? 'dialog' : undefined}
      aria-expanded={group.items.length > 1 ? openGroupName === group.name : undefined}
      aria-label={groupCount(group) > 0 ? spokenGroup(group) : undefined}
      onclick={() => activateGroup(group)}
    >
      <RailIcon name={group.items[0].icon} />
      <span class="label">{group.name}</span>
      {#if groupCount(group) > 0}
        <!-- aria-hidden: the button's own aria-label already speaks the
             count in words, same convention as NavRail's badge. -->
        <span class="count" aria-hidden="true">{groupCount(group)}</span>
      {/if}
    </button>
  {/each}
</nav>

{#if openGroup}
  <div class="scrim" onclick={onBackdropClick} role="presentation"></div>
  <div
    class="sheet"
    role="dialog"
    aria-modal="true"
    aria-labelledby="sheet-heading"
    tabindex="-1"
    use:trapFocus
  >
    <div class="handle" aria-hidden="true"></div>
    <div class="sheet-header">
      <span id="sheet-heading" class="sheet-title">{openGroup.name}</span>
      <button type="button" class="close" onclick={closeSheet} aria-label="Close">✕</button>
    </div>
    <ul class="sheet-items">
      {#each openGroup.items as item (item.label)}
        <li>
          <button
            class="sheet-item"
            class:current={isCurrent(item)}
            aria-current={isCurrent(item) ? 'page' : undefined}
            aria-label={showCount(item) ? spokenItem(item) : undefined}
            onclick={() => selectItem(item)}
          >
            <RailIcon name={item.icon} />
            <span class="label">{item.label}</span>
            {#if showCount(item)}
              <span class="count" aria-hidden="true">{flagCount}</span>
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  </div>
{/if}

<style>
  /* Pointer-width chrome (dock, density) is a rail-only affordance --
     nothing here reads railPref or renders anything with an equivalent
     job, per the record. */
  .bottom-bar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 40;
    display: flex;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    padding-bottom: env(safe-area-inset-bottom);
  }

  .group-btn {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    /* 44px minimum touch target (issue #85's convention, carried
       forward). */
    min-height: 52px;
    padding: 6px 2px;
    border: 0;
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: 0.68rem;
    cursor: pointer;
  }

  .group-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  .group-btn.current {
    color: var(--fg);
    font-weight: 600;
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .count {
    position: absolute;
    top: 2px;
    right: calc(50% - 20px);
    border-radius: 8px;
    padding: 1px 5px;
    background: var(--alarm);
    color: var(--bg);
    font-size: 0.62rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }

  /* Same scrim/sheet visual language as the rest of the app's mobile
     sheets (ThemeMenu, EventDetailSheet): translucent scrim, a rounded
     panel rising from the bottom edge with a drag handle. */
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    z-index: 50;
  }

  .sheet {
    position: fixed;
    left: 0;
    right: 0;
    /* A half-sheet, per the record -- roughly half the viewport rather
       than the near-full-height EventDetailSheet uses for a single
       event's whole detail. */
    max-height: 50vh;
    bottom: 0;
    overflow-y: auto;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    border-radius: 16px 16px 0 0;
    padding: 10px 14px calc(14px + env(safe-area-inset-bottom));
    box-shadow: 0 -20px 50px rgba(0, 0, 0, 0.4);
    z-index: 51;
  }

  .handle {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border);
    margin: 0 auto 10px;
  }

  .sheet-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 8px;
    margin-bottom: 4px;
    border-bottom: 1px solid var(--border);
  }

  .sheet-title {
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .close {
    background: none;
    border: none;
    color: var(--fg-muted);
    cursor: pointer;
    font-size: 1rem;
    padding: 8px;
  }

  .close:hover {
    color: var(--fg);
  }

  .sheet-items {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .sheet-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 44px;
    padding: 8px 6px;
    border: 0;
    border-radius: 6px;
    background: none;
    color: var(--fg-muted);
    font: inherit;
    font-size: 0.92rem;
    text-align: left;
    cursor: pointer;
  }

  .sheet-item:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }

  .sheet-item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    color: var(--fg);
  }

  .sheet-item.current {
    background: var(--accent-bg);
    color: var(--fg);
    font-weight: 600;
  }

  .sheet-item .count {
    position: static;
    margin-left: auto;
  }
</style>
