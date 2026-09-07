<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1015: the ingest-loss banner family (#995's dropped,
  // rejectedConfigured, rejectedUndeclared, oversized, wsDropped),
  // stacked here instead of collapsing to one line inside
  // ConnectionBanner.svelte. Overlays the top of the content column
  // (App.svelte's `.content` is the positioning context, z-index 35 --
  // above the deck (20) and FilterBar (30/31), below menus and popovers
  // (40) and modals (50)) rather than pushing it, superseding
  // App.svelte:227's "pushes content rather than overlaying it" note
  // for this family only -- the connecting/disconnected line in
  // ConnectionBanner.svelte still pushes. Why: the deck's cards are
  // full-viewport, and every row here can appear or clear on its own
  // (section B's freshness signal), which would otherwise reflow the
  // card underneath on every 5s poll.
  //
  // Row selection (which rows show, in what order) lives in
  // lib/ingestLossBanners.ts (selectIngestLossRows) so it stays unit
  // testable without a component harness -- the same split
  // ConnectionBanner.svelte used before this moved out of it. This file
  // owns only the open/hidden state and the reopen rule's bookkeeping
  // (which kinds were showing when the operator last hid it -- also in
  // lib/ingestLossBanners.ts as shouldReopen, for the same reason).
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import {
    selectIngestLossRows,
    shouldReopen,
    type IngestLossBannerId,
    type IngestLossSeverity,
  } from '../lib/ingestLossBanners'
  import { goToSection } from '../lib/sectionLink'

  // #1001: same viewer gate as the old ConnectionBanner's `details`
  // link -- a viewer has no Settings card to land `details` on
  // (deckCards.ts, #785), and "Clear all" is an irreversible whole-
  // stack action, so both are canEdit-only.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)

  const rows = $derived(
    selectIngestLossRows({
      loss: appState.stats?.syslog?.loss,
      wsDropped: { recent: appState.wsDroppedEpisode.recent, active: appState.wsDroppedActive },
    }),
  )
  const activeIds = $derived(rows.map((r) => r.id))
  // rows is already severity-ordered worst-first (selectIngestLossRows'
  // own contract), so the worst active row is simply the first one.
  const worst = $derived<IngestLossSeverity | null>(rows[0]?.severity ?? null)

  const WORST_COLOR_VAR: Record<IngestLossSeverity, string> = {
    critical: '--alarm',
    warn: '--warn',
    caution: '--caution',
    info: '--log',
  }

  // Not persisted -- no localStorage key (owner ruling, 2026-09-06): a
  // reload always reopens if anything is active, same as first load,
  // since `hidden` starts false.
  let hidden = $state(false)
  // The kinds that were showing at the moment the operator hid the
  // drawer -- "hiding says I have seen these". Checked against
  // activeIds below on every change so a kind outside that set -- new,
  // or one that cleared and came back -- reopens it, because a
  // newcomer has not been seen.
  let hiddenAt = $state<ReadonlySet<IngestLossBannerId>>(new Set())

  $effect(() => {
    if (hidden && shouldReopen(hiddenAt, activeIds)) hidden = false
  })

  const open = $derived(rows.length > 0 && !hidden)

  // Handle click, Enter or Space (a <button>'s native activation) --
  // Escape is not bound, the drawer is not modal (owner ruling).
  function toggle() {
    if (open) {
      hiddenAt = new Set(activeIds)
      hidden = true
    } else {
      hidden = false
    }
  }

  // No toast, no confirm (owner ruling): appState.clearIngestLoss's own
  // refreshDevicesAndStats() call is what makes the drawer disappear at
  // once, and that disappearance is the whole of the feedback.
  async function clearAll() {
    try {
      await appState.clearIngestLoss()
    } catch (err) {
      console.error('Could not clear ingest-loss counters', err)
    }
  }
</script>

{#if rows.length > 0}
  <div
    id="ingest-loss-drawer"
    class="ingest-loss-drawer"
    class:closed={!open}
    role="region"
    aria-label="Ingest loss"
    style="--worst-color: var({worst ? WORST_COLOR_VAR[worst] : '--fg-dim'})"
  >
    <div class="rows-clip">
      <div class="rows">
        {#each rows as row (row.id)}
          <div class="banner banner-{row.severity}" role="status">
            <strong>{row.label}:</strong> <span class="dt">{row.detail}</span>
            {#if row.details && canEdit}
              <button class="detail" type="button" onclick={() => goToSection(row.details!)}>
                details
              </button>
            {/if}
          </div>
        {/each}
      </div>
    </div>
    <div class="sill">
      <button
        type="button"
        class="handle"
        data-testid="ingest-loss-handle"
        aria-expanded={open}
        aria-controls="ingest-loss-drawer"
        title={open ? 'Hide ingest-loss banners' : `Show ingest-loss banners (${rows.length})`}
        aria-label={open ? 'Hide ingest-loss banners' : `Show ingest-loss banners (${rows.length})`}
        onclick={toggle}
      >
        <span class="handle-mark" aria-hidden="true"></span>
      </button>
      {#if canEdit}
        <button type="button" class="clear-all" data-testid="ingest-loss-clear-all" onclick={clearAll}>
          Clear all
        </button>
      {/if}
    </div>
  </div>
{/if}

<style>
  .ingest-loss-drawer {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    z-index: 35;
  }

  /* --- rows: the #995 banner verbatim, ported from ConnectionBanner ---
     Collapsed via the grid 1fr/0fr trick rather than a JS-measured
     max-height, so no ResizeObserver is needed to animate an unknown
     content height (one row vs five). The inner .rows carries the
     overflow:hidden a grid track's own animation needs. */
  .rows-clip {
    display: grid;
    grid-template-rows: 1fr;
    transition: grid-template-rows 180ms ease-out;
  }
  .ingest-loss-drawer.closed .rows-clip {
    grid-template-rows: 0fr;
  }
  .rows {
    overflow: hidden;
    min-height: 0;
  }

  .banner {
    position: relative;
    padding: 8px 16px;
    font-size: 13px;
    text-align: center;
    border-bottom: 1px solid var(--border);
  }
  .banner strong {
    font-weight: 700;
  }
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
  /* #1001, ported verbatim: the details control sits on the banner's
     right edge, within 40px of it -- the live scenario asserts this. */
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

  /* --- the sill: 20px open, a 3px worst-colour line closed --- */
  .sill {
    position: relative;
    height: 20px;
    min-height: 20px;
    display: flex;
    align-items: center;
    background: var(--bg-elevated);
    border-top: 1px solid var(--border);
    border-bottom: 1px solid var(--border);
    transition:
      height 180ms ease-out,
      border-color 180ms ease-out;
  }
  .ingest-loss-drawer.closed .sill {
    height: 3px;
    min-height: 3px;
    border-color: transparent;
    background: var(--worst-color);
  }

  /* The handle: centred, hit area >=72x24 always. Open, it is
     vertically centred on the 20px sill; closed, its box top-aligns to
     the sill/line's own top so the visible mark below (top-aligned
     within the box) sits flush with the line and hangs 5px past its
     3px height -- "straddling the line, 5px of it below" per the
     ratified spec. The invisible remainder of the 24px hit box hangs
     below that, over whatever scene is centred beneath it. */
  .handle {
    position: absolute;
    left: 50%;
    top: 50%;
    width: 72px;
    height: 24px;
    margin: 0;
    padding: 0;
    border: none;
    background: transparent;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    transform: translate(-50%, -12px);
    cursor: pointer;
  }
  .ingest-loss-drawer.closed .handle {
    top: 0;
    transform: translateX(-50%);
  }

  /* Open: a small chevron, up (collapse) -- CSS triangle, no icon font
     or SVG asset needed for one shape that only ever points two ways. */
  .handle-mark {
    width: 0;
    height: 0;
    margin-top: 7px;
    border-left: 5px solid transparent;
    border-right: 5px solid transparent;
    border-bottom: 6px solid var(--fg-dim);
    transition: border-color 120ms ease-out;
  }
  .handle:hover .handle-mark {
    border-bottom-color: var(--fg);
  }
  /* Closed: the chevron triangle is replaced outright by the 48x8
     lozenge -- no count and no text on the line, severity is its
     colour alone, the count lives in the tooltip/aria-label above. */
  .ingest-loss-drawer.closed .handle-mark {
    width: 48px;
    height: 8px;
    border: none;
    border-radius: 4px;
    background: var(--worst-color);
    margin-top: 0;
  }

  /* --- Clear all: same size and ink as .detail above, so the right
     edge reads as one actions column -- details per row, Clear all for
     the whole stack. Hidden closed: "no text on the line". --- */
  .clear-all {
    margin-left: auto;
    margin-right: 16px;
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: var(--fg-dim);
    border: 1px solid color-mix(in srgb, currentColor 40%, transparent);
    border-radius: 6px;
    padding: 2px 8px;
    background: transparent;
    cursor: pointer;
  }
  .clear-all:hover {
    color: var(--fg);
  }
  .ingest-loss-drawer.closed .clear-all {
    opacity: 0;
    pointer-events: none;
    transition: opacity 120ms ease-out;
  }

  @media (prefers-reduced-motion: reduce) {
    .rows-clip,
    .sill,
    .handle,
    .handle-mark,
    .clear-all {
      transition: none;
    }
  }

  /* Phone widths: rows wrap to two lines by ordinary text flow (no
     white-space:nowrap is set anywhere above) -- nothing else changes,
     per the ratified spec. */
</style>
