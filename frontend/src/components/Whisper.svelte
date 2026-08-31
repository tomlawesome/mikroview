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
  // window every time, and drops the mockup's redundant "autoscroll:
  // on/off" clause -- the scene bar's own Autoscroll button already is
  // that state's one source of truth (see whisper.svelte.ts's seek).
  import { appState } from '../lib/state.svelte'
  import { whisperState } from '../lib/whisper.svelte'
  import { formatEps, formatHM } from '../lib/format'
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
      {#if statWindow.kind === 'fence'}
        <b class="k">fenced {hm(statWindow.start)}–{hm(statWindow.end)}</b>
      {:else if statRate !== null}
        <b class="k">{formatEps(statRate)}/s</b>
        {statWindow.kind === 'seek' ? hm(statWindow.ms) : 'now'}
      {/if}
      {' · '}
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
