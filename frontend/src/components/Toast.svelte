<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // App-wide transient status message (issue #439) -- mounted once in
  // App.svelte, next to ConnectionBanner. See lib/toast.svelte.ts for why
  // this exists: there was nothing like it in the app before the
  // hover-revealed copy glyph on live-view row tokens needed a "copied"
  // confirmation that isn't just a tooltip (which a mouse that has
  // already moved to click something else would never see).
  //
  // role="status" (not role="alert"): this is a low-priority confirmation
  // of something the user just did, not an error interrupting them --
  // status is polite-queued by assistive tech rather than cutting off
  // whatever it's already reading, matching ConnectionBanner.svelte's own
  // role="status" for its (persistent) messages.
  import { toastState } from '../lib/toast.svelte'
</script>

{#if toastState.message}
  <div class="toast" role="status">{toastState.message}</div>
{/if}

<style>
  .toast {
    position: fixed;
    left: 50%;
    bottom: 28px;
    transform: translateX(-50%);
    z-index: 50;
    padding: 8px 16px;
    border-radius: 8px;
    background: var(--bg-elevated);
    color: var(--fg);
    border: 1px solid var(--border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    font-size: 13px;
    pointer-events: none;
    animation: toast-fade 1.5s ease forwards;
  }

  /* Dismissal itself is driven by toastState's timer (the message
     becomes null and the {#if} unmounts this element) -- this animation
     is only the fade in/out cosmetic, so disabling it under
     prefers-reduced-motion still dismisses on schedule, just as a hard
     cut rather than a fade. */
  @media (prefers-reduced-motion: reduce) {
    .toast {
      animation: none;
    }
  }

  @keyframes toast-fade {
    0% {
      opacity: 0;
      transform: translateX(-50%) translateY(6px);
    }
    10%,
    85% {
      opacity: 1;
      transform: translateX(-50%) translateY(0);
    }
    100% {
      opacity: 0;
    }
  }
</style>
