<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #646 beat 6, "The tour": deck by deck, ringing key controls and info
  // in accent hairline with a concise label each -- round 29's ratified
  // shape ("58, yes but it should highlight key handles/inputs/outputs
  // and label/explain concisely"), walking "THE FALL · 1 OF 6 · NEXT ▸"
  // as the design demonstrates it. The count is never hardcoded: it is
  // journeyState.cards.length, the deck's own real card list (#647 grew
  // it to seven for an admin).
  //
  // The deck itself (App.svelte) stays mounted underneath and does the
  // actual rolling -- journeyState.nextCard() only ever sets appState.view,
  // exactly like clicking the roll rail. This overlay draws the rings and
  // the progress bar on top of it.
  import { journeyState } from '../lib/journey.svelte'
  import { TOUR_HIGHLIGHTS } from '../lib/tourHighlights'

  const total = $derived(journeyState.cards.length)
  const card = $derived(journeyState.cards[journeyState.cardIndex])
  const highlights = $derived(card ? (TOUR_HIGHLIGHTS[card.key] ?? []) : [])
  const isLast = $derived(journeyState.cardIndex >= total - 1)
</script>

{#if card}
  <div class="tour" role="group" aria-label="The tour: {card.name}, {journeyState.cardIndex + 1} of {total}">
    <div class="rings" aria-hidden="true">
      {#each highlights as h (h.label)}
        <div class="ring" style="--h-top: {h.top}; --h-left: {h.left}; --h-width: {h.width}; --h-height: {h.height}">
          <span class="tag">{h.label}</span>
        </div>
      {/each}
    </div>

    <div class="bar">
      <span class="progress">
        {card.name.toUpperCase()} · {journeyState.cardIndex + 1} OF {total}
      </span>
      <button type="button" class="next" onclick={() => journeyState.nextCard()}>
        {isLast ? 'finish ▸' : 'next ▸'}
      </button>
      <button type="button" class="leave" onclick={() => journeyState.leaveTour()}>leave the tour</button>
    </div>
  </div>
{/if}

<style>
  .rings {
    position: fixed;
    inset: 0;
    z-index: 45;
    pointer-events: none;
  }

  .ring {
    position: absolute;
    top: var(--h-top);
    left: var(--h-left);
    width: var(--h-width);
    height: var(--h-height);
    border: 1px solid var(--accent);
    border-radius: 8px;
    opacity: 0.85;
  }

  .tag {
    position: absolute;
    top: -20px;
    left: 0;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.02em;
    color: var(--accent);
    white-space: nowrap;
    text-shadow:
      0 0 4px var(--bg),
      0 0 4px var(--bg),
      0 0 4px var(--bg);
  }

  .bar {
    position: fixed;
    left: 0;
    right: 0;
    bottom: 24px;
    z-index: 46;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 16px;
    margin: 0 auto;
    width: fit-content;
    max-width: 90vw;
    padding: 10px 18px;
    background: var(--bg-elevated);
    border: 1px solid var(--hair-2, var(--border));
    border-radius: 999px;
    box-shadow: 0 12px 32px -8px rgba(0, 0, 0, 0.45);
  }

  .progress {
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .next {
    font: 600 11px var(--font-mono);
    letter-spacing: 0.04em;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 999px;
    padding: 4px 14px;
    cursor: pointer;
    white-space: nowrap;
  }

  .next:hover {
    background: var(--accent-bg-hover, var(--bg-hover));
  }

  .leave {
    font-size: 11px;
    color: var(--fg-dim);
    background: transparent;
    border: 0;
    padding: 0;
    cursor: pointer;
    white-space: nowrap;
  }

  .leave:hover {
    color: var(--fg-muted);
  }
</style>
