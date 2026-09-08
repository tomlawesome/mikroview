<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1015: the ingest-loss banner family (#995's dropped,
  // rejectedConfigured, rejectedUndeclared, oversized, wsDropped),
  // stacked here instead of collapsing to one line inside
  // ConnectionBanner.svelte. Sits at the top of the content column in
  // flow, exactly where the banners it replaces sat, and pushes what is
  // below it -- App.svelte's "pushes content rather than overlaying it"
  // rule holds for this family too, and the connecting/disconnected
  // line in ConnectionBanner.svelte above it is unchanged. Folding is
  // the whole difference from the old banners: open it takes the room a
  // banner strip always took, closed it is a 2px line with a small
  // arrow pull tab centred on it. The 20px sill an earlier cut put
  // under the rows is gone: the owner rejected it as too thick
  // (2026-09-07) -- "no more than a thin bright orange line with a
  // little arrow pull tab in the centre".
  //
  // An earlier cut of this overlaid the column instead
  // (position:absolute, z-index 35), to avoid reflowing the deck's
  // full-viewport cards when a row clears on its own. That covered the
  // scene bar: with a single row showing, the flags count, the watchers
  // count and the account menu all sat underneath it and could not be
  // clicked (measured on a live instance, #1015). Owner's correction,
  // 2026-09-07 -- the drawer is where the banners were, always; folding
  // is what it adds. Only `position: relative` and a z-index above the
  // deck remain, so the closed handle's hit area, which deliberately
  // hangs below the line, stays clickable over the scene bar's top edge.
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
        {#each rows as row, i (row.id)}
          <div
            class="banner banner-{row.severity}"
            class:has-clear={i === 0 && canEdit}
            class:has-detail={!!row.details && canEdit}
            role="status"
          >
            {#if i === 0 && canEdit}
              <button
                type="button"
                class="clear-all"
                data-testid="ingest-loss-clear-all"
                onclick={clearAll}
              >
                Clear all
              </button>
            {/if}
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
    <div class="line">
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
    </div>
  </div>
{/if}

<style>
  .ingest-loss-drawer {
    /* In flow -- see this file's header comment. Relative and above the
       deck only so the closed handle's overhanging hit area stays
       clickable, not to lift the drawer off the column. */
    position: relative;
    z-index: 25;
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
    /* Clipping alone leaves the buttons in here focusable by keyboard
       while the drawer is folded shut. The old sill placement made
       Clear all `pointer-events: none` when closed for that reason;
       now that it sits in a row, the guarantee has to come from here
       -- and `details` gets it too, which clipping never gave it.
       Delayed to the end of the fold so the collapse still animates. */
    visibility: visible;
    transition: visibility 0s linear 0s;
  }
  .ingest-loss-drawer.closed .rows {
    visibility: hidden;
    transition: visibility 0s linear 180ms;
  }

  .banner {
    position: relative;
    padding: 8px 16px;
    font-size: 13px;
    text-align: center;
    border-bottom: 1px solid var(--border);
  }
  /* The line below is the drawer's bottom edge; without this the last
     row draws its own on top of it and the "thin" line is 3px. */
  .banner:last-child {
    border-bottom: none;
  }
  /* Clear all takes its room out of this row, per the owner's
     "should cut into the banner next to it's space". Both sides, so
     the row's centred text stays centred and clears `details` on the
     right as well -- that link is asserted to sit within 40px of the
     right edge (#1001), so it cannot move to make room. */
  .banner.has-clear {
    padding-left: 92px;
    padding-right: 92px;
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

  /* --- the line: the drawer's bottom edge, 2px in both states ---
     It does not resize, so there is no height to animate and nothing
     that reads as a strip: open it is an ordinary rule under the last
     row, closed it carries the worst row's colour and is the only
     thing left on screen besides the tab. --- */
  .line {
    position: relative;
    height: 2px;
    background: var(--border);
    transition: background-color 180ms ease-out;
  }
  .ingest-loss-drawer.closed .line {
    background: var(--worst-color);
  }

  /* The pull tab: a 72x24 hit area, mostly invisible, centred on the
     line and hanging just below it in *both* states, so toggling turns
     the arrow over and moves nothing. Below rather than above because
     a row's text is centred too: an open-state tab sitting on the line
     would land on the last row's own words. What it hangs over instead
     is the scene under the drawer, which is what the z-index in the
     header comment is for. */
  .handle {
    position: absolute;
    left: 50%;
    top: 2px;
    width: 72px;
    height: 24px;
    margin: 0;
    padding: 0;
    border: none;
    background: transparent;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    transform: translateX(-50%);
    cursor: pointer;
  }

  /* An arrow both ways, 10x6 -- CSS triangles, no icon font or SVG
     asset for one shape that only ever points two ways. It replaces
     the 48x8 lozenge the owner rejected: that was six times the width
     and, sitting under a 3px line, read as a second bar rather than as
     something to pull. */
  .handle-mark {
    width: 0;
    height: 0;
    margin-top: 2px;
    border-left: 5px solid transparent;
    border-right: 5px solid transparent;
    border-bottom: 6px solid var(--fg-dim);
    transition: border-color 120ms ease-out;
  }
  .handle:hover .handle-mark {
    border-bottom-color: var(--fg);
  }
  /* Closed: the same arrow, turned over to point down at what it will
     open, and taking the line's colour so the two read as one mark.
     No count and no text on the line -- severity is the colour alone,
     and the count is in the tooltip and the aria-label. */
  .ingest-loss-drawer.closed .handle-mark {
    border-bottom: none;
    border-top: 6px solid var(--worst-color);
  }
  .ingest-loss-drawer.closed .handle:hover .handle-mark {
    border-top-color: color-mix(in srgb, var(--worst-color) 65%, var(--fg));
  }

  /* --- Clear all: mirrors .detail exactly, on the opposite edge of
     the top row -- same size, same ink, same box. One control for the
     whole stack, so it renders once, on the first row only; per-row it
     would read as clearing that row. Left, because `details` owns the
     right edge and the owner's ruling was to move it aside rather than
     let the two collide. --- */
  .clear-all {
    position: absolute;
    left: 12px;
    top: 50%;
    transform: translateY(-50%);
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: inherit;
    opacity: 0.75;
    border: 1px solid color-mix(in srgb, currentColor 40%, transparent);
    border-radius: 6px;
    padding: 2px 8px;
    background: transparent;
    cursor: pointer;
  }
  .clear-all:hover {
    opacity: 1;
  }

  @media (prefers-reduced-motion: reduce) {
    .rows-clip,
    .rows,
    .line,
    .handle,
    .handle-mark,
    .clear-all {
      transition: none;
    }
  }

  /* Phone widths: rows wrap by ordinary text flow (no white-space is
     set anywhere above). What does change is the padding. Both controls
     are absolutely positioned over a centred line of text, and at 390px
     there is no longer room for that: rendered at phone width, `details`
     sat on top of its own row's words on every row that carried one.
     So the rule the owner set for Clear all -- a control cuts into the
     banner's space rather than over its text -- is applied to `details`
     too, but only here, where it actually collides. Desktop is wide
     enough and is deliberately left as it was ratified. */
  @media (max-width: 640px) {
    .banner.has-clear {
      padding-left: 84px;
      padding-right: 16px;
    }
    .banner.has-detail {
      padding-right: 84px;
    }
    .banner.has-clear.has-detail {
      padding-left: 84px;
      padding-right: 84px;
    }
  }
</style>
