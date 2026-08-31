<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The fall (#616): the ratified hero live view, and the landing page.
  // Bounding record: issue #616's body, amended twice by Fable's
  // 2026-08-29 review on #616. Visual reference:
  // docs/design/concepts/round-3/direction-o-waterfall.html, scene 1
  // only. The rig below is that scene's own SVG — its coordinate
  // system (a 76px time rail, spectrum baseline at y=170, NOW at y=186,
  // the fall pouring y=196..760), its class vocabulary (.blab/.bsub/
  // .plab/.spec/.anno/.tlab/.darkband/.horizon) and its CSS values move
  // across from the mockup as-is; only the coordinates are computed from
  // live data instead of being hand-drawn. The band pitch that scene's
  // own SVG used (a fixed 160 units) is no longer fixed -- #722 gives it
  // a sizing policy instead, read from round 30's own mockup; see the
  // "Band sizing policy" comment below.
  // Where the record's plain vocabulary contradicts the mockup's radio
  // ambience ("no antenna"), the record wins: dark bands say "nothing is
  // logged".
  //
  // What "boundary" means here is narrower than the ratified record asks
  // for, and that gap is deliberate and disclosed rather than invented
  // silently -- see lib/fall.svelte.ts's header comment and PR #620.
  import { appState } from '../lib/state.svelte'
  import AccountMenu from './AccountMenu.svelte'
  import {
    fallState,
    boundaryKeyOf,
    laneColors,
    openBoundaryInStream as openInStream,
    type FallBoundary,
  } from '../lib/fall.svelte'
  import { fetchEventsWindow, fetchFlags } from '../lib/api'
  import { formatHM } from '../lib/format'
  import { lookupPort } from '../lib/commonPorts'
  import type { ClientEvent, Flag } from '../lib/types'
  import ConnectionIndicator from './ConnectionIndicator.svelte'
  import AlarmCluster from './AlarmCluster.svelte'

  // Bucket counts scale with the span so a ~64s knock still reads as
  // distinct dashes: 60 buckets at 15m is 15s/bucket. `railStep` is the
  // time-rail gridline interval, in minutes.
  const SPANS = [
    { id: '15m', label: '15 m', ms: 15 * 60 * 1000, buckets: 60, railStep: 3 },
    { id: '1h', label: '1 h', ms: 60 * 60 * 1000, buckets: 60, railStep: 15 },
    { id: '24h', label: '24 h', ms: 24 * 60 * 60 * 1000, buckets: 72, railStep: 360 },
    { id: '14d', label: '14 d', ms: 14 * 24 * 60 * 60 * 1000, buckets: 56, railStep: 4320 },
  ] as const
  type SpanId = (typeof SPANS)[number]['id']

  const POLL_MS = 5000
  // The review's cap: the 8 most recently active carriers per band render
  // as individual marks; the rest fold into "+n quieter".
  const MAX_CARRIERS = 8

  // The following three surfaces are implemented and load-bearing (the
  // data behind them is real), but the ratified round-30 mockup
  // (docs/design/concepts/round-30/shots/fall.png, the-whole.html #s2)
  // draws none of them anywhere on the fall -- an empty band is simply
  // empty, a quiet carrier count folds silently, and the window range
  // is not printed in the bottom-right corner. Round 30 builds to the
  // mockup first (#700); each gap is tracked on #691 for a future round
  // to remount. Do not delete these -- flip the relevant const and
  // restore the markup below when #691 is picked up.
  const EMPTY_BAND_CAPTION_ENABLED: boolean = false
  const QUIETER_COUNT_ENABLED: boolean = false
  const WINDOW_RANGE_CAPTION_ENABLED: boolean = false

  // ── The mockup's own geometry, in its own units ─────────────────────
  const RAIL = 76 // left time rail width
  const SPEC_TOP = 62 // spectrum strip top
  const SPEC_BASE = 170 // spectrum baseline
  const NOW_Y = 186 // the NOW line
  const FALL_TOP = 196 // the fall's top edge
  const FALL_BOT = 760 // the fall's floor
  const PORTLAB_Y = 775 // port labels under the floor
  const RIG_H = 800
  const DASH_W = 2.4 // a carrier dash's width — thin, never a fill
  const HIT_W = 14 // a carrier's invisible click/focus target width

  // ── Band sizing policy (#722): an ideal width, elastic within limits,
  // and pages beyond them ─────────────────────────────────────────────
  // Before this, the rig gave every band a fixed 160-unit slot but drew
  // its svg at `width: 100%` -- so the viewBox (which grows with the
  // band count) always got rescaled to fit the same pixel width, which
  // is exactly "divide the available width by however many there are"
  // in disguise. At 16 boundaries the titles overlapped outright
  // (#709's seeding took the fall from 3 bands to 16). The fix below
  // gives the svg an intrinsic pixel size (see `rig`'s `width`/`height`
  // attributes in the template) so a band's width is chosen on purpose,
  // never a side effect of how many neighbours it has.
  //
  // Read off round 30's own mockup (docs/design/concepts/round-30/
  // shots/fall.png; markup the-whole.html section #s2), which draws the
  // comfortable case: nine bands across the frame. That mockup's own rig
  // is a 1400×560 viewBox rendered into a 1600px-wide frame (scale
  // 1600/1400 = 8/7); its nine band headers sit on a 150-unit pitch --
  // compare consecutive `.bh-name` label x's: 120, 270, 420, … 1320,
  // each 150 apart. Scaled to the real frame that pitch is the
  // "comfortable" figure below.
  const IDEAL_PITCH = 171 // 150 × 8/7 ≈ 171.4 -- the mockup's own 9-band pitch at 1600px wide
  // Too few to fill the frame: bands may stretch past the ideal, but
  // capped so two (or one) boundaries never draw two comically wide
  // columns (owner's own wording). The mockup has no few-boundary scene
  // to measure, so this is chosen rather than read: roughly double the
  // ideal, wide enough to look deliberate without reading as a poster.
  const MAX_PITCH = 340
  // Slightly too many: shrink by "a very small amount" (owner) before
  // giving up and paginating -- about 88% of the ideal, enough give to
  // fit a couple more boundaries onto one page without the scene
  // reading cramped. Below this floor the policy stops shrinking and
  // pages instead.
  const MIN_PITCH = 150
  const GUTTER = 10 // the shipped rig's own band-to-band gutter -- unchanged, not part of this issue
  // Only used to size a carrier's invisible click target (below) before
  // the render pass has counted this page's bands and settled on a real
  // pitch; imprecision here only affects a hit-target's width slightly,
  // never anything drawn.
  const NOMINAL_BAND_W = IDEAL_PITCH - GUTTER
  // jsdom (tests) and the very first paint report a 0 clientWidth before
  // layout has run; 1600 is both this issue's own verification width and
  // a reasonable fallback meanwhile.
  const DEFAULT_FRAME_W = 1600

  let span = $state<SpanId>('15m')
  let flags = $state<Flag[]>([])
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
    // Flags ride the same poll, non-fatally: the fall prints them at
    // their moment on their band, but a flags fetch failing must never
    // take the waterfall down with it.
    fetchFlags()
      .then((r) => (flags = r.flags))
      .catch(() => {})
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
    // the fall is open (a router pushes a fresh rule table).
    fallState.refresh()
    const id = setInterval(() => fallState.refresh(), POLL_MS)
    return () => clearInterval(id)
  })

  $effect(() => {
    // Re-run whenever `span` (and so bucket count) changes, and every
    // POLL_MS besides -- a poll-and-rebucket over a bounded server
    // query, never one computation per arriving event.
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
    mostRecentActive: number
    maxBucketTotal: number
    activeBuckets: number
    lane: Lane // the carrier's dominant lane over the whole window
    x: number // 0-100 within the band -- "position = port"
    hitW: number // click target width, shrunk where carriers crowd
  }

  interface FlagMark {
    idx: number
    type: string
    hm: string
    n: number // how many flags this mark stands for, clustered
  }

  interface BandView extends FallBoundary {
    carriers: Carrier[]
    quieterCount: number
    total: number
    nowMax: number // busiest carrier's current-instant rate, for spectrum scaling
    maxBucket: number // busiest single bucket on the band, for dash scaling
    dropShare: number // 0-1 over the window, for the red ramp wash
    deepestActive: number // deepest bucket with traffic -- below it the band is black
    flagMarks: FlagMark[]
  }

  // portX maps carriers onto [10, 90] linearly by port number.
  function assignX(all: { port: number }[]): number[] {
    if (all.length === 0) return []
    if (all.length === 1) return [50]
    const ports = all.map((c) => c.port)
    const lo = Math.min(...ports)
    const hi = Math.max(...ports)
    if (lo === hi) return all.map(() => 50)
    return ports.map((p) => 10 + ((p - lo) / (hi - lo)) * 80)
  }

  function laneTotals(bl: Bucket[]): Bucket {
    const t = emptyBucket()
    for (const b of bl) {
      t.accept += b.accept
      t.drop += b.drop
      t.nat += b.nat
      t.other += b.other
      t.total += b.total
    }
    return t
  }

  // bands buckets every event in the current window, once, into
  // buckets-per-(boundary, port) slices -- O(events + pairs * buckets),
  // never one DOM node per event.
  const bandsData = $derived.by((): BandView[] => {
    const boundaries = fallState.boundaries
    const byKey = new Map<string, FallBoundary>()
    for (const b of boundaries) byKey.set(b.key, b)

    const bucketCount = buckets
    type PortMap = Map<number, Bucket[]>
    const portsByKey = new Map<string, PortMap>()
    const totalByKey = new Map<string, number>()
    const span2 = Math.max(windowEnd - windowStart, 1)
    const mkBuckets = () => Array.from({ length: bucketCount }, emptyBucket)

    for (const b of boundaries) portsByKey.set(b.key, new Map())
    let unmatchedPorts: PortMap | null = null
    let unmatchedTotal = 0
    const ipToKey = new Map<string, string>()

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
      // The flag join: a flag names only its target IP, so a flag is
      // placed on the band its target actually appeared on this window
      // -- an honest join, never a guess. First sighting wins.
      if (!isUnmatched) {
        if (e.srcIp && !ipToKey.has(e.srcIp)) ipToKey.set(e.srcIp, key)
        if (e.dstIp && !ipToKey.has(e.dstIp)) ipToKey.set(e.dstIp, key)
      }
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

      if (isUnmatched) unmatchedTotal++
      else totalByKey.set(key, (totalByKey.get(key) ?? 0) + 1)
    }

    function carriersFor(ports: PortMap): { carriers: Carrier[]; quieterCount: number } {
      const all: Omit<Carrier, 'x' | 'hitW'>[] = []
      for (const [port, bucketList] of ports) {
        let total = 0
        let mostRecentActive: number = bucketCount
        let maxBucketTotal = 0
        let activeBuckets = 0
        for (let i = 0; i < bucketList.length; i++) {
          total += bucketList[i].total
          if (bucketList[i].total > 0) {
            activeBuckets++
            if (mostRecentActive === bucketCount) mostRecentActive = i
          }
          maxBucketTotal = Math.max(maxBucketTotal, bucketList[i].total)
        }
        all.push({
          port,
          buckets: bucketList,
          total,
          mostRecentActive,
          maxBucketTotal,
          activeBuckets,
          lane: dominantLane(laneTotals(bucketList)),
        })
      }
      all.sort((a, b) => a.mostRecentActive - b.mostRecentActive || b.total - a.total)
      const shown = all.slice(0, MAX_CARRIERS)
      const xs = assignX(shown)
      const withX: Carrier[] = shown.map((c, i) => {
        let nearest = Infinity
        for (let j = 0; j < xs.length; j++) if (j !== i) nearest = Math.min(nearest, Math.abs(xs[j] - xs[i]))
        // 0-100 band space → band units happens at render; keep the
        // target no wider than the gap to its neighbour so an overlapped
        // target never steals its neighbour's clicks.
        const hitW = Math.max(DASH_W + 1, Math.min(HIT_W, (nearest / 100) * NOMINAL_BAND_W))
        return { ...c, x: xs[i], hitW }
      })
      return { carriers: withX, quieterCount: Math.max(0, all.length - MAX_CARRIERS) }
    }

    // Flags at their moment: every uncleared flag whose target IP
    // appeared on a band this window, bucketed by when it FIRED
    // (firstSeen) -- "flag at its moment", not at its latest repeat.
    // Flags of one type landing within a couple of buckets of each
    // other on the same band cluster into one mark carrying a count,
    // so a storm of near-simultaneous flags never prints as a stack of
    // colliding rings and labels.
    const flagsByKey = new Map<string, FlagMark[]>()
    for (const f of flags) {
      if (f.cleared) continue
      const key = ipToKey.get(f.target)
      if (!key) continue
      const t = new Date(f.firstSeen).getTime()
      if (Number.isNaN(t) || t < windowStart || t > windowEnd) continue
      let idx = Math.floor(((windowEnd - t) / span2) * bucketCount)
      if (idx < 0) idx = 0
      if (idx >= bucketCount) idx = bucketCount - 1
      const list = flagsByKey.get(key) ?? []
      const near = list.find((m) => m.type === f.type && Math.abs(m.idx - idx) <= 2)
      if (near) {
        near.n++
        if (idx < near.idx) {
          near.idx = idx
          near.hm = formatHM(f.firstSeen)
        }
      } else {
        list.push({ idx, type: f.type, hm: formatHM(f.firstSeen), n: 1 })
      }
      flagsByKey.set(key, list)
    }

    function toView(b: FallBoundary, ports: PortMap, total: number): BandView {
      const { carriers, quieterCount } = carriersFor(ports)
      const nowMax = Math.max(1, ...carriers.map((c) => c.buckets[0]?.total ?? 0))
      const maxBucket = Math.max(1, ...carriers.map((c) => c.maxBucketTotal))
      let drops = 0
      let tot = 0
      let deepestActive = 0
      for (const [, bl] of ports)
        for (let i = 0; i < bl.length; i++) {
          drops += bl[i].drop
          tot += bl[i].total
          if (bl[i].total > 0 && i > deepestActive) deepestActive = i
        }
      return {
        ...b,
        carriers,
        quieterCount,
        total,
        nowMax,
        maxBucket,
        dropShare: tot > 0 ? drops / tot : 0,
        deepestActive,
        flagMarks: flagsByKey.get(b.key) ?? [],
      }
    }

    const known = boundaries.map((b) => toView(b, portsByKey.get(b.key)!, totalByKey.get(b.key) ?? 0))
    if (unmatchedPorts)
      known.push(
        toView(
          {
            key: '__unmatched__',
            chain: '',
            inInterface: '',
            outInterface: '',
            srcAddressList: '',
            label: 'other traffic',
            coverage: 'unknown',
            epithet: '',
          },
          unmatchedPorts,
          unmatchedTotal,
        ),
      )
    return known
  })

  const allBands = $derived(bandsData)
  const darkBands = $derived(allBands.filter((b) => b.coverage === 'dark'))

  // ── Pagination: apply the sizing policy above ───────────────────────
  // `.rig`'s own measured width (see `bind:clientWidth` in the template)
  // -- the real budget the policy divides up, falling back to
  // DEFAULT_FRAME_W before the first layout pass (or under jsdom).
  let rigW = $state(0)
  const containerWidth = $derived(rigW || DEFAULT_FRAME_W)
  const bandsAreaW = $derived(Math.max(0, containerWidth - RAIL - 4))

  // The most boundaries a single page can hold, packed as tight as the
  // shrink floor (MIN_PITCH) allows -- past this count the policy stops
  // shrinking and pages instead.
  const maxPerPage = $derived(Math.max(1, Math.floor(bandsAreaW / MIN_PITCH)))
  const totalPages = $derived(allBands.length === 0 ? 1 : Math.max(1, Math.ceil(allBands.length / maxPerPage)))
  // Bands per page, spread evenly across `totalPages` (never the
  // lopsided "15 on page one, 1 on page two" the owner ruled out) --
  // pages differ by at most one boundary when the count doesn't divide
  // exactly.
  const perPage = $derived(allBands.length === 0 ? 0 : Math.ceil(allBands.length / totalPages))

  let page = $state(0)
  $effect(() => {
    // Clamp when the boundary count (or the window, changing
    // maxPerPage) shrinks the page count out from under the current
    // page -- never reads or sets `page` when it's already in range, so
    // this can't loop.
    if (page > 0 && page >= totalPages) page = Math.max(0, totalPages - 1)
  })

  const pageBands = $derived.by(() => {
    if (allBands.length === 0) return []
    const start = page * perPage
    return allBands.slice(start, start + perPage)
  })

  // The pitch this page actually renders at: stretch toward MAX_PITCH
  // when there's slack, sit at IDEAL_PITCH (or above) when the count is
  // comfortable, or give a little (down to MIN_PITCH) when it's
  // slightly over -- `perPage`, not the page's own possibly-smaller
  // last-page count, so every page in a paginated fall draws its bands
  // at the same size.
  const pitch = $derived.by(() => {
    const n = perPage || 1
    const natural = bandsAreaW / n
    if (natural >= IDEAL_PITCH) return Math.min(MAX_PITCH, natural)
    return Math.max(MIN_PITCH, natural)
  })
  const bandW = $derived(Math.max(20, pitch - GUTTER))

  // ── Rig layout: every band on this page gets its own pitch-wide slot ─
  interface BandSlot {
    band: BandView
    bx: number // band's left edge in rig units
    laneColor: string // header underline colour
  }

  const rig = $derived.by(() => {
    const slotsSource = pageBands
    const n = slotsSource.length
    const width = RAIL + n * pitch + 4
    // One boundary, one colour, everywhere, on every page: keyed off the
    // full boundary set (not just this page's), the same lane map the
    // atlas overlay's zones draw from.
    const lanes = laneColors(allBands)
    const slots: BandSlot[] = slotsSource.map((band, i) => ({
      band,
      bx: RAIL + i * pitch,
      laneColor: lanes.get(band.key) || 'var(--o-ink3)',
    }))
    return { width, slots }
  })

  // A carrier's x within the rig.
  function cx(slot: BandSlot, c: Carrier): number {
    return slot.bx + 10 + (c.x / 100) * (bandW - 20)
  }

  // ── The time rail: gridlines at the span's own step ─────────────────
  const railLines = $derived.by(() => {
    if (!windowStart || !windowEnd) return []
    const stepMs = spanDef.railStep * 60 * 1000
    const lines: { y: number; label: string }[] = []
    const spanLen = windowEnd - windowStart
    for (let t = Math.ceil(windowStart / stepMs) * stepMs; t < windowEnd - stepMs * 0.08; t += stepMs) {
      const y = FALL_TOP + ((windowEnd - t) / spanLen) * (FALL_BOT - FALL_TOP)
      if (y < FALL_TOP + 12 || y > FALL_BOT - 4) continue
      const d = new Date(t)
      const label =
        span === '14d'
          ? `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
          : formatHM(d.toISOString())
      lines.push({ y, label })
    }
    return lines
  })

  const bucketH = $derived((FALL_BOT - FALL_TOP) / buckets)

  function bucketY(idx: number): number {
    return FALL_TOP + idx * bucketH
  }

  // ── The spectrum: one needle per carrier arriving this instant ──────
  interface Needle {
    x: number
    tipY: number
    lane: Lane
    port: number
  }

  function needlesFor(slot: BandSlot): Needle[] {
    const b = slot.band
    return b.carriers
      .filter((c) => c.buckets[0].total > 0)
      .map((c) => ({
        x: cx(slot, c),
        tipY: SPEC_BASE - (24 + (c.buckets[0].total / b.nowMax) * 72),
        lane: dominantLane(c.buckets[0]),
        port: c.port,
      }))
  }

  // A wave, not a tent (#650, round 21/22): a smooth mound with soft
  // skirts that meets the baseline at both ends but never draws it --
  // the two cubics climb from base to the shoulder of the peak, and a
  // shallow quadratic caps it. No fill: the line is the only coloured
  // bit (round 22, owner).
  function wavePath(n: Needle): string {
    const halfW = 8
    const x0 = n.x - halfW
    const x1 = n.x + halfW
    const h = SPEC_BASE - n.tipY
    const cap = halfW * 0.18
    const overshoot = h * 0.1
    const shoulderX = halfW * 0.55
    const shoulderY = SPEC_BASE - h * 0.6
    return (
      `M ${x0},${SPEC_BASE} ` +
      `C ${x0 + halfW * 0.41},${SPEC_BASE - 1} ${x0 + shoulderX},${shoulderY} ${n.x - cap},${n.tipY} ` +
      `Q ${n.x},${n.tipY - overshoot} ${n.x + cap},${n.tipY} ` +
      `C ${x1 - shoulderX},${shoulderY} ${x1 - halfW * 0.41},${SPEC_BASE - 1} ${x1},${SPEC_BASE}`
    )
  }

  // Peak labels, culled so neighbours never collide: strongest first,
  // then any label whose text would overlap one already kept is dropped.
  const peakLabels = $derived.by(() => {
    const cands: { x: number; y: number; text: string; lane: Lane; h: number }[] = []
    for (const slot of rig.slots) {
      if (slot.band.coverage === 'dark') continue
      for (const n of needlesFor(slot))
        cands.push({ x: n.x, y: n.tipY - 8, text: portLabel(n.port), lane: n.lane, h: SPEC_BASE - n.tipY })
    }
    cands.sort((a, b) => b.h - a.h)
    const kept: typeof cands = []
    for (const c of cands) {
      const w = c.text.length * 6.2
      // Round 30 places one label above each curve, never stacked on
      // another (#700) -- the build's own 11-unit vertical tolerance was
      // tight enough that two peaks differing in height by just over
      // that still rendered close enough for a 10px label (which draws
      // roughly a full line's height either side of its baseline) to
      // visibly overlap. Widened to a margin that actually clears a
      // label's own rendered height, alongside a slightly wider
      // horizontal gap.
      if (kept.some((k) => Math.abs(k.x - c.x) < (k.text.length * 6.2 + w) / 2 + 8 && Math.abs(k.y - c.y) < 15)) continue
      kept.push(c)
    }
    return kept
  })

  // Port labels under the floor, culled the same way (heaviest carrier
  // keeps its label; a crowded band folds the rest) -- but only ever
  // against another candidate on the *same* band (#700). Comparing
  // across bands let a heavy carrier on one boundary silently suppress
  // a lighter one several bands away whenever the two candidates'
  // absolute x (adjacent bands sit only pitch-bandW apart, i.e. one
  // gutter) happened to
  // fall inside the text-width gap, which is how the build ended up
  // labelling only a couple of ports total instead of every band along
  // the foot.
  const portLabels = $derived.by(() => {
    const cands: { x: number; text: string; lane: Lane; port: number; bandKey: string; w: number }[] = []
    for (const slot of rig.slots) {
      for (const c of slot.band.carriers) {
        const text = portLabel(c.port)
        cands.push({ x: cx(slot, c), text, lane: c.lane, port: c.port, bandKey: slot.band.key, w: c.total })
      }
    }
    cands.sort((a, b) => b.w - a.w)
    const kept: typeof cands = []
    for (const c of cands) {
      const w = c.text.length * 6.2
      if (kept.some((k) => k.bandKey === c.bandKey && Math.abs(k.x - c.x) < (k.text.length * 6.2 + w) / 2 + 4)) continue
      kept.push(c)
    }
    return kept
  })

  // Flag horizons: each flagged moment draws the mockup's dotted line
  // through every band, its time on the rail, and its name on the band
  // it joined to.
  const flagHorizons = $derived.by(() => {
    const list: { y: number; hm: string; type: string; bx: number; n: number }[] = []
    for (const slot of rig.slots)
      for (const m of slot.band.flagMarks)
        list.push({ y: bucketY(m.idx) + bucketH / 2, hm: m.hm, type: m.type, bx: slot.bx, n: m.n })
    return list
  })

  // The same identity join bandsData's own flagsByKey uses (a flag names
  // only its target IP; the boundary it belongs to is whichever band
  // that IP actually appeared on this window) -- kept as its own
  // derived, independent of the per-bucket pass above, so flagChips can
  // resolve a boundary label without being tied to bandsData's window
  // gating.
  const ipToBoundaryKey = $derived.by(() => {
    const knownKeys = new Set(fallState.boundaries.map((b) => b.key))
    const m = new Map<string, string>()
    for (const e of windowEvents) {
      const key = boundaryKeyOf(e.chain, e.inInterface, e.outInterface)
      if (!knownKeys.has(key)) continue
      if (e.srcIp && !m.has(e.srcIp)) m.set(e.srcIp, key)
      if (e.dstIp && !m.has(e.dstIp)) m.set(e.dstIp, key)
    }
    return m
  })

  // The attention chips: one per flag type among every currently open
  // (uncleared) flag -- deliberately independent of the visible span.
  // Round 30's own defect (#700): the build derived these from
  // flagHorizons, which only ever carries flags whose firstSeen falls
  // inside the current window AND whose target IP happened to appear in
  // that same window's events -- so a flag raised more than one span ago
  // (a `15 m` default is a common case) silently dropped off every chip,
  // even though the header's own flag count (⚑) is span-independent and
  // kept counting it. The chips are the fall's restatement of that same
  // current state, not a narrower one, so they read straight off `flags`
  // instead. The boundary label (", iot → bridge1") is still
  // window-scoped best-effort via ipToBoundaryKey above -- omitted, not
  // guessed, when the flagged IP hasn't produced a matching event in the
  // window currently loaded.
  const flagChips = $derived.by(() => {
    const byKeyLabel = new Map(fallState.boundaries.map((b) => [b.key, b.label]))
    const byType = new Map<string, { n: number; t: number; hm: string; boundaryLabel?: string }>()
    for (const f of flags) {
      if (f.cleared) continue
      const t = new Date(f.firstSeen).getTime()
      if (Number.isNaN(t)) continue
      const boundaryKey = ipToBoundaryKey.get(f.target)
      const boundaryLabel = boundaryKey ? byKeyLabel.get(boundaryKey) : undefined
      const cur = byType.get(f.type)
      if (!cur) byType.set(f.type, { n: 1, t, hm: formatHM(f.firstSeen), boundaryLabel })
      else {
        cur.n++
        if (t > cur.t) {
          cur.t = t
          cur.hm = formatHM(f.firstSeen)
          if (boundaryLabel) cur.boundaryLabel = boundaryLabel
        }
      }
    }
    return [...byType.entries()]
      .sort((a, b) => b[1].t - a[1].t)
      .map(([type, v]) => ({ type, n: v.n, hm: v.hm, boundaryLabel: v.boundaryLabel }))
  })

  function keyActivate(e: KeyboardEvent, fn: () => void) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      fn()
    }
  }

  function bandHeadSummary(b: BandView): string {
    const parts: string[] = [b.label]
    if (b.key === '__unmatched__') parts.push('events whose boundary is not in a pushed rule table yet')
    else if (b.coverage === 'dark') parts.push('dark -- blank because nothing is logged, not because nothing is sent')
    else if (b.coverage === 'unknown') parts.push('coverage unknown -- no router has pushed its rule table yet')
    if (b.total === 0) parts.push('no traffic in this window')
    else parts.push(`${b.total} events this window`)
    parts.push('activate to open in Stream, filtered to this boundary')
    return parts.join('. ')
  }

  function carrierSummary(c: Carrier): string {
    const now = c.buckets[0]
    const state = now.total > 0 ? `${now.accept} accepted, ${now.drop} dropped, ${now.nat} nat right now` : 'quiet right now'
    return `carrier ${portLabel(c.port)}, ${c.total} events this window, ${state}. Activate to open in Stream, filtered to this boundary and port.`
  }

  function nowClock(): string {
    return windowEnd ? new Date(windowEnd).toTimeString().slice(0, 8) : ''
  }
</script>

<div class="fall">
  <div class="bar">
    <span class="wm">MIKRO<em>VIEW</em></span>
    <!-- No name and no strap (#697/#700, owner verbatim: "I meant all...
         No page heading, no strap"). This bar drew its own rather than
         going through SceneBar, which is how it survived the first
         sweep. The rail says which card you are on. -->
    <div class="status-cluster">
      <!-- The fall's spans ride the status cluster, ahead of LIVE
           (round 30's README §4), not mid-bar under a SPAN label. -->
      <div class="span-control" role="group" aria-label="Time span">
        {#each SPANS as s (s.id)}
          <button type="button" class="rng" class:on={span === s.id} aria-pressed={span === s.id} onclick={() => (span = s.id)}>
            {s.label}
          </button>
        {/each}
      </div>
      <ConnectionIndicator />
      <AlarmCluster />
      <AccountMenu />
    </div>
  </div>

  <!-- Below the bar, not riding it (#683, round 29: the bar carries only
       wordmark · name · strap · the page's own control · LIVE·rate ·
       ⚑ · ◉○ · account -- these per-boundary alerts are the fall's own
       content, same as .fallwrap's fall-head in the mockup).

       Centred, not left-aligned: the mockup's own `.fall-head` example
       packs its two chips against the left edge, but the owner asked
       for the row to sit centred on the scene and fan outward in both
       directions as chips are added, rather than growing rightward from
       a fixed left start -- ratified in session on 2026-08-31, beyond
       what the mockup itself shows. `justify-content: center` on this
       row (below) is that instruction, not a mockup value. -->
  <span class="attention" aria-live="polite">
    {#each flagChips.slice(0, 3) as f, fi (fi)}
      <button type="button" class="att alarm" onclick={() => (appState.view = 'flags')}>
        <i></i>{f.type.replace(/_/g, ' ').toUpperCase()}{f.n > 1 ? ` ×${f.n}` : ''} — {f.hm}{f.boundaryLabel
          ? ` · ${f.boundaryLabel}`
          : ''}
      </button>
    {/each}
    {#if darkBands.length > 0}
      <button type="button" class="att dark" onclick={() => openInStream(darkBands[0])}>
        <i></i>{darkBands.length} dark boundar{darkBands.length === 1 ? 'y' : 'ies'} — nothing logged
      </button>
    {/if}
  </span>

  {#if fallState.loading}
    <p class="state-msg">Reading pushed firewall rules…</p>
  {:else if fallState.error}
    <p class="state-msg error" role="alert">Could not read the pushed rule tables: {fallState.error}</p>
  {:else if allBands.length === 0 && !windowLoading}
    <!-- Unmissable, mid-page: an empty fall must read as waiting, never
         as broken or unbuilt (owner, 2026-08-30). -->
    <div class="state-block">
      <p class="state-msg">nothing has arrived yet — waiting for data, not broken</p>
      <p class="state-sub">
        the fall draws each boundary's log as it falls; it needs your router to push its filter rules.
        Settings → Run setup… prints the script.
      </p>
    </div>
  {:else}
    <div class="rig" bind:clientWidth={rigW}>
      <!-- Explicit pixel width/height, not `width: 100%`: this is what
           actually fixes #722 -- the svg renders at its own chosen size
           (1 viewBox unit = 1px) instead of being rescaled to fit
           whatever the container happens to be, which is what silently
           divided the available width by the band count before. `.rig`'s
           own `justify-content: center` (below) centres it, leaving
           empty space on either side when the policy stretch-caps a
           small estate rather than filling the frame. -->
      <svg viewBox="0 0 {rig.width} {RIG_H}" width={rig.width} height={RIG_H}>
        <defs>
          <pattern id="fall-hatch" width="8" height="8" patternTransform="rotate(45)" patternUnits="userSpaceOnUse">
            <line x1="0" y1="0" x2="0" y2="8" class="hatch-line" />
          </pattern>
          <linearGradient id="fall-ramp" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0" class="ramp-hi" /><stop offset="1" class="ramp-lo" />
          </linearGradient>
        </defs>

        <!-- ══ the time gutter (a flag's moment outranks a colliding
             time label or now label) -- round 30 draws no grid at all,
             here or between bands: the labels are the only marks in
             this margin (#700). ══ -->
        {#if !flagHorizons.some((f) => Math.abs(f.y - (FALL_TOP + 4)) < 16)}
          <text class="tlab now-t" x={RAIL - 14} y={FALL_TOP + 4} text-anchor="end">{formatHM(new Date(windowEnd || Date.now()).toISOString())}</text>
        {/if}
        {#each railLines as l (l.y)}
          {#if !flagHorizons.some((f) => Math.abs(f.y - l.y) < 12)}
            <text class="tlab" x={RAIL - 14} y={l.y + 3} text-anchor="end">{l.label}</text>
          {/if}
        {/each}

        {#each rig.slots as slot (slot.band.key)}
          {@const b = slot.band}
          <g class="band" class:dark={b.coverage === 'dark'}>
            <!-- ══ the dial: this band's header ══ -->
            <g
              class="band-head"
              role="button"
              tabindex="0"
              aria-label={bandHeadSummary(b)}
              onclick={() => openInStream(b)}
              onkeydown={(e) => keyActivate(e, () => openInStream(b))}
            >
              <rect class="head-hit" x={slot.bx} y="6" width={bandW} height="56" />
              <text class="blab band-label" x={slot.bx + 6} y="22">{b.label}</text>
              {#if b.epithet}<text class="bsub band-epithet" x={slot.bx + 6} y="36">{b.epithet}</text>{/if}
              {#if b.key === '__unmatched__'}
                <text class="chip ch-mut band-caption quiet" x={slot.bx + 6} y="50">NOT IN A PUSHED TABLE</text>
              {:else if b.coverage === 'dark'}
                <text class="chip ch-bad band-caption bad" x={slot.bx + 6} y="50">DARK — NO LOG RULE</text>
              {:else if b.coverage === 'unknown'}
                <text class="chip ch-mut band-caption quiet" x={slot.bx + 6} y="50">COVERAGE UNKNOWN</text>
              {:else if b.flagMarks.length > 0}
                {@const fired = b.flagMarks.reduce((a, m) => (m.idx > a.idx ? m : a), b.flagMarks[0])}
                <text class="chip ch-bad band-caption bad" x={slot.bx + 6} y="50"
                  >✱ {fired.type.replace(/_/g, ' ').toUpperCase()}</text
                >
              {:else}
                <!-- Round 30 reads a quiet-but-covered band the same as
                     a busy one: the watch holds either way (README
                     "quiet is a fact, not a fault") -- there is no
                     separate QUIET caption. -->
                <text class="chip ch-ok band-caption ok" x={slot.bx + 6} y="50">WATCH HOLDING ✓</text>
              {/if}
              <rect x={slot.bx} y="56" width={bandW} height="3" rx="1.5" fill={slot.laneColor} />
            </g>

            <!-- ══ the panadapter: this band's live spectrum ══ -->
            <g class="spectrum" aria-hidden="true">
              {#if b.coverage === 'dark'}
                <line x1={slot.bx} y1={SPEC_BASE} x2={slot.bx + bandW} y2={SPEC_BASE} class="dark-baseline" />
                <text class="anno bad-anno" x={slot.bx + bandW / 2} y={SPEC_BASE - 32} text-anchor="middle"
                  >nothing logged —</text>
                <text class="anno bad-anno" x={slot.bx + bandW / 2} y={SPEC_BASE - 20} text-anchor="middle"
                  >no trace, and no claim of one</text>
              {:else}
                {#each needlesFor(slot) as n (n.port)}
                  <path class="spec {n.lane}" data-port={n.port} d={wavePath(n)} />
                {/each}
                {#if b.carriers.length > 0 && needlesFor(slot).length === 0}
                  <line x1={slot.bx} y1={SPEC_BASE} x2={slot.bx + bandW} y2={SPEC_BASE} class="spec-floor" />
                {/if}
              {/if}
            </g>

            <!-- ══ this band's stretch of the fall ══ -->
            <g class="waterfall">
              {#if b.coverage === 'dark'}
                <rect class="darkband" x={slot.bx} y={FALL_TOP} width={bandW} height={FALL_BOT - FALL_TOP} />
                <text class="anno bad-anno strong" x={slot.bx + bandW / 2} y="420" text-anchor="middle"
                  >blank because nothing is logged</text>
                <text class="anno" x={slot.bx + bandW / 2} y="434" text-anchor="middle"
                  >— not because nothing is sent</text>
              {:else}
                {#if b.dropShare > 0.5 && b.total > 0}
                  <!-- the red tide: a drop-dominated band carries the
                       mockup's top-heavy ramp behind its dashes -->
                  <!-- the wash reaches only as deep as the traffic does:
                       below the last active bucket the band is black to
                       the floor, the way the ratified scene draws it -->
                  <rect
                    x={slot.bx}
                    y={FALL_TOP}
                    width={bandW}
                    height={Math.min(FALL_BOT - FALL_TOP, (b.deepestActive + 2) * bucketH)}
                    fill="url(#fall-ramp)"
                    opacity={0.3 + 0.45 * b.dropShare}
                  />
                {/if}
                {#each b.carriers as c (c.port)}
                  {#if c.lane === 'accept' && b.total > 0 && c.total / b.total >= 0.55 && c.activeBuckets >= buckets * 0.5}
                    <!-- the broad warm carrier of the household -->
                    <rect x={cx(slot, c) - 24} y={FALL_TOP} width="48" height={FALL_BOT - FALL_TOP} class="glow glow-outer" />
                    <rect x={cx(slot, c) - 12} y={FALL_TOP} width="24" height={FALL_BOT - FALL_TOP} class="glow glow-inner" />
                  {/if}
                {/each}
                {#each b.carriers as c (c.port)}
                  <rect
                    class="carrier-hit"
                    data-port={c.port}
                    role="button"
                    tabindex="0"
                    aria-label={carrierSummary(c)}
                    x={cx(slot, c) - c.hitW / 2}
                    y={FALL_TOP}
                    width={c.hitW}
                    height={FALL_BOT - FALL_TOP}
                    onclick={() => openInStream(b, c.port)}
                    onkeydown={(e) => keyActivate(e, () => openInStream(b, c.port))}
                  />
                {/each}
                {#each b.carriers as c (c.port)}
                  {#each c.buckets as bucket, i (i)}
                    {#if bucket.total > 0}
                      {@const rate = bucket.total / b.maxBucket}
                      {@const dashH = Math.min(bucketH, 1.8 + rate * (bucketH - 1.8))}
                      <rect
                        class="mark {dominantLane(bucket)}"
                        data-port={c.port}
                        x={cx(slot, c) - DASH_W / 2}
                        y={bucketY(i) + (bucketH - dashH) / 2}
                        width={DASH_W}
                        height={dashH}
                        opacity={0.45 + rate * 0.5}
                        pointer-events="none"
                      />
                    {/if}
                  {/each}
                {/each}
                {#if QUIETER_COUNT_ENABLED && b.quieterCount > 0}
                  <text
                    class="quieter"
                    role="button"
                    tabindex="0"
                    aria-label="{b.quieterCount} quieter port{b.quieterCount === 1 ? '' : 's'} on {b.label}, folded out of the individual carriers above. Activate to open the whole boundary in Stream."
                    x={slot.bx + bandW / 2}
                    y={FALL_BOT - 8}
                    text-anchor="middle"
                    onclick={() => openInStream(b)}
                    onkeydown={(e) => keyActivate(e, () => openInStream(b))}>+{b.quieterCount} quieter</text>
                {/if}
                {#if EMPTY_BAND_CAPTION_ENABLED && b.carriers.length === 0 && b.total === 0}
                  <text class="anno" x={slot.bx + bandW / 2} y="420" text-anchor="middle">no traffic in this window</text>
                {/if}
              {/if}
            </g>

            <!-- flag annotations at their moment on this band. The
                 mockup's single wide "new_talker" callout (a red band
                 across the full rig width -- "✳ new_talker · cam-porch
                 → nas :445 · born 13:52 on a band blank for 41 days ·
                 ×14 · open ▸") has no counterpart here: `new_talker`
                 is not a FlagType this codebase raises (see
                 lib/types.ts's FlagType union -- the closest existing
                 detector, `new_device`, answers "has this device ever
                 been seen at all", not "has this device ever used this
                 boundary/port before"), so there is no evidence to draw
                 the callout from, not a styling gap. Tracked for #700
                 follow-up: a new detector and FlagType would need to
                 land server-side (internal/detect, internal/flags)
                 before this callout can be built here. -->
            {#each b.flagMarks as m, mi (mi)}
              {@const fy = bucketY(m.idx) + bucketH / 2}
              <g class="flag-mark" aria-hidden="true">
                <circle cx={slot.bx + 8} cy={fy} r="5" class="flag-ring" />
                <circle cx={slot.bx + 8} cy={fy} r="1.8" class="flag-core" />
                <text class="anno bad-anno" x={slot.bx + 18} y={fy - 5}
                  >◉ {m.type}{m.n > 1 ? ` ×${m.n}` : ''} · {m.hm}</text>
              </g>
            {/each}
          </g>
        {/each}

        <!-- ══ peak + port labels, collision-culled across the rig ══ -->
        {#each peakLabels as p, pi (pi)}
          <text class="plab {p.lane}" x={p.x} y={p.y} text-anchor="middle">{p.text}</text>
        {/each}
        {#each portLabels as p (p.bandKey + p.port)}
          <text class="plab carrier-label {p.lane}" data-port={p.port} x={p.x} y={PORTLAB_Y} text-anchor="middle"
            >{p.text}</text>
        {/each}

        <!-- ══ flag horizons: the line through every band ══ -->
        {#each flagHorizons as f, fi (fi)}
          <line class="horizon" x1={RAIL} y1={f.y} x2={rig.width - 14} y2={f.y} />
          <!-- Round 30 draws every gutter time in the same quiet dim ink
               (the-whole.html #s2's `.gut`) -- no per-minute colouring
               and no mark beside a flagged minute's label, even though
               the horizon line through the bands still shows where it
               fired. This label used to ride `.flag-t` (alarm-coloured,
               bold, with a trailing ◉) as a leftover of the pre-round-30
               build; that read as if some minutes were flagged red/pink
               and others weren't, which round 30 never draws. -->
          <text class="tlab" x={RAIL - 14} y={f.y + 3} text-anchor="end">{f.hm}</text>
        {/each}

        <!-- ══ the NOW edge ══ -->
        <text class="now-caption" x={RAIL} y={NOW_Y - 5}
          >NOW · {nowClock()}</text>
        <line class="nowline" x1={RAIL} y1={NOW_Y} x2={rig.width - 14} y2={NOW_Y} />
        <circle class="now-dot" cx={rig.width - 18} cy={NOW_Y} r="2.5" />
      </svg>
    </div>

    <!-- Deep explanation never sits in the UI (round 30 README §5, itself
         restating round 5's ruling): a learned display explains itself
         once, in the docs. What remains on-screen is a tiny (i), well
         out of the way -- not the toggleable key-plus-legend the build
         had grown, which round 30 never draws. -->
    <div class="fall-foot">
      <button type="button" class="ibtn" title="How to read the fall — full explanation in the docs">i</button>
      <!-- The pager (#722): only drawn once there's more than one page --
           an estate that fits on one page never shows back/next arrows
           it has no use for. -->
      {#if totalPages > 1}
        <div class="pager" aria-label="Fall pages">
          <button
            type="button"
            class="pgbtn"
            aria-label="Previous page of boundaries"
            disabled={page === 0}
            onclick={() => (page = Math.max(0, page - 1))}>‹</button
          >
          <span class="pgnum">{page + 1} / {totalPages}</span>
          <button
            type="button"
            class="pgbtn"
            aria-label="Next page of boundaries"
            disabled={page === totalPages - 1}
            onclick={() => (page = Math.min(totalPages - 1, page + 1))}>›</button
          >
        </div>
      {/if}
      {#if WINDOW_RANGE_CAPTION_ENABLED}
        <span class="window-caption">
          {#if windowHasMore}showing the most recent 5,000 events; more exist ·
          {/if}{#if windowStart && windowEnd}{formatHM(new Date(windowStart).toISOString())} – {formatHM(
              new Date(windowEnd).toISOString(),
            )}, newest at the top{/if}
        </span>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* The mockup's palette, routed through the theme tokens so the fall
     keeps its identity in every theme block (#492 revisits). */
  .fall {
    --o-ink: var(--fg);
    --o-ink2: var(--fg-muted);
    --o-ink3: var(--fg-dim);
    --o-grid2: color-mix(in srgb, var(--fg) 15%, transparent);
    --o-acc: var(--fall-accept);
    --o-drop: var(--fall-drop);
    --o-nat: var(--fall-nat);
    --o-other: var(--fall-other);
    --o-ok: var(--accept);
    padding: 14px 20px 20px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    height: 100%;
    background: var(--fall-canvas);
  }

  /* ── the bar ─────────────────────────────────────────────────────── */
  .bar {
    display: flex;
    align-items: baseline;
    gap: 20px;
    flex-wrap: wrap;
  }
  .scname {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .epi {
    font-weight: 400;
    color: var(--o-ink3);
    font-size: 12px;
  }
  .status-cluster {
    margin-left: auto;
    display: flex;
    gap: 16px;
    align-items: center;
  }
  /* Round 30's `.wordmark`, ported field-for-field (same fix as
     SceneBar.svelte's `.wm`/`.wm em`, #683/#700): MIKRO in the bar's own
     near-white ink, not dimmed, with VIEW carrying the accent -- this
     bar draws its own wordmark instead of going through SceneBar (that
     is how it missed the shared fix the first time), so the values are
     restated here rather than dimly wide-tracked. */
  .wm {
    font-size: 15px;
    font-weight: 800;
    letter-spacing: 0.04em;
    color: var(--fg);
  }
  .wm em {
    color: var(--accent);
    font-style: normal;
  }
  .span-control {
    display: flex;
    gap: 2px;
  }
  /* Round 30's `.spans`/`.spans span`/`.spans .on` (the-whole.html #s2):
     a compact quiet cluster, the active one lifted only by a subtle
     filled pill -- not the oversized, wide-spaced buttons with a heavy
     amber underline this bar drew before (same class of miss as the
     wordmark above: its own control, not routed through the shared
     switcher). */
  .rng {
    background: transparent;
    border: 1px solid transparent;
    font: 10.5px var(--font-mono);
    color: var(--o-ink3);
    padding: 3px 9px;
    border-radius: 6px;
    cursor: pointer;
  }
  .rng:hover {
    color: var(--o-ink);
  }
  .rng.on {
    color: var(--o-ink);
    background: var(--bg-elevated);
    border-color: var(--border);
  }

  /* ── attention chips, riding the bar ─────────────────────────────── */
  /* Centred and fanning outward as chips are added (owner, ratified
     2026-08-31 -- see the template comment above): a full-width flex
     row with justify-content: center, not the inline/left-packed row
     the mockup's own two-chip example draws. */
  .attention {
    display: flex;
    justify-content: center;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }
  /* Sized to the mockup's `.fall-chip` (the-whole.html #s2): font,
     padding and pill height brought down from the oversized, loosely
     spaced pills this bar drew before (owner: "comically oversized"). */
  .att {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.05em;
    font-family: inherit;
    border: 1px solid var(--o-grid2);
    border-radius: 12px;
    padding: 3px 12px;
    color: var(--o-ink2);
    background: transparent;
  }
  button.att {
    cursor: pointer;
  }
  button.att:hover {
    background: var(--bg-hover);
  }
  .att i {
    width: 6px;
    height: 6px;
    border-radius: 50%;
  }
  .att.alarm {
    border-color: color-mix(in srgb, var(--o-drop) 50%, transparent);
    color: var(--o-drop);
  }
  .att.alarm i {
    background: var(--o-drop);
  }
  @media (prefers-reduced-motion: no-preference) {
    .att.alarm i {
      animation: fall-pulse 1.6s ease-in-out infinite;
    }
  }
  .att.dark {
    color: var(--o-drop);
  }
  .att.dark i {
    background: transparent;
    border: 1px solid var(--o-drop);
  }

  .state-msg {
    color: var(--o-ink2);
    font-size: 14px;
    padding: 20px 0;
  }
  .state-block {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    gap: 2px;
  }
  .state-block .state-msg {
    padding: 0;
    font-size: 15px;
  }
  .state-sub {
    margin: 0;
    max-width: 460px;
    color: var(--o-ink3, var(--o-ink2));
    font-size: 12.5px;
  }
  .state-msg.error {
    color: var(--o-drop);
  }

  /* ── the rig ─────────────────────────────────────────────────────── */
  .rig {
    flex: 1;
    min-height: 320px;
    display: flex;
    justify-content: center;
  }
  .rig svg {
    /* No width: 100% here (#722) -- the svg's own width/height
       attributes (set in the template from the sizing policy above) are
       its real size; `justify-content: center` on `.rig` centres it,
       which is how a stretch-capped small estate gets to leave empty
       space either side instead of being rescaled to fill the frame. */
    display: block;
    max-width: 100%;
  }
  .rig svg text {
    font-family: var(--font-mono);
  }

  .tlab {
    fill: var(--o-ink3);
    font-size: 11.5px;
  }
  .tlab.now-t {
    fill: var(--now);
  }
  .blab {
    fill: var(--o-ink);
    font-size: 13px;
    font-weight: 700;
    font-family: inherit;
  }
  .bsub {
    fill: var(--o-ink3);
    font-size: 10px;
    font-family: inherit;
  }
  .chip {
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.07em;
    font-family: inherit;
  }
  .ch-ok {
    fill: var(--o-ok);
  }
  .ch-bad {
    fill: var(--o-drop);
  }
  .ch-mut {
    fill: var(--o-ink3);
  }
  .band-head {
    cursor: pointer;
  }
  .head-hit {
    fill: transparent;
  }
  .band-head:hover .head-hit {
    fill: color-mix(in srgb, var(--fg) 5%, transparent);
  }
  .band-head:focus-visible {
    outline: none;
  }
  .band-head:focus-visible .head-hit {
    stroke: var(--accent);
    stroke-width: 1.5;
  }

  /* ── spectrum ────────────────────────────────────────────────────── */
  .spec {
    fill: none;
    stroke-width: 1.4;
  }
  .spec.accept {
    stroke: var(--o-acc);
  }
  .spec.drop {
    stroke: var(--o-drop);
  }
  .spec.nat {
    stroke: var(--o-nat);
  }
  .spec.other {
    stroke: var(--o-other);
  }
  .spec-floor {
    stroke: var(--o-grid2);
    stroke-width: 1;
  }
  .dark-baseline {
    stroke: var(--o-grid2);
    stroke-width: 1;
    stroke-dasharray: 2 4;
  }
  .plab {
    fill: var(--o-ink2);
    font-size: 10px;
  }
  .plab.accept {
    fill: var(--o-acc);
  }
  .plab.drop {
    fill: var(--o-drop);
  }
  .plab.nat {
    fill: var(--o-nat);
  }
  .plab.other {
    fill: var(--o-ink2);
  }

  /* ── the NOW edge ────────────────────────────────────────────────── */
  .nowline {
    stroke: var(--now);
    stroke-width: 1.6;
  }
  .now-caption {
    fill: var(--now);
    font-size: 12px;
    font-weight: 700;
  }
  .now-dot {
    fill: var(--now);
  }
  @media (prefers-reduced-motion: no-preference) {
    .nowline,
    .now-dot {
      animation: fall-pulse 1.4s ease-in-out infinite;
    }
  }
  @keyframes fall-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }

  /* ── the fall ────────────────────────────────────────────────────── */
  .ramp-hi {
    stop-color: color-mix(in srgb, var(--fall-drop) 16%, transparent);
  }
  .ramp-lo {
    stop-color: color-mix(in srgb, var(--fall-drop) 2%, transparent);
  }
  .glow-outer {
    fill: color-mix(in srgb, var(--o-acc) 10%, transparent);
  }
  .glow-inner {
    fill: color-mix(in srgb, var(--o-acc) 14%, transparent);
  }
  .mark.accept {
    fill: var(--o-acc);
    color: var(--o-acc);
  }
  .mark.drop {
    fill: var(--o-drop);
    color: var(--o-drop);
  }
  .mark.nat {
    fill: var(--o-nat);
    color: var(--o-nat);
  }
  .mark.other {
    fill: var(--o-other);
    color: var(--o-other);
  }
  .mark {
    filter: drop-shadow(0 0 1.6px currentColor);
  }
  .darkband {
    fill: url(#fall-hatch);
    opacity: 0.45;
  }
  .hatch-line {
    stroke: var(--o-grid2);
    stroke-width: 1.5;
  }
  .anno {
    fill: var(--o-ink3);
    font-size: 10.5px;
  }
  .anno.bad-anno {
    fill: var(--o-drop);
  }
  .anno.strong {
    font-weight: 700;
  }
  .horizon {
    stroke: var(--o-drop);
    stroke-width: 1;
    stroke-dasharray: 2 6;
    opacity: 0.55;
  }
  .flag-ring {
    fill: var(--fall-canvas);
    stroke: var(--o-drop);
    stroke-width: 1.4;
  }
  .flag-core {
    fill: var(--o-drop);
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

  .quieter {
    fill: var(--o-ink3);
    font-size: 10.5px;
    cursor: pointer;
  }
  .quieter:hover {
    fill: var(--o-ink);
  }
  .quieter:focus-visible {
    outline: none;
    fill: var(--accent);
  }

  /* ── the foot: the (i), the pager, and (when enabled) the window
     caption ──────────────────────────────────────────────────────────── */
  .fall-foot {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }
  /* The pager (#722): "a small back arrow and next arrow ... with an
     elegant page number between them" (owner, verbatim) -- `margin: 0
     auto` keeps it centred in the foot row regardless of whether the
     (i) button or the window caption either side of it are present. */
  .pager {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0 auto;
  }
  .pgbtn {
    width: 20px;
    height: 20px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    border: 1px solid var(--o-grid2);
    background: transparent;
    color: var(--o-ink3);
    font-size: 13px;
    line-height: 1;
    padding: 0;
    cursor: pointer;
  }
  .pgbtn:hover:not(:disabled) {
    color: var(--accent);
    border-color: var(--accent);
  }
  .pgbtn:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .pgnum {
    font: 11px var(--font-mono);
    letter-spacing: 0.04em;
    color: var(--o-ink3);
    min-width: 34px;
    text-align: center;
  }
  .ibtn {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 1px solid var(--o-grid2);
    background: transparent;
    color: var(--o-ink3);
    font: italic 600 11px Georgia, serif;
    padding: 0;
    cursor: pointer;
    line-height: 1;
  }
  .ibtn:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
  .window-caption {
    margin: 0;
    font-size: 11.5px;
    color: var(--o-ink3);
    font-family: var(--font-mono);
  }
</style>
