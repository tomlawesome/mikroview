<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1363: an already-open tab whose server was upgraded either reloads
  // itself (lib/freshness.svelte.ts's safe path) or, when that was not
  // safe -- something open, held, or mid-edit -- shows this line
  // instead. Ratified 2026-09-30:
  //
  //   - 7a: once shown it waits for a click and never reloads on its
  //     own after that, whatever the busy reason it appeared for does
  //     next.
  //   - 8a: names no version, just "a newer version" -- nobody reading
  //     this needs a build id to know what to do.
  //
  // In the same banner stack as UpgradeNotice (App.svelte), directly
  // above it, with the same raised chrome -- but for every role, not
  // just admin: any session's tab can be the one still on the old
  // version. No ✕, for the same reason UpgradeNotice has none: reloading
  // is the only way this goes away, so there is nothing a dismissal
  // would be for.
  import { freshnessState } from '../lib/freshness.svelte'
</script>

{#if freshnessState.banner}
  <div class="banner" role="status">
    <span class="line">mikroview has been upgraded to a newer version</span>
    <button type="button" class="link" onclick={() => freshnessState.reloadNow()}>reload</button>
  </div>
{/if}

<style>
  .banner {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.55rem 0.9rem;
    /* Same raised-neutral-plus-accent-ink chrome as UpgradeNotice,
       directly below this in the stack -- see that component's own
       note on why it is this pair and not ConfigProblemBanner's reject
       one: an upgrade having landed is not something wrong. */
    background: var(--bg-elevated);
    color: var(--accent);
    border-bottom: 1px solid var(--border);
    font-size: 0.8rem;
    line-height: 1.45;
  }

  .line {
    flex: 1;
    min-width: 0;
  }

  .link {
    background: none;
    border: none;
    padding: 0;
    flex-shrink: 0;
    color: var(--accent);
    text-decoration: underline;
    cursor: pointer;
    font: inherit;
  }

  .link:hover {
    color: var(--fg);
  }
</style>
