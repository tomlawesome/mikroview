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
  // nothing floats over the map). The lens row carries round 30's and
  // round 39's five: three base lenses that repaint the pair lines, and
  // two overlays -- flags and watch -- that place ledger objects on top
  // of whichever base is showing (#715 item 3). The two families split
  // by data source, which is why one is exclusive and the other is not:
  // a base lens is a different reading of the same edges, an overlay is
  // a different kind of thing marked on them. One fixed picture, tabs
  // repaint it: the Policy lens keeps every island where Traffic put it
  // and swaps the observed ribs for what the pushed rule table intends.
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
  import { composeCommand, reachComposeInput, refusingCommentFor } from '../lib/compose'
  import type { ReachStrand } from '../lib/reach'
  import { portsLine, reachFor } from '../lib/reach'
  import { authState } from '../lib/auth.svelte'
  import { isPublicIp, formatHM, formatRelative } from '../lib/format'
  import { flagsState, extractSourceIp } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import { familyOf, ADVISORY_INK } from '../lib/flagPalette'
  import { parseCidr, addressInCidr } from '../lib/addressMatch'
  import { FLAG_TYPE_LABELS } from '../lib/metricsSeries'
  import { nightlySummary } from '../lib/watchWindow'
  import type { Flag, WatchlistEntry } from '../lib/types'
  import City from './City.svelte'
  import { STOPS, R2, flatFit, FX, FY } from '../lib/city/project'
  import { layoutGround } from '../lib/city/layout'
  import { cityInputFrom } from '../lib/city/input'
  import type { Ground } from '../lib/city/types'
  import { ALTITUDE_LABELS, CENTRE_ALTITUDE, isCityAltitude, type Altitude } from '../lib/altitude'
  import { altitudeStopState } from '../lib/altitudeStop.svelte'

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
    }
  })

  const isAdmin = $derived(authState.role === 'admin')

  /** Component-unique prefix for this instance's SVG ids. */
  const uid = $props.id()

  // Which lens repaints the fixed picture. Reach layers on top of
  // any of them (#626: a mode, not a place).
  let lens = $state<'traffic' | 'policy' | 'coverage'>('traffic')
  // The two overlays (#715 item 3). Independent of `lens` and of each
  // other, both on by default, and session state like `lens` -- nothing
  // here is persisted.
  let flagsOn = $state(true)
  let watchOn = $state(true)
  // Estate-wide, deliberately: this is the same number the dial and the
  // scene bar show, and a second map-scoped flag count on one screen
  // would fight them. Drawn only above zero -- no round draws "flags 0",
  // and a zero is not a thing to report.
  const flagCountAll = $derived(flagsState.activeCount)

  const zones = $derived(zonesState.zones)
  const eps = $derived(appState.stats?.eventsPerSecond ?? 0)

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
    const n = zones.length
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

  // The host row is sized to the card it sits in (#699; round 30's own
  // list, the-whole.html:1007, which drops to one name on the narrow
  // Guest card rather than running past its edge). `.n-hosts` is the
  // sans face at 10px, so ~0.55em an advance is a safe estimate -- and
  // whatever the estimate gets wrong the clip catches, so a name can
  // never reach the neighbouring lane again.
  const HOST_CH = 5.5

  function hostsShown(z: ZoneInfo): { hosts: { label: string; ip: string }[]; more: number } {
    const budget = cardW - 2 * cardPad
    const shown: { label: string; ip: string }[] = []
    let used = 0
    for (const h of z.hosts) {
      if (shown.length >= 3) break
      const w = h.label.length * HOST_CH + (shown.length > 0 ? 3 * HOST_CH : 0)
      const rest = z.hostCount - (shown.length + 1)
      const tail = rest > 0 ? (4 + String(rest).length) * HOST_CH : 0
      // The first name always draws: a card that names none of its
      // hosts says less than one that names one and clips it.
      if (shown.length > 0 && used + w + tail > budget) break
      used += w
      shown.push(h)
    }
    return { hosts: shown, more: z.hostCount - shown.length }
  }

  // Shared by ribPath and the internet-edge limbs (#726: "bundle the
  // corridor, fan at the waist") so a rib and its edge cannot drift
  // apart -- both draw the same slot for the same lane.
  function slotSpread(i: number, n: number): number {
    return n === 1 ? 0 : -55 + (110 / (n - 1)) * i
  }

  function ribPath(i: number, n: number): string {
    const x = laneX(i, n)
    const spread = slotSpread(i, n)
    return `M ${700 + spread} 302 C ${700 + spread * 2.2} 380, ${x + (700 - x) * 0.25} 420, ${x} 480`
  }

  // Click-through per the shaped surface: a zone lands on the live view
  // filtered to its boundary; the whole map never navigates on a miss.
  function openZone(id: string) {
    appState.setFilter('interface', id)
    appState.view = 'live'
  }

  // --- the policy lens (#628: layer 2, intended-policy edges) --------------
  // Every crossing passes the router, so every edge routes through the
  // waist -- and an intended refusal dies there, ⊣, the same grammar the
  // reach's membrane already taught. Calm ink throughout: an intended
  // block is policy, not the alarm.
  const WAIST = { x: 700, y: 312 }
  const EDGE_CAP = 12

  // `idx` is only set for 'zone' anchors -- an internet edge's own slot
  // is its zone end's lane index (#726), so it rides along with the
  // anchor rather than being re-derived from the edge's position.
  type EdgeAnchor = { x: number; y: number; kind: 'zone' | 'internet' | 'tunnel' | 'any'; idx?: number }

  // The tunnel node's own place on the stage, ported from round 30
  // (the-whole.html:986, `translate(1128 132)`). Its card is drawn
  // asymmetrically about this point -- -84 to +104 -- exactly as the
  // mockup does; TUNNEL_ANCHOR is the underside its lines leave from.
  const TUNNEL = { x: 1128, y: 132 }
  const TUNNEL_ANCHOR = { x: 1080, y: 186 }

  function anchorOf(iface: string): EdgeAnchor | null {
    if (iface === '') return { ...WAIST, kind: 'any' }
    if (iface === zonesState.wanInterface) return { x: 700, y: 104, kind: 'internet' }
    // Before the lane row is consulted: the tunnel left it (#877), and
    // an edge that used to find its lane card must now find the node
    // rather than fall through to null and be dropped silently.
    if (iface === tunnelIface) return { ...TUNNEL_ANCHOR, kind: 'tunnel' }
    const i = zones.findIndex((z) => z.id === iface)
    if (i === -1) return null
    return { x: laneX(i, zones.length), y: 484, kind: 'zone', idx: i }
  }

  // A line between two anchors, shared by both lenses: the Policy lens
  // draws intent along it, the Traffic lens draws what actually
  // happened (#629). `crosses` is whether it arrives or dies at the
  // waist.
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
    const dx = to.x - from.x
    const dy = to.y - from.y
    const len = Math.hypot(dx, dy) || 1
    const spread = from.kind === 'any' || to.kind === 'any' ? SPLIT + ANY_CLEAR : SPLIT
    // A→B and B→A split to either side of the pair's shared line.
    return { from, to, off: { x: (-dy / len) * spread, y: (dx / len) * spread }, crosses }
  }

  interface DrawnEdge {
    edge: PolicyEdge
    line: Line
  }

  const drawnEdges = $derived.by((): { drawn: DrawnEdge[]; undrawn: number } => {
    const drawn: DrawnEdge[] = []
    let undrawn = 0
    for (const e of policyState.edges) {
      const line = drawn.length < EDGE_CAP ? lineFor(e.from, e.to, e.accepted) : null
      // Counted and said, never silently dropped.
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ edge: e, line })
    }
    return { drawn, undrawn }
  })

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
    return slotSpread(laneAnchor.idx ?? 0, zones.length)
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
  function edgePath(l: Line): string {
    const { from, to, off } = l
    if (isInternetEdge(l) && l.crosses) {
      const spread = internetSlotSpread(l)
      const laneAnchor = from.kind === 'zone' ? from : to
      const waistPt = { x: 700 + spread, y: 302 }
      const laneCtrl = { x: laneAnchor.x + (700 - laneAnchor.x) * 0.25, y: 420 }
      const waistCtrl = { x: 700 + spread * 2.2, y: 380 }
      const pt = (p: { x: number; y: number }) => `${p.x + off.x} ${p.y + off.y}`
      return from.kind === 'zone'
        ? `M ${pt(laneAnchor)} C ${pt(laneCtrl)}, ${pt(waistCtrl)}, ${pt(waistPt)}`
        : `M ${pt(waistPt)} C ${pt(waistCtrl)}, ${pt(laneCtrl)}, ${pt(laneAnchor)}`
    }
    const w = { x: WAIST.x + off.x, y: WAIST.y + off.y }
    if (l.crosses) {
      return `M ${from.x + off.x} ${from.y + off.y} C ${w.x} ${w.y}, ${w.x} ${w.y}, ${to.x + off.x} ${to.y + off.y}`
    }
    const dp = deathPoint(l)
    return `M ${from.x + off.x} ${from.y + off.y} Q ${(from.x + dp.x) / 2 + off.x} ${(from.y + dp.y) / 2 + off.y}, ${dp.x} ${dp.y}`
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

  function badgeLine(e: PolicyEdge): string {
    const ports = e.accepted ? e.acceptPorts : e.refusePorts
    const shown = ports.slice(0, 3).join(' ')
    const more = ports.length > 3 ? ` +${ports.length - 3}` : ''
    const mark = e.accepted ? (e.refused ? '→ ⊣' : '→') : '⊣'
    return ports.length > 0 ? `${mark} ${shown}${more}` : mark
  }

  function edgeLabel(e: PolicyEdge): string {
    const name = (i: string, kind: string) => (kind === 'internet' ? 'the internet' : i === '' ? 'any lane' : i)
    const from = anchorOf(e.from)
    const to = anchorOf(e.to)
    const what = e.accepted ? 'may reach' : 'is refused toward'
    return `${name(e.from, from?.kind ?? 'zone')} ${what} ${name(e.to, to?.kind ?? 'zone')}${e.comment ? ` — ${e.comment}` : ''}`
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

  function openEdge(e: PolicyEdge) {
    openPair(e.from, e.to, e.accepted ? e.acceptPorts : e.refusePorts)
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

  // Round 30's own ribs carry the lane's ink even once they stop being
  // the placeholder volume line and become a real observed pair (the-
  // whole.html:930-935: `.rib` is stroked var(--lan)/var(--srv)/etc, not
  // one shared grey, whichever zone it touches). A pair not touching any
  // recognised lane (internet-to-waist, or a boundary the address push
  // has not named) has no lane ink to borrow, so it keeps the calm
  // shared one -- never a fabricated colour. The one saturated colour
  // stays alarm's alone: callers skip this for an unplanned pair.
  function laneInkFor(fromIface: string, toIface: string): string {
    const i = zones.findIndex((z) => z.id === fromIface)
    if (i !== -1) return LANE_INKS[i % LANE_INKS.length]
    const j = zones.findIndex((z) => z.id === toIface)
    if (j !== -1) return LANE_INKS[j % LANE_INKS.length]
    return 'var(--fg-muted)'
  }

  function realityBadge(r: RealityEdge): string {
    const ports = r.topPorts.slice(0, 3).join(' ')
    const n = r.events.toLocaleString()
    if (r.accepts === 0) return `⊣ ${n}× ${r.verdict === 'holding' ? 'held' : 'dropped'}`
    const mark = r.verdict === 'unplanned' ? 'unplanned ·' : '→'
    return `${mark} ${ports ? `${ports} · ` : ''}${n}×`
  }

  // --- the coverage paint (#630: layer 4, the #392 model) ------------------
  // Per boundary and direction: observed (a rule logs) draws solid,
  // dark (rules, none logging) draws dotted and is labelled dark --
  // drawn, never omitted, because an edge's absence is information.
  // Declared-quiet boundaries arrive with the declarations store.
  const drawnCoverage = $derived.by((): { drawn: DrawnEdge[]; undrawn: number } => {
    const drawn: DrawnEdge[] = []
    let undrawn = 0
    for (const e of policyState.edges) {
      // Coverage is about the boundary-direction, not passage: every
      // pair draws the full crossing, logged or dark.
      const line = drawn.length < EDGE_CAP ? lineFor(e.from, e.to, true) : null
      if (!line) {
        undrawn++
        continue
      }
      drawn.push({ edge: e, line })
    }
    return { drawn, undrawn }
  })

  // The edge cap's own honesty (#699). The count used to be drawn as a
  // caption in the stage's bottom-right corner, where it ran straight
  // through the rightmost lane's aggregate bar; round 30 draws no such
  // caption anywhere. The fact is not lost -- it rides the map's own
  // accessible name instead, so nothing on the drawing carries it and
  // nothing about the map's incompleteness goes unsaid.
  const undrawnPairs = $derived(lens === 'traffic' ? drawnReality.undrawn : lens === 'coverage' ? drawnCoverage.undrawn : drawnEdges.undrawn)
  const undrawnNote = $derived(
    undrawnPairs > 0
      ? `. ${undrawnPairs} further pair${undrawnPairs === 1 ? '' : 's'} are not drawn — off this map's islands, or beyond its ${EDGE_CAP}-edge calm`
      : '',
  )

  type CoverageStateOf = 'observed' | 'quiet' | 'dark'

  function coverageOf(e: PolicyEdge): CoverageStateOf {
    if (e.logged) return 'observed'
    return coverageState.byKey.has(e.key) ? 'quiet' : 'dark'
  }

  function pairName(from: string, to: string): string {
    const name = (i: string) => (i === zonesState.wanInterface ? 'the internet' : i === '' ? 'any lane' : i)
    return `${name(from)} → ${name(to)}`
  }

  function coverageLabel(e: PolicyEdge): string {
    const st = coverageOf(e)
    if (st === 'observed') return `${pairName(e.from, e.to)}: logged`
    if (st === 'quiet') {
      const d = coverageState.byKey.get(e.key)
      return `${pairName(e.from, e.to)}: intentionally quiet — ${d?.reason ?? ''}`
    }
    return `${pairName(e.from, e.to)}: dark — no rule on this boundary-direction logs`
  }

  // The declare-a-gap interaction (#392: one acknowledgement, stored
  // with its reason). Admin-only, per #490's grammar: for a viewer the
  // affordance is absent, never disabled. Opened by clicking a dark or
  // quiet edge in the Coverage lens.
  let declarePanel = $state<{ key: string; from: string; to: string } | null>(null)
  let declareReason = $state('')
  let declareBusy = $state(false)

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
        ...drawnReality.drawn.map((d) => ({ line: d.line, text: d === worstUnplanned ? '' : realityBadge(d.r) })),
        ...ghostIntents.map((g) => ({ line: g.line, text: 'never exercised', dy: -12 })),
      ],
      worstUnplannedCard,
    ),
  )

  function coverageBadgeText(e: PolicyEdge): string {
    const st = coverageOf(e)
    if (st === 'observed') return ''
    if (st === 'dark') return 'dark'
    return `quiet · ${(coverageState.byKey.get(e.key)?.reason ?? '').slice(0, 28)}`
  }

  const coverageBadges = $derived(placeBadges(drawnCoverage.drawn.map((d) => ({ line: d.line, text: coverageBadgeText(d.edge) }))))

  const policyBadges = $derived(
    placeBadges(
      drawnEdges.drawn.map((d) => ({
        line: d.line,
        // A port-less refusal's badge would only repeat the bar, so it
        // draws none -- and takes no room in the layout either.
        text: d.edge.accepted || d.edge.refusePorts.length > 0 ? badgeLine(d.edge) : '',
      })),
    ),
  )

  function openCoverage(e: PolicyEdge) {
    if (!isAdmin || coverageOf(e) === 'observed') return
    coverageState.error = null
    declareReason = coverageState.byKey.get(e.key)?.reason ?? ''
    declarePanel = { key: e.key, from: e.from, to: e.to }
  }

  async function submitDeclaration() {
    if (!declarePanel || !declareReason.trim()) return
    declareBusy = true
    const ok = await coverageState.declare(declarePanel.key, declareReason.trim())
    declareBusy = false
    if (ok) declarePanel = null
  }

  async function removeDeclaration() {
    if (!declarePanel) return
    declareBusy = true
    const ok = await coverageState.undeclare(declarePanel.key)
    declareBusy = false
    if (ok) declarePanel = null
  }

  // The zone card's coverage caption, kept on every lens (the shaped
  // surface: the Coverage lens carries the full model, the others keep
  // the captions). Toward/from the internet, per direction.
  function zoneCaption(zoneId: string): string | null {
    const wan = zonesState.wanInterface
    if (!policyState.anyPushed || !wan) return null
    const stateOf = (key: string): 'observed' | 'quiet' | 'dark' | 'none' => {
      const e = policyState.edges.find((p) => p.key === key)
      if (!e) return 'none'
      if (e.logged) return 'observed'
      return coverageState.byKey.has(key) ? 'quiet' : 'dark'
    }
    const out = stateOf(`${zoneId}|${wan}`)
    const inward = stateOf(`${wan}|${zoneId}`)
    const fine = (st: string) => st === 'observed' || st === 'quiet'
    if (out === 'observed' && inward === 'observed') return 'LOGGED BOTH WAYS'
    if (fine(out) && fine(inward)) return 'COVERED — logged or declared quiet'
    if (out === 'quiet' && !fine(inward)) return 'DARK FROM WAN — quiet toward it by choice'
    if (fine(out)) return 'DARK FROM WAN — no log rule inbound'
    if (fine(inward)) return 'DARK TOWARD WAN — no log rule on this boundary'
    return 'DARK BOTH WAYS — no log rule on this boundary'
  }

  // The coverage badge's own colour (#682): logged reads healthy green,
  // dark reads alarm red, anything else (quiet, or logged-or-declared-
  // quiet) stays the calm dim ink -- the same three-way split the
  // ratified round-29 card wears, per zoneCaption's own vocabulary.
  function covClass(caption: string): 'cov-l' | 'cov-d' | 'cov-q' {
    if (caption.startsWith('DARK')) return 'cov-d'
    if (caption.startsWith('LOGGED')) return 'cov-l'
    return 'cov-q'
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
  let reach = $state<{ zoneId: string; host: string; ip: string } | null>(null)

  function descend(zoneId: string, host: string, ip: string) {
    reach = { zoneId, host, ip }
  }

  function surface() {
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
    if (e.key === 'Escape') {
      // Esc walks out one level: the node card first, then the composer,
      // then the reach.
      if (nodeCard) nodeCard = null
      else if (compose) compose = null
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

  const reachSummary = $derived(reach ? reachFor(reach.ip, zonesState.wanInterface, appState.events) : null)

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

  function strandPath(counterpart: string, outcome: string, direction: string): string {
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
    return `M ${from.x} ${from.y} Q ${mid.x} ${mid.y}, ${to.x} ${to.y}`
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

  const reachZoneInk = $derived(reach ? LANE_INKS[Math.max(0, zoneIndex(reach.zoneId)) % LANE_INKS.length] : 'var(--accent)')

  const siblings = $derived.by(() => {
    if (!reach) return []
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
  // The last stop visited persists per user, the same small-module
  // convention cityImportance.svelte.ts uses for its reading.
  let altitude = $state<Altitude>(ALTITUDE_LABELS.indexOf(altitudeStopState.stop) as Altitude)
  const cityStop = $derived(isCityAltitude(altitude) ? STOPS[altitude - CENTRE_ALTITUDE] : null)

  // The one ground plan (#852, #869): the same estate the city already
  // lays out, computed once here rather than once per view. Passed down
  // to City below (its own `groundProp` fallback stays for City's own
  // tests, which mount it without a Topography) and read directly by
  // the zones stop's own flat drawing further down -- one source, so
  // the two views can never quietly drift into disagreeing position
  // models.
  const ground = $derived<Ground>(
    layoutGround(
      cityInputFrom(
        appState.devices,
        zones,
        appState.events,
        realityEdges(appState.events, policyState.edges, policyState.anyPushed),
        policyState.edges,
        policyState.anyPushed,
        primaryDevice?.id ?? null,
        zonesState.wanInterface,
        tunnelsState.list,
        policyState.pushed,
      ),
    ),
  )

  // A flat (non-isometric) fit of that same ground into the 2D stage,
  // for the zones stop below.
  const flatCam = $derived(flatFit(ground.bounds, 1400, 720))

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
      if (reach) {
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
  const watcherTotal = $derived(watchlistState.entries.length)
  const watcherBroken = $derived(watchlistState.brokenCount)
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

  function onWindowClick() {
    if (nodeCard) nodeCard = null
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

  // --- the tunnel node (#877, #701's fact 3 as split) ----------------------
  // Round 30 draws a second upper node beside Internet: `WireGuard` /
  // `wg0 · 10.99.0.0/24` / `QUIET`, with its own watch bar
  // (the-whole.html:986-1006). Nothing drew it before because a tunnel
  // was not a thing this scene had -- a WireGuard interface could only
  // land in the lane row, which is what zonesState.tunnelInterface now
  // takes it out of.
  const tunnelIface = $derived(zonesState.tunnelInterface)

  const tunnelApi = $derived(tunnelIface ? (tunnelsState.list.find((t) => t.iface === tunnelIface) ?? null) : null)

  /** The tunnel's own events in this window: what separates an API
   * `up` that is carrying traffic from one that is lit and empty. */
  const tunnelEvents = $derived(
    tunnelIface
      ? appState.events.filter((e) => e.inInterface === tunnelIface || e.outInterface === tunnelIface).length
      : 0,
  )

  // 'quiet' is mikroview's own reading on top of the API's vocabulary,
  // and QUIET is exactly what round 30 draws on this card.
  const tunnelState = $derived(bridgeStateFor(tunnelApi?.apiState ?? null, tunnelEvents))
  const tunnelStateLabel = $derived(bridgeStateLabel(tunnelState))
  const tunnelCovClass = $derived(tunnelState === 'up' ? 'cov-l' : tunnelState === 'down' ? 'cov-d' : 'cov-q')

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
  const tunnelSubnet = $derived.by((): string | null => {
    if (!tunnelIface) return null
    const row = zonesState.pushed.find((a) => a.interface === tunnelIface)
    if (!row) return null
    const slash = row.address.indexOf('/')
    const prefix = slash === -1 ? '' : row.address.slice(slash + 1)
    if (row.network && prefix) return `${row.network}/${prefix}`
    return row.address || null
  })

  /** The node's watch bar. Correlated exactly as a lane's is -- through
   * the pushed CIDR -- so no third correlation rule enters the scene,
   * and absent entirely while no address names the tunnel's range,
   * which is the same refusal zoneAggregate already makes. */
  const tunnelAggregate = $derived.by((): ZoneAggregate | null => {
    if (!tunnelIface || !tunnelSubnet) return null
    return zoneAggregate({
      id: tunnelIface,
      name: tunnelIface,
      cidr: tunnelSubnet,
      hosts: [],
      hostCount: 0,
      eventCount: tunnelEvents,
    })
  })

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
</script>

<svelte:window onkeydown={onKeydown} onclick={onWindowClick} />

<div class="topo">
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
    <div class="crumb">
      <div class="path">
        <button class="crumb-link" onclick={surface}>Network</button>
        <span class="sep">▸</span>
        <button class="crumb-link" onclick={surface}>{zones.find((z) => z.id === reach?.zoneId)?.name ?? reach.zoneId}</button>
        <span class="sep">▸</span>
        <span class="here">{reach.host}</span>
      </div>
      {#if reachSummary}
        <div class="sub">
          reaches <b>{reachSummary.reaches}</b> · reached by <b>{reachSummary.reachedBy}</b>
          {#if reachSummary.topBlocked}
            {@const b = reachSummary.topBlocked}
            {@const far = b.counterpart === 'internet' ? 'the internet' : b.counterpart}
            · <b class="alarm">{b.direction === 'out' ? `blocked toward ${far}` : `knocked from ${far}, refused`} — {b.count}×</b>
          {/if}
        </div>
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
            {bp.direction === 'out' ? `${reach.host} → ${peer}` : `${peer} → ${reach.host}`}{hit ? ` · ${hit.proto}/${hit.port}` : ''}
            · {#if bp.outcome === 'blocked'}<b class="alarm">refused</b>{:else}accepted{/if}
          </div>
        {/if}
      {/if}
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

  <!-- The lens selector (#682, ported from the scene's `.wlens2`): the
       bottom-left bar, not a top-right tab strip -- round 29 has no
       such strip beside the dials. -->
  <div class="wlens2">
    <div class="wl-tabs" role="tablist" aria-label="Map lenses">
      {#if reach}
        <span class="on" role="tab" aria-selected="true">reach</span>
        <span role="tab" aria-selected="false">{lens === 'policy' ? 'policy' : 'traffic'}</span>
      {:else}
        <button class:on={lens === 'traffic'} role="tab" aria-selected={lens === 'traffic'} onclick={() => (lens = 'traffic')}>
          traffic
        </button>
        <button class:on={lens === 'policy'} role="tab" aria-selected={lens === 'policy'} onclick={() => (lens = 'policy')}>
          policy
        </button>
        <button class:on={lens === 'coverage'} role="tab" aria-selected={lens === 'coverage'} onclick={() => (lens = 'coverage')}>
          coverage
        </button>
      {/if}
    </div>
    {#if !reach}
      <!-- The overlays sit outside the tablist, behind a hairline
           (round-39:943). Two reasons, and they agree: a toggle inside
           a tablist breaks its semantics, and the divider is what tells
           a reader that these two latch while the three to their left
           move a highlight. Both default on -- rounds 30 and 39 draw
           the scene wearing them, and an overlay with nothing to show
           paints nothing, so the calm default survives (Fable 5). -->
      <span class="wl-div" aria-hidden="true"></span>
      <div class="wl-overlays" role="group" aria-label="Map overlays">
        <button
          type="button"
          class="wl-ov"
          class:on={flagsOn}
          aria-pressed={flagsOn}
          aria-label={flagCountAll > 0
            ? `Flags overlay — ${flagCountAll} open: mark flagged places on the map`
            : 'Flags overlay — mark flagged places on the map'}
          onclick={() => (flagsOn = !flagsOn)}
        >
          flags{#if flagCountAll > 0}&nbsp;<span class="ov-n">{flagCountAll}</span>{/if}
        </button>
        <button
          type="button"
          class="wl-ov"
          class:on={watchOn}
          aria-pressed={watchOn}
          aria-label="Watch overlay — mark watched places on the map"
          onclick={() => (watchOn = !watchOn)}
        >
          watch
        </button>
      </div>
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
    <svg
      viewBox="0 0 1400 720"
      preserveAspectRatio="xMidYMid meet"
      role="img"
      aria-label="The network map: internet above, the router at the waist, observed lanes below{undrawnNote}"
    >
      <defs>
        <!-- The host row's clip. Every lane card shares one local
             coordinate space, so one clip serves them all. -->
        <clipPath id="{uid}-hosts">
          <rect x={-cardHalf + cardPad - 2} y="40" width={cardW - 2 * cardPad + 4} height="18" />
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
      {#if tunnelIface}
        <path class="rib" d="M1080 186 C 990 215, 880 240, 830 252" stroke="var(--accent)" stroke-width="1.7" />
        {#if tunnelGhostLane !== null}
          {@const gx = laneX(tunnelGhostLane, zones.length)}
          <path class="rib-ghost" d="M 1100 186 C 945 300, {gx + 95} 385, {gx} 476" />
        {/if}
      {/if}
      {#if lens === 'traffic'}
        {#if eps > 0}
          <circle class="mote" r="2.5" fill="var(--accent)" />
        {/if}

        {#if drawnReality.drawn.length === 0}
          <!-- Before pair-carrying traffic arrives, the lanes' simple
               volume ribs keep the place alive. -->
          {#each zones as z, i (z.id)}
            <path class="rib" d={ribPath(i, zones.length)} stroke={LANE_INKS[i % LANE_INKS.length]} stroke-width="2.4" />
          {/each}
        {/if}

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
          <g
            class="edge-g"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to this pair: {realityLabel(d.r)}"
            onclick={() => openPair(d.r.from, d.r.to, d.r.topPorts)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openPair(d.r.from, d.r.to, d.r.topPorts)
              }
            }}
          >
            <title>{realityLabel(d.r)}</title>
            <path class="edge-hit" d={edgePath(d.line)} />
            <path
              class="redge"
              class:alarm={d.r.verdict === 'unplanned'}
              d={edgePath(d.line)}
              style:stroke-width="{realityWidth(d.r)}px"
              style:stroke={d.r.verdict === 'unplanned' ? undefined : laneInkFor(d.r.from, d.r.to)}
            />
            {#if d.r.drops > 0}
              {@const bar = edgeBarAt(d.line)}
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar" class:alarm-bar={d.r.verdict === 'unplanned'} x1="-7" y1="0" x2="7" y2="0" />
              </g>
            {/if}
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
               what the card below already says (#897 item 2). The
               coverage and policy lenses skip their empty labels the
               same way. -->
          {#if d !== worstUnplanned}
            {@const badge = trafficBadges[di]}
            <g
              class="detail"
              role="button"
              tabindex="0"
              aria-label="Open the stream filtered to this pair: {realityLabel(d.r)}"
              onclick={() => openPair(d.r.from, d.r.to, d.r.topPorts)}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  openPair(d.r.from, d.r.to, d.r.topPorts)
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
        {#if worstUnplanned && worstUnplannedCard}
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
        {/if}
        {#each ghostIntents as g, gi (g.edge.key)}
          {@const badge = trafficBadges[drawnReality.drawn.length + gi]}
          <g class="detail">
            <rect class="edge-plate" x={badge.x - badge.w / 2} y={badge.y - 10} width={badge.w} height="14" rx="4" />
            <text class="edge-badge ghost-t" x={badge.x} y={badge.y} text-anchor="middle">never exercised</text>
          </g>
        {/each}

      {:else if lens === 'coverage'}
        <!-- The coverage paint (#630): every boundary-direction with
             rules, drawn by what it logs. Dark is drawn dark, never
             omitted. Lines first, labels last -- same two-pass split as
             the traffic lens above, same reason (#723). -->
        {#each drawnCoverage.drawn as d, di (d.edge.key)}
          {@const st = coverageOf(d.edge)}
          <g
            class="cov-g"
            class:actionable={isAdmin && st !== 'observed'}
            {...isAdmin && st !== 'observed'
              ? { role: 'button', tabindex: 0, 'aria-label': `Declare or review this gap: ${coverageLabel(d.edge)}` }
              : {}}
            onclick={() => openCoverage(d.edge)}
            onkeydown={(e) => {
              if (e.key === 'Enter') openCoverage(d.edge)
            }}
          >
            <title>{coverageLabel(d.edge)}</title>
            <path class="edge-hit" d={edgePath(d.line)} />
            {#if st === 'observed'}
              <path class="cedge observed" d={edgePath(d.line)} />
            {:else if st === 'quiet'}
              <path class="cedge quiet" d={edgePath(d.line)} />
            {:else}
              <path class="cedge dark" d={edgePath(d.line)} />
            {/if}
          </g>
        {/each}

        {#each drawnCoverage.drawn as d, di (d.edge.key)}
          {@const st = coverageOf(d.edge)}
          {#if st !== 'observed'}
            {@const badge = coverageBadges[di]}
            <g
              class="detail"
              class:actionable={isAdmin}
              {...isAdmin ? { role: 'button', tabindex: 0, 'aria-label': `Declare or review this gap: ${coverageLabel(d.edge)}` } : {}}
              onclick={() => openCoverage(d.edge)}
              onkeydown={(e) => {
                if (e.key === 'Enter') openCoverage(d.edge)
              }}
            >
              <title>{coverageLabel(d.edge)}</title>
              <rect class="edge-plate" x={badge.x - badge.w / 2} y={badge.y - 10} width={badge.w} height="14" rx="4" />
              <text class="edge-badge {st === 'dark' ? 'dark-t' : 'quiet-t'}" x={badge.x} y={badge.y} text-anchor="middle">
                {coverageBadgeText(d.edge)}
              </text>
            </g>
          {/if}
        {/each}

        {#if !policyState.anyPushed}
          <g transform="translate(700 400)">
            <text y="0" text-anchor="middle" class="n-sub">no rule table has been pushed yet — nothing is broken, this lens is waiting for data</text>
            <text y="20" text-anchor="middle" class="n-sub">coverage is read from which pushed rules log; Settings → Run setup… prints the push script</text>
          </g>
        {:else if drawnCoverage.drawn.length === 0}
          <text x="700" y="400" text-anchor="middle" class="n-sub">the pushed table has no forward rules — no boundary-direction to paint</text>
        {/if}
      {:else}
        <!-- Intended-policy edges (#628): what the pushed table says
             may cross, refused where it says it may not. Drawn beneath
             the islands, like the ribs they replace. Lines first, labels
             last -- same two-pass split as the two lenses above (#723). -->
        {#each drawnEdges.drawn as d, di (d.edge.key)}
          {@const bar = edgeBarAt(d.line)}
          <g
            class="edge-g"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to this pair: {edgeLabel(d.edge)}"
            onclick={() => openEdge(d.edge)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openEdge(d.edge)
              }
            }}
          >
            <title>{edgeLabel(d.edge)}</title>
            <path class="edge-hit" d={edgePath(d.line)} />
            <path class="edge" class:refused={!d.edge.accepted} d={edgePath(d.line)} />
            {#if !d.edge.accepted}
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar" x1="-7" y1="0" x2="7" y2="0" />
              </g>
            {:else if d.edge.refused}
              <!-- The pair also carries refusals: the crossing line
                   stands, and the ⊣ tick beside the waist says some of
                   it is turned away. -->
              <g transform="translate({bar.x} {bar.y}) rotate({bar.angle})">
                <line class="edge-bar dim" x1="-5" y1="0" x2="5" y2="0" />
              </g>
            {/if}
          </g>
        {/each}

        {#each drawnEdges.drawn as d, di (d.edge.key)}
          {#if d.edge.accepted || d.edge.refusePorts.length > 0}
            <!-- A port-less refusal's badge would only repeat the bar. -->
            {@const badge = policyBadges[di]}
            <g
              class="detail"
              role="button"
              tabindex="0"
              aria-label="Open the stream filtered to this pair: {edgeLabel(d.edge)}"
              onclick={() => openEdge(d.edge)}
              onkeydown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  openEdge(d.edge)
                }
              }}
            >
              <title>{edgeLabel(d.edge)}</title>
              <rect class="edge-plate" x={badge.x - badge.w / 2} y={badge.y - 10} width={badge.w} height="14" rx="4" />
              <text class="edge-badge" x={badge.x} y={badge.y} text-anchor="middle">{badgeLine(d.edge)}</text>
            </g>
          {/if}
        {/each}

        {#if !policyState.anyPushed}
          <!-- Waiting for data is a state, not a fault -- say so. -->
          <g transform="translate(700 400)">
            <text y="0" text-anchor="middle" class="n-sub">no rule table has been pushed yet — nothing is broken, this lens is waiting for data</text>
            <text y="20" text-anchor="middle" class="n-sub">the policy layer draws what your router intends; Settings → Run setup… prints the push script</text>
          </g>
        {:else if drawnEdges.drawn.length === 0}
          <text x="700" y="400" text-anchor="middle" class="n-sub">the pushed table has no forward rules — nothing crosses between lanes by intent</text>
        {/if}
      {/if}

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
          {#if internetAggregate}
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
      {#if tunnelIface}
        <g transform="translate(1128 132)" class="passive">
          <g class="isl-card">
            <rect class="isl" x="-84" y="-28" width="188" height="56" rx="12" />
            <text x="-66" y="-2" class="n-name tn-name">WireGuard</text>
            {#if tunnelSubnet}
              <text x="-66" y="14" class="n-cidr">{tunnelIface}{` · ${tunnelSubnet}`}</text>
            {:else}
              <!-- One interface's own missing row, not the whole map's
                   degraded state, so this is not `.cidr-deg`'s toggle. -->
              <text x="-66" y="14" class="n-cidr"
                >{tunnelIface}<tspan class="cidr-none">{' · no address pushed'}</tspan></text
              >
            {/if}
            <text x="54" y="-4" class="n-cov {tunnelCovClass}">{tunnelStateLabel}</text>
            {#if tunnelAggregate}
              {@render aggregateBar(tunnelAggregate, -84, 188, 32, 16, { id: tunnelIface, name: tunnelIface })}
            {/if}
          </g>
          <g class="g-dot">
            <!-- Mirrored: the node sits at the right of the stage, so
                 its survey label runs leftward off the dot. -->
            <circle r="8" class="zone-dot" stroke="var(--accent)" />
            <text x="-16" y="4" text-anchor="end" class="zone-label">WireGuard</text>
          </g>
        </g>
      {/if}

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
                }}>Run setup… ▸</tspan
              > adds it</text
            >
          {/if}
        </g>
      </g>

      <!-- The lanes -->
      {#each zones as z, i (z.id)}
        {@const agg = zoneAggregate(z)}
        {@const shown = hostsShown(z)}
        {@const capt = zoneCaption(z.id)}
        {@const ink = LANE_INKS[i % LANE_INKS.length]}
        <g
          transform="translate({laneX(i, zones.length)} 490)"
          class="zone"
          role="button"
          tabindex="0"
          aria-label="Open the stream filtered to {z.name}"
          onclick={() => openZone(z.id)}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              openZone(z.id)
            }
          }}
        >
          <!-- The card. Its width is the lane pitch's own (#699), so
               everything inside it is measured from the card's edge
               rather than from round 30's four fixed lane positions. -->
          <g class="isl-card">
            <rect class="isl" x={-cardHalf} y="0" width={cardW} height="106" rx="12" />
            <circle cx={-cardHalf + cardPad} cy="22" r="3.5" fill={ink} />
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
            {#if shown.hosts.length > 0}
              <!-- Each host is the reach's door (#626): clicking the name
                   recentres on that node rather than opening the zone.
                   The name is the whole target. Rounds 23 and 30 both
                   draw this list as plain names (round-23:871,
                   round-30's own `n-sub` line); #648's "node symbols
                   bigger" was about the map's circles -- the zone dots
                   and station rings -- and a per-name text dot was read
                   into it that no round ever drew (Fable 5, #715 item
                   10). How many names are drawn is the card's own width
                   budget (#699); the clip is the backstop, so an
                   unusually long name cannot reach the neighbouring lane
                   whatever the estimate said. -->
              <text x={-cardHalf + cardPad} y="52" class="n-hosts" clip-path="url(#{uid}-hosts)">
                {#each shown.hosts as h, hi (h.ip)}
                  {#if hi > 0}<tspan> · </tspan>{/if}
                  <tspan
                    class="host-link"
                    role="button"
                    tabindex="0"
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
                    }}>{h.label}</tspan>
                {/each}
                {#if shown.more > 0}<tspan> · +{shown.more}</tspan>{/if}
              </text>
            {/if}
            {#if capt}
              {@const [badge, detail] = capt.split(' — ')}
              <!-- Two lines, badge over detail (#682, ratified round-29):
                   collapsing them into one crammed sentence was the
                   defect, not a design choice -- the badge's own y moves
                   down 4px when a detail line follows it, matching the
                   scene's own Guest card exactly. -->
              <text x={-cardHalf + cardPad} y={detail ? 74 : 70} class="n-cov {covClass(capt)}">{badge}</text>
              {#if detail}
                <text x={-cardHalf + cardPad} y="88" class="n-sub">{detail}</text>
              {/if}
            {/if}
            <!-- Round 30's zone card carries name, subnet, hosts and the
                 coverage badge, and stops there (the-whole.html:1002-1008).
                 A fifth "N events this window" line was drawn here and the
                 mockup draws it nowhere, so it is off (#715 item 9). The
                 count itself stays: `zones.svelte.ts:161` sorts the lane row
                 by it, so it is load-bearing data, not dead code. -->
            {#if agg}
              <!-- Flush with the card and 16 tall (#699; round 30's
                   `translate(-108 110)` at 216x16, the-whole.html:
                   1009-1015). The old 188x12 floated inset under a
                   216-wide card and read as unrelated furniture. -->
              {@render aggregateBar(agg, -cardHalf, cardW, 110, 16, z)}
            {/if}
          </g>
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
          {#if svc}
            <!-- "Ports floating in the wind" (owner, #723): the port list
                 named nothing it sat above -- a 24-unit gap to the card
                 with no line, no plate, nothing between them. Pulled in
                 to a small, proximate gap and given a leader tick down to
                 the card's own top edge (y=490), the tie every other
                 label on this map already has via its plate or its line. -->
            <text x={laneX(i, zones.length)} y="472" text-anchor="middle" class="svc-t">{svc}</text>
            <line class="svc-leader" x1={laneX(i, zones.length)} y1="477" x2={laneX(i, zones.length)} y2="489" />
          {/if}
        {/each}
      </g>
      <g class="cli">
        {#each zones as z, i (z.id)}
          {@const cx = laneX(i, zones.length)}
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
              <path class="cli-spoke" d="M{cx} 636 C {cx - 6} 644, {cx + 6} 647, {cx} 655" stroke={ink} />
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
              <path class="cli-spoke" d="M{cx} 636 C {cx} 644, {x} 644, {x} 650" stroke={ink} />
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

      {#if zones.length === 0}
        <!-- The honest empty state: the place before the data. -->
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
           removed survey dot used to follow. Left alongside the lens's
           own edge lines above rather than reconciling the two
           coordinate systems -- that reconciliation is #726's, still
           open. -->
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
        {#each ground.roads as r (r.id)}
          <path class="gf-road gf-road-{r.k}" d={'M' + r.pts.map((p) => `${R2(FX(flatCam, p[0]))} ${R2(FY(flatCam, p[1]))}`).join(' L')} />
        {/each}
        {#each ground.nodes as n (n.id)}
          {@const nx = FX(flatCam, n.u)}
          {@const ny = FY(flatCam, n.v)}
          <circle class="gf-node" cx={nx} cy={ny} r={Math.max(4, n.R * flatCam.S * 0.6)} />
          <text class="gf-node-label" x={nx} y={ny - n.R * flatCam.S * 0.6 - 4} text-anchor="middle">{n.name}</text>
        {/each}
        {#each ground.districts as d (d.id)}
          {@const gx = FX(flatCam, d.u)}
          {@const gy = FY(flatCam, d.v)}
          {@const gr = Math.max(30, d.r * flatCam.S * 0.9)}
          {@const total = d.buildings.length + d.more}
          <g
            class="gf-card"
            class:dark={d.dark}
            transform="translate({R2(gx)} {R2(gy)})"
            role="button"
            tabindex="0"
            aria-label="Open the stream filtered to {d.name}"
            onclick={() => openZone(d.id)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openZone(d.id)
              }
            }}
          >
            <rect class="gf-plate" x={-gr} y={-gr * 0.42} width={gr * 2} height={gr * 0.84} rx="10" stroke={LANE_INKS[d.ink % LANE_INKS.length]} />
            <text class="n-name" x={-gr + 12} y={-gr * 0.42 + 20}>{d.name}</text>
            <text class="n-cidr" x={gr - 12} y={-gr * 0.42 + 20} text-anchor="end">{d.cidr ?? 'from boundaries'}</text>
            <text class="gf-count" x={-gr + 12} y={-gr * 0.42 + 42}>{total} host{total === 1 ? '' : 's'}</text>
            {#if d.dark}
              <text class="zone-state bad" x={gr - 12} y={-gr * 0.42 + 42} text-anchor="end">DARK</text>
            {/if}
          </g>
        {/each}
      </g>
      </g>
    </svg>
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
         ratified record gives both views the same lens tabs, header,
         badges and callout wording -- all the 2D map's, drawn once
         above and reused here; threading Topography's own `lens` down
         is "whatever lens plumbing City.svelte already has" until the
         city grows lenses of its own. `ground` is the one shared layout
         (#852) computed above, so this side and the zones stop can
         never disagree on a position. `initialS`/`initialCentre` and
         the two `on*` callbacks are the pan/reach carry described by
         crossAltitudeCentre's own doc comment, above. -->
    <City
      stop={cityStop}
      lens={lens}
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
      <svg viewBox="0 0 1400 620" preserveAspectRatio="xMidYMid meet">
        <circle cx={MX} cy={MY} r={MR} class="membrane" />
        <text x={MX} y="502" text-anchor="middle" class="n-sub">
          the membrane — lane-mates inside talk freely; every crossing needs a rule, per direction
        </text>

        {#each reachSummary.strands as s (s.key)}
          {@const p = membranePoint(s.counterpart, s.outcome, s.direction)}
          <path
            class="strand"
            d={strandPath(s.counterpart, s.outcome, s.direction)}
            stroke={s.outcome === 'accepted' ? 'var(--accept)' : 'var(--alarm)'}
            stroke-width={s.outcome === 'accepted' ? 2.2 : 2}
          />
          {#if s.outcome === 'blocked'}
            <g transform="translate({p.x} {p.y}) rotate({p.angle})">
              <line x1="-8" y1="0" x2="8" y2="0" stroke="var(--alarm)" stroke-width="3" />
            </g>
            <!-- Labels stagger by direction and outcome so no two
                 strands of one crossing ever overprint. -->
            <!-- The blocked label is the composer's door (scene 4): a
                 denial becomes a rule in two clicks. -->
            <text
              x={p.x + 14}
              y={p.y + (s.direction === 'in' ? 30 : -22)}
              class="chip-t alarm-t strand-door"
              role="button"
              tabindex="0"
              aria-label="Draft the rule: what may it say on this strand?"
              onclick={(e) => {
                e.stopPropagation()
                openCompose(s)
              }}
              onkeydown={(e) => {
                if (e.key === 'Enter') {
                  e.stopPropagation()
                  openCompose(s)
                }
              }}
            >
              ⊣ {s.direction === 'out' ? 'dies at the membrane' : 'refused at the membrane'} · {portsLine(s.ports)} · {s.count}×{s.refusedBy
                ? ` · ${s.refusedBy}`
                : ''}
            </text>
          {:else}
            <text x={p.x + 14} y={p.y + (s.direction === 'in' ? 14 : -6)} class="chip-t ok-t">
              {s.direction === 'out' ? '→' : '→ in'} {portsLine(s.ports)} · {s.count}×
            </text>
          {/if}
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
          <path d="M 180 574 C 460 622, 940 622, 1220 574" fill="none" stroke="var(--hair-2)" stroke-width="1.1" />
          <text x="1250" y="580" class="n-sub">INTERNET</text>
          {#each reachSummary.strands.filter((s) => s.counterpart === 'internet').slice(0, 3) as s, si (s.key)}
            <text
              x={280 + si * 300}
              y={si % 2 === 0 ? 566 : 588}
              class="n-sub"
              fill={s.outcome === 'accepted' ? 'var(--fg-muted)' : 'var(--alarm)'}
            >
              {s.peers.slice(0, 1).join('')} {portsLine(s.ports)}
            </text>
          {/each}
        {/if}

        {#if reachSummary.strands.length === 0}
          <!-- #626's honest empty: never a spinner standing in for an answer. -->
          <text x={MX} y={MY + 80} text-anchor="middle" class="n-sub">nothing observed this window</text>
        {/if}
      </svg>
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

  {#if declarePanel && lens === 'coverage' && !reach}
    {@const existing = coverageState.byKey.get(declarePanel.key)}
    <!-- The declare-a-gap panel (#392): one acknowledgement, with its
         reason, on the record. Reached only by an admin clicking a
         non-observed edge. -->
    <div class="declare" role="dialog" aria-label="Declare this gap intentionally quiet">
      <p class="d-pair">{pairName(declarePanel.from, declarePanel.to)}</p>
      {#if existing}
        <p class="d-meta">declared quiet by <b>{existing.declaredBy}</b> · {new Date(existing.declaredAt).toLocaleDateString()}</p>
      {:else}
        <p class="d-meta">dark — nothing on this boundary-direction logs. If that is a choice, say why once and it stays said.</p>
      {/if}
      <textarea rows="2" placeholder="why this gap is intentional…" bind:value={declareReason}></textarea>
      {#if coverageState.error}
        <p class="d-error">{coverageState.error}</p>
      {/if}
      <div class="d-row">
        <button class="d-primary" disabled={declareBusy || !declareReason.trim()} onclick={submitDeclaration}>
          {existing ? 'Update the reason' : 'Declare intentionally quiet'}
        </button>
        {#if existing}
          <button class="d-danger" disabled={declareBusy} onclick={removeDeclaration}>Remove — it goes dark again</button>
        {/if}
        <button class="d-quiet" onclick={() => (declarePanel = null)}>Cancel</button>
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

  .crumb .path {
    font-size: 18px;
    font-weight: 550;
    letter-spacing: -0.01em;
    color: var(--fg);
  }

  .crumb .sep {
    color: var(--fg-dim);
    font-weight: 300;
    padding: 0 8px;
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

  /* The lens selector (#682, ported from the scene's `.wlens2`): the
     bottom-left bar, exact position and type from round 29 -- not an
     approximation of it as a top-right tab strip. */
  .wlens2 {
    position: absolute;
    bottom: 12px;
    left: 26px;
    z-index: 2;
    display: flex;
    gap: 14px;
    font: 500 10.5px var(--font-sans);
    color: var(--fg-dim);
    opacity: 0.8;
  }

  .wlens2 button {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }

  .wlens2 span {
    cursor: default;
  }

  .wlens2 .on {
    color: var(--fg);
  }

  /* The three tabs and the two overlays, split by a hairline
     (round-39:943). The row is one flex line; the two groups are their
     own so the divider sits between them rather than between any two
     controls. */
  .wl-tabs,
  .wl-overlays {
    display: flex;
    gap: 14px;
  }

  .wl-div {
    width: 1px;
    align-self: stretch;
    background: var(--hair-2);
  }

  /* An overlay's count wears the alarm ink, as round 39 draws it, but
     only the digits -- the word stays the row's own colour so an
     overlay that is merely available does not read as an alarm. */
  .wl-ov.on {
    color: var(--fg);
  }

  .wl-ov.on .ov-n {
    color: var(--alarm);
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

  .n-hosts {
    fill: var(--fg-muted);
    font-size: 10px;
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

  /* --- the policy lens (#628) -------------------------------------------- */
  .edge {
    fill: none;
    stroke: var(--fg-muted);
    stroke-width: 1.8;
    stroke-linecap: round;
    opacity: 0.6;
  }

  .edge.refused {
    stroke-dasharray: 5 5;
  }

  /* The invisible hit area a 1.8px line cannot be. */
  .edge-hit {
    fill: none;
    stroke: transparent;
    stroke-width: 14;
  }

  .edge-g {
    cursor: pointer;
  }

  .edge-g:hover .edge,
  .edge-g:focus-visible .edge {
    stroke: var(--fg);
    opacity: 0.95;
  }

  .edge-g:focus-visible {
    outline: none;
  }

  .edge-bar {
    stroke: var(--fg-muted);
    stroke-width: 2.6;
  }

  /* --- the reality overlay (#629) ---------------------------------------- */
  .redge {
    fill: none;
    stroke: var(--fg-muted);
    stroke-linecap: round;
    opacity: 0.65;
  }

  .redge.alarm {
    stroke: var(--alarm);
    opacity: 0.85;
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

  /* --- the coverage paint (#630) ----------------------------------------- */
  .cedge {
    fill: none;
    stroke-linecap: round;
  }

  .cedge.observed {
    stroke: var(--fg-muted);
    stroke-width: 2;
    opacity: 0.6;
  }

  /* Dark is drawn dark: a dotted line in the darkest ink, labelled. */
  .cedge.dark {
    stroke: var(--fg);
    stroke-width: 1.6;
    stroke-dasharray: 2 5;
    opacity: 0.7;
  }

  .dark-t {
    fill: var(--fg);
    opacity: 0.8;
  }

  .n-cov {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.08em;
  }

  /* The coverage badge's colour is not decoration -- it is the same
     read as the zone's edges on the map (#682, ratified round-29). */
  .n-cov.cov-l {
    fill: var(--accept);
  }

  .n-cov.cov-q {
    /* fg-dim on the card's --bg-elevated reads ~3.1:1 (#715); fg-muted
       clears the 4.5:1 floor at ~7.4:1. */
    fill: var(--fg-muted);
  }

  .n-cov.cov-d {
    fill: var(--alarm);
  }

  /* Intentionally quiet: muted and named -- calmer than dark, dimmer
     than observed. */
  .cedge.quiet {
    stroke: var(--fg-dim);
    stroke-width: 1.6;
    stroke-dasharray: 6 4;
    opacity: 0.5;
  }

  .quiet-t {
    /* fg-dim on the edge-plate's --bg reads ~3.3:1 (#715); fg-muted
       clears the 4.5:1 floor at ~8:1. */
    fill: var(--fg-muted);
    font-style: italic;
  }

  .cov-g.actionable {
    cursor: pointer;
  }

  .cov-g.actionable:hover .cedge,
  .cov-g.actionable:focus-visible .cedge {
    opacity: 1;
  }

  .cov-g:focus-visible {
    outline: none;
  }

  .declare {
    position: absolute;
    left: 24px;
    bottom: 34px;
    z-index: 3;
    width: 320px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 14px 16px;
  }

  .d-pair {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg);
  }

  .d-meta {
    margin: 0;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .d-meta b {
    color: var(--fg-muted);
  }

  .declare textarea {
    resize: vertical;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    color: var(--fg);
    font: inherit;
    font-size: 12.5px;
    padding: 8px 9px;
  }

  .declare textarea:focus {
    outline: none;
    border-color: var(--accent);
  }

  .d-error {
    margin: 0;
    font-size: 11.5px;
    color: var(--reject);
  }

  .d-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .d-row button {
    border-radius: 7px;
    font-size: 11.5px;
    padding: 6px 10px;
    cursor: pointer;
    border: 1px solid var(--border);
    background: transparent;
    color: var(--fg-muted);
  }

  .d-primary {
    background: var(--accent) !important;
    border-color: var(--accent) !important;
    color: var(--bg) !important;
    font-weight: 600;
  }

  .d-danger:hover {
    color: var(--reject);
    border-color: var(--reject);
  }

  .d-row button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* --- the composer (round 2 scene 4) ------------------------------------ */
  .strand-door {
    pointer-events: auto;
    cursor: pointer;
  }

  .strand-door:hover,
  .strand-door:focus-visible {
    text-decoration: underline;
  }

  .strand-door:focus,
  .strand-door:focus-visible {
    outline: none;
  }

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

  .edge-bar.dim {
    opacity: 0.55;
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

  .strand {
    fill: none;
    stroke-linecap: round;
    opacity: 0.8;
  }

  .cluster {
    fill: var(--glass);
    stroke: var(--hair-2);
  }

  .chiprow {
    font-family: var(--font-mono);
    font-size: 10px;
  }

  .chip-t {
    font-family: var(--font-mono);
    font-size: 9px;
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

  .dial svg {
    width: 56px;
    height: 56px;
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
  .camera .cli {
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

  .camera.cam-zones .isl-card,
  .camera.cam-zones .detail {
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
  .zone-state {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 700;
    letter-spacing: 0.1em;
    fill: var(--fg-dim);
  }

  .zone-state.bad {
    fill: var(--alarm);
  }

  .gf-river {
    fill: var(--fg-dim);
    fill-opacity: 0.08;
    stroke: var(--fg-dim);
    stroke-opacity: 0.3;
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
</style>
