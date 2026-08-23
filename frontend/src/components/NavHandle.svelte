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
  // Two things the record puts on the handle that this issue does not
  // build: the open-flag count badge is #546's, and the pulse is
  // specified as "the receiving dot's pulse" -- the rail-head dot itself
  // is #549's chrome work and does not exist yet, so the hub pulses off
  // the same signal the dot will use (a live stream) rather than
  // inventing a second source of truth.
  import { appState } from '../lib/state.svelte'
  import { railPref } from '../lib/rail.svelte'

  let { onrestore }: { onrestore: () => void } = $props()

  const receiving = $derived(appState.connState === 'open')

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
  aria-label="Restore navigation"
  title="Restore navigation"
>
  <svg class="mark" viewBox="0 0 30 84" aria-hidden="true" focusable="false">
    <!-- Hub sits on the edge itself: half the circle is off-canvas, which
         is what makes it read as attached to the viewport edge rather
         than floating in the tab. -->
    <g fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
      <path d="M6 42h6.5M6 42l7-7M6 42l7 7" opacity="0.85" />
    </g>
    <circle class="hub" cx="6" cy="42" r="3.4" fill="currentColor" />
  </svg>
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
    background: color-mix(in srgb, var(--bg-elevated) 72%, transparent);
    backdrop-filter: blur(6px);
    color: var(--fg-muted);
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
