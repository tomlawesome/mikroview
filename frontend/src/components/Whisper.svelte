<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The whisper (issue #644, ratified round-9/round-22/round-23 "amazing!",
  // built to round-29's the-whole.html #wbar/#wsvg/#wline/#wcursor/#wband/
  // #wstat): a quiet full-width strip above the live table carrying the
  // rate curve, drop share, top talker and top port for the last
  // WHISPER_WINDOW_MINUTES -- and it commands the stream.
  //
  // #717 redrew the fence: the old "⧉ fence" pill (arm, then two clicks)
  // sat centred against the curve's own two-row stack and read as a mode
  // with a label rather than something that belongs to the curve. Now
  // the curve does both jobs itself -- a click seeks (unchanged), a drag
  // sets the fence to the range dragged, and a click outside a drawn
  // band both seeks *and* clears it, which is the whole of "clearing".
  // The curve is also the one focusable control: arrow keys move a
  // cursor along it, Enter marks one fence edge then the other, Escape
  // clears. Fencing still dims LiveTable rather than writing to
  // appState.filters, matching the-whole.html's own #wband/.outside
  // {opacity} treatment -- see lib/whisper.svelte.ts.
  //
  // The mockup's own #wstat only ever prints three of the four ratified
  // facts at a time (rolling: rate+drops+talker; seek: rate+drops; fence:
  // drops only) -- an artifact of each branch's demo copy being written
  // separately, not a stated rule about which fact matters when. This
  // shows whichever of the four are actually available for the active
  // window every time. Round 36 settles where "autoscroll: on/off" went:
  // it leaves the prose and becomes the `following` verb in the hand
  // below, so the state is a control rather than a sentence about one.
  //
  // ---- The hand (rounds 36-38, `#s5` `.spans.hand` + `#hwipe`/`#hcsv`)
  //
  // The whisper commands the stream, and its own seek is what stops the
  // lines following -- so the verbs sit right of its facts rather than
  // anywhere else: `following · pause · group` in the span pills'
  // segmented idiom, then `wipe` and `csv ↓` as quiet pills. Before
  // this they had no home at all: round 30 retired the toolbar that
  // carried them and SceneBar recorded them as gaps, which turned
  // Autoscroll from a toggle into a one-way trapdoor (#749).
  //
  // Following is two-way here, which is that defect drawn shut. A seek
  // or a fence turns it off and the pill reads `follow` in the now ink
  // until it is taken; taking it follows again and clears the cursor
  // and the window -- see whisperState.resumeFollowing.
  import { appState } from '../lib/state.svelte'
  import { whisperState } from '../lib/whisper.svelte'
  import { groupModeState } from '../lib/groupMode.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import { downloadEventsCsv } from '../lib/export'
  import { formatEps, formatHM, formatTime } from '../lib/format'
  import {
    WHISPER_WINDOW_MINUTES,
    bucketAt,
    bucketTotal,
    dropShare,
    eventsBetween,
    recentBuckets,
    topPort,
    topTalker,
  } from '../lib/whisperStats'
  import type { TimeBucket } from '../lib/types'

  // A drag shorter than this (screen pixels) is a sloppy click, not a
  // fence -- click and drag must never be ambiguous.
  const DRAG_THRESHOLD_PX = 6

  function hm(ms: number): string {
    return formatHM(new Date(ms).toISOString())
  }

  const buckets = $derived(recentBuckets(appState.stats?.timeSeries ?? [], WHISPER_WINDOW_MINUTES))
  const windowStartMs = $derived(buckets.length ? new Date(buckets[0].time).getTime() : null)
  const windowEndMs = $derived(windowStartMs === null ? null : windowStartMs + buckets.length * 60_000)
  const maxTotal = $derived(Math.max(1, ...buckets.map(bucketTotal)))

  // x position (0-1000, the SVG's own viewBox units) for the bucket at
  // index i -- shared by the polyline, the cursor/band and the pointer
  // math below so all of them always agree on where a minute sits.
  function xForIndex(i: number): number {
    return buckets.length > 1 ? (i / (buckets.length - 1)) * 1000 : 500
  }

  function xForMs(ms: number): number {
    if (windowStartMs === null || buckets.length === 0) return 500
    const idx = Math.min(Math.max(Math.floor((ms - windowStartMs) / 60_000), 0), buckets.length - 1)
    return xForIndex(idx)
  }

  function msAtIndex(i: number): number | null {
    const b = buckets[i]
    return b ? new Date(b.time).getTime() : null
  }

  const points = $derived(
    buckets.map((b, i) => `${xForIndex(i)},${30 - (bucketTotal(b) / maxTotal) * 28}`).join(' '),
  )

  let svgEl = $state<SVGSVGElement | undefined>()

  // The one focus/hover position on the curve, in bucket-index terms --
  // shared by the mouse (both a plain click and a drag move it) and the
  // keyboard (every arrow key moves it), so one piece of state drives
  // the cursor line and the drag/keyboard fence preview alike.
  let kbIndex = $state<number | null>(null)
  // The keyboard's own pending fence edge -- set by the first Enter,
  // consumed by the second. Never touched by the mouse.
  let kbAnchorIndex = $state<number | null>(null)
  // The mouse's own pending fence edge, from pointerdown -- null again
  // once the gesture ends, whether it turned into a click or a drag.
  let dragAnchorIndex = $state<number | null>(null)
  let dragging = $state(false)
  let announcement = $state('')

  // Plain (non-reactive) bookkeeping for the gesture in progress --
  // nothing on screen reads these directly.
  let activePointerId: number | null = null
  let downClientX = 0
  let downClientY = 0

  function indexFromClientX(clientX: number): number {
    const n = buckets.length
    if (n === 0 || !svgEl) return 0
    const rect = svgEl.getBoundingClientRect()
    const frac = rect.width > 0 ? Math.min(1, Math.max(0, (clientX - rect.left) / rect.width)) : 0
    return n > 1 ? Math.round(frac * (n - 1)) : 0
  }

  function onPointerDown(event: PointerEvent) {
    if (buckets.length === 0 || event.button !== 0) return
    svgEl?.focus()
    downClientX = event.clientX
    downClientY = event.clientY
    activePointerId = event.pointerId
    dragAnchorIndex = indexFromClientX(event.clientX)
    dragging = false
    kbAnchorIndex = null
    kbIndex = dragAnchorIndex
    svgEl?.setPointerCapture?.(event.pointerId)
  }

  function onPointerMove(event: PointerEvent) {
    if (activePointerId === null || event.pointerId !== activePointerId || dragAnchorIndex === null) return
    const dx = event.clientX - downClientX
    const dy = event.clientY - downClientY
    if (!dragging && Math.hypot(dx, dy) >= DRAG_THRESHOLD_PX) dragging = true
    if (dragging) kbIndex = indexFromClientX(event.clientX)
  }

  function onPointerUp(event: PointerEvent) {
    if (activePointerId === null || event.pointerId !== activePointerId || dragAnchorIndex === null) return
    const endIndex = indexFromClientX(event.clientX)
    if (dragging) {
      const anchorMs = msAtIndex(dragAnchorIndex)
      const endMs = msAtIndex(endIndex)
      if (anchorMs !== null && endMs !== null) {
        const lo = Math.min(anchorMs, endMs)
        const hi = Math.max(anchorMs, endMs)
        whisperState.setFenceRange(lo, hi + 60_000)
        announcement = `Fenced ${hm(lo)}–${hm(hi)}`
      }
      kbIndex = endIndex
    } else {
      const ms = msAtIndex(dragAnchorIndex)
      if (ms !== null) whisperState.seek(ms)
      kbIndex = dragAnchorIndex
    }
    resetDrag()
  }

  function resetDrag() {
    dragging = false
    dragAnchorIndex = null
    activePointerId = null
  }

  function markEdge(idx: number) {
    const ms = msAtIndex(idx)
    if (ms === null) return
    if (kbAnchorIndex === null) {
      kbAnchorIndex = idx
      announcement = `Fence start marked at ${hm(ms)} — move and press Enter again to close it`
      return
    }
    const anchorMs = msAtIndex(kbAnchorIndex)
    kbAnchorIndex = null
    if (anchorMs === null) return
    const lo = Math.min(anchorMs, ms)
    const hi = Math.max(anchorMs, ms)
    whisperState.setFenceRange(lo, hi + 60_000)
    announcement = `Fenced ${hm(lo)}–${hm(hi)}`
  }

  function onKeyDown(event: KeyboardEvent) {
    const n = buckets.length
    if (n === 0) return
    const from = kbIndex ?? n - 1
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        kbIndex = Math.max(0, from - 1)
        break
      case 'ArrowRight':
      case 'ArrowUp':
        kbIndex = Math.min(n - 1, from + 1)
        break
      case 'Home':
        kbIndex = 0
        break
      case 'End':
        kbIndex = n - 1
        break
      case 'Enter':
        markEdge(from)
        break
      case 'Escape':
        if (kbAnchorIndex !== null) {
          kbAnchorIndex = null
          announcement = 'Fence start cleared'
        } else if (whisperState.fenceRange) {
          whisperState.clearFence()
          announcement = 'Fence cleared'
        }
        break
      default:
        return
    }
    event.preventDefault()
  }

  // The cursor marks whichever position is currently live -- the
  // pointer while dragging, the keyboard's own focus otherwise, or a
  // plain seek's single minute. Never more than one applies at a time.
  const cursorMs = $derived(kbIndex !== null ? msAtIndex(kbIndex) : whisperState.seekMs)
  const cursorX = $derived(cursorMs === null ? null : xForMs(cursorMs))

  // The range still being dragged or keyed in -- shown the same way the
  // closed fence is, so there is no visual jump when the gesture ends.
  const previewRange = $derived.by(() => {
    if (dragging && dragAnchorIndex !== null && kbIndex !== null) {
      const s = msAtIndex(Math.min(dragAnchorIndex, kbIndex))
      const e = msAtIndex(Math.max(dragAnchorIndex, kbIndex))
      return s === null || e === null ? null : { start: s, end: e + 60_000 }
    }
    if (kbAnchorIndex !== null && kbIndex !== null) {
      const s = msAtIndex(Math.min(kbAnchorIndex, kbIndex))
      const e = msAtIndex(Math.max(kbAnchorIndex, kbIndex))
      return s === null || e === null ? null : { start: s, end: e + 60_000 }
    }
    return null
  })

  const fenceBand = $derived.by(() => {
    const r = previewRange ?? whisperState.fenceRange
    if (!r) return null
    const x1 = xForMs(r.start)
    const x2 = xForMs(r.end - 1)
    return { x: Math.min(x1, x2), width: Math.max(Math.abs(x2 - x1), 6) }
  })

  // What the curve announces as its value on every move -- read by a
  // screen reader the way a native slider's value is, without a live
  // region talking over the page for something that isn't a mode
  // change (see kbValueText's own doc below for the mode changes that
  // do get a live announcement instead).
  const kbValueText = $derived.by(() => {
    const n = buckets.length
    if (n === 0) return 'No minutes to fence yet'
    const idx = kbIndex ?? n - 1
    const b = buckets[idx]
    const time = hm(new Date(b.time).getTime())
    const total = bucketTotal(b)
    const share = dropShare([b])
    const drops = share === null ? '' : `, ${Math.round(share * 100)}% drops`
    const which = kbAnchorIndex === null ? 'Fence start candidate' : 'Fence end candidate'
    return `${which}: ${time} — ${total} event${total === 1 ? '' : 's'}${drops}`
  })

  type StatWindow =
    | { kind: 'fence'; start: number; end: number }
    | { kind: 'seek'; bucket: TimeBucket; ms: number }
    | { kind: 'rolling' }

  // The stat line's own window: the fence when one is drawn, else a
  // plain seek's single minute, else the whole rolling span -- always
  // exactly one of these three.
  const statWindow = $derived.by((): StatWindow => {
    if (whisperState.fenceRange) return { kind: 'fence', ...whisperState.fenceRange }
    if (whisperState.seekMs !== null) {
      const b = bucketAt(buckets, whisperState.seekMs)
      if (b) return { kind: 'seek', bucket: b, ms: whisperState.seekMs }
    }
    return { kind: 'rolling' }
  })

  const statDropShare = $derived.by(() => {
    if (statWindow.kind === 'fence') {
      const inRange = buckets.filter((b) => {
        const t = new Date(b.time).getTime()
        return t >= statWindow.start && t < statWindow.end
      })
      return dropShare(inRange)
    }
    if (statWindow.kind === 'seek') return dropShare([statWindow.bucket])
    return dropShare(buckets)
  })

  // [start, end) for whichever events belong to the active window --
  // null only when there is no rolling window yet (stats haven't loaded).
  const statRangeMs = $derived.by((): [number, number] | null => {
    if (statWindow.kind === 'fence') return [statWindow.start, statWindow.end]
    if (statWindow.kind === 'seek') return [statWindow.ms, statWindow.ms + 60_000]
    if (windowStartMs === null || windowEndMs === null) return null
    return [windowStartMs, windowEndMs]
  })

  const statTalker = $derived(statRangeMs ? topTalker(eventsBetween(appState.events, ...statRangeMs)) : undefined)
  const statPort = $derived(statRangeMs ? topPort(eventsBetween(appState.events, ...statRangeMs)) : undefined)

  const statRate = $derived.by((): number | null => {
    if (statWindow.kind === 'seek') return bucketTotal(statWindow.bucket) / 60
    if (statWindow.kind === 'rolling') return appState.stats?.eventsPerSecond ?? null
    return null // the fence reports a range, not an instantaneous rate
  })

  const statReady = $derived(appState.stats !== null)

  // ---- the hand ----

  // What `csv ↓` would give, said on the control itself rather than
  // discovered by downloading it. The same set the table is drawn from
  // (the buffer this screen holds, under the filter that is on), so the
  // figure and the rows are one thing.
  const heldEvents = $derived(appState.filteredEvents)

  function toggleFollow() {
    if (appState.autoscroll) whisperState.stopFollowing()
    else whisperState.resumeFollowing()
  }

  // hh:mm:ss for a moment the interface is reporting back ("held at",
  // "wiped") -- the same clock the time column reads in, without its
  // milliseconds, which are precision nobody needs to hear a hold in.
  function clock(ms: number): string {
    return formatTime(new Date(ms).toISOString())
  }
</script>

<div
  class="whisper"
  aria-label="The last {WHISPER_WINDOW_MINUTES} minutes, whispered — click the curve to seek, drag to fence a time range"
>
  <div class="wbar">
    <svg
      bind:this={svgEl}
      viewBox="0 0 1000 30"
      preserveAspectRatio="none"
      class="wsvg"
      role="slider"
      tabindex="0"
      aria-label="Event rate curve — click to seek, drag to fence a range; arrow keys move the cursor, Enter marks a fence edge, Escape clears the fence"
      aria-orientation="horizontal"
      aria-valuemin="0"
      aria-valuemax={Math.max(0, buckets.length - 1)}
      aria-valuenow={kbIndex ?? Math.max(0, buckets.length - 1)}
      aria-valuetext={kbValueText}
      onpointerdown={onPointerDown}
      onpointermove={onPointerMove}
      onpointerup={onPointerUp}
      onpointercancel={resetDrag}
      onlostpointercapture={resetDrag}
      onkeydown={onKeyDown}
    >
      <polyline class="wline" fill="none" points={points} />
      {#if fenceBand}
        <rect class="wband" x={fenceBand.x} y="0" width={fenceBand.width} height="30"></rect>
      {/if}
      {#if cursorX !== null}
        <line class="wcursor" x1={cursorX} x2={cursorX} y1="0" y2="30"></line>
      {/if}
    </svg>
    <div class="wtimes">
      <span class="wt0">{windowStartMs !== null ? hm(windowStartMs) : ''}</span>
      <span class="wt1">{windowEndMs !== null ? `${hm(windowEndMs)} · now` : ''}</span>
    </div>
  </div>

  <span class="wstat">
    {#if !statReady}
      <span class="dim">gathering the last {WHISPER_WINDOW_MINUTES} minutes…</span>
    {:else}
      <!-- The two facts the hand creates, ahead of the window's own
           (round 36: `paused` holds the lines and the stat counts what
           waits; a wipe says when it happened). Neither is recoverable
           from the buffer afterwards, which is why appState records the
           moment -- and the wipe clause stands only while the lines are
           still gone, since once they are back "wiped at" is no longer
           what is true of what you are looking at. -->
      {#if appState.paused && appState.pausedAt !== null}
        <b class="k">held at {clock(appState.pausedAt)}</b>
        {' · '}<b class="k">{appState.pendingCount}</b>{' arrived since, waiting · '}
      {:else if appState.wipedAt !== null && appState.events.length === 0}
        <b class="k">wiped {clock(appState.wipedAt)}</b>{' · '}
      {/if}
      <!-- The rate stands down while the stream is held: "34/s now" is a
           reading off a stream the reader has just stopped watching, and
           the held clause above has already said what is happening
           instead. The drawing does the same (round 36's stat drops its
           first clause when paused). The separator rides inside each
           branch rather than after them, so a branch that says nothing
           does not leave a leading "·" behind. -->
      {#if statWindow.kind === 'fence'}
        <b class="k">fenced {hm(statWindow.start)}–{hm(statWindow.end)}</b>{' · '}
      {:else if statRate !== null && !appState.paused}
        <b class="k">{formatEps(statRate)}/s</b>
        {statWindow.kind === 'seek' ? hm(statWindow.ms) : 'now'}{' · '}
      {/if}
      {#if statDropShare === null}
        <span class="dim">no drops recorded yet</span>
      {:else}
        <b class="r">drops {Math.round(statDropShare * 100)}%</b>{statWindow.kind === 'fence' ? ' in the fence' : ''}
      {/if}
      {#if statTalker}
        {' · top talker '}<b class="k">{statTalker}</b>
      {/if}
      {#if statPort}
        {' · top port '}<b class="k">{statPort}</b>
      {/if}
    {/if}
  </span>

  <!-- The hand. Real buttons, not the mockup's role="button" spans: each
       one is a toggle a keyboard has to reach and a screen reader has to
       be able to read the state of, which aria-pressed on a real button
       gives for nothing. The pills' own look is ported from the drawing
       (`.spans`/`.spans .on`, `.hand .on`, `#hpause.on`, `#hfollow:not(.on)`). -->
  <span class="spans hand" role="group" aria-label="The stream's hand">
    <button
      type="button"
      class="hand-btn follow"
      class:on={appState.autoscroll}
      aria-pressed={appState.autoscroll}
      title={appState.autoscroll
        ? 'Follow the newest line as it arrives — off while you are reading back'
        : 'Following is off — the table stays put where you left it. Follow the newest line again, clearing the cursor and the window'}
      onclick={toggleFollow}>{appState.autoscroll ? 'following' : 'follow'}</button
    >
    <button
      type="button"
      class="hand-btn held"
      class:on={appState.paused}
      aria-pressed={appState.paused}
      title="Hold the lines where they are; what arrives waits, counted"
      onclick={() => appState.togglePause()}>{appState.paused ? 'paused' : 'pause'}</button
    >
    <!-- Absent at phone width, where it would do nothing: below 700px
         LiveTable renders EventCardMobile, which has no grouped-row path
         at all. Carried over from the retired scene-bar control, which
         was hidden there for the same reason -- a toggle that changes
         nothing is worse than one that is not offered. -->
    {#if !viewportState.isMobile}
      <button
        type="button"
        class="hand-btn"
        class:on={groupModeState.enabled}
        aria-pressed={groupModeState.enabled}
        title="Fold repeats of the same line into one, with a count"
        onclick={() => groupModeState.toggle()}>group</button
      >
    {/if}
  </span>

  <button
    type="button"
    class="wpill"
    title="Wipe the lines held on this screen — the server keeps its own"
    onclick={() => appState.clearBuffer()}>wipe</button
  >
  <!-- Says what it gives before it gives it, and is absent as a working
       control when there is nothing to give -- disabled rather than
       hidden, so the way to an export never silently stops existing. -->
  <button
    type="button"
    class="wpill"
    disabled={heldEvents.length === 0}
    title="The lines held on this screen, as a CSV file — {heldEvents.length} rows, every column"
    onclick={() => downloadEventsCsv(heldEvents)}>csv ↓</button
  >

  <p class="sr-only" role="status">{announcement}</p>
</div>

<style>
  .whisper {
    display: flex;
    gap: 14px;
    align-items: center;
    padding: 2px 0 6px;
  }

  .wbar {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .wsvg {
    display: block;
    width: 100%;
    height: 30px;
    cursor: crosshair;
    outline: none;
  }

  .wsvg:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: 3px;
  }

  .wline {
    stroke: var(--accent);
    stroke-width: 1.3;
    opacity: 0.75;
  }

  .wband {
    fill: color-mix(in srgb, var(--now) 13%, transparent);
    stroke: color-mix(in srgb, var(--now) 50%, transparent);
    stroke-width: 0.5;
  }

  .wcursor {
    stroke: var(--now);
    stroke-width: 1.4;
  }

  .wtimes {
    display: flex;
    justify-content: space-between;
    margin-top: 2px;
    font: 9px var(--font-mono);
    color: var(--fg-dim);
  }

  .wstat {
    flex: none;
    font: 11px var(--font-mono);
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .wstat .k {
    color: var(--fg-muted);
    font-weight: 600;
  }

  .wstat .r {
    color: var(--fall-drop);
    font-weight: 600;
  }

  .wstat .dim {
    color: var(--fg-dim);
  }

  /* Round 30's `.spans` idiom, which the span pills on the filter line
     already wear -- the hand borrows it rather than inventing a third
     kind of toggle for the same page. */
  .spans {
    flex: none;
    display: flex;
    gap: 2px;
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
  }

  .hand-btn {
    padding: 3px 9px;
    border-radius: 6px;
    /* Transparent at rest rather than absent, so a pill lighting up
       does not shove its neighbours a pixel sideways. */
    border: 1px solid transparent;
    background: transparent;
    font: inherit;
    color: inherit;
    cursor: pointer;
    white-space: nowrap;
  }

  .hand-btn:hover {
    color: var(--fg);
  }

  .hand-btn.on {
    color: var(--accent);
    background: var(--bg-elevated);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }

  /* A hold is attention, not alarm, so it takes the now ink -- the same
     ink the cursor and the fence band are already drawn in. */
  .hand-btn.held.on {
    color: var(--now);
    background: var(--bg-elevated);
    border-color: color-mix(in srgb, var(--now) 50%, transparent);
  }

  /* "Held is a state worth noticing: the way back wears the now ink
     until it is taken" (round 36). This is the one control here that is
     louder switched *off* than on, and deliberately so -- a stream that
     has quietly stopped following is exactly what #749 was. */
  .hand-btn.follow:not(.on) {
    color: var(--now);
    border-color: color-mix(in srgb, var(--now) 50%, transparent);
  }

  .hand-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* `wipe` and `csv ↓`: quiet pills, from the drawing's own `.wfence`. */
  .wpill {
    flex: none;
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: var(--fg-dim);
    background: transparent;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    padding: 3px 12px;
    cursor: pointer;
    white-space: nowrap;
  }

  .wpill:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .wpill:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  /* Clipped rather than hidden -- display:none would remove the live
     region from the accessibility tree and silence every announcement. */
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

  @media (max-width: 900px) {
    .whisper {
      flex-wrap: wrap;
    }

    .wstat {
      flex-basis: 100%;
      white-space: normal;
    }
  }
</style>
