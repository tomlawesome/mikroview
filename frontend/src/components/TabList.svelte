<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // The house tablist (docs/design/screens/navigation/DESIGN.md,
  // "Keyboard and accessibility": "Tabs inside pages are the house
  // tablist (arrow keys)"). The standard WAI-ARIA tabs pattern with
  // automatic activation -- moving focus with the arrow keys also
  // changes the selected tab, which is what lets a single Tab stop
  // (roving tabindex: only the selected tab is in the sequence) reach
  // every tab without a second key to "activate" one once focused.
  //
  // Shared by Flags' Exclusions tab and Watchlist's Suggestions tab
  // (#547) rather than each growing its own tab bar -- the two merges
  // are the same interaction, and a second implementation is exactly
  // how they'd have drifted.
  let {
    tabs,
    selected,
    onselect,
    label,
  }: {
    tabs: { id: string; label: string; count?: number }[]
    selected: string
    onselect: (id: string) => void
    label: string
  } = $props()

  let buttons: (HTMLButtonElement | null)[] = []

  function focusAndSelect(index: number) {
    const tab = tabs[index]
    if (!tab) return
    onselect(tab.id)
    buttons[index]?.focus()
  }

  function onKeydown(e: KeyboardEvent, index: number) {
    switch (e.key) {
      case 'ArrowRight':
        e.preventDefault()
        focusAndSelect((index + 1) % tabs.length)
        break
      case 'ArrowLeft':
        e.preventDefault()
        focusAndSelect((index - 1 + tabs.length) % tabs.length)
        break
      case 'Home':
        e.preventDefault()
        focusAndSelect(0)
        break
      case 'End':
        e.preventDefault()
        focusAndSelect(tabs.length - 1)
        break
    }
  }
</script>

<div class="tablist" role="tablist" aria-label={label}>
  {#each tabs as tab, i (tab.id)}
    <button
      bind:this={buttons[i]}
      type="button"
      role="tab"
      id="tab-{tab.id}"
      aria-controls="panel-{tab.id}"
      aria-selected={selected === tab.id}
      tabindex={selected === tab.id ? 0 : -1}
      class="tab"
      class:active={selected === tab.id}
      onclick={() => onselect(tab.id)}
      onkeydown={(e) => onKeydown(e, i)}
    >
      {tab.label}
      {#if tab.count != null}
        <!-- Quiet, outlined -- never the rail's single alarm-filled
             count, which the record reserves for Flags alone
             (DESIGN.md, "Badges and broken state"). -->
        <span class="count">{tab.count}</span>
      {/if}
    </button>
  {/each}
</div>

<style>
  .tablist {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--border);
    flex: none;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    padding: 9px 14px;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .tab:hover {
    color: var(--fg);
  }

  .tab:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .tab.active {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  .count {
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 6px;
    font-size: 11px;
    font-weight: 700;
    color: var(--fg-muted);
    font-variant-numeric: tabular-nums;
  }

  .tab.active .count {
    border-color: var(--accent);
    color: var(--accent);
  }
</style>
