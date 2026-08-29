<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The fall (#616): the ratified hero live view, and the landing page.
  // Bounding record: issue #616's body, amended twice by Fable's
  // 2026-08-29 review on #616 (deviation review, then a rendering-spec
  // follow-up after the first cut's carriers read as solid blocks
  // rather than a waterfall). Visual reference:
  // docs/design/concepts/round-3/direction-o-waterfall.html, scene 1
  // only (scenes 2-3 and round 4's water/space ambience were not kept as
  // spec here) -- and docs/design/concepts/round-3/shots/o-s1.png for
  // the actual pixel comparison.
  //
  // One column per boundary. Above a NOW line: the live spectrum, one
  // peak per carrier positioned by port (amendment: "position within a
  // band = port"). Below it: time pours downward, newest at the top --
  // a carrier per (boundary, port) drawn as a THIN dash column (2-4 CSS
  // px, not a fill spanning the band), a short dash only in a time
  // bucket that actually had traffic so the gaps between dashes carry
  // the cadence. Bucket count scales with the SPAN (finer at 15m, so a
  // ~64s knock still reads as distinct dashes) rather than staying fixed
  // regardless of window length. Capped at the 8 most recently active
  // carriers per band, with a "+n quieter" affordance for the rest --
  // the cap is what keeps this render-bounded at ~594 events/s, not
  // flattening carriers into an aggregate lane heat (that first attempt
  // was reviewed and rejected: "a band that has always been black
  // growing a carrier is the thing this display exists to show").
  //
  // What "boundary" means here is narrower than the ratified record asks
  // for, and that gap is deliberate and disclosed rather than invented
  // silently -- see lib/fall.svelte.ts's header comment and the PR this
  // shipped on.
  import { appState } from '../lib/state.svelte'
  import { fallState, boundaryKeyOf, type FallBoundary } from '../lib/fall.svelte'
  import { fetchEventsWindow } from '../lib/api'
  import { formatHM } from '../lib/format'
  import { lookupPort } from '../lib/commonPorts'
  import type { ClientEvent } from '../lib/types'

  // buckets: the time resolution at this span. Fixed at 48 regardless of
  // span (the first cut) made a 64s knocker (the record's own cadence
  // example) invisible at spans wider than ~15m and coarse even there --
  // 60 buckets at 15m is 15s/bucket, matching the review's own ask.
  const SPANS = [
    { id: '15m', label: '15 m', ms: 15 * 60 * 1000, buckets: 60 },
    { id: '1h', label: '1 h', ms: 60 * 60 * 1000, buckets: 60 },
    { id: '24h', label: '24 h', ms: 24 * 60 * 60 * 1000, buckets: 72 },
    { id: '14d', label: '14 d', ms: 14 * 24 * 60 * 60 * 1000, buckets: 56 },
  ] as const
  type SpanId = (typeof SPANS)[number]['id']

  const POLL_MS = 5000
  // The review's own cap: the 8 most recently active carriers per band
  // render as individual marks; the rest fold into a "+n quieter"
  // affordance instead of growing the DOM per port.
  const MAX_CARRIERS = 8
  // The visible dash's width and the (wider, invisible) click/focus
  // target's width, both in the waterfall SVG's 0-100 coordinate space.
  const DASH_WIDTH = 3
  const HIT_WIDTH = 10

  let span = $state<SpanId>('15m')
  let windowEvents = $state<ClientEvent[]>([])
  let windowHasMore = $state(false)
  let windowLoading = $state(true)
  let windowError = $state<string | null>(null)
  let windowStart = $state(0)
  let windowEnd = $state(0)

  const spanDef = $derived(SPANS.find((s) => s.id === span)!)
  const spanMs = $derived(spanDef.ms)
  const buckets = $derived(spanDef.buckets)

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
    // Re-run whenever `span` (and so bucket count) changes, and every
    // POLL_MS besides -- a poll-and-rebucket rather than consuming the
    // WS tail event-by-event, which is the point: aggregation happens
    // over a bounded server query, not over however many thousand
    // events actually arrived.
    void span
    windowLoading = true
    loadWindow()
    const id = setInterval(loadWindow, POLL_MS)
    return () => clearInterval(id)
  })

  type Lane = 'accept' | 'drop' | 'nat' | 'other'

  function laneOf(action: string): Lane {
    if (action === 'accept') return 'accept'
    if (action === 'drop' || action === 'reject') return 'drop'
    if (action === 'natted') return 'nat'
    return 'other'
  }

  // The port a carrier is keyed by: the destination port is "the
  // service", which is what the record's dial-position metaphor reads
  // as port identity. 0 stands for "no port" (ICMP and the like) --
  // still a real, honestly-labelled carrier, not dropped from the view.
  function portOf(e: ClientEvent): number {
    if (typeof e.dstPort === 'number' && e.dstPort > 0) return e.dstPort
    if (typeof e.srcPort === 'number' && e.srcPort > 0) return e.srcPort
    return 0
  }

  function portLabel(port: number): string {
    if (port === 0) return 'no port'
    const known = lookupPort(port)?.[0]?.name
    return known ? `:${port} ${known}` : `:${port}`
  }

  interface Bucket {
    accept: number
    drop: number
    nat: number
    other: number
    total: number
  }

  function emptyBucket(): Bucket {
    return { accept: 0, drop: 0, nat: 0, other: 0, total: 0 }
  }

  function dominantLane(b: Bucket): Lane {
    let best: Lane = 'other'
    let bestN = -1
    for (const lane of ['accept', 'drop', 'nat', 'other'] as const) {
      if (b[lane] > bestN) {
        best = lane
        bestN = b[lane]
      }
    }
    return best
  }

  interface Carrier {
    port: number
    buckets: Bucket[] // index 0 = most recent
    total: number
    // Smallest bucket index with any activity -- 0 means "active right
    // now", buckets.length means "never in this window". Used only to
    // rank which carriers are the 8 most recently active.
    mostRecentActive: number
    maxBucketTotal: number
    x: number // this carrier's position (0-100) in the shared band coordinate space -- "position = port"
  }

  interface BandView extends FallBoundary {
    carriers: Carrier[]
    quieterCount: number
    total: number
    nowTotal: Bucket
    nowMax: number // the busiest carrier's current-instant rate, for spectrum scaling
  }

  // portX maps a set of carriers' ports onto [10, 90] linearly by port
  // number -- "position within a band = port" -- with a single carrier
  // centred rather than dividing by zero.
  function assignX(all: { port: number }[]): number[] {
    if (all.length === 0) return []
    if (all.length === 1) return [50]
    const ports = all.map((c) => c.port)
    const lo = Math.min(...ports)
    const hi = Math.max(...ports)
    if (lo === hi) return all.map(() => 50)
    return ports.map((p) => 10 + ((p - lo) / (hi - lo)) * 80)
  }

  // bands buckets every event in the current window, once, into
  // buckets-per-(boundary, port) time slices -- O(events + distinct
  // (boundary, port) pairs * buckets), never one DOM node or one
  // reactive computation per event. Only the capped carrier set per
  // band is ever rendered; the full per-port map stays a plain object
  // computed once per re-derive.
  const bandsData = $derived.by((): { known: BandView[]; unmatched: BandView | null } => {
    const boundaries = fallState.boundaries
    const byKey = new Map<string, FallBoundary>()
    for (const b of boundaries) byKey.set(b.key, b)

    const bucketCount = buckets
    type PortMap = Map<number, Bucket[]>
    const portsByKey = new Map<string, PortMap>()
    const totalByKey = new Map<string, number>()
    const nowByKey = new Map<string, Bucket>()
    const span2 = Math.max(windowEnd - windowStart, 1)
    const mkBuckets = () => Array.from({ length: bucketCount }, emptyBucket)

    for (const b of boundaries) {
      portsByKey.set(b.key, new Map())
      nowByKey.set(b.key, emptyBucket())
    }
    let unmatchedPorts: PortMap | null = null
    let unmatchedTotal = 0
    let unmatchedNow = emptyBucket()

    for (const e of windowEvents) {
      const key = boundaryKeyOf(e.chain, e.inInterface, e.outInterface)
      let ports = portsByKey.get(key)
      let isUnmatched = false
      if (!ports) {
        if (!byKey.has(key)) {
          if (!unmatchedPorts) unmatchedPorts = new Map()
          ports = unmatchedPorts
          isUnmatched = true
        } else {
          continue
        }
      }
      const t = new Date(e.time).getTime()
      if (Number.isNaN(t)) continue
      // Newest at the top: bucket 0 is the most recent slice.
      const age = windowEnd - t
      let idx = Math.floor((age / span2) * bucketCount)
      if (idx < 0) idx = 0
      if (idx >= bucketCount) idx = bucketCount - 1

      const port = portOf(e)
      let list = ports.get(port)
      if (!list) {
        list = mkBuckets()
        ports.set(port, list)
      }
      const lane = laneOf(e.action)
      list[idx][lane]++
      list[idx].total++

      if (isUnmatched) {
        unmatchedTotal++
        if (idx === 0) {
          unmatchedNow[lane]++
          unmatchedNow.total++
        }
      } else {
        totalByKey.set(key, (totalByKey.get(key) ?? 0) + 1)
        if (idx === 0) {
          const now = nowByKey.get(key)!
          now[lane]++
          now.total++
        }
      }
    }

    function carriersFor(ports: PortMap): { carriers: Carrier[]; quieterCount: number } {
      const all: Omit<Carrier, 'x'>[] = []
      for (const [port, bucketList] of ports) {
        let total = 0
        let mostRecentActive: number = bucketCount
        let maxBucketTotal = 0
        for (let i = 0; i < bucketList.length; i++) {
          total += bucketList[i].total
          if (bucketList[i].total > 0 && mostRecentActive === bucketCount) mostRecentActive = i
          maxBucketTotal = Math.max(maxBucketTotal, bucketList[i].total)
        }
        all.push({ port, buckets: bucketList, total, mostRecentActive, maxBucketTotal })
      }
      all.sort((a, b) => a.mostRecentActive - b.mostRecentActive || b.total - a.total)
      const shown = all.slice(0, MAX_CARRIERS)
      // Positioned by port among the carriers actually shown -- adding
      // a quieter port back into view later can reflow the others,
      // which is honest (position is always relative to what's on
      // screen) rather than reserving space for ports nobody sees yet.
      const xs = assignX(shown)
      const withX: Carrier[] = shown.map((c, i) => ({ ...c, x: xs[i] }))
      return { carriers: withX, quieterCount: Math.max(0, all.length - MAX_CARRIERS) }
    }

    function toView(b: FallBoundary, ports: PortMap, total: number, now: Bucket): BandView {
      const { carriers, quieterCount } = carriersFor(ports)
      const nowMax = Math.max(1, ...carriers.map((c) => c.buckets[0]?.total ?? 0))
      return { ...b, carriers, quieterCount, total, nowTotal: now, nowMax }
    }

    const known = boundaries.map((b) => toView(b, portsByKey.get(b.key)!, totalByKey.get(b.key) ?? 0, nowByKey.get(b.key)!))
    const unmatched = unmatchedPorts
      ? toView(
          {
            key: '__unmatched__',
            chain: '',
            inInterface: '',
            outInterface: '',
            srcAddressList: '',
            label: 'other traffic',
            coverage: 'unknown',
          },
          unmatchedPorts,
          unmatchedTotal,
          unmatchedNow,
        )
      : null
    return { known, unmatched }
  })

  const allBands = $derived([...bandsData.known, ...(bandsData.unmatched ? [bandsData.unmatched] : [])])
  const darkBands = $derived(allBands.filter((b) => b.coverage === 'dark'))
  const isCalm = $derived(!windowLoading && !windowError && allBands.length > 0 && darkBands.length === 0)

  function openInStream(b: FallBoundary, port?: number) {
    appState.setFilter('interface', b.inInterface || b.outInterface || '')
    appState.setFilter('chain', b.chain)
    if (typeof port === 'number' && port > 0) appState.setFilter('port', String(port))
    appState.view = 'live'
  }

  function carrierKey(e: KeyboardEvent, b: FallBoundary, port: number) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openInStream(b, port)
    }
  }

  function bandHeadSummary(b: BandView): string {
    const parts: string[] = [b.label]
    if (b.key === '__unmatched__') parts.push('events whose boundary is not in a pushed rule table yet')
    else if (b.coverage === 'dark') parts.push('dark -- blank because nothing is logged, not because nothing is sent')
    else if (b.coverage === 'unknown') parts.push('coverage unknown -- no router has pushed its rule table yet')
    if (b.total === 0) parts.push('no traffic in this window')
    else parts.push(`${b.nowTotal.accept} accepted, ${b.nowTotal.drop} dropped, ${b.nowTotal.nat} nat in the most recent slice`)
    parts.push('activate to open in Stream, filtered to this boundary')
    return parts.join('. ')
  }

  function carrierSummary(c: Carrier): string {
    const now = c.buckets[0]
    const state = now.total > 0 ? `${now.accept} accepted, ${now.drop} dropped, ${now.nat} nat right now` : 'quiet right now'
    return `carrier ${portLabel(c.port)}, ${c.total} events this window, ${state}. Activate to open in Stream, filtered to this boundary and port.`
  }
</script>

<div class="fall">
  <div class="fall-bar">
    <h1>The fall</h1>
    <p class="sub">
      a band per boundary, a carrier per port · blue = accepted · red = dropped · violet = nat
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
      Each column is one boundary: an interface pair a firewall rule or a live event actually carries. Within a
      band, each carrier is a port with real traffic, positioned left-to-right by port number. Click a boundary or a
      carrier to open it in Stream, filtered.
    </p>
    <div class="rig scrollbar">
      {#each allBands as b (b.key)}
        <div class="band" class:dark={b.coverage === 'dark'}>
          <button type="button" class="band-head" onclick={() => openInStream(b)} aria-label={bandHeadSummary(b)}>
            <span class="band-label">{b.label}</span>
            {#if b.key === '__unmatched__'}
              <span class="band-caption quiet">events not yet in a pushed rule table</span>
            {:else if b.coverage === 'dark'}
              <span class="band-caption bad">dark — no log rule</span>
            {:else if b.coverage === 'observed' && b.total === 0}
              <span class="band-caption quiet">quiet</span>
            {:else if b.coverage === 'observed'}
              <span class="band-caption ok">watch holding ✓</span>
            {/if}
          </button>

          {#if b.coverage === 'dark'}
            <svg class="dark-fill" viewBox="0 0 100 200" preserveAspectRatio="none" aria-hidden="true">
              <defs>
                <pattern id="hatch-{b.key}" width="4" height="4" patternTransform="rotate(45)" patternUnits="userSpaceOnUse">
                  <line x1="0" y1="0" x2="0" y2="4" class="hatch-line" />
                </pattern>
              </defs>
              <rect x="0" y="0" width="100" height="200" fill="url(#hatch-{b.key})" />
            </svg>
            <span class="sr-only">blank because nothing is logged — not because nothing is sent</span>
          {:else if b.carriers.length === 0}
            <p class="band-empty">no traffic in this window</p>
          {:else}
            <div class="band-body">
              <!-- The live spectrum: one peak per carrier, positioned by
                   port, height = its current-instant rate. "What is
                   arriving this instant." -->
              <svg class="spectrum" viewBox="0 0 100 26" preserveAspectRatio="none" aria-hidden="true">
                <polyline
                  class="spectrum-line"
                  points={b.carriers
                    .map((c) => `${c.x},${26 - (c.buckets[0].total / b.nowMax) * 16}`)
                    .join(' ')}
                />
                {#each b.carriers as c (c.port)}
                  {#if c.buckets[0].total > 0}
                    {@const h = (c.buckets[0].total / b.nowMax) * 16}
                    <circle cx={c.x} cy={26 - h} r="1.4" class="mark {dominantLane(c.buckets[0])}" data-port={c.port} />
                  {/if}
                {/each}
              </svg>
              <div class="now-line" aria-hidden="true"><span class="now-dot"></span></div>

              <svg class="waterfall" viewBox="0 0 100 {buckets}" preserveAspectRatio="none" role="presentation">
                {#each b.carriers as c (c.port)}
                  <!-- Invisible, wider hit target -- keyboard and pointer
                       both land here; the visible dash above (pointer-
                       events: none) is decoration on top of it. -->
                  <rect
                    x={c.x - HIT_WIDTH / 2}
                    y="0"
                    width={HIT_WIDTH}
                    height={buckets}
                    class="carrier-hit"
                    data-port={c.port}
                    role="button"
                    tabindex="0"
                    aria-label={carrierSummary(c)}
                    onclick={() => openInStream(b, c.port)}
                    onkeydown={(e) => carrierKey(e, b, c.port)}
                  />
                {/each}
                {#each b.carriers as c (c.port)}
                  {#each c.buckets as bucket, i (i)}
                    {#if bucket.total > 0}
                      {@const rate = c.maxBucketTotal > 0 ? bucket.total / c.maxBucketTotal : 0}
                      <rect
                        x={c.x - DASH_WIDTH / 2}
                        y={i}
                        width={DASH_WIDTH}
                        height="1"
                        class="mark {dominantLane(bucket)}"
                        data-port={c.port}
                        opacity={0.35 + rate * 0.65}
                        pointer-events="none"
                      />
                    {/if}
                  {/each}
                {/each}
              </svg>

              <div class="carrier-labels" aria-hidden="true">
                {#each b.carriers as c (c.port)}
                  <span class="carrier-label" data-port={c.port} style="left: {c.x}%">{portLabel(c.port)}</span>
                {/each}
              </div>

              {#if b.quieterCount > 0}
                <button
                  type="button"
                  class="quieter"
                  onclick={() => openInStream(b)}
                  aria-label="{b.quieterCount} quieter port{b.quieterCount === 1 ? '' : 's'} on {b.label}, folded out of the individual carriers above. Activate to open the whole boundary in Stream."
                >
                  +{b.quieterCount} quieter
                </button>
              {/if}
            </div>
          {/if}
        </div>
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
    /* The hero fills the viewport: the rig takes all remaining height so
       the bands run the full page like the ratified scene, rather than
       stopping at the waterfall's minimum and leaving dead page below. */
    flex: 1;
    min-height: 320px;
  }

  .band {
    flex: 1 0 220px;
    min-width: 220px;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
  }

  .band-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 10px 6px;
    border: none;
    border-bottom: 1px solid var(--border);
    background: transparent;
    text-align: left;
    cursor: pointer;
    color: inherit;
    font: inherit;
  }
  .band-head:hover {
    background: var(--bg-hover);
  }
  .band-head:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
    z-index: 1;
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

  .band-empty {
    margin: 0;
    padding: 16px 10px;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .dark-fill {
    flex: 1;
    width: 100%;
    display: block;
    background: var(--bg);
  }
  .hatch-line {
    stroke: var(--border);
    stroke-width: 1.5;
  }

  .band-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    position: relative;
  }

  .spectrum {
    width: 100%;
    height: 26px;
    display: block;
    flex: 0 0 26px;
    background: var(--bg);
  }
  .spectrum-line {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1;
    opacity: 0.85;
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
  .mark.other {
    fill: var(--fg-dim);
  }

  .now-line {
    height: 1px;
    background: var(--now);
    position: relative;
    margin: 2px 0;
    flex: 0 0 1px;
  }
  .now-dot {
    position: absolute;
    right: 2px;
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
    min-height: 220px;
    background: var(--bg);
  }
  .carrier-hit {
    fill: transparent;
    cursor: pointer;
  }
  .carrier-hit:hover,
  .carrier-hit:focus-visible {
    fill: color-mix(in srgb, var(--accent) 8%, transparent);
    outline: none;
  }
  .carrier-hit:focus-visible {
    stroke: var(--accent);
    stroke-width: 1;
  }

  .carrier-labels {
    position: relative;
    height: 16px;
    flex: 0 0 16px;
    border-top: 1px solid var(--border);
  }
  .carrier-label {
    position: absolute;
    top: 2px;
    transform: translateX(-50%);
    font-size: 8.5px;
    font-family: var(--font-mono);
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .quieter {
    flex: 0 0 auto;
    background: var(--bg);
    border: none;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
    font-size: 10px;
    padding: 4px 8px;
    text-align: center;
    cursor: pointer;
    width: 100%;
  }
  .quieter:hover {
    background: var(--bg-hover);
    color: var(--fg);
  }
  .quieter:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
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
