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

  // A ring that names an element is measured off the live render rather
  // than drawn at a hand-placed percentage (#750). Measured every frame
  // while the tour is open, not once per card: the deck's roll runs for
  // ~700ms after the card changes, so a single reading would place the
  // ring against a card still in flight -- the same trap goTo() in the
  // live-check harness documents.
  type Box = { top: string; left: string; width: string; height: string }
  let boxes = $state<Record<string, Box>>({})

  function measure(list: typeof highlights) {
    const next: Record<string, Box> = {}
    for (const h of list) {
      if (!h.selector) continue
      const el = document.querySelector(h.selector)
      if (!el) continue
      const r = el.getBoundingClientRect()
      // A card that is mounted but not rendered has a zero box on both
      // axes; falling back to the hand-placed value beats ringing a
      // point. A rule or hairline is legitimately zero on *one* axis
      // only (a plain horizontal SVG <line>'s bounding box is exactly
      // 0 tall, not merely thin -- the live-check skill's own note on
      // Playwright and SVG geometry) -- that case is real and goes on
      // to the padding below, not to the empty-box fallback.
      if (r.width === 0 && r.height === 0) continue
      // A rule or hairline measures only a pixel or two thick -- the
      // fall's now line is ~1px tall. Pad it to a visible band, centred
      // on the element, rather than ringing a sliver (#750).
      const MIN_PX = 28
      let top = r.top
      let height = r.height
      if (height < MIN_PX) {
        top -= (MIN_PX - height) / 2
        height = MIN_PX
      }
      let left = r.left
      let width = r.width
      if (width < MIN_PX) {
        left -= (MIN_PX - width) / 2
        width = MIN_PX
      }
      next[h.label] = {
        top: `${(top / window.innerHeight) * 100}%`,
        left: `${(left / window.innerWidth) * 100}%`,
        width: `${(width / window.innerWidth) * 100}%`,
        height: `${(height / window.innerHeight) * 100}%`,
      }
    }
    // Only assign when something moved. This runs every frame, and a
    // fresh object each time would re-render the rings continuously.
    if (JSON.stringify(next) !== JSON.stringify(boxes)) boxes = next
  }

  $effect(() => {
    const list = highlights
    if (list.length === 0) {
      boxes = {}
      return
    }
    let raf = 0
    const tick = () => {
      measure(list)
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  })
</script>

{#if card}
  <div class="tour" role="group" aria-label="The tour: {card.name}, {journeyState.cardIndex + 1} of {total}">
    <div class="rings" aria-hidden="true">
      {#each highlights as h (h.label)}
        {@const box = boxes[h.label] ?? h}
        <div class="ring" style="--h-top: {box.top}; --h-left: {box.left}; --h-width: {box.width}; --h-height: {box.height}">
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
