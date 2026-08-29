<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The fall (#616): the ratified hero live view, and the landing page.
  // Bounding record: issue #616's body. Visual reference: round 3's
  // docs/design/concepts/round-3/direction-o-waterfall.html, scene 1
  // only (scenes 2-3 and round 4's water/space ambience were not kept as
  // spec here).
  //
  // One column per boundary. A top strip is "what is arriving this
  // instant"; below a NOW line, time pours downward, newest at the top,
  // bucketed rather than drawn one mark per event -- production traffic
  // runs ~594 events/s (the record's own figure), so a bucketed heat row
  // per time slice is what stays render-bounded regardless of volume,
  // per docs/decisions/ui-framework.md's bundle/perf posture.
  //
  // What "boundary" means here is narrower than the ratified record asks
  // for, and that gap is deliberate and disclosed rather than invented
  // silently -- see lib/fall.svelte.ts's header comment and the PR this
  // shipped on. In short: the record says bands are "boundaries... the
  // same boundaries the coverage model (#392) and topography use,"
  // derived from "the actual configured networks" in a fixed WAN-first/
  // guarded-last order. #392 is explicitly undesigned and its build is
  // deferred to #485 (out of scope for #616), and nothing in mikroview
  // names a network group (LAN/SRV/GUEST/IOT) or a WAN interface. Rather
  // than invent that taxonomy -- a product/config decision, not
  // implementation -- this groups by the one boundary identity mikroview
  // already has honestly: the (chain, inInterface, outInterface) a
  // pushed rule or a live event actually carries, ordered alphabetically
  // (deterministic, not the record's semantic order).
  import { appState } from '../lib/state.svelte'
  import { fallState, boundaryKeyOf, type FallBoundary } from '../lib/fall.svelte'
  import { fetchEventsWindow } from '../lib/api'
  import { formatHM } from '../lib/format'
  import type { ClientEvent } from '../lib/types'

  const SPANS = [
    { id: '15m', label: '15 m', ms: 15 * 60 * 1000 },
    { id: '1h', label: '1 h', ms: 60 * 60 * 1000 },
    { id: '24h', label: '24 h', ms: 24 * 60 * 60 * 1000 },
    { id: '14d', label: '14 d', ms: 14 * 24 * 60 * 60 * 1000 },
  ] as const
  type SpanId = (typeof SPANS)[number]['id']

  const BUCKETS = 48
  const POLL_MS = 5000

  let span = $state<SpanId>('15m')
  let windowEvents = $state<ClientEvent[]>([])
  let windowHasMore = $state(false)
  let windowLoading = $state(true)
  let windowError = $state<string | null>(null)
  let windowStart = $state(0)
  let windowEnd = $state(0)

  const spanMs = $derived(SPANS.find((s) => s.id === span)!.ms)

  async function loadWindow() {
    const end = Date.now()
    const start = end - spanMs
    try {
      const res = await fetchEventsWindow({ since: new Date(start).toISOString(), limit: 5000 })
      const receivedAt = Date.now()
      windowEvents = res.events.map((e) => ({ ...e, receivedAt }))
      windowHasMore = res.hasMore
      windowError = null
    } catch (e) {
      windowError = e instanceof Error ? e.message : String(e)
    } finally {
      windowStart = start
      windowEnd = end
      windowLoading = false
    }
  }

  $effect(() => {
    // Polled, not fetched once: which boundaries exist can change while
    // the fall is open (a router pushes a fresh rule table), and a
    // reload-to-see-it landing page would fail its own "live" claim.
    fallState.refresh()
    const id = setInterval(() => fallState.refresh(), POLL_MS)
    return () => clearInterval(id)
  })

  $effect(() => {
    // Re-run whenever `span` changes, and every POLL_MS besides -- a
    // poll-and-rebucket rather than consuming the WS tail event-by-event,
    // which is the point: aggregation happens over a bounded server
    // query, not over however many thousand events actually arrived.
    void span
    windowLoading = true
    loadWindow()
    const id = setInterval(loadWindow, POLL_MS)
    return () => clearInterval(id)
  })

  function laneOf(action: string): 'accept' | 'drop' | 'nat' | 'other' {
    if (action === 'accept') return 'accept'
    if (action === 'drop' || action === 'reject') return 'drop'
    if (action === 'natted') return 'nat'
    return 'other'
  }

  interface Bucket {
    accept: number
    drop: number
    nat: number
    other: number
    total: number
  }

  interface BandView extends FallBoundary {
    buckets: Bucket[]
    nowBucket: Bucket
    total: number
    maxBucketTotal: number
  }

  function emptyBucket(): Bucket {
    return { accept: 0, drop: 0, nat: 0, other: 0, total: 0 }
  }

  // bands buckets every event in the current window, once, into
  // BUCKETS-per-boundary time slices -- O(events + boundaries*BUCKETS),
  // never one DOM node or one reactive computation per event.
  const bands = $derived.by((): { known: BandView[]; unmatched: BandView | null } => {
    const boundaries = fallState.boundaries
    const byKey = new Map<string, FallBoundary>()
    for (const b of boundaries) byKey.set(b.key, b)

    const bucketsByKey = new Map<string, Bucket[]>()
    const totalByKey = new Map<string, number>()
    const span2 = Math.max(windowEnd - windowStart, 1)
    const mk = () => Array.from({ length: BUCKETS }, emptyBucket)

    for (const b of boundaries) bucketsByKey.set(b.key, mk())
    let unmatchedBuckets: Bucket[] | null = null
    let unmatchedTotal = 0

    for (const e of windowEvents) {
      const key = boundaryKeyOf(e.chain, e.inInterface, e.outInterface)
      let list = bucketsByKey.get(key)
      if (!list) {
        if (!byKey.has(key)) {
          if (!unmatchedBuckets) unmatchedBuckets = mk()
          list = unmatchedBuckets
        } else {
          continue
        }
      }
      const t = new Date(e.time).getTime()
      if (Number.isNaN(t)) continue
      // Newest at the top: bucket 0 is the most recent slice.
      const age = windowEnd - t
      let idx = Math.floor((age / span2) * BUCKETS)
      if (idx < 0) idx = 0
      if (idx >= BUCKETS) idx = BUCKETS - 1
      const bucket = list[idx]
      const lane = laneOf(e.action)
      bucket[lane]++
      bucket.total++
      totalByKey.set(key, (totalByKey.get(key) ?? 0) + 1)
      if (list === unmatchedBuckets) unmatchedTotal++
    }

    function toView(b: FallBoundary, buckets: Bucket[]): BandView {
      const maxBucketTotal = buckets.reduce((m, x) => Math.max(m, x.total), 0)
      return {
        ...b,
        buckets,
        nowBucket: buckets[0] ?? emptyBucket(),
        total: totalByKey.get(b.key) ?? 0,
        maxBucketTotal,
      }
    }

    const known = boundaries.map((b) => toView(b, bucketsByKey.get(b.key)!))
    const unmatched = unmatchedBuckets
      ? toView(
          {
            key: '__unmatched__',
            chain: '',
            inInterface: '',
            outInterface: '',
            label: 'other traffic',
            coverage: 'unknown',
          },
          unmatchedBuckets,
        )
      : null
    if (unmatched) unmatched.total = unmatchedTotal
    return { known, unmatched }
  })

  const allBands = $derived([...bands.known, ...(bands.unmatched ? [bands.unmatched] : [])])
  const darkBands = $derived(allBands.filter((b) => b.coverage === 'dark'))
  const isCalm = $derived(!windowLoading && !windowError && allBands.length > 0 && darkBands.length === 0)

  function openInStream(b: FallBoundary, lane?: 'accept' | 'drop' | 'nat') {
    appState.setFilter('interface', b.inInterface || b.outInterface || '')
    appState.setFilter('chain', b.chain)
    if (lane === 'accept') appState.setFilter('action', 'accept')
    else if (lane === 'drop') appState.setFilter('action', 'drop')
    else if (lane === 'nat') appState.setFilter('action', 'natted')
    appState.view = 'live'
  }

  function bandSummary(b: BandView): string {
    const parts: string[] = [b.label]
    if (b.coverage === 'dark') parts.push('dark -- blank because nothing is logged, not because nothing is sent')
    else if (b.coverage === 'unknown') parts.push('coverage unknown -- no router has pushed its rule table yet')
    if (b.total === 0) parts.push('no traffic in this window')
    else parts.push(`${b.nowBucket.accept} accepted, ${b.nowBucket.drop} dropped, ${b.nowBucket.nat} nat in the most recent slice`)
    parts.push('activate to open in Stream, filtered to this boundary')
    return parts.join('. ')
  }

  function pct(n: number, total: number): number {
    return total > 0 ? (n / total) * 100 : 0
  }
</script>

<div class="fall">
  <div class="fall-bar">
    <h1>The fall</h1>
    <p class="sub">
      a band per boundary · blue = accepted · red = dropped · violet = nat
      {#if windowHasMore}<span class="truncated">— showing the most recent 5,000 events in this window; more exist</span>{/if}
    </p>
    <div class="span-control" role="group" aria-label="Time span">
      {#each SPANS as s (s.id)}
        <button type="button" class="span-btn" class:on={span === s.id} aria-pressed={span === s.id} onclick={() => (span = s.id)}>
          {s.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="chips" aria-live="polite">
    {#if darkBands.length > 0}
      <button type="button" class="chip dark" onclick={() => openInStream(darkBands[0])}>
        {darkBands.length} dark boundar{darkBands.length === 1 ? 'y' : 'ies'} — nothing logged
      </button>
    {:else if isCalm}
      <span class="chip calm">every boundary reads clean</span>
    {/if}
  </div>

  {#if fallState.loading}
    <p class="state-msg">Reading pushed firewall rules…</p>
  {:else if fallState.error}
    <p class="state-msg error" role="alert">Could not read the pushed rule tables: {fallState.error}</p>
  {:else if allBands.length === 0 && !windowLoading}
    <p class="state-msg">
      No boundaries yet — waiting for a router to push its filter rules. See Settings → Run setup… to configure the
      push.
    </p>
  {:else}
    <p class="howto">
      Each column is one boundary: an interface pair a firewall rule or a live event actually carries. Click any
      boundary to open it in Stream, filtered.
    </p>
    <div class="rig scrollbar">
      {#each allBands as b (b.key)}
        <button type="button" class="band" class:dark={b.coverage === 'dark'} onclick={() => openInStream(b)} aria-label={bandSummary(b)}>
          <span class="band-head">
            <span class="band-label">{b.label}</span>
            {#if b.coverage === 'dark'}
              <span class="band-caption bad">dark — no log rule</span>
            {:else if b.coverage === 'observed' && b.total === 0}
              <span class="band-caption quiet">quiet</span>
            {:else if b.coverage === 'observed'}
              <span class="band-caption ok">watch holding ✓</span>
            {/if}
          </span>

          <svg class="spectrum" viewBox="0 0 100 12" preserveAspectRatio="none" aria-hidden="true">
            <rect x="0" y="0" width={pct(b.nowBucket.accept, b.nowBucket.total)} height="12" class="mark accept" />
            <rect
              x={pct(b.nowBucket.accept, b.nowBucket.total)}
              y="0"
              width={pct(b.nowBucket.drop, b.nowBucket.total)}
              height="12"
              class="mark drop"
            />
            <rect
              x={pct(b.nowBucket.accept, b.nowBucket.total) + pct(b.nowBucket.drop, b.nowBucket.total)}
              y="0"
              width={pct(b.nowBucket.nat, b.nowBucket.total)}
              height="12"
              class="mark nat"
            />
          </svg>
          <div class="now-line"><span class="now-dot" aria-hidden="true"></span></div>

          <svg
            class="waterfall"
            viewBox="0 0 100 {BUCKETS * 4}"
            preserveAspectRatio="none"
            aria-hidden="true"
          >
            {#if b.coverage === 'dark'}
              <defs>
                <pattern id="hatch-{b.key}" width="4" height="4" patternTransform="rotate(45)" patternUnits="userSpaceOnUse">
                  <line x1="0" y1="0" x2="0" y2="4" class="hatch-line" />
                </pattern>
              </defs>
              <rect x="0" y="0" width="100" height={BUCKETS * 4} fill="url(#hatch-{b.key})" />
            {:else}
              {#each b.buckets as bucket, i (i)}
                {#if bucket.total > 0}
                  {@const rate = b.maxBucketTotal > 0 ? bucket.total / b.maxBucketTotal : 0}
                  {@const y = i * 4}
                  <rect x="0" {y} width={pct(bucket.accept, bucket.total)} height="4" class="mark accept" opacity={0.25 + rate * 0.75} />
                  <rect
                    x={pct(bucket.accept, bucket.total)}
                    {y}
                    width={pct(bucket.drop, bucket.total)}
                    height="4"
                    class="mark drop"
                    opacity={0.25 + rate * 0.75}
                  />
                  <rect
                    x={pct(bucket.accept, bucket.total) + pct(bucket.drop, bucket.total)}
                    {y}
                    width={pct(bucket.nat, bucket.total)}
                    height="4"
                    class="mark nat"
                    opacity={0.25 + rate * 0.75}
                  />
                {/if}
              {/each}
            {/if}
          </svg>
          {#if b.coverage === 'dark'}
            <span class="sr-only">blank because nothing is logged — not because nothing is sent</span>
          {/if}
        </button>
      {/each}
    </div>
    <p class="window-caption">
      {#if windowStart && windowEnd}{formatHM(new Date(windowStart).toISOString())} – {formatHM(
          new Date(windowEnd).toISOString(),
        )}, newest at the top{/if}
    </p>
  {/if}
</div>

<style>
  .fall {
    padding: 16px 20px 32px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
    height: 100%;
  }

  .fall-bar {
    display: flex;
    align-items: baseline;
    gap: 16px;
    flex-wrap: wrap;
  }
  .fall-bar h1 {
    font-size: 18px;
    font-weight: 650;
    margin: 0;
    color: var(--fg);
  }
  .sub {
    margin: 0;
    font-size: 12px;
    color: var(--fg-dim);
  }
  .truncated {
    color: var(--fg-muted);
  }

  .span-control {
    margin-left: auto;
    display: flex;
    gap: 2px;
    border-bottom: 1px solid var(--border);
    padding-bottom: 4px;
  }
  .span-btn {
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font: inherit;
    font-size: 12px;
    padding: 3px 10px;
    cursor: pointer;
    border-radius: 3px;
  }
  .span-btn:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }
  .span-btn.on {
    color: var(--fg);
    border-bottom: 2px solid var(--now);
    margin-bottom: -6px;
  }

  .chips {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    min-height: 24px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    font-weight: 600;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 12px;
    color: var(--fg-muted);
    background: transparent;
    cursor: default;
  }
  button.chip {
    cursor: pointer;
  }
  button.chip:hover {
    background: var(--bg-hover);
  }
  .chip.dark {
    border-color: color-mix(in srgb, var(--chart-refused) 55%, transparent);
    color: var(--chart-refused);
  }
  .chip.calm {
    color: var(--accept);
  }

  .state-msg {
    color: var(--fg-muted);
    font-size: 13px;
    padding: 20px 0;
  }
  .state-msg.error {
    color: var(--reject);
  }

  .howto {
    margin: 0;
    font-size: 11.5px;
    color: var(--fg-dim);
  }

  .rig {
    display: flex;
    gap: 1px;
    overflow-x: auto;
    background: var(--border);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  .band {
    flex: 1 0 150px;
    min-width: 150px;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border: none;
    padding: 0;
    text-align: left;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  .band:hover {
    background: var(--bg-hover);
  }
  .band:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    z-index: 1;
  }

  .band-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 10px 6px;
    border-bottom: 1px solid var(--border);
  }
  .band-label {
    font-size: 11.5px;
    font-weight: 650;
    color: var(--fg);
    overflow-wrap: break-word;
  }
  .band-caption {
    font-size: 9px;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .band-caption.ok {
    color: var(--accept);
  }
  .band-caption.bad {
    color: var(--chart-refused);
  }
  .band-caption.quiet {
    color: var(--fg-dim);
  }

  .spectrum {
    width: 100%;
    height: 12px;
    display: block;
    background: var(--bg);
  }
  .mark.accept {
    fill: var(--chart-traffic);
  }
  .mark.drop {
    fill: var(--chart-refused);
  }
  .mark.nat {
    fill: var(--fall-nat);
  }

  .now-line {
    height: 1px;
    background: var(--now);
    position: relative;
    margin: 2px 0;
  }
  .now-dot {
    position: absolute;
    right: 6px;
    top: -2px;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--now);
    display: block;
  }
  @media (prefers-reduced-motion: no-preference) {
    .now-dot {
      animation: now-pulse 1.6s ease-in-out infinite;
    }
  }
  @keyframes now-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }

  .waterfall {
    width: 100%;
    flex: 1;
    display: block;
    background: var(--bg);
  }
  .hatch-line {
    stroke: var(--border);
    stroke-width: 1.5;
  }

  .window-caption {
    margin: 0;
    font-size: 10.5px;
    color: var(--fg-dim);
    text-align: right;
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
