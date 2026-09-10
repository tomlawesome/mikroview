<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The decommission card (#460, design round 55), used unchanged by the
  // flat map and the city.
  //
  // Round 55's rule is explicit: "One `card` component for the offer,
  // the ghost and the straggler, on both surfaces; the watchlist row
  // inside a card is the same row the watchlist draws." So this is one
  // component with three faces rather than three components that would
  // drift, and each surface supplies only the placement -- the geometry
  // is the one thing round 55 lets the two surfaces disagree about.
  //
  // The markup is round 49's `.card`, tag for tag: the title row with its
  // pin, `.s` fact lines each carrying the material's own swatch, a
  // `.quote` for anything said in the app's own voice, the `.form` the
  // answer is given in, and an `.acts` row of doors. The leader is drawn
  // here too, because round 55 changed it -- a ghost is faint by design,
  // so the line tying its card to it is accent ink at 1.8px rather than
  // round 49's hairline, and the card sits beside the ghost.
  import type { Placement } from '../lib/cardAnchor'
  import {
    GHOST_INK,
    forceRemoveContract,
    ghostStateOf,
    hoursOfWindow,
    offerHeadline,
    offerQuestion,
    offerSummary,
    receiptSentence,
    retiresAt,
    stragglerHeadline,
    stragglerProvenance,
    stragglerReading,
    stragglerTitle,
    watchlistRowText,
  } from '../lib/decommission'
  import { formatHM, parseGoDurationSeconds } from '../lib/format'
  import type { DecommissionOffer, DecommissionWatch } from '../lib/types'

  let {
    element = $bindable(null),
    kind,
    offer = null,
    watch = null,
    peerName = '',
    nowMs,
    place = null,
    busy = false,
    error = null,
    onaccept,
    ondismiss,
    onforce,
    onclose,
    ontrace,
    onwatchhost,
    onreach,
    onstream,
    onwatchlist,
  }: {
    /** The card's own element, so the surface can measure and place it. */
    element?: HTMLElement | null
    kind: 'offer' | 'ghost' | 'straggler'
    offer?: DecommissionOffer | null
    watch?: DecommissionWatch | null
    peerName?: string
    nowMs: number
    place?: Placement | null
    busy?: boolean
    error?: string | null
    onaccept?: () => void
    ondismiss?: () => void
    onforce?: (reason: string) => void
    onclose?: () => void
    ontrace?: () => void
    onwatchhost?: () => void
    onreach?: () => void
    onstream?: () => void
    onwatchlist?: () => void
  } = $props()

  // The confirm state of force-remove. It is a state of this card rather
  // than a separate dialogue because the warning is about what stays
  // behind, and what stays behind is the watchlist row already on the
  // card -- moving it into a modal would take the evidence away at the
  // moment it is needed.
  let confirming = $state(false)
  // The recorded override's reason. The server refuses an empty one
  // (#385's pattern is a heavy warning *plus* a recorded override), so
  // the confirm state asks for it rather than sending a placeholder that
  // would record only that someone clicked through.
  let reason = $state('')

  const ghost = $derived(ghostStateOf(watch, nowMs))
  const ink = $derived(GHOST_INK[ghost])
  const straggler = $derived(watch?.lastStraggler ?? null)

  const title = $derived.by(() => {
    if (kind === 'offer' && offer) return offerHeadline(offer)
    if (kind === 'straggler' && straggler) return stragglerTitle(straggler, peerName)
    return watch?.name || watch?.interface || ''
  })

  const sub = $derived.by(() => {
    if (kind === 'offer') return offer?.cidr ?? ''
    if (kind === 'straggler') return 'straggler'
    return `ghost · ${watch?.cidr ?? ''}`
  })
</script>

{#if place}
  <!-- Round 55: "The leader is heavier than round 49's. A ghost is faint
       by design, so the line from its card to it is accent ink at 1.8px,
       and the card sits beside the ghost on both surfaces." -->
  <svg class="leader" aria-hidden="true">
    <path d="M{place.from.x} {place.from.y}L{place.to.x} {place.to.y}" stroke="var(--accent)" stroke-opacity="0.75" stroke-width="1.8" fill="none" />
    <circle cx={place.from.x} cy={place.from.y} r="4" fill="var(--accent)" />
  </svg>
{/if}

<div
  bind:this={element}
  data-decomm={kind}
  class="card"
  class:pinned={kind !== 'straggler'}
  class:placed={place !== null}
  style={place ? `left:${place.left}px;top:${place.top}px` : undefined}
  role="dialog"
  tabindex="-1"
  aria-label={kind === 'offer' && offer
    ? `${offerHeadline(offer)} — watch the dead range for stragglers?`
    : kind === 'straggler'
      ? `${title} — a straggler on a retired range`
      : `${title} — retired range ${watch?.cidr ?? ''}, watch ${ghost}`}
>
  <div class="t">
    <span class="n">{title}<small>{sub}</small></span>
    {#if onclose}
      <button class="pin" title="close this card" onclick={() => onclose?.()}>✕</button>
    {/if}
  </div>

  {#if kind === 'offer' && offer}
    <div class="s">{offerSummary(offer)}</div>
    {#if offer.receipt}
      <!-- The receipt is drawn in watch ink because it is what the watch
           would have shown. It is the only argument the offer makes. -->
      <div class="s wt"><i class="sw" style="background:{GHOST_INK.holding};opacity:.9"></i>{receiptSentence(offer.receipt)}</div>
    {:else if offer.decline}
      <!-- An honest refusal rather than a flattering zero: there was
           nothing to replay this against, and saying "would have caught
           0" would read as evidence of quiet. -->
      <div class="s dk"><i class="sw dk"></i>no receipt — {offer.decline.reason}</div>
    {/if}
    <div class="quote">{offerQuestion(offer)}</div>
    {#if error}<p class="d-error">{error}</p>{/if}
    <div class="form">
      <div class="btns">
        <button type="button" class="go" disabled={busy} onclick={() => onaccept?.()}>Watch it</button>
        <button type="button" class="no" disabled={busy} onclick={() => ondismiss?.()}>no — let it go</button>
        <!-- The window is the server's number, not a constant repeated
             here: config engine.decommissionCleanWindow owns it and the
             offer carries it, so a deployment that tuned it does not get
             a card promising six hours. -->
        <span class="who">retires after {Math.round(parseGoDurationSeconds(offer.suggested.cleanWindow) / 3600)} h quiet</span>
      </div>
    </div>
  {/if}

  {#if kind === 'ghost' && watch}
    {#if ghost === 'holding'}
      <div class="s wt">
        <i class="sw dot" style="background:{ink}"></i>watch holding · quiet <b>{hoursOfWindow(watch, nowMs)}</b> · retires by itself at {retiresAt(watch)}
      </div>
    {:else}
      <div class="s al">
        <i class="sw dot" style="background:{ink}"></i>
        {#if watch.trafficCount > 0}
          watch broken · <b>{watch.trafficCount} line{watch.trafficCount === 1 ? '' : 's'}</b> since {formatHM(watch.createdAt)}
        {:else}
          watch broken · nothing logs this range, so quiet cannot be claimed
        {/if}
      </div>
    {/if}
    <div class="s">
      retired {formatHM(watch.createdAt)}
      {#if watch.trafficCount > 0}
        · {watch.trafficCount} straggler{watch.trafficCount === 1 ? '' : 's'} caught{watch.lastTrafficAt ? `, the last ${formatHM(watch.lastTrafficAt)}` : ''}
      {:else}
        · nothing has straggled
      {/if}
    </div>

    {#if !confirming}
      <!-- The watchlist's own row, so what stays behind when the ghost
           leaves the map is visible before it leaves. -->
      <div class="wrow">
        <span class="k">WATCHLIST</span>
        <b>{watch.name || watch.interface} · {watch.cidr}</b> · decommission ·
        <span class={ghost === 'broken' ? 'al' : 'wt'}>{watchlistRowText(watch, nowMs)}</span>
      </div>
    {:else}
      <div class="quote hot">{forceRemoveContract(watch, nowMs)}</div>
      <div class="form">
        <label for="dc-force-why">WHY — RECORDED WITH THE OVERRIDE</label>
        <input id="dc-force-why" bind:value={reason} placeholder="why this has to come off the map now…" />
        {#if error}<p class="d-error">{error}</p>{/if}
        <div class="btns">
          <button type="button" class="go hot" disabled={busy || !reason.trim()} onclick={() => onforce?.(reason.trim())}>Remove the ghost</button>
          <button type="button" class="no" onclick={() => (confirming = false)}>cancel</button>
        </div>
      </div>
    {/if}

    <div class="acts">
      {#if !confirming}
        <button
          type="button"
          class="hot"
          title="take the ghost off the map now — the watch stays in the watchlist until it retires"
          onclick={() => (confirming = true)}>force-remove</button
        >
      {/if}
      <button type="button" onclick={() => onwatchlist?.()}>watchlist ▸</button>
      <button type="button" class="dim" onclick={() => onstream?.()}>stream ▸</button>
    </div>
  {/if}

  {#if kind === 'straggler' && watch && straggler}
    <div class="s al"><i class="sw al"></i>{stragglerHeadline(watch, straggler)}</div>
    {#if stragglerProvenance(watch, straggler)}
      <div class="s">{stragglerProvenance(watch, straggler)}</div>
    {/if}
    {#if stragglerReading(watch, straggler)}
      <div class="s">{stragglerReading(watch, straggler)}</div>
    {/if}
    <div class="wrow">
      <span class="k">WATCHLIST</span>
      <b>{watch.name || watch.interface} · {watch.cidr}</b> · decommission ·
      <span class="al">{watchlistRowText(watch, nowMs)}</span>
    </div>
    <!-- Fable's ruling on round 55's open question 3: the trace comes
         first. The operator's question at this moment is "what is it
         still doing", and the evidence answers it before anything is set
         up; watching the host is the second answer, not the first. -->
    <div class="acts">
      <button type="button" onclick={() => ontrace?.()}>trace ▸</button>
      <button type="button" onclick={() => onwatchhost?.()}>watch this host</button>
      <button type="button" onclick={() => onreach?.()}>reach ▸</button>
      <button type="button" class="dim" onclick={() => onstream?.()}>stream ▸</button>
    </div>
  {/if}
</div>

<style>
  /* The leader and the card are round 49's, ported property for property
     from Topography.svelte's own `.card` block so a decommission card is
     the same object as every other card on the map. Only the two things
     round 55 changed differ: the width (312px, to carry the receipt
     sentence on two lines rather than four) and the leader's weight. */
  .leader {
    position: absolute;
    inset: 0;
    z-index: 2;
    width: 100%;
    height: 100%;
    pointer-events: none;
    overflow: visible;
  }

  .card {
    position: absolute;
    left: 24px;
    bottom: 34px;
    z-index: 3;
    width: 312px;
    padding: 9px 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
    font: 10.5px var(--font-mono);
    color: var(--fg-muted);
  }

  .card.placed {
    bottom: auto;
  }

  .card.pinned {
    border-color: var(--accent);
  }

  .card .t {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .card .t .n {
    flex: 1;
    font: 650 13.5px var(--font-sans);
    color: var(--fg);
  }

  .card .t .n small {
    margin-left: 6px;
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
  }

  .card .pin {
    align-self: flex-start;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1px solid var(--hair-2);
    background: transparent;
    color: var(--fg-dim);
    font: 11px var(--font-sans);
    line-height: 1;
    cursor: pointer;
  }

  .card .pin:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  .card .s {
    margin-top: 3px;
    color: var(--fg-muted);
  }

  .card .s.dk {
    color: var(--fg-dim);
  }

  /* The watch's own ink and the alarm, so a line about the watch is read
     in the colour the ghost is painted in. */
  .card .s.wt {
    color: var(--fg);
  }

  .card .s.al {
    color: var(--alarm);
  }

  .card .sw {
    display: inline-block;
    width: 22px;
    height: 3px;
    border-radius: 2px;
    vertical-align: middle;
    margin-right: 6px;
  }

  .card .sw.dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
  }

  .card .sw.dk {
    background: repeating-linear-gradient(90deg, var(--fg-dim) 0 3px, transparent 3px 6px);
  }

  .card .sw.al {
    background: var(--alarm);
  }

  /* The watchlist row, drawn as the watchlist draws it: a boxed line
     with its column key, so the operator recognises the thing that
     survives force-remove rather than reading a paraphrase of it. */
  .card .wrow {
    margin-top: 6px;
    padding: 4px 8px;
    border: 1px solid var(--hair-2);
    border-radius: 6px;
    color: var(--fg-muted);
  }

  .card .wrow .k {
    margin-right: 6px;
    font-size: 9px;
    letter-spacing: 0.1em;
    color: var(--fg-dim);
  }

  .card .wrow .wt {
    color: var(--marked);
  }

  .card .wrow .al,
  .card .s .al {
    color: var(--alarm);
  }

  .card .quote {
    margin-top: 5px;
    padding: 5px 8px;
    border-left: 2px solid var(--hair-2);
    color: var(--fg);
    font: italic 11px var(--font-sans);
  }

  /* The force-remove warning is the one quote on the map drawn in the
     alarm: it is the only card action that cannot be taken back. */
  .card .quote.hot {
    border-left-color: var(--alarm);
  }

  .card .acts {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
    margin-top: 8px;
    padding-top: 7px;
    border-top: 1px solid var(--hair-2);
  }

  .card .acts button {
    border: none;
    padding: 0;
    background: none;
    color: var(--accent);
    font: 10.5px var(--font-mono);
    cursor: pointer;
  }

  .card .acts button:hover {
    text-decoration: underline;
  }

  .card .acts button.dim {
    color: var(--fg-dim);
  }

  .card .acts button.hot {
    color: var(--alarm);
  }

  .card .form {
    margin-top: 7px;
  }

  .card .form label {
    display: block;
    margin-bottom: 3px;
    font: 600 9px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-dim);
  }

  .card .form input {
    width: 100%;
    padding: 5px 8px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--fg);
    font: 11px var(--font-sans);
    outline: none;
  }

  .card .form input:focus {
    border-color: var(--accent);
  }

  .card .form .btns {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: 7px;
  }

  .card .form .go {
    padding: 4px 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    color: var(--fg);
    font: 600 10.5px var(--font-mono);
    cursor: pointer;
  }

  .card .form .go:hover:not(:disabled) {
    border-color: var(--accent);
  }

  .card .form .go.hot {
    border-color: var(--alarm);
    color: var(--alarm);
  }

  .card .form .go:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .card .form .no {
    border: 0;
    background: none;
    padding: 0;
    color: var(--fg-dim);
    font: 10.5px var(--font-mono);
    cursor: pointer;
  }

  .card .form .who {
    margin-left: auto;
    color: var(--fg-dim);
  }

  .d-error {
    margin: 5px 0 0;
    font-size: 11.5px;
    color: var(--alarm);
  }
</style>
