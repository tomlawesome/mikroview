<script module lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // #1090: one pass over the event buffer, building every tunnel
  // interface's own count at once, rather than tunnelCards calling a
  // per-interface filter (N full scans of the buffer for N tunnels).
  // Pure and in the module script so it is both the component's own
  // read and directly unit-testable (Topography.svelte.test.ts) without
  // a render. An event counts once per interface it touches, matching
  // the old `e.inInterface === iface || e.outInterface === iface` OR
  // check: if in and out are the same interface, that is one count.
  import type { ClientEvent } from '../lib/types'

  export function tunnelEventCounts(events: readonly ClientEvent[]): Map<string, number> {
    const counts = new Map<string, number>()
    for (const e of events) {
      const ifaces = new Set<string | undefined>([e.inInterface, e.outInterface])
      for (const iface of ifaces) {
        if (!iface) continue
        counts.set(iface, (counts.get(iface) ?? 0) + 1)
      }
    }
    return counts
  }
</script>

<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Topography, layer 1 of #485 (#627): the map alone, from what
  // mikroview already knows. Internet above, the router as the waist,
  // subnet lanes below -- fixed, deliberate positions, hand-rolled SVG,
  // never force-directed physics. One saturated colour stays reserved
  // for the alarm, which this layer never draws (that is layer 3's job).
  //
  // States per #626's ratified record: the place renders before the
  // data (frames first, traffic arrives into them), the empty state is
  // honest ("the map draws itself as traffic arrives" -- round 26's
  // first-hour beat), and while the /ip address table has not been
  // pushed the zones degrade to boundary-derived names, with the router
  // card carrying the one statement that names the missing push and
  // every address slot saying what it truly holds (#802, round 36 --
  // nothing floats over the map). There are no lens tabs any more
  // (round 49, #1016): traffic is the picture, coverage is the material
  // it is drawn in, and policy went in slice C. What is left of the row
  // is two overlay pills -- flags and watch -- that mark ledger objects
  // on the one picture (#715 item 3, round 49's own reduction).
  //
  // Coverage as material: a rib is two halves split at the pair's
  // midpoint, the half nearest an end carrying the direction leaving
  // that end, so one boundary reads logged one way and dark the other.
  // Logged draws in the verdict ink, quiet on purpose in white, dark in
  // grey dashes -- and no traffic is drawn across a dark or quiet
  // direction at all, because a line there would claim a log line that
  // was never written. Nothing is captioned that the material says.
  //
  // Deviation from #627's letter, declared on the issue: "the Map page
  // in the Live group's reserved slot" predates the deck -- topography
  // is a deck card (#633, rounds 20-29), and the reach (#626: a mode of
  // this scene, not a place) follows in its own change.
  import { appState } from '../lib/state.svelte'
  import { zonesState, type ZoneInfo } from '../lib/zones.svelte'
  import { tunnelsState } from '../lib/tunnels.svelte'
  // The tunnel node's state comes from the city's own derivation rather
  // than a second one beside it (#877): two readings of "is this tunnel
  // up" would eventually disagree, and #874 exists precisely so the app
  // stops guessing. If its shape ever stops suiting both drawings, it
  // moves out of city/ -- it does not get forked.
  import { bridgeStateFor, bridgeStateLabel } from '../lib/city/tunnelState'
  import { policyState, type PolicyEdge } from '../lib/policy.svelte'
  import { realityEdges, unexercisedIntents, worstUnplannedOf, type RealityEdge } from '../lib/reality'
  import { coverageState } from '../lib/coverage.svelte'
  import { edgeCoverage, type Coverage } from '../lib/coverageRule'
  import { composeCommand, reachComposeInput, refusingCommentFor } from '../lib/compose'
  import type { ReachStrand, ReachSubject } from '../lib/reach'
  import { hostSubject, portsLine, reachFor, reachLineSummary } from '../lib/reach'
  import { authState } from '../lib/auth.svelte'
  import { isPublicIp, formatHM, formatRelative } from '../lib/format'
  import { dossierState } from '../lib/dossier.svelte'
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import { logEveryRuleNavState } from '../lib/logEveryRuleNav.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import { familyOf, ADVISORY_INK } from '../lib/flagPalette'
  import { parseCidr, addressInCidr, type ParsedCidr } from '../lib/addressMatch'
  import { FLAG_TYPE_LABELS } from '../lib/metricsSeries'
  import { nightlySummary } from '../lib/watchWindow'
  import type { Flag, WatchlistEntry } from '../lib/types'
  import City from './City.svelte'
  import { STOPS, R2, flatFit, FX, FY, ease, reducedMotion } from '../lib/city/project'
  // The tunnel cluster's geometry (#890, round 52): pure arithmetic in
  // its own module, so the packing rule and the frame are unit-testable
  // without a component. lib/fit.ts is the fit rule the city's cityFitS
  // and this map's own frame now share rather than each writing it.
  import type { Box } from '../lib/fit'
  import {
    cardBox,
    packCluster,
    samplePath,
    topographyFrame,
    viewBoxOf,
    zoomPercent,
    type Placed,
  } from '../lib/topography/cluster'
  import { CIDR_FALLBACK, layoutGround, plateHalfWidth } from '../lib/city/layout'
  import { cityInputFrom, ghostCityZones } from '../lib/city/input'
  import { hostMarksFrom } from '../lib/city/presence'
  import {
    EXPECTED_LABEL,
    EXPECTED_ONE_LABEL,
    expectedAllLabel,
    expectedPlaceholder,
    expectedTargets,
    expectedWho,
    offersExpectedAll,
    type ExpectedScope,
  } from '../lib/city/expected'
  import type { District, Ground } from '../lib/city/types'
  import { ALTITUDE_LABELS, CENTRE_ALTITUDE, isCityAltitude, type Altitude } from '../lib/altitude'
  import { cardSize, drawnPathRects, drawnRect, drawnRects, grace, mapRect, placeCard, stageRect, unitMapper, watchCardSize, type Placement, type Rect } from '../lib/cardAnchor'
  import { altitudeStopState } from '../lib/altitudeStop.svelte'
  // Living hosts (#1016). The register is the source of presence, not
  // the event buffer: zonesState derives its host list from the lines
  // still in the buffer, so a machine that stops talking scrolls out and
  // vanishes -- which is the one thing presence must not do. The
  // server's register keeps the record instead, and hostsState is the
  // browser's view of it.
  import { hostsState, presenceOf, HOST_QUIET_AFTER_MS, type HostPresence } from '../lib/hosts.svelte'
  import { baselineState } from '../lib/baseline.svelte'
  // The two filters on the map (#1018, round 53). Neither is a new
  // view: they redraw this one. portFilter.ts holds the arithmetic and
  // the wording, the two stores hold what is selected and what the
  // server answered, and everything below only draws it.
  import { portFilterState } from '../lib/portFilter.svelte'
  import { mapTraceState } from '../lib/mapTrace.svelte'
  import TraceCrumb from './TraceCrumb.svelte'
  import {
    chooseDoorSpot,
    doorAccepts,
    doorHalf,
    doorTs,
    emptyNote,
    litRibs,
    parsePortList,
    ribKey,
    zoneTally,
  } from '../lib/portFilter'
  import type { OffBaselineLine } from '../lib/baseline'
  import type { Host } from '../lib/api'
  // The decommission ghost (#460, round 55): a segment the router has
  // stopped carrying, still drawn where it was until its watch retires.
  import DecommissionCard from './DecommissionCard.svelte'
  import { decommissionsState } from '../lib/decommission.svelte'
  import { GHOST_INK, ghostStateOf, ghostTally, retiredNote, stragglerCallout, undoable } from '../lib/decommission'
  import type { DecommissionOffer, DecommissionWatch, GhostState } from '../lib/types'

  // Five fixed lane inks. The fifth was --marked until #715 item 11 --
  // the ink this same screen uses for watchers, so one colour carried
  // two meanings. It has its own token now; see app.css for why olive.
  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--lane-5)']

  // The pushed /ip address table names the zones, the pushed rule
  // table draws the policy edges, and #874's tunnel tables give the
  // city's footbridges their state (#866); all refreshed whenever the
  // device list itself changes (it loads after mount).
  $effect(() => {
    if (appState.devices.length > 0) {
      zonesState.refresh()
      policyState.refresh()
      coverageState.refresh()
      tunnelsState.refresh()
      hostsState.refresh()
      // Today's off-baseline lines (#1016, round 49). Brightness is the
      // baseline, so this is an input to the drawing itself and not an
      // overlay -- without it every rib reads established, which is the
      // honest picture while the register is unread and the wrong one
      // the moment it lands.
      baselineState.refresh()
      // What has left the router and what is still being watched for
      // stragglers (#460). Refreshed with the rest rather than on its
      // own timer: an offer appears on a push, and a push is what moves
      // every other table here too.
      decommissionsState.refresh()
    }
  })

  const isAdmin = $derived(authState.role === 'admin')

  /** Component-unique prefix for this instance's SVG ids. */
  const uid = $props.id()

  // The two overlay pills are gone (#981, owner 2026-09-08): "something
  // that's always there is easy to ignore; if it's not always there you
  // know it's there for a reason." A mark exists while there is
  // something behind it and vanishes when there is not, on both
  // surfaces, and nothing switches it.

  // A retired boundary's lane is its ghost (#460, round 55): the ghost
  // stands exactly where the segment was, so the live lane for that
  // boundary steps aside rather than being drawn beside it.
  //
  // The two can genuinely coexist for a while -- zones are derived from
  // the event buffer, and a straggler is by definition traffic still
  // arriving on a range that has been retired -- and drawing both would
  // put the same segment on the map twice, once as a working lane and
  // once as the thing that says it is gone.
  const zones = $derived(zonesState.zones.filter((z) => !ghostLanes.some((g) => g.iface === z.id)))
  const eps = $derived(appState.stats?.eventsPerSecond ?? 0)

  /* ---------------- the ghost lanes (#460, round 55) ---------------- */

  // A lane for a segment that has left the router: offered and not yet
  // answered, or answered yes and still being watched.
  //
  // Round 55's second open question was decided by Fable on 2026-09-09:
  // the ghost stays exactly where the segment was and nothing reflows
  // around it -- its whole point is that the operator recognises the
  // place. The lane row is ordered busiest-first and a departed segment
  // has stopped talking, so its place is the end of the row, which is
  // also where the mockup draws it. The row itself re-spaces by count,
  // as it already does for any fifth lane (four at 285/577/848/1116,
  // five at 188/444/700/956/1212), so the ghost costs the live lanes
  // nothing but width.
  interface GhostLane {
    key: string
    device: string
    iface: string
    name: string
    cidr: string
    state: GhostState
    watch: DecommissionWatch | null
    offer: DecommissionOffer | null
    /** The last-known hosts, frozen at the moment the range went. */
    hosts: { label: string; ip: string }[]
  }

  // One dot per address, not one per table that named it: routerstate's
  // frozen snapshot is newest-named first and a single address can be in
  // it twice, once from the lease table and once from ARP. The first
  // entry wins, which is the newest name.
  const ghostHosts = (known: readonly { address: string; name?: string }[]) => {
    const seen = new Set<string>()
    const out: { label: string; ip: string }[] = []
    for (const k of known) {
      if (seen.has(k.address)) continue
      seen.add(k.address)
      out.push({ label: k.name || k.address, ip: k.address })
    }
    return out
  }

  const ghostLanes = $derived.by((): GhostLane[] => {
    const out: GhostLane[] = []
    for (const o of decommissionsState.offers) {
      out.push({
        key: `offer:${o.device}|${o.cidr}`,
        device: o.device,
        iface: o.interface,
        name: o.name || o.interface,
        cidr: o.cidr,
        state: 'none',
        watch: null,
        offer: o,
        hosts: ghostHosts(o.lastKnown ?? []),
      })
    }
    for (const w of decommissionsState.ghosts) {
      out.push({
        key: `watch:${w.id}`,
        device: w.device,
        iface: w.interface,
        name: w.name || w.interface,
        cidr: w.cidr,
        state: ghostStateOf(w, nowMs),
        watch: w,
        offer: null,
        hosts: ghostHosts(w.lastKnown ?? []),
      })
    }
    return out
  })

  // Lanes, live and ghost together. Every geometry answer -- the pitch,
  // the rib curves, the callout stops -- is asked of this rather than of
  // the live zones, so a ghost is spaced like a lane because it is one.
  const laneCount = $derived(zones.length + ghostLanes.length)

  // Which card is open on a ghost, and which ghost. The offer opens by
  // itself -- a segment leaving the router is the moment the offer is
  // about, and an offer nobody is shown is not an offer -- while the
  // ghost's own card and the straggler's are opened by pointing at them.
  let openGhostKey = $state<string | null>(null)
  let openStragglerKey = $state<string | null>(null)
  let ghostBusy = $state(false)
  let ghostError = $state<string | null>(null)

  // The offer answers itself first: while one is pending it is the card
  // on the map, because it is the only card here with a question in it.
  const pendingOffer = $derived(ghostLanes.find((g) => g.state === 'none') ?? null)
  const openGhostLane = $derived(ghostLanes.find((g) => g.key === openGhostKey) ?? null)
  const stragglerLane = $derived(
    ghostLanes.find((g) => g.key === openStragglerKey && g.state === 'broken' && g.watch?.lastStraggler) ?? null,
  )
  // The one ghost whose watch a straggler has just broken. One, not all:
  // the alarm arc and its callout are drawn for the line that needs
  // answering now, the same way the map escalates one unplanned pair
  // rather than every one of them.
  const brokenGhost = $derived(ghostLanes.find((g) => g.state === 'broken' && g.watch?.lastStraggler) ?? null)

  function openGhost(g: GhostLane) {
    ghostError = null
    openGhostKey = openGhostKey === g.key ? null : g.key
  }

  // Where a straggler's peer lives, so the alarm arc lands on the lane
  // the traffic actually reached. Null where no pushed address table
  // claims it -- the arc is not drawn at all then, rather than being
  // pointed at a lane that might not be the right one.
  function laneIndexForIp(ip: string | undefined): number | null {
    if (!ip) return null
    for (let i = 0; i < zones.length; i++) {
      const c = zones[i].cidr
      if (!c) continue
      const parsed = parseCidr(c)
      if (parsed && addressInCidr(ip, parsed)) return i
    }
    return null
  }

  // The friendliest name anything on this map has for an address. The
  // bare address where nothing named it -- never a guess.
  function nameForIp(ip: string | undefined): string {
    if (!ip) return ''
    for (const z of zones) {
      const h = z.hosts.find((x) => x.ip === ip)
      if (h?.label) return h.label
    }
    return ip
  }

  // One decommission card at a time, and which one is a precedence, not
  // a choice: a straggler the operator has just opened outranks the
  // ghost's own card, which outranks an offer waiting to be answered.
  // Round 55 never draws two, and two cards on one ghost would be the
  // map saying two things about the same place.
  const decommCard = $derived.by((): { kind: 'offer' | 'ghost' | 'straggler'; lane: GhostLane } | null => {
    if (stragglerLane) return { kind: 'straggler', lane: stragglerLane }
    if (openGhostLane && openGhostLane.watch) return { kind: 'ghost', lane: openGhostLane }
    if (pendingOffer) return { kind: 'offer', lane: pendingOffer }
    return null
  })

  let decommCardEl = $state<HTMLElement | null>(null)
  let decommCardPlace = $state<Placement | null>(null)
  let decommCardTick = $state(0)

  // Placed the same way every other card on this map is placed, so it
  // clears its own subject and the stage edge. The one difference round
  // 55 asks for is in the card itself: the leader is heavier, because a
  // ghost is faint and a hairline to a faint thing reads as nothing.
  $effect(() => {
    const open = decommCard
    const svg = mapSvgEl
    const host = topoEl
    const card = decommCardEl
    void altitude
    void stageTick
    void decommCardTick
    void laneCount

    if (!open || !svg || !host || !card || reach) {
      decommCardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      decommCardPlace = null
      return
    }
    const gi = ghostLanes.indexOf(open.lane)
    if (gi < 0) {
      decommCardPlace = null
      return
    }
    const gx = laneX(zones.length + gi, laneCount)
    const anchor = map({ x: gx, y: 486 })
    // The ghost's own plate is the one thing the card must not sit on:
    // it is what the card is about, and round 55 puts the card beside
    // the ghost for exactly that reason.
    const avoid = [mapRect(map, { x: gx - cardHalf, y: 466, w: cardW, h: 150 })]
    const softAvoid = zones.map((_z, i) => mapRect(map, { x: laneX(i, laneCount) - cardHalf, y: 466, w: cardW, h: 150 }))
    decommCardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid, prefer: ['right', 'left', 'top'] })
  })

  // The card grows when force-remove opens its confirm state, and a
  // placement worked out for the short card would leave the tall one
  // sitting on the ghost it is about (#1028's lesson, same fix).
  $effect(() => {
    const card = decommCardEl
    if (!card) return
    return watchCardSize(card, () => decommCardTick++)
  })

  async function answerOffer(lane: GhostLane, yes: boolean) {
    if (!lane.offer) return
    ghostBusy = true
    const err = yes ? await decommissionsState.accept(lane.offer) : await decommissionsState.dismiss(lane.offer)
    ghostBusy = false
    ghostError = err
  }

  async function forceRemoveGhost(lane: GhostLane, why: string) {
    if (!lane.watch) return
    ghostBusy = true
    const err = await decommissionsState.force(lane.watch.id, why)
    ghostBusy = false
    ghostError = err
    if (!err) openGhostKey = null
  }

  const primaryDevice = $derived.by(() => {
    const list = appState.devices
    if (list.length === 0) return null
    const configured = list.filter((d) => d.configured)
    const pool = configured.length > 0 ? configured : list
    return [...pool].sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0]
  })

  // The waist card's sub-line: round 30 draws
  // "RouterOS <version> · the waist · <N> rules"
  // (the-whole.html:978). Each end is dropped when it cannot be
  // answered, leaving "the waist" alone rather than a placeholder --
  // and "the waist" is always true, so the line never empties.
  //
  // The count is enabled pushed filter rules for this device only
  // (owner, 2026-09-03, #701 fact 2). Never pushed is null, not zero:
  // "0 rules" about a router with a full rule set would be a fact about
  // our own silence dressed as a fact about the network.
  //
  // The live events/s figure this line used to carry is gone. Round 30
  // draws no rate on this node, and #715 item 7 is that divergence; the
  // rate is still on the scene bar and in Metrics, so nothing is lost.
  const waistRuleCount = $derived(policyState.enabledRuleCount(primaryDevice?.id))
  const waistSub = $derived.by(() => {
    const parts: string[] = []
    if (primaryDevice?.routerosVersion) parts.push(`RouterOS ${primaryDevice.routerosVersion}`)
    parts.push('the waist')
    if (waistRuleCount !== null) parts.push(`${waistRuleCount.toLocaleString()} ${waistRuleCount === 1 ? 'rule' : 'rules'}`)
    return parts.join(' · ')
  })

  // Lane geometry (#699). The old spread pinned the first and last lane
  // to x=285 and x=1116 whatever N was, so at five lanes the pitch fell
  // to 207.75 against a 216-wide card and the cards overlapped. Round 30
  // draws four lanes at 285/577/848/1116 -- a 268-292 pitch against the
  // same card -- so the *pitch* is what the drawing fixes, not the span.
  // The row therefore keeps that pitch and centres on the waist, and
  // shrinks card and pitch together (never one without the other) only
  // when N lanes would not otherwise fit the stage. Cards cannot overlap
  // at any N. zones.svelte.ts caps the list at five today, which the
  // full pitch still fits (50..1350 on a 1400 stage), so the shrink is
  // the guard for that cap moving rather than something drawn now.
  const LANE_CARD_W = 216
  const LANE_PITCH = 271
  const LANE_GAP = LANE_PITCH - LANE_CARD_W
  const STAGE_W = 1400
  const STAGE_INSET = 24

  const laneScale = $derived.by(() => {
    // Ghost lanes are counted here too: a ghost is a lane the row has to
    // make room for, and leaving it out would let the cards overlap at
    // exactly the moment a fifth lane appears.
    const n = laneCount
    if (n < 2) return 1
    const want = n * LANE_CARD_W + (n - 1) * LANE_GAP
    const room = STAGE_W - 2 * STAGE_INSET
    return want <= room ? 1 : room / want
  })
  const lanePitch = $derived(LANE_PITCH * laneScale)
  const cardW = $derived(LANE_CARD_W * laneScale)
  const cardHalf = $derived(cardW / 2)
  /** The card's own text inset, round 30's (x=-90 against a -108 edge). */
  const cardPad = $derived(18 * laneScale)

  function laneX(i: number, n: number): number {
    if (n <= 1) return 700
    return 700 + (i - (n - 1) / 2) * lanePitch
  }

  /* ---------------- living hosts (#1016) ---------------- */

  // Round 49 draws the lane card's hosts as a row of dots rather than a
  // list of names: ten dots then `+N`, every dot one host and clickable
  // to its reach, with a count line under them (round-49/index.html's
  // node2, and DESIGN.md "Living hosts"). The names went because a dot
  // row says how many machines a lane has and which of them are quiet --
  // three clipped names said neither, and #715 item 10 had already
  // struck the per-name dot the old list grew.
  const MAX_HOST_DOTS = 10
  // The mockup's own geometry at the 216-wide card: the row starts on
  // the card's own left inset, 17 apart, r 4.5, baseline y 56. Only the
  // pitch scales with the card -- everything vertical in this card is
  // unscaled, and a narrower lane must still fit ten dots and the `+N`.
  const HOST_DOT_PITCH = 17
  const HOST_DOT_Y = 56
  const hostDotPitch = $derived(HOST_DOT_PITCH * laneScale)
  const hostDotR = $derived(Math.min(4.5, hostDotPitch / 2 - 1))

  /** Where a dot sits inside its lane card, in the card's own space. */
  function hostDotX(i: number): number {
    return -cardHalf + cardPad + i * hostDotPitch
  }

  /** One drawn host: what the dot is, and what its card would say. */
  interface HostDot {
    key: string
    label: string
    ip: string
    presence: HostPresence
    /** The register's record, absent for a host only the buffer knows. */
    host: Host | null
  }

  interface HostRow {
    dots: HostDot[]
    /** The hosts past the tenth dot -- what `+N` stands for. Kept, not
     * just counted, so a filter can say how many of *them* are in its
     * answer from the same list the row was built from (#1018). */
    hidden: HostDot[]
    more: number
    total: number
    quiet: number
    intended: number
  }

  // Presence is a function of the clock, so it has to be recomputed
  // without the server saying anything. A minute is far finer than the
  // 24-hour window needs and costs nothing.
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 60_000)
    return () => clearInterval(t)
  })

  /** The configured window where there is one, else the 24-hour default. */
  const quietAfterMs = $derived(baselineState.hostQuietAfterMs > 0 ? baselineState.hostQuietAfterMs : HOST_QUIET_AFTER_MS)

  const hostsByIface = $derived.by(() => {
    const m = new Map<string, Host[]>()
    for (const h of hostsState.hosts) {
      const at = m.get(h.iface)
      if (at) at.push(h)
      else m.set(h.iface, [h])
    }
    return m
  })

  // A zone's id is its boundary interface, and the register keys a host
  // `"<iface>|<ip>"` (internal/hosts.KeyFor), so the two line up without
  // a second index in between.
  function laneHostRow(z: ZoneInfo): HostRow {
    const out: HostDot[] = []
    const seen = new Set<string>()
    for (const h of hostsByIface.get(z.id) ?? []) {
      const presence = presenceOf(h, nowMs, quietAfterMs)
      // Dismissed is "take this off my map", so it leaves the row --
      // and comes back by itself, because the server drops the mark the
      // moment the host speaks again (internal/hosts.Register.Observe).
      if (presence === 'dismissed') continue
      seen.add(h.ip)
      out.push({ key: h.key, label: h.label ?? h.ip, ip: h.ip, presence, host: h })
    }
    // Anything the buffer has seen that the register has not answered
    // for yet -- the first paint, before GET /api/hosts lands. Being in
    // the buffer *is* evidence of having just been heard, so live is not
    // a guess here; it is the same fact the register will confirm.
    for (const h of z.hosts) {
      if (seen.has(h.ip)) continue
      seen.add(h.ip)
      out.push({ key: `${z.id}|${h.ip}`, label: h.label, ip: h.ip, presence: 'live', host: null })
    }
    // Sorted by key, which is the register's own order: stable, so a dot
    // does not move under the pointer when one host's presence changes.
    out.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0))
    return {
      dots: out.slice(0, MAX_HOST_DOTS),
      hidden: out.slice(MAX_HOST_DOTS),
      more: Math.max(0, out.length - MAX_HOST_DOTS),
      total: out.length,
      quiet: out.filter((d) => d.presence === 'quiet').length,
      intended: out.filter((d) => d.presence === 'intended').length,
    }
  }

  /** The count line under the dots. Zero clauses are not drawn: "0
   * quiet" is not a thing to report. */
  function hostTally(r: HostRow): string {
    const parts = [`${r.total} host${r.total === 1 ? '' : 's'}`]
    if (r.quiet > 0) parts.push(`${r.quiet} quiet`)
    if (r.intended > 0) parts.push(`${r.intended} quiet on purpose`)
    return parts.join(' · ')
  }

  /** A span in the card's own wording -- the `26 h` of DESIGN.md's
   * `quiet · 26 h`, and the `24 h` of the window beside it. */
  function spanLabel(ms: number): string {
    const mins = Math.max(0, Math.floor(ms / 60_000))
    if (mins < 60) return `${mins} m`
    const hrs = Math.floor(mins / 60)
    if (hrs < 48) return `${hrs} h`
    return `${Math.floor(hrs / 24)} d`
  }

  /** How long a host has been silent. An unparseable stamp says so
   * rather than guessing a number off it. */
  function quietFor(lastSeen: string, now: number): string {
    const t = Date.parse(lastSeen)
    if (Number.isNaN(t)) return 'an unknown time'
    return spanLabel(now - t)
  }

  /** The dot's own tooltip, and the accessible name of its button. */
  function hostDotLabel(d: HostDot): string {
    if (d.presence === 'intended') return `${d.label} · quiet on purpose`
    if (d.presence === 'quiet' && d.host) return `${d.label} · quiet · ${quietFor(d.host.lastSeen, nowMs)}`
    return d.label
  }

  // Shared by ribPath and the internet-edge limbs (#726: "bundle the
  // corridor, fan at the waist") so a rib and its edge cannot drift
  // apart -- both draw the same slot for the same lane.
  function slotSpread(i: number, n: number): number {
    return n === 1 ? 0 : -55 + (110 / (n - 1)) * i
  }

  /** A lane rib's own four control points -- the one place its shape is
   * written, so the drawn path and the box the tunnel cluster packs
   * around it cannot drift apart (#890). */
  function ribCurve(i: number, n: number): [[number, number], [number, number], [number, number], [number, number]] {
    const x = laneX(i, n)
    const spread = slotSpread(i, n)
    return [
      [700 + spread, 302],
      [700 + spread * 2.2, 380],
      [x + (700 - x) * 0.25, 420],
      [x, 480],
    ]
  }

  function ribPath(i: number, n: number): string {
    const c = ribCurve(i, n)
    return `M ${c[0][0]} ${c[0][1]} C ${c[1][0]} ${c[1][1]}, ${c[2][0]} ${c[2][1]}, ${c[3][0]} ${c[3][1]}`
  }

  // The stream, filtered to a zone's own boundary -- what the reach's
  // own `stream ▸` leads to when the subject is a zone. The whole map
  // never navigates on a miss.
  function openZone(id: string) {
    appState.setFilter('interface', id)
    appState.view = 'live'
  }

  // --- the edge geometry (#628: layer 2) -----------------------------------
  // Every crossing passes the router, so every edge routes through the
  // waist -- and a refusal dies there, ⊣, the same grammar the reach's
  // membrane already taught. Calm ink throughout: a refusal by the
  // pushed table is intent, not the alarm.
  const WAIST = { x: 700, y: 312 }
  const EDGE_CAP = 12

  // `idx` is only set for 'zone' anchors -- an internet edge's own slot
  // is its zone end's lane index (#726), so it rides along with the
  // anchor rather than being re-derived from the edge's position.
  type EdgeAnchor = { x: number; y: number; kind: 'zone' | 'internet' | 'tunnel' | 'any'; idx?: number }

  // Where the tunnels stand, and where their lines leave them, is
  // lib/topography/cluster.ts's now (#890): round 30's single seat
  // (the-whole.html:986, `translate(1128 132)`) is the first card's,
  // and every other is packed around it. The card is drawn
  // asymmetrically about its own point -- -84 to +104 -- exactly as the
  // mockup does.

  function anchorOf(iface: string): EdgeAnchor | null {
    if (iface === '') return { ...WAIST, kind: 'any' }
    if (iface === zonesState.wanInterface) return { x: 700, y: 104, kind: 'internet' }
    // Before the lane row is consulted: the tunnel left it (#877), and
    // an edge that used to find its lane card must now find the node
    // rather than fall through to null and be dropped silently.
    const t = drawnTunnels.find((d) => d.iface === iface)
    if (t) return { x: t.placed.anchor.x, y: t.placed.anchor.y, kind: 'tunnel' }
    const i = zones.findIndex((z) => z.id === iface)
    if (i === -1) return null
    return { x: laneX(i, laneCount), y: 484, kind: 'zone', idx: i }
  }

  // A line between two anchors, shared by both lenses: the Traffic lens
  // draws what actually happened along it, the Coverage lens draws what
  // the pushed table logs (#629). `crosses` is whether it arrives or
  // dies at the waist.
  interface Line {
    from: EdgeAnchor
    to: EdgeAnchor
    /** Perpendicular offset splitting the two directions of a pair. */
    off: { x: number; y: number }
    crosses: boolean
  }

  const SPLIT = 7
  // An edge to "anywhere" ends on the waist itself, which is the point
  // every crossing edge is pulled through -- so at the ordinary split it
  // runs along that same lane's edge to the internet, and neither line
  // can be followed (#726: measured, 0.30 of the run within 4 units).
  // It keeps its anchor and takes a wider lane instead, so it still
  // reads as leaving the same island toward the same waist.
  //
  // 26, not the 13 this started at: once internet edges landed on their
  // own slots (#726's bundle decision) the lane holding the middle slot
  // ends its limb 10 units from the waist, which is where an "anywhere"
  // edge dies -- so the old clearance smeared those two together again
  // for that one lane. The gate caught it on the real map. Measured, the
  // fault clears at 15 and holds from 22 up; 26 keeps a margin without
  // reading as a line detached from its own island.
  const ANY_CLEAR = 26

  function lineFor(fromIface: string, toIface: string, crosses: boolean): Line | null {
    const from = anchorOf(fromIface)
    const to = anchorOf(toIface)
    // A pair whose boundary the map has no island for (no address push
    // named it, nothing spoke on it) cannot be drawn honestly.
    if (!from || !to || (from.kind === 'any' && to.kind === 'any')) return null
    // Round 49: the two directions of a pair no longer split to either
    // side of a shared line -- they are the two halves of one rib, so
    // they must be the same curve. The offset is taken from the pair's
    // canonical order (the two boundary names sorted), which is the
    // same vector whichever way round the direction is drawn. Its
    // magnitude is unchanged, so an edge to "anywhere" still clears the
    // same lane's edge to the internet by ANY_CLEAR (#726).
    const [ax, bx] = fromIface <= toIface ? [from, to] : [to, from]
    const dx = bx.x - ax.x
    const dy = bx.y - ax.y
    const len = Math.hypot(dx, dy) || 1
    const spread = from.kind === 'any' || to.kind === 'any' ? SPLIT + ANY_CLEAR : SPLIT
    return { from, to, off: { x: (-dy / len) * spread, y: (dx / len) * spread }, crosses }
  }

  interface DrawnEdge {
    edge: PolicyEdge
    line: Line
  }

  // #726 ("bundle the corridor, fan at the waist"): an edge whose far
  // end is the internet no longer runs through the single WAIST point --
  // it draws only the limb between its lane's own slot and the router
  // card, and the corridor above carries one shared trunk instead. An
  // edge to "anywhere" (kind 'any') is unaffected -- it still crosses at
  // WAIST, per ANY_CLEAR above.
  function isInternetEdge(l: Line): boolean {
    return (l.from.kind === 'internet' && l.to.kind === 'zone') || (l.from.kind === 'zone' && l.to.kind === 'internet')
  }

  // The lane end's own slot -- shared by the limb, its death point and
  // its badge, so all three agree on where a given lane's internet edge
  // rides. Keyed off the lane's index in `zones`, never the edge's
  // position in the edge list.
  function internetSlotSpread(l: Line): number {
    const laneAnchor = l.from.kind === 'zone' ? l.from : l.to
    return slotSpread(laneAnchor.idx ?? 0, laneCount)
  }

  // A refusal dies on the waist's near side, so its bar is never behind
  // the island: arriving from the internet it dies at the top edge,
  // from a lane at the bottom. An internet edge's death point takes its
  // own lane's slot on the x axis rather than the shared waist x, so two
  // refusals to different lanes no longer coincide (#726).
  function deathPoint(l: Line): { x: number; y: number } {
    const x = isInternetEdge(l) ? 700 + internetSlotSpread(l) : WAIST.x
    return { x: x + l.off.x, y: (l.from.y < 268 ? 226 : WAIST.y) + l.off.y }
  }

  // The visible line: a cubic pulled through the waist, dying there, or
  // (for an internet edge) the limb alone -- ribPath's own cubic,
  // reversed, landing on the lane's slot rather than the waist (#726).
  // Drawn from cubicOf/quadOf's own points rather than a second
  // computation of the same curve (#1053) -- two copies of "where the
  // waist handles sit" is exactly how the lateral-rib hook fix could
  // have landed in only one of them.
  function edgePath(l: Line): string {
    const c = cubicOf(l)
    if (c) return `M ${c[0].x} ${c[0].y} C ${c[1].x} ${c[1].y}, ${c[2].x} ${c[2].y}, ${c[3].x} ${c[3].y}`
    const q = quadOf(l)
    return `M ${q[0].x} ${q[0].y} Q ${q[1].x} ${q[1].y}, ${q[2].x} ${q[2].y}`
  }

  /** A line that dies at the waist, as its three quadratic points --
   * pulled out of edgePath so the leader's own anchor is a point on the
   * curve actually drawn, rather than a second guess at where it runs. */
  function quadOf(l: Line): [Pt, Pt, Pt] {
    const { from, off } = l
    const dp = deathPoint(l)
    return [
      { x: from.x + off.x, y: from.y + off.y },
      { x: (from.x + dp.x) / 2 + off.x, y: (from.y + dp.y) / 2 + off.y },
      { x: dp.x, y: dp.y },
    ]
  }

  // --- a rib is two halves (round 49) --------------------------------------
  // The pair draws one curve; the half nearest an end carries the
  // direction leaving that end. Both directions of a pair therefore
  // have to agree on the curve itself, which is why `lineFor` takes the
  // perpendicular offset from the pair's canonical order rather than
  // from the direction being drawn -- A→B and B→A are then the same
  // cubic, walked from opposite ends, and each direction's own first
  // half is the half nearest its own island.
  //
  // Splitting a cubic at t=0.5 is de Casteljau, exactly the mockup's
  // `bezRange` (round-49/index.html:563). A line that dies at the waist
  // is not split: it already runs from its own end to the middle, so it
  // is that direction's half.
  type Pt = { x: number; y: number }

  function cubicOf(l: Line): [Pt, Pt, Pt, Pt] | null {
    if (!l.crosses) return null
    const { from, to, off } = l
    const at = (p: Pt): Pt => ({ x: p.x + off.x, y: p.y + off.y })
    if (isInternetEdge(l)) {
      const spread = internetSlotSpread(l)
      const laneAnchor = from.kind === 'zone' ? from : to
      const waistPt = { x: 700 + spread, y: 302 }
      const laneCtrl = { x: laneAnchor.x + (700 - laneAnchor.x) * 0.25, y: 420 }
      const waistCtrl = { x: 700 + spread * 2.2, y: 380 }
      return from.kind === 'zone'
        ? [at(laneAnchor), at(laneCtrl), at(waistCtrl), at(waistPt)]
        : [at(waistPt), at(waistCtrl), at(laneCtrl), at(laneAnchor)]
    }
    // A lateral pair -- both ends on the same side of the waist, like two
    // adjacent zone lanes (LAN <-> Servers) -- used to pull both handles
    // onto the waist point itself (`w, w` below). With both handles on
    // one point the curve overshot past the nearer end's own x and
    // doubled back into a small hook right where it turned (#1053).
    // Handles that stop partway to the pair's own midpoint keep the bend
    // inside the two ends' own x-range -- still rising toward the
    // waist's height, never past either end -- while a pair that
    // straddles the waist still meets there exactly, unchanged below.
    if ((from.x - WAIST.x) * (to.x - WAIST.x) > 0) {
      const midX = (from.x + to.x) / 2
      // 0.6: far enough toward the midpoint/waist height to read as a
      // real bend rather than a near-straight line; the unit test below
      // is what actually guards against a reversal creeping back in.
      const PULL = 0.6
      const c1 = at({ x: from.x + (midX - from.x) * PULL, y: from.y + (WAIST.y - from.y) * PULL })
      const c2 = at({ x: to.x + (midX - to.x) * PULL, y: to.y + (WAIST.y - to.y) * PULL })
      return [at(from), c1, c2, at(to)]
    }
    const w = at(WAIST)
    return [at(from), w, w, at(to)]
  }

  const mid = (a: Pt, b: Pt): Pt => ({ x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 })

  /** The half of this direction's own line nearest its own island, as
   * its own four control points -- pulled out of halfPath (#1018) so a
   * door's posts and a traced half sit on the curve that is actually
   * drawn rather than on a second guess at where it runs, the same
   * reason quadOf exists for the leader's anchor. */
  function halfCubic(l: Line): [Pt, Pt, Pt, Pt] | null {
    const c = cubicOf(l)
    if (!c) return null
    const [p0, p1, p2, p3] = c
    const a = mid(p0, p1)
    const b = mid(p1, p2)
    const cc = mid(p2, p3)
    const d = mid(a, b)
    const e = mid(b, cc)
    return [p0, a, d, mid(d, e)]
  }

  /** The half of this direction's own line nearest its own island. */
  function halfPath(l: Line): string {
    const h = halfCubic(l)
    if (!h) return edgePath(l)
    return `M ${R2(h[0].x)} ${R2(h[0].y)} C ${R2(h[1].x)} ${R2(h[1].y)}, ${R2(h[2].x)} ${R2(h[2].y)}, ${R2(h[3].x)} ${R2(h[3].y)}`
  }

  /** The middle of the half actually drawn: where this boundary's card
   * puts its accent dot, and where its leader starts (round 49). */
  function halfMid(l: Line): Pt {
    const c = cubicOf(l)
    if (!c) {
      const [a, b, cc] = quadOf(l)
      return { x: (a.x + 2 * b.x + cc.x) / 4, y: (a.y + 2 * b.y + cc.y) / 4 }
    }
    // The same half halfPath draws, then that cubic's own midpoint:
    // (p0 + 3c1 + 3c2 + p3) / 8.
    const [p0, a, d, f] = halfCubic(l) ?? c
    return { x: (p0.x + 3 * a.x + 3 * d.x + f.x) / 8, y: (p0.y + 3 * a.y + 3 * d.y + f.y) / 8 }
  }

  /**
   * Where an off-baseline rib's ring goes: the end the traffic arrived
   * at, which is the last point of this direction's own curve.
   *
   * The ring throbs *in place*, hugging the shape -- it never pulses
   * outward (owner, 2026-09-07): a travelling ring reads as something
   * moving through the network, and nothing here moved.
   *
   * Null where the arrival end is the waist itself, which is every line
   * toward the internet and every line to "anywhere": the drawing stops
   * at the router there, so a ring would sit on the waist card naming no
   * island at all. The mockup skips exactly these (round-49/index.html
   * :1220, `r[1] !== 'rb'`).
   */
  function ringPoint(l: Line): Pt | null {
    const c = cubicOf(l)
    if (!c) return null
    if (l.to.kind === 'any') return null
    if (isInternetEdge(l) && l.from.kind === 'zone') return null
    return c[3]
  }

  /** The island a line's end sits on, as a box in the map's own units:
   * what a card anchored to that line must not cover. The numbers are
   * the `.isl` rects drawn below, offset by their groups' transforms. */
  function islandRect(a: EdgeAnchor): Rect {
    if (a.kind === 'internet') return { x: 600, y: 38, w: 200, h: 60 }
    if (a.kind === 'tunnel') return { x: 1044, y: 104, w: 188, h: 56 }
    if (a.kind === 'zone') return { x: a.x - cardHalf, y: 484, w: cardW, h: 106 }
    return { x: 572, y: 234, w: 256, h: degradedStatement ? 100 : 68 }
  }

  // Where the ⊣ bar and the badges sit for a line.
  function edgeBarAt(l: Line): { x: number; y: number; angle: number } {
    const dp = deathPoint(l)
    return { x: dp.x, y: dp.y, angle: (Math.atan2(dp.y - l.from.y, dp.x - l.from.x) * 180) / Math.PI + 90 }
  }

  // Badges near the internet corridor stack when several edges share
  // it, so crossing badges stagger along their line by draw order.
  // Every lane's edge toward the internet runs up the *same* waist-to-
  // internet segment, so the stagger is the only thing separating them:
  // an even 0.12 step spreads six labels about 25 map units apart
  // against a 14-unit plate, where the old five hand-picked fractions
  // left neighbouring pairs a couple of units apart (#699).
  // The range is the free corridor between the two islands the crossing
  // lines run through: the internet island's bar ends at y=118 and the
  // waist island starts at y=234, and the islands paint over the edges,
  // so a plate outside that band is a label nobody can read.
  const BADGE_FRAC_0 = 0.42
  const BADGE_FRAC_STEP = 0.09
  const BADGE_FRAC_N = 6

  // The unit vector perpendicular to (dx, dy): pushes a badge off to
  // the side of the line it annotates rather than centred on top of
  // it (#682 -- labels were reading as if printed across their own
  // edges).
  function perp(dx: number, dy: number): { x: number; y: number } {
    const len = Math.hypot(dx, dy) || 1
    return { x: -dy / len, y: dx / len }
  }

  const BADGE_CLEAR = 11

  function edgeBadgeAt(l: Line, i = 0): { x: number; y: number } {
    const { from, to, off } = l
    const side = off.x !== 0 || off.y !== 0 ? Math.sign(off.x || off.y) : 1
    if (!l.crosses) {
      // Beside the bar, pushed back toward the source and clear of the
      // opposite direction's line -- then off the line itself.
      const dp = deathPoint(l)
      const dx = from.x - dp.x
      const dy = from.y - dp.y
      const len = Math.hypot(dx, dy) || 1
      const p = perp(dx, dy)
      // Staggered by draw order, the same reason BADGE_FRACS staggers
      // the crossing ones: several pairs die at the same waist from the
      // same direction, so a fixed pushback stacked their labels on one
      // spot. Invisible while a label was bare text over a bare line;
      // once every label sits on an opaque plate (#699) the top one
      // hides the rest, so the stagger has to be real.
      const back = 20 + (i % 4) * 24
      return {
        x: dp.x + (dx / len) * back + off.x * 4.2 + p.x * BADGE_CLEAR * side,
        y: dp.y + (dy / len) * back + off.y * 4.2 + p.y * BADGE_CLEAR * side,
      }
    }
    if (isInternetEdge(l)) {
      // On the limb itself, measured from the waist end -- the corridor
      // stagger above has nothing left to separate now that each lane
      // has its own slot (#726).
      const spread = internetSlotSpread(l)
      const waistPt = { x: 700 + spread, y: 302 }
      const laneAnchor = from.kind === 'zone' ? from : to
      const dx = laneAnchor.x - waistPt.x
      const dy = laneAnchor.y - waistPt.y
      const f = 0.35 + (i % 4) * 0.13
      const p = perp(dx, dy)
      return {
        x: waistPt.x + dx * f + off.x * 4.2 + p.x * BADGE_CLEAR * side,
        y: waistPt.y + dy * f + off.y * 4.2 + p.y * BADGE_CLEAR * side,
      }
    }
    // Past the waist, toward the destination, on its own side -- and
    // off the line itself.
    const f = BADGE_FRAC_0 + (i % BADGE_FRAC_N) * BADGE_FRAC_STEP
    const p = perp(to.x - WAIST.x, to.y - WAIST.y)
    return {
      x: WAIST.x + (to.x - WAIST.x) * f + off.x * 4.2 + p.x * BADGE_CLEAR * side,
      y: WAIST.y + (to.y - WAIST.y) * f + off.y * 4.2 + p.y * BADGE_CLEAR * side,
    }
  }

  // Round 30 ratified a general rule -- nothing sits on a line as text
  // only, without a box (the-whole.html:941-944 boxes its own edge
  // label). #682's backdrop-coloured halo holds over empty map but not
  // over a bright edge, where the label then reads as ink scattered
  // across the line. Every badge sits on a real plate now, sized from
  // its own string: the badge face is monospace at 9.5px, so a character
  // is a known width and no measuring pass is needed.
  const BADGE_CH = 5.72
  const PLATE_PAD = 6

  function plateW(text: string): number {
    return text.length * BADGE_CH + 2 * PLATE_PAD
  }

  /** A badge's resolved plate: centre, the plate's own width, and its
   * height where that is not the ordinary one-line plate. The escalated
   * unplanned card (#715 item 4) is two lines and 40 tall, and a
   * layout that assumed every plate was PLATE_H would let ordinary
   * badges settle inside it. */
  interface PlacedBadge {
    x: number
    y: number
    w: number
    h?: number
  }

  const PLATE_H = 14
  const PLATE_CLEAR = 3
  /** The escalated card's own box, ported from the-whole.html:941 --
   * a 40-tall rect with the mockup's 12-unit left inset. */
  const CARD_H = 40
  const CARD_PAD = 26

  // The corridor is the free band between the two islands every crossing
  // line runs through: the internet island's own bar ends at y=118 and
  // the waist island starts at y=234. The islands paint over the edges,
  // so a plate outside that band is a label nobody can read.
  const CORRIDOR_TOP = 132
  const CORRIDOR_FLOOR = 228
  const CORRIDOR_SLOT = 18
  /** Half the gap the two corridor stacks leave down the centre line. */
  const CORRIDOR_GUTTER = 8

  /**
   * placeBadges lays a lens's labels out so no two plates cover each
   * other, and none lands under an island. Staggering along the lines
   * cannot do it on its own: every lane's edge toward the internet runs
   * up the *same* waist-to-internet segment, so several plates land in
   * one corridor whatever fractions they are given -- and an opaque
   * plate (#699) hides whatever it covers, where the old bare text
   * merely read as a tangle.
   *
   * Two rules, both deterministic. Anything in the corridor takes a slot
   * in one of the two stacks either side of the waist's centre line,
   * filled from the floor up and squared off against that line, keeping
   * its own vertical order -- squared off because the plates are wider
   * than the gap between the two stacks' natural centres, so leaving
   * them where the line put them puts one column's plate through the
   * other's. Anything else -- the labels out by the lanes, where there
   * is room -- is pushed clear of whatever it would have covered. A
   * label with no text takes no space, so a lens that badges only some
   * of its edges still keeps its indices aligned.
   */
  function placeBadges(items: { line: Line; text: string; dy?: number }[], reserved?: PlacedBadge): PlacedBadge[] {
    const raw = items.map((it, i) => {
      const p = edgeBadgeAt(it.line, i)
      return { x: p.x, y: p.y + (it.dy ?? 0), w: plateW(it.text), text: it.text }
    })
    const inCorridor = (b: { x: number; y: number; text?: string }) =>
      b.text !== '' && b.y > 100 && b.y < 310 && Math.abs(b.x - WAIST.x) < 170

    // The escalated card is placed by the data, not by this pass, and
    // never moved by it. It goes in first so everything else dodges it
    // -- an opaque two-line card with a badge settled inside it is the
    // exact failure the corridor rules exist to prevent (#715 item 4).
    const settled: PlacedBadge[] = []
    const cardSide = reserved ? (reserved.x < WAIST.x ? -1 : 1) : 0
    const cardInCorridor = reserved ? inCorridor({ ...reserved, text: 'card' }) : false
    if (reserved) settled.push(reserved)

    // A plate's box hangs from its anchor: y-10 to y-10+h. So two
    // boxes clear each other on their centres, not on their anchors --
    // which are the same thing only while every plate is the same
    // height, as they were before the escalated card (#715 item 4).
    // For two ordinary plates this is exactly the old arithmetic.
    const centreOf = (b: { y: number; h?: number }) => b.y - 10 + (b.h ?? PLATE_H) / 2

    for (const side of [-1, 1]) {
      const col = raw.filter((b) => inCorridor(b) && (b.x < WAIST.x ? -1 : 1) === side).sort((a, b) => b.y - a.y)
      // A card holding this side's floor pushes the whole stack above
      // it, rather than letting the first badge land inside it.
      const floor = cardInCorridor && side === cardSide ? CORRIDOR_FLOOR - (CARD_H - PLATE_H) - PLATE_H - PLATE_CLEAR : CORRIDOR_FLOOR
      const slot = Math.min(CORRIDOR_SLOT, (floor - CORRIDOR_TOP) / Math.max(1, col.length - 1))
      col.forEach((b, k) => {
        b.y = floor - k * slot
        b.x = WAIST.x + side * (CORRIDOR_GUTTER + b.w / 2)
        settled.push({ x: b.x, y: b.y, w: b.w })
      })
    }

    for (const b of raw.filter((b) => b.text !== '' && !inCorridor(b)).sort((a, b) => a.y - b.y)) {
      // At most one push per already-settled plate: each pass clears one
      // conflict, and there are finitely many of them.
      for (let pass = 0; pass < settled.length + 1; pass++) {
        const hit = settled.find(
          (d) =>
            Math.abs(d.x - b.x) < (d.w + b.w) / 2 + PLATE_CLEAR &&
            Math.abs(centreOf(d) - centreOf(b)) < ((d.h ?? PLATE_H) + PLATE_H) / 2 + PLATE_CLEAR,
        )
        if (!hit) break
        // Solve centreOf(b) for b.y: the anchor sits 10 above its box.
        b.y = centreOf(hit) + ((hit.h ?? PLATE_H) + PLATE_H) / 2 + PLATE_CLEAR + 10 - PLATE_H / 2
      }
      settled.push({ x: b.x, y: b.y, w: b.w })
    }
    return raw
  }

  // Click-through per the shaped surface: the pair and its direction,
  // said in the filters the live view already speaks -- the zones' own
  // CIDRs where the address push named them, scope for the internet
  // side, the boundary name as the fallback. A single-port edge narrows
  // to it; a port *set* stays unnarrowed (the port filter takes one
  // query), declared on #628.
  function openPair(fromIface: string, toIface: string, ports: string[]) {
    const from = anchorOf(fromIface)
    const to = anchorOf(toIface)
    appState.resetFilters()
    const fromZone = zones.find((z) => z.id === fromIface)
    const toZone = zones.find((z) => z.id === toIface)
    if (from?.kind === 'internet') appState.setFilter('srcScope', 'external')
    else if (fromZone?.cidr) appState.setFilter('srcQuery', fromZone.cidr)
    else if (fromIface) appState.setFilter('interface', fromIface)
    if (to?.kind === 'internet') appState.setFilter('dstScope', 'external')
    else if (toZone?.cidr) appState.setFilter('dstQuery', toZone.cidr)
    else if (toIface && !appState.filters.interface) appState.setFilter('interface', toIface)
    if (ports.length === 1 && /^:\d+$/.test(ports[0])) appState.setFilter('port', ports[0].slice(1))
    appState.view = 'live'
  }

  // --- the reality overlay (#629: layer 3, observed on intended) -----------
  // The Traffic lens draws what actually happened, pair by pair, judged
  // against the intended edges: unplanned traffic finally spends the
  // reserved saturated colour. What happened is also the geometry --
  // accepted traffic crosses, drops die at the waist whatever the
  // verdict, and a pair carrying both draws the crossing with a ⊣ tick
  // for its refused share.
  const observed = $derived.by(() => realityEdges(appState.events, policyState.edges, policyState.anyPushed))

  interface DrawnReality {
    r: RealityEdge
    line: Line
  }

  const drawnReality = $derived.by((): { drawn: DrawnReality[]; undrawn: number } => {
    const drawn: DrawnReality[] = []
    let undrawn = 0
    for (const r of observed) {
      const line = drawn.length < EDGE_CAP ? lineFor(r.from, r.to, r.accepts > 0) : null
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ r, line })
    }
    return { drawn, undrawn }
  })

  // The second delta: accepting rules no packet has exercised, drawn as
  // ghosts of the intent nothing arrived to fill.
  const ghostIntents = $derived.by((): DrawnEdge[] => {
    if (!policyState.anyPushed) return []
    return unexercisedIntents(observed, policyState.edges)
      .map((edge) => {
        const line = lineFor(edge.from, edge.to, true)
        return line ? { edge, line } : null
      })
      .filter((g): g is DrawnEdge => g !== null)
      .slice(0, 4)
  })

  // The services layer (#699, altitude 1 -- round 30's own `.svc`, which
  // the build had no equivalent of): the ports this lane was actually
  // observed accepting, ranked across the same observed edges the
  // traffic lens already draws. Absent where nothing has been observed;
  // this layer never invents a service the events did not carry.
  function laneServices(z: ZoneInfo): string | null {
    const counts = new Map<string, number>()
    for (const r of observed) {
      if (r.from !== z.id && r.to !== z.id) continue
      r.topPorts.forEach((p, rank) => counts.set(p, (counts.get(p) ?? 0) + (3 - Math.min(2, rank))))
    }
    if (counts.size === 0) return null
    return [...counts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([p]) => p)
      .join(' ')
  }

  // Volume speaks through weight: 1 event is a hairline, thousands a
  // firm stroke, never a shout.
  function realityWidth(r: RealityEdge): number {
    return Math.min(4.4, 1.3 + Math.log10(Math.max(1, r.events)))
  }

  // Colour is the verdict (round 49, #1016), which is why an observed
  // rib no longer takes the lane's own ink (#715's rule, round 30's
  // `.rib` stroked var(--lan)/var(--srv)/etc): green where anything was
  // accepted, red where the boundary only ever dropped. The lane inks
  // stay everywhere else -- the lane dot, the client tier, the
  // placeholder volume ribs before any pair has been observed -- so
  // nothing lost its lane, only the ribs stopped saying two things at
  // once. The one saturated colour is still reserved: the escalated
  // unplanned pair takes it whole, undivided and glowing.
  function verdictInk(r: RealityEdge): string {
    return r.accepts > 0 ? 'var(--accept)' : 'var(--alarm)'
  }

  function realityBadge(r: RealityEdge): string {
    const ports = r.topPorts.slice(0, 3).join(' ')
    const n = r.events.toLocaleString()
    if (r.accepts === 0) return `⊣ ${n}× ${r.verdict === 'holding' ? 'held' : 'dropped'}`
    const mark = r.verdict === 'unplanned' ? 'unplanned ·' : '→'
    return `${mark} ${ports ? `${ports} · ` : ''}${n}×`
  }

  // --- brightness is the baseline (#1016, round 49) ------------------------
  // "Colour is the verdict; brightness is the baseline." A *line* is
  // `source → destination · port · proto`; a line on the pattern is
  // established and recedes, a line off it is bright. The register sends
  // only today's off-baseline lines, never the established ones (see
  // lib/baseline.ts for why that is the design and not an optimisation),
  // so absence from this index *is* established. Nothing is ever removed
  // from the map by any of this -- only dimmed.

  /** The lanes whose CIDR the address push actually named, parsed once.
   * A lane with no pushed CIDR claims no address, so a line can never be
   * attributed to it -- which is right: the map would be guessing. */
  const laneCidrs = $derived.by((): { id: string; cidr: ParsedCidr }[] => {
    const out: { id: string; cidr: ParsedCidr }[] = []
    for (const z of zones) {
      const p = z.cidr ? parseCidr(z.cidr) : null
      if (p) out.push({ id: z.id, cidr: p })
    }
    return out
  })

  /**
   * The roll-up, indexed rather than searched.
   *
   * "A rib, lane or host dot is as bright as the brightest line it
   * carries: one off-baseline line among a thousand lights the one place
   * it happened." Asked the obvious way -- `offBaselineMatching(off,
   * pred)` at every drawn element -- that is O(lines × elements) on
   * every re-render, and it would put the cost back on traffic volume,
   * which is the one thing this whole feature exists to escape. One pass
   * over the lines answers it for every element at once; each element's
   * own question is then a `Map.get`, whatever the register's size.
   *
   * The number of *drawn* elements does not grow with traffic either:
   * one rib per zone pair, one dot per host, one spoke per client. This
   * index adds no lines to the map, it only changes how the existing
   * ones are drawn.
   */
  interface OffIndex {
    /** By boundary-direction key, `${from}|${to}` -- a rib's own key. */
    byPair: Map<string, OffBaselineLine[]>
    /** By host address, both ends -- a dot's and its spoke's. */
    byIp: Map<string, OffBaselineLine[]>
    /** By `${host address}|${counterpart}` -- one reach strand's own key.
     *
     * A strand is one host against one counterpart, so neither of the two
     * maps above can answer it: `byIp` lights every strand the host has
     * the moment any one of its lines is off the baseline, and `byPair`
     * knows nothing about which host inside the lane it was. The
     * counterpart is spelled the way `reachFor` spells it -- the far
     * interface, or 'internet' for the WAN -- so the drawing can look a
     * strand up by the key it already holds, with no second mapping to
     * keep in step. Built from the same one pass and the same `zoneOf`,
     * so a strand and its rib can never disagree about a line. */
    byHostCounterpart: Map<string, OffBaselineLine[]>
  }

  const offIndex = $derived.by((): OffIndex => {
    const byPair = new Map<string, OffBaselineLine[]>()
    const byIp = new Map<string, OffBaselineLine[]>()
    const byHostCounterpart = new Map<string, OffBaselineLine[]>()
    const lanes = laneCidrs
    const wan = zonesState.wanInterface
    // One answer per distinct address rather than per line: an
    // established network talks on the same routes, so even the
    // off-baseline set repeats the same few machines, and the CIDR test
    // is the expensive part of this loop.
    const zoneCache = new Map<string, string | null>()
    const zoneOf = (ip: string): string | null => {
      const hit = zoneCache.get(ip)
      if (hit !== undefined) return hit
      let found: string | null = null
      for (const l of lanes) {
        if (addressInCidr(ip, l.cidr)) {
          found = l.id
          break
        }
      }
      // Off every drawn lane and public: the far side of the WAN
      // boundary, which is the only other end this map draws. A private
      // address no lane claims stays unattributed rather than being
      // drawn on a boundary it may never have crossed.
      if (found === null && wan !== null && isPublicIp(ip)) found = wan
      zoneCache.set(ip, found)
      return found
    }
    const push = (m: Map<string, OffBaselineLine[]>, k: string, l: OffBaselineLine) => {
      const at = m.get(k)
      if (at) at.push(l)
      else m.set(k, [l])
    }
    for (const l of baselineState.off.lines) {
      push(byIp, l.srcIp, l)
      if (l.dstIp !== l.srcIp) push(byIp, l.dstIp, l)
      const from = zoneOf(l.srcIp)
      const to = zoneOf(l.dstIp)
      // Each end's own strand: the host at one end, keyed by the lane the
      // *other* end sits in, spelled as `reachFor` spells a counterpart.
      // Done before the rib test below, because a line inside one lane
      // still has a strand -- it crosses no boundary, so it lights no
      // rib, but the reach draws it all the same.
      const far = (z: string | null): string | null => (z === null ? null : z === wan ? 'internet' : z)
      const srcFar = far(to)
      const dstFar = far(from)
      if (srcFar !== null) push(byHostCounterpart, `${l.srcIp}|${srcFar}`, l)
      if (dstFar !== null && l.dstIp !== l.srcIp) push(byHostCounterpart, `${l.dstIp}|${dstFar}`, l)
      // A line inside one lane crosses no boundary, so it lights no rib.
      // It still lights its own hosts' dots, above.
      if (from === null || to === null || from === to) continue
      push(byPair, `${from}|${to}`, l)
    }
    return { byPair, byIp, byHostCounterpart }
  })

  /** The lines one rib carries that are off the baseline today. */
  function offLinesFor(key: string): OffBaselineLine[] {
    return offIndex.byPair.get(key) ?? []
  }

  /** The roll-up as the drawing asks it: is this rib bright? */
  function ribOffBaseline(key: string): boolean {
    return offIndex.byPair.has(key)
  }

  /** The same question for a host dot and its own spoke. */
  function hostOffBaseline(ip: string): boolean {
    return offIndex.byIp.has(ip)
  }

  /** The lines one host is an end of, for its card's own count. */
  function offLinesForIp(ip: string): OffBaselineLine[] {
    return offIndex.byIp.get(ip) ?? []
  }

  /** The same roll-up again for one drawn reach strand: is this strand
   * bright? "As bright as the brightest line it carries" is the same
   * sentence for a strand as for a rib -- only the key differs, because
   * a strand is one host against one counterpart rather than a zone
   * pair. Answered off the centred host, so it is meaningless with no
   * reach open. */
  function strandOffBaseline(counterpart: string): boolean {
    return reach !== null && offIndex.byHostCounterpart.has(`${reach.ip}|${counterpart}`)
  }

  /** The lines one strand carries off the baseline, for its line card. */
  function strandOffLines(counterpart: string): OffBaselineLine[] {
    return reach === null ? [] : (offIndex.byHostCounterpart.get(`${reach.ip}|${counterpart}`) ?? [])
  }

  /** The header's `⟡ off-baseline today · N`. Estate-wide, and the
   * register's own count rather than this index's: the index drops what
   * the map has no lane for, and the header is reporting the register,
   * not the drawing. */
  const offBaselineCount = $derived(baselineState.count)

  /** A host's own name where anything knows one, its address otherwise:
   * the card's table says `tom-desktop → nas`, not two addresses. */
  function addressLabel(ip: string): string {
    for (const h of hostsState.hosts) {
      if (h.ip === ip && h.label) return h.label
    }
    for (const z of zones) {
      for (const h of z.hosts) {
        if (h.ip === ip && h.label && h.label !== ip) return h.label
      }
    }
    return ip
  }

  /** The `PORT` column: `5001/tcp`, and the proto alone where the event
   * carried no destination port -- 0 is not a port (lib/baseline.ts). */
  function linePort(l: OffBaselineLine): string {
    return l.port === null ? l.proto : `${l.port}/${l.proto}`
  }

  /** The `FIRST` column. Every line in the register is today's by
   * construction, so "today" is a fact here and not a guess. */
  function lineFirstSeen(l: OffBaselineLine): string {
    return `today ${formatHM(new Date(l.firstSeenToday).toISOString())}`
  }

  // --- coverage is the material (#630, #392; round 49 #1016) ---------------
  // Not a lens any more: the state of a boundary-direction is how its
  // own half of the rib is drawn, always. The three states come from
  // coverageRule.ts, the one place the rule lives (slice A) -- logged
  // takes the verdict ink, quiet on purpose white, dark grey dashes.
  const covByKey = $derived(new Map(policyState.edges.map((e) => [e.key, edgeCoverage(e, quietKeys)] as const)))

  /** A direction nothing logs: dark, or declared quiet on purpose.
   * Nothing is drawn crossing one -- a traffic line there would claim a
   * log line that was never written. */
  function silentDir(key: string): boolean {
    const st = covByKey.get(key)
    return st === 'dark' || st === 'quiet'
  }

  // The halves the material draws by itself: every dark or quiet
  // boundary-direction, whether or not anything was observed on it.
  // A logged direction needs no half of its own -- the traffic drawn
  // along it is already in the verdict ink, which is what logged means.
  const drawnCoverage = $derived.by((): { drawn: (DrawnEdge & { cov: Coverage })[]; undrawn: number } => {
    const drawn: (DrawnEdge & { cov: Coverage })[] = []
    let undrawn = 0
    for (const e of policyState.edges) {
      const cov = covByKey.get(e.key)
      if (cov !== 'dark' && cov !== 'quiet') continue
      const line = drawn.length < EDGE_CAP ? lineFor(e.from, e.to, true) : null
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ edge: e, line, cov })
    }
    return { drawn, undrawn }
  })

  // The edge cap's own honesty (#699). The count used to be drawn as a
  // caption in the stage's bottom-right corner, where it ran straight
  // through the rightmost lane's aggregate bar; round 30 draws no such
  // caption anywhere. The fact is not lost -- it rides the map's own
  // accessible name instead, so nothing on the drawing carries it and
  // nothing about the map's incompleteness goes unsaid.
  const undrawnPairs = $derived(drawnReality.undrawn + drawnCoverage.undrawn)
  const undrawnNote = $derived(
    undrawnPairs > 0
      ? `. ${undrawnPairs} further pair${undrawnPairs === 1 ? '' : 's'} are not drawn — off this map's islands, or beyond its ${EDGE_CAP}-edge calm`
      : '',
  )

  function coverageOf(e: PolicyEdge): Coverage {
    return edgeCoverage(e, quietKeys)
  }

  // The card's title, in the names the map itself draws: the zone's own
  // name where the pushed address table gave it one (round 49 titles
  // its boundary card `Guest → wan`, not `bridge4 → ether1`), the
  // interface where it did not.
  function laneName(i: string): string {
    if (i === zonesState.wanInterface) return 'the internet'
    if (i === '') return 'any lane'
    return zones.find((z) => z.id === i)?.name ?? i
  }

  function pairName(from: string, to: string): string {
    return `${laneName(from)} → ${laneName(to)}`
  }

  function coverageLabel(e: PolicyEdge): string {
    const st = coverageOf(e)
    if (st === 'logged') return `${pairName(e.from, e.to)}: logged`
    if (st === 'quiet') {
      const d = coverageState.byKey.get(e.key)
      return `${pairName(e.from, e.to)}: intentionally quiet — ${d?.reason ?? ''}`
    }
    return `${pairName(e.from, e.to)}: dark — no rule on this boundary-direction logs`
  }

  // --- the boundary card and the declare path (#392; round 49) -------------
  // Cards are the one interaction: clicking a dark or quiet half opens
  // its boundary card, which says what the rule does and what both
  // directions are, and carries the actions. Pinning it opens the
  // declare form -- reason, both directions, Declare, and who -- which
  // is the only way a gap becomes quiet on purpose. A quiet boundary's
  // card quotes the reason back with who and when, and offers to
  // undeclare. Admin-only, per #490's grammar: for a viewer the
  // affordance is absent, never disabled.
  let boundaryCard = $state<{ key: string; from: string; to: string } | null>(null)
  let cardPinned = $state(false)
  let declareReason = $state('')
  /** Round 49 open question 7, drawn checked: one direction declared and
   * the other still dark leaves the boundary grey and the card
   * explaining why, which is nobody's intent. */
  let declareBoth = $state(true)
  let declareBusy = $state(false)

  const reverseKey = (from: string, to: string) => `${to}|${from}`

  const cardCoverage = $derived(boundaryCard ? (covByKey.get(boundaryCard.key) ?? 'dark') : 'dark')
  const cardBackCoverage = $derived(boundaryCard ? covByKey.get(reverseKey(boundaryCard.from, boundaryCard.to)) : undefined)
  const cardEdge = $derived(boundaryCard ? (policyState.edges.find((e) => e.key === boundaryCard?.key) ?? null) : null)
  const cardDeclaration = $derived(boundaryCard ? (coverageState.byKey.get(boundaryCard.key) ?? null) : null)

  /** What the rule actually does on this direction, in one line: the
   * card's own reason for existing is that the material cannot say it. */
  const cardRuleLine = $derived.by((): string => {
    const e = cardEdge
    if (!boundaryCard) return ''
    const pair = pairName(boundaryCard.from, boundaryCard.to)
    if (!e) return `no pushed rule names ${pair}`
    const what = e.accepted ? 'accepts' : e.refused ? 'refuses' : 'names'
    const rules = `${e.ruleCount} rule${e.ruleCount === 1 ? '' : 's'}`
    return `${rules} ${what} ${pair}, without log=yes`
  })

  /** The other direction of the same boundary, said in full: a wall has
   * no direction, so the card lists both (round 49). */
  const cardBackLine = $derived.by((): string => {
    if (!boundaryCard) return ''
    const back = pairName(boundaryCard.to, boundaryCard.from)
    if (cardBackCoverage === undefined) return `${back} · no pushed rule names it`
    if (cardBackCoverage === 'logged') return `${back} · logged`
    if (cardBackCoverage === 'quiet') return `${back} · quiet on purpose`
    return `${back} · dark — nothing logs it`
  })

  // One placement pass per lens, over everything that lens draws: the
  // traffic lens's reality badges and its ghost-intent labels share a
  // corridor, so they are laid out together rather than in two passes
  // that cannot see each other.
  // Round 30 escalates the worst unplanned flow out of the row of
  // identical pills into its own two-line card (the-whole.html:940-944).
  // The choice of which pair is reality.ts's worstUnplannedOf, shared
  // with the city's own escalated road, so the two views cannot name
  // different pairs from the same data (#869).
  const worstUnplanned = $derived(worstUnplannedOf(drawnReality.drawn, (d) => d.r))

  function cardLines(r: RealityEdge): [string, string] {
    const asked = r.topAsked[0]
    const one = `UNPLANNED · ${r.from} → ${r.to}${asked ? ` · ${asked.proto}/${asked.port}` : ''}`
    // All three shapes an unplanned pair comes in, each said truthfully:
    // traffic passing where the table only refuses; drops caught by a
    // rule that named itself; drops caught by one that did not.
    const caught = r.accepts > 0 ? `${r.accepts}× passing` : r.refusedBy ? `caught by ${r.refusedBy}` : 'caught, no rule named'
    return [one, `${caught} · ${r.events}× · open ▸`]
  }

  const worstUnplannedCard = $derived.by((): PlacedBadge | undefined => {
    if (!worstUnplanned) return undefined
    const [one, two] = cardLines(worstUnplanned.r)
    const at = edgeBadgeAt(worstUnplanned.line, drawnReality.drawn.indexOf(worstUnplanned))
    const w = Math.max(plateW(one), plateW(two)) + CARD_PAD
    // The card obeys the corridor like any other plate: several lanes'
    // edges run up the same waist-to-internet segment, so a card left
    // where the line put it lands on the stack rather than beside it.
    // It takes the floor of its own side and the badges stack above.
    if (at.y > 100 && at.y < 310 && Math.abs(at.x - WAIST.x) < 170) {
      const side = at.x < WAIST.x ? -1 : 1
      return { x: WAIST.x + side * (CORRIDOR_GUTTER + w / 2), y: CORRIDOR_FLOOR - (CARD_H - PLATE_H), w, h: CARD_H }
    }
    return { x: at.x, y: at.y, w, h: CARD_H }
  })

  const trafficBadges = $derived.by(() =>
    placeBadges(
      [
        // The escalated pair keeps its slot in this array so every other
        // index still lines up; its own text is empty, so it takes no
        // space and draws no pill -- the card replaces it.
        // A silent direction draws no traffic and so carries no badge
        // either; it keeps its slot, empty, for the same reason.
        ...drawnReality.drawn.map((d) => ({
          line: d.line,
          text: d === worstUnplanned || silentDir(d.r.key) ? '' : realityBadge(d.r),
        })),
        ...ghostIntents.map((g) => ({ line: g.line, text: 'never exercised', dy: -12 })),
      ],
      worstUnplannedCard,
    ),
  )

  /** The card's grace period (#1027), the same object the city uses:
   * the pointer gets CARD_GRACE_MS to travel from a boundary to its own
   * card, so the card is still there when it arrives. */
  const cardGrace = grace()

  function openBoundary(e: PolicyEdge) {
    const st = coverageOf(e)
    if (st === 'logged') return
    cardGrace.hold()
    // Already open on this boundary: the pointer coming back to it is
    // not a reason to throw away a half-typed reason.
    if (boundaryCard?.key === e.key) return
    // A pinned card is kept until it is let go (DESIGN.md "Cards"), so
    // brushing past another boundary does not take it away -- the same
    // rule as the city, where a pinned wall outranks a hovered one.
    if (cardPinned) return
    coverageState.error = null
    declareReason = coverageState.byKey.get(e.key)?.reason ?? ''
    declareBoth = true
    cardPinned = false
    boundaryCard = { key: e.key, from: e.from, to: e.to }
  }

  /** The pointer has left the boundary, or the card. It may be crossing
   * between them, so nothing is taken down until the grace period has
   * passed with the pointer arriving at neither. */
  function releaseBoundary() {
    const open = boundaryCard
    if (!open || cardPinned) return
    cardGrace.release(() => {
      if (!cardPinned && boundaryCard?.key === open.key) closeBoundary()
    })
  }

  function closeBoundary() {
    cardGrace.hold()
    boundaryCard = null
    cardPinned = false
  }

  /* ---------------- where the card floats ---------------- */

  // Round 49 draws the card beside the boundary it is about, joined by
  // a leader with an accent dot at the boundary's end. The rules are in
  // lib/cardAnchor.ts, shared with the city -- one implementation, for
  // the reason DESIGN.md already records about `worstUnplannedOf`.
  let topoEl: HTMLDivElement | undefined = $state()
  let mapSvgEl: SVGSVGElement | undefined = $state()
  let cardEl: HTMLDivElement | undefined = $state()
  let cardPlace = $state<Placement | null>(null)
  /** Bumped when the stage changes size under us. */
  let stageTick = $state(0)
  /** Bumped when the card itself changes size -- the declare form going
   * in makes it taller, and where it can sit depends on how tall it is
   * (#1028). */
  let cardTick = $state(0)

  /**
   * The rendered plates of some zones, in the host's own pixels (#1028).
   *
   * A zone is drawn twice over: once as a lane card along the foot
   * (`g.zone`) and once as a ground-plan card (`g.gf-card`), and the
   * stop decides which of the two a reader can see -- the other is left
   * in the DOM at `opacity: 0`, still measurable and still the wrong
   * answer. Both carry `data-zone`, so both are asked, and
   * `cardAnchor`'s `drawnRects` keeps only what is actually on screen.
   *
   * Reading it off the drawing rather than recomputing it also means the
   * card follows the drawing when the drawing changes, instead of
   * quietly avoiding where a plate used to be.
   */
  function zonePlates(host: Element, ids: readonly string[]): Rect[] {
    const els: Element[] = []
    for (const id of ids) {
      const sel = `g.zone[data-zone="${CSS.escape(id)}"] rect.isl, g.gf-card[data-zone="${CSS.escape(id)}"] rect.gf-plate`
      for (const el of host.querySelectorAll(sel)) els.push(el)
    }
    return drawnRects(els, host)
  }

  /**
   * The open boundary's own line, as boxes the card keeps off (#1030).
   *
   * The plates named in the card's title were in the avoid-set and the
   * line between them was not, so the card came down on the tail of the
   * very boundary it was describing. Taken off the drawing rather than
   * recomputed from `halfPath`, for the reason `zonePlates` gives:
   * whatever is on screen is what the card has to clear.
   *
   * `drawnPathRects` walks the rib, so a diagonal is a chain of boxes
   * along it rather than one box round the whole sweep -- the card
   * stays beside its own line instead of being pushed off it.
   */
  function openEdgeRects(host: Element): Rect[] {
    const el = host.querySelector('g.cov-g.on path.cedge')
    return el === null ? [] : drawnPathRects(el, host)
  }

  /**
   * The open rib's own line, as boxes the card keeps off (#1030).
   *
   * The same fault `openEdgeRects` fixes for the boundary card, and the
   * same fix: the off-baseline card kept clear of the two zone plates
   * its title names and of nothing else, so the rib running between
   * them -- the one thing the card is about -- ran in under the card.
   *
   * The open rib carries `on`, the way the open boundary half does, so
   * this is the drawn line and not one of the others.
   */
  function openRibRects(host: Element): Rect[] {
    const el = host.querySelector('g.edge-g.on path.redge')
    return el === null ? [] : drawnPathRects(el, host)
  }

  /** Whether the lane row is the drawing on screen, as opposed to the
   * ground plan that replaces it at the zones stop. */
  function laneRowDrawn(host: Element): boolean {
    const el = host.querySelector('g.zone rect.isl')
    return el !== null && drawnRect(el, host) !== null
  }

  /** The open boundary's own drawn half, taken live rather than kept
   * from when the card opened: the lane row re-lays itself out as zones
   * arrive, and a leader pointing where the rib used to be is worse
   * than no leader at all. */
  const openDrawn = $derived.by(() => {
    const open = boundaryCard
    if (!open) return null
    return drawnCoverage.drawn.find((d) => d.edge.key === open.key) ?? null
  })

  $effect(() => {
    // Read first, so this re-runs on everything that moves the subject:
    // which boundary is open, where its rib is drawn, the altitude, the
    // card's arrival in the DOM, and the stage's size.
    const drawn = openDrawn
    const open = boundaryCard
    const svg = mapSvgEl
    const host = topoEl
    const card = cardEl
    void altitude
    void stageTick
    void cardTick

    if (!drawn || !open || !svg || !host || !card || reach) {
      cardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      cardPlace = null
      return
    }
    const anchor = map(halfMid(drawn.line))
    // Both plates, not just the near one: the card names a pair, and
    // sitting on either end hides half of what it is describing.
    //
    // Measured off the drawing rather than recomputed from `islandRect`
    // (#1028). A zone is drawn by the lane row at clients and services
    // and by the ground plan at zones, in a completely different place,
    // and the two swap with `opacity: 0` -- so a set built from the lane
    // row's own coordinates is, at the zones stop, a set of rectangles
    // nobody can see, and the card cleared those while coming down on
    // the ground-plan plate its title names. `zonePlates` returns
    // whichever layer is really on screen.
    //
    // And the boundary's own line with them (#1030): a card sitting on
    // the line hides the tail of the one thing it is about, which the
    // two plates alone never stopped.
    const avoid = zonePlates(host, [open.from, open.to]).concat(openEdgeRects(host))
    // Every other plate is worth keeping clear too, but only as a
    // tie-break: the plates fill the map, and insisting would leave
    // nowhere to put the card at all.
    const others = zones.map((z) => z.id).filter((id) => id !== open.from && id !== open.to)
    const softAvoid = zonePlates(host, others).concat(
      // The waist and the internet island belong to the lane-row drawing
      // and go with it: at the zones stop the ground plan has replaced
      // them, and avoiding where they used to be is the same mistake
      // again.
      laneRowDrawn(host) ? [islandRect({ ...WAIST, kind: 'any' }), islandRect({ x: 700, y: 104, kind: 'internet' })].map((r) => mapRect(map, r)) : [],
    )
    cardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  // The stage's size is the one input nothing else reports. jsdom has
  // no ResizeObserver and lays nothing out, so there the card keeps its
  // unplaced position rather than taking a wrong one.
  $effect(() => {
    const host = topoEl
    if (!host || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => stageTick++)
    ro.observe(host)
    return () => ro.disconnect()
  })

  // And the card's own size, which nothing else reports either: the
  // declare form goes in behind the pin and the card gets taller
  // (#1028). watchCardSize is the city's too -- one rule, both surfaces
  // -- and it says there why this cannot move the card round in circles.
  $effect(() => {
    const card = cardEl
    if (!card) return
    return watchCardSize(card, () => cardTick++)
  })

  /* ---------------- the host card (#1016) ---------------- */

  // The same one interaction as the boundary card, on the same shared
  // placement (lib/cardAnchor): the card floats beside its dot with a
  // leader, the pointer gets the grace period to travel to it, and it
  // re-places itself when the mark form makes it taller.
  let hostCard = $state<{ zoneId: string; key: string } | null>(null)
  let hostCardPinned = $state(false)
  let hostCardEl = $state<HTMLDivElement>()
  let hostCardPlace = $state<Placement | null>(null)
  let hostCardTick = $state(0)
  /** The reason form, behind `mark quiet on purpose` -- the mark reads
   * first and acts once it is asked for, as the declare form does. */
  let hostMarkOpen = $state(false)
  let hostReason = $state('')
  let hostBusy = $state(false)
  const hostGrace = grace()

  /** The open host, taken live rather than kept from when the card
   * opened: the lane row re-lays itself out as zones arrive, and a
   * leader pointing where the dot used to be is worse than no leader. */
  const openHostDot = $derived.by(() => {
    const open = hostCard
    if (!open) return null
    const zi = zones.findIndex((z) => z.id === open.zoneId)
    if (zi < 0) return null
    const row = laneHostRow(zones[zi])
    const di = row.dots.findIndex((d) => d.key === open.key)
    if (di < 0) return null
    return { zi, di, zone: zones[zi], dot: row.dots[di] }
  })

  $effect(() => {
    const open = openHostDot
    const svg = mapSvgEl
    const host = topoEl
    const card = hostCardEl
    void altitude
    void stageTick
    void hostCardTick

    if (!open || !svg || !host || !card || reach) {
      hostCardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      hostCardPlace = null
      return
    }
    // The dot's own point on the map, in the lane card's space plus the
    // lane's own offset.
    const anchor = map({ x: laneX(open.zi, laneCount) + hostDotX(open.di), y: 490 + HOST_DOT_Y })
    // The dot's own plate is the one thing the card must not sit on: it
    // is the thing being pointed at. Measured off the drawing, for the
    // reason the boundary card's own placement gives above (#1028).
    const avoid = zonePlates(host, [open.zone.id])
    const others = zones.map((z) => z.id).filter((id) => id !== open.zone.id)
    const softAvoid = zonePlates(host, others).concat(
      laneRowDrawn(host) ? [islandRect({ ...WAIST, kind: 'any' }), islandRect({ x: 700, y: 104, kind: 'internet' })].map((r) => mapRect(map, r)) : [],
    )
    hostCardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = hostCardEl
    if (!card) return
    return watchCardSize(card, () => hostCardTick++)
  })

  function openHostCard(zoneId: string, key: string) {
    hostGrace.hold()
    // Already open on this dot: coming back to it is not a reason to
    // throw away a half-typed reason.
    if (hostCard?.key === key) return
    // A pinned card is kept until it is let go, whichever card it is --
    // the same rule the boundary card already follows.
    if (hostCardPinned || cardPinned) return
    hostsState.error = null
    hostReason = ''
    hostMarkOpen = false
    hostCard = { zoneId, key }
  }

  /** The pointer has left the dot, or the card. It may be crossing
   * between them, so nothing is taken down until the grace period has
   * passed with the pointer arriving at neither. */
  function releaseHostCard() {
    const open = hostCard
    if (!open || hostCardPinned) return
    hostGrace.release(() => {
      if (!hostCardPinned && hostCard?.key === open.key) closeHostCard()
    })
  }

  function closeHostCard() {
    hostGrace.hold()
    hostCard = null
    hostCardPinned = false
    hostMarkOpen = false
    hostReason = ''
  }

  function pinHostCard() {
    hostCardPinned = !hostCardPinned
  }

  /** `mark quiet on purpose` needs a reason -- the reason *is* the mark
   * (internal/hosts.Register.Mark), so the action opens the form rather
   * than writing an empty statement. */
  function askHostMark() {
    hostsState.error = null
    hostMarkOpen = true
    hostCardPinned = true
  }

  async function submitHostMark() {
    const open = hostCard
    if (!open || !hostReason.trim()) return
    hostBusy = true
    const ok = await hostsState.mark(open.key, 'intended', hostReason.trim())
    hostBusy = false
    if (ok) {
      hostMarkOpen = false
      hostReason = ''
    }
  }

  /** Dismiss takes the host off the map. It needs no reason, and the
   * server gives it back by itself the moment the feed hears the host
   * again -- so this is never a deletion, only a "not now". */
  async function dismissHost() {
    const open = hostCard
    if (!open) return
    hostBusy = true
    const ok = await hostsState.mark(open.key, 'dismissed')
    hostBusy = false
    if (ok) closeHostCard()
  }

  /** A dot's own door into the reach. The card comes down on the way
   * through: it is about a dot on the map behind, and leaving it open
   * would bring it back when the reach is surfaced from. */
  function descendFromHost(zoneId: string, label: string, ip: string) {
    closeHostCard()
    descend(zoneId, label, ip)
  }

  /** The stream, filtered to this host's own address -- the same door
   * the node card already offers, from the card that named the host. */
  function openStreamFromHost() {
    const open = openHostDot
    if (!open) return
    appState.resetFilters()
    appState.setFilter('srcQuery', open.dot.ip)
    appState.view = 'live'
    closeHostCard()
  }

  /** Takes the statement back, whichever kind it was. */
  async function unmarkHost() {
    const open = hostCard
    if (!open) return
    hostBusy = true
    const ok = await hostsState.unmark(open.key)
    hostBusy = false
    if (ok) {
      hostMarkOpen = false
      hostReason = ''
    }
  }

  /* ---------------- the off-baseline card (#1016, round 49) ---------------- */

  // Round 49's `flat-new` scene: the bright half hovered, and its card
  // rolls the rib up -- a thousand established lines in one dim word,
  // and the one line that is not on the pattern spelled out with when it
  // was first seen and how often, then the two ways to answer it.
  //
  // Same one interaction, same shared placement (lib/cardAnchor) as the
  // boundary and host cards: beside its own half with a leader, a grace
  // period for the pointer to travel to it, and a re-place when the
  // reason form makes it taller.
  let offCard = $state<{ key: string } | null>(null)
  let offCardPinned = $state(false)
  let offCardEl = $state<HTMLDivElement>()
  let offCardPlace = $state<Placement | null>(null)
  let offCardTick = $state(0)
  /** The reason, behind `expected ▸`. Required: the server refuses an
   * empty one, and a statement with no reason stays said with nothing
   * to say for itself. */
  let offReason = $state('')
  let offBusy = $state(false)
  /** Which lines the open reason form will speak for: one named line
   * (the default), or every line this rib carries off the baseline.
   * Null means no form is open. The city road card holds exactly the
   * same state, through the same lib/city/expected helpers. */
  let offScope = $state<ExpectedScope | null>(null)
  const offGrace = grace()

  /** The open rib's own drawn half, taken live rather than kept from
   * when the card opened -- the lane row re-lays itself out as zones
   * arrive, and a leader pointing where the rib used to be is worse than
   * no leader at all (the same reason `openDrawn` gives above). */
  const openOffDrawn = $derived.by(() => {
    const open = offCard
    if (!open) return null
    return drawnReality.drawn.find((d) => d.r.key === open.key) ?? null
  })

  /** The lines the card lists. Empty means the rib stopped being bright
   * under the card -- the register refreshed, or `expected` landed -- and
   * the card closes itself rather than standing there describing nothing. */
  const offCardLines = $derived(offCard ? offLinesFor(offCard.key) : [])

  /** The lines the open form is about to speak for, and how many. */
  const offScopeLines = $derived(offScope ? expectedTargets(offScope, offCardLines) : [])

  $effect(() => {
    if (offCard && offCardLines.length === 0) closeOffCard()
  })

  $effect(() => {
    // Read first, so this re-runs on everything that moves the subject.
    const drawn = openOffDrawn
    const svg = mapSvgEl
    const host = topoEl
    const card = offCardEl
    void altitude
    void stageTick
    void offCardTick

    if (!drawn || !svg || !host || !card || reach) {
      offCardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      offCardPlace = null
      return
    }
    const anchor = map(halfMid(drawn.line))
    // Both plates: the card names a pair, and sitting on either end
    // hides half of what it is describing. Measured off the drawing, for
    // the reason the boundary card's own placement gives (#1028).
    //
    // And the rib itself with them (#1030): the plates alone never
    // stopped the card coming down on the line between them, which is
    // the whole subject. The boundary card was given its own line here;
    // this one was left out.
    const avoid = zonePlates(host, [drawn.r.from, drawn.r.to]).concat(openRibRects(host))
    const others = zones.map((z) => z.id).filter((id) => id !== drawn.r.from && id !== drawn.r.to)
    const softAvoid = zonePlates(host, others).concat(
      laneRowDrawn(host) ? [islandRect({ ...WAIST, kind: 'any' }), islandRect({ x: 700, y: 104, kind: 'internet' })].map((r) => mapRect(map, r)) : [],
    )
    offCardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = offCardEl
    if (!card) return
    return watchCardSize(card, () => offCardTick++)
  })

  function openOffCard(key: string) {
    offGrace.hold()
    if (offCard?.key === key) return
    // A pinned card is kept until it is let go, whichever card it is.
    if (offCardPinned || cardPinned || hostCardPinned) return
    baselineState.error = null
    offReason = ''
    offScope = null
    offCardPinned = false
    offCard = { key }
  }

  /** The pointer has left the rib, or the card. It may be crossing
   * between them, so nothing comes down until the grace period has
   * passed with the pointer arriving at neither (#1027). */
  function releaseOffCard() {
    const open = offCard
    if (!open || offCardPinned) return
    offGrace.release(() => {
      if (!offCardPinned && offCard?.key === open.key) closeOffCard()
    })
  }

  function closeOffCard() {
    offGrace.hold()
    offCard = null
    offCardPinned = false
    offCardPlace = null
    offReason = ''
    offScope = null
  }

  /** The header pin, which keeps the card up. It no longer opens the
   * form: the form belongs to a line now, and it is the line's own
   * `expected ▸` (or the bulk control) that opens it. */
  function pinOffCard() {
    if (!isAdmin) {
      closeOffCard()
      return
    }
    offCardPinned = !offCardPinned
    if (!offCardPinned) offScope = null
  }

  /** Open the reason form for one line -- the default action, one per
   * listed line. Pinning keeps the card up while the reason is typed. */
  function startExpectedOne(key: string) {
    openExpectedForm({ kind: 'one', key })
  }

  /** Open the reason form for every line this rib carries off the
   * baseline. Only ever reached from the control that said how many. */
  function startExpectedAll() {
    openExpectedForm({ kind: 'all' })
  }

  function openExpectedForm(scope: ExpectedScope) {
    if (!isAdmin) return
    offGrace.hold()
    offCardPinned = true
    offScope = scope
    offReason = ''
    baselineState.error = null
  }

  /**
   * `expected` says the lines this write covers are meant to be there,
   * and they read established from then on. The server stamps who and
   * when from the session; the reason is ours to require.
   *
   * Per-line is the default (owner, 2026-09-07, #1016). Marking one of
   * several leaves the rest in the register, so the other rows stay
   * bright and the rib stays bright until nothing off-baseline is left
   * on it -- nothing here has to model that, because the register is
   * re-read on every success. A bulk mark is this same single-line write
   * repeated with the same reason, never a second kind of record.
   */
  async function submitExpected() {
    const targets = offScopeLines
    const reason = offReason.trim()
    if (targets.length === 0 || reason === '' || offBusy) return
    offBusy = true
    for (const l of targets) {
      // Stops at the first refusal rather than pressing on: the error is
      // shown, and a half-written statement the operator cannot see the
      // shape of is worse than none.
      if (!(await baselineState.expected(l.key, reason))) break
    }
    offBusy = false
    if (baselineState.error === null) {
      offScope = null
      offReason = ''
    }
  }

  /** The stream, filtered to this rib's own pair. */
  function openStreamFromOffCard() {
    const drawn = openOffDrawn
    if (!drawn) return
    const { from, to } = drawn.r
    closeOffCard()
    openPair(from, to, [])
  }

  /** The card's own verdict sentence: what the router did, and that
   * nothing decided it was wanted. Never a rule number for an accept --
   * PolicyEdge carries a rule *count*, not an identity, and the mockup's
   * `(rule #12)` would be invented here. A refusal names its catcher,
   * because the events themselves say who caught it. */
  const offCardVerdict = $derived.by((): string => {
    const lines = offCardLines
    if (lines.length === 0) return ''
    const one = lines.length === 1
    const it = one ? 'it' : 'them'
    const dropped = lines.filter((l) => l.outcome === 'drop').length
    const accepted = lines.length - dropped
    const caught = openOffDrawn?.r.refusedBy
    const by = caught ? ` (caught by ${caught})` : ''
    if (dropped === 0) return `the router accepted ${it}; nothing decided ${one ? 'it was' : 'they were'} wanted`
    if (accepted === 0) return `the router refused ${it}${by}`
    return `the router accepted ${accepted} and refused ${dropped}${by}; nothing decided the accepted ${accepted === 1 ? 'one was' : 'ones were'} wanted`
  })

  /** The `reach ▸` action's subject: the first listed line's source,
   * where that address is on a lane this map draws. Absent otherwise --
   * there is nowhere to descend to. */
  const offCardReach = $derived.by((): { zoneId: string; label: string; ip: string } | null => {
    const l = offCardLines[0]
    if (!l) return null
    for (const lane of laneCidrs) {
      if (addressInCidr(l.srcIp, lane.cidr)) return { zoneId: lane.id, label: addressLabel(l.srcIp), ip: l.srcIp }
    }
    return null
  })

  /** The pin is what opens the form (round 49): the card reads first,
   * and acts only once it is kept. */
  function pinBoundary() {
    if (!isAdmin) return
    cardPinned = !cardPinned
  }

  async function submitDeclaration() {
    if (!boundaryCard || !declareReason.trim()) return
    declareBusy = true
    const reason = declareReason.trim()
    // "Both directions" is two declarations, because the API's key is a
    // boundary-direction (#392) -- there is no both-ways record to
    // write, and inventing one here would be a second model of the same
    // fact. The back direction is declared even where no pushed rule
    // names it: the declaration is about the boundary, and a rule
    // arriving later then lands on a gap already accounted for.
    let ok = await coverageState.declare(boundaryCard.key, reason)
    if (ok && declareBoth) ok = await coverageState.declare(reverseKey(boundaryCard.from, boundaryCard.to), reason)
    declareBusy = false
    if (ok) closeBoundary()
  }

  async function removeDeclaration() {
    if (!boundaryCard) return
    declareBusy = true
    const back = reverseKey(boundaryCard.from, boundaryCard.to)
    let ok = await coverageState.undeclare(boundaryCard.key)
    if (ok && coverageState.byKey.has(back)) ok = await coverageState.undeclare(back)
    declareBusy = false
    if (ok) closeBoundary()
  }

  // Log every rule's other way in (#435 decision 2): a dark connection in
  // this same card is the thing prompting it. primaryDevice stands in
  // for "which router" -- the map has no per-edge device attribution
  // (policyState aggregates every device's pushed table), the same
  // approximation waistSub above already makes for the rule count.
  // This is the card's `rules ▸`: the rules for this very boundary,
  // which is where the round-49 mockup's `rules #31 ▸` leads.
  function openLogEveryRuleFromDark() {
    if (!boundaryCard || !primaryDevice) return
    logEveryRuleNavState.request(primaryDevice.id, boundaryCard.key)
    appState.view = 'tune-logging'
    closeBoundary()
  }

  /** The card's `stream ▸`: the live view, filtered to this pair. */
  function openStreamFromCard() {
    if (!boundaryCard) return
    const { from, to } = boundaryCard
    closeBoundary()
    openPair(from, to, [])
  }

  function realityLabel(r: RealityEdge): string {
    const name = (i: string) => (i === zonesState.wanInterface ? 'the internet' : i)
    const what =
      r.verdict === 'unplanned'
        ? 'unplanned by any pushed rule'
        : r.verdict === 'holding'
          ? 'held by policy'
          : r.verdict === 'planned'
            ? 'as intended'
            : 'unjudged — no rule table pushed'
    return `${name(r.from)} → ${name(r.to)}: ${r.events.toLocaleString()} events, ${what}`
  }

  // --- the reach (#626: a mode of this scene, not a place) -----------------
  // Recentring folds the map into the membrane view; Esc, the
  // breadcrumb or clicking off anywhere surfaces exactly where you
  // were. The map stays beneath, blurred, at the level you left
  // (round 24); zoom and pan sleep while descended (none exist yet, so
  // there is nothing to put to sleep -- recorded for when they do).
  /** What the reach is centred on, and how the crumb and the centre
   * node read it. `subject` is what lib/reach answers for (#1016: a
   * host, a zone or a rib); `zoneId` is the lane the subject belongs to,
   * for its ink and for the composer's own "from" side; `host`/`ip` are
   * the two lines the centre node and the crumb print -- a name and an
   * address for a host, a name and a subnet for a zone, and the pair's
   * own name and nothing for a rib, which has no address of its own to
   * state and is not given an invented one. */
  let reach = $state<{ subject: ReachSubject; zoneId: string; host: string; ip: string } | null>(null)

  /** Only a host's reach can draft a rule, filter the stream by an
   * address or be handed across the slider to a city building: all
   * three are about one machine. A zone's or a rib's reach draws and
   * says everything else the same way. */
  const reachIsHost = $derived(reach?.subject.kind === 'host')


  /** Standing on something.
   *
   * The stop is deliberately not changed: the reach is a mode of this
   * scene, not a place of its own (#626), so the map stays at the
   * altitude and position it was left at and `surface()` has nothing to
   * restore -- "Esc surfaces to the stop and position you came from"
   * holds because nothing moved. The city, whose camera *does* move to
   * stand on a building, saves and restores it instead (City.svelte's
   * `savedS`/`savedCentre`). */
  function descend(zoneId: string, host: string, ip: string) {
    closeLineCard()
    clearMapFilters()
    reach = { subject: hostSubject(ip), zoneId, host, ip }
  }

  /** Standing on something clears #1018's two filters, the same way
   * opening one of them clears the reach. Three answers layered on one
   * map would stack their crumbs on each other and leave nobody able to
   * say which of them a dim rib was dim because of.
   *
   * Declared here rather than beside the filters themselves because the
   * three descends below call it, and they are declared above the
   * filter block. */
  function clearMapFilters() {
    portFilterState.clear()
    mapTraceState.clear()
  }

  /** The two zones a ground-plan road joins, or null when it joins none
   * -- which of the roads is a rib. layout.ts keys a pair road by
   * `[a,b].sort().join('|')` over the district ids, and a district id is
   * the zone's own boundary interface, so the ends are read from the id
   * rather than guessed from where the road runs. A lane (`lane:<id>`)
   * is one building's own street and a bridge leg is a crossing, so
   * neither is a line between two zones. */
  function ribEndsOf(r: { id: string; lane?: boolean }): [string, string] | null {
    if (r.lane) return null
    const bar = r.id.indexOf('|')
    if (bar <= 0) return null
    const a = r.id.slice(0, bar)
    const b = r.id.slice(bar + 1)
    if (!b) return null
    const isDistrict = (id: string) => ground.districts.some((d) => d.id === id)
    return isDistrict(a) && isDistrict(b) ? [a, b] : null
  }

  /** A zone's own reach (#1016): the plate is the subject, and its
   * boundary interface is the side lib/reach matches against. */
  function descendZone(z: { id: string; name: string; cidr: string | null }) {
    closeLineCard()
    clearMapFilters()
    reach = { subject: { kind: 'zone', iface: z.id }, zoneId: z.id, host: z.name, ip: z.cidr ?? '' }
  }

  /** A rib's own reach (#1016): the line between two zones. Direction is
   * read from `a`, so the rib is opened with the end the reader clicked
   * from first -- `from → to` the way the drawn half already runs. */
  function descendRib(a: string, b: string) {
    // An event can name only one of its two interfaces, and reality.ts
    // keys that pair with an empty end. There is no line between two
    // zones there, so there is no rib to stand on: the stream, filtered
    // to whatever the pair does name, is the honest door instead.
    if (!a || !b) {
      openPair(a, b, [])
      return
    }
    closeLineCard()
    clearMapFilters()
    reach = { subject: { kind: 'rib', a, b }, zoneId: a, host: pairName(a, b), ip: '' }
  }

  function surface() {
    closeLineCard()
    reach = null
    compose = null
  }

  // A flag's own "where" link (#678) hands off a host to descend into
  // rather than navigating here directly -- see topologyNav.svelte.ts's
  // own doc comment for why a shared slot, not a route param. Consumed
  // (cleared) the instant it's read, so arriving here a second time
  // without a fresh request just shows the plain map. Left for City's
  // own effect to consume when the city is the active side of the
  // slider (#868) -- otherwise this effect runs regardless of altitude
  // (the 2D map stays mounted, only hidden, while the city shows) and
  // would steal the request before City ever saw it.
  $effect(() => {
    const pending = topologyNavState.pendingDescend
    if (pending && cityStop === null) {
      descend(pending.zoneId, pending.host, pending.ip)
      topologyNavState.pendingDescend = null
    }
  })

  function onKeydown(e: KeyboardEvent) {
    // The map's own pan (#890 item 4), on the same keys the city pans on.
    if (e.key.startsWith('Arrow') && mapTakesArrows(e.target)) {
      const step = mapBox.w / 5
      const vstep = mapBox.h / 5
      const dx = e.key === 'ArrowLeft' ? -step : e.key === 'ArrowRight' ? step : 0
      const dy = e.key === 'ArrowUp' ? -vstep : e.key === 'ArrowDown' ? vstep : 0
      if (dx !== 0 || dy !== 0) {
        e.preventDefault()
        panMap(dx, dy)
        return
      }
    }
    if (e.key === 'Escape') {
      // Esc walks out one level: the open card first, then the composer,
      // then a filter on the map, then the reach itself. A filter is
      // above the reach in that order because it is the outermost thing
      // the operator turned on: leaving the map filtered while surfacing
      // out of a reach would put them somewhere they did not ask to be.
      if (nodeCard) nodeCard = null
      else if (lineCard) closeLineCard()
      else if (compose) compose = null
      // The list (#1050, A1) is its own rung: Esc closes it first, a
      // second Esc clears the trace underneath it. The city has the same
      // rung in its own ladder (#1055) and owns it while it is the
      // surface being read: two handlers on one window would otherwise
      // take two rungs at once on a single press.
      else if (cityStop === null && mapTraceState.active && mapTraceState.listOpen) mapTraceState.listOpen = false
      else if (cityStop === null && mapTraceState.active) mapTraceState.clear()
      else if (cityStop === null && (portFilterState.active || portFilterState.open)) portFilterState.clear()
      else if (reach) surface()
    }
  }

  // --- the composer (#626/#633, round 2 scene 4) ---------------------------
  // A denial becomes a rule in two clicks: the port panel ranks what
  // the host has been asking for, and the command card prints the
  // RouterOS line for the operator to paste -- mikroview drafts, it
  // never runs (the observes-never-connects invariant). Identical for
  // viewer and admin, like the rest of the reach: nothing here mutates.
  let compose = $state<ReachStrand | null>(null)
  let composeMode = $state<'allow' | 'block'>('allow')
  let composePort = $state<number | null>(null)
  let composeFree = $state('')
  let composeScope = $state<'host' | 'subnet'>('host')
  let copied = $state(false)

  function openCompose(s: ReachStrand) {
    compose = s
    composeMode = 'allow'
    composePort = s.portHits[0]?.port ?? null
    composeFree = ''
    composeScope = 'host'
    copied = false
  }

  const counterpartIface = $derived(
    compose ? (compose.counterpart === 'internet' ? (zonesState.wanInterface ?? '') : compose.counterpart) : '',
  )
  const counterpartZone = $derived(compose && compose.counterpart !== 'internet' ? zones.find((z) => z.id === compose!.counterpart) : undefined)
  const composePeerAddr = $derived(compose?.peerAddrs[0] ?? '')
  const composePeerName = $derived(compose?.peers[0] ?? (compose?.counterpart === 'internet' ? 'the internet' : (compose?.counterpart ?? '')))
  const chosenPort = $derived.by((): { port: number; proto: string } | null => {
    const free = Number.parseInt(composeFree, 10)
    if (!Number.isNaN(free) && free > 0 && free < 65536) return { port: free, proto: 'tcp' }
    if (composePort === null) return null
    const hit = compose?.portHits.find((h) => h.port === composePort)
    return hit ? { port: hit.port, proto: hit.proto } : { port: composePort, proto: 'tcp' }
  })

  // The strand-to-command translation itself is reachComposeInput
  // (lib/compose.ts, #868): this panel's own allow/block toggle,
  // chosen port and host/subnet scope are just its overrides on the
  // same defaults the city's plainer composer calls unadorned, so the
  // two views print the same line for the same strand by construction,
  // not by coincidence.
  const composedCommand = $derived.by((): string | null => {
    if (!reach || !compose) return null
    const input = reachComposeInput(
      compose,
      { hostIp: reach.ip, hostName: reach.host, zoneId: reach.zoneId, wanInterface: zonesState.wanInterface, zones, edges: policyState.edges },
      { mode: composeMode, port: composePort, free: composeFree, scope: composeScope },
    )
    return input ? composeCommand(input) : null
  })

  const composePlaceBefore = $derived(
    reach && compose
      ? refusingCommentFor(
          policyState.edges,
          compose.direction === 'out' ? reach.zoneId : counterpartIface,
          compose.direction === 'out' ? counterpartIface : reach.zoneId,
        )
      : undefined,
  )

  async function copyCommand() {
    if (!composedCommand) return
    try {
      await navigator.clipboard.writeText(composedCommand)
      copied = true
      setTimeout(() => (copied = false), 1600)
    } catch {
      // Clipboard can be refused; the text stays selectable by hand.
    }
  }

  const reachSummary = $derived(reach ? reachFor(reach.subject, zonesState.wanInterface, appState.events) : null)

  /** The crumb's `refused N`: distinct counterparts that were refused,
   * counted the same way `reachFor` counts `reaches` and `reached by`, so
   * the three numbers on one line are the same kind of number. Not the
   * event count -- "refused 1" means one line was refused, which is what
   * the drawing shows one ✕ for. */
  const reachRefused = $derived(
    reachSummary === null ? 0 : new Set(reachSummary.strands.filter((s) => s.outcome === 'blocked').map((s) => s.counterpart)).size,
  )

  // The zone a strand's counterpart names, for its lane ink and label.
  function zoneIndex(id: string): number {
    return zones.findIndex((z) => z.id === id)
  }

  // Counterpart slots around the membrane, mockup-placed: first
  // top-left, second right, third lower-right; the internet is always
  // the band along the foot.
  const SLOTS = [
    { x: 240, y: 26, w: 330 },
    { x: 1010, y: 224, w: 270 },
    { x: 1000, y: 430, w: 270 },
  ]

  const reachCounterparts = $derived.by(() => {
    if (!reachSummary) return []
    const seen: string[] = []
    for (const s of reachSummary.strands) {
      if (s.counterpart !== 'internet' && !seen.includes(s.counterpart)) seen.push(s.counterpart)
    }
    return seen.slice(0, SLOTS.length)
  })

  const reachHasInternet = $derived(reachSummary?.strands.some((s) => s.counterpart === 'internet') ?? false)

  // Strand geometry: from the host's edge toward the counterpart. An
  // accepted strand passes the membrane; a blocked one dies at it.
  const MX = 560
  const MY = 330
  const MR = 152

  function strandTarget(counterpart: string): { x: number; y: number } {
    if (counterpart === 'internet') return { x: 700, y: 596 }
    const i = reachCounterparts.indexOf(counterpart)
    const s = SLOTS[i] ?? SLOTS[0]
    return { x: s.x + s.w / 2, y: s.y + 48 }
  }

  // Parallel offset so every strand to one counterpart reads as its own
  // line: direction splits the pair wide, outcome nudges within it.
  function strandOffset(outcome: string, direction: string): number {
    const dir = direction === 'out' ? 1 : -1
    return dir * (outcome === 'blocked' ? 18 : 8)
  }

  /** One strand's own three points: the host end, the far end, and the
   * control point between them. Pulled out of `strandPath` so the ring
   * and the card's leader can sit on the drawn line itself rather than
   * on a second guess at where it runs (the same reason `halfMid` exists
   * for a rib). */
  function strandEnds(
    counterpart: string,
    outcome: string,
    direction: string,
  ): { from: { x: number; y: number }; to: { x: number; y: number }; mid: { x: number; y: number } } {
    const t = strandTarget(counterpart)
    const dx = t.x - MX
    const dy = t.y - MY
    const len = Math.hypot(dx, dy) || 1
    const off = strandOffset(outcome, direction)
    const ox = (-dy / len) * off
    const oy = (dx / len) * off
    const from = { x: MX + (dx / len) * 48 + ox, y: MY + (dy / len) * 48 + oy }
    const to =
      outcome === 'blocked'
        ? { x: MX + (dx / len) * MR + ox, y: MY + (dy / len) * MR + oy }
        : { x: t.x - (dx / len) * 40 + ox, y: t.y - (dy / len) * 40 + oy }
    const mid = { x: (from.x + to.x) / 2 + ox * 0.6, y: (from.y + to.y) / 2 + oy * 0.6 }
    return { from, to, mid }
  }

  function strandPath(counterpart: string, outcome: string, direction: string): string {
    const e = strandEnds(counterpart, outcome, direction)
    return `M ${e.from.x} ${e.from.y} Q ${e.mid.x} ${e.mid.y}, ${e.to.x} ${e.to.y}`
  }

  /** Where the off-baseline ring throbs: "a small ring throbbing in
   * place at the end it arrived at". Out of the centred host, the
   * traffic arrived at the far end; into it, at the host's own end. */
  function strandRingPoint(s: ReachStrand): { x: number; y: number } {
    const e = strandEnds(s.counterpart, s.outcome, s.direction)
    return s.direction === 'out' ? e.to : e.from
  }

  /** The midpoint of the drawn strand, where its card's leader lands. */
  function strandMid(counterpart: string): { x: number; y: number } {
    const e = strandEnds(counterpart, 'accepted', 'out')
    // The quadratic's own midpoint, not the chord's: at t=0.5 a Q curve
    // sits halfway between the chord and the control point, which is
    // where the line is actually drawn.
    return { x: (e.from.x + 2 * e.mid.x + e.to.x) / 4, y: (e.from.y + 2 * e.mid.y + e.to.y) / 4 }
  }

  function membranePoint(counterpart: string, outcome: string, direction: string): { x: number; y: number; angle: number } {
    const t = strandTarget(counterpart)
    const dx = t.x - MX
    const dy = t.y - MY
    const len = Math.hypot(dx, dy) || 1
    const off = strandOffset(outcome, direction)
    return {
      x: MX + (dx / len) * MR + (-dy / len) * off,
      y: MY + (dy / len) * MR + (dx / len) * off,
      angle: (Math.atan2(dy, dx) * 180) / Math.PI + 90,
    }
  }

  // One anchor per counterpart (#976 item 3), not one per strand: the
  // membrane point above already fans each of a counterpart's up to
  // four strands (out/in x accepted/blocked) by a small perpendicular
  // offset, ±8 or ±18 -- enough to keep the *lines* apart, but a label
  // is much taller than 16-36 units, so their pills still landed on
  // each other. Every strand toward one counterpart now stacks its own
  // pill from this single, offset-free point instead, the same way the
  // cluster card's own chiprow list already stacks (#726's "stack or
  // thin", applied here to the membrane's own labels).
  function counterpartAnchor(counterpart: string): { x: number; y: number } {
    const t = strandTarget(counterpart)
    const dx = t.x - MX
    const dy = t.y - MY
    const len = Math.hypot(dx, dy) || 1
    return { x: MX + (dx / len) * MR, y: MY + (dy / len) * MR }
  }

  // A strand's place in its own counterpart's stack -- stable because
  // reachSummary.strands has one fixed order per render, not because
  // this reorders anything.
  function strandRank(s: ReachStrand): number {
    return reachSummary!.strands.filter((x) => x.counterpart === s.counterpart).indexOf(s)
  }

  /** The reach's own side of a line, for the sentences that read
   * `subject → counterpart`. A rib is already a pair, so it reads from
   * its near end rather than printing `LAN → Servers → Servers`. */
  const reachSideName = $derived(reach ? (reach.subject.kind === 'rib' ? laneName(reach.subject.a) : reach.host) : '')

  const reachZoneInk = $derived(reach ? LANE_INKS[Math.max(0, zoneIndex(reach.zoneId)) % LANE_INKS.length] : 'var(--accent)')

  /* ---------------- the line card (#1016, round 49) ---------------- */

  // Round 49's `flat-reach` scene: nothing is written on a strand, and
  // pointing at one opens the card that carries what used to be printed
  // along it -- every port tried, the protocol, what landed and what was
  // dropped, the totals, and on a refused line the rule that refused it.
  //
  // The reading is `reachLineSummary`, which merges a counterpart's
  // strands back into one line: `reachFor` splits accepted from dropped
  // and out from in, but "which ports were attempted here, and was each
  // dropped or accepted" is a question about the pair. Keyed by
  // counterpart for exactly that reason -- one card per drawn line, not
  // one per strand, so hovering either half of a pair reads the same.
  //
  // Same one interaction and the same shared placement (lib/cardAnchor)
  // as the boundary, host and off-baseline cards: beside its subject
  // with a leader, a grace period for the pointer to travel to it, and a
  // re-place when it changes size.
  let lineCard = $state<{ counterpart: string } | null>(null)
  let lineCardPinned = $state(false)
  let lineCardEl = $state<HTMLDivElement>()
  let lineCardPlace = $state<Placement | null>(null)
  let lineCardTick = $state(0)
  let membraneSvgEl = $state<SVGSVGElement>()
  const lineGrace = grace()

  /** The hovered line's own reading. Recomputed from the live summary
   * rather than kept from when the card opened, so a strand that keeps
   * carrying traffic keeps a card that says so. */
  const lineCardSummary = $derived(lineCard && reachSummary ? reachLineSummary(reachSummary.strands, lineCard.counterpart) : null)

  /** The strands behind the open card, for the actions it offers: the
   * composer's door needs the refused strand itself, not the merged
   * reading, because it drafts from that strand's own port hits. */
  const lineCardStrands = $derived(lineCard && reachSummary ? reachSummary.strands.filter((s) => s.counterpart === lineCard!.counterpart) : [])

  /** The refused strand a `draft the rule ▸` would compose from -- the
   * busiest, matching how `reachLineSummary` picks the rule it names. */
  const lineCardRefused = $derived(
    lineCardStrands.filter((s) => s.outcome === 'blocked').reduce<ReachStrand | null>((best, s) => (best === null || s.count > best.count ? s : best), null),
  )

  /** The lines this strand carries off the baseline today, for the card's
   * own bright note -- the same roll-up the strand is drawn by. */
  const lineCardOffLines = $derived(lineCard ? strandOffLines(lineCard.counterpart) : [])

  /** The counterpart's readable name: a zone's own name where the map has
   * one, 'the internet' for the WAN, the interface otherwise. */
  function counterpartName(id: string): string {
    if (id === 'internet') return 'the internet'
    return zones.find((z) => z.id === id)?.name ?? id
  }

  /** The counterpart's CIDR under the card's title, where the map has a
   * pushed one. Blank rather than invented for the internet and for a
   * boundary-derived zone with no address table. */
  const lineCardCidr = $derived(lineCard ? (zones.find((z) => z.id === lineCard!.counterpart)?.cidr ?? '') : '')

  /** The ports on this line that are off the baseline today, so the
   * table can mark the new one the way the mockup does. */
  const lineCardNewPorts = $derived(new Set(lineCardOffLines.map((l) => l.port).filter((p): p is number => p !== null)))

  /** Everything the line carried, for the tcp/udp picture's own scale.
   * The three counts total this by construction (lib/reach.ts). */
  const lineCardTotal = $derived(lineCardSummary ? lineCardSummary.tcp + lineCardSummary.udp + lineCardSummary.other : 0)

  /** The ports the refusal was about, as the card names them:
   * `:22 refused by #17 default drop`. Every dropped port, not just the
   * busiest -- naming one where three were refused would be a smaller
   * claim than the events make. */
  const lineCardRefusedPorts = $derived.by(() => {
    const ports = (lineCardSummary?.ports ?? []).filter((p) => p.dropped > 0).map((p) => `:${p.port}`)
    // Portless traffic can be refused too, and has no `:port` to print.
    return ports.length > 0 ? ports.join(' ') : 'this line'
  })

  /** The stream, filtered to whatever the reach is centred on -- the
   * same door the host card already offers, from the card that named
   * the line, and asked of the subject rather than always of an address
   * a zone and a rib do not have. */
  function openLineStream() {
    if (!reach) return
    const subj = reach.subject
    if (subj.kind === 'host') {
      appState.resetFilters()
      appState.setFilter('srcQuery', subj.ip)
      appState.view = 'live'
    } else if (subj.kind === 'zone') {
      openZone(subj.iface)
    } else {
      openPair(subj.a, subj.b, [])
    }
    closeLineCard()
  }

  // A card describing a line that stopped existing (the buffer rolled,
  // the reach changed) closes itself rather than standing there
  // describing nothing -- the same rule the off-baseline card follows.
  $effect(() => {
    if (lineCard && lineCardStrands.length === 0) closeLineCard()
  })

  $effect(() => {
    // Read first, so this re-runs on everything that moves the subject.
    const open = lineCard
    const svg = membraneSvgEl
    const host = topoEl
    const card = lineCardEl
    void stageTick
    void lineCardTick

    if (!open || !svg || !host || !card) {
      lineCardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      lineCardPlace = null
      return
    }
    const anchor = map(strandMid(open.counterpart))
    // The card names two ends, so it must not sit on either: the centred
    // host, and the counterpart cluster the strand runs to. Same
    // reasoning as the boundary card's two plates.
    const avoid = [mapRect(map, { x: MX - 56, y: MY - 56, w: 112, h: 112 })]
    const ci = reachCounterparts.indexOf(open.counterpart)
    if (ci >= 0) avoid.push(mapRect(map, { x: SLOTS[ci].x, y: SLOTS[ci].y, w: SLOTS[ci].w, h: 96 }))
    // Every other cluster is worth keeping clear too, but only as a
    // preference -- never at the cost of covering its own subject.
    const softAvoid = reachCounterparts
      .filter((c) => c !== open.counterpart)
      .map((c) => SLOTS[reachCounterparts.indexOf(c)])
      .filter((s) => s !== undefined)
      .map((s) => mapRect(map, { x: s.x, y: s.y, w: s.w, h: 96 }))
    lineCardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = lineCardEl
    if (!card) return
    return watchCardSize(card, () => lineCardTick++)
  })

  function openLineCard(counterpart: string) {
    lineGrace.hold()
    if (lineCard?.counterpart === counterpart) return
    // A pinned card is kept until it is let go, whichever card it is.
    if (lineCardPinned) return
    lineCardPinned = false
    lineCard = { counterpart }
  }

  /** The pointer has left the strand, or the card. It may be crossing
   * between them, so nothing comes down until the grace period has
   * passed with the pointer arriving at neither (#1027). */
  function releaseLineCard() {
    const open = lineCard
    if (!open || lineCardPinned) return
    lineGrace.release(() => {
      if (!lineCardPinned && lineCard?.counterpart === open.counterpart) closeLineCard()
    })
  }

  function closeLineCard() {
    lineGrace.hold()
    lineCard = null
    lineCardPinned = false
    lineCardPlace = null
  }

  /** Every card pins (DESIGN.md "Cards"). */
  function pinLineCard() {
    lineCardPinned = !lineCardPinned
    if (!lineCardPinned) closeLineCard()
  }

  const siblings = $derived.by(() => {
    // A rib stands between two lanes rather than in one, so it draws no
    // lane-mates: the hosts that use it are on its own line's card.
    if (!reach || reach.subject.kind === 'rib') return []
    const z = zones.find((zz) => zz.id === reach!.zoneId)
    return (z?.hosts ?? []).filter((h) => h.ip !== reach!.ip).slice(0, 2)
  })

  // --- the altitude slider (#648, concept T -- round 6 ratified, round
  // 14/24 amendments; joined to the city, #869) ---------------------------
  // One axis, seven stops (lib/altitude.ts): clients, services, zones,
  // then the city -- centred and the default -- then the city's own
  // borough, district, street. No text, a tiny symbol per stop on the
  // line itself (the city wears the atlas diamond now: it is the
  // survey), click anywhere on the line to jump to the nearest stop.
  // The three left of the diamond are a camera framing of the same real
  // 2D map -- closer for clients, a little back for services, the whole
  // estate flat and less detailed at zones (#852) -- never a fabricated
  // new layer of data this app does not have. The three right of it are
  // the city's own camera heights (lib/city/project.ts's STOPS).
  // Deliberately not reset by descend()/surface(): round 24's "keeps the
  // level and zoom you left" falls out of that for free, and now
  // carries across the join too (crossAltitudeCentre below).
  //
  // The last stop visited persists per user via altitudeStopState,
  // using the small-module pattern shared with colorway and retention.
  let altitude = $state<Altitude>(ALTITUDE_LABELS.indexOf(altitudeStopState.stop) as Altitude)
  const cityStop = $derived(isCityAltitude(altitude) ? STOPS[altitude - CENTRE_ALTITUDE] : null)

  // The one ground plan (#852, #869): the same estate the city already
  // lays out, computed once here rather than once per view. Passed down
  // to City below (its own `groundProp` fallback stays for City's own
  // tests, which mount it without a Topography) and read directly by
  // the zones stop's own flat drawing further down -- one source, so
  // the two views can never quietly drift into disagreeing position
  // models.
  // The declared-quiet boundary-directions (#392), read by everything
  // below that applies the coverage rule -- including the ground plan
  // itself, which used to know only the policy edges and so called a
  // declared boundary dark on the plaque and the zones card (#1014).
  const quietKeys = $derived(new Set(coverageState.byKey.keys()))

  // What each address is flagged and watched with (#981). Computed once
  // here and carried down inside the ground plan, so the city's marks
  // and this map's own halo and ring are two drawings of one reading
  // rather than two readings that agree today.
  const cityHostMarks = $derived(hostMarksFrom(flagsState.list, watchlistState.entries))

  const ground = $derived<Ground>(
    layoutGround(
      cityInputFrom(
        appState.devices,
        zones,
        appState.events,
        // #1090: `observed` (above) already is this same call over the
        // same three arguments -- reuse it instead of a second full
        // pass over the buffer on every recompute.
        observed,
        policyState.edges,
        policyState.anyPushed,
        primaryDevice?.id ?? null,
        zonesState.wanInterface,
        tunnelsState.list,
        policyState.pushed,
        quietKeys,
        [],
        cityHostMarks,
        // The retired segments, laid out as districts so the city draws
        // a ghost where the flat map draws a ghost lane (#460).
        ghostCityZones(decommissionsState.offers, decommissionsState.ghosts, nowMs, primaryDevice?.id ?? ''),
      ),
    ),
  )

  // A flat (non-isometric) fit of that same ground into the 2D stage,
  // for the zones stop below.
  const flatCam = $derived(flatFit(ground.bounds, 1400, 720))

  // District cards must never overlap (#976 follow-up): districts sit
  // on fixed slots (PRIMARY_SLOTS/BOROUGH_SLOTS, lib/city/layout.ts)
  // that can be close enough together to collide even at a conservative
  // size floor, and a floor alone only lowers the odds rather than
  // ruling it out. After each card's own size and position are
  // computed, this pairwise pass nudges any pair still closer than an
  // 8px gutter apart along whichever axis needs the smaller move, a
  // few iterations -- the same idea #726 used to separate edge labels,
  // applied here to boxes instead of points.
  interface FlatCard {
    d: District
    x: number
    y: number
    gr: number
    gh: number
  }
  const CARD_GUTTER = 8
  const CARD_PUSH_ITERATIONS = 6
  function pushCardsApart(cards: FlatCard[]): FlatCard[] {
    const out = cards.map((c) => ({ ...c }))
    for (let iter = 0; iter < CARD_PUSH_ITERATIONS; iter++) {
      let moved = false
      for (let a = 0; a < out.length; a++) {
        for (let b = a + 1; b < out.length; b++) {
          const c1 = out[a]
          const c2 = out[b]
          const dx = c2.x - c1.x
          const dy = c2.y - c1.y
          const overlapX = c1.gr + c2.gr + CARD_GUTTER - Math.abs(dx)
          const overlapY = c1.gh / 2 + c2.gh / 2 + CARD_GUTTER - Math.abs(dy)
          if (overlapX <= 0 || overlapY <= 0) continue // clear on at least one axis
          moved = true
          if (overlapX < overlapY) {
            const push = overlapX / 2
            const dir = dx === 0 ? 1 : Math.sign(dx)
            c1.x -= push * dir
            c2.x += push * dir
          } else {
            const push = overlapY / 2
            const dir = dy === 0 ? 1 : Math.sign(dy)
            c1.y -= push * dir
            c2.y += push * dir
          }
        }
      }
      if (!moved) break
    }
    return out
  }
  // #989: every card sits beside the router that serves it. The ground
  // plan already puts each district on a slot around its own router
  // (PRIMARY_SLOTS/BOROUGH_SLOTS, lib/city/layout.ts), but those slots
  // are ground units while the cards have a screen-space size floor, so
  // at this stop a card could cover the very router it belongs to -- and
  // the push-apart pass below then moved it wherever there happened to
  // be room, which is how the association was lost (owner on #976's
  // final shot: "there's no lines to the blocks"). Keep the slot's
  // direction, which is the arrangement the owner ratified, and slide
  // the card straight out along it until its near edge clears the
  // router dot.
  const ROUTER_CLEARANCE = 16
  interface FlatHome {
    x: number
    y: number
    r: number
  }
  const flatHomes = $derived.by(() => {
    const homes = new Map<string, FlatHome>()
    for (const n of ground.nodes) {
      if (n.kind !== 'router' && n.kind !== 'router-ant') continue
      // Same radius the node's own circle is drawn with, below.
      homes.set(n.id, { x: FX(flatCam, n.u), y: FY(flatCam, n.v), r: Math.max(4, n.R * flatCam.S * 0.6) })
    }
    return homes
  })
  const flatCards = $derived.by(() =>
    pushCardsApart(
      ground.districts.map((d) => {
        // #1013: half-width from the larger of the host-count size and
        // what the plate's own name/subnet text needs -- see
        // plateHalfWidth's own doc comment for the text-metric estimate
        // and why it also folds in the old `Math.max(38, ...)` floor.
        const gr = plateHalfWidth(d, flatCam.S)
        // 56, not 44: the card's third line (host count, at -gh/2+48
        // below) needs the plate's own bottom edge past 48 with some
        // padding, or that line prints below the opaque plate rather
        // than on it -- the map's own roads then show straight through
        // "0 hosts" rather than stopping at the card's edge (owner
        // review, #976 follow-up: it read as "lines painted through the
        // card," but the actual cause was this card being too short for
        // its own text, not the roads drawn in the wrong order -- they
        // were always behind the plate, they just had nothing solid to
        // stop against past its bottom edge). The push-apart pass above
        // absorbs the size increase by spacing cards further apart, so
        // growing this floor no longer risks two cards colliding.
        const gh = Math.max(56, gr * 0.84)
        let x = FX(flatCam, d.u)
        let y = FY(flatCam, d.v)
        const home = flatHomes.get(d.routerId)
        if (home) {
          const dx = x - home.x
          // A district whose slot projects straight onto its own router
          // has no direction to keep, so it goes south, where the ground
          // plan puts a borough's own row.
          const dy = dx === 0 && y === home.y ? 1 : y - home.y
          const len = Math.hypot(dx, dy)
          const ux = dx / len
          const uy = dy / len
          // How far the card reaches back towards the router along that
          // direction -- the rectangle's own support radius, so a wide
          // card approached from the side clears by its width and a
          // tall one from above clears by its height.
          const reach = Math.abs(ux) * gr + Math.abs(uy) * (gh / 2)
          const want = home.r + ROUTER_CLEARANCE + reach
          if (len < want) {
            x = home.x + ux * want
            y = home.y + uy * want
          }
        }
        return { d, x, y, gr, gh }
      }),
    ),
  )

  // The "belongs to" line (#989): router dot to the nearest edge of the
  // card, saying only that this subnet lives on that router. Deliberately
  // not a road -- roads are seen traffic, coloured by verdict and drawn
  // over the top of these; a district with no traffic yet has its grey
  // line and nothing else. Computed from the pushed positions, so a card
  // the push-apart pass moved keeps its line attached.
  const flatBelongs = $derived.by(() =>
    flatCards.flatMap((fc) => {
      const home = flatHomes.get(fc.d.routerId)
      if (!home) return []
      const dx = fc.x - home.x
      const dy = fc.y - home.y
      const len = Math.hypot(dx, dy)
      if (len === 0) return []
      // Where the line meets the card: scale the direction back until it
      // reaches whichever of the card's own edges comes first.
      const t = Math.min(dx === 0 ? Infinity : fc.gr / Math.abs(dx), dy === 0 ? Infinity : fc.gh / 2 / Math.abs(dy))
      return [
        {
          id: fc.d.id,
          x1: home.x + (dx / len) * home.r,
          y1: home.y + (dy / len) * home.r,
          x2: fc.x - dx * t,
          y2: fc.y - dy * t,
        },
      ]
    }),
  )

  // Crossing the centre swaps which side draws. The lens is one piece
  // of state already shared by both (`lens`, threaded straight into
  // City), so it carries for free. The reach and the city's own
  // "standing on a building" (#868) are each private to their own
  // side, so a host reach carries only if the same host exists as a
  // building on the other side -- otherwise it resets rather than
  // half-applying (a router or a bridge post has no 2D reach to land
  // on). The city's pan is saved across its own mount/unmount --
  // City only ever exists in the DOM while a city stop is active (an
  // existing test proves it) -- and restored on return: `cityView` is
  // that save slot, `cityStandBuilding` the building (if any) the city
  // currently has stood on, both kept current by the callbacks passed
  // to <City> below.
  let cityView = $state<{ S: number; centre: [number, number] } | null>(null)
  let cityStandBuilding = $state<{ ip: string; name: string; districtId: string | null; kind: string } | null>(null)

  function crossAltitudeCentre(intoCity: boolean) {
    if (intoCity) {
      // The trace no longer clears on the way over (round 56, #1050,
      // same rule the port filter follows for #1055): the city draws its
      // own copy of the traced road, the ✕ and the ghost, and mounts the
      // same crumb, so the selection survives the crossing in both
      // directions the same way the port filter's does. Clearing it here
      // would throw away the operator's own question for moving the
      // slider, onto a view that can now answer it.
      if (reach && reachIsHost) {
        // Handed to City's own pending-descend effect (#868's own
        // consumer, shared with the flags "where" link) rather than
        // duplicated here: it already resolves an ip to a building, or
        // leaves the request unconsumed when none exists, which is
        // exactly "resets cleanly" for this direction.
        topologyNavState.pendingDescend = { zoneId: reach.zoneId, host: reach.host, ip: reach.ip }
        surface()
      }
    } else if (cityStandBuilding && cityStandBuilding.kind === 'host' && cityStandBuilding.districtId) {
      const z = zones.find((zz) => zz.id === cityStandBuilding!.districtId)
      const host = z?.hosts.find((h) => h.ip === cityStandBuilding!.ip)
      if (z && host) descend(z.id, host.label, host.ip)
    }
  }

  function jumpAltitude(i: number) {
    const next = Math.max(0, Math.min(ALTITUDE_LABELS.length - 1, Math.round(i))) as Altitude
    if (next === altitude) return
    const wasCity = isCityAltitude(altitude)
    const willBeCity = isCityAltitude(next)
    if (wasCity !== willBeCity) crossAltitudeCentre(willBeCity)
    altitude = next
    altitudeStopState.set(ALTITUDE_LABELS[next])
  }

  function onAltitudeInput(e: Event) {
    jumpAltitude(Number((e.currentTarget as HTMLInputElement).value))
  }

  // --- the health dials (#648, rounds 19-20: "love the dials") -----------
  // Two rings, top-right: flags splits by the mark grammar (✱ alarm /
  // ▲ advisory), watchers splits by #546's broken ring (healthy / broken).
  // Solid var(--accept) whenever there is nothing to report -- the "at
  // rest" state -- and each ring's own symbol wears its ink regardless
  // (the flag red, the eye the docket's own watcher purple). Both click
  // through to the docket, the same door the scene bar's flag badge uses.
  const activeFlags = $derived(flagsState.list.filter((f) => !f.cleared))
  const alarmFlagCount = $derived(activeFlags.filter((f) => familyOf(f.type).mark === '✱').length)
  const advisoryFlagCount = $derived(activeFlags.length - alarmFlagCount)
  // A ghost is a watched thing, so the dials count it: round 55's "the
  // header counts move with the watch -- ◉ +1 while it holds, ○ +1 while
  // it is broken". A decommission watch is a programmatic definition and
  // never reaches the definitions store, so it cannot be double-counted
  // through watchlistState.
  const ghostWatchers = $derived(ghostLanes.filter((g) => g.watch !== null).length)
  const ghostWatchersBroken = $derived(ghostLanes.filter((g) => g.watch !== null && g.state === 'broken').length)
  const watcherTotal = $derived(watchlistState.entries.length + ghostWatchers)
  const watcherBroken = $derived(watchlistState.brokenCount + ghostWatchersBroken)
  const watcherHealthy = $derived(watcherTotal - watcherBroken)

  const DIAL_R = 20
  const DIAL_CIRC = 2 * Math.PI * DIAL_R

  function ringArc(count: number, total: number, priorLen: number): { dasharray: string; offset: number } {
    const len = total > 0 ? (count / total) * DIAL_CIRC : 0
    return { dasharray: `${len} ${DIAL_CIRC - len}`, offset: -priorLen }
  }

  function openDocket(view: 'flags' | 'watchlist') {
    appState.view = view
  }

  // --- the dials' condensed panel (#724: "clicking on the dial once
  // expands a small condensed view of the issues. Second click takes you
  // to the appropriate pages.") ------------------------------------------
  // A dial is now a two-step control: first click expands a small panel
  // beneath it, worst first, capped at five rows plus an "and N more"
  // row; a second click on a *row* is what leaves for the docket -- the
  // dial itself only ever opens or closes the panel, never navigates.
  //
  // Note on scope (2026-08-31): the row's own destination is only "the
  // right tab" here (appState.view). Actually landing on that one flag's
  // or watch's own drawer needs a small pending-selection consumed by
  // Flags.svelte/Watchlist.svelte, the same shape topologyNavState
  // already gives the reverse direction (docket -> topography host).
  // This change's file scope is this component alone, so that consumer
  // side isn't built yet -- see the issue thread for the follow-up.
  type DialKind = 'flags' | 'watchlist'
  const PANEL_ROW_CAP = 5

  let expandedDial = $state<DialKind | null>(null)
  let flagsDialEl: HTMLButtonElement | undefined = $state()
  let watchDialEl: HTMLButtonElement | undefined = $state()
  let flagsWrapEl: HTMLDivElement | undefined = $state()
  let watchWrapEl: HTMLDivElement | undefined = $state()
  // Shared by whichever panel is actually rendered -- only one is ever
  // open at a time, so there is never a stale array to confuse with a
  // live one.
  let panelRowEls: (HTMLButtonElement | undefined)[] = $state([])
  let panelZeroEl: HTMLParagraphElement | undefined = $state()

  function toggleDialPanel(which: DialKind) {
    expandedDial = expandedDial === which ? null : which
  }

  function closeDialPanel() {
    expandedDial = null
  }

  // Escape is the keyboard user's way out (owner-approved #724 keyboard
  // section): closes the panel *and* hands focus back to the dial that
  // opened it, since destroying the panel alone would drop focus to the
  // document body.
  function closeDialPanelToTrigger(which: DialKind) {
    expandedDial = null
    ;(which === 'flags' ? flagsDialEl : watchDialEl)?.focus()
  }

  // Wired up as a window listener (below), not a template `onkeydown` on
  // the panel div: that div deliberately carries no role (Care, #724),
  // and Svelte's own a11y check flags a keydown handler on a roleless
  // static element, so the handler lives in script instead of markup.
  function onPanelKeydown(e: KeyboardEvent, which: DialKind) {
    if (e.key === 'Escape') {
      e.stopPropagation()
      closeDialPanelToTrigger(which)
      return
    }
    if (e.key === 'Tab') {
      // Deliberately not trapped (unlike lib/focusTrap.ts's sheets):
      // Tab leaves the panel and closes it behind you, letting the
      // browser's own default action carry focus onward.
      closeDialPanel()
      return
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const rows = panelRowEls.filter((el): el is HTMLButtonElement => !!el)
      if (rows.length === 0) return
      e.preventDefault()
      const at = rows.indexOf(document.activeElement as HTMLButtonElement)
      const next = e.key === 'ArrowDown' ? (at + 1) % rows.length : (at - 1 + rows.length) % rows.length
      rows[next]?.focus()
    }
  }

  // Moves focus into the panel exactly once, on the open transition --
  // guarded by a plain (non-reactive) local rather than re-running on
  // every later change to the row list, which would otherwise yank
  // keyboard focus back to row one on a live update while the operator
  // is still reading it.
  let focusedForDial: DialKind | null = null
  $effect(() => {
    const which = expandedDial
    if (which && which !== focusedForDial) {
      ;(panelRowEls.find((el): el is HTMLButtonElement => !!el) ?? panelZeroEl)?.focus()
    }
    focusedForDial = which
  })

  // Click-away (owner: "just click somewhere else on the page"): a
  // capturing listener on window sees the click before the element under
  // the pointer does, so closing here and stopping propagation spends
  // the click on dismissal alone -- it never also reaches whatever was
  // underneath. Clicking the *other* dial while a panel is open counts
  // as "somewhere else" by the same rule: it closes the open panel
  // rather than also opening the other one, which the click-away rule
  // above requires as a direct consequence.
  $effect(() => {
    if (!expandedDial) return
    const wrap = expandedDial === 'flags' ? flagsWrapEl : watchWrapEl
    function onClickAway(e: MouseEvent) {
      if (wrap && e.target instanceof Node && wrap.contains(e.target)) return
      expandedDial = null
      e.stopPropagation()
      e.preventDefault()
    }
    window.addEventListener('click', onClickAway, true)
    return () => window.removeEventListener('click', onClickAway, true)
  })

  // Escape/Tab/Up/Down (#724's keyboard section), scoped to keydowns
  // that land inside this dial's own wrap (its trigger or its panel) so
  // pressing Escape somewhere unrelated on the page while the panel
  // happens to be open doesn't steal focus back to the dial.
  $effect(() => {
    if (!expandedDial) return
    const which = expandedDial
    const wrap = which === 'flags' ? flagsWrapEl : watchWrapEl
    function onKeydown(e: KeyboardEvent) {
      if (!(wrap && e.target instanceof Node && wrap.contains(e.target))) return
      onPanelKeydown(e, which)
    }
    window.addEventListener('keydown', onKeydown)
    return () => window.removeEventListener('keydown', onKeydown)
  })

  function familyName(mark: string): string {
    return mark === '✱' ? 'Alarm' : 'Advisory'
  }

  interface FlagPanelRow {
    key: string
    mark: string
    ink: string
    label: string
    time: string
    count: number
    ariaLabel: string
  }

  // Worst first: alarms before advisories, most recently fired within
  // each -- the docket's own default read (age, newest first).
  const sortedActiveFlags = $derived(
    [...activeFlags].sort((a, b) => {
      const am = familyOf(a.type).mark === '✱' ? 0 : 1
      const bm = familyOf(b.type).mark === '✱' ? 0 : 1
      return am !== bm ? am - bm : new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime()
    }),
  )

  // The row's own words are the docket flag row's own words -- mark,
  // type label, target, count -- not the drawer's prose headline, which
  // a glance at the panel has not earned yet (nothing learned here has
  // to be unlearned there).
  const flagPanelRows = $derived(
    sortedActiveFlags.slice(0, PANEL_ROW_CAP).map((f: Flag): FlagPanelRow => {
      const fam = familyOf(f.type)
      const label = FLAG_TYPE_LABELS[f.type] ?? f.type
      const time = formatHM(f.lastSeen)
      return {
        key: f.id,
        mark: fam.mark,
        ink: fam.ink,
        label,
        time,
        count: f.count,
        ariaLabel: `${familyName(fam.mark)}: ${label}, ×${f.count}, ${time}, ${f.target}. Opens this flag in the docket.`,
      }
    }),
  )

  const flagPanelMoreCount = $derived(Math.max(0, sortedActiveFlags.length - PANEL_ROW_CAP))

  const lastClearedFlag = $derived.by((): Flag | null => {
    let best: Flag | null = null
    for (const f of flagsState.list) {
      if (!f.cleared || !f.clearedAt) continue
      if (!best?.clearedAt || new Date(f.clearedAt) > new Date(best.clearedAt)) best = f
    }
    return best
  })

  // At rest, the panel still opens and says so (#724) -- honest about
  // "nothing is open" rather than a click that silently does nothing.
  // No invented clear time when nothing has ever been cleared.
  const flagsZeroLine = $derived(
    lastClearedFlag?.clearedAt ? `no open flags · the last cleared at ${formatHM(lastClearedFlag.clearedAt)}` : 'no open flags',
  )

  const flagPanelGroupLabel = $derived(
    activeFlags.length === 0
      ? 'no open flags'
      : `${activeFlags.length} open flag${activeFlags.length === 1 ? '' : 's'}, worst first`,
  )

  // #724's second click: the row's own destination, not just the right
  // tab. requestFlag stashes which flag this row was so Flags.svelte can
  // open that flag's drawer on arrival (topologyNav.svelte.ts) -- the
  // "and N more" row below deliberately never calls this, since the
  // owner's ruling has it land with nothing selected.
  function activateFlagRow(id: string) {
    closeDialPanel()
    topologyNavState.requestFlag(id)
    openDocket('flags')
  }

  function activateFlagMore() {
    closeDialPanel()
    appState.resetFilters()
    openDocket('flags')
  }

  interface WatchPanelRow {
    key: string
    glyph: string
    broken: boolean
    name: string
    boundary: string
    detail: string
    ariaLabel: string
  }

  function watchIsBroken(e: WatchlistEntry): boolean {
    return e.enabled && (watchlistState.coverage[e.id] === 'no-logging' || !!e.ring?.broken)
  }

  function watchBoundary(e: WatchlistEntry): string {
    const source = e.source?.mac ?? e.source?.ip ?? 'any source'
    const dest = e.destIp ? e.destIp : e.invert ? 'its observed destinations' : 'any destination'
    return `${source} → ${dest}`
  }

  // Either the nights summary (lib/watchWindow.ts's own ratified copy,
  // "five kept nights · two empty") or, for a broken ring, why it broke
  // -- no-logging read live from router state, or a recorded break read
  // as a duration since the first empty window.
  function watchDetail(e: WatchlistEntry): string {
    if (watchlistState.coverage[e.id] === 'no-logging') return 'no logging visible'
    if (e.ring?.broken) {
      if (e.ring.since) return `no events for ${formatRelative(e.ring.since, appState.now).replace(/ ago$/, '')}`
      return 'no events recorded'
    }
    return nightlySummary(e.nights) ?? 'watching'
  }

  // Broken ones first (#724); stable otherwise, so a row doesn't hop
  // around the panel on every poll.
  const sortedWatchEntries = $derived(
    [...watchlistState.entries].sort((a, b) => Number(watchIsBroken(b)) - Number(watchIsBroken(a))),
  )

  const watchPanelRows = $derived(
    sortedWatchEntries.slice(0, PANEL_ROW_CAP).map((e: WatchlistEntry): WatchPanelRow => {
      const broken = watchIsBroken(e)
      const name = e.name || '(unnamed)'
      const boundary = watchBoundary(e)
      const detail = watchDetail(e)
      return {
        key: e.id,
        glyph: broken ? '○' : '◉',
        broken,
        name,
        boundary,
        detail,
        ariaLabel: `${broken ? 'Ring broken' : 'Watching'}: ${name}, ${boundary}, ${detail}. Opens this watch in the docket.`,
      }
    }),
  )

  const watchPanelMoreCount = $derived(Math.max(0, sortedWatchEntries.length - PANEL_ROW_CAP))

  const watchesZeroLine = 'no watches yet'

  const watchPanelGroupLabel = $derived(
    watcherTotal === 0 ? 'no watches' : `${watcherTotal} watcher${watcherTotal === 1 ? '' : 's'}, broken first`,
  )

  // Same handoff as activateFlagRow above, for the watchlist tab.
  function activateWatchRow(id: string) {
    closeDialPanel()
    topologyNavState.requestWatch(id)
    openDocket('watchlist')
  }

  function activateWatchMore() {
    closeDialPanel()
    appState.resetFilters()
    openDocket('watchlist')
  }

  // --- the aggregate bar (#648, round 23: "LOVE this, this is what we
  // needed" -- supersedes round-14's two-bar concept C) -------------------
  // One bar per zone: absent when nothing is open or watched on it,
  // purple-only, red-only, or split half red / half purple with a centre
  // divider when both. Correlated by IP-in-CIDR against the zone's own
  // pushed range, the same containment addressInCidr already answers for
  // the filter boxes and NAT parity (lib/addressMatch.ts) -- a flag or
  // watch counts toward a zone only when its address actually falls
  // inside it, never a guess. A degraded zone (no CIDR pushed yet) has
  // nothing to correlate against, so its bar stays absent rather than
  // drawing a wrong one.
  interface ZoneAggregate {
    flagCount: number
    watchCount: number
    watchBroken: number
  }

  function zoneAggregate(z: ZoneInfo): ZoneAggregate | null {
    if (!z.cidr) return null
    const cidr = parseCidr(z.cidr)
    if (!cidr) return null
    const inZone = (ip: string | null | undefined) => !!ip && addressInCidr(ip, cidr)
    const flagCount = activeFlags.filter((f) => inZone(extractSourceIp(f.target))).length
    const touching = watchlistState.entries.filter((e) => inZone(e.source?.ip) || inZone(e.destIp))
    const watchBroken = touching.filter((e) => e.enabled && watchlistState.coverage[e.id] === 'no-logging').length
    return { flagCount, watchCount: touching.length, watchBroken }
  }

  // The bar's flag half opens the docket filtered to whatever the bar
  // belongs to -- a lane, or (since #699) the internet island, which is
  // not a ZoneInfo. Only the boundary and its name are ever needed.
  function openZoneFlags(e: Event, z: { id: string; name: string }) {
    e.stopPropagation()
    appState.resetFilters()
    if (z.id) appState.setFilter('interface', z.id)
    appState.view = 'flags'
  }

  // The internet island's own aggregate (#699; round 30 draws a split
  // bar under it, the-whole.html:966-973). "On the internet" is a public
  // address -- isPublicIp, the same read wanInterface is derived from --
  // never "an address no lane claimed", which would turn an incomplete
  // address push into a false claim.
  const internetAggregate = $derived.by((): ZoneAggregate | null => {
    const flagCount = activeFlags.filter((f) => isPublicIp(extractSourceIp(f.target) ?? undefined)).length
    const touching = watchlistState.entries.filter((e) => isPublicIp(e.source?.ip) || isPublicIp(e.destIp ?? undefined))
    const watchBroken = touching.filter((e) => e.enabled && watchlistState.coverage[e.id] === 'no-logging').length
    if (flagCount === 0 && touching.length === 0) return null
    return { flagCount, watchCount: touching.length, watchBroken }
  })

  function openWatchlist(e: Event) {
    e.stopPropagation()
    appState.view = 'watchlist'
  }

  // A rounded-rect path for the bar's outer half(es) -- SVG's <rect> only
  // takes one radius for all four corners, and the split bar needs one
  // end square at the centre divider, so each half draws its own path.
  function fullBarPath(x0: number, x1: number, h: number): string {
    const r = h / 2
    return `M ${x0 + r} 0 H ${x1 - r} A ${r} ${r} 0 0 1 ${x1} ${r} V ${h - r} A ${r} ${r} 0 0 1 ${x1 - r} ${h} H ${x0 + r} A ${r} ${r} 0 0 1 ${x0} ${h - r} V ${r} A ${r} ${r} 0 0 1 ${x0 + r} 0 Z`
  }

  function leftBarPath(x0: number, x1: number, h: number): string {
    const r = h / 2
    return `M ${x0 + r} 0 H ${x1} V ${h} H ${x0 + r} A ${r} ${r} 0 0 1 ${x0} ${h - r} V ${r} A ${r} ${r} 0 0 1 ${x0 + r} 0 Z`
  }

  function rightBarPath(x0: number, x1: number, h: number): string {
    const r = h / 2
    return `M ${x0} 0 H ${x1 - r} A ${r} ${r} 0 0 1 ${x1} ${r} V ${h - r} A ${r} ${r} 0 0 1 ${x1 - r} ${h} H ${x0} Z`
  }

  // --- node info cards (#648, rounds 22-23: "really good") ---------------
  // A small glass card -- name, address, lane, open warnings, actions --
  // for a node that has no single-purpose click of its own yet: the
  // reach's counterpart clusters and its own centred host are inert
  // today. A zone card's host link already does its most useful job on
  // click (entering the reach, #626) and stays exactly that -- this is
  // additive furniture, not a replacement for it.
  interface NodeCard {
    name: string
    address: string | null
    lane: string | null
    flagCount: number
    watchCount: number
    x: number
    y: number
  }

  let nodeCard = $state<NodeCard | null>(null)

  function nodeWarnings(ip: string | null): { flagCount: number; watchCount: number } {
    if (!ip) return { flagCount: 0, watchCount: 0 }
    const flagCount = activeFlags.filter((f) => extractSourceIp(f.target) === ip).length
    const watchCount = watchlistState.entries.filter((e) => e.source?.ip === ip || e.destIp === ip).length
    return { flagCount, watchCount }
  }

  // Anchored to the pointer for a click; a keyboard activation carries no
  // coordinates, so it falls back to the activated element's own centre.
  function openNodeCard(e: MouseEvent | KeyboardEvent, name: string, address: string | null, lane: string | null) {
    e.stopPropagation()
    const w = nodeWarnings(address)
    const rect = (e.currentTarget as Element).getBoundingClientRect()
    const px = e instanceof MouseEvent ? e.clientX : rect.left + rect.width / 2
    const py = e instanceof MouseEvent ? e.clientY : rect.top + rect.height / 2
    nodeCard = {
      name,
      address,
      lane,
      flagCount: w.flagCount,
      watchCount: w.watchCount,
      x: Math.min(px + 14, window.innerWidth - 260),
      y: Math.min(py + 10, window.innerHeight - 220),
    }
  }

  function closeNodeCard() {
    nodeCard = null
  }

  function onWindowClick(e: MouseEvent) {
    if (nodeCard) nodeCard = null
    // The picker has no Done button and needs none: clicking away from
    // it is what closes it, and the selection it made stays on the map.
    // Guarded on the row itself rather than on the bar, so a click on
    // the ✕ beside it is not read as a click away from it.
    //
    // isConnected is the other half, and it is not defensive padding:
    // the click that *opens* the picker lands on the idle pill, which
    // this same click has just replaced with the bar. By the time the
    // window sees it, that button is detached, its ancestor chain is
    // gone, and `closest` answers null -- so without this the picker
    // would close on the click that opened it, every time.
    if (portFilterState.open && !(e.target instanceof Element && (!e.target.isConnected || e.target.closest('.pills')))) {
      portFilterState.closePicker()
    }
  }

  // The degraded map (#802; round 36, carried into rounds 37-38).
  // Round 29's floating "zones are boundary-derived" note is gone rather
  // than re-mounted: round 36 draws no note over the map at all
  // (round-36/README.md, "#s3 -- the degraded note"). The one statement
  // lives on the router card where its other pushed-table facts live
  // (the-whole.html:997-998), and every address slot holds what it truly
  // holds -- the `#s3.degraded` rules at the-whole.html:118-125.
  //
  // Only while the map itself is what is on screen: under a reach the map
  // is a blurred backdrop whose single affordance is surfacing again, and
  // a live "Run setup… ▸" behind the blur would be a second one.
  const degradedStatement = $derived(zonesState.degraded && zones.length > 0 && !reach)

  // The WAN card's own address slot. zonesState.zones deliberately drops
  // the wan interface from the lanes, so the pushed row for the internet
  // boundary is read here rather than through a zone.
  const wanAddress = $derived(
    zonesState.pushed.find((a) => !!a.interface && a.interface === zonesState.wanInterface)?.address ?? null,
  )

  // --- the tunnel cluster (#877, then #890's round 52) ---------------------
  // Round 30 drew one tunnel node beside Internet: `WireGuard` /
  // `wg0 · 10.99.0.0/24`, with its own watch bar (the-whole.html:986).
  // #877 built that one and left a second tunnel as a design question;
  // rounds 50-52 answered it and #890 builds the answer. Every pushed
  // tunnel now gets that same card, in one group in the space right of
  // the router, packed by lib/topography/cluster.ts's rule -- busiest
  // nearest the router, the busiest keeping round 30's literal seat, so
  // with one tunnel nothing moves. None of them is a lane any more:
  // zonesState.tunnelOrder is what the lane row drops.
  const tunnelIfaces = $derived(zonesState.tunnelOrder)

  /** Every tunnel interface's own event count, in one pass over the
   * buffer (#1090) -- what separates an API `up` that is carrying
   * traffic from one that is lit and empty, read per-card below rather
   * than re-scanned per card. */
  const tunnelCounts = $derived.by(() => tunnelEventCounts(appState.events))

  /**
   * `wg0 · 10.99.0.0/24`, as drawn: the tunnel's own row in the pushed
   * /ip address table, read the way wanAddress reads the WAN's.
   *
   * The network form rather than the host address because that is what
   * round 30 draws here, and RouterIPAddress carries `network`
   * alongside `address` -- so no arithmetic and no guess. Null where no
   * address has been pushed for the tunnel: the slot then says so,
   * rather than inferring a range from a peer's allowed address, which
   * is a /32 of one peer and not the tunnel's subnet at all.
   */
  function tunnelSubnetOf(iface: string): string | null {
    const row = zonesState.pushed.find((a) => a.interface === iface)
    if (!row) return null
    const slash = row.address.indexOf('/')
    const prefix = slash === -1 ? '' : row.address.slice(slash + 1)
    if (row.network && prefix) return `${row.network}/${prefix}`
    return row.address || null
  }

  /** How long since this tunnel was last heard from, in the card's own
   * wording -- the `3 d` of round 52's quiet footprint. Null where no
   * peer carried a handshake the server could date: "how long" is then
   * not a question the pushed data answers. */
  function tunnelQuietFor(lastHeard: string | null): string | null {
    if (!lastHeard) return null
    const t = Date.parse(lastHeard)
    if (Number.isNaN(t)) return null
    return spanLabel(nowMs - t)
  }

  /** What the map draws for one tunnel, before it is given a place. */
  interface TunnelCard {
    iface: string
    subnet: string | null
    state: ReturnType<typeof bridgeStateFor>
    stateLabel: string
    covClass: string
    /** Not heard for a while: round 52 draws its footprint dashed, with
     * the span at the card's right edge. */
    quiet: boolean
    quietFor: string | null
    aggregate: ZoneAggregate | null
  }

  const tunnelCards = $derived.by((): TunnelCard[] =>
    tunnelIfaces.map((iface): TunnelCard => {
      const api = tunnelsState.list.find((t) => t.iface === iface) ?? null
      const events = tunnelCounts.get(iface) ?? 0
      // 'quiet' is mikroview's own reading on top of the API's
      // vocabulary; the state words themselves are the city's.
      const state = bridgeStateFor(api?.apiState ?? null, events)
      const subnet = tunnelSubnetOf(iface)
      return {
        iface,
        subnet,
        state,
        stateLabel: bridgeStateLabel(state),
        covClass: state === 'down' ? 'cov-d' : 'cov-q',
        quiet: state === 'down',
        quietFor: state === 'down' ? tunnelQuietFor(api?.lastHeard ?? null) : null,
        /** The node's watch bar. Correlated exactly as a lane's is --
         * through the pushed CIDR -- so no third correlation rule enters
         * the scene, and absent entirely while no address names the
         * tunnel's range, which is the same refusal zoneAggregate makes. */
        aggregate: subnet
          ? zoneAggregate({ id: iface, name: iface, cidr: subnet, hosts: [], hostCount: 0, eventCount: events })
          : null,
      }
    }),
  )

  /** The furniture the cluster packs around: what is already drawn, in
   * map units. A pocket may touch none of it. */
  const mapFurniture = $derived.by(() => {
    // The Internet island (200x60 about its own point) with the
    // aggregate bar that hangs under it, and the waist -- which grows
    // to hold the degraded statement, so the box grows with it.
    const boxes: Box[] = [
      { x: 600, y: 38, w: 200, h: 80 },
      { x: 572, y: 234, w: 256, h: degradedStatement ? 100 : 68 },
    ]
    const curves = [
      samplePath([
        [700, 104],
        [700, 150],
        [700, 190],
        [700, 232],
      ]),
    ]
    zones.forEach((_z, i) => {
      // The lane card, its label above it and its aggregate bar below.
      boxes.push({ x: laneX(i, laneCount) - cardHalf, y: 466, w: cardW, h: 150 })
      curves.push(samplePath(ribCurve(i, laneCount)))
    })
    return { boxes, curves }
  })

  /** Every tunnel with the place the packing rule gave it. */
  const drawnTunnels = $derived.by((): (TunnelCard & { placed: Placed })[] => {
    const placed = packCluster({
      count: tunnelCards.length,
      bars: tunnelCards.map((t) => t.aggregate !== null),
      obstacles: mapFurniture,
    })
    return tunnelCards.map((t, i) => ({ ...t, placed: placed[i] }))
  })

  /** The busiest tunnel -- the one that keeps round 30's seat, and the
   * one the ghost reference line belongs to. */
  const tunnelIface = $derived(tunnelIfaces[0] ?? null)

  /**
   * The ghost reference line's far end: the lane the tunnel's traffic
   * actually reaches most, or null while nothing has been observed
   * crossing it.
   *
   * Round 30 draws this line ending inside a lane card rather than at
   * the router (`the-whole.html:936` runs to 610,476, the srv lane's
   * top edge), which reads as where the tunnel's traffic goes -- a
   * reference, drawn faintly, not a policy edge. Absent rather than
   * guessed: a tunnel nobody has crossed has no such destination.
   */
  const tunnelGhostLane = $derived.by((): number | null => {
    if (!tunnelIface) return null
    const counts = new Map<string, number>()
    for (const e of appState.events) {
      const other = e.inInterface === tunnelIface ? e.outInterface : e.outInterface === tunnelIface ? e.inInterface : null
      if (!other) continue
      counts.set(other, (counts.get(other) ?? 0) + 1)
    }
    let best = -1
    let bestN = 0
    zones.forEach((z, i) => {
      const n = counts.get(z.id) ?? 0
      if (n > bestN) {
        best = i
        bestN = n
      }
    })
    return best === -1 ? null : best
  })

  // --- the frame, and moving inside it (#890 items 3 and 4) ----------------
  // Round 49 drew this scene on a fixed 1400x720 frame, so a tunnel
  // packed past its edge would simply have been off the map. Round 51's
  // rule -- carried into 52 -- is that the frame fits the map: the union
  // of that nominal frame and every drawn node's bounds plus 40 px of
  // air, and never closer than 1:1, so a map that fits opens exactly
  // where it always did and the frame only ever grows.
  //
  // What the frame is grown around is what can move: the tunnel cards
  // the packing rule places, and the lane row, whose width follows the
  // number of lanes. The Internet island and the waist stand exactly
  // where round 49 put them, and the nominal frame is the frame round 49
  // drew around them -- padding those two again would push the default
  // map a couple of pixels off round 49's own drawing for no reason.
  const mapNodeBoxes = $derived.by((): Box[] => {
    const boxes: Box[] = zones.map((_z, i) => ({ x: laneX(i, laneCount) - cardHalf, y: 466, w: cardW, h: 150 }))
    for (const t of drawnTunnels) boxes.push(cardBox(t.placed, t.aggregate !== null))
    return boxes
  })

  const mapFrame = $derived(topographyFrame(mapNodeBoxes))

  /** Where the operator has zoomed and panned to, or null for the
   * fitted frame -- the same viewBox by another route. */
  let mapView = $state<Box | null>(null)
  const mapBox = $derived(mapView ?? mapFrame)
  const mapViewBox = $derived(viewBoxOf(mapBox))
  /** The fit chip's percentage: this box's scale against the nominal
   * frame's, so a fitted map that needed no extra frame reads 100 %. */
  const mapZoom = $derived(zoomPercent(mapBox))

  let mapAnim: number | null = null

  function stopMapAnim() {
    if (mapAnim !== null) cancelAnimationFrame(mapAnim)
    mapAnim = null
  }

  /** Back to the fitted frame. Eased, unless the reader has asked for
   * reduced motion -- the city's own decision, read from the one place
   * that makes it (project.ts's reducedMotion). */
  function fitMap() {
    stopMapAnim()
    const from = mapView
    if (!from || reducedMotion() || typeof requestAnimationFrame !== 'function') {
      mapView = null
      return
    }
    const to = mapFrame
    const t0 = performance.now()
    const step = (now: number) => {
      const t = ease((now - t0) / 240)
      if (t >= 1) {
        mapView = null
        mapAnim = null
        return
      }
      mapView = {
        x: from.x + (to.x - from.x) * t,
        y: from.y + (to.y - from.y) * t,
        w: from.w + (to.w - from.w) * t,
        h: from.h + (to.h - from.h) * t,
      }
      mapAnim = requestAnimationFrame(step)
    }
    mapAnim = requestAnimationFrame(step)
  }

  $effect(() => () => stopMapAnim())

  /** Map units per client pixel, for `xMidYMid meet`. */
  function mapScale(): number {
    if (!mapSvgEl) return 1
    const r = mapSvgEl.getBoundingClientRect()
    if (!r.width || !r.height) return 1
    return Math.max(mapBox.w / r.width, mapBox.h / r.height)
  }

  // How far out the map may be taken by hand, either way, against the
  // frame it fits at: close enough to read a host dot, far enough to see
  // a cluster that has grown well past the frame.
  const MAP_ZOOM_IN = 0.25
  const MAP_ZOOM_OUT = 3

  function clampBox(b: Box): Box {
    const w = Math.min(mapFrame.w * MAP_ZOOM_OUT, Math.max(mapFrame.w * MAP_ZOOM_IN, b.w))
    const h = b.h * (w / b.w)
    return { x: b.x + (b.w - w) / 2, y: b.y + (b.h - h) / 2, w, h }
  }

  let mapDrag: { x: number; y: number; box: Box; moved: boolean } | null = null
  /** A drag ends with a click on whatever is under the pointer; that
   * click must not also open the thing the pan happened to finish over. */
  let mapDragged = false

  function onMapPointerDown(e: PointerEvent) {
    if (e.button !== 0) return
    mapDragged = false
    stopMapAnim()
    mapDrag = { x: e.clientX, y: e.clientY, box: { ...mapBox }, moved: false }
  }

  function onMapPointerMove(e: PointerEvent) {
    if (!mapDrag) return
    const k = mapScale()
    const dx = (e.clientX - mapDrag.x) * k
    const dy = (e.clientY - mapDrag.y) * k
    if (!mapDrag.moved) {
      if (Math.abs(dx) + Math.abs(dy) < 3 * k) return
      mapDrag.moved = true
      // Capture only once a real drag is under way (#977's lesson from
      // City.svelte): capturing on pointerdown makes every plain click
      // land on the svg instead of the card under the pointer.
      ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
    }
    mapView = { ...mapDrag.box, x: mapDrag.box.x - dx, y: mapDrag.box.y - dy }
  }

  function onMapPointerUp() {
    mapDragged = mapDrag?.moved ?? false
    mapDrag = null
  }

  function onMapClickCapture(e: MouseEvent) {
    if (!mapDragged) return
    mapDragged = false
    e.stopPropagation()
    e.preventDefault()
  }

  /** Wheel zoom about the pointer, so the thing under the cursor stays
   * under it. Attached by hand rather than as an attribute: Svelte adds
   * `passive` to a wheel listener, and a passive listener cannot stop
   * the page scrolling behind the map. */
  function onMapWheel(e: WheelEvent) {
    if (!mapSvgEl) return
    e.preventDefault()
    stopMapAnim()
    const r = mapSvgEl.getBoundingClientRect()
    if (!r.width || !r.height) return
    const k = Math.max(mapBox.w / r.width, mapBox.h / r.height)
    // `xMidYMid meet` letterboxes, so the drawn map is centred in the
    // element: take the offset off before converting to map units.
    const ox = (r.width - mapBox.w / k) / 2
    const oy = (r.height - mapBox.h / k) / 2
    const px = mapBox.x + (e.clientX - r.left - ox) * k
    const py = mapBox.y + (e.clientY - r.top - oy) * k
    const f = Math.exp(e.deltaY * 0.0015)
    const next = clampBox({ x: mapBox.x, y: mapBox.y, w: mapBox.w * f, h: mapBox.h * f })
    // Keep (px, py) where it was: shift by how much the box grew about it.
    const s = next.w / mapBox.w
    mapView = { ...next, x: px - (px - mapBox.x) * s, y: py - (py - mapBox.y) * s }
  }

  $effect(() => {
    const el = mapSvgEl
    if (!el) return
    el.addEventListener('wheel', onMapWheel, { passive: false })
    return () => el.removeEventListener('wheel', onMapWheel)
  })

  /** Arrow keys pan, a fifth of the frame at a time. Only while the map
   * itself is what the keyboard is on: anything focused that wants its
   * own arrows keeps them. */
  function panMap(dx: number, dy: number) {
    stopMapAnim()
    mapView = { ...mapBox, x: mapBox.x + dx, y: mapBox.y + dy }
  }

  function mapTakesArrows(target: EventTarget | null): boolean {
    if (reach || cityStop !== null || nodeCard || lineCard || compose) return false
    if (!(target instanceof Element)) return true
    if (target === document.body) return true
    return target === mapSvgEl || target.closest('.stage') !== null
  }

  // --- the two filters (#1018, round 53) -----------------------------------
  //
  // Neither adds anything to the map. Both dim it to their own answer:
  // what is on the port, or what one line did. Nothing is removed --
  // every rib, card and dot stays where it was, greyed, so the answer is
  // read against the whole network rather than against a cropped one.
  //
  // The two are mutually exclusive by construction: opening one clears
  // the other. Two filters at once would leave the operator unable to
  // say which of them a dim rib was dim because of.

  // Settled, not merely selected: between a chip's click and its answer
  // there is nothing to draw, and dimming on `active` alone would put
  // "0 of 12 hosts on 22/tcp" and a wholly grey map on screen for the
  // length of every request -- an answer to a question still in flight.
  /** The fit chip's own width, measured rather than assumed: the label
   * is `NN%` and grows with the zoom, so a hardcoded gap beside it
   * would be right at 100 % and wrong at 1000 %. The legend sits left
   * of it by this plus FIT_CHIP_CLEAR.
   *
   * Read from the element rather than through `bind:clientWidth`, which
   * makes Svelte install a ResizeObserver on it -- one this scene does
   * not otherwise need, and which jsdom does not have. The two things
   * that change the chip's width are the zoom label and the chip coming
   * and going, and both are already state this effect reads. */
  let fitChipEl: HTMLButtonElement | undefined = $state()
  let fitChipW = $state(0)
  $effect(() => {
    mapZoom
    filterOn
    const el = fitChipEl
    fitChipW = el ? el.getBoundingClientRect().width : 0
  })
  const FIT_CHIP_RIGHT = 16
  const FIT_CHIP_CLEAR = 16
  /** Where the filter legend's right edge sits: clear of the fit chip,
   * which keeps its own place. */
  const legendRight = $derived(reach ? 24 : FIT_CHIP_RIGHT + fitChipW + FIT_CHIP_CLEAR)

  const portOn = $derived(portFilterState.active && portFilterState.settled)
  const traceOn = $derived(mapTraceState.active)
  const filterOn = $derived(portOn || traceOn)

  /**
   * Every direction the trace lights: the half it came in on and the
   * half it left on, in the verdict's own ink.
   *
   * An accepted line crossed, so both halves of its pair light -- that
   * is one packet through the router, not a claim about traffic coming
   * back. A refused line with no out-interface named (an input-chain
   * drop) never reached one, so only the half it arrived on lights, and
   * it dies at the waist where the ✕ is. A refused line the router
   * *did* name an out-interface for (a forward-chain drop, C1, round
   * 56) got as far as the far boundary before being stopped there, so
   * it lights the same way an accept does -- both halves, red.
   */
  const traceLit = $derived.by((): Map<string, 'accept' | 'refused'> => {
    const out = new Map<string, 'accept' | 'refused'>()
    const e = mapTraceState.event
    if (!e || !mapTraceState.verdict) return out
    const verdict = mapTraceState.verdict === 'accepted' ? 'accept' : 'refused'
    const inIface = e.inInterface ?? ''
    const outIface = e.outInterface ?? ''
    if (inIface === '' && outIface === '') return out
    out.set(ribKey(inIface, outIface), verdict)
    // Both directions carry the same verdict whenever the line reached
    // an out-interface at all: an accept crossed it, and a forward-chain
    // refusal (C1) reached the far boundary before being stopped there.
    // An input-chain refusal names no out-interface and stays one-sided.
    if (inIface !== '' && outIface !== '') out.set(ribKey(outIface, inIface), verdict)
    return out
  })

  /** What the filter in force lights, keyed the same way reality.ts
   * keys a direction, so a lit direction and a drawn one are one string
   * rather than two conventions that agree until they do not. */
  const lit = $derived(portOn ? litRibs(portFilterState.ribs) : traceOn ? traceLit : new Map())

  interface LitHalf {
    key: string
    d: string
    verdict: 'accept' | 'refused'
    width: number
    /** Where the line stopped, for the refusal's ✕, or null. */
    stop: Pt | null
    /** Where it arrived, for an accepted trace's ring, or null. */
    ring: Pt | null
  }

  /** C1 (round 56): where a forward-chain refusal's ✕ stands when the
   * log names an out-interface -- the far end of the destination's own
   * half, exactly where an accepted trace's ring would sit, because the
   * line got through the router and was stopped at the far boundary
   * rather than at the router itself. Null for an input-chain refusal
   * (no out-interface at all), which still dies at the waist. */
  const refusedGateStop = $derived.by((): Pt | null => {
    if (!traceOn || mapTraceState.verdict !== 'refused') return null
    const e = mapTraceState.event
    if (!e?.inInterface || !e?.outInterface) return null
    const line = lineFor(e.inInterface, e.outInterface, true)
    if (!line) return null
    return ringPoint(line)
  })

  /**
   * The lit layer. Drawn over the dimmed map rather than instead of it,
   * so a rib that is both drawn and lit keeps its own geometry: one
   * curve, two treatments, never two curves that could disagree.
   *
   * A direction with no drawable line -- a boundary the map has no
   * island for -- is dropped rather than drawn somewhere plausible,
   * the same refusal lineFor already makes for the unfiltered map.
   */
  const litHalves = $derived.by((): LitHalf[] => {
    const out: LitHalf[] = []
    for (const [key, verdict] of lit) {
      const [from, to] = key.split('|')
      // A refusal that reached the far boundary (refusedGateStop) draws
      // the same full curve an accept does; one that died at the
      // router does not.
      const crosses = verdict === 'accept' || (traceOn && verdict === 'refused' && !!refusedGateStop)
      const line = lineFor(from, to, crosses)
      if (!line) continue
      out.push({
        key,
        d: halfPath(line),
        verdict,
        // Full width for the answer, whatever the volume: the filter is
        // not a traffic reading, it is "this is the one you asked about".
        width: verdict === 'accept' ? 2.6 : 2.4,
        stop: verdict === 'refused' ? (refusedGateStop ?? deathPoint(line)) : null,
        ring: verdict === 'accept' && traceOn ? ringPoint(line) : null,
      })
    }
    return out
  })

  /** Where a refused trace stopped: the far gate (C1) or the router's
   * own edge. */
  const traceStop = $derived(traceOn ? (litHalves.find((h) => h.stop)?.stop ?? null) : null)
  const traceRing = $derived(traceOn ? (litHalves.find((h) => h.ring)?.ring ?? null) : null)

  /** A point on a cubic at t -- de Casteljau again, the one place a
   * door's posts are placed from. */
  function bezAt(c: [Pt, Pt, Pt, Pt], t: number): Pt {
    const u = 1 - t
    return {
      x: u * u * u * c[0].x + 3 * u * u * t * c[1].x + 3 * u * t * t * c[2].x + t * t * t * c[3].x,
      y: u * u * u * c[0].y + 3 * u * u * t * c[1].y + 3 * u * t * t * c[2].y + t * t * t * c[3].y,
    }
  }

  interface DrawnDoor {
    key: string
    label: string
    action: string
    who: string
    accepts: boolean
    /** The two posts, the leaf or bar between them, and the label. */
    posts: string
    leaf: string
    lx: number
    ly: number
    anchor: 'start' | 'end'
    /** The direction the door sits on, so an unused rib can be told
     * apart from an off-filter one and dimmed less (round 53). */
    half: string
    /** The rib itself, where nothing else on the map draws it. */
    guide: string | null
  }

  const DOOR_W = 8

  /** How finely a rib is sampled when the door chooser measures
   * clearance against it. Twelve points is enough to catch a crossing
   * anywhere along a half without turning the chooser into a curve
   * intersection problem -- the answer only has to rank candidates. */
  const RIB_SAMPLES = 12

  /** The curve a line is actually drawn as, sampled. A direction that
   * dies at the waist draws the quad `edgePath` draws, not a half of a
   * cubic, so the samples follow whichever is on screen -- measuring
   * clearance against a curve nobody can see would move a door away
   * from nothing. */
  function drawnPoints(l: Line): Pt[] {
    const out: Pt[] = []
    const c = halfCubic(l)
    if (c) {
      for (let i = 0; i <= RIB_SAMPLES; i++) out.push(bezAt(c, i / RIB_SAMPLES))
      return out
    }
    const [a, b, cc] = quadOf(l)
    for (let i = 0; i <= RIB_SAMPLES; i++) {
      const t = i / RIB_SAMPLES
      const u = 1 - t
      out.push({ x: u * u * a.x + 2 * u * t * b.x + t * t * cc.x, y: u * u * a.y + 2 * u * t * b.y + t * t * cc.y })
    }
    return out
  }

  /** Every drawn half, sampled and keyed, for the door chooser. */
  const ribSamples = $derived.by((): { key: string; pts: Pt[] }[] => {
    if (!portOn) return []
    const out: { key: string; pts: Pt[] }[] = []
    for (const d of drawnCoverage.drawn) out.push({ key: d.edge.key, pts: drawnPoints(d.line) })
    for (const d of drawnReality.drawn) out.push({ key: d.r.key, pts: drawnPoints(d.line) })
    return out
  })

  /** Every direction the unfiltered map already draws a line for. A
   * door whose own direction is not in here has no rib under it -- the
   * pair was never observed and no coverage half covers it -- so the
   * door draws its own faint guide, in the dim grey rather than in any
   * verdict ink: it is where a rule sits, not where anything went. */
  const drawnHalfKeys = $derived(
    new Set([...drawnReality.drawn.map((d) => d.r.key), ...drawnCoverage.drawn.map((d) => d.edge.key)]),
  )

  /**
   * The doors: every pushed rule that names the selected port, drawn as
   * two posts across the rib where the rule sits -- the leaf swung open
   * for accept, a bar across for anything that refuses.
   *
   * Policy, never traffic. A door on a rib nobody used still draws, and
   * that rib dims less than the rest, because knowing where a door is
   * open even when unused is the point of interrogating a port (owner,
   * 2026-09-08).
   */
  const drawnDoors = $derived.by((): DrawnDoor[] => {
    if (!portOn) return []
    const out: DrawnDoor[] = []
    // Filled as each door lands, so the next one is measured against it
    // too and two doors on converging ribs do not stack.
    const placedDoorPts: Pt[] = []
    for (const door of portFilterState.placedDoors) {
      const half = doorHalf(door)
      if (!half) continue
      const line = lineFor(half.from, half.to, true)
      if (!line) continue
      const c = halfCubic(line)
      if (!c) continue
      const key = ribKey(half.from, half.to)
      // Where on its own rib: the sampled point with the most room
      // around it, measured against every *other* drawn rib and every
      // door already placed. A fixed fraction put every door in the same
      // place, and on this map every rib converges on the router, so
      // that place is the pile-up.
      const ts = doorTs()
      const others = ribSamples.filter((r) => r.key !== key).map((r) => r.pts)
      if (placedDoorPts.length > 0) others.push(placedDoorPts)
      const spot = chooseDoorSpot(
        ts.map((t) => bezAt(c, t)),
        others,
      )
      const t = ts[spot.index]
      const p = bezAt(c, t)
      placedDoorPts.push(p)
      const q = bezAt(c, Math.min(1, t + 0.02))
      const len = Math.hypot(q.x - p.x, q.y - p.y) || 1
      const tx = (q.x - p.x) / len
      const ty = (q.y - p.y) / len
      const nx = -ty
      const ny = tx
      const a = { x: p.x + nx * DOOR_W, y: p.y + ny * DOOR_W }
      const b = { x: p.x - nx * DOOR_W, y: p.y - ny * DOOR_W }
      const accepts = doorAccepts(door)
      // The label goes on the side away from whatever is nearest -- the
      // one part of a door that has a side to choose, and choosing the
      // crowded one throws away the clearance the point was picked for.
      // With nothing near it, away from the router, which is where the
      // cards are not.
      const side = spot.toward
        ? (spot.toward.x - p.x) * nx + (spot.toward.y - p.y) * ny > 0
          ? -1
          : 1
        : p.x < WAIST.x
          ? -1
          : 1
      out.push({
        key: `${door.device}#${door.ordinal}`,
        label: door.label,
        action: door.action,
        who: door.who,
        accepts,
        posts:
          `M ${R2(a.x - tx * 4)} ${R2(a.y - ty * 4)} L ${R2(a.x + tx * 4)} ${R2(a.y + ty * 4)}` +
          ` M ${R2(b.x - tx * 4)} ${R2(b.y - ty * 4)} L ${R2(b.x + tx * 4)} ${R2(b.y + ty * 4)}`,
        leaf: accepts
          ? `M ${R2(a.x)} ${R2(a.y)} L ${R2(a.x - nx * 7 + tx * 9)} ${R2(a.y - ny * 7 + ty * 9)}`
          : `M ${R2(a.x)} ${R2(a.y)} L ${R2(b.x)} ${R2(b.y)}`,
        lx: R2(p.x + nx * (DOOR_W + 8) * side),
        ly: R2(p.y + ny * (DOOR_W + 8) * side + 3),
        anchor: nx * side < 0 ? 'end' : 'start',
        half: key,
        guide: drawnHalfKeys.has(key) ? null : halfPath(line),
      })
    }
    return out
  })

  /** The directions carrying a door -- dimmed less than the rest. */
  const doorHalves = $derived(new Set(drawnDoors.map((d) => d.half)))

  /** How dim an off-filter direction is drawn. Never removed: the
   * answer is read against the whole network. */
  function dimFor(key: string): number {
    if (!filterOn) return 1
    if (lit.has(key)) return 1
    return doorHalves.has(key) ? 0.6 : 0.4
  }

  /** The addresses the filter lights: the hosts on the port, or the two
   * ends of the traced line. */
  const litHosts = $derived.by((): Set<string> => {
    if (portOn) return portFilterState.hostIps
    const e = mapTraceState.event
    if (!e) return new Set<string>()
    return new Set([e.srcIp, e.dstIp].filter((ip): ip is string => !!ip))
  })

  /** The lane card's line under a filter: `2 of 12 hosts on 445/tcp`
   * for the port, and the traced end's own tally for the trace
   * (`cam-porch · 14× today`, `tom-desktop · never reached`).
   *
   * The port's count runs over this row's own dots -- the hosts the
   * surface knows about -- and never over the answer's host list
   * directly, so an address seen on the port that no lane draws is not
   * counted anywhere (#1056). Null falls back to the presence count
   * below, which is what a lane with no known host says instead of a
   * fraction with nothing on either side of it. */
  function filterTally(row: HostRow): string | null {
    if (portOn) {
      const on = row.dots.filter((d) => litHosts.has(d.ip)).length + hiddenLitHosts(row)
      return zoneTally(on, row.total, portFilterState.label)
    }
    const e = mapTraceState.event
    if (!e) return null
    if (e.srcIp && row.dots.some((d) => d.ip === e.srcIp)) {
      return `${e.srcHostName || e.srcIp} · ${mapTraceState.srcSeen}× in the window`
    }
    if (e.dstIp && row.dots.some((d) => d.ip === e.dstIp)) {
      const name = e.dstHostName || e.dstIp
      return mapTraceState.dstReached > 0
        ? `${name} · reached ${mapTraceState.dstReached}×`
        : `${name} · never reached`
    }
    return null
  }

  /** Hosts on the port that this lane has but the card does not draw --
   * the `+N` overflow. Counted so the tally is about the lane, not
   * about the ten dots that happened to fit. */
  function hiddenLitHosts(row: HostRow): number {
    return row.hidden.filter((d) => litHosts.has(d.ip)).length
  }

  /** Whether a lane card has anything the filter is about. A card with
   * nothing on the port dims whole -- it is still there, it just has no
   * part in the answer. */
  function laneInFilter(z: ZoneInfo, row: HostRow): boolean {
    if (!filterOn) return true
    if (row.dots.some((d) => litHosts.has(d.ip)) || hiddenLitHosts(row) > 0) return true
    for (const key of lit.keys()) {
      const [from, to] = key.split('|')
      if (from === z.id || to === z.id) return true
    }
    return [...doorHalves].some((key) => key.split('|').includes(z.id))
  }

  /** The one line under the map when the window carried nothing on the
   * port -- a sentence, not an empty state. The door is still drawn. */
  const nothingSeenNote = $derived(
    portFilterState.nothingSeen ? emptyNote(portFilterState.label, portFilterState.doors) : null,
  )

  /** Where the chip sits: beside the router, round 53/54's own fixed
   * spot -- or beside the ✕ when C1 moves it to the far gate, since the
   * chip always sits by the ✕ it explains. */
  const traceChipBox = $derived.by((): Pt => {
    const atRouter = { x: 308, y: 234 }
    if (!refusedGateStop) return atRouter
    const w = 248
    return {
      x: Math.min(Math.max(refusedGateStop.x - w / 2, 12), 1400 - w - 12),
      y: Math.max(refusedGateStop.y - 96, 20),
    }
  })

  /** The trace's chip beside the router: the router's own decision. */
  const traceChip = $derived.by((): { verdict: string; rule: string; path: string } | null => {
    const e = mapTraceState.event
    if (!mapTraceState.request) return null
    if (!e) return null
    const rule = e.ruleName || e.ruleLabel || 'no rule named'
    const verdict =
      mapTraceState.verdict === 'accepted'
        ? `✓ ACCEPTED · ${rule}`
        : mapTraceState.verdict === 'refused'
          ? `✕ REFUSED · ${rule}`
          : `· ${rule}`
    const proto = (e.protocol ?? '').toLowerCase()
    const port = e.dstPort ? `${e.dstPort}/${proto || '?'}` : proto
    // NAT where the router said it translated, and nothing where it did
    // not: an absent translation is not a translation to nowhere.
    const nat = e.natIp ? `${e.chain ?? 'nat'} → ${e.natIp}${e.natPort ? `:${e.natPort}` : ''} · ` : ''
    const path = `in: ${e.inInterface || '—'} → out: ${e.outInterface || '—'} · ${nat}${port} · ${formatHM(e.time)}`
    return { verdict, rule, path }
  })

  /** The ghost: the rib the refused line would have taken, dashed, with
   * the note beside it. Drawn from the destination's own lane, and only
   * where the map has one -- an address in no drawn zone gets no ghost
   * rather than a guessed one. Only for a refusal that dies at the
   * router (no out-interface named): one that reached the far boundary
   * (C1, `refusedGateStop`) has nowhere further to draw toward, and
   * `traceStopNote` carries its own words instead. */
  const traceGhost = $derived.by((): { d: string; at: Pt; text: string } | null => {
    const e = mapTraceState.event
    if (!traceOn || mapTraceState.verdict !== 'refused' || !e?.dstIp || !e.inInterface) return null
    if (refusedGateStop) return null
    const zone = zones.find((z) => {
      if (z.hosts.some((h) => h.ip === e.dstIp)) return true
      if (!z.cidr) return false
      const cidr = parseCidr(z.cidr)
      return cidr ? addressInCidr(e.dstIp ?? '', cidr) : false
    })
    if (!zone || zone.id === e.inInterface) return null
    const line = lineFor(zone.id, e.inInterface, true)
    if (!line) return null
    // Over the card it names, in the slot the lane's service list uses
    // (and vacates under a filter). Anywhere on the ghost's own curve
    // puts a long note across the dashes it annotates, which is the one
    // place a label must not be; over the card, it reads as being about
    // that card, which it is.
    const i = zones.findIndex((z) => z.id === zone.id)
    const text = `would have reached ${e.dstHostName || e.dstIp} · never left the router`
    // Kept inside the nominal frame: the leftmost lane is close enough
    // to the edge that a note centred on it runs off it, and the frame
    // grows for nodes, not for labels.
    const halfW = text.length * 2.9
    return {
      d: halfPath(line),
      at: { x: Math.min(Math.max(laneX(i, laneCount), halfW + 12), 1400 - halfW - 12), y: 470 },
      text,
    }
  })

  /** C1's own two grey words, in the ghost's place, once the ✕ already
   * stands at the far gate: there is nowhere left to draw a dashed rib
   * toward, so the note alone says what would have happened. Anchored
   * the same way the ghost's own note was -- over the destination's
   * lane, in the slot its service list vacates under a filter. */
  const traceStopNote = $derived.by((): { at: Pt; lines: string[] } | null => {
    const e = mapTraceState.event
    if (!refusedGateStop || !e?.dstIp) return null
    const zone = zones.find((z) => {
      if (z.hosts.some((h) => h.ip === e.dstIp)) return true
      if (!z.cidr) return false
      const cidr = parseCidr(z.cidr)
      return cidr ? addressInCidr(e.dstIp ?? '', cidr) : false
    })
    const lines = [`would have reached ${e.dstHostName || e.dstIp}`, zone ? `stopped at the ${zone.name} boundary` : 'stopped at the boundary']
    const halfW = Math.max(...lines.map((l) => l.length)) * 2.9
    const i = zone ? zones.findIndex((z) => z.id === zone.id) : -1
    const x = i >= 0 ? Math.min(Math.max(laneX(i, laneCount), halfW + 12), 1400 - halfW - 12) : refusedGateStop.x
    return { at: { x, y: 470 }, lines }
  })

  /** Opening one filter closes the other, and both close the reach:
   * three answers layered on one map would leave nobody able to say
   * which of them a dim rib was dim because of. */
  async function openPortPicker() {
    mapTraceState.clear()
    await portFilterState.openPicker()
  }

  /** What the callout asks for: the pair it names, exactly. A pair the
   * router logged with no out-interface says so (`noOut`) rather than
   * leaving it unset -- unset means "any", and the trace would then be
   * free to land on a newer line that *did* leave the router, which is a
   * different thing from the one the card is about. */
  function traceAsk(r: RealityEdge, asked: { port: number; proto: string } | undefined) {
    return {
      in: r.from,
      out: r.to === '' ? undefined : r.to,
      noOut: r.to === '' ? true : undefined,
      port: asked?.port,
      proto: asked?.proto,
    }
  }

  function openTrace(req: Parameters<typeof mapTraceState.open>[0], opts?: Parameters<typeof mapTraceState.open>[1]) {
    portFilterState.clear()
    if (reach) surface()
    void mapTraceState.open(req, opts)
  }

  /** The picker's text field: enter applies the list, and a list that
   * cannot be read whole is refused rather than half-applied. */
  async function applyTypedPorts() {
    const parsed = parsePortList(portFilterState.typed)
    if (parsed === null) {
      portFilterState.typedBad = true
      return
    }
    portFilterState.typedBad = false
    await portFilterState.setPorts(parsed)
  }

  /** #1018's cross-page handoff: a stream row names an event and flips
   * to this view. Read and cleared on arrival, whether that is this
   * component's own mount or the instant the slot changes under an
   * already-mounted tab -- the same one-shot idiom pendingDescend uses. */
  $effect(() => {
    const pending = topologyNavState.pendingTrace
    if (!pending) return
    topologyNavState.pendingTrace = null
    openTrace(pending)
  })
</script>

<svelte:window onkeydown={onKeydown} onclick={onWindowClick} />

<div class="topo" bind:this={topoEl}>
  <!-- The aggregate bar (#648, round 23): absent, purple-only, red-only,
       or split with a centre divider -- reused under every zone island
       and every reach counterpart cluster below. -->
  {#snippet aggregateBar(agg: ZoneAggregate, x0: number, width: number, y: number, h: number, z: { id: string; name: string })}
    {@const x1 = x0 + width}
    {@const mid = x0 + width / 2}
    {@const both = agg.flagCount > 0 && agg.watchCount > 0}
    {#if agg.watchCount > 0}
      <g
        class="hbar-g"
        role="button"
        tabindex="0"
        aria-label="{agg.watchCount} watcher{agg.watchCount === 1 ? '' : 's'}{agg.watchBroken > 0 ? `, ${agg.watchBroken} broken` : ''} — open the watchlist"
        onclick={openWatchlist}
        onkeydown={(e) => {
          if (e.key === 'Enter') openWatchlist(e)
        }}
      >
        <path class="hb hb-w" d={both ? leftBarPath(x0, mid, h) : fullBarPath(x0, x1, h)} transform="translate(0 {y})" />
        <text class="hbt wp" x={both ? x0 + width / 4 : mid} y={y + h / 2 + 3.5} text-anchor="middle">
          ◉ {agg.watchCount}{agg.watchBroken > 0 ? ' ○' : ''}
        </text>
      </g>
    {/if}
    {#if agg.flagCount > 0}
      <g
        class="hbar-g"
        role="button"
        tabindex="0"
        aria-label="{agg.flagCount} open flag{agg.flagCount === 1 ? '' : 's'} — open flags filtered to {z.name}"
        onclick={(e) => openZoneFlags(e, z)}
        onkeydown={(e) => {
          if (e.key === 'Enter') openZoneFlags(e, z)
        }}
      >
        <path class="hb hb-f" d={both ? rightBarPath(mid, x1, h) : fullBarPath(x0, x1, h)} transform="translate(0 {y})" />
        <text class="hbt fchip" x={both ? x1 - width / 4 : mid} y={y + h / 2 + 3.5} text-anchor="middle">✱ {agg.flagCount}</text>
      </g>
    {/if}
    {#if both}
      <line class="hb-div" x1={mid} y1={y} x2={mid} y2={y + h} />
    {/if}
  {/snippet}

  <!-- The breadcrumb exists only descended: surfaced, the scene bar
       already names the place, and a placeholder crumb was mockup
       residue (owner, 2026-08-30). -->
  {#if reach}
    <!-- The crumb reads the city's own wording, on both surfaces, so the
         two views read as one product (round 49; DESIGN.md "The reach":
         `name · ip · reaches N · reached by N · refused N · Esc
         surfaces ▸`). The old `Network ▸ Zone ▸ Host` path is gone: it
         named the trail rather than the thing stood on, and both ends of
         it did the same thing this one `Esc surfaces ▸` does. -->
    <div class="crumb">
      <div class="path">
        <span class="here">{reach.host}</span>
        <!-- A host's address, a zone's pushed subnet, and nothing at all
             for a rib: it has neither, and an invented one would be the
             app stating a fact it does not have (#1016). -->
        {#if reach.ip}<span class="ip">{reach.ip}</span>{/if}
        {#if reachSummary}
          <i class="bar"></i>
          <span>reaches <b>{reachSummary.reaches}</b></span>
          <span>reached by <b>{reachSummary.reachedBy}</b></span>
          <span class:alarm={reachRefused > 0}>refused <b>{reachRefused}</b></span>
        {/if}
        <i class="bar"></i>
        <button class="crumb-link esc" onclick={surface}>Esc surfaces ▸</button>
      </div>
      {#if reachSummary}
        <!-- Round 30 states this on the reach's own zone card
             (the-whole.html:1136); this build's reach is the membrane
             view, whose analogue of that facts line is this crumb sub,
             which already carries the reach's derived facts.
             The mockup words it "right now", and the ranking is a
             decayed sum over the whole buffer rather than an
             instantaneous reading -- a pathway that stopped ten minutes
             ago can still win it. So the owner's ruling requires the
             sentence to say it is weighted toward now, and it does,
             in their words (#701; wording by Fable 5).
             No empty state of its own: `busiest` is null exactly when
             no strand was observed, which the reach block already
             answers with "nothing observed this window". -->
        {#if reachSummary.busiest}
          {@const bp = reachSummary.busiest}
          {@const peer = bp.peers[0] ?? (bp.counterpart === 'internet' ? 'the internet' : bp.counterpart)}
          {@const hit = bp.portHits[0]}
          <div class="sub">
            the busiest pathway, weighted toward now:
            {bp.direction === 'out' ? `${reachSideName} → ${peer}` : `${peer} → ${reachSideName}`}{hit ? ` · ${hit.proto}/${hit.port}` : ''}
            · {#if bp.outcome === 'blocked'}<b class="alarm">refused</b>{:else}accepted{/if}
          </div>
        {/if}
      {/if}
    </div>
  {/if}
  <!-- The traced line's own crumb, plus its list (#1018 round 53, #1050
       round 56 A1) -- lifted into its own component so the same markup
       mounts on the city too, which draws its own copy of this same
       component rather than sharing this one: hidden here exactly when
       `.stage` is, so the two never both show at once (an earlier build
       of this left it unconditional, which put two crumbs on screen at
       the city stop, both wired to the same store). -->
  {#if cityStop === null}
    <TraceCrumb />
  {/if}
  <!-- Both tools swap the legend for their own entries (round 53's
       `chrome`). The map carries no legend of its own -- round 49
       decided the material is the statement -- so this appears with a
       filter and goes with it, which is also the rule #981 set for the
       flag and watch marks: something always there is easy to ignore. -->
  {#if filterOn || ghostLanes.length > 0}
    <div
      class="map-legend"
      style:right="{legendRight}px"
      aria-label="What this filter's colours mean"
    >
      {#if filterOn}
        <span><i class="sw ok"></i>accepted</span>
        <span><i class="sw al"></i>refused</span>
      {/if}
      {#if portOn}
        <span>
          <svg width="14" height="11" aria-hidden="true"
            ><path d="M2 1V10M12 1V10" stroke="var(--accept)" stroke-width="1.5" stroke-linecap="round" /><path
              d="M2 1L8 5.5"
              stroke="var(--accept)"
              stroke-width="1.5"
              stroke-linecap="round"
            /></svg
          >door open</span
        >
        <span>
          <svg width="14" height="11" aria-hidden="true"
            ><path d="M2 1V10M12 1V10" stroke="var(--alarm)" stroke-width="1.5" stroke-linecap="round" /><path
              d="M2 5.5H12"
              stroke="var(--alarm)"
              stroke-width="1.5"
              stroke-linecap="round"
            /></svg
          >door shut</span
        >
      {:else if traceGhost}
        <span><i class="sw ghost"></i>would have gone</span>
      {/if}
      {#if filterOn}
        <span><i class="sw off"></i>off the {portOn ? 'port' : 'line'}</span>
      {/if}
      <!-- The ghost's own entry (#460, round 55). It appears with a
           ghost and goes with it, the rule #981 set for every mark on
           this map: something always there is easy to ignore, and a
           legend line for a state nothing is in explains nothing. -->
      {#each [...new Set(ghostLanes.map((g) => g.state))] as gs (gs)}
        <span
          ><i class="sw" style:background="repeating-linear-gradient(90deg, {GHOST_INK[gs]} 0 3px, transparent 3px 6px)"></i>ghost · {gs === 'none'
            ? 'offered, no watch yet'
            : gs === 'holding'
              ? 'watch holding'
              : 'watch broken — a straggler'}</span
        >
      {/each}
    </div>
  {/if}
  <!-- The health dials (#648, rounds 19-20; repositioned #682 clear of
       the lens tabs and the deck's roll rail; #724 makes each a two-step
       control): two rings, flags and watchers, solid green whenever
       there is nothing to report. Each ring's own symbol (⚑ / the eye)
       sits beneath its count as the ring's legend. First click expands
       the condensed panel beneath the dial it belongs to; a second
       dial click collapses it -- navigation now belongs to the panel's
       own rows (see the script's "dials' condensed panel" section). -->
  <div class="dials">
      <div class="dial-wrap" bind:this={flagsWrapEl}>
        <button
          class="dial"
          bind:this={flagsDialEl}
          aria-expanded={expandedDial === 'flags'}
          aria-controls="{uid}-flags-panel"
          onclick={() => toggleDialPanel('flags')}
          aria-label="{activeFlags.length} open flag{activeFlags.length === 1 ? '' : 's'} — open the summary"
        >
          <svg viewBox="0 0 56 56" aria-hidden="true">
            {#if activeFlags.length === 0}
              <circle class="dring d-rest" cx="28" cy="28" r={DIAL_R} transform="rotate(-90 28 28)" />
            {:else}
              {@const alarmArc = ringArc(alarmFlagCount, activeFlags.length, 0)}
              {@const advisoryArc = ringArc(advisoryFlagCount, activeFlags.length, (alarmFlagCount / activeFlags.length) * DIAL_CIRC)}
              <circle
                class="dring d-alarm"
                cx="28"
                cy="28"
                r={DIAL_R}
                transform="rotate(-90 28 28)"
                stroke-dasharray={alarmArc.dasharray}
                stroke-dashoffset={alarmArc.offset}
              />
              <circle
                class="dring"
                cx="28"
                cy="28"
                r={DIAL_R}
                stroke={ADVISORY_INK}
                transform="rotate(-90 28 28)"
                stroke-dasharray={advisoryArc.dasharray}
                stroke-dashoffset={advisoryArc.offset}
              />
            {/if}
            <text x="28" y="27" class="dnum" text-anchor="middle">{activeFlags.length}</text>
            <text x="28" y="41" class="dsym flag-sym" text-anchor="middle">⚑</text>
          </svg>
        </button>
        {#if expandedDial === 'flags'}
          <!-- No role on this div at all (Care, #724): an explicit
               role="group" would satisfy "a group named for what it
               holds", but any container role here also hides the row
               buttons inside it from a screen reader. aria-label alone
               keeps the div in the accessibility tree with that name,
               without adding a role. -->
          <div
            id="{uid}-flags-panel"
            class="dial-panel"
            aria-label={flagPanelGroupLabel}
          >
            {#if flagPanelRows.length === 0}
              <p class="dp-zero" tabindex="-1" bind:this={panelZeroEl}>{flagsZeroLine}</p>
            {:else}
              {#each flagPanelRows as row, i (row.key)}
                <button
                  type="button"
                  class="dp-row"
                  bind:this={panelRowEls[i]}
                  onclick={() => activateFlagRow(row.key)}
                  aria-label={row.ariaLabel}
                >
                  <span class="dp-mark" style="color: {row.ink}" aria-hidden="true">{row.mark}</span>
                  <span class="dp-label">{row.label}</span>
                  <span class="dp-time">{row.time}</span>
                  <span class="dp-count">×{row.count}</span>
                </button>
              {/each}
              {#if flagPanelMoreCount > 0}
                <button
                  type="button"
                  class="dp-row dp-more"
                  bind:this={panelRowEls[flagPanelRows.length]}
                  onclick={activateFlagMore}
                  aria-label="and {flagPanelMoreCount} more. Opens the flags tab."
                >
                  and {flagPanelMoreCount} more
                </button>
              {/if}
            {/if}
          </div>
        {/if}
      </div>
      <div class="dial-wrap" bind:this={watchWrapEl}>
        <button
          class="dial"
          bind:this={watchDialEl}
          aria-expanded={expandedDial === 'watchlist'}
          aria-controls="{uid}-watch-panel"
          onclick={() => toggleDialPanel('watchlist')}
          aria-label="{watcherTotal} watcher{watcherTotal === 1 ? '' : 's'}{watcherBroken > 0 ? `, ${watcherBroken} broken` : ''} — open the summary"
        >
          <svg viewBox="0 0 56 56" aria-hidden="true">
            {#if watcherTotal === 0}
              <circle class="dring d-rest" cx="28" cy="28" r={DIAL_R} transform="rotate(-90 28 28)" />
            {:else}
              {@const healthyArc = ringArc(watcherHealthy, watcherTotal, 0)}
              {@const brokenArc = ringArc(watcherBroken, watcherTotal, (watcherHealthy / watcherTotal) * DIAL_CIRC)}
              <circle
                class="dring d-healthy"
                cx="28"
                cy="28"
                r={DIAL_R}
                transform="rotate(-90 28 28)"
                stroke-dasharray={healthyArc.dasharray}
                stroke-dashoffset={healthyArc.offset}
              />
              <circle
                class="dring d-broken"
                cx="28"
                cy="28"
                r={DIAL_R}
                transform="rotate(-90 28 28)"
                stroke-dasharray={brokenArc.dasharray}
                stroke-dashoffset={brokenArc.offset}
              />
            {/if}
            <text x="28" y="27" class="dnum" text-anchor="middle">{watcherTotal}</text>
            <!-- The docket's own eye (#682, ported from the scene's dial
                 markup) -- not the "◉" text glyph, which is the aggregate
                 bar's own mark, not the ring's legend. -->
            <g class="dsym watch-sym" transform="translate(28 36)">
              <path d="M-6 0 Q0 -4.6 6 0 Q0 4.6 -6 0 Z" fill="none" stroke="currentColor" stroke-width="1.1" />
              <circle r="1.7" fill="currentColor" />
            </g>
          </svg>
        </button>
        {#if expandedDial === 'watchlist'}
          <!-- Same "no container role" reasoning as the flags panel
               above. -->
          <div
            id="{uid}-watch-panel"
            class="dial-panel"
            aria-label={watchPanelGroupLabel}
          >
            {#if watchPanelRows.length === 0}
              <p class="dp-zero" tabindex="-1" bind:this={panelZeroEl}>{watchesZeroLine}</p>
            {:else}
              {#each watchPanelRows as row, i (row.key)}
                <button
                  type="button"
                  class="dp-row"
                  bind:this={panelRowEls[i]}
                  onclick={() => activateWatchRow(row.key)}
                  aria-label={row.ariaLabel}
                >
                  <span class="dp-mark dp-watch-mark" class:broken={row.broken} aria-hidden="true">{row.glyph}</span>
                  <span class="dp-label">{row.name}</span>
                  <span class="dp-boundary">{row.boundary}</span>
                  <span class="dp-detail">{row.detail}</span>
                </button>
              {/each}
              {#if watchPanelMoreCount > 0}
                <button
                  type="button"
                  class="dp-row dp-more"
                  bind:this={panelRowEls[watchPanelRows.length]}
                  onclick={activateWatchMore}
                  aria-label="and {watchPanelMoreCount} more. Opens the watchlist tab."
                >
                  and {watchPanelMoreCount} more
                </button>
              {/if}
            {/if}
          </div>
        {/if}
      </div>
    </div>

  <!-- What is left of the lens row (round 49 reduced it to two pills;
       #981 took those too). There is nothing to choose between: traffic
       is the picture, coverage is the material it is drawn in, policy
       went in slice C, and the flag and watch marks are drawn by the
       data rather than switched on. The off-baseline tally stays -- it
       is a count, not a control. -->
  <div class="pills" role="group" aria-label="Map overlays">
    <!-- The port pill (#1018, round 53), bottom-left where round 49's
         two lens pills were before #981 retired them. Three shapes and
         no other surface: idle it is `⌕ port`; clicking opens it into a
         bar of the same shape -- chips for the ports seen or named in
         the window, a field for a typed list, then tcp / udp; and with
         something selected it collapses onto the answer. No Show
         button: the map filters as the selection changes (owner,
         2026-09-08, "it loads automatically").

         Drawn at every altitude (#1055, round 54): the city filters to
         the same answer from the same store, so hiding the control on
         that side would have been a tool the operator could only reach
         by moving the slider. -->
    {#if portFilterState.open}
      <div
        class="pill p edit"
        role="group"
        aria-label="Pick ports — click to select, several at once; or type a list and press enter"
      >
        <span aria-hidden="true">⌕</span>
        <span class="ports" role="group" aria-label="Ports seen or named in the window">
          {#each portFilterState.candidates as c (c.port)}
            <button
              class="chip"
              class:on={portFilterState.ports.includes(c.port)}
              aria-pressed={portFilterState.ports.includes(c.port)}
              title={c.count > 0
                ? `${c.count} line${c.count === 1 ? '' : 's'} on ${c.port}/${[...new Set(c.protos)].join(' and ')} in the window${c.named ? ' — and a pushed rule names it' : ''}`
                : `nothing logged on ${c.port} in the window — a pushed rule names it`}
              onclick={() => portFilterState.togglePort(c.port)}>{c.port}</button
            >
          {/each}
          {#if portFilterState.candidates.length === 0}
            <span class="chip-none">no ports seen or named yet</span>
          {/if}
        </span>
        <input
          class="typed"
          class:bad={portFilterState.typedBad}
          bind:value={portFilterState.typed}
          placeholder="22,23 ↵"
          aria-label="Or type ports, comma separated, and press enter"
          aria-invalid={portFilterState.typedBad}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              applyTypedPorts()
            }
          }}
        />
        <span class="bar"></span>
        <span class="seg" role="group" aria-label="Protocol">
          {#each ['tcp', 'udp'] as const as pr (pr)}
            <button
              class="chip"
              class:on={portFilterState.proto === pr}
              aria-pressed={portFilterState.proto === pr}
              onclick={() => portFilterState.setProto(portFilterState.proto === pr ? '' : pr)}>{pr}</button
            >
          {/each}
        </span>
      </div>
      {#if portOn}
        <button class="pill-x" aria-label="Clear the port filter" onclick={() => portFilterState.clear()}>✕</button>
      {/if}
    {:else if portOn}
      <button
        class="pill p on"
        aria-pressed="true"
        title="filtered to {portFilterState.label} — click to change, ✕ to clear"
        onclick={openPortPicker}>⌕ <b>{portFilterState.label}</b> <em>· {portFilterState.summary}</em></button
      >
      <button class="pill-x" aria-label="Clear the port filter" onclick={() => portFilterState.clear()}>✕</button>
    {:else}
      <button class="pill p" aria-pressed="false" aria-expanded="false" onclick={openPortPicker}>⌕ port</button>
    {/if}
    <!-- `⟡ off-baseline today · N`, ahead of the ⚑ count, in the accept
         ink (round-49/index.html's `chrome`, the `.nmk` mark, and
         DESIGN.md's own wording with the separator). Not a control: it
         is the sieve's own tally, and the places it counts are already
         lit on the map behind it.

         Drawn only above zero, the same rule the ⚑ count next to it
         follows. Before the register has been read the count is zero
         too, so a permanent `· 0` would be the map claiming an all-clear
         it has not been told. -->
    {#if offBaselineCount > 0}
      <span
        class="nmk"
        title="{offBaselineCount} line{offBaselineCount === 1 ? '' : 's'} off the baseline today — a route or port not seen on {baselineState.off.config.days} of the last {baselineState.off.config.of} days, accepted or not"
        >⟡ off-baseline <b>today</b> · {offBaselineCount}</span
      >
    {/if}
  </div>

  <!-- While descended, the map stays beneath as the reach's backdrop —
       blurred, at the level you left (round 24); clicking it surfaces
       exactly there. -->
  <!-- `map-degraded` is the drawing's `#s3.degraded` (the-whole.html:118-125),
       named apart from a bare `degraded` because that class already names
       something else here: round 29's removed floating note, whose absence
       the "floats no note over the map" test still guards by that literal
       class name. -->
  <div
    class="stage"
    class:backdrop={reach !== null}
    class:map-degraded={zonesState.degraded}
    hidden={cityStop !== null}
    onclick={reach ? surface : undefined}
    role={reach ? 'button' : undefined}
    aria-label={reach ? 'Surface — back to the map as you left it' : undefined}
  >
    <!-- The frame fits the map (#890 item 3), and the operator may take
         it by hand from there (item 4): drag or arrow keys to pan, the
         wheel to zoom, and the fit chip below to come back. The wheel
         listener is attached in script, not here, because Svelte marks a
         wheel attribute passive and a passive listener cannot stop the
         page scrolling under the map. -->
    <svg
      bind:this={mapSvgEl}
      viewBox={mapViewBox}
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label="The network map: internet above, the router at the waist, observed lanes below{undrawnNote}"
      class="pannable"
      onpointerdown={onMapPointerDown}
      onpointermove={onMapPointerMove}
      onpointerup={onMapPointerUp}
      onpointercancel={onMapPointerUp}
      onclickcapture={onMapClickCapture}
    >
      <defs>
        <!-- The host row's clip. Every lane card shares one local
             coordinate space, so one clip serves them all. Tall enough
             for a dot's outermost ring (the open card's, r + 4 at y 56)
             and wide enough for the `+N` after the tenth dot: the clip
             is the backstop that keeps a crowded lane inside its own
             card whatever the pitch works out to. -->
        <clipPath id="{uid}-hosts">
          <rect x={-cardHalf + cardPad - 8} y="44" width={cardW - 2 * cardPad + 16} height="24" />
        </clipPath>
      </defs>
      <!-- The altitude's camera (#648, concept T): a framing of the same
           real map, never a fabricated layer. "Zones" (index 2) is
           unchanged from today's card. -->
      <g class="camera" class:cam-clients={altitude === 0} class:cam-services={altitude === 1} class:cam-zones={altitude === 2}>
      <!-- The trunk: router to internet, one line, every lens, never
           per-edge (#726 -- "bundle the corridor, fan at the waist").
           Individual edges fan at the waist card instead; see
           edgePath's internet-edge branch. -->
      <path class="rib" d="M700 104 V 232" stroke="var(--accent)" stroke-width="3.5" />
      <!-- The tunnel's own two lines (#877), drawn like the trunk: once,
           on every lens, never per-edge. Both are ported from round 30
           (the-whole.html:935-936), which draws them outside the node's
           own group -- easy to miss, and the reason an earlier reading
           of the mockup called the card free-floating.
           Deviation, deliberate: round 30 strokes the solid lane
           `var(--lan)`, the LAN lane's own ink, because in that scene
           the tunnel's traffic goes to LAN. With real data that ink
           would claim a lane this line does not touch, so it takes the
           trunk's accent instead -- the same relationship the trunk
           draws, an upper node joined to the router. -->
      <!-- Round 52 (#890): one strand per tunnel, each to the router's
           right shoulder (or its top edge, for a card in the pocket
           left of the first one, beside the Internet rib). Drawn here,
           before the cards, so a strand that has to run under a nearer
           card passes beneath it -- as the Guest rib runs under the
           Guest card. A tunnel not heard for a while draws its strand
           in the dark ink, dashed, the same grammar the material uses. -->
      {#each drawnTunnels as t (t.iface)}
        <path
          class="rib"
          class:tunnel-quiet={t.quiet}
          data-tunnel-strand={t.iface}
          d={t.placed.strand}
          stroke={t.quiet ? 'var(--fg-muted)' : 'var(--accent)'}
          stroke-width="1.7"
        />
      {/each}
      {#if tunnelIface && tunnelGhostLane !== null}
        {@const gx = laneX(tunnelGhostLane, laneCount)}
        <path class="rib-ghost" d="M 1100 186 C 945 300, {gx + 95} 385, {gx} 476" />
      {/if}
      <!-- The material, under everything (round 49): every dark or quiet
           boundary-direction, drawn as its own half of the pair's rib
           whether or not anything crossed it. Dark is grey dashes,
           quiet on purpose is white -- neither carries a caption,
           because the material is the statement. Clicking one opens its
           boundary card, which is where the words live. -->
      {#each drawnCoverage.drawn as d (d.edge.key)}
        <g
          class="cov-g actionable"
          class:on={boundaryCard?.key === d.edge.key}
          role="button"
          tabindex="0"
          aria-label="Open this boundary: {coverageLabel(d.edge)}"
          onclick={() => openBoundary(d.edge)}
          onpointerenter={() => openBoundary(d.edge)}
          onpointerleave={releaseBoundary}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              openBoundary(d.edge)
            }
          }}
        >
          <title>{coverageLabel(d.edge)}</title>
          <path class="edge-hit" d={halfPath(d.line)} />
          <!-- Under a filter every direction that is not the answer
               becomes one thin grey line, and a direction carrying a
               door recedes less than the rest (#1018). Nothing is
               removed: the answer is read against the whole map. -->
          <path
            class="cedge"
            class:dark={d.cov === 'dark'}
            class:quiet={d.cov === 'quiet'}
            class:port-off={filterOn}
            d={halfPath(d.line)}
            style:opacity={filterOn ? dimFor(d.edge.key) : undefined}
          />
        </g>
      {/each}

      {#if eps > 0}
        <circle class="mote" r="2.5" fill="var(--accent)" />
      {/if}

      {#if drawnReality.drawn.length === 0}
        <!-- Before pair-carrying traffic arrives, the lanes' simple
             volume ribs keep the place alive. -->
        {#each zones as z, i (z.id)}
          <path class="rib" d={ribPath(i, laneCount)} stroke={LANE_INKS[i % LANE_INKS.length]} stroke-width="2.4" />
        {/each}
      {/if}

      <!-- The ghost's own rib (#460): still tied to the router, drawn
           dashed and in the watch's ink, because the segment is no
           longer carried but the place still is. -->
      {#each ghostLanes as g, gi (g.key)}
        <path
          class="rib grib"
          d={ribPath(zones.length + gi, laneCount)}
          stroke={GHOST_INK[g.state]}
          stroke-opacity={g.state === 'none' ? 0.55 : 0.8}
        />
      {/each}

        <!-- The reality overlay (#629): what actually happened, pair by
             pair. Accepted traffic crosses; drops die at the waist
             whatever the verdict; unplanned traffic spends the reserved
             saturated colour.

             Lines first, every label after (#723: "lines draw over the
             chips"). Each pair used to draw its own line then its own
             plate together, one <g> per pair -- but the pairs are
             siblings, so a later pair's line still painted (document
             order is paint order) over an *earlier* pair's plate
             whenever the two crossed near it. Splitting into two passes
             over the same drawnReality/ghostIntents arrays -- every line
             in this lens, then every label in it -- means no line can
             land after any plate, whatever the pairs' own routing does
             (that routing, the "lines overlapping each other" report, is
             a separate job -- see the code comment on ghostIntents). -->
        {#each drawnReality.drawn as d, di (d.r.key)}
          <!-- Nothing is drawn across a dark or quiet direction: the
               material's own half is already there, and a traffic line
               beside it would claim a log line that was never written
               (round 49). The escalated unplanned pair is the one rib
               that stays undivided -- it is not a boundary's state,
               it is the thing that should not be happening. -->
          {#if !silentDir(d.r.key)}
            {@const whole = d.r.verdict === 'unplanned'}
            <!-- Brightness is the baseline (round 49, #1016). An accepted
                 half carrying nothing off the pattern is *established*:
                 thin (0.6×) and dim, no flow, because the same routes and
                 ports every day are what a working network looks like and
                 the map should not shout them. One off-baseline line among
                 a thousand -- the roll-up in `offIndex` -- brings the whole
                 half forward at full width with the flow moving on it and a
                 ring where the traffic arrived. Refused and the escalated
                 unplanned pair are unchanged: colour is still the verdict. -->
            {@const accepted = !whole && d.r.accepts > 0}
            {@const nb = accepted && ribOffBaseline(d.r.key)}
            {@const est = accepted && !nb}
            <g
              class="edge-g"
              class:on={offCard?.key === d.r.key}
              role="button"
              tabindex="0"
              aria-label="{realityLabel(d.r)} — open this rib's reach"
              onclick={(e) => {
                e.stopPropagation()
                descendRib(d.r.from, d.r.to)
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  e.stopPropagation()
                  descendRib(d.r.from, d.r.to)
                }
              }}
              onpointerenter={nb ? () => openOffCard(d.r.key) : undefined}
              onpointerleave={nb ? releaseOffCard : undefined}
              onfocus={nb ? () => openOffCard(d.r.key) : undefined}
              onblur={nb ? releaseOffCard : undefined}
            >
              <title>{realityLabel(d.r)}</title>
              <path class="edge-hit" d={whole ? edgePath(d.line) : halfPath(d.line)} />
              <!-- Under a filter this keeps its own curve and loses its
                   verdict ink: one thin grey line, dimmed, never taken
                   off the map (#1018). The lit layer further down draws
                   the answer over it, on the same curve. -->
              <path
                class="redge"
                class:alarm={whole && !filterOn}
                class:dropped={!whole && d.r.accepts === 0}
                class:established={est}
                class:offbase={nb && !filterOn}
                class:port-off={filterOn}
                d={whole ? edgePath(d.line) : halfPath(d.line)}
                style:stroke-width="{filterOn ? 1.4 : est ? Math.max(1, realityWidth(d.r) * 0.6) : realityWidth(d.r)}px"
                style:stroke={filterOn ? 'var(--fg-muted)' : whole ? undefined : verdictInk(d.r)}
                style:opacity={filterOn ? dimFor(d.r.key) : undefined}
              />
              {#if nb && !filterOn}
                {@const rp = ringPoint(d.line)}
                <!-- The flow: dashes travelling the way the traffic ran, so
                     the bright half reads as something happening now rather
                     than a thicker line. -->
                <path
                  class="flow"
                  d={halfPath(d.line)}
                  style:stroke={verdictInk(d.r)}
                  style:stroke-width="{Math.max(1.1, realityWidth(d.r) * 0.42)}px"
                />
                {#if rp}
                  <circle class="nb-ring" cx={R2(rp.x)} cy={R2(rp.y)} r="6" />
                {/if}
              {/if}
              {#if d.r.drops > 0 && !filterOn}
                {@const bar = edgeBarAt(d.line)}
                <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                  <line class="edge-bar" class:alarm-bar={whole} x1="-7" y1="0" x2="7" y2="0" />
                </g>
              {/if}
            </g>
          {/if}
        {/each}

        <!-- The lit layer (#1018): what the filter in force is about,
             drawn over the dimmed map on the same curves rather than
             instead of them, so a rib that is both drawn and lit cannot
             end up with two geometries that disagree. Full width in the
             verdict's own ink -- the filter is not a volume reading, it
             is "this is the one you asked about" -- with the flow moving
             on an accepted half, as everywhere else. -->
        {#each litHalves as h (h.key)}
          <g class="lit-g">
            <path
              class="lit-half"
              class:refused={h.verdict === 'refused'}
              d={h.d}
              style:stroke-width="{h.width}px"
            />
            {#if h.verdict === 'accept'}
              <path class="flow lit-flow" d={h.d} style:stroke-width="{Math.max(1.1, h.width * 0.45)}px" />
            {/if}
          </g>
        {/each}
        <!-- The rib a refused line would have taken, dashed, with the
             note beside it. Never a solid line: nothing travelled it,
             and the whole reason it is dashed is that the router's log
             ends at the router. -->
        {#if traceGhost}
          <path class="trace-ghost" d={traceGhost.d} />
          <text class="trace-note" x={R2(traceGhost.at.x)} y={R2(traceGhost.at.y)} text-anchor="middle"
            >{traceGhost.text}</text
          >
        {:else if traceStopNote}
          <!-- C1: the ✕ already stands at the far gate, so there is
               nowhere left to draw a dashed rib toward -- the two grey
               words say what would have happened instead. -->
          <text class="trace-note" x={R2(traceStopNote.at.x)} y={R2(traceStopNote.at.y)} text-anchor="middle">
            {#each traceStopNote.lines as line, i (i)}
              <tspan x={R2(traceStopNote.at.x)} dy={i ? 12 : 0}>{line}</tspan>
            {/each}
          </text>
        {/if}
        <!-- Where a refused line stopped, and where an accepted one
             arrived. -->
        {#if traceStop}
          <g class="trace-stop" transform="translate({R2(traceStop.x)} {R2(traceStop.y)})">
            <circle r="9" fill="none" stroke="var(--alarm)" stroke-opacity="0.5" />
            <path d="M-5 -5L5 5M-5 5L5 -5" stroke="var(--alarm)" stroke-width="2.2" stroke-linecap="round" />
          </g>
        {/if}
        {#if traceRing}
          <circle class="nb-ring trace-ring" cx={R2(traceRing.x)} cy={R2(traceRing.y)} r="7" />
        {/if}
        <!-- The doors (#1018): every pushed rule that names the selected
             port, seen or not. Two posts across the rib where the rule
             sits, the leaf swung open for accept and a bar across for a
             refusal. Not traffic and never counted as any. -->
        {#each drawnDoors as door (door.key)}
          <g class="door" class:shut={!door.accepts}>
            <title>{door.label} · {door.who} — a pushed rule names this port here</title>
            {#if door.guide}
              <path class="door-guide" d={door.guide} />
            {/if}
            <path class="door-post" d={door.posts} />
            <path class="door-post" d={door.leaf} />
            <text class="door-t" x={door.lx} y={door.ly} text-anchor={door.anchor}
              >{door.label} <tspan class="door-act">{door.accepts ? 'accept' : door.action}</tspan></text
            >
          </g>
        {/each}

        <!-- The second delta: intent nothing arrived to fill. -->
        {#each ghostIntents as g, gi (g.edge.key)}
          <path class="gedge" d={edgePath(g.line)} />
        {/each}

        <!-- Every label this lens draws, now that every line above it is
             down. The click/keydown here duplicate the line's own (the
             plate is a real, sizeable target and deserves to be one) --
             see this file's own report on what that costs the tab order. -->
        {#each drawnReality.drawn as d, di (d.r.key)}
          <!-- The escalated pair keeps its slot in trafficBadges so
               every later index still lines up, but the slot carries no
               text and so is sized for none: its pill would be a label
               at full width over a plate 12 wide, saying a second time
               what the card below already says (#897 item 2). A silent
               direction skips its empty label the same way. -->
          {#if d !== worstUnplanned && !silentDir(d.r.key) && !filterOn}
            {@const badge = trafficBadges[di]}
            <g
              class="detail"
              role="button"
              tabindex="0"
              aria-label="{realityLabel(d.r)} — open this rib's reach"
              onclick={(e) => {
                e.stopPropagation()
                descendRib(d.r.from, d.r.to)
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  e.stopPropagation()
                  descendRib(d.r.from, d.r.to)
                }
              }}
            >
              <title>{realityLabel(d.r)}</title>
              <rect class="edge-plate" x={badge.x - badge.w / 2} y={badge.y - 10} width={badge.w} height="14" rx="4" />
              <text class="edge-badge" class:alarm-t={d.r.verdict === 'unplanned'} x={badge.x} y={badge.y} text-anchor="middle">
                {realityBadge(d.r)}
              </text>
            </g>
          {/if}
        {/each}

        <!-- The worst unplanned flow, escalated out of the row of
             identical pills into round 30's own card
             (the-whole.html:940-944, its inks kept exactly).
             The mockup's `open ▸` opens a flags-table drawer for an
             UNPLANNED flag. This product has no such flag type:
             "unplanned" is a reality verdict computed here in the
             client (reality.ts), with no id and no drawer. So the card
             opens the stream filtered to the pair -- what every other
             reality plate already does, and where the mockup's own
             drawer ultimately led. Only the middle layer is missing
             (Fable 5, #715 item 4; the finding is on the issue). -->
        {#if worstUnplanned && worstUnplannedCard && !filterOn}
          {@const c = worstUnplannedCard}
          {@const [line1, line2] = cardLines(worstUnplanned.r)}
          <g
            class="detail unplanned-card"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to this pair: {realityLabel(worstUnplanned.r)}"
            onclick={() => openPair(worstUnplanned.r.from, worstUnplanned.r.to, worstUnplanned.r.topPorts)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openPair(worstUnplanned.r.from, worstUnplanned.r.to, worstUnplanned.r.topPorts)
              }
            }}
          >
            <title>{realityLabel(worstUnplanned.r)}</title>
            <!-- The box hangs from the anchor exactly as an ordinary
                 plate does (y-10), so the layout pass above can reason
                 about both in one geometry. Everything inside is
                 measured from the box's own top-left, at the mockup's
                 offsets: the dot 13 down and 14 in, the two baselines
                 16 and 30 down (the-whole.html:941-944). -->
            <rect class="uc-box" x={c.x - c.w / 2} y={c.y - 10} width={c.w} height={CARD_H} rx="9" fill="#170a12" stroke="var(--alarm)" stroke-opacity="0.8" />
            <circle cx={c.x - c.w / 2 + 14} cy={c.y + 3} r="3" fill="var(--alarm)" />
            <text class="alarm-t" x={c.x - c.w / 2 + CARD_PAD} y={c.y + 6}>{line1}</text>
            <text class="chip-t" x={c.x - c.w / 2 + CARD_PAD} y={c.y + 20}>{line2}</text>
          </g>
          <!-- The trace opens from the flag's own chip (#1018, round
               53), and opens the list with it (#1050, round 56, B1) so
               the operator lands on the picker rather than a random
               line. A token on the card, not a sentence: `trace ▸`, the
               same `▸` grammar every other action on this map uses. It
               takes its own click back from the card, which opens the
               stream. -->
          {@const asked = worstUnplanned.r.topAsked[0]}
          <g
            class="detail uc-trace"
            role="button"
            tabindex="0"
            aria-label="Trace one of these lines on the map"
            onclick={(e) => {
              e.stopPropagation()
              openTrace(traceAsk(worstUnplanned.r, asked), { openList: true })
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                openTrace(traceAsk(worstUnplanned.r, asked), { openList: true })
              }
            }}
          >
            <text class="uc-trace-t" x={c.x + c.w / 2 - CARD_PAD} y={c.y + 20} text-anchor="end">trace ▸</text>
          </g>
        {/if}

        <!-- The straggler (#460, round 55): a line to or from a range
             that was declared dead. It takes the same arc an unplanned
             pair takes -- a pair with no rib of its own -- because it is
             the same kind of statement: traffic the map had no reason to
             expect. The line may be perfectly legal and still be this;
             what makes it a violation is only that the range is gone. -->
        {#if brokenGhost && brokenGhost.watch?.lastStraggler && !filterOn}
          {@const s = brokenGhost.watch.lastStraggler}
          {@const gi = ghostLanes.indexOf(brokenGhost)}
          {@const gx = laneX(zones.length + gi, laneCount)}
          {@const ti = laneIndexForIp(s.peer)}
          {@const call = stragglerCallout(brokenGhost.watch, s, nameForIp(s.peer))}
          {#if ti !== null}
            {@const tx = laneX(ti, laneCount)}
            {@const dir = tx < gx ? -1 : 1}
            <path
              class="rib straggler-rib"
              d="M {R2(gx + dir * 22)} 486 C {R2(gx + dir * 122)} 415, {R2(tx - dir * 116)} 415, {R2(tx - dir * 22)} 486"
            />
          {/if}
          <!-- The callout says who it was, so the operator reads a name
               they gave a machine rather than an address the move left
               behind. It sits between the two lanes, on the arc. -->
          {@const cx = ti !== null ? (gx + laneX(ti, laneCount)) / 2 : gx}
          <g
            class="detail straggler-call"
            role="button"
            tabindex="0"
            aria-label="{call.head} — open this straggler"
            onclick={(e) => {
              e.stopPropagation()
              openStragglerKey = openStragglerKey === brokenGhost.key ? null : brokenGhost.key
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                openStragglerKey = openStragglerKey === brokenGhost.key ? null : brokenGhost.key
              }
            }}
          >
            <rect class="uc-box" x={cx - 150} y="417" width="300" height={CARD_H} rx="9" fill="#170a12" stroke="var(--alarm)" stroke-opacity="0.8" />
            <circle cx={cx - 136} cy="430" r="3" fill="var(--alarm)" />
            <text class="alarm-t" x={cx - 124} y="433">{call.head}</text>
            <text class="chip-t" x={cx - 124} y="447">{call.detail} · open ▸</text>
          </g>
        {/if}
        <!-- The router's own decision (#1018): what it did, which rule
             did it, the two lanes and the NAT. Beside the router by
             default, so the leader reads as the router's own answer; or
             beside the ✕ once C1 (round 56) moves it to the far gate,
             since the chip always sits by the ✕ it explains. -->
        {#if traceChip}
          <!-- Accepted green, refused red, and neither for a log, mark
               or NAT line: those say which kind of rule logged the
               packet, not whether it passed, so the chip takes the
               page's own ink rather than picking a verdict for them. -->
          <g
            class="detail trace-chip"
            class:refused={mapTraceState.verdict === 'refused'}
            class:unjudged={!mapTraceState.verdict}
          >
            <rect x={traceChipBox.x} y={traceChipBox.y} width="248" height="42" rx="9" />
            <text class="alarm-t chip-verdict" x={traceChipBox.x + 12} y={traceChipBox.y + 18}>{traceChip.verdict}</text>
            <text class="chip-t" x={traceChipBox.x + 12} y={traceChipBox.y + 33}>{traceChip.path}</text>
            {#if refusedGateStop}
              <path
                class="trace-leader"
                d="M {R2(traceChipBox.x + 248)} {R2(traceChipBox.y + 20)} L {R2(refusedGateStop.x)} {R2(refusedGateStop.y)}"
              />
            {:else}
              <path class="trace-leader" d="M556 254 L 572 258" />
            {/if}
          </g>
        {/if}
        {#each filterOn ? [] : ghostIntents as g, gi (g.edge.key)}
          {@const badge = trafficBadges[drawnReality.drawn.length + gi]}
          <g class="detail">
            <rect class="edge-plate" x={badge.x - badge.w / 2} y={badge.y - 10} width={badge.w} height="14" rx="4" />
            <text class="edge-badge ghost-t" x={badge.x} y={badge.y} text-anchor="middle">never exercised</text>
          </g>
        {/each}



      <!-- Internet. The card itself is passive to the pointer, so a
           policy edge arriving beneath it stays clickable; its aggregate
           bar (#699 -- round 30 draws one, the-whole.html:966-973) takes
           its own clicks back. -->
      <g transform="translate(700 68)" class="passive">
        <g class="isl-card">
          <rect class="isl" x="-100" y="-30" width="200" height="60" rx="12" />
          <text x="-82" y="-3" class="n-name">Internet</text>
          {#if zonesState.wanInterface}
            <!-- `ether1 · 203.0.113.7` where the push names the boundary's
                 address, `ether1 · no address pushed` where it does not
                 (the-whole.html:977). Both tspans are drawn, sibling to
                 each other, and the `.stage.map-degraded` toggle (the-whole
                 .html:118-125's `#s3.degraded`) picks which one shows --
                 the slot is never blank and never stale either way. -->
            <text x="-82" y="14" class="n-cidr"
              >{zonesState.wanInterface}<tspan class="cidr-v">{` · ${wanAddress ?? ''}`}</tspan><tspan class="cidr-deg"
                >{' · no address pushed'}</tspan
              ></text
            >
          {:else}
            <text x="-82" y="14" class="n-sub">no public traffic observed yet</text>
          {/if}
          {#if internetAggregate && !filterOn}
            {@render aggregateBar(internetAggregate, -100, 200, 34, 16, { id: zonesState.wanInterface ?? '', name: 'the internet' })}
          {/if}
        </g>
      </g>

      <!-- The tunnel node (#877): the second upper node round 30 draws
           beside Internet (the-whole.html:986-1006), ported including
           its asymmetry -- the card runs -84 to +104 about its own
           point, not centred on it, and its bar is flush with the left
           edge. Passive for the same reason Internet is; its bar takes
           its own clicks back through `.passive .hbar-g`.
           Drawn only when a WireGuard interface has actually been
           pushed: no placeholder node, and no "unknown" card standing
           in for a tunnel nobody has told us about. -->
      {#each drawnTunnels as t (t.iface)}
        <g transform="translate({t.placed.x} {t.placed.y})" class="passive" data-tunnel={t.iface}>
          <g class="isl-card">
            <!-- Not heard for a while: round 52 draws the footprint
                 dashed in the dark ink, and writes how long at the
                 card's right edge -- the host rule, on a tunnel. -->
            <rect class="isl" class:quiet-print={t.quiet} x="-84" y="-28" width="188" height="56" rx="12" />
            {#if t.quiet && t.quietFor}
              <text x="100" y="-14" class="n-quiet" text-anchor="end">quiet · {t.quietFor}</text>
            {/if}
            <text x="-66" y="-2" class="n-name tn-name">WireGuard</text>
            {#if t.subnet}
              <text x="-66" y="14" class="n-cidr">{t.iface}{` · ${t.subnet}`}</text>
            {:else}
              <!-- One interface's own missing row, not the whole map's
                   degraded state, so this is not `.cidr-deg`'s toggle. -->
              <text x="-66" y="14" class="n-cidr"
                >{t.iface}<tspan class="cidr-none">{' · no address pushed'}</tspan></text
              >
            {/if}
            <!-- Round 49: the card says `name · subnet`, and adds a
                 word only where the drawing cannot. Round 30's UP and
                 QUIET badges sat here in the coverage badge's own ink
                 and vocabulary, saying what the ribs leaving the node
                 now say themselves -- so they are gone with the rest of
                 the coverage captions. A tunnel the router calls *down*
                 is a different fact, and the one thing on this card no
                 line on the 2D map draws, so it stays. -->
            {#if t.state === 'down' || t.state === 'unknown'}
              <text x="54" y="-4" class="n-cov {t.covClass}">{t.stateLabel}</text>
            {/if}
            {#if t.aggregate && !filterOn}
              {@render aggregateBar(t.aggregate, -84, 188, 32, 16, { id: t.iface, name: t.iface })}
            {/if}
          </g>
          <!-- The removed survey stop used to draw every node as a plain
               dot-plus-label (#869); this was that dot for the tunnel
               node specifically, left behind when the stop went. It sat
               unconditioned by any camera class -- unlike every other
               tiered element on this map -- so it stayed on screen at
               every altitude, its own "WireGuard" printed over the
               card's (#976 item 1/2: "both renderings show at once").
               The card above already names the tunnel; nothing here was
               a second fact. -->
        </g>
      {/each}

      <!-- The waist. Passive like the internet: every policy edge
           routes through here, and the island must not eat their clicks. -->
      <g transform="translate(700 268)" class="passive">
        <g class="isl-card">
          <!-- The card grows to hold the statement rather than the
               statement overrunning the card (round-36/README.md's own
               validation note): the-whole.html's `#s3.degraded .rt-isl
               { height: 100px }`, bound here rather than set in CSS so
               the geometry is the attribute the renderer reads. -->
          <rect class="isl waist" x="-128" y="-34" width="256" height={degradedStatement ? 100 : 68} rx="12" />
          <text x="-110" y="-6" class="n-name">{primaryDevice?.name ?? 'your router'}</text>
          <text x="-110" y="12" class="n-sub">{waistSub}</text>
          {#if degradedStatement}
            <text x="-110" y="34" class="deg-t">no address table pushed — zones from boundaries</text>
            <text x="-110" y="50" class="deg-t"
              ><tspan
                class="deg-go"
                role="button"
                tabindex="0"
                aria-label="Run setup — it adds the /ip address table"
                onclick={(e) => {
                  e.stopPropagation()
                  wizardState.launch()
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    e.stopPropagation()
                    wizardState.launch()
                  }
                }}>Run setup ▸</tspan
              > adds the address table</text
            >
          {/if}
        </g>
      </g>

      <!-- The lanes -->
      {#each zones as z, i (z.id)}
        {@const agg = zoneAggregate(z)}
        {@const row = laneHostRow(z)}
        {@const ink = LANE_INKS[i % LANE_INKS.length]}
        <!-- Under a filter a lane with nothing in the answer recedes
             whole (#1018): still drawn, still clickable, simply not part
             of what was asked. -->
        <g
          transform="translate({laneX(i, laneCount)} 490)"
          class="zone"
          class:lane-off={filterOn && !laneInFilter(z, row)}
          role="button"
          tabindex="0"
          data-zone={z.id}
          aria-label="{z.name} — open its reach"
          onclick={(e) => {
            // Clicking off anywhere surfaces, so a door into the reach
            // has to stop its own click reaching that -- the same guard
            // the host dots and the router carry.
            e.stopPropagation()
            descendZone(z)
          }}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              e.stopPropagation()
              descendZone(z)
            }
          }}
        >
          <!-- The card. Its width is the lane pitch's own (#699), so
               everything inside it is measured from the card's edge
               rather than from round 30's four fixed lane positions. -->
          <g class="isl-card">
            <rect class="isl" x={-cardHalf} y="0" width={cardW} height="106" rx="12" />
            <!-- The lane's own ink. Named, because the card now holds a
                 row of host dots too and "the card's circles" stopped
                 being one thing. -->
            <circle class="lane-ink" cx={-cardHalf + cardPad} cy="22" r="3.5" fill={ink} />
            <text x={-cardHalf + cardPad + 11} y="26" class="n-name">{z.name}</text>
            <!-- Anchored to the card's right inset, not a fixed x: a
                 narrower card moves it in rather than past the edge. The
                 slot always holds something true: the pushed CIDR, or
                 "from boundaries" where the zone's name and extent rest
                 on the boundary alone -- both tspans drawn sibling to
                 each other (the-whole.html:1026), the `.stage.map-degraded`
                 toggle picking which one shows. -->
            <text x={cardHalf - 14} y="26" class="n-cidr" text-anchor="end"
              ><tspan class="cidr-v">{z.cidr ?? ''}</tspan><tspan class="cidr-deg">from boundaries</tspan></text
            >
            {#if row.dots.length > 0}
              <!-- The host dot row (round 49, ported from
                   round-49/index.html's node2): one dot per host, ten
                   then `+N`, in the lane's own ink while the host is
                   live, grey once it has gone quiet, white translucent
                   where the operator said the quiet was on purpose.
                   Every dot is the reach's door (#626) -- clicking it
                   recentres on that host rather than opening the zone --
                   and hovering it opens the host's card. -->
              <g class="hostrow" clip-path="url(#{uid}-hosts)">
                {#each row.dots as d, di (d.key)}
                  {@const w = nodeWarnings(d.ip)}
                  {@const dotNb = hostOffBaseline(d.ip)}
                  <!-- Under a filter only the hosts in the answer stay
                       lit; the rest recede without leaving the card
                       (#1018). A dot is never removed -- "2 of 12" is a
                       statement about twelve hosts, and the twelve have
                       to still be there for it to be one. -->
                  <g
                    class="hot"
                    class:dot-off={filterOn && !litHosts.has(d.ip)}
                    role="button"
                    tabindex="0"
                    aria-label="{hostDotLabel(d)} — open its reach"
                    onclick={(e) => {
                      e.stopPropagation()
                      descendFromHost(z.id, d.label, d.ip)
                    }}
                    onkeydown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        e.stopPropagation()
                        descendFromHost(z.id, d.label, d.ip)
                      }
                    }}
                    onpointerenter={() => openHostCard(z.id, d.key)}
                    onpointerleave={releaseHostCard}
                    onfocus={() => openHostCard(z.id, d.key)}
                    onblur={releaseHostCard}
                  >
                    <title>{hostDotLabel(d)}</title>
                    <circle
                      class="h-dot"
                      class:quiet={d.presence === 'quiet'}
                      class:intended={d.presence === 'intended'}
                      cx={hostDotX(di)}
                      cy={HOST_DOT_Y}
                      r={hostDotR}
                      fill={d.presence === 'live' ? ink : undefined}
                    />
                    <!-- The quiet host's dashed footprint (DESIGN.md
                         "Presence", city and 2D alike): the dot keeps
                         its place on the map and says only that nothing
                         has been heard from it. -->
                    {#if d.presence === 'quiet'}
                      <circle class="h-foot" cx={hostDotX(di)} cy={HOST_DOT_Y} r={hostDotR + 2} />
                    {/if}
                    <!-- Flagged: a halo that hugs the dot and throbs in
                         place. It never pulses outward (owner,
                         2026-09-07) -- a travelling ring reads as
                         something moving through the network, and
                         nothing here moved. Drawn whenever the host has
                         an open flag and never otherwise (#981): there
                         is no pill in front of it. -->
                    {#if w.flagCount > 0}
                      <circle class="h-halo" cx={hostDotX(di)} cy={HOST_DOT_Y} r={hostDotR + 1.5} />
                    {/if}
                    <!-- The roll-up reaches the dot too (DESIGN.md's
                         "a road, rib or host dot is as bright as the
                         brightest line it carries"): a machine that spoke
                         a line off the pattern today takes the accept
                         ink's own ring, throbbing in place the way the
                         rib's does. Pushed out past the flag halo when
                         that is drawn as well, so the two facts stay two
                         rings rather than merging into one. The mockup
                         draws no dot ring -- it puts today's three lines
                         on ribs -- so this is DESIGN.md's rule applied
                         where the mockup is silent. -->
                    {#if dotNb}
                      <circle class="h-nb" cx={hostDotX(di)} cy={HOST_DOT_Y} r={hostDotR + (w.flagCount > 0 ? 3.5 : 1.5)} />
                    {/if}
                    <!-- Watched: the same purple this screen already uses
                         for watchers, drawn whenever something watches
                         this host (#981). -->
                    {#if w.watchCount > 0}
                      <circle class="h-watch" cx={hostDotX(di)} cy={HOST_DOT_Y} r={hostDotR + 2.5} />
                    {/if}
                    {#if hostCard?.key === d.key}
                      <circle class="h-open" cx={hostDotX(di)} cy={HOST_DOT_Y} r={hostDotR + 4} />
                    {/if}
                  </g>
                {/each}
                {#if row.more > 0}
                  <text class="c-label more" x={hostDotX(row.dots.length) - 4} y={HOST_DOT_Y + 4}>+{row.more}</text>
                {/if}
              </g>
              <!-- The count line: how many hosts the lane has, and how
                   many of them are not being heard from. -->
              <!-- Under a filter this says what the filter is about --
                   `2 of 12 hosts on 445/tcp`, or the traced end's own
                   tally -- and goes back to the presence count when the
                   filter clears. -->
              {@const tally = filterOn ? filterTally(row) : null}
              <text x={-cardHalf + cardPad} y="82" class="n-sub hosttally" class:filter-tally={!!tally}
                >{tally ?? hostTally(row)}</text
              >
            {:else if !filterOn}
              <!-- #1165: a zone the router named but whose addresses
                   resolved to nothing left this band blank, which reads
                   as a card that failed to draw. It says the fact
                   instead, in the tally's own slot. Under a filter it
                   stays silent: #1056 ruled that a lane with no known
                   host says nothing rather than `0 of 0`, and "no hosts
                   seen yet" would be answering a question about the
                   port that this lane cannot answer either. -->
              <text x={-cardHalf + cardPad} y="82" class="n-sub hosttally">no hosts seen yet</text>
            {/if}
            <!-- Round 49: the card says `name · subnet` and stops.
                 LOGGED / DARK / COVERED are gone from it -- the ribs
                 leaving the lane already carry each boundary's own
                 state, and a badge repeating them was the map saying
                 twice what it had already drawn once.
                 `no rule table pushed` stays, dim, because it is a
                 different fact from dark: dark means a table exists and
                 nothing on it logs this boundary; this means there is
                 no table at all (#865's wording). It sits under the
                 count line rather than where the old name list left it:
                 round 49 gives y 82 to the tally and is silent about
                 this line, and the two facts are both the card's own
                 sub-text, so they stack. -->
            {#if !policyState.anyPushed}
              <!-- Always under the tally line now that an empty lane
                   prints one of its own (#1165), rather than moving up
                   into the slot that line occupies. -->
              <text x={-cardHalf + cardPad} y="96" class="n-sub no-table">no rule table pushed</text>
            {/if}
            <!-- Round 30's zone card carries name, subnet, hosts and the
                 coverage badge, and stops there (the-whole.html:1002-1008).
                 A fifth "N events this window" line was drawn here and the
                 mockup draws it nowhere, so it is off (#715 item 9). The
                 count itself stays: `zones.svelte.ts:161` sorts the lane row
                 by it, so it is load-bearing data, not dead code. -->
            {#if agg && !filterOn}
              <!-- Flush with the card and 16 tall (#699; round 30's
                   `translate(-108 110)` at 216x16, the-whole.html:
                   1009-1015). The old 188x12 floated inset under a
                   216-wide card and read as unrelated furniture. -->
              {@render aggregateBar(agg, -cardHalf, cardW, 110, 16, z)}
            {/if}
          </g>
        </g>
      {/each}

      <!-- The ghost lanes (#460, round 55): a segment the router has
           stopped carrying, kept in its place on the map until its watch
           retires. Round 30's lane card, hollowed: the plate is dashed
           and unfilled, the lane dot is a ring rather than a disc, the
           hosts are rings because they are names the range last had
           rather than machines anyone can see now, and the whole thing
           is drawn in its watch's own ink -- grey while the offer is
           unanswered, watch purple while it holds, alarm red when a
           straggler has just contradicted the declaration. -->
      {#each ghostLanes as g, gi (g.key)}
        {@const ink = GHOST_INK[g.state]}
        {@const strag = g.watch?.lastStraggler ?? null}
        <g
          transform="translate({laneX(zones.length + gi, laneCount)} 490)"
          class="zone ghost-lane"
          class:broken={g.state === 'broken'}
          role="button"
          tabindex="0"
          data-ghost={g.cidr}
          aria-label="{g.name} — retired range {g.cidr}, watch {g.state}"
          onclick={(e) => {
            e.stopPropagation()
            openGhost(g)
          }}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              e.stopPropagation()
              openGhost(g)
            }
          }}
        >
          <rect class="isl gisl" x={-cardHalf} y="0" width={cardW} height="106" rx="12" stroke={ink} stroke-opacity={g.state === 'none' ? 0.5 : 0.75} />
          <circle class="gdot" cx={-cardHalf + cardPad} cy="22" r="3.5" stroke={ink} />
          <text x={-cardHalf + cardPad + 11} y="26" class="n-name gname">{g.name}</text>
          <text x={cardHalf - 14} y="26" class="n-cidr" text-anchor="end">{g.cidr}</text>
          {#if g.hosts.length > 0}
            <g class="hostrow">
              {#each g.hosts.slice(0, MAX_HOST_DOTS) as h, hi (h.ip)}
                <g>
                  <title>{h.label} · last known here; the range is retired</title>
                  <circle class="ghost-host" cx={hostDotX(hi)} cy={HOST_DOT_Y} r={hostDotR} />
                  <!-- The straggler wears the halo, so the ring the
                       operator has to deal with is the one the callout
                       is about rather than whichever came first. -->
                  {#if g.state === 'broken' && strag?.address === h.ip}
                    <circle class="halo ghost-halo" cx={hostDotX(hi)} cy={HOST_DOT_Y} r={hostDotR + 2.5} />
                  {/if}
                </g>
              {/each}
              <text x={hostDotX(Math.min(g.hosts.length, MAX_HOST_DOTS)) + 2} y={HOST_DOT_Y + 4} class="c-label">last known</text>
            </g>
          {/if}
          <text x={-cardHalf + cardPad} y="82" class="n-sub gsub" style:fill={g.state === 'none' ? undefined : ink}>
            {ghostTally(g.state, g.watch, g.offer, nowMs)}
          </text>
          <!-- The watcher pip, the same bar every live lane carries: a
               ghost is a watched thing, and round 55 asks for the same
               pip rather than a mark of its own. -->
          {#if g.watch && !filterOn}
            {@render aggregateBar({ watchCount: 1, watchBroken: g.state === 'broken' ? 1 : 0, flagCount: 0 }, -cardHalf, cardW, 110, 16, {
              id: g.iface,
              name: g.name,
            })}
          {/if}
        </g>
      {/each}

      <!-- Depth, not zoom (#699). Round 30's depth stops *add* layers --
           services, then the clients beneath their own lane -- where the
           build scaled the same picture up and pushed its right edge off
           the stage. Both are drawn from what the app already knows and
           are absent where it knows nothing. -->
      <g class="svc">
        {#each zones as z, i (z.id)}
          {@const svc = laneServices(z)}
          <!-- Silent under a filter: this lane's busiest ports are a
               different question from the one being asked, and two port
               answers on one card is the map saying two things at once. -->
          {#if svc && !filterOn}
            <!-- "Ports floating in the wind" (owner, #723): the port list
                 named nothing it sat above -- a 24-unit gap to the card
                 with no line, no plate, nothing between them. Pulled in
                 to a small, proximate gap and given a leader tick down to
                 the card's own top edge (y=490), the tie every other
                 label on this map already has via its plate or its line. -->
            <text x={laneX(i, laneCount)} y="472" text-anchor="middle" class="svc-t">{svc}</text>
            <line class="svc-leader" x1={laneX(i, laneCount)} y1="477" x2={laneX(i, laneCount)} y2="489" />
          {/if}
        {/each}
      </g>
      <g class="cli">
        {#each zones as z, i (z.id)}
          {@const cx = laneX(i, laneCount)}
          {@const spread = Math.min(70, lanePitch * 0.26)}
          {@const drawn = z.hosts.slice(0, 3)}
          {@const ink = LANE_INKS[i % LANE_INKS.length]}
          {#each drawn as h, hi (h.ip)}
            {@const dx = (hi - (drawn.length - 1) / 2) * spread}
            {@const x = cx + dx}
            <!-- Each client is the reach's door too, same as its own name
                 in the card above (#723): the mockup wires its own
                 .c-dot/.c-label to open that host's information
                 (the-whole.html:2238), which this build never carried
                 over -- these sat inert, so a click meant for a client
                 icon fell through to whatever was behind it. Vertical
                 span compressed from the old 636→716 (which put "+n more"
                 4px off the stage's own 720 floor, "nodes clash with
                 bottom", #723) down to 636→696, comfortably clear at
                 every altitude stop. -->
            {#if Math.abs(dx) < 0.5}
              <!-- One lane per host to its gate, and it takes the
                   brightness of the brightest line it carries: dim while
                   the machine is talking on the same routes it always
                   does, forward the day it is not. -->
              <path class="cli-spoke" class:offbase={hostOffBaseline(h.ip)} d="M{cx} 636 C {cx - 6} 644, {cx + 6} 647, {cx} 655" stroke={ink} />
              <circle
                class="c-dot"
                cx={cx}
                cy="659"
                r="5"
                fill={ink}
                role="button"
                tabindex="0"
                aria-label="Recentre on {h.label}"
                onclick={(e) => {
                  e.stopPropagation()
                  descend(z.id, h.label, h.ip)
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    e.stopPropagation()
                    descend(z.id, h.label, h.ip)
                  }
                }}
              />
              <text
                x={cx}
                y="680"
                text-anchor="middle"
                class="c-label"
                role="button"
                tabindex="0"
                aria-label="Recentre on {h.label}"
                onclick={(e) => {
                  e.stopPropagation()
                  descend(z.id, h.label, h.ip)
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    e.stopPropagation()
                    descend(z.id, h.label, h.ip)
                  }
                }}>{h.label}</text
              >
            {:else}
              <path class="cli-spoke" class:offbase={hostOffBaseline(h.ip)} d="M{cx} 636 C {cx} 644, {x} 644, {x} 650" stroke={ink} />
              <circle
                class="c-dot"
                cx={x}
                cy="654"
                r="5"
                fill={ink}
                role="button"
                tabindex="0"
                aria-label="Recentre on {h.label}"
                onclick={(e) => {
                  e.stopPropagation()
                  descend(z.id, h.label, h.ip)
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    e.stopPropagation()
                    descend(z.id, h.label, h.ip)
                  }
                }}
              />
              <text
                x={x}
                y="666"
                text-anchor="middle"
                class="c-label"
                role="button"
                tabindex="0"
                aria-label="Recentre on {h.label}"
                onclick={(e) => {
                  e.stopPropagation()
                  descend(z.id, h.label, h.ip)
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    e.stopPropagation()
                    descend(z.id, h.label, h.ip)
                  }
                }}>{h.label}</text
              >
            {/if}
          {/each}
          {#if z.hostCount > drawn.length}
            <text x={cx} y="696" text-anchor="middle" class="c-label more">+ {z.hostCount - drawn.length} more</text>
          {/if}
        {/each}
      </g>

      <!-- Nothing seen on the port is one line under the map, not an
           empty state (#1018, round 53). The map is not broken and the
           doors above are still drawn; this says why it looks quiet and
           what policy has to say about the port anyway. -->
      {#if nothingSeenNote}
        <text x="700" y="662" text-anchor="middle" class="note-t">{nothingSeenNote}</text>
      {/if}

      <!-- Retirement is silent (#460, round 55): six quiet hours and the
           ghost leaves by itself, the map goes back to what it was, and
           one line under it says what happened. The undo stands for the
           hour after, because a retirement nobody was asked about should
           be reversible by the person who finds out afterwards. -->
      {#each decommissionsState.retired.filter((w) => undoable(w, nowMs)) as w (w.id)}
        <text x="700" y={nothingSeenNote ? 678 : 662} text-anchor="middle" class="note-t"
          >{retiredNote(w)} ·
          <tspan
            class="b"
            role="button"
            tabindex="0"
            aria-label="Undo the retirement of {w.cidr} — bring the ghost back"
            onclick={(e) => {
              e.stopPropagation()
              void decommissionsState.undo(w.id)
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                void decommissionsState.undo(w.id)
              }
            }}>undo ▸</tspan
          ></text
        >
      {/each}

      {#if laneCount === 0}
        <!-- The honest empty state: the place before the data. A ghost
             lane counts against it -- a retired segment is something
             that arrived, and saying "nothing has arrived yet" beside
             one drawn on the map would be untrue. -->
        <g transform="translate(700 500)">
          <rect class="isl ghost" x="-108" y="0" width="216" height="106" rx="12" />
          <text x="0" y="40" text-anchor="middle" class="n-sub">nothing has arrived yet — waiting for data, not broken</text>
          <text x="0" y="58" text-anchor="middle" class="n-sub">the map draws itself as traffic arrives; mikroview never draws a guess</text>
        </g>
      {/if}

      <!-- The zones stop's own ground plan (#852, #869's ruling on
           #852): the same positions the city lays out for every zone,
           router, tunnel, river and road -- `ground`, computed once
           above and shared with <City> below -- drawn flat, in the 2D
           map's own vocabulary, and reduced: a card with a host count,
           no dots, no per-host labels (those are `clients`'s job).
           Present at every altitude like every other camera layer;
           `.camera.cam-zones` is what shows it, the same convention the
           removed survey dot used to follow. It used to be left showing
           alongside the lens's own edge lines above rather than
           reconciling the two coordinate systems -- #726 bundled the
           edges' own overlap but never touched this -- so both
           renderings painted at once: this card's river and roads
           together with the lane-based rib/edge lines, neither drawn to
           the other's positions (#976 item 1). The stylesheet now hides
           `.rib`/`.rib-ghost`/`.mote`/`.edge-g`/`.gedge` at cam-zones
           alongside `.isl-card`/`.detail`, so zones draws this ground
           plan alone. -->
      <g class="ground-flat">
        {#if ground.river}
          <path
            class="gf-river"
            d={'M' +
              ground.river.bankN.map((p) => `${R2(FX(flatCam, p[0]))} ${R2(FY(flatCam, p[1]))}`).join(' L') +
              ' L' +
              [...ground.river.bankF]
                .reverse()
                .map((p) => `${R2(FX(flatCam, p[0]))} ${R2(FY(flatCam, p[1]))}`)
                .join(' L') +
              ' Z'}
          />
        {/if}
        <!-- Under every road and every card, on purpose (#989): the tie
             between a subnet and its router is background, and traffic
             is the thing being read. -->
        {#each flatBelongs as b (b.id)}
          <line class="gf-belong" x1={R2(b.x1)} y1={R2(b.y1)} x2={R2(b.x2)} y2={R2(b.y2)} />
        {/each}
        {#each ground.roads as r (r.id)}
          {@const d = 'M' + r.pts.map((p) => `${R2(FX(flatCam, p[0]))} ${R2(FY(flatCam, p[1]))}`).join(' L')}
          {@const rib = ribEndsOf(r)}
          {#if rib}
            <!-- Clicking a rib opens its reach, the same as clicking one
                 on the lens above (#1016). Nothing is written on the
                 road itself: what it carried is on the card its own line
                 opens, and round 49 put no words on a road. -->
            <g
              class="gf-road-g"
              role="button"
              tabindex="0"
              aria-label="{pairName(rib[0], rib[1])} — open this rib's reach"
              onclick={(e) => {
                e.stopPropagation()
                descendRib(rib[0], rib[1])
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  e.stopPropagation()
                  descendRib(rib[0], rib[1])
                }
              }}
            >
              <title>{pairName(rib[0], rib[1])}</title>
              <path class="gf-road-hit" {d} />
              <path class="gf-road gf-road-{r.k}" {d} />
            </g>
          {:else}
            <path class="gf-road gf-road-{r.k}" {d} />
          {/if}
        {/each}
        {#each ground.nodes as n (n.id)}
          {@const nx = FX(flatCam, n.u)}
          {@const ny = FY(flatCam, n.v)}
          {@const nr = Math.max(4, n.R * flatCam.S * 0.6)}
          <!-- Clicking anything opens its reach (round 49, DESIGN.md
               "The reach"). A router is a subject the reach already
               understands: the city stands on the router building by the
               very same address, `Building.ip` off the shared ground
               model (lib/city/layout.ts), so this is the city's own
               ratified subject rather than a second idea of what a
               router's reach means. A bridge-head post is not -- its
               `ip` is the words 'wan bridge', not an address -- so it
               stays undrawn as a door rather than opening a reach on a
               subject the events never carry. -->
          {#if n.kind === 'router' || n.kind === 'router-ant'}
            <g
              class="gf-node-g"
              role="button"
              tabindex="0"
              data-router={n.id}
              aria-label="{n.name} — open its reach"
              onclick={(e) => {
                // Clicking off anywhere surfaces, so a door into the
                // reach has to stop its own click reaching that -- the
                // same guard the host dots carry.
                e.stopPropagation()
                descend(n.districtId ?? '', n.name, n.ip)
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  e.stopPropagation()
                  descend(n.districtId ?? '', n.name, n.ip)
                }
              }}
            >
              <title>{n.name} · {n.ip}</title>
              <circle class="gf-node" cx={nx} cy={ny} r={nr} />
              <text class="gf-node-label" x={nx} y={ny - nr - 4} text-anchor="middle">{n.name}</text>
            </g>
          {:else}
            <circle class="gf-node" cx={nx} cy={ny} r={nr} />
            <text class="gf-node-label" x={nx} y={ny - nr - 4} text-anchor="middle">{n.name}</text>
          {/if}
        {/each}
        {#each flatCards as fc (fc.d.id)}
          <!-- The name and CIDR used to sit either side of the card's
               own centre line, so the smallest districts (`gr` at its
               30 floor, a 60-wide card) printed them on top of each
               other -- "10.0.10.1/24 shows through LAN" (#976 item 2).
               They now stack instead, which is the actual fix: it holds
               regardless of card width. `gr`/`gh`'s floors and the
               push-apart pass computing `flatCards` (above) are what
               keep two cards clear of each other -- see that comment
               for why a floor alone was not enough. -->
          {@const total = fc.d.buildings.length + fc.d.more}
          <g
            class="gf-card"
            class:dark={fc.d.dark}
            transform="translate({R2(fc.x)} {R2(fc.y)})"
            role="button"
            tabindex="0"
            data-zone={fc.d.id}
            aria-label="{fc.d.name} — open its reach"
            onclick={(e) => {
              e.stopPropagation()
              descendZone(fc.d)
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                descendZone(fc.d)
              }
            }}
          >
            <rect
              class="gf-plate"
              x={-fc.gr}
              y={-fc.gh / 2}
              width={fc.gr * 2}
              height={fc.gh}
              rx="10"
              stroke={LANE_INKS[fc.d.ink % LANE_INKS.length]}
            />
            <text class="n-name" x={-fc.gr + 12} y={-fc.gh / 2 + 18}>{fc.d.name}</text>
            <text class="n-cidr" x={-fc.gr + 12} y={-fc.gh / 2 + 32}>{fc.d.cidr ?? CIDR_FALLBACK}</text>
            <!-- Round 49: `name · subnet`, and the zones stop's own
                 host count. The DARK word that used to trail the count
                 is gone with every other coverage caption -- the
                 plate's own material says it, dashed and grey where
                 every boundary of the district is dark. -->
            <text class="gf-count" x={-fc.gr + 12} y={-fc.gh / 2 + 48}>{total} host{total === 1 ? '' : 's'}</text>
          </g>
        {/each}
      </g>
      </g>
    </svg>
    <!-- The fit chip (round 51, carried into 52): how far out the frame
         is, and the way back to it. `NN%` is this view's scale against
         the map's nominal frame, so a fitted map that needed no extra
         frame reads 100 %; clicking returns to the fitted frame. Hidden
         under a reach, where the map is a blurred backdrop whose one
         affordance is surfacing. -->
    {#if !reach}
      <button
        type="button"
        class="fitchip"
        bind:this={fitChipEl}
        class:hand={mapView !== null}
        aria-label={mapView !== null ? 'Zoomed by hand — fit the whole map' : 'The whole map, fitted'}
        title={mapView !== null ? 'zoomed by hand — click to fit the whole map' : 'the whole map, fitted'}
        onclick={(e) => {
          e.stopPropagation()
          fitMap()
        }}
      >
        <span class="z">{mapZoom}%</span><span class="sep"></span><span class:dimt={mapView === null}>⤢ fit</span>
      </button>
    {/if}
    {#if reach}
      <!-- The ascend control (#682, ported from the scene): inside the
           map's own flow, top-left of the stage -- not a fixed pill
           floating over the whole card. -->
      <button
        class="ascend"
        onclick={(e) => {
          e.stopPropagation()
          surface()
        }}
      >
        ⌃ surface — the map, as you left it
      </button>
    {/if}
  </div>

  {#if cityStop}
    <!-- The city (#863), joined to the map (#869): the same estate in
         isometric, at the three stops right of the diamond. The
         ratified record gives both views the same header, pills,
         badges and callout wording -- all the 2D map's, drawn once
         above and reused here. There is no lens to thread down any
         more (round 49): coverage is the material on both surfaces and
         policy went in slice C. `ground` is the one shared layout
         (#852) computed above, so this side and the zones stop can
         never disagree on a position. `initialS`/`initialCentre` and
         the two `on*` callbacks are the pan/reach carry described by
         crossAltitudeCentre's own doc comment, above. -->
    <City
      stop={cityStop}
      ground={ground}
      initialS={cityView?.S}
      initialCentre={cityView?.centre}
      onCameraChange={(s, centre) => (cityView = { S: s, centre })}
      onStandChange={(b) => (cityStandBuilding = b)}
    />
  {/if}

  {#if reach && reachSummary}
    <!-- The membrane view. pointer-events pass through everywhere
         except its own content, so clicking off anywhere surfaces. -->
    <div class="membrane-layer" aria-label="The reach: {reach.host} and what it talks to">
      <svg viewBox="0 0 1400 620" preserveAspectRatio="xMidYMid meet" bind:this={membraneSvgEl}>
        <circle cx={MX} cy={MY} r={MR} class="membrane" />
        <text x={MX} y="502" text-anchor="middle" class="n-sub">
          the membrane — lane-mates inside talk freely; every crossing needs a rule, per direction
        </text>

        <!-- Nothing is written on a strand (round 49; DESIGN.md "The
             reach", and its own Superseded list: "Pill labels on reach
             strands and ports written along a road: the card carries
             them"). The pills that used to stack from each counterpart's
             anchor are gone, and with them the composer's old door --
             `draft the rule ▸` on the refused line's card is the door
             now, which is where the ports it drafts from are read
             anyway.

             What is left is the drawing saying it itself: colour is the
             verdict, brightness is the baseline. An established line is
             thin and dim with no flow; one off the baseline is full
             width and bright with the flow moving on it and a ring
             throbbing where it arrived; a refused one is red and ends in
             a ✕ where the rule stopped it. Nothing is removed, only
             dimmed. Same three-way treatment, and the same class names,
             as the ribs above. -->
        {#each reachSummary.strands as s (s.key)}
          {@const refused = s.outcome === 'blocked'}
          {@const nb = !refused && strandOffBaseline(s.counterpart)}
          {@const est = !refused && !nb}
          {@const d = strandPath(s.counterpart, s.outcome, s.direction)}
          <g
            class="strand-g"
            class:on={lineCard?.counterpart === s.counterpart}
            role="button"
            tabindex="0"
            aria-label="{reachSideName} {s.direction === 'out' ? '→' : '←'} {counterpartName(s.counterpart)} — the ports on this line"
            onpointerenter={() => openLineCard(s.counterpart)}
            onpointerleave={releaseLineCard}
            onfocus={() => openLineCard(s.counterpart)}
            onblur={releaseLineCard}
            onclick={(e) => {
              e.stopPropagation()
              openLineCard(s.counterpart)
              lineCardPinned = true
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                e.stopPropagation()
                openLineCard(s.counterpart)
                lineCardPinned = true
              }
            }}
          >
            <title>{reachSideName} {s.direction === 'out' ? '→' : '←'} {counterpartName(s.counterpart)}</title>
            <!-- A wide invisible line, so the pointer can find a strand
                 drawn 1.3px thin without having to trace it exactly. -->
            <path class="strand-hit" {d} />
            <path
              class="strand"
              class:established={est}
              class:offbase={nb}
              class:refused
              {d}
              stroke={refused ? 'var(--alarm)' : 'var(--accept)'}
              style:stroke-width="{est ? 1.3 : 2.2}px"
            />
            {#if nb}
              {@const rp = strandRingPoint(s)}
              <path class="flow" {d} style:stroke="var(--accept)" style:stroke-width="1.1px" />
              <circle class="nb-ring" cx={R2(rp.x)} cy={R2(rp.y)} r="7" />
            {/if}
            {#if refused}
              <!-- The ✕ where the rule stopped it, at the membrane the
                   strand died at (round-49/index.html:1319). -->
              {@const p = membranePoint(s.counterpart, s.outcome, s.direction)}
              <g class="strand-x" transform="translate({R2(p.x)} {R2(p.y)})">
                <path d="M-5 -5L5 5M-5 5L5 -5" stroke="var(--alarm)" stroke-width="2" stroke-linecap="round" />
                <circle r="9" fill="none" stroke="var(--alarm)" stroke-opacity="0.45" />
              </g>
            {/if}
          </g>
        {/each}

        <!-- the host, centred, with its lane-mates inside. Its own node
             card (#648, rounds 22-23) -- currently inert here otherwise,
             so this is additive. -->
        <g
          transform="translate({MX} {MY})"
          class="host-node"
          role="button"
          tabindex="0"
          aria-label="{reach.host}'s information"
          onclick={(e) => openNodeCard(e, reach!.host, reach!.ip, zones.find((z) => z.id === reach!.zoneId)?.name ?? null)}
          onkeydown={(e) => {
            if (e.key === 'Enter') openNodeCard(e, reach!.host, reach!.ip, zones.find((z) => z.id === reach!.zoneId)?.name ?? null)
          }}
        >
          <circle r="46" class="host-circle" style:stroke={reachZoneInk} />
          {#if reach.host !== reach.ip}
            <text y="-5" text-anchor="middle" class="n-name">{reach.host}</text>
            <text y="12" text-anchor="middle" class="n-cidr small">{reach.ip}</text>
          {:else}
            <text y="4" text-anchor="middle" class="n-cidr small">{reach.ip}</text>
          {/if}
        </g>
        {#each siblings as sib, i (sib.ip)}
          <g
            transform="translate({i === 0 ? 478 : 646} {i === 0 ? 408 : 412})"
            class="sibling"
            role="button"
            tabindex="0"
            aria-label="Recentre on {sib.label}"
            onclick={(e) => {
              e.stopPropagation()
              if (reach) descend(reach.zoneId, sib.label, sib.ip)
            }}
            onkeydown={(e) => {
              if (e.key === 'Enter' && reach) {
                e.stopPropagation()
                descend(reach.zoneId, sib.label, sib.ip)
              }
            }}
          >
            <circle r="20" class="sibling-circle" style:stroke={reachZoneInk} />
            <text y="34" text-anchor="middle" class="n-sub">{sib.label}</text>
          </g>
        {/each}

        <!-- counterpart clusters. Each one's own node card (#648, rounds
             22-23: "any client dot or name... in the descended view") --
             currently inert here otherwise, so this is additive -- and
             the same aggregate bar (round 23) the surfaced zone islands
             carry, correlated the same way when the counterpart is
             itself a real zone. -->
        {#each reachCounterparts as c, i (c)}
          {@const slot = SLOTS[i]}
          {@const strandsFor = reachSummary.strands.filter((s) => s.counterpart === c)}
          {@const cZone = zones.find((z) => z.id === c)}
          {@const agg = cZone ? zoneAggregate(cZone) : null}
          <g
            transform="translate({slot.x} {slot.y})"
            class="cluster-g"
            role="button"
            tabindex="0"
            aria-label="{c}'s information"
            onclick={(e) => openNodeCard(e, c, cZone?.cidr ?? null, cZone?.name ?? (c === 'internet' ? 'the internet' : c))}
            onkeydown={(e) => {
              if (e.key === 'Enter') openNodeCard(e, c, cZone?.cidr ?? null, cZone?.name ?? (c === 'internet' ? 'the internet' : c))
            }}
          >
            <rect class="cluster" x="0" y="0" width={slot.w} height={agg ? 96 : 88} rx="12" />
            <circle cx="20" cy="22" r="4" fill={LANE_INKS[Math.max(0, zoneIndex(c)) % LANE_INKS.length]} />
            <text x="32" y="27" class="n-name cluster-name">{c}</text>
            {#each strandsFor.slice(0, 2) as s (s.key)}
              {@const si = strandsFor.indexOf(s)}
              <text x="20" y={52 + si * 20} class="chiprow" fill={s.outcome === 'accepted' ? 'var(--accept)' : 'var(--alarm)'}>
                {s.outcome === 'accepted' ? (s.direction === 'out' ? '✓→' : '✓→in') : '⊣'}
                {s.peers.slice(0, 2).join(' · ')} {portsLine(s.ports)} · {s.count}×
              </text>
            {/each}
            {#if agg && cZone}
              {@render aggregateBar(agg, 10, slot.w - 20, 80, 12, cZone)}
            {/if}
          </g>
        {/each}

        {#if reachHasInternet}
          <!-- The internet's own band along the foot. Its peers and
               ports used to be printed along it; round 49 supersedes
               "ports written along a road" on both surfaces, so the band
               is named and nothing else -- the internet strand's own
               card carries what was on it. -->
          <path d="M 180 574 C 460 622, 940 622, 1220 574" fill="none" stroke="var(--hair-2)" stroke-width="1.1" />
          <text x="1250" y="580" class="n-sub">INTERNET</text>
        {/if}

        {#if reachSummary.strands.length === 0}
          <!-- #626's honest empty: never a spinner standing in for an answer. -->
          <text x={MX} y={MY + 80} text-anchor="middle" class="n-sub">nothing observed this window</text>
        {/if}
      </svg>
    </div>
  {/if}

  <!-- The line card (round 49's `flat-reach`, DESIGN.md "Cards": "Line
       card in the reach: the port / proto / accepted / dropped table,
       the totals, `:22 refused by #17 default drop`, and on a refused
       strand the composer's `draft the rule ▸`").

       This is where everything that used to be printed on the strands
       went. Its reading is `reachLineSummary`, so the ports, the split
       and the refusing rule are one shared implementation with the
       city's own line card rather than a second one drawn from the same
       events. Placed by lib/cardAnchor like every other card here. -->
  {#if reach && lineCard && lineCardSummary}
    {#if lineCardPlace}
      <svg class="leader" aria-hidden="true">
        <path
          d="M{lineCardPlace.from.x} {lineCardPlace.from.y}L{lineCardPlace.to.x} {lineCardPlace.to.y}"
          stroke="var(--hair-2)"
          stroke-width="1"
          fill="none"
        />
        <circle cx={lineCardPlace.from.x} cy={lineCardPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="card line-card"
      class:pinned={lineCardPinned}
      class:placed={lineCardPlace !== null}
      style={lineCardPlace ? `left:${R2(lineCardPlace.left)}px;top:${R2(lineCardPlace.top)}px` : undefined}
      bind:this={lineCardEl}
      role="dialog"
      tabindex="-1"
      aria-label="{reachSideName} → {counterpartName(lineCard.counterpart)}: the ports on this line"
      onpointerenter={lineGrace.hold}
      onpointerleave={releaseLineCard}
    >
      <div class="t">
        <span class="n"
          >{reachSideName} → {counterpartName(lineCard.counterpart)}{#if lineCardCidr}<small>{lineCardCidr}</small>{/if}</span
        >
        <button
          class="pin"
          class:on={lineCardPinned}
          aria-pressed={lineCardPinned}
          title={lineCardPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={pinLineCard}
        >
          {lineCardPinned ? '✕' : '⊙'}
        </button>
      </div>

      {#if lineCardSummary.ports.length > 0}
        <table>
          <thead>
            <tr><th>PORT</th><th>PROTO</th><th class="n">ACCEPTED</th><th class="n">DROPPED</th></tr>
          </thead>
          <tbody>
            {#each lineCardSummary.ports as p (`${p.port}|${p.proto}`)}
              {@const fresh = lineCardNewPorts.has(p.port)}
              <tr class:lit={fresh} data-line-port={p.port}>
                <td
                  >{p.port}{#if fresh}<span class="newmark"> new</span>{/if}</td
                >
                <!-- '' where the events named no protocol: unknown, never
                     assumed TCP (lib/reach.ts says why). -->
                <td class:dim={p.proto === ''}>{p.proto === '' ? 'unnamed' : p.proto}</td>
                <td class="n" class:ok={p.accepted > 0} class:dim={p.accepted === 0}>{p.accepted > 0 ? p.accepted.toLocaleString() : '—'}</td>
                <td class="n" class:al={p.dropped > 0} class:dim={p.dropped === 0}>{p.dropped > 0 ? p.dropped.toLocaleString() : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <!-- Traffic with no destination port at all -- ICMP and friends.
             It still totals below; an empty table is not an empty line. -->
        <div class="s">no destination port was named on this line</div>
      {/if}

      <!-- The totals, and the same three numbers drawn as the picture
           the design asks for: tcp against udp, with everything that is
           neither in `other`, so the bar totals the line. -->
      <div class="s totals" data-line-totals>
        tcp <b>{lineCardSummary.tcp.toLocaleString()}</b> · udp <b>{lineCardSummary.udp.toLocaleString()}</b> · other
        <b>{lineCardSummary.other.toLocaleString()}</b>
      </div>
      {#if lineCardTotal > 0}
        <div class="protobar" aria-hidden="true">
          <i class="tcp" style:width="{(lineCardSummary.tcp / lineCardTotal) * 100}%"></i>
          <i class="udp" style:width="{(lineCardSummary.udp / lineCardTotal) * 100}%"></i>
          <i class="oth" style:width="{(lineCardSummary.other / lineCardTotal) * 100}%"></i>
        </div>
      {/if}

      {#if lineCardSummary.refusedBy}
        <!-- The refusing rule, named from the events themselves and
             never guessed at (#967). -->
        <div class="s al" data-refused-by>
          {lineCardRefusedPorts} refused by {lineCardSummary.refusedBy}
        </div>
      {:else if lineCardSummary.dropped > 0}
        <div class="s al">{lineCardRefusedPorts} refused — the drop named no rule</div>
      {/if}

      {#each lineCardOffLines as l (l.key)}
        <div class="s nb"><i class="sw nb"></i>{linePort(l)} first seen {lineFirstSeen(l)} · not on the baseline</div>
      {/each}

      <div class="acts">
        {#if lineCardRefused && reachIsHost}
          <!-- The composer's door, which the removed strand pill used to
               be. It drafts and never runs, the same invariant as
               before: mikroview observes, it never connects.
               Only on a host's reach: the line it prints names one
               machine as its source, and a zone's or a rib's refused
               line is a statement about many (#1016). -->
          <button
            class="hot"
            data-draft-rule
            onclick={() => {
              const s = lineCardRefused
              if (s) openCompose(s)
            }}>draft the rule ▸</button
          >
        {/if}
        <button onclick={openLineStream}>stream ▸</button>
      </div>
    </div>
  {/if}

  {#if reach && compose}
    <div class="composer" role="dialog" aria-label="Draft a rule for this strand">
      <div class="portpanel">
        <h4>
          {compose.direction === 'out'
            ? `What may ${reach.host} say to ${compose.counterpart === 'internet' ? 'the internet' : (counterpartZone?.name ?? compose.counterpart)}?`
            : `What may ${compose.counterpart === 'internet' ? 'the internet' : (counterpartZone?.name ?? compose.counterpart)} say to ${reach.host}?`}
        </h4>
        <p class="q">the ports it has been asking for come first — a denial becomes a rule in two clicks</p>
        {#each compose.portHits.slice(0, 3) as h (h.port)}
          <button
            class="pchip"
            class:sel={chosenPort?.port === h.port && !composeFree}
            onclick={() => {
              composePort = h.port
              composeFree = ''
            }}
          >
            <b>{h.proto}/{h.port}</b>
            <span class="why">it's been asking · {h.n}×</span>
          </button>
        {/each}
        <input class="pfree" placeholder="or type a port…" bind:value={composeFree} />
        {#if counterpartZone?.cidr}
          <div class="scope">
            to:
            <button class="opt" class:sel={composeScope === 'host'} onclick={() => (composeScope = 'host')}>
              just {composePeerName} · {composePeerAddr}
            </button>
            <button class="opt" class:sel={composeScope === 'subnet'} onclick={() => (composeScope = 'subnet')}>
              the whole subnet {counterpartZone.cidr}
            </button>
          </div>
        {/if}
        <p class="statef">replies ride back — stateful, no return rule needed.</p>
      </div>

      <div class="cmdcard">
        <div class="cmdtabs">
          <button class="ctab" class:sel={composeMode === 'allow'} onclick={() => (composeMode = 'allow')}>Allow it</button>
          <button class="ctab" class:sel={composeMode === 'block'} onclick={() => (composeMode = 'block')}>Name the block instead</button>
          <button class="copy" onclick={copyCommand}>{copied ? '✓ copied' : '⧉ copy'}</button>
        </div>
        {#if composedCommand}
          <pre class="cmd">{composedCommand}</pre>
          <p class="cmdnote">
            <b>Paste it in RouterOS yourself — mikroview never touches the router.</b>
            {#if composeMode === 'allow'}
              {composePlaceBefore ? `Placed before ${composePlaceBefore}, logged` : 'Logged'} and named, so the map
              learns it: on the next rule push this strand turns green and the unplanned stamp retires itself.
            {:else}
              The denial stays, but logged and named — the anonymous drop retires, and the dark boundary lights up.
            {/if}
          </p>
        {:else}
          <p class="cmdnote">pick a port — the command drafts itself from what was observed</p>
        {/if}
        <button class="composer-close" onclick={() => (compose = null)}>Close</button>
      </div>
    </div>
  {/if}

  {#if openHostDot && !reach}
    {@const d = openHostDot.dot}
    <!-- The host card (round 49's `tvQuiet`, and DESIGN.md "Cards"):
         presence, last and first seen, events, and the two marks. It is
         the same card furniture as the boundary's, on the same shared
         placement, because a host and a boundary are read the same way
         -- hover to open, pin to keep. -->
    {#if hostCardPlace}
      <svg class="leader" aria-hidden="true">
        <path
          d="M{hostCardPlace.from.x} {hostCardPlace.from.y}L{hostCardPlace.to.x} {hostCardPlace.to.y}"
          stroke="var(--hair-2)"
          stroke-width="1"
          fill="none"
        />
        <circle cx={hostCardPlace.from.x} cy={hostCardPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="card"
      class:pinned={hostCardPinned}
      class:placed={hostCardPlace !== null}
      style={hostCardPlace ? `left:${R2(hostCardPlace.left)}px;top:${R2(hostCardPlace.top)}px` : undefined}
      bind:this={hostCardEl}
      role="dialog"
      tabindex="-1"
      aria-label="The host {d.label}"
      onpointerenter={hostGrace.hold}
      onpointerleave={releaseHostCard}
    >
      <div class="t">
        <span class="n">{d.label}<small>{d.ip}</small></span>
        <button
          class="pin"
          class:on={hostCardPinned}
          aria-pressed={hostCardPinned}
          title={hostCardPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={pinHostCard}
        >
          {hostCardPinned ? '✕' : '⊙'}
        </button>
      </div>

      <!-- What the host is. Each line claims only "not heard": neither
           quiet nor quiet-on-purpose is a statement about the network. -->
      {#if d.presence === 'intended'}
        <div class="s qt"><i class="sw qt"></i>quiet on purpose</div>
        {#if d.host?.mark?.reason}
          <div class="quote">{d.host.mark.reason}</div>
          <div class="s">{d.host.mark.by} · {new Date(d.host.mark.at).toLocaleString()}</div>
        {/if}
      {:else if d.presence === 'quiet'}
        <div class="s dk">
          <i class="sw dk"></i>quiet · not heard for <b>{d.host ? quietFor(d.host.lastSeen, nowMs) : 'an unknown time'}</b> (window
          {spanLabel(quietAfterMs)})
        </div>
      {:else}
        <!-- A class, never an inline style: a `style` attribute is
             refused under this app's CSP in Firefox (#659). -->
        <div class="s"><i class="sw live"></i>live · heard from within the last {spanLabel(quietAfterMs)}</div>
      {/if}

      {#if d.host}
        <div class="s">
          last seen {formatRelative(d.host.lastSeen, nowMs)} · first seen {new Date(d.host.firstSeen).toLocaleDateString()} ·
          {d.host.events.toLocaleString()} events · {openHostDot.zone.name}
        </div>
      {:else}
        <div class="s">{openHostDot.zone.name} · seen in the live feed; the host register has not answered for it yet</div>
      {/if}
      {#if d.presence !== 'live'}
        <div class="s">comes back by itself when the feed hears it again</div>
      {/if}

      <!-- Why this dot is ringed. The lines themselves, and `expected ▸`,
           live on the rib's own card: every one of these lines crosses a
           boundary, so the roll-up that lit this dot lit that rib too,
           and the answer belongs where the pair is named. -->
      {#if hostOffBaseline(d.ip)}
        <div class="s nb">
          <i class="sw nb"></i><b>{offLinesForIp(d.ip).length} off the baseline today</b> — the lines are on the rib's card
        </div>
      {/if}

      {#if hostMarkOpen && isAdmin}
        <!-- The reason is the mark: internal/hosts keeps it as the thing
             that stays said, so there is nothing to write without it. -->
        <div class="form">
          <label for="{uid}-host-why">QUIET ON PURPOSE — WHY?</label>
          <input id="{uid}-host-why" bind:value={hostReason} placeholder="why this machine is expected to be silent…" />
          <div class="btns">
            <button class="go" disabled={hostBusy || !hostReason.trim()} onclick={submitHostMark}>Mark</button>
            <button class="no" onclick={() => (hostMarkOpen = false)}>cancel</button>
            <span class="who">as {authState.username}</span>
          </div>
        </div>
      {/if}

      {#if hostsState.error}
        <p class="d-error">{hostsState.error}</p>
      {/if}

      <div class="acts">
        {#if isAdmin && d.host}
          {#if d.host.mark}
            <button class="hot" disabled={hostBusy} onclick={unmarkHost}>unmark ▸</button>
          {:else if !hostMarkOpen}
            <button disabled={hostBusy} onclick={askHostMark}>mark quiet on purpose ▸</button>
          {/if}
          <button class="hot" disabled={hostBusy} onclick={dismissHost}>dismiss ▸</button>
        {/if}
        <!-- #410: the host click reaches the dossier. One hook, no state
             of its own -- lib/dossier.svelte.ts owns the card. -->
        <button class="dim" onclick={(e) => dossierState.open(d.ip, e.currentTarget)}>dossier ▸</button>
        <button class="dim" onclick={openStreamFromHost}>stream ▸</button>
        <button
          class="dim"
          onclick={() => {
            const open = openHostDot
            if (open) descendFromHost(open.zone.id, open.dot.label, open.dot.ip)
          }}>reach ▸</button
        >
      </div>
    </div>
  {/if}

  <!-- The decommission card (#460, round 55): the offer, the ghost or
       the straggler, in the one component the city draws too. -->
  {#if decommCard && !reach}
    <DecommissionCard
      bind:element={decommCardEl}
      kind={decommCard.kind}
      offer={decommCard.lane.offer}
      watch={decommCard.lane.watch}
      peerName={nameForIp(decommCard.lane.watch?.lastStraggler?.peer)}
      {nowMs}
      place={decommCardPlace}
      busy={ghostBusy}
      error={ghostError}
      onaccept={() => answerOffer(decommCard.lane, true)}
      ondismiss={() => answerOffer(decommCard.lane, false)}
      onforce={(why) => forceRemoveGhost(decommCard.lane, why)}
      onclose={() => {
        openStragglerKey = null
        openGhostKey = null
      }}
      ontrace={() => {
        const s = decommCard.lane.watch?.lastStraggler
        if (!s) return
        const ti = laneIndexForIp(s.peer)
        openTrace(
          { in: s.interface || undefined, out: ti !== null ? zones[ti].id : undefined, port: s.port, proto: s.protocol },
          { openList: true },
        )
      }}
      onwatchhost={() => {
        const s = decommCard.lane.watch?.lastStraggler
        if (!s) return
        topologyNavState.requestWatchDraft({
          who: s.address,
          toward: s.peer ? `${s.peer}${s.port ? `:${s.port}` : ''}` : undefined,
          mode: 'expect',
          provenance: `from a straggler on the retired range ${decommCard.lane.cidr}`,
        })
        appState.view = 'watchlist'
      }}
      onreach={() => {
        const s = decommCard.lane.watch?.lastStraggler
        if (s) descend(decommCard.lane.iface, s.name || s.address, s.address)
      }}
      onstream={() => {
        appState.resetFilters()
        appState.setFilter('srcQuery', decommCard.lane.cidr)
        appState.view = 'live'
      }}
      onwatchlist={() => {
        // #1069: land on the row this ghost's watch draws in the
        // watchlist, not just the tab -- the card's own copy already
        // promises that's where the watch lives on.
        const id = decommCard.lane.watch?.id
        if (id) topologyNavState.requestDecommissionWatch(id)
        appState.view = 'watchlist'
      }}
    />
  {/if}

  {#if boundaryCard && !reach && !hostCard}
    <!-- The boundary card (round 49, ported from round-49/index.html's
         `.card`: title row with the pin, `.s` fact lines each with the
         material's own swatch, an `.acts` row, and the form the pin
         opens). It is the one interaction on a boundary: what the rule
         does, what both directions are, and the three doors. The
         declare form is behind the pin because the card reads first and
         acts only once it is kept (#392 is still the record it writes:
         one acknowledgement, with its reason and its author). -->
    {#if cardPlace}
      <!-- The leader (round-49/index.html:1112): a hairline from the
           card to the boundary it is about, with an accent dot at the
           boundary's end. Without a viewBox an SVG's user units are its
           own CSS pixels, which is the space the card is placed in. -->
      <svg class="leader" aria-hidden="true">
        <path d="M{cardPlace.from.x} {cardPlace.from.y}L{cardPlace.to.x} {cardPlace.to.y}" stroke="var(--hair-2)" stroke-width="1" fill="none" />
        <circle cx={cardPlace.from.x} cy={cardPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="card"
      class:pinned={cardPinned}
      class:placed={cardPlace !== null}
      style={cardPlace ? `left:${R2(cardPlace.left)}px;top:${R2(cardPlace.top)}px` : undefined}
      bind:this={cardEl}
      role="dialog"
      tabindex="-1"
      aria-label="The {pairName(boundaryCard.from, boundaryCard.to)} boundary"
      onpointerenter={cardGrace.hold}
      onpointerleave={releaseBoundary}
    >
      <div class="t">
        <span class="n">{pairName(boundaryCard.from, boundaryCard.to)}<small>boundary</small></span>
        {#if isAdmin}
          <button
            class="pin"
            class:on={cardPinned}
            aria-pressed={cardPinned}
            title={cardPinned ? 'pinned — click to let it go' : 'pin this card'}
            onclick={pinBoundary}
          >
            {cardPinned ? '✕' : '⊙'}
          </button>
        {:else}
          <button class="pin" title="close this card" onclick={closeBoundary}>✕</button>
        {/if}
      </div>

      {#if cardCoverage === 'quiet' && cardDeclaration}
        <div class="s qt"><i class="sw qt"></i>quiet on purpose</div>
        <div class="quote">{cardDeclaration.reason}</div>
        <div class="s">{cardDeclaration.declaredBy} · {new Date(cardDeclaration.declaredAt).toLocaleString()}</div>
        <div class="s">{cardBackLine}</div>
      {:else}
        <div class="s dk"><i class="sw dk"></i>dark — nothing logs this boundary</div>
        <div class="s">{cardRuleLine}</div>
        <div class="s dk"><i class="sw dk"></i>{cardBackLine}</div>
        <div class="s">nothing drawn across it is a fact; nothing is known</div>
      {/if}

      {#if cardPinned && isAdmin && cardCoverage !== 'quiet'}
        <!-- The declare form: a reason, both directions, and who. Both
             directions is checked by default (round 49 item 7) because
             one direction declared and the other still dark leaves the
             boundary grey and this card explaining why.
             The city writes this out tag for tag (#1031) -- DESIGN.md
             "Cards" ratifies one interaction, the same on both surfaces,
             and it is the footer's shape a reader notices when the
             slider crosses. `type="button"` is said rather than assumed:
             a <button> with no type is a submit button, harmless here
             only because no <form> encloses it. -->
        <div class="form">
          <label for="{uid}-declare-why">QUIET ON PURPOSE — WHY?</label>
          <input id="{uid}-declare-why" bind:value={declareReason} placeholder="why this gap is intentional…" />
          <label class="both">
            <input type="checkbox" bind:checked={declareBoth} />
            both directions
          </label>
          {#if coverageState.error}
            <p class="d-error">{coverageState.error}</p>
          {/if}
          <div class="btns">
            <button type="button" class="go" disabled={declareBusy || !declareReason.trim()} onclick={submitDeclaration}>Declare</button>
            <button type="button" class="no" onclick={closeBoundary}>cancel</button>
            <span class="who">as {authState.username}</span>
          </div>
        </div>
      {/if}

      <div class="acts">
        {#if cardCoverage === 'quiet'}
          {#if isAdmin}
            <button class="hot" disabled={declareBusy} onclick={removeDeclaration}>undeclare ▸</button>
          {/if}
        {:else if isAdmin && !cardPinned}
          <button onclick={pinBoundary}>declare quiet on purpose ▸</button>
        {/if}
        <!-- Log every rule (#435): the other remedy for a dark pair --
             switch logging on for what actually crosses it, rather than
             declaring the silence a choice. -->
        {#if isAdmin}
          <button disabled={!primaryDevice} onclick={openLogEveryRuleFromDark}>rules ▸</button>
        {/if}
        <button class="dim" onclick={openStreamFromCard}>stream ▸</button>
      </div>
    </div>
  {/if}

  <!-- The reason form, written once and rendered wherever it was opened:
       under the line whose own `expected ▸` opened it, or under the bulk
       control. The reason is the statement -- the server refuses an
       empty one, and `expected` with nothing said for it would be a line
       quietly leaving the sieve with no record of who decided. Same
       words and same control order as the city road card, which renders
       the identical wording out of lib/city/expected. -->
  {#snippet expectedForm()}
    {@const scope = offScope}
    {#if scope}
      <div class="form">
        <label for="{uid}-expected-why">{EXPECTED_LABEL}</label>
        <input id="{uid}-expected-why" bind:value={offReason} placeholder={expectedPlaceholder(scope, offScopeLines.length)} />
        <div class="btns">
          <button class="go" disabled={offBusy || !offReason.trim()} onclick={submitExpected}>Expected</button>
          <button class="no" onclick={() => (offScope = null)}>cancel</button>
          <span class="who">{expectedWho(authState.username, scope, offScopeLines.length)}</span>
        </div>
        {#if baselineState.error}
          <p class="d-error">{baselineState.error}</p>
        {/if}
      </div>
    {/if}
  {/snippet}

  {#if openOffDrawn && offCardLines.length > 0 && !reach && !hostCard && !boundaryCard}
    <!-- The off-baseline card (round 49's `flat-new`, ported from
         round-49/index.html's `newLine`): the bright half hovered, and
         the rib rolled up. A thousand established lines in one dim word,
         the lines that are not on the pattern spelled out with when they
         were first seen and how often, what the router actually did, and
         the one way a line leaves the bright state early -- `expected`,
         a reason that stays said.

         Same furniture and the same shared placement as the two cards
         above it, because a rib is read the same way: hover to open,
         pin to keep, pin opens the form. -->
    {#if offCardPlace}
      <svg class="leader" aria-hidden="true">
        <path
          d="M{offCardPlace.from.x} {offCardPlace.from.y}L{offCardPlace.to.x} {offCardPlace.to.y}"
          stroke="var(--hair-2)"
          stroke-width="1"
          fill="none"
        />
        <circle cx={offCardPlace.from.x} cy={offCardPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="card off-card"
      class:pinned={offCardPinned}
      class:placed={offCardPlace !== null}
      style={offCardPlace ? `left:${R2(offCardPlace.left)}px;top:${R2(offCardPlace.top)}px` : undefined}
      bind:this={offCardEl}
      role="dialog"
      tabindex="-1"
      aria-label="Off the baseline on the {pairName(openOffDrawn.r.from, openOffDrawn.r.to)} rib"
      onpointerenter={offGrace.hold}
      onpointerleave={releaseOffCard}
    >
      <div class="t">
        <span class="n">{pairName(openOffDrawn.r.from, openOffDrawn.r.to)}<small>rib</small></span>
        {#if isAdmin}
          <button
            class="pin"
            class:on={offCardPinned}
            aria-pressed={offCardPinned}
            title={offCardPinned ? 'pinned — click to let it go' : 'pin this card'}
            onclick={pinOffCard}
          >
            {offCardPinned ? '✕' : '⊙'}
          </button>
        {:else}
          <button class="pin" title="close this card" onclick={closeOffCard}>✕</button>
        {/if}
      </div>

      <!-- The mockup says `established · 1,214 lines ...`. The count is
           left out here, deliberately: the register sends only today's
           off-baseline lines and never the established set, so any
           number in that slot would be invented (lib/baseline.ts says
           why the payload is built that way). The threshold itself is
           the server's own, carried on the document. -->
      <div class="s es">
        <i class="sw es"></i>established · the same routes and ports on {baselineState.off.config.days} of the last
        {baselineState.off.config.of} days
      </div>
      <div class="s nb"><i class="sw nb"></i><b>{offCardLines.length} off the baseline today</b></div>

      <table>
        <thead>
          <tr><th>LINE</th><th>PORT</th><th class="n">SEEN</th><th class="n">FIRST</th></tr>
        </thead>
        <tbody>
          {#each offCardLines as l (l.key)}
            <tr class="lit">
              <td>{addressLabel(l.srcIp)} → {addressLabel(l.dstIp)}</td>
              <td>{linePort(l)}</td>
              <td class="n" class:ok={l.outcome === 'accept'} class:al={l.outcome === 'drop'}>{l.count.toLocaleString()}</td>
              <td class="n">{lineFirstSeen(l)}</td>
            </tr>
            {#if offScope?.kind === 'one' && offScope.key === l.key}
              <tr class="formrow">
                <td colspan="4">{@render expectedForm()}</td>
              </tr>
            {:else if isAdmin}
              <!-- Per-line is the default: this button marks the line it
                   sits under and nothing else, so the ones above and
                   below it stay bright and the rib stays bright until
                   none of them is left (#1016, owner 2026-09-07). -->
              <tr class="actrow">
                <td colspan="4"
                  ><button type="button" class="linkact" data-expected-one={l.key} onclick={() => startExpectedOne(l.key)}>{EXPECTED_ONE_LABEL}</button
                  ></td
                >
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>

      <!-- Accepting the lot, off the table and clearly apart from the
           per-line buttons in it. It says its own count, so nobody can
           retire several lines by clicking what looked like one line's
           action. Never shown for a single line: that is what the button
           in the row already does. Same words and same order as the city
           road card, out of lib/city/expected. -->
      {#if isAdmin && offersExpectedAll(offCardLines.length)}
        {#if offScope?.kind === 'all'}
          <div class="allof">{@render expectedForm()}</div>
        {:else}
          <div class="allof">
            <button type="button" class="linkact all" disabled={offBusy} onclick={startExpectedAll}>{expectedAllLabel(offCardLines.length)}</button>
          </div>
        {/if}
      {/if}

      <div class="s">{offCardVerdict}</div>

      <div class="acts">
        {#if offCardReach}
          <button
            onclick={() => {
              const r = offCardReach
              closeOffCard()
              if (r) descend(r.zoneId, r.label, r.ip)
            }}>reach {offCardReach.label} ▸</button
          >
        {/if}
        <button class="dim" onclick={openStreamFromOffCard}>stream ▸</button>
      </div>
    </div>
  {/if}

  <!-- The altitude slider (#648, concept T; named ends #682; joined to
       the city #869): one quiet axis at the map's foot, its two
       extremes named ("clients" ... "street", the city's own far
       stop), a tiny symbol per stop between them -- the city, centred
       and the default, wears the diamond -- click anywhere on the line
       to jump. -->
  <div class="altitude">
    <span class="alt-end">clients</span>
    <span class="alt-track">
      <!-- A real range input: native click-anywhere-to-jump and arrow-key
           stepping for free, an implicit slider role, no hand-rolled a11y
           to get wrong. The stops are a purely decorative overlay -- the
           input underneath is what's operable. -->
      <input
        class="alt-range"
        type="range"
        min="0"
        max={ALTITUDE_LABELS.length - 1}
        step="1"
        value={altitude}
        oninput={onAltitudeInput}
        aria-label="Altitude: {ALTITUDE_LABELS.join(', ')}"
        aria-valuetext={ALTITUDE_LABELS[altitude]}
      />
      <div class="alt-ticks" aria-hidden="true">
        {#each ALTITUDE_LABELS as label (label)}
          <i class="tick" class:on={ALTITUDE_LABELS[altitude] === label} class:diamond={label === 'city'}></i>
        {/each}
      </div>
    </span>
    <span class="alt-end">street</span>
  </div>

  {#if nodeCard}
    <!-- The node info card (#648, rounds 22-23): a small glass card --
         name, address, lane, open warnings, actions -- anchored to the
         click, clamped inside the viewport. -->
    <div class="node-card" role="dialog" aria-label="{nodeCard.name}'s information" style:left="{nodeCard.x}px" style:top="{nodeCard.y}px">
      <button class="nc-close" onclick={closeNodeCard} aria-label="Close">✕</button>
      <b class="nc-name">{nodeCard.name}</b>
      {#if nodeCard.address}<span class="nc-addr">{nodeCard.address}</span>{/if}
      {#if nodeCard.lane}<p class="nc-lane">{nodeCard.lane}</p>{/if}
      {#if nodeCard.flagCount > 0}
        <p class="nc-warn">✱ {nodeCard.flagCount} open flag{nodeCard.flagCount === 1 ? '' : 's'}</p>
      {/if}
      {#if nodeCard.watchCount > 0}
        <p class="nc-watch">◉ watched</p>
      {/if}
      <div class="nc-acts">
        {#if nodeCard.address}
          <button
            class="nc-act"
            onclick={() => {
              appState.resetFilters()
              appState.setFilter('srcQuery', nodeCard!.address!)
              appState.view = 'live'
              closeNodeCard()
            }}
          >
            open in stream ▸
          </button>
        {/if}
        {#if nodeCard.flagCount > 0}
          <button
            class="nc-act"
            onclick={() => {
              appState.view = 'flags'
              closeNodeCard()
            }}
          >
            flags ▸
          </button>
        {/if}
        {#if nodeCard.watchCount > 0}
          <button
            class="nc-act"
            onclick={() => {
              appState.view = 'watchlist'
              closeNodeCard()
            }}
          >
            watch ▸
          </button>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .topo {
    flex: 1;
    min-height: 0;
    position: relative;
    display: flex;
    flex-direction: column;
  }

  .crumb {
    position: absolute;
    top: 14px;
    left: 24px;
    z-index: 2;
  }

  /* `name · ip · reaches N · reached by N · refused N · Esc surfaces ▸`
     -- the city's own crumb, one row, with the counts sized down from
     the name so the thing stood on still reads first. */
  .crumb .path {
    display: flex;
    flex-wrap: wrap;
    gap: 11px;
    align-items: baseline;
    font-size: 18px;
    font-weight: 550;
    letter-spacing: -0.01em;
    color: var(--fg);
  }

  .crumb .path > span:not(.here) {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--fg-dim);
  }

  .crumb .path > span > b {
    color: var(--fg-muted);
    font-weight: 550;
  }

  .crumb .path .ip {
    color: var(--fg-muted);
  }

  .crumb .path span.alarm,
  .crumb .path span.alarm > b {
    color: var(--alarm);
  }

  /* The city's own divider: a hairline rule, not a printed separator. */
  .crumb i.bar {
    width: 1px;
    height: 12px;
    background: var(--hair-2);
    align-self: center;
  }

  .crumb .esc {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 400;
    color: var(--accent);
  }

  .crumb .here {
    color: var(--accent);
  }

  .crumb .sub {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 3px;
  }

  .crumb .sub b {
    color: var(--fg-muted);
    font-weight: 550;
  }

  /* What the lens row is now: the off-baseline tally alone. The two
     overlay pills that stood beside it went with #981 -- the marks are
     drawn by the data, not by a switch. */
  .pills {
    position: absolute;
    bottom: 12px;
    left: 26px;
    z-index: 2;
    display: flex;
    gap: 8px;
    /* The row stops short of the altitude slider (#1136). Both are
       absolute on the same bottom line, and the slider is centred on
       the stage and about 274px wide (CLIENTS + track + STREET), so
       half of that plus a gutter is what this row may not have. Open,
       the picker had grown clean across it: the chip bar reached the
       same width at every viewport, so at 1100 it covered the slider
       and at 1920 the off-baseline tally printed over CLIENTS.
       Wrapping rather than clipping keeps that tally readable when the
       bar takes the whole line -- the row is bottom-anchored, so a
       second line grows upward, over the map and not off it. */
    flex-wrap: wrap;
    max-width: calc(50% - 190px);
  }

  /* The off-baseline mark (round-49/index.html:76-77's `.nmk`): the
     sieve's own tally, in the accept ink, with `today` dropped back
     because the count is the fact and the day is the qualifier. */
  .nmk {
    display: inline-flex;
    align-items: center;
    padding: 3px 2px;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.04em;
    color: var(--accept);
  }

  .nmk b {
    margin: 0 0.35em;
    font-weight: 400;
    color: var(--fg-dim);
  }

  .stage {
    position: relative;
    flex: 1;
    min-height: 0;
  }

  .stage svg {
    width: 100%;
    height: 100%;
    display: block;
  }

  .stage svg text {
    font-family: inherit;
  }

  /* The map is dragged to pan (#890 item 4). `touch-action: none` so a
     drag on a touch screen pans the map rather than scrolling the deck
     out from under it. */
  .stage svg.pannable {
    cursor: grab;
    touch-action: none;
  }

  .stage svg.pannable:active {
    cursor: grabbing;
  }

  /* The fit chip (round 51/52): bottom right of the stage, clear of the
     legend row. Dim until the view has been moved by hand, when the
     accent says there is somewhere to come back from. */
  .fitchip {
    position: absolute;
    right: 16px;
    bottom: 12px;
    z-index: 3;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 4px 9px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg-elevated);
    font: 500 10.5px var(--font-mono);
    color: var(--fg-muted);
    cursor: pointer;
  }

  .fitchip .z {
    color: var(--fg);
  }

  .fitchip .sep {
    width: 1px;
    height: 9px;
    background: var(--border);
  }

  .fitchip .dimt {
    color: var(--fg-dim);
  }

  .fitchip.hand {
    border-color: var(--accent);
    color: var(--fg);
  }

  /* A tunnel not heard for a while (#890): the footprint dashed in the
     dark ink, and the span at the card's right edge -- the same
     grammar a dark boundary and a quiet host already use. */
  .isl.quiet-print {
    stroke: var(--fg-muted);
    stroke-dasharray: 3 6;
  }

  .n-quiet {
    fill: var(--fg-muted);
    font-size: 9px;
    font-family: var(--font-mono);
  }

  .rib.tunnel-quiet {
    stroke-dasharray: 3 6;
  }

  .n-name {
    fill: var(--fg);
    font-size: 15px;
    font-weight: 600;
  }

  .n-sub {
    /* fg-dim on this scene's two grounds (--bg and --bg-elevated) reads
       at ~3.1-3.3:1 -- under the 4.5:1 small-text floor, i.e. dark text
       on a dark fill (#715). fg-muted clears both (~7.4:1 / ~8:1). */
    fill: var(--fg-muted);
    font-size: 9.5px;
  }

  /* The one line left on a lane card that is not name, subnet or hosts:
     a table exists nowhere, which is a different fact from dark and so
     cannot be drawn by the material (round 49, #865's wording). Dim,
     because it is a state of our own knowledge, not of the network. */
  .n-sub.no-table {
    font-family: var(--font-mono);
    letter-spacing: 0.04em;
    opacity: 0.85;
  }

  .n-hosts {
    fill: var(--fg-muted);
    font-size: 10px;
  }

  /* ---- the host dot row (#1016, round-49/index.html's node2) ---- */

  .hot {
    cursor: pointer;
  }

  .hot:hover .h-dot,
  .hot:focus-visible .h-dot {
    filter: brightness(1.3);
  }

  .hot:focus-visible {
    outline: none;
  }

  /* Live wears the lane's own ink, set as a `fill` attribute the way the
     card's accent dot already is. Quiet and quiet-on-purpose are states
     the row itself decides, so they are classes: grey for "not heard",
     white translucent for "the operator said so". Neither is a claim
     about the network beyond that. */
  .h-dot.quiet {
    fill: var(--fg-dim);
    fill-opacity: 0.75;
  }

  .h-dot.intended {
    fill: var(--fg);
    fill-opacity: 0.55;
  }

  /* The quiet host's dashed footprint: it keeps its place on the map. */
  .h-foot {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1;
    stroke-opacity: 0.6;
    stroke-dasharray: 2 2.5;
  }

  /* Flagged: a halo that hugs the dot. It throbs in place -- opacity and
     stroke weight only, never the radius -- because a ring that travels
     outward reads as something moving through the network, and nothing
     here moved (owner, 2026-09-07). */
  .h-halo {
    fill: none;
    stroke: var(--alarm);
    animation: h-halo 1.6s ease-in-out infinite;
  }

  @keyframes h-halo {
    0%,
    100% {
      stroke-opacity: 0.45;
      stroke-width: 1.2;
    }
    50% {
      stroke-opacity: 1;
      stroke-width: 2;
    }
  }

  /* Watched: this screen's own watcher ink, the same one the aggregate
     bar and the dials use. */
  .h-watch {
    fill: none;
    stroke: var(--marked);
    stroke-width: 1.1;
    stroke-opacity: 0.9;
  }

  /* Whose card is open. Dashed and in the accent, so it reads as the
     pointer's own mark rather than as anything about the host. */
  .h-open {
    fill: none;
    stroke: var(--accent);
    stroke-width: 1;
    stroke-dasharray: 2 3;
  }

  .hosttally {
    font-size: 10px;
  }

  /* The live swatch on the host card, beside `.sw.dk` and `.sw.qt`. */
  .card .sw.live {
    background: var(--accept);
  }

  .n-cidr {
    fill: var(--fg-muted);
    font-size: 10px;
    font-family: var(--font-mono);
  }

  .n-cidr.small {
    font-size: 9.5px;
  }

  /* The tunnel node's name, a size down from Internet's as round 30
     draws it (the-whole.html:989). A class rather than the mockup's
     inline style: a static `style` attribute is refused under this
     app's CSP in Firefox, which is how #659 shipped past every check
     and broke on the owner's screen. */
  .tn-name {
    font-size: 13px;
  }

  /* No pushed address row for the tunnel. Distinct from `.cidr-deg`,
     which is the whole-map toggle for "no push at all" -- this is one
     interface missing from a table that did arrive. */
  .cidr-none {
    font-style: italic;
  }

  .cluster-name {
    font-size: 13.5px;
  }

  .isl {
    fill: var(--bg-elevated);
    stroke: var(--border);
    stroke-width: 1;
  }

  .isl.waist {
    stroke: var(--hair-2);
  }

  .isl.ghost {
    fill: transparent;
    stroke-dasharray: 4 6;
  }

  .passive {
    pointer-events: none;
  }

  .zone {
    cursor: pointer;
  }

  .zone:hover .isl,
  .zone:focus-visible .isl {
    stroke: var(--accent);
  }

  .zone:focus-visible {
    outline: none;
  }

  .rib {
    fill: none;
    stroke-linecap: round;
    opacity: 0.55;
  }

  /* --- the ghost lane (#460, round 55) ----------------------------------- */

  /* The rib to a ghost: still there, no longer carried. Dashed and in
     the watch's own ink, drawn at the material's weight rather than a
     live lane's 2.4px -- a segment nothing is routing is not a volume. */
  .grib {
    opacity: 1;
    stroke-width: 1.6;
    stroke-dasharray: 3 6;
  }

  /* The ghost's plate: round 30's lane card, hollowed. No fill of its
     own beyond the island's, a dashed border in the state's ink, and a
     glow only when a straggler has broken it -- the one moment on this
     lane that is asking for something. */
  .gisl {
    fill-opacity: 0.35;
    stroke-dasharray: 4 5;
  }

  .ghost-lane.broken .gisl {
    filter: drop-shadow(0 0 6px rgba(255, 84, 112, 0.35));
  }

  /* The lane dot is a ring, not a disc: the lane is a place that was,
     not a place that is. */
  .gdot {
    fill: none;
    stroke-width: 1.2;
  }

  /* The name reads a step back from a live lane's, because it is the
     name the range had rather than one anything answers to now. */
  .gname {
    fill: var(--fg-muted);
  }

  .gsub {
    font-size: 10px;
  }

  /* A last-known host: the quiet material, dashed tighter and faded. It
     is a name the range last had, not a machine anyone can see. */
  .ghost-host {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1.1;
    stroke-dasharray: 2 2.5;
    opacity: 0.7;
  }

  .ghost-halo {
    fill: none;
    stroke: var(--alarm);
  }

  /* The straggler's line: the same arc an unplanned pair takes, in the
     same reserved saturated colour, because it is the same kind of
     statement -- traffic the map had no reason to expect. */
  .straggler-rib {
    opacity: 1;
    stroke: var(--alarm);
    stroke-width: 2;
  }

  .straggler-call {
    cursor: pointer;
  }

  /* --- the shared edge chrome (#628) ------------------------------------- */
  /* The invisible hit area a 1.8px line cannot be. */
  .edge-hit {
    fill: none;
    stroke: transparent;
    stroke-width: 14;
  }

  .edge-g {
    cursor: pointer;
  }

  .edge-g:focus-visible {
    outline: none;
  }

  .edge-bar {
    stroke: var(--fg-muted);
    stroke-width: 2.6;
  }

  /* --- the reality overlay (#629), in the verdict's ink (round 49) -------- */
  /* A logged direction's own half: green where anything was accepted at
     .55, red where the boundary only ever dropped at .7 -- the round-49
     rule table's two opacities for a rib half. */
  .redge {
    fill: none;
    stroke: var(--fg-muted);
    stroke-linecap: round;
    opacity: 0.55;
  }

  .redge.dropped {
    opacity: 0.7;
  }

  /* Brightness is the baseline (round 49, #1016), at the mockup's own
     two opacities (round-49/index.html:1201). Established recedes;
     off-baseline comes forward. Neither is ever removed -- this is a
     sieve, not a filter. */
  .redge.established {
    opacity: 0.26;
  }

  .redge.offbase {
    opacity: 0.85;
  }

  /* The rib whose off-baseline card is open, marked as the subject the
     card is about -- the same mark the boundary card leaves on its own
     half (`.cov-g.on .cedge` below). Without it the card floats beside a
     rib indistinguishable from its neighbours. */
  .edge-g.on .redge {
    opacity: 1;
  }

  /* The flow: dashes travelling the way the traffic ran, over an
     off-baseline line only (round-49/index.html:163). An established
     line has no flow at all -- that is most of what makes the map calm. */
  .flow {
    fill: none;
    stroke-linecap: round;
    stroke-dasharray: 7 11;
    stroke-opacity: 0.9;
    animation: flow 1.5s linear infinite;
    pointer-events: none;
  }

  @keyframes flow {
    to {
      stroke-dashoffset: -18;
    }
  }

  /* The ring where an off-baseline line arrived. It hugs the shape and
     throbs in place -- opacity and stroke weight only, never the radius
     -- for the same reason the flag halo does: a ring that travels
     outward reads as something moving through the network, and nothing
     here moved (owner, 2026-09-07). It borrows the halo's own keyframes
     so the two can never drift apart. */
  .nb-ring,
  .h-nb {
    fill: none;
    stroke: var(--accept);
    animation: h-halo 1.6s ease-in-out infinite;
    pointer-events: none;
  }

  /* The escalated unplanned pair: undivided, alarm, and glowing, as
     round 30 draws it and round 49 keeps it. */
  .redge.alarm {
    stroke: var(--alarm);
    opacity: 0.85;
    filter: drop-shadow(0 0 6px rgba(255, 84, 112, 0.45));
  }

  .edge-bar.alarm-bar {
    stroke: var(--alarm);
  }

  /* Intent nothing arrived to fill: fainter than any observed line. */
  .gedge {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1.1;
    stroke-dasharray: 2 7;
    stroke-linecap: round;
    opacity: 0.55;
  }

  .ghost-t {
    opacity: 0.75;
    font-style: italic;
  }

  /* --- coverage is the material (#630, #392; round 49) -------------------- */
  /* The two treatments the rule table names, at the mockup's own inks
     and opacities (round-49/index.html:1199-1200). `--fg` is the page
     ink the mockup calls `--quiet` and `--fg-dim` the third ink it
     calls `--dark` -- the same two hexes, already tokens here. */
  .cedge {
    fill: none;
    stroke-linecap: round;
  }

  /* Dark: nothing logs this boundary-direction. */
  .cedge.dark {
    stroke: var(--fg-dim);
    stroke-width: 1.7;
    stroke-dasharray: 3 6;
    opacity: 0.5;
  }

  /* Quiet on purpose: declared, with a reason in its card. */
  .cedge.quiet {
    stroke: var(--fg);
    stroke-width: 1.7;
    opacity: 0.3;
  }

  .n-cov {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
  }

  .n-cov.cov-q {
    /* fg-dim on the card's --bg-elevated reads ~3.1:1 (#715); fg-muted
       clears the 4.5:1 floor at ~7.4:1. */
    fill: var(--fg-muted);
  }

  .n-cov.cov-d {
    fill: var(--alarm);
  }

  .cov-g.actionable {
    cursor: pointer;
  }

  .cov-g.actionable:hover .cedge,
  .cov-g.actionable:focus-visible .cedge {
    opacity: 1;
  }

  /* The open boundary stays marked for as long as its card is open,
     pinned or not (DESIGN.md "Cards"). Ten grey dashed boundaries can
     be on screen at once, and a card naming two of them in words alone
     does not say which -- the leader points at one end, and this accent
     glow says the rib half it points at is the subject. It traces the
     material rather than recolouring it, because the colour is the
     coverage and would be a different statement. */
  .cov-g.on .cedge {
    opacity: 1;
    filter: drop-shadow(0 0 3px var(--accent));
  }

  .cov-g:focus-visible {
    outline: none;
  }

  /* --- the card (round 49, ported from round-49/index.html:174-230) ------
     One card for anything you can point at; the boundary is the first
     of them. Round 40's hovercard grown a title row, a swatch per fact,
     an actions foot and the form the pin opens. It sits where the
     declare panel sat -- the map's bottom-left, clear of the lanes --
     rather than floating on a leader from the rib: the leader is the
     mockup's, and placing one needs the stage's own pixel geometry,
     which this SVG (viewBox, xMidYMid meet) does not hand out. */
  /* The leader, under the card it joins. It covers the whole view and
     takes no pointer, so it can never come between the pointer and
     either end of the journey it is drawing (#1027). */
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
    /* Where the card sits before it has been placed, and wherever there
       is nothing to measure. Once `placed` lands, left/top come from
       lib/cardAnchor.ts instead. */
    left: 24px;
    bottom: 34px;
    z-index: 3;
    width: 288px;
    padding: 9px 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
    font: 10.5px var(--font-mono);
    color: var(--fg-muted);
  }

  /* Placed, the card is positioned from its own top-left, so the corner
     anchoring above has to be released. */
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

  .card .pin.on,
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

  .card .s.qt {
    color: var(--fg);
    opacity: 0.7;
  }

  /* The material's own swatch, so a line about a boundary is read in
     the same ink the boundary is drawn in. */
  .card .sw {
    display: inline-block;
    width: 22px;
    height: 3px;
    border-radius: 2px;
    vertical-align: middle;
    margin-right: 6px;
  }

  .card .sw.dk {
    background: repeating-linear-gradient(90deg, var(--fg-dim) 0 3px, transparent 3px 6px);
  }

  .card .sw.qt {
    background: var(--fg);
    opacity: 0.4;
  }

  /* The two baseline swatches, so a line about brightness is read in the
     ink the map draws that brightness in (round-49/index.html:199-200):
     established a thin dim rule, off-baseline the same green lit. */
  .card .sw.es {
    height: 2px;
    background: var(--accept);
    opacity: 0.3;
  }

  .card .sw.nb {
    background: var(--accept);
    box-shadow: 0 0 0 2px rgba(62, 207, 126, 0.3);
  }

  .card .s.es {
    color: var(--fg-dim);
  }

  .card .s.nb {
    color: var(--fg);
  }

  /* The card's own table (round-49/index.html:209-213): the off-baseline
     lines, and the reach's port table before it. */
  .card table {
    width: 100%;
    margin-top: 6px;
    border-collapse: collapse;
    font: 10.5px var(--font-mono);
  }

  .card th {
    padding: 0 0 3px;
    border-bottom: 1px solid var(--hair-2);
    font: 600 9px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-dim);
    text-align: left;
  }

  .card td {
    padding: 2px 0;
    color: var(--fg-muted);
  }

  /* The line itself reads at full strength: it is the one thing on this
     card that is not background. */
  .card tr.lit td {
    color: var(--fg);
  }

  .card th.n,
  .card td.n {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .card td.ok {
    color: var(--accept);
  }

  .card td.al {
    color: var(--alarm);
  }

  /* A dash rather than a zero: nothing was dropped here, which is a
     different statement from "0 was dropped here". */
  .card td.dim {
    color: var(--fg-dim);
  }

  /* ---- the reach's line card (round 49's `flat-reach`) ---- */

  /* Wider than the standard card: it carries a four-column table, and
     round 49's own line card is 292px against the 264px default
     (round-49/index.html:1138). */
  .line-card {
    width: 292px;
  }

  /* The port that is off the baseline today, marked in the table the way
     the mockup marks it -- the row itself already reads at full strength
     through `tr.lit`. */
  .line-card .newmark {
    color: var(--accept);
    font-weight: 600;
  }

  .line-card .totals {
    margin-top: 6px;
  }

  /* The tcp-versus-udp picture: the totals line drawn as one bar, so the
     split reads at a glance rather than being three numbers to compare.
     The three widths total the line by construction. */
  .line-card .protobar {
    display: flex;
    height: 3px;
    margin-top: 5px;
    border-radius: 2px;
    overflow: hidden;
    background: var(--hair);
  }

  .line-card .protobar i {
    display: block;
    height: 100%;
  }

  .line-card .protobar i.tcp {
    background: var(--accept);
    opacity: 0.85;
  }

  .line-card .protobar i.udp {
    background: var(--nat, var(--accent));
    opacity: 0.8;
  }

  .line-card .protobar i.oth {
    background: var(--fg-dim);
    opacity: 0.6;
  }

  /* The per-line `expected ▸` and the form it opens, each under the line
     it is about. Same shape as the city road card's own rows, so a line
     reads the same on either side of the altitude slider (#1016). */
  .card tr.actrow td,
  .card tr.formrow td {
    padding: 2px 0 8px;
  }

  .card .linkact {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: 10px var(--font-sans);
    cursor: pointer;
  }

  .card .linkact:hover {
    text-decoration: underline;
  }

  /* The bulk control sits below the table, ruled off from it, so it
     reads as being about the whole list rather than about whichever row
     it happens to sit under. Dim rather than accent: it is the
     deliberate one, never the one the eye lands on first. */
  .card .allof {
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--hair-2);
  }

  .card .linkact.all {
    color: var(--fg-dim);
  }

  .card .linkact.all:hover {
    color: var(--accent);
  }

  .card .linkact:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .card .quote {
    margin-top: 5px;
    padding: 5px 8px;
    border-left: 2px solid var(--hair-2);
    color: var(--fg);
    font: italic 11px var(--font-sans);
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

  .card .acts button:disabled {
    opacity: 0.5;
    cursor: default;
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

  .card .form label.both {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 7px;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
  }

  .card .form label.both input {
    width: auto;
    accent-color: var(--accent);
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

  /* Kept in step with the city's `.bcard .form` block, property for
     property (#1031): one interaction, the same on both surfaces, and
     the footer is where the two had drifted. `:not(:disabled)` because
     Declare is refused until there is a reason, and a refused button
     lighting up under the pointer offers something it will not do. */
  .card .form .go:hover:not(:disabled) {
    border-color: var(--accent);
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
    color: var(--reject);
  }

  /* --- the composer (round 2 scene 4) ------------------------------------ */
  .composer {
    position: absolute;
    right: 24px;
    top: 96px;
    z-index: 4;
    width: 330px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: calc(100% - 130px);
    overflow-y: auto;
  }

  .portpanel,
  .cmdcard {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .portpanel h4 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .q,
  .statef {
    margin: 0;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .pchip {
    display: flex;
    align-items: baseline;
    gap: 8px;
    text-align: left;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 7px 10px;
    font-size: 12px;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .pchip b {
    font-family: var(--font-mono);
    color: var(--fg);
    font-weight: 600;
  }

  .pchip .why {
    margin-left: auto;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .pchip.sel {
    border-color: var(--accent);
  }

  .pfree {
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 7px 10px;
    font-size: 12px;
    color: var(--fg);
  }

  .pfree:focus {
    outline: none;
    border-color: var(--accent);
  }

  .scope {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .opt {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 3px 10px;
    font-size: 11px;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .opt.sel {
    border-color: var(--accent);
    color: var(--fg);
  }

  .cmdtabs {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .ctab {
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 4px 10px;
    font-size: 11.5px;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .ctab.sel {
    border-color: var(--accent);
    color: var(--fg);
    font-weight: 600;
  }

  .copy {
    margin-left: auto;
    background: transparent;
    border: none;
    color: var(--fg-dim);
    font-size: 11px;
    cursor: pointer;
  }

  .copy:hover {
    color: var(--fg);
  }

  .cmd {
    margin: 0;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.55;
    color: var(--fg);
    white-space: pre-wrap;
    word-break: break-all;
    user-select: all;
  }

  .cmdnote {
    margin: 0;
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--fg-dim);
  }

  .cmdnote b {
    color: var(--fg-muted);
  }

  .composer-close {
    align-self: flex-end;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 7px;
    padding: 4px 12px;
    font-size: 11px;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .composer-close:hover {
    color: var(--fg);
  }

  /* The plate every edge label sits on (#699, round 30's ratified rule:
     nothing sits on a line as text only, without a box). #682's
     backdrop-coloured halo holds over empty map but not over a bright
     edge, where the label reads as ink scattered across the line.
     Round 30's own plate is near-opaque and matches this scene deliberately
     for that reason -- but a fill identical to --bg is a *different*
     thing: it is not near-opaque, it is the void itself, so the plate
     gives the eye nothing to land on and the (already dark) fill-behind
     line reads straight through. The owner overruled fidelity here
     (2026-08-31, #715 follow-up): --bg-elevated is opaque -- no line can
     show through it, coloured or not -- and pairs with --hair-2 for the
     hairline, the app's own pairing for "an elevated surface over the
     page" (see app.css). Measured var(--fg) on var(--bg-elevated):
     ~15.8:1, comfortably past the 4.5:1 floor. */
  .edge-plate {
    fill: var(--bg-elevated);
    stroke: var(--hair-2);
    stroke-width: 1;
  }

  .edge-badge {
    /* The figures are what the plate exists to say, so they carry the
       brightest ink on it -- --fg, not --fg-muted (#715's own fix, which
       cleared contrast but was never meant to be the final word once the
       plate itself became legible). */
    fill: var(--fg);
    font-family: var(--font-mono);
    font-size: 9.5px;
  }

  .edge-g:hover .edge-badge {
    fill: var(--accent);
  }

  .mote {
    opacity: 0.9;
    offset-path: path('M700 104 V 232');
    animation: travel 1.8s linear infinite;
  }

  @keyframes travel {
    from {
      offset-distance: 0%;
    }
    to {
      offset-distance: 100%;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .mote {
      display: none;
    }

    /* Instant under reduced motion (DESIGN.md "Honesty and motion"): the
       halo still marks the flagged host, it just stops throbbing. */
    .h-halo,
    .nb-ring,
    .h-nb {
      animation: none;
      stroke-opacity: 1;
      stroke-width: 1.6;
    }

    /* The same rule for the flow: the off-baseline line stays at full
       width and full brightness, it just stops moving, so nothing the
       map was saying is lost (round-49/index.html:256-257). */
    .flow {
      animation: none;
      stroke-dasharray: none;
    }
  }

  /* --- the reach ----------------------------------------------------------- */
  /* Round 24: the backdrop is the map at the level you left, blurred,
     at 0.38 -- readable as the place beneath, never mistakable for the
     live layer. */
  .stage.backdrop {
    filter: blur(7px);
    opacity: 0.38;
    transition:
      filter 0.25s,
      opacity 0.25s;
    cursor: pointer;
  }

  .membrane-layer {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }

  .membrane-layer svg {
    width: 100%;
    height: 100%;
    display: block;
  }

  .membrane {
    fill: color-mix(in srgb, var(--accent) 3%, transparent);
    stroke: var(--hair-2);
    stroke-width: 1.3;
  }

  .host-circle {
    fill: var(--bg-elevated);
    stroke-width: 1.5;
  }

  .sibling {
    pointer-events: auto;
    cursor: pointer;
  }

  .sibling-circle {
    fill: var(--bg-elevated);
    stroke-opacity: 0.35;
  }

  .sibling:hover .sibling-circle,
  .sibling:focus-visible .sibling-circle {
    stroke-opacity: 0.9;
  }

  .sibling:focus-visible {
    outline: none;
  }

  /* The strands, drawn to round 49's brightness table
     (round-49/README.md "Brightness (the baseline)"), the same three
     states and the same numbers as the ribs above: established .28 at
     0.6x width with no flow, off-baseline .85 at full width with flow
     and a ring, refused in alarm ink. Nothing is written on any of
     them. */
  .strand {
    fill: none;
    stroke-linecap: round;
    opacity: 0.8;
  }

  .strand.established {
    opacity: 0.28;
  }

  .strand.offbase {
    opacity: 0.85;
  }

  .strand.refused {
    opacity: 0.75;
    filter: drop-shadow(0 0 5px rgba(255, 84, 112, 0.4));
  }

  /* The pointer's target: a strand is drawn as thin as 1.3px when it is
     established, which is far too fine to hit. Invisible, wide, and
     under the drawn line. */
  .strand-hit {
    fill: none;
    stroke: transparent;
    stroke-width: 14px;
    stroke-linecap: round;
  }

  .strand-g {
    /* `.membrane-layer` is pointer-events: none so that clicking off
       anywhere surfaces, and everything in it that can be pointed at
       opts back in -- `.sibling`, `.host-node`, `.cluster-g`. The strand
       used to opt in through the `.strand-door` pill sitting on it;
       round 49 (#1016) moved the interaction onto the whole strand and
       deleted the pill, and the opt-in went with it. Every strand was
       then unhittable: no hover, no click, so no line card, and the
       composer behind that card could not be reached by pointer at all
       -- only by tabbing to the strand, because focus does not care
       about pointer-events. */
    pointer-events: auto;
    cursor: pointer;
  }

  .strand-g:hover .strand,
  .strand-g.on .strand {
    opacity: 1;
  }

  .strand-g:focus-visible {
    outline: none;
  }

  .strand-g:focus-visible .strand {
    opacity: 1;
    stroke-dasharray: none;
  }

  .strand-x {
    pointer-events: none;
  }

  .cluster {
    fill: var(--glass);
    stroke: var(--hair-2);
  }

  .chiprow {
    font-family: var(--font-mono);
    font-size: 10px;
  }

  /* Round 30 strokes this `var(--ink-2)` (the-whole.html's `.chip-t`);
     the port dropped the fill, and SVG's own default is black -- so the
     unplanned callout's second line, `caught by #17 default drop · 14×
     · open ▸`, has been drawn in black on a black map. Found while
     building #1018's own chip, which uses the same class. */
  .chip-t {
    font-family: var(--font-mono);
    font-size: 9px;
    fill: var(--fg-muted);
  }

  .alarm-t {
    fill: var(--alarm);
  }

  .ok-t {
    fill: var(--accept);
  }

  .crumb-link {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--fg);
    cursor: pointer;
  }

  .crumb-link:hover {
    color: var(--accent);
  }

  .crumb .alarm {
    color: var(--alarm);
  }

  /* Ported from the scene (#682): inside the map's own flow, top-left
     of the stage, a plain text link -- not a bordered pill floating
     over the whole card. */
  .ascend {
    position: absolute;
    top: 8px;
    left: 4px;
    z-index: 3;
    background: none;
    border: none;
    padding: 0;
    font: 500 11px var(--font-sans);
    color: var(--accent);
    cursor: pointer;
  }

  .ascend:hover {
    text-decoration: underline;
  }

  .host-link {
    cursor: pointer;
  }

  .host-link:hover,
  .host-link:focus-visible {
    fill: var(--accent);
  }

  /* The degraded map (#802). the-whole.html:121-122 draws the statement
     in the mono face at 10px in the quiet ink; --fg-muted rather than
     --fg-dim for the reason .n-sub gives above (contrast on this scene's
     two grounds). */
  .deg-t {
    fill: var(--fg-muted);
    font-size: 10px;
    font-family: var(--font-mono);
  }

  /* The way in, in the accent -- the one ability the statement carries.
     The waist card is `.passive` so policy edges beneath it stay
     clickable; this restores pointer events for the link alone. */
  .deg-go {
    fill: var(--accent);
    pointer-events: auto;
    cursor: pointer;
  }

  .deg-go:hover,
  .deg-go:focus-visible {
    text-decoration: underline;
  }

  /* Two sibling tspans, drawn once per address slot (the-whole.html:977,
     :1026): the pushed CIDR/address, and the never-pushed fallback.
     `.stage.map-degraded` (the-whole.html:118-125's `#s3.degraded`) toggles
     which one shows; italic marks the fallback apart from a real
     address (the-whole.html:125). */
  .cidr-deg {
    display: none;
  }
  .stage.map-degraded .cidr-v {
    display: none;
  }
  .stage.map-degraded .cidr-deg {
    display: initial;
    font-style: italic;
  }

  /* --- the health dials (#648, rounds 19-20; #699) ---------------------- */
  /* Round 30 hangs them in the stage's top-right corner (the-whole.html
     :486), not in the card's top-left flow, and draws them at their own
     56-unit geometry rather than half-size. The mockup's own `top: 108px`
     was measured from #s3's own top edge, where the scene bar is a
     `position: absolute` overlay and the stage itself is inset a further
     132px to clear it -- the 108px sits inside that reserved gutter, a
     modest step below the bar. This build's SceneBar is a normal flow
     sibling above `.topo` (Deck.svelte), so `.topo`'s own top edge
     already IS the bar's bottom edge -- carrying the mockup's raw number
     unadjusted stacked a second, much bigger gap on top of the first,
     landing the dials well down the card (owner, 2026-08-31: "well below
     it"). 14px -- this file's own close-to-an-edge rhythm, `.card-body`'s
     padding in Deck.svelte -- clears the bar without crowding it, #721's
     concern for any fixed chrome. */
  .dials {
    position: absolute;
    top: 14px;
    right: 26px;
    z-index: 6;
    display: flex;
    gap: 12px;
  }

  .dial {
    background: none;
    border: none;
    padding: 0;
    cursor: pointer;
    line-height: 0;
  }

  /* 68px, not the 56px #699 shipped: every round from 20 through 39's
     the-whole.html agrees on this figure (`.dial svg`), so 56px was a
     fidelity defect against the mockup's own long-standing value, not a
     judgement call (#743). The geometry is a 56-unit viewBox either way;
     this is presentation size only. */
  .dial svg {
    width: 68px;
    height: 68px;
    display: block;
  }

  .dring {
    fill: none;
    stroke-width: 4;
  }

  .dring.d-rest,
  .dring.d-healthy {
    stroke: var(--accept);
  }

  .dring.d-alarm,
  .dring.d-broken {
    stroke: var(--alarm);
  }

  .dnum {
    fill: var(--fg);
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 700;
  }

  .dsym {
    font-size: 10px;
  }

  .flag-sym {
    fill: var(--alarm);
  }

  /* The eye icon (#682) is a path + circle using currentColor, not
     text with its own fill -- color is what currentColor reads. */
  .watch-sym {
    color: var(--marked);
  }

  .dial:hover .dnum,
  .dial:focus-visible .dnum {
    fill: var(--accent);
  }

  .dial:focus-visible {
    outline: none;
  }

  /* The dial's own wrapper (#724) exists only so the panel can anchor
     "beneath itself" per-dial rather than under the whole `.dials` row,
     and so the click-away listener has one element per dial to test
     against. */
  .dial-wrap {
    position: relative;
  }

  /* The condensed panel sits over the map, so it needs an opaque plate
     -- the same fix the edge chips needed in #715 -- or the strands
     underneath read straight through it. --bg-elevated is that opaque
     ground everywhere else in this file already uses for the same
     reason (see the edge-plate rule above); .node-card's --glass +
     backdrop-filter is deliberately not reused here, since a blur still
     lets shapes and motion read through at the panel's edges. */
  .dial-panel {
    position: absolute;
    top: 100%;
    right: 0;
    margin-top: 6px;
    z-index: 7;
    min-width: 220px;
    max-width: 320px;
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border: 1px solid var(--hair-2);
    border-radius: 10px;
    box-shadow: 0 14px 36px rgba(0, 0, 0, 0.35);
    padding: 4px;
  }

  .dp-zero {
    margin: 0;
    padding: 10px 12px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  /* Rows are buttons, not the panel (Care, #724) -- a wrapping
     role/listbox semantic here would hide them from a screen reader. */
  .dp-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    width: 100%;
    background: none;
    border: none;
    border-radius: 6px;
    padding: 6px 8px;
    font-size: 11.5px;
    color: var(--fg);
    text-align: left;
    cursor: pointer;
  }

  .dp-row:hover {
    background: color-mix(in srgb, var(--fg) 8%, transparent);
  }

  /* Never `all: unset` -- this is the one focus ring convention every
     plain HTML control in this file (as opposed to the SVG-embedded
     dial/edge/host controls, which draw their own ink-based focus
     signal) is expected to carry. */
  .dp-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .dp-mark {
    font-size: 12px;
  }

  .dp-watch-mark {
    color: var(--marked);
  }

  .dp-watch-mark.broken {
    color: var(--alarm);
  }

  .dp-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dp-time,
  .dp-count,
  .dp-boundary,
  .dp-detail {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .dp-more {
    color: var(--fg-muted);
    justify-content: center;
  }

  /* --- the altitude's camera (#648, concept T) --------------------------- */
  .camera {
    transform-origin: 700px 310px;
    transition: transform 0.35s ease;
  }

  /* Depth adds layers; it never scales the same picture up (#699).
     scale(1.22) about (700,310) put the right-hand cards' edge at
     1717.8 against a 1586-wide stage, so the clients altitude clipped
     whatever it was meant to reveal. Round 30's stops instead reveal
     the services chips and then the client tier beneath each lane. */
  .camera .isl-card,
  .camera .ground-flat,
  .camera .detail,
  .camera .svc,
  .camera .cli,
  .camera .rib,
  .camera .rib-ghost,
  .camera .mote,
  .camera .edge-g,
  .camera .gedge {
    transition: opacity 0.55s ease;
  }

  .camera .svc,
  .camera .cli {
    opacity: 0;
    pointer-events: none;
  }

  .camera.cam-services .svc,
  .camera.cam-clients .svc {
    opacity: 1;
  }

  /* The client tier is the mockup's own `.c-dot`/`.c-label`/`.c-hit`
     (the-whole.html:2173, 2238): every one of them wired to open that
     host's own information, cursor:pointer included. The build carried
     the geometry over without the wiring or the reveal -- pointer-events
     stayed none even once opacity returned to 1, so a revealed client
     node was visible but inert, and a click meant for it fell through to
     whatever was behind (owner, #723: "goes to the stream instead").
     Restored here alongside real onclick/onkeydown handlers below. */
  .camera.cam-clients .cli {
    opacity: 1;
    pointer-events: auto;
  }

  /* zones (#852, #869): the cards and the old furniture go, the shared
     ground plan's own flat drawing arrives -- a card with a host count,
     no dots, no per-host labels. The dots' old survey camera tilt
     (rotateX/scale/translateY) retired with the survey stop itself: the
     city is the overview now, drawn in its own component, not a CSS
     transform on this one. */
  .camera .ground-flat {
    opacity: 0;
    pointer-events: none;
  }

  .camera.cam-zones .ground-flat {
    opacity: 1;
    pointer-events: auto;
  }

  /* #976 item 1: the trunk, the tunnel's own rib, the travelling mote
     and every lens's edges/badges are the lane-based drawing zones
     replaced -- left visible here, they painted across the ground
     plan's river and roads at the same stop, unreconciled with its
     coordinates. Still shown at clients/services, where the old
     lane-card drawing they belong to is what's on screen. */
  .camera.cam-zones .isl-card,
  .camera.cam-zones .detail,
  .camera.cam-zones .rib,
  .camera.cam-zones .rib-ghost,
  .camera.cam-zones .mote,
  .camera.cam-zones .edge-g,
  .camera.cam-zones .gedge {
    opacity: 0;
    pointer-events: none;
  }

  @media (prefers-reduced-motion: reduce) {
    .camera {
      transition: none;
    }
  }

  /* --- the altitude slider (#648, concept T; named ends #682) ----------- */
  .altitude {
    position: absolute;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 10px;
    font: 500 9px var(--font-mono);
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    opacity: 0.6;
    transition: opacity 0.25s;
  }

  .altitude:hover,
  .altitude:focus-within {
    opacity: 1;
  }

  .alt-track {
    position: relative;
    display: inline-flex;
    align-items: center;
    width: 170px;
  }

  /* A custom track and diamond thumb (#682): the native range input
     rendered as browser-default chrome under the map, unlike anything
     else this card wears. */
  .alt-range {
    display: block;
    width: 100%;
    margin: 0;
    height: 14px;
    background: transparent;
    cursor: pointer;
    -webkit-appearance: none;
    appearance: none;
  }

  .alt-range::-webkit-slider-runnable-track {
    height: 2px;
    background: var(--hair-2);
    border-radius: 1px;
  }

  .alt-range::-moz-range-track {
    height: 2px;
    background: var(--hair-2);
    border-radius: 1px;
  }

  .alt-range::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 9px;
    height: 9px;
    margin-top: -4px;
    background: var(--bg);
    border: 1.5px solid var(--accent);
    border-radius: 2px;
    transform: rotate(45deg);
    cursor: pointer;
  }

  .alt-range::-moz-range-thumb {
    width: 9px;
    height: 9px;
    background: var(--bg);
    border: 1.5px solid var(--accent);
    border-radius: 2px;
    transform: rotate(45deg);
    cursor: pointer;
  }

  /* The stops: a purely decorative overlay -- aria-hidden, no pointer
     events of their own -- so the range input beneath is what a click or
     a screen reader actually sees. Evenly spaced by the flex row itself,
     never a static per-stop offset. */
  .alt-ticks {
    position: absolute;
    inset: 0 2px;
    top: 6px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    pointer-events: none;
  }

  .tick {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--fg-dim);
  }

  .tick.on {
    background: var(--accent);
  }

  .tick.diamond {
    border-radius: 0;
    background: transparent;
    border: 1.2px solid var(--fg-dim);
    transform: rotate(45deg);
  }

  .tick.diamond.on {
    border-color: var(--accent);
  }

  /* --- the aggregate bar (#648, round 23) -------------------------------- */
  .hbar-g {
    cursor: pointer;
  }

  .hbar-g:focus-visible {
    outline: none;
  }

  .hb {
    stroke-width: 0.9;
  }

  /* Mixed against --bg-elevated, not transparent (owner, 2026-08-31): a
     10%-into-transparent fill is 90% see-through, so a crossing edge
     line -- which round 30's own coloured ribs (#715) can now paint in
     any lane's saturated ink -- reads straight through the pill and its
     count. Document order already draws this bar after every edge (see
     the lens template's own two-pass ordering); the fix here is opacity,
     not stacking. Measured: --marked on this fill ~5.1:1, --alarm on its
     own ~4.6:1 -- both past the 4.5:1 floor. */
  .hb-w {
    fill: color-mix(in srgb, var(--marked) 18%, var(--bg-elevated));
    stroke: color-mix(in srgb, var(--marked) 40%, transparent);
  }

  .hb-f {
    fill: color-mix(in srgb, var(--alarm) 18%, var(--bg-elevated));
    stroke: color-mix(in srgb, var(--alarm) 45%, transparent);
  }

  .hbar-g:hover .hb,
  .hbar-g:focus-visible .hb {
    stroke-width: 1.4;
  }

  .hb-div {
    stroke: var(--hair-2);
    stroke-width: 1;
  }

  .hbt {
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
  }

  .wp {
    fill: var(--marked);
  }

  .fchip {
    fill: var(--alarm);
  }

  /* --- the depth layers (#699) and the zones stop's flat ground plan
     (#852, #869) ------------------------------------------------------ */
  .gf-river {
    fill: var(--fg-dim);
    fill-opacity: 0.08;
    stroke: var(--fg-dim);
    stroke-opacity: 0.3;
  }

  /* #989: a hairline in the dim ink, no arrow -- it means "this subnet
     lives on that router" and nothing else, so it must not read as one
     of the roads above it. */
  .gf-belong {
    stroke: var(--fg-dim);
    stroke-width: 1;
    opacity: 0.45;
  }

  .gf-road {
    fill: none;
    stroke: var(--fg-dim);
    stroke-width: 1.5;
  }

  .gf-road-a {
    stroke: var(--accept);
  }

  .gf-road-d {
    stroke: var(--drop);
  }

  .gf-road-x {
    stroke: var(--alarm);
  }

  /* A road drawn 1.5 wide is not a pointer target, so every rib carries
     a wide invisible line for the pointer to find -- the same
     `edge-hit`/`strand-hit` trick the lens and the membrane already
     use. */
  .gf-road-g {
    cursor: pointer;
  }

  .gf-road-hit {
    fill: none;
    stroke: transparent;
    stroke-width: 12;
  }

  .gf-road-g:hover .gf-road,
  .gf-road-g:focus-visible .gf-road {
    stroke-width: 2.6;
  }

  .gf-node-g {
    cursor: pointer;
  }

  .gf-node-g:hover .gf-node,
  .gf-node-g:focus-visible .gf-node {
    fill: var(--accent);
  }

  .gf-node {
    fill: var(--bg-elevated);
    stroke: var(--accent);
    stroke-width: 2;
  }

  .gf-node-label {
    fill: var(--fg-muted);
    font-size: 10.5px;
  }

  .gf-card {
    cursor: pointer;
  }

  .gf-plate {
    fill: var(--bg-elevated);
    stroke-width: 2;
  }

  .gf-card.dark .gf-plate {
    opacity: 0.55;
  }

  /* A size down from the default `.n-cidr` (#976 item 2): this card is
     the smallest thing on the map that prints a full CIDR, and it now
     stacks under the name rather than racing it across one line, so
     the narrower glyphs buy back some of the margin a modest card-width
     floor did not. */
  .gf-card .n-cidr {
    font-size: 9px;
  }

  .gf-count {
    fill: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  .svc-t {
    fill: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 10.5px;
  }

  .svc-leader {
    stroke: var(--hair-2);
    stroke-width: 1;
    stroke-dasharray: 2 2;
  }

  .cli-spoke {
    fill: none;
    stroke-width: 0.8;
    opacity: 0.3;
  }

  /* A host's own lane, as bright as the brightest line it carries
     (round 49, #1016). Established is the 0.3 above -- already receding,
     which is what the rule asks for. */
  .cli-spoke.offbase {
    stroke-width: 1.4;
    opacity: 0.85;
  }

  .c-label,
  .c-dot {
    cursor: pointer;
  }

  .c-label {
    fill: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 9.5px;
  }

  .c-label:hover,
  .c-label:focus-visible,
  .camera .c-dot:hover,
  .camera .c-dot:focus-visible {
    fill: var(--fg);
  }

  .c-dot:focus-visible,
  .c-label:focus-visible {
    outline: none;
  }

  .c-label.more {
    /* fg-dim directly on the void (--bg) reads ~3.3:1 (#715); fg-muted
       clears 4.5:1 at ~8:1. */
    fill: var(--fg-muted);
  }

  /* An island passive to the pointer still owes its aggregate bar its
     own clicks (#699: the internet island gained one). */
  .passive .hbar-g {
    pointer-events: auto;
  }

  /* --- node info cards (#648, rounds 22-23) ------------------------------ */
  .host-node,
  .cluster-g {
    pointer-events: auto;
    cursor: pointer;
  }

  .host-node:hover .host-circle,
  .host-node:focus-visible .host-circle {
    stroke-opacity: 0.9;
  }

  .cluster-g:hover .cluster,
  .cluster-g:focus-visible .cluster {
    stroke: var(--accent);
  }

  .host-node:focus-visible,
  .cluster-g:focus-visible {
    outline: none;
  }

  .node-card {
    position: fixed;
    z-index: 20;
    min-width: 210px;
    max-width: 250px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    background: var(--glass);
    backdrop-filter: blur(10px);
    border: 1px solid var(--hair-2);
    border-radius: 10px;
    padding: 12px 14px;
    box-shadow: 0 14px 36px rgba(0, 0, 0, 0.35);
  }

  .nc-close {
    position: absolute;
    top: 8px;
    right: 10px;
    background: none;
    border: none;
    color: var(--fg-dim);
    font-size: 11px;
    cursor: pointer;
  }

  .nc-close:hover {
    color: var(--fg);
  }

  .nc-name {
    font-size: 13px;
    color: var(--fg);
  }

  .nc-addr {
    margin-left: 8px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .nc-lane {
    margin: 0;
    font-size: 11px;
    color: var(--fg-muted);
  }

  .nc-warn {
    margin: 0;
    font-size: 11px;
    color: var(--alarm);
  }

  .nc-watch {
    margin: 0;
    font-size: 11px;
    color: var(--marked);
  }

  .nc-acts {
    margin-top: 6px;
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }

  .nc-act {
    background: none;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    padding: 3px 10px;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--accent);
    cursor: pointer;
  }

  .nc-act:hover {
    border-color: var(--accent);
  }

  /* --- the two filters (#1018, round 53) --------------------------------- */
  /* The port pill, bottom-left where round 49's lens pills were: one
     shape in three states, ported from round-53/index.html's `.pill.p`,
     `.pill.p.on` and `.pill.p.edit`. */
  .pill {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    padding: 3px 11px 3px 9px;
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    background: transparent;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.04em;
    color: var(--fg-muted);
    cursor: pointer;
  }

  .pill:hover {
    border-color: var(--fg-dim);
  }

  .pill.p.on {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }

  .pill.p.on b {
    color: var(--fg);
  }

  .pill.p.on em {
    font-style: normal;
    color: var(--fg-dim);
    margin-left: 4px;
  }

  /* The ✕ is its own control, not a hot corner of the pill: clearing a
     filter and changing it are different intentions, and one target
     doing both is the misclick nobody notices they made. */
  .pill-x {
    align-self: center;
    background: none;
    border: 1px solid transparent;
    border-radius: 50%;
    width: 20px;
    height: 20px;
    padding: 0;
    color: var(--fg-dim);
    font-size: 11px;
    line-height: 1;
    cursor: pointer;
  }

  .pill-x:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  /* Open, the pill becomes a bar of the same shape (owner, 2026-09-08:
     "make it look more like the bar" -- "click and select"). No Show
     button: the map filters as the selection changes. */
  .pill.p.edit {
    cursor: default;
    gap: 0;
    padding: 3px 10px 3px 9px;
    color: var(--fg-dim);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
    max-width: min(60vw, 640px);
    /* A flex item will not shrink past its content by default, which
       would push the tally beside it out of the row the moment the
       chip strip is long (#1136). Shrinking is what the strip's own
       `overflow-x: auto` is there for. */
    min-width: 0;
  }

  .pill.p.edit .ports,
  .pill.p.edit .seg {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }

  .pill.p.edit .ports {
    margin-left: 6px;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .pill.p.edit .chip {
    background: none;
    border: 0;
    padding: 1px 6px;
    border-radius: 999px;
    color: var(--fg-dim);
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.04em;
    cursor: pointer;
  }

  .pill.p.edit .chip:hover {
    color: var(--fg-muted);
  }

  .pill.p.edit .chip.on {
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--fg);
  }

  .pill.p.edit .chip-none {
    padding: 1px 6px;
    color: var(--fg-dim);
    font-style: italic;
  }

  .pill.p.edit .typed {
    width: 84px;
    margin-left: 4px;
    background: transparent;
    border: 0;
    padding: 1px 4px;
    font: 600 10.5px var(--font-mono);
    letter-spacing: 0.04em;
    color: var(--fg);
    outline: none;
  }

  .pill.p.edit .typed::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
    font-weight: 400;
  }

  /* A list that cannot be read whole is refused, and says so where it
     was typed rather than by quietly filtering to half of it. */
  .pill.p.edit .typed.bad {
    color: var(--alarm);
    box-shadow: inset 0 -1px 0 var(--alarm);
  }

  .pill.p.edit .bar {
    width: 1px;
    height: 12px;
    background: var(--hair-2);
    margin: 0 8px;
  }

  /* Both tools swap the legend for their own entries, and the map has
     none otherwise: round 49 ruled the material is the statement. */
  /* Left of the fit chip, never under it: `right` is set inline from
     the chip's measured width (see legendRight), and this is only the
     fallback for the moment before that measurement lands. */
  .map-legend {
    position: absolute;
    bottom: 13px;
    right: 120px;
    z-index: 2;
    display: flex;
    gap: 15px;
    align-items: center;
    font: 9.5px var(--font-mono);
    color: var(--fg-dim);
    letter-spacing: 0.02em;
    pointer-events: none;
  }

  .map-legend span {
    display: inline-flex;
    gap: 5px;
    align-items: center;
  }

  .map-legend i.sw {
    width: 14px;
    height: 3px;
    border-radius: 2px;
    display: inline-block;
  }

  .map-legend i.sw.ok {
    background: var(--accept);
    opacity: 0.85;
  }

  .map-legend i.sw.al {
    background: var(--alarm);
  }

  .map-legend i.sw.ghost {
    background: repeating-linear-gradient(90deg, var(--fg-dim) 0 2px, transparent 2px 5px);
    opacity: 0.8;
  }

  .map-legend i.sw.off {
    background: var(--fg-muted);
    opacity: 0.5;
  }

  /* The lit layer: full width in the verdict's own ink. Not a volume
     reading -- this is "the one you asked about". */
  .lit-half {
    fill: none;
    stroke: var(--accept);
    stroke-linecap: round;
    opacity: 0.85;
  }

  .lit-half.refused {
    stroke: var(--alarm);
    opacity: 0.8;
    filter: drop-shadow(0 0 5px color-mix(in srgb, var(--alarm) 40%, transparent));
  }

  .lit-flow {
    fill: none;
    stroke: var(--accept);
    stroke-opacity: 0.9;
  }

  /* Off the filter: one thin grey line, dimmed, never removed. The
     opacity itself is inline (dimFor), because a direction carrying a
     door recedes less than the rest. */
  .redge.port-off,
  .cedge.port-off {
    stroke-dasharray: none;
    filter: none;
  }

  .cedge.port-off {
    stroke: var(--fg-muted);
  }

  /* A dot not in the answer recedes; it is never taken off the card,
     because "2 of 12" is a claim about twelve hosts. */
  .dot-off {
    opacity: 0.2;
  }

  .lane-off {
    opacity: 0.45;
  }

  .filter-tally {
    fill: var(--fg-muted);
  }

  /* The rib a door stands on where the map draws none of its own: the
     boundary the rule guards, in the dim grey and at the door's own
     0.6, never in a verdict ink. Policy, and it must never read as a
     line that happened. */
  .door-guide {
    fill: none;
    stroke: var(--fg-muted);
    stroke-width: 1.4;
    stroke-linecap: round;
    opacity: 0.6;
  }

  /* A door: two posts across the rib where a rule sits, the leaf swung
     open for accept and a bar across for a refusal. */
  .door-post {
    fill: none;
    stroke: var(--accept);
    stroke-width: 1.8;
    stroke-linecap: round;
  }

  .door.shut .door-post {
    stroke: var(--alarm);
  }

  .door-t {
    font: 600 9px var(--font-mono);
    fill: var(--fg-dim);
  }

  .door-act {
    fill: var(--accept);
  }

  .door.shut .door-act {
    fill: var(--alarm);
  }

  /* The rib a refused line would have taken. Dashed, always: nothing
     travelled it, and the router's log ends at the router. */
  .trace-ghost {
    fill: none;
    stroke: var(--fg-muted);
    stroke-width: 1.4;
    stroke-dasharray: 2 5;
    stroke-linecap: round;
    opacity: 0.7;
  }

  .trace-ring {
    stroke: var(--accept);
  }

  /* The ghost's own note. Its own size and ink rather than the
     "never exercised" badge's `.ghost-t`, which is a modifier on
     `.edge-badge` and inherits that plate's font -- alone it would take
     the SVG's 16px default and run off the frame. */
  .trace-note {
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-style: italic;
    fill: var(--fg-dim);
  }

  /* The three labels the two filters add sit over lines by design --
     a door stands on a rib, and the ghost's note runs beside the dashes
     it is about. Round 53's own device for that is the mockup's
     `.flat text`: the glyphs are painted over a stroke of the page
     colour, so whatever runs under them passes behind rather than
     through. Same numbers as the mockup. */
  .trace-note,
  .door-t,
  .note-t {
    paint-order: stroke;
    stroke: var(--bg);
    stroke-width: 3.4px;
    stroke-linejoin: round;
  }

  /* The router's own decision, beside the router. */
  .trace-chip rect {
    fill: color-mix(in srgb, var(--accept) 8%, var(--bg-raised));
    stroke: var(--accept);
    stroke-opacity: 0.8;
  }

  .trace-chip.refused rect {
    fill: color-mix(in srgb, var(--alarm) 8%, var(--bg-raised));
    stroke: var(--alarm);
  }

  .trace-chip.unjudged rect {
    fill: var(--bg-raised);
    stroke: var(--hair-2);
  }

  .trace-chip.unjudged .chip-verdict {
    fill: var(--fg-muted);
  }

  .trace-chip .chip-verdict {
    fill: var(--accept);
  }

  .trace-chip.refused .chip-verdict {
    fill: var(--alarm);
  }

  .trace-leader {
    fill: none;
    stroke: var(--hair-2);
    stroke-width: 1;
  }

  /* Nothing seen: one line under the map, not an empty state. */
  .note-t {
    font: 11px var(--font-mono);
    fill: var(--fg-dim);
  }

  /* `trace ▸` on the unplanned callout -- a token, not a sentence. */
  .uc-trace-t {
    font: 600 10px var(--font-mono);
    fill: var(--accent);
    cursor: pointer;
  }

  .uc-trace:hover .uc-trace-t,
  .uc-trace:focus-visible .uc-trace-t {
    text-decoration: underline;
  }
</style>
