<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The chrome's Loading state (#549), per docs/design/screens/navigation/
  // DESIGN.md's "States of the chrome": "shell plus ghost rows -- never a
  // spinner page." A spinner tells the operator nothing about what is
  // coming; a handful of placeholder rows echoes the shape of the table
  // that is about to fill in, so a page mid-fetch reads as "this is about
  // to have rows" rather than going blank or vague.
  //
  // Purely decorative -- aria-hidden, with a single sr-only status text
  // carrying the accessible name -- because a stack of shimmering bars has
  // no meaning of its own to announce, and announcing each one would be
  // noise. `label` defaults to something generic enough for any caller
  // that does not pass its own.
  let { label = 'Loading…', rows = 6 }: { label?: string; rows?: number } = $props()
</script>

<div class="ghost-rows" aria-hidden="true">
  {#each Array(rows) as _, i (i)}
    <div class="ghost-row" style="animation-delay: {i * 70}ms">
      <span class="bar bar-a"></span>
      <span class="bar bar-b"></span>
      <span class="bar bar-c"></span>
      <span class="bar bar-d"></span>
    </div>
  {/each}
</div>
<p class="sr-only" role="status">{label}</p>

<style>
  .ghost-rows {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px;
  }

  .ghost-row {
    display: flex;
    align-items: center;
    gap: 14px;
    animation: fade-in 0.3s ease-out both;
  }

  .bar {
    height: 12px;
    border-radius: 4px;
    background: linear-gradient(90deg, var(--bg-hover) 25%, var(--border) 50%, var(--bg-hover) 75%);
    background-size: 200% 100%;
    animation: shimmer 1.6s ease-in-out infinite;
  }

  .bar-a {
    width: 64px;
    flex: none;
  }

  .bar-b {
    flex: 2;
    max-width: 220px;
  }

  .bar-c {
    flex: 1;
    max-width: 140px;
  }

  .bar-d {
    flex: 1;
    max-width: 90px;
  }

  @keyframes shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }

  @keyframes fade-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }

  /* Reduced motion turns every pulse and slide instant, per the record's
     Keyboard-and-accessibility section -- the shimmer and stagger are
     exactly that, so both collapse to a static placeholder. */
  @media (prefers-reduced-motion: reduce) {
    .bar {
      animation: none;
      background: var(--bg-hover);
    }

    .ghost-row {
      animation: none;
    }
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
