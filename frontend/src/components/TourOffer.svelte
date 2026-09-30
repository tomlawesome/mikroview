<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The tour's offer (#1386): one small panel over the live fall, after
  // the wizard's Finish has landed there. The look is the retired
  // glass's (#646 beat 5, round 27: "one glass over the live fall...
  // never a modal maze") -- the same floating panel at the foot of the
  // screen, the same accent pill and quiet link -- because that was the
  // ratified way the tour was offered; only the words change, since the
  // glass offered the tour on the way *to* the wizard and this offers it
  // after. The deck underneath stays fully visible and interactive --
  // there is no veil, and nothing here covers the fall's bar or its
  // bands: the panel sits where the tour's own bar will.
  //
  // It rises a beat after the way out has landed (lib/tour.svelte.ts's
  // OFFER_DELAY_MS), so it never fights the fall, the bar and the groups
  // striking on. Escape is "not now", so a keyboard user is not held.
  import { tourState, tourLengthSentence } from '../lib/tour.svelte'

  const cardCount = $derived(tourState.cards.length)

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      tourState.declineOffer()
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="offerwrap">
  <div class="offer" role="dialog" aria-label="Take the tour?">
    <p class="big">Take the tour?</p>
    <p class="story">{tourLengthSentence(cardCount)}</p>
    <button type="button" class="begin" onclick={() => tourState.acceptOffer()}>begin the tour</button>
    <button type="button" class="later" onclick={() => tourState.declineOffer()}>
      not now — it stays in the account menu
    </button>
  </div>
</div>

<style>
  .offerwrap {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 24px;
    z-index: 40;
    display: flex;
    justify-content: center;
    pointer-events: none;
  }

  .offer {
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
    animation: rise 360ms ease-out both;
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
    to {
      opacity: 1;
      transform: none;
    }
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

  .later {
    align-self: flex-start;
    font-size: 11px;
    color: var(--fg-dim);
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
    text-align: left;
  }

  .later:hover {
    color: var(--fg-muted);
  }

  @media (prefers-reduced-motion: reduce) {
    .offer {
      animation: none;
    }
  }
</style>
