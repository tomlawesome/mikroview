<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #995: the ingest-loss banner family. MikroView can lose log data
  // four distinct ways; this slot used to show only a fifth condition
  // (wsDropped, one browser tab falling behind a feed it already
  // stored) and styled it as if it were the same kind of loss. Built to
  // docs/design/concepts/ingest-loss-995 (owner: "happy"): each signal
  // gets its own one-line banner on the ratified severity scale, and
  // several firing at once collapse to one bar -- the worst severity
  // leading, a "+N more" chip expanding the rest -- so the app is never
  // pushed down more than one line. Healthy is silent: no banner at
  // all, with the counters idling in the Settings readout instead (see
  // EngineRoom.svelte's "ingest" section).
  //
  // Selection and collapse logic lives in lib/ingestLossBanners.ts,
  // unit tested there without a component harness; this file only
  // renders whatever that pure function returns.
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { selectIngestLossBar, toIngestLossInputs } from '../lib/ingestLossBanners'
  import { goToSection } from '../lib/sectionLink'

  let expanded = $state(false)

  // #1001: the `details` link goes to the Settings readout, and the
  // Settings card only exists for a session that can edit
  // (deckCards.ts) -- a viewer sent to that view gets an empty deck
  // (#785). So a viewer sees the banner without the link, the same
  // grammar Flags.svelte uses to gate its audit-log link.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)

  const bar = $derived(
    selectIngestLossBar(
      toIngestLossInputs(
        appState.stats?.syslog ?? {
          rejected: 0,
          rejectedConfigured: 0,
          rejectedConfiguredHosts: [],
          dropped: 0,
          oversized: 0,
          oversizedHost: '',
        },
        appState.wsDropped,
      ),
    ),
  )

  // Re-collapses once there is nothing left to expand -- "the same
  // chip... re-collapses itself when the counters stop moving" per the
  // ratified drawing. Without this, clearing every signal but one while
  // expanded would leave `expanded` stuck true with no chip left to
  // undo it.
  $effect(() => {
    if (bar.moreCount === 0) expanded = false
  })
</script>

{#if appState.connState !== 'open'}
  <div class="banner banner-{appState.connState}" role="status">
    {appState.connState === 'connecting'
      ? 'Connecting to mikroview…'
      : 'Disconnected from server — attempting to reconnect…'}
  </div>
{:else if bar.lead}
  {#if expanded}
    <div id="ingest-loss-stack">
      {#each bar.banners as banner, i (banner.id)}
        <div class="banner banner-{banner.severity}" role="status">
          <strong>{banner.label}:</strong> <span class="dt">{banner.detail}</span>
          {#if i === 0}
            <button
              class="more"
              type="button"
              aria-expanded="true"
              aria-controls="ingest-loss-stack"
              onclick={() => (expanded = false)}
            >
              less
            </button>
            {#if banner.details && canEdit}
              <button class="detail" type="button" onclick={() => goToSection(banner.details!)}>
                details
              </button>
            {/if}
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <div class="banner banner-{bar.lead.severity}" role="status">
      <strong>{bar.lead.label}:</strong> <span class="dt">{bar.lead.detail}</span>
      {#if bar.moreCount > 0}
        <button
          class="more"
          type="button"
          aria-expanded="false"
          aria-controls="ingest-loss-stack"
          onclick={() => (expanded = true)}
        >
          +{bar.moreCount} more
        </button>
      {/if}
      {#if bar.lead.details && canEdit}
        <button class="detail" type="button" onclick={() => goToSection(bar.lead!.details!)}>
          details
        </button>
      {/if}
    </div>
  {/if}
{/if}

<style>
  .banner {
    position: relative;
    padding: 8px 16px;
    font-size: 13px;
    text-align: center;
    border-bottom: 1px solid var(--border);
  }

  /* #995 (owner ruling, 2026-09-06): each banner is set in two tiers,
     the label bold and dominant, the detail at text weight and
     subordinate. Weight is the only cue -- no size, opacity or
     letter-spacing on top -- ported from
     docs/design/concepts/ingest-loss-995's <strong> rule. */
  .banner strong {
    font-weight: 700;
  }

  .banner-connecting {
    background: var(--row-log-bg);
    color: var(--log);
  }

  .banner-closed {
    background: var(--row-reject-bg);
    color: var(--reject);
  }

  /* The ingest-loss severity scale (owner-ratified, 2026-09-06): red
     critical, orange warning, yellow below warning, cyan information.
     --warn and --caution are #995's own tokens (see app.css); the
     other two reuse the app's existing --alarm and --log. */
  .banner-critical {
    background: color-mix(in srgb, var(--alarm) 9%, transparent);
    color: var(--alarm);
  }

  .banner-warn {
    background: color-mix(in srgb, var(--warn) 9%, transparent);
    color: var(--warn);
  }

  .banner-caution {
    background: color-mix(in srgb, var(--caution) 8%, transparent);
    color: var(--caution);
  }

  .banner-info {
    background: var(--row-log-bg);
    color: var(--log);
  }

  /* #1001: ported verbatim from
     docs/design/concepts/ingest-loss-995's `.banner .detail` rule, plus
     the resets a <button> needs that the drawing's <a> did not -- the
     app has no URL for this, so the affordance is a button that moves
     the view rather than a link that navigates. */
  .detail {
    position: absolute;
    right: 12px;
    top: 50%;
    transform: translateY(-50%);
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: inherit;
    opacity: 0.75;
    text-decoration: none;
    border: 1px solid color-mix(in srgb, currentColor 40%, transparent);
    border-radius: 6px;
    padding: 2px 8px;
    background: transparent;
    cursor: pointer;
  }

  .more {
    display: inline-block;
    margin-left: 10px;
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: inherit;
    background: transparent;
    border: 1px solid color-mix(in srgb, currentColor 40%, transparent);
    border-radius: 6px;
    padding: 2px 8px;
    vertical-align: 1px;
    cursor: pointer;
  }
</style>
