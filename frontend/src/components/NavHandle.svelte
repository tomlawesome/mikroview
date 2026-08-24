<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The docked rail's handle (#545, under #486), per
  // docs/design/screens/navigation/DESIGN.md: a 30x84px glass tab on the
  // left edge, vertically centred on the viewport *always* -- independent
  // of scroll and page, which is why it is fixed-positioned here rather
  // than living inside the shell's flex row.
  //
  // The mark is a hub on the edge with three links fanning inward. The
  // owner accepted it "for now" and recorded that it will be revisited;
  // it is not open for redesign here.
  //
  // The open-flag count badge arrived with #546: docking the navigation
  // never docks the alarm, so the one count on the rail follows it here.
  // The pulse is specified as "the receiving dot's pulse" -- #549 built
  // that dot (NavRail.svelte's .rail-head-dot), but it is not mounted
  // while docked, so the hub here pulses off the same underlying signal
  // (connState === 'open') rather than reaching for a dot that is not on
  // screen. Deliberately still not connection loss, though: the record is
  // explicit that connection state is never the handle's job (see the
  // label below), so 'closed' does not turn this alarm-red the way it
  // turns the dot -- only ConnectionBanner (ConnectionBanner.svelte,
  // mounted regardless of dock state) carries that here.
  import { appState } from '../lib/state.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { railPref } from '../lib/rail.svelte'

  let { onrestore }: { onrestore: () => void } = $props()

  const receiving = $derived(appState.connState === 'open')

  // Same count the rail's Flags row carries -- see NavRail.svelte for why
  // activeCount is already "open *unexcluded*" with no filter on top.
  const flagCount = $derived(flagsState.activeCount)

  // Connection state is never the handle's job (the record is explicit),
  // so the label says what the control does and what the alarm holds,
  // and nothing about the stream.
  const label = $derived(
    flagCount > 0 ? `Restore navigation — ${flagCount} open flags` : 'Restore navigation',
  )

  let el: HTMLButtonElement | undefined = $state()

  // Only when docking is what put this here -- see railPref.justDocked
  // for why an ordinary load must not pull focus.
  $effect(() => {
    if (!el || !railPref.justDocked) return
    railPref.justDocked = false
    el.focus()
  })
</script>

<button
  class="handle"
  class:receiving
  bind:this={el}
  onclick={onrestore}
  aria-label={label}
  title={label}
>
  <svg class="mark" viewBox="0 0 30 84" aria-hidden="true" focusable="false">
    <!-- Hub sits on the edge itself rather than centred in the tab: it is
         meant to read as something attached to the viewport edge. The
         links fan inward from it, which is the direction the rail
         returns from. -->
    <g fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <path d="M8 42h11M8 42l10-9M8 42l10 9" />
    </g>
    <circle class="hub" cx="8" cy="42" r="4.5" fill="currentColor" />
  </svg>

  {#if flagCount > 0}
    <!-- aria-hidden for the same reason as the rail's: the button's own
         label already speaks the count in words. -->
    <span class="count" aria-hidden="true">{flagCount}</span>
  {/if}
</button>

<style>
  .handle {
    position: fixed;
    left: 0;
    /* Centred on the viewport, not the document: fixed + 50% is what
       keeps it put while the content column scrolls under it. */
    top: 50%;
    transform: translateY(-50%);
    z-index: 40;
    width: 30px;
    height: 84px;
    padding: 0;
    border: 1px solid var(--border);
    border-left: 0;
    border-radius: 0 8px 8px 0;
    /* "Glass": the content column has to remain legible through it, so
       this is a translucent surface with a blur rather than a solid one. */
    background: color-mix(in srgb, var(--bg-elevated) 88%, transparent);
    backdrop-filter: blur(6px);
    /* The only route back to a hidden rail, so it earns full-strength
       foreground rather than the muted tone the rail's own rows use. */
    color: var(--fg);
    box-shadow: 1px 0 8px rgb(0 0 0 / 0.28);
    cursor: pointer;
    display: grid;
    place-items: center;
  }

  .handle:hover {
    color: var(--fg);
    background: color-mix(in srgb, var(--bg-elevated) 88%, transparent);
  }

  .handle:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .mark {
    width: 30px;
    height: 84px;
  }

  /* Sits at the foot of the tab rather than stacked under the mark: the
     mark is drawn across the full 84px with its hub on the centre line,
     so anything placed beside it would land on the links fanning inward.
     The bottom strip is the one part of the tab the drawing leaves free. */
  .count {
    position: absolute;
    bottom: 6px;
    left: 50%;
    transform: translateX(-50%);
    border-radius: 7px;
    padding: 0.5px 4px;
    background: var(--alarm);
    color: var(--bg);
    font-size: 0.62rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }

  @media (prefers-reduced-motion: no-preference) {
    .handle.receiving .hub {
      animation: hub-pulse 2.4s ease-in-out infinite;
    }
  }

  @keyframes hub-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.45;
    }
  }
</style>
