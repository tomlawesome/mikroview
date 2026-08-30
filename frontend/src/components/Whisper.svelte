<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The whisper (issue #644, ratified round-9/round-22/round-23 "amazing!",
  // built to round-29's the-whole.html #wbar/#wsvg/#wline/#wcursor/#wband/
  // #wfence/#wstat): a quiet full-width strip above the live table carrying
  // the rate curve, drop share, top talker and top port for the last
  // WHISPER_WINDOW_MINUTES -- and it commands the stream. Clicking the
  // curve seeks (autoscroll off, the stat line swaps to that minute); the
  // fence toggle plus two clicks dims everything in LiveTable outside the
  // picked range instead of removing it, matching the-whole.html's own
  // #wband/.outside{opacity} treatment rather than writing to
  // appState.filters -- fencing is a display lens, the same relationship
  // appState.streamHeld already has to Autoscroll-off, never a second
  // filter state alongside FilterBar's.
  //
  // The mockup's own #wstat only ever prints three of the four ratified
  // facts at a time (rolling: rate+drops+talker; seek: rate+drops; fence:
  // drops only) -- an artifact of each branch's demo copy being written
  // separately, not a stated rule about which fact matters when. This
  // shows whichever of the four are actually available for the active
  // window every time, and drops the mockup's redundant "autoscroll:
  // on/off" clause -- the scene bar's own Autoscroll button already is
  // that state's one source of truth (see whisper.svelte.ts's clickMinute).
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

  function keyActivate(e: KeyboardEvent, fn: () => void) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      fn()
    }
  }

  function hm(ms: number): string {
    return formatHM(new Date(ms).toISOString())
  }

  const buckets = $derived(recentBuckets(appState.stats?.timeSeries ?? [], WHISPER_WINDOW_MINUTES))
  const windowStartMs = $derived(buckets.length ? new Date(buckets[0].time).getTime() : null)
  const windowEndMs = $derived(windowStartMs === null ? null : windowStartMs + buckets.length * 60_000)
  const maxTotal = $derived(Math.max(1, ...buckets.map(bucketTotal)))

  // x position (0-1000, the SVG's own viewBox units) for the bucket at
  // index i -- shared by the polyline, the per-minute click targets and
  // the cursor/band so all four always agree on where a minute sits.
  function xForIndex(i: number): number {
    return buckets.length > 1 ? (i / (buckets.length - 1)) * 1000 : 500
  }

  function xForMs(ms: number): number {
    if (windowStartMs === null || buckets.length === 0) return 500
    const idx = Math.min(Math.max(Math.floor((ms - windowStartMs) / 60_000), 0), buckets.length - 1)
    return xForIndex(idx)
  }

  const points = $derived(
    buckets.map((b, i) => `${xForIndex(i)},${30 - (bucketTotal(b) / maxTotal) * 28}`).join(' '),
  )

  // The cursor marks the fence's pending first click while its second is
  // still awaited, or a plain seek -- never both, since fencing and
  // seeking are mutually exclusive per whisper.svelte.ts.
  const cursorMs = $derived(whisperState.fenceFirst ?? whisperState.seekMs)
  const cursorX = $derived(cursorMs === null ? null : xForMs(cursorMs))

  const fenceBand = $derived.by(() => {
    const r = whisperState.fenceRange
    if (!r) return null
    const x1 = xForMs(r.start)
    const x2 = xForMs(r.end - 1)
    return { x: Math.min(x1, x2), width: Math.max(Math.abs(x2 - x1), 6) }
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

  function bucketLabel(b: TimeBucket): string {
    const time = hm(new Date(b.time).getTime())
    const total = bucketTotal(b)
    const share = dropShare([b])
    const drops = share === null ? '' : `, ${Math.round(share * 100)}% drops`
    if (whisperState.fenceOn) {
      const which = whisperState.fenceFirst === null ? 'Fence start' : 'Fence end'
      return `${which}: ${time} — ${total} event${total === 1 ? '' : 's'}${drops}`
    }
    return `Seek to ${time} — ${total} event${total === 1 ? '' : 's'}${drops}`
  }
</script>

<div
  class="whisper"
  aria-label="The last {WHISPER_WINDOW_MINUTES} minutes, whispered — click a point on the line to seek; the fence toggle plus two clicks dims everything outside a time block"
>
  <button
    class="wfence"
    class:on={whisperState.fenceOn}
    aria-pressed={whisperState.fenceOn}
    onclick={() => whisperState.toggleFence()}
    title="Time filter: two clicks on the line fence the live view"
  >
    ⧉ fence
  </button>

  <div class="wbar">
    <svg viewBox="0 0 1000 30" preserveAspectRatio="none" class="wsvg" role="img" aria-label="Event rate curve">
      <polyline class="wline" fill="none" points={points} />
      {#if fenceBand}
        <rect class="wband" x={fenceBand.x} y="0" width={fenceBand.width} height="30"></rect>
      {/if}
      {#if cursorX !== null}
        <line class="wcursor" x1={cursorX} x2={cursorX} y1="0" y2="30"></line>
      {/if}
      {#each buckets as b, i (b.time)}
        {@const w = 1000 / buckets.length}
        <rect
          class="wtick"
          x={xForIndex(i) - w / 2}
          y="0"
          width={w}
          height="30"
          role="button"
          tabindex="0"
          aria-label={bucketLabel(b)}
          onclick={() => whisperState.clickMinute(new Date(b.time).getTime())}
          onkeydown={(e) => keyActivate(e, () => whisperState.clickMinute(new Date(b.time).getTime()))}
        ></rect>
      {/each}
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
</div>

<style>
  .whisper {
    display: flex;
    gap: 14px;
    align-items: center;
    padding: 2px 0 6px;
  }

  .wfence {
    flex: none;
    font: 600 10px var(--font-mono);
    letter-spacing: 0.06em;
    color: var(--fg-dim);
    background: transparent;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    padding: 3px 12px;
    cursor: pointer;
  }

  .wfence:hover {
    color: var(--fg-muted);
    border-color: var(--fg-muted);
  }

  /* var(--now) matches the-whole.html's own wfence.on rule exactly --
     the amber "current position" tint the rest of the app already uses
     for the same idea (see UptimeBadge's own nowline). */
  .wfence.on {
    color: var(--now);
    border-color: var(--now);
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

  .wtick {
    fill: transparent;
    cursor: pointer;
  }

  /* Matches Fall.svelte's own carrier-hit rule (its neighbouring
     clickable SVG shape): outline renders inconsistently on SVG rects
     across browsers, so focus is a stroke instead. */
  .wtick:hover,
  .wtick:focus-visible {
    fill: color-mix(in srgb, var(--accent) 12%, transparent);
    outline: none;
  }

  .wtick:focus-visible {
    stroke: var(--accent);
    stroke-width: 1;
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
