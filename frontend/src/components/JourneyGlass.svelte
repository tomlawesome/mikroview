<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #646 beats 4 and 5, "Connecting and building" then "The glass" --
  // both a single floating panel over the live fall (round 27: "one
  // glass over the live fall... never a modal maze"), which is why this
  // is one component rather than two: the panel's position and frame
  // never move, only its content does. The deck underneath (mounted by
  // App.svelte) stays fully visible and interactive -- there is no veil.
  import { journeyState, CONNECTING_MS } from '../lib/journey.svelte'

  // Not a wait for real evidence -- see CONNECTING_MS's own comment.
  // "Skip ahead" exists for anyone who does not want to sit through a
  // beat with nothing to click; the timer is the default pace.
  $effect(() => {
    if (journeyState.phase !== 'connecting') return
    const timer = setTimeout(() => journeyState.fromConnecting(), CONNECTING_MS)
    return () => clearTimeout(timer)
  })

  const cardCount = $derived(journeyState.cards.length)
</script>

<div class="glasswrap">
  <div class="glass">
    {#if journeyState.phase === 'connecting'}
      <div class="pulse" aria-hidden="true"><i></i><i></i><i></i></div>
      <p class="big">The pipe is coming alive.</p>
      <p class="story">Building your instance from what has just arrived…</p>
      <button type="button" class="skip" onclick={() => journeyState.fromConnecting()}>Skip ahead</button>
    {:else}
      <p class="big">MikroView is flowing.</p>
      <p class="story">
        {cardCount} {cardCount === 1 ? 'card' : 'cards'}. About two minutes. It ends at the wizard either way.
      </p>
      <button type="button" class="begin" onclick={() => journeyState.beginTour()}>begin the tour</button>
      <button type="button" class="skip-link" onclick={() => journeyState.skipToWizard()}>
        skip straight to the wizard ▸
      </button>
    {/if}
  </div>
</div>

<style>
  .glasswrap {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 24px;
    z-index: 40;
    display: flex;
    justify-content: center;
    pointer-events: none;
  }

  .glass {
    pointer-events: auto;
    width: 100%;
    max-width: 340px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 18px;
    background: var(--bg-elevated);
    border: 1px solid var(--hair-2, var(--border));
    border-radius: 12px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.45);
  }

  .big {
    margin: 0;
    font: 600 13px var(--font-mono);
    color: var(--fg);
  }

  .story {
    margin: 0;
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .pulse {
    display: flex;
    gap: 6px;
  }

  .pulse i {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--now);
    animation: pulse 1.2s ease-in-out infinite;
  }

  .pulse i:nth-child(2) {
    animation-delay: 0.2s;
  }

  .pulse i:nth-child(3) {
    animation-delay: 0.4s;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.3;
    }
    50% {
      opacity: 1;
    }
  }

  .begin {
    align-self: flex-start;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.05em;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 999px;
    padding: 5px 16px;
    cursor: pointer;
  }

  .begin:hover {
    background: var(--accent-bg-hover, var(--bg-hover));
  }

  .skip {
    align-self: flex-start;
    font-size: 11px;
    color: var(--fg-dim);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 12px;
    cursor: pointer;
  }

  .skip-link {
    align-self: flex-start;
    font-size: 11px;
    color: var(--fg-dim);
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
  }

  .skip-link:hover {
    color: var(--fg-muted);
  }

  @media (prefers-reduced-motion: reduce) {
    .pulse i {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
