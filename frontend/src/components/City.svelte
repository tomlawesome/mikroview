<script lang="ts">
  // The city (#863): the same estate the 2D stops draw, in isometric.
  // One map, four cameras -- city, borough, district, street -- each a
  // height over the same ground (lib/city/project.ts), with free pan
  // and a minimap showing where the viewport is. The geometry is drawn
  // once per stop at that stop's height with the camera at the origin;
  // pan and the move between stops are a single transform on the group,
  // so a drag never rebuilds a path. Round 40's isometric.html is the
  // drawing; the model behind it lives in lib/city and is tested there.
  //
  // What stands on the district plate comes from one place, symbolFor
  // (#864 replaces the plain blocks). Rule gates and walls are #865's;
  // the river and bridges are #866's; standing on a building (#868) is
  // this file's own "the reach" section below. #986 dropped height and
  // the plinth (#867): devices sit flat on their district plate.
  import { tick, untrack } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { dossierState } from '../lib/dossier.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { tunnelsState } from '../lib/tunnels.svelte'
  import { policyState } from '../lib/policy.svelte'
  import { coverageState } from '../lib/coverage.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import { logEveryRuleNavState } from '../lib/logEveryRuleNav.svelte'
  import { addressInCidr, parseCidr } from '../lib/addressMatch'
  import { realityEdges } from '../lib/reality'
  // The decommission ghost (#460, round 55), on the city's own
  // geometry: a plate with no fill, a dashed wall all round, its
  // last-known hosts faded inside, and the same card the flat map draws.
  import DecommissionCard from './DecommissionCard.svelte'
  import { decommissionsState } from '../lib/decommission.svelte'
  import { GHOST_INK, boroughLabel, ghostNote, ghostStateOf, stragglerCallout } from '../lib/decommission'
  import type { DecommissionSighting, DecommissionWatch, GhostState } from '../lib/types'
  import { symbolFor } from '../lib/city/blocks'
  import { markFor, type BuildingMark } from '../lib/city/marks'
  import { buildingDepth, paintOrder, pieceDepth } from '../lib/city/depth'
  import { cityInputFrom, ghostCityZones } from '../lib/city/input'
  import { layoutGround } from '../lib/city/layout'
  import {
    IK,
    R2,
    SREF,
    STAGE_H,
    STAGE_W,
    STOP_HEIGHT,
    VK,
    ZK,
    X,
    Y,
    cam,
    cityFitS,
    clampCentre,
    clearDropLabels,
    diamond,
    ease,
    gbox,
    groundAt,
    lerpCam,
    minimapCam,
    reducedMotion,
    viewportRect,
    type BoxFaces,
    type Cam,
    type Pt,
    type Stop,
  } from '../lib/city/project'
  import { gateToward, lerpP, roadPieces, type Entity, type Gate } from '../lib/city/roads'
  import { hostSubject, reachFor, reachLineSummary, type ReachStrand, type ReachSubject, type ReachSummary } from '../lib/reach'
  import { composeCommand, reachComposeInput } from '../lib/compose'
  import { riverScene } from '../lib/city/river'
  import { P, type Paint } from '../lib/city/paint'
  import { bridgeStateLabel } from '../lib/city/tunnelState'
  import { deviceKindFor } from '../lib/city/deviceKind'
  import { deviceScale, deviceStampAttrs, type DeviceStampAttrs } from '../lib/city/devices'
  import { faceCoverage, faceOf, facePoint, wallPiece, wallSegments, GATE_HALF_WIDTH, WALL_H, type WallBreak, type WallSide } from '../lib/city/walls'
  import { worseCoverage } from '../lib/city/gates'
  import { cardSize, drawnPathRects, drawnRect, grace, mapRect, placeCard, stageRect, unitMapper, watchCardSize, type Placement, type Rect } from '../lib/cardAnchor'
  import type { Coverage } from '../lib/coverageRule'
  import { authState } from '../lib/auth.svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { portFilterState } from '../lib/portFilter.svelte'
  import { mapTraceState } from '../lib/mapTrace.svelte'
  import { doorAccepts, doorHalf, emptyNote, litRibs, plaqueTally } from '../lib/portFilter'
  import type { PortDoor } from '../lib/api'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { HOST_QUIET_AFTER_MS, hostsState, presenceOf } from '../lib/hosts.svelte'
  import { baselineState } from '../lib/baseline.svelte'
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
  import {
    EMPTY_ROAD_BASELINE,
    addressName,
    endName,
    portWords,
    roadEnds,
    rollUpRoads,
    verdictWords,
    type RoadBaselineEntry,
    type RoadRing,
  } from '../lib/city/baselineRoads'
  import { EMPTY_REACH_LANES, rollUpLanes, type LaneSubject, type ReachLaneEntry } from '../lib/city/reachRoads'
  import { formatHM } from '../lib/format'
  import type { OffBaselineLine } from '../lib/baseline'
  import { hostMarksFrom, presenceNote, quietFor, type CityHost, type HostPresence } from '../lib/city/presence'
  import CityDeviceDefs from './CityDeviceDefs.svelte'
  import TraceCrumb from './TraceCrumb.svelte'
  import type { Building, CityPeer, District, DistrictGate, Ground, Road, RoadKind } from '../lib/city/types'

  let {
    stop,
    ground: groundProp,
    initialS,
    initialCentre,
    onCameraChange,
    onStandChange,
  }: {
    stop: Stop
    ground?: Ground
    /** The pan this side had when the slider last crossed away from it
     * (#869): Topography saves what onCameraChange reports below and
     * hands it back here across City's own mount/unmount, since a fresh
     * mount would otherwise always start centred on the stop's default. */
    initialS?: number
    initialCentre?: Pt
    onCameraChange?: (s: number, centre: Pt) => void
    /** Which building (if any) the city currently stands on, so a
     * crossing back to the 2D side can open the same host's reach there
     * (#869) -- null when nothing is stood on. */
    onStandChange?: (building: Building | null) => void
  } = $props()

  /** Unique per mount, so the declare form's label and input are the 2D
   * map's line for line rather than a hard-coded id that would collide
   * if this were ever mounted twice (#1031). */
  const uid = $props.id()

  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--lane-5)']
  const VERDICT: Record<RoadKind, string> = { a: 'var(--accept)', d: 'var(--drop)', x: 'var(--alarm)', q: 'var(--fg-dim)' }
  const VOID = '#080d18'
  const MOVE_MS = 620

  // Round 49's three treatments (#1016), named once so the wall, the
  // gate posts and the bridge deck all read from the same place -- and
  // so the city and the 2D map cannot drift apart on what dark looks
  // like. Quiet is the page ink (white, translucent); dark is the third
  // ink (grey), and only dark is ever dashed.
  const COVERAGE_INK: Record<Coverage, string> = { logged: 'var(--accent)', quiet: 'var(--fg)', dark: 'var(--fg-dim)' }
  const WALL_TREATMENT: Record<Coverage, { back: number; fo: number; so: number; sw: number; dash?: string }> = {
    logged: { back: 0.92, fo: 0.3, so: 0.6, sw: 0.5 },
    quiet: { back: 0.7, fo: 0.16, so: 0.4, sw: 0.5 },
    dark: { back: 0.7, fo: 0.12, so: 0.45, sw: 0.6, dash: '3 3' },
  }
  /** The ghost's own wall treatment (#460, round 55), one per state:
   * lighter than any live wall and dashed all round, because the wall
   * of a segment nothing routes is a boundary the operator remembers
   * rather than one anything is holding. Broken sits heaviest of the
   * three -- it is the one asking for something. */
  const GHOST_WALL: Record<GhostState, { back: number; fo: number; so: number; sw: number; dash?: string }> = {
    none: { back: 0.14, fo: 0.11, so: 0.45, sw: 0.6, dash: '3 4' },
    holding: { back: 0.16, fo: 0.12, so: 0.5, sw: 0.6, dash: '3 4' },
    broken: { back: 0.2, fo: 0.15, so: 0.6, sw: 0.7, dash: '3 4' },
  }
  /** A gate post: half its ground extent, and how tall it stands -- half
   * again the wall's own height (walls.ts's WALL_H), the mockup's ratio. */
  const GATE_POST_HALF = 0.6
  const GATE_POST_H = 2.25
  /** How tall a door stands (#1055) -- a shade proud of the gate post it
   * stands beside, so the two read as a door in a gate rather than as
   * four posts in a row. */
  const DOOR_H = 2.6

  /* ---------------- the model ---------------- */

  const primaryDevice = $derived.by(() => {
    const list = appState.devices
    if (list.length === 0) return null
    const configured = list.filter((d) => d.configured)
    const pool = configured.length > 0 ? configured : list
    return [...pool].sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0]
  })

  /* ---------------- living hosts (round 49, #1016) ---------------- */

  // Presence is read against the clock, and the clock moves on its own:
  // a host does not become quiet because anything happened, but because
  // 24 hours passed with nothing happening. So the map keeps its own
  // tick. A minute is far finer than a 24-hour window needs and costs
  // one derivation; anything coarser would leave a building drawn live
  // for up to its own period after it stopped being so.
  const PRESENCE_TICK_MS = 60_000
  let nowTick = $state(Date.now())

  $effect(() => {
    // No reactive read here on purpose: the register is fetched once on
    // mount and again after every write (hostsState.mark/unmark refresh
    // themselves), and reading hostsState.hosts here would make this
    // effect its own trigger.
    void hostsState.refresh()
    // The baseline register, on the same tick and for the same reason:
    // brightness is a statement about today, and a line that stops being
    // off-baseline (because someone said it was expected, here or on the
    // 2D map) should stop being bright without a reload.
    void baselineState.refresh()
    const t = setInterval(() => {
      nowTick = Date.now()
      // Re-read on the same tick, which is what makes "it comes back by
      // itself" true without a reload: the server clears a dismissal
      // when the feed hears the host again, and this is where the map
      // finds out. Nothing in the browser has to model that rule.
      void hostsState.refresh()
      void baselineState.refresh()
    }, PRESENCE_TICK_MS)
    return () => clearInterval(t)
  })

  /** The register, reduced to what the ground plan needs and read
   * against the current tick. `presenceOf` is the shared rule -- the 2D
   * map answers the same question with the same function, so a host
   * cannot read live on one surface and quiet on the other. */
  const registeredHosts = $derived<CityHost[]>(
    hostsState.hosts.map((h) => ({
      key: h.key,
      ip: h.ip,
      label: h.label ?? '',
      presence: presenceOf(h, nowTick, HOST_QUIET_AFTER_MS),
      lastSeen: h.lastSeen,
      firstSeen: h.firstSeen,
      events: h.events,
      reason: h.mark?.reason ?? null,
      markedBy: h.mark?.by ?? null,
      markedAt: h.mark?.at ?? null,
      // The register knows nothing about flags or watchers; cityInputFrom
      // puts the marks on from `hostMarks` below, for registered and
      // buffer-only hosts alike.
      flags: 0,
      watch: 0,
      spike: false,
    })),
  )

  /** What each address is flagged and watched with (#981). One reading
   * for both surfaces -- `hostMarksFrom` is the same function the 2D
   * map's own row calls -- so a host cannot be red on one side and
   * plain on the other. Only used by the fallback ground below: in the
   * app Topography hands the ground down with the marks already on it. */
  const hostMarks = $derived(hostMarksFrom(flagsState.list, watchlistState.entries))

  /* ---------------- the ghost district (#460, round 55) ---------------- */

  // The map's own clock, on the same one-minute beat the flat map keeps:
  // a ghost's tally counts hours of quiet, and an hour that has passed
  // has to show without waiting for traffic to arrive.
  let nowMs = $state(Date.now())
  $effect(() => {
    const t = setInterval(() => (nowMs = Date.now()), 60_000)
    return () => clearInterval(t)
  })

  // The watch or the offer behind a ghost district, by the boundary the
  // district is keyed on -- the same identity the topography keys a zone
  // on, so an accepted offer's ghost lands where its district stood.
  const ghostWatchFor = (id: string) => decommissionsState.ghosts.find((w) => w.interface === id) ?? null
  const ghostOfferFor = (id: string) => decommissionsState.offers.find((o) => o.interface === id) ?? null

  const ground: Ground = $derived(
    groundProp ??
      layoutGround(
        cityInputFrom(
          appState.devices,
          zonesState.zones,
          appState.events,
          realityEdges(appState.events, policyState.edges, policyState.anyPushed),
          policyState.edges,
          policyState.anyPushed,
          primaryDevice?.id ?? null,
          zonesState.wanInterface,
          tunnelsState.list,
          policyState.pushed,
          new Set(coverageState.byKey.keys()),
          registeredHosts,
          hostMarks,
          ghostCityZones(decommissionsState.offers, decommissionsState.ghosts, nowMs, primaryDevice?.id ?? ''),
        ),
      ),
  )


  const inkOf = (d: District) => LANE_INKS[d.ink % LANE_INKS.length]
  const districtOf = (id: string | null) => (id ? (ground.districts.find((d) => d.id === id) ?? null) : null)
  const allBuildings = $derived<Building[]>([...ground.nodes, ...ground.districts.flatMap((d) => d.buildings)])


  // The ghost the operator has opened a card on, and the ghost whose
  // watch a straggler has just broken. One card and one alarm road at a
  // time, the same rule the flat map keeps: two cards on one ghost would
  // be the map saying two things about the same place.
  let openGhostId = $state<string | null>(null)
  let ghostBusy = $state(false)
  let ghostError = $state<string | null>(null)

  const ghostDistricts = $derived(ground.districts.filter((d) => d.ghost))
  const brokenGhostDistrict = $derived(
    ghostDistricts.find((d) => {
      const w = ghostWatchFor(d.id)
      return w !== null && ghostStateOf(w, nowMs) === 'broken' && w.lastStraggler
    }) ?? null,
  )

  // The straggler's road: from the ghost's gate to the gate of whatever
  // district its peer stands in. Drawn only where a pushed address table
  // claims the peer -- a road to a district that might not be the right
  // one would be a guess, and the callout still names the line.
  const stragglerRoad = $derived.by((): {
    g: District
    w: DecommissionWatch
    st: DecommissionSighting
    from: Pt | null
    to: Pt | null
  } | null => {
    const g = brokenGhostDistrict
    const w = g ? ghostWatchFor(g.id) : null
    const st = w?.lastStraggler
    if (!g || !w || !st?.peer) return null
    const peer = st.peer
    const target = ground.districts.find((d) => {
      if (d.ghost || !d.cidr) return false
      const parsed = parseCidr(d.cidr)
      return parsed !== null && addressInCidr(peer, parsed)
    })
    if (!target) return { g, w, st, from: null, to: null }
    return { g, w, st, from: gateToward(g, [target.u, target.v]).p, to: gateToward(target, [g.u, g.v]).p }
  })

  // What card, if any, this surface is showing. Same precedence as the
  // flat map's: the straggler the operator opened, then the ghost's own
  // card, then an offer nobody has answered.
  const cityDecommCard = $derived.by((): { kind: 'offer' | 'ghost' | 'straggler'; district: District } | null => {
    const openD = ghostDistricts.find((d) => d.id === openGhostId) ?? null
    if (openD) {
      const w = ghostWatchFor(openD.id)
      if (w?.lastStraggler && ghostStateOf(w, nowMs) === 'broken') return { kind: 'straggler', district: openD }
      if (w) return { kind: 'ghost', district: openD }
    }
    const offered = ghostDistricts.find((d) => ghostOfferFor(d.id) !== null)
    if (offered) return { kind: 'offer', district: offered }
    return null
  })

  let gcardEl: HTMLElement | null = $state(null)
  let ghostPlace = $state<Placement | null>(null)
  let ghostCardTick = $state(0)

  // Placed beside the ghost, with the heavier leader the card itself
  // draws (round 55's second fix): a ghost is faint by design, so a
  // hairline to one reads as nothing at all.
  $effect(() => {
    const open = cityDecommCard
    const c = viewCam
    const svg = svgEl
    const host = cityEl
    const card = gcardEl
    void effectiveStop
    void stageTick
    void ghostCardTick
    if (!open || !c || !svg || !host || !card) {
      ghostPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      ghostPlace = null
      return
    }
    const d = open.district
    const anchor = map({ x: X(c, d.u), y: Y(c, d.v) })
    const avoid = [mapRect(map, plateBox(d))]
    const softAvoid = ground.districts.filter((x) => x.id !== d.id).map((x) => mapRect(map, plateBox(x)))
    ghostPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid, prefer: ['left', 'right', 'top'] })
  })

  $effect(() => {
    const card = gcardEl
    if (!card) return
    return watchCardSize(card, () => ghostCardTick++)
  })

  async function answerGhostOffer(d: District, yes: boolean) {
    const o = ghostOfferFor(d.id)
    if (!o) return
    ghostBusy = true
    const err = yes ? await decommissionsState.accept(o) : await decommissionsState.dismiss(o)
    ghostBusy = false
    ghostError = err
  }

  async function forceRemoveGhostDistrict(d: District, why: string) {
    const w = ghostWatchFor(d.id)
    if (!w) return
    ghostBusy = true
    const err = await decommissionsState.force(w.id, why)
    ghostBusy = false
    ghostError = err
    if (!err) openGhostId = null
  }



  /* ---------------- brightness by baseline (#1016) ---------------- */

  // "Colour is the verdict; brightness is the baseline" (DESIGN.md, "The
  // always-on picture"). Nothing is ever removed from the map -- an
  // established road recedes, it does not disappear -- so this is a
  // sieve rather than a filter.
  //
  // The roll-up lives here, in one $derived, for cost: the scene below
  // is rebuilt on every camera frame, and this is not. It depends only
  // on the ground and on the off-baseline document, so panning and
  // zooming never re-run it; when it does run it is O(lines + roads),
  // and the per-road drawing then reads it with a single Map lookup.
  /** Opacity to two decimals. R2 is the coordinate rounder and answers
   *  to one, which is right for a pixel and wrong for an opacity: the
   *  baseline's dim road is .26, and a tenth of a unit would draw it at
   *  .3 -- a value nobody ratified, and a visibly shallower gap between
   *  established and off-baseline than the design asks for. */
  const RO = (n: number): number => Math.round(n * 100) / 100

  const roadBaseline = $derived(
    baselineState.off.lines.length === 0
      ? EMPTY_ROAD_BASELINE
      : rollUpRoads(ground, baselineState.off, zonesState.wanInterface),
  )

  /* ---------------- the camera ---------------- */

  // Standing on a building (#868): a click or Enter drops the camera to
  // the street stop centred on it, whatever stop the slider is actually
  // at. `effectiveStop` is what every rendering decision below reads
  // instead of the bare `stop` prop, so standing reuses exactly the
  // street stop's own geometry, lanes and labels rather than a second
  // copy of them. `savedS`/`savedCentre` are the camera as it stood the
  // instant before standing -- not recomputed on surfacing, so Esc and
  // the crumb land on the exact pan position, never a default.
  //
  // Round 49 widens what can be stood on: "Clicking anything on the
  // map, at any stop, opens its reach -- a building, a host dot, a
  // district, a road" (DESIGN.md "The reach"). `kind` says which, and
  // everything that differs between them -- the height the camera drops
  // to, what counts as "its own" roads, what the crumb can honestly say
  // -- reads it. Surfacing is one path for all three.
  type StandKind = 'building' | 'district' | 'road'
  interface Stand {
    kind: StandKind
    districtId: string | null
    id: string
    savedS: number
    savedCentre: Pt
  }
  let stand = $state<Stand | null>(null)
  /** A district's reach frames the district; a building's and a road's
   * drop to the street, where the buildings and their labels are. */
  const effectiveStop = $derived<Stop>(stand ? (stand.kind === 'district' ? 'district' : 'street') : stop)

  // Geometry is built at the stop's own height with the camera at the
  // origin; S and centre are what the viewer sees, and the group's
  // transform reconciles the two.
  const Sgeom = $derived(STOP_HEIGHT[effectiveStop])
  const geomCam = $derived<Cam>({ S: Sgeom, ox: 0, oy: 0 })

  let S = $state(STOP_HEIGHT.city)
  let centre = $state<Pt>([0, 0])
  let started = false
  let lastStop: Stop | null = null
  let anim: number | null = null
  let svgEl: SVGSVGElement | undefined = $state()

  /** The height a stop's camera moves to: the city stop opens wide
   * enough to take in the whole estate (#979, cityFitS -- capped so it
   * never zooms in), every other stop keeps its fixed height. */
  const stopS = (s: Stop): number => (s === 'city' ? cityFitS(ground.bounds) : STOP_HEIGHT[s])

  const viewCam = $derived(cam(centre[0], centre[1], S))
  const viewTransform = $derived('translate(' + R2(viewCam.ox) + ' ' + R2(viewCam.oy) + ') scale(' + R2(S / Sgeom) + ')')
  const viewport = $derived(viewportRect(viewCam))

  /** The inverse of `viewTransform`'s own scale -- nested inside it
   * (the same `translate(point) scale(traceK)` shape a building's own
   * stamp already uses, below), it cancels that scale back to exactly
   * this component's local pixel units, so whatever it wraps renders at
   * a fixed size regardless of how far the camera is zoomed, while the
   * translate it sits inside still moves with the pan and the zoom
   * (#1050 round 56 defect 3: the trace's ✕, chip and ghost note used
   * to be drawn at the isometric drawing's own scale, legible only by
   * accident at whichever zoom this component happened to open on). */
  const traceK = $derived(Sgeom / S)

  type Focus = { districtId: string | null; id: string } | null
  let focus = $state<Focus>(null)

  function boundsCentre(b: { u0: number; u1: number; v0: number; v1: number }): Pt {
    return [(b.u0 + b.u1) / 2, (b.v0 + b.v1) / 2]
  }

  /** Where the camera looks at a stop: the whole estate, the borough,
   * the district or the building in focus, or the first of each. */
  function centreFor(s: Stop, f: Focus): Pt {
    const fd = districtOf(f?.districtId ?? (f?.id ?? null))
    const fb = f && f.districtId ? (fd?.buildings.find((b) => b.id === f.id) ?? null) : null
    const fn = f && !f.districtId ? (ground.nodes.find((n) => n.id === f.id) ?? null) : null
    if (s === 'city') return boundsCentre(ground.bounds)
    if (s === 'borough') {
      const rid = fd?.routerId ?? fn?.routerId ?? ground.boroughs[0]?.routerId
      const b = ground.boroughs.find((x) => x.routerId === rid) ?? ground.boroughs[0]
      return b ? boundsCentre(b.bounds) : boundsCentre(ground.bounds)
    }
    if (fb) return [fb.u, fb.v]
    if (fn) return [fn.u, fn.v]
    if (fd) return [fd.u, fd.v]
    const d0 = ground.districts[0]
    return d0 ? [d0.u, d0.v] : boundsCentre(ground.bounds)
  }

  function moveCamera(toS: number, to: Pt) {
    const target = clampCentre(to, ground.bounds)
    if (anim !== null) cancelAnimationFrame(anim)
    anim = null
    if (reducedMotion() || typeof requestAnimationFrame !== 'function') {
      S = toS
      centre = target
      return
    }
    const from = { S, ox: centre[0], oy: centre[1] }
    const dest = { S: toS, ox: target[0], oy: target[1] }
    const t0 = performance.now()
    const step = (now: number) => {
      const t = ease((now - t0) / MOVE_MS)
      const c = lerpCam(from, dest, t)
      S = c.S
      centre = [c.ox, c.oy]
      anim = t < 1 ? requestAnimationFrame(step) : null
    }
    anim = requestAnimationFrame(step)
  }

  $effect(() => {
    const s = stop
    const g = ground
    untrack(() => {
      // ground is read here only so the first real layout (devices
      // arriving after mount) reaches the `!started` branch below with
      // real bounds rather than an empty stub -- it must not, by itself,
      // re-centre an already-started view. Every live event redraws
      // ground (#975: layoutGround returns a fresh object on every
      // appState.events change), so treating ground as a re-centre
      // trigger snapped a released drag straight back to the stop's
      // default the moment the next event arrived. Only a genuine stop
      // change re-centres; lastStop is updated unconditionally, even
      // while standing, so a stop change that happens while standing
      // does not surface as a stale mismatch once standSurface runs.
      const stopChanged = s !== lastStop
      lastStop = s
      // While standing, the camera belongs to standOn/standSurface --
      // the slider's own stop keeps changing under it unread, so
      // surfacing lands on the position it actually saved rather than
      // wherever the prop drifted to meanwhile.
      if (stand) return
      if (!started) {
        started = true
        S = initialS ?? (s === 'city' ? cityFitS(g.bounds) : STOP_HEIGHT[s])
        centre = clampCentre(initialCentre ?? centreFor(s, focus), g.bounds)
        return
      }
      if (!stopChanged) return
      moveCamera(stopS(s), centreFor(s, focus))
    })
  })

  $effect(() => () => {
    if (anim !== null) cancelAnimationFrame(anim)
  })

  // Reports the live camera back to whoever asked (#869: Topography
  // saves this to hand back as initialS/initialCentre next time this
  // side mounts, since crossing the slider's centre destroys and
  // recreates this component). Not gated on anything -- a caller that
  // does not care simply does not pass the callback.
  $effect(() => {
    onCameraChange?.(S, centre)
  })

  /* ---------------- the reach: standing on a building (#868) ---------------- */

  // The design record: clicking a building at any city stop drops the
  // camera to the street stop centred on it; Esc or the crumb surfaces
  // to the exact camera you came from. `standBuilding` re-resolves the
  // id every render (rather than caching the object standOn saw) so a
  // live layout change is reflected while standing; if the building
  // itself disappears (aged out of the window), standing has nothing
  // left to mean and the effect below surfaces on its own.
  const standBuilding = $derived.by((): Building | null => {
    const s = stand
    if (!s || s.kind !== 'building') return null
    return s.districtId ? (districtOf(s.districtId)?.buildings.find((b) => b.id === s.id) ?? null) : (ground.nodes.find((n) => n.id === s.id) ?? null)
  })

  /** The district whose reach is open, when one is. */
  const standDistrict = $derived(stand?.kind === 'district' ? (districtOf(stand.id) ?? null) : null)

  /** The road whose reach is open, when one is. */
  const standRoad = $derived(stand?.kind === 'road' ? (ground.roads.find((r) => r.id === stand!.id) ?? null) : null)

  // Reports the stood-on building back to whoever asked (#869: crossing
  // the slider's centre while standing on a host hands it to the 2D
  // side's reach, if the same host exists there).
  $effect(() => {
    onStandChange?.(standBuilding)
  })

  /** The two districts a road joins, or null when it joins none. A pair
   * road is keyed `[a,b].sort().join('|')` by layout.ts over the
   * district ids, and a district id is the zone's own boundary
   * interface, so the ends are read from the id rather than guessed
   * from where the road runs. A building's lane and a bridge leg are
   * not lines between two districts. */
  function ribEndsOf(r: { id: string; lane?: boolean }): [string, string] | null {
    if (r.lane) return null
    const bar = r.id.indexOf('|')
    if (bar <= 0) return null
    const a = r.id.slice(0, bar)
    const b = r.id.slice(bar + 1)
    return b && districtOf(a) && districtOf(b) ? [a, b] : null
  }

  /** What the reach is centred on (#1016, ratified 2026-09-08): whatever
   * was stood on. A building is a host, a district is its zone, and a
   * road between two districts is the rib they share. A lane or a bridge
   * leg is no line between two districts and so has no subject: the
   * camera still comes to it and the crumb still names it, and it states
   * nothing it cannot derive. */
  const standSubject = $derived.by((): ReachSubject | null => {
    if (standBuilding) return hostSubject(standBuilding.ip)
    if (standDistrict) return { kind: 'zone', iface: standDistrict.id }
    const ends = standRoad ? ribEndsOf(standRoad) : null
    return ends ? { kind: 'rib', a: ends[0], b: ends[1] } : null
  })

  // reachFor is #626/#485's own strand model: the city draws exactly
  // what it derives, never a second reading of the same events. It now
  // answers for all three subjects, so the district and the road stopped
  // needing a workaround of their own.
  const standReach = $derived(standSubject ? reachFor(standSubject, zonesState.wanInterface, appState.events) : null)

  /** The subject's own name, for the crumb and the cards. */
  const standName = $derived(standBuilding?.name ?? standDistrict?.name ?? standRoad?.label ?? '')

  /** The crumb's second cell: a host's address, a district's pushed
   * subnet, and nothing for a road, which has neither. */
  const standSub = $derived(standBuilding ? standBuilding.ip : standDistrict ? (standDistrict.cidr ?? 'no address pushed') : '')

  /** The reach's own side of a line, for the cards that read
   * `subject → peer`. A rib is already a pair, so it reads from its near
   * end rather than printing the pair twice. */
  const standSideName = $derived.by((): string => {
    const subj = standSubject
    if (subj?.kind === 'rib') return districtOf(subj.a)?.name ?? subj.a
    return standName
  })

  /** The district the subject stands in or starts from, for keeping a
   * card off the plates its own line runs between. */
  const standDistrictId = $derived.by((): string | null => {
    const subj = standSubject
    if (subj?.kind === 'rib') return subj.a
    if (standDistrict) return standDistrict.id
    return standBuilding?.districtId ?? null
  })

  /** The crumb's third count (round 49, DESIGN.md "The reach": `name ·
   * ip · reaches N · reached by N · refused N`).
   *
   * Counted the same way `reachFor` counts the other two -- distinct
   * counterparts, not events and not strands -- so the three numbers in
   * the crumb are the same kind of thing and add up the way a reader
   * assumes they do. It is derived here rather than in reach.ts because
   * both surfaces derive it from `strands` identically, and reach.ts is
   * shared ground neither surface's slice edits. */
  const standRefused = $derived(
    standReach === null ? 0 : new Set(standReach.strands.filter((s) => s.outcome === 'blocked').map((s) => s.counterpart)).size,
  )

  /**
   * The composer (#868, DESIGN.md "The reach"): a card pinned to the
   * wall where the busiest blocked strand's road stopped, printing the
   * RouterOS line for a new gate -- reachComposeInput/composeCommand
   * (lib/compose.ts) unadorned by any picker, so it is byte-identical
   * to what the 2D composer prints for the same strand before its own
   * allow/block toggle or port chips are touched (a vitest proves it).
   * Drafted, never run: the app never connects to or probes any host.
   */
  const standComposeCommand = $derived.by((): string | null => {
    const b = standBuilding
    const s = standReach?.topBlocked
    if (!b || !s) return null
    const input = reachComposeInput(s, {
      hostIp: b.ip,
      hostName: b.name,
      zoneId: b.districtId ?? b.id,
      wanInterface: zonesState.wanInterface,
      zones: ground.districts.map((d) => ({ id: d.id, cidr: d.cidr, name: d.name })),
      edges: policyState.edges,
    })
    return input ? composeCommand(input) : null
  })

  function standOn(districtId: string | null, id: string) {
    const b = districtId ? districtOf(districtId)?.buildings.find((x) => x.id === id) : ground.nodes.find((n) => n.id === id)
    if (!b) return
    open({ kind: 'building', districtId, id }, [b.u, b.v])
    focus = { districtId, id }
  }

  /** A district's own reach (round 49): the camera frames it and every
   * road that is not the district's own fades, the same sentence
   * standing on a building reads. */
  function standOnDistrict(id: string) {
    const d = districtOf(id)
    if (!d) return
    open({ kind: 'district', districtId: id, id }, [d.u, d.v])
    focus = { districtId: null, id }
  }

  /** A road's own reach (round 49): the road alone, everything else
   * faded, at the height its buildings are labelled at. */
  function standOnRoad(id: string) {
    const r = ground.roads.find((x) => x.id === id)
    if (!r || r.pts.length === 0) return
    const mid = r.pts[Math.floor(r.pts.length / 2)] ?? r.pts[0]
    open({ kind: 'road', districtId: null, id }, mid)
  }

  function open(subject: { kind: StandKind; districtId: string | null; id: string }, at: Pt) {
    // Re-standing on something else from within a reach keeps the
    // original saved camera -- surfacing always returns to where you
    // stood before the first click, not to whichever subject you last
    // passed through.
    const savedS = stand ? stand.savedS : S
    const savedCentre = stand ? stand.savedCentre : centre
    stand = { ...subject, savedS, savedCentre }
    // Standing clears the port filter, exactly as it does on the flat
    // map (#1018's own rule, kept here by #1055). Two answers layered on
    // one city would stack their dimming on each other and leave nobody
    // able to say which of them a grey road was grey because of.
    portFilterState.clear()
    // Each reach starts from the drawing: the last building's draft does
    // not follow you to the next one.
    composerOpen = false
    // Any card the previous stop had open is about the previous stop.
    hoverRoad = null
    pinnedRoad = null
    moveCamera(STOP_HEIGHT[subject.kind === 'district' ? 'district' : 'street'], at)
  }

  function standSurface() {
    if (!stand) return
    const { savedS, savedCentre } = stand
    stand = null
    composerOpen = false
    moveCamera(savedS, savedCentre)
  }

  // A reach whose subject the ground no longer draws surfaces by itself
  // rather than standing on nothing -- a host that went away, a district
  // or road that a fresh rule table stopped drawing.
  $effect(() => {
    if (!stand) return
    const gone = stand.kind === 'building' ? !standBuilding : stand.kind === 'district' ? !standDistrict : !standRoad
    if (gone) standSurface()
  })

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    // A ladder, top rung first (#1002): a card was opened on purpose, so
    // Escape takes that back before it takes back where you are
    // standing. Two Escapes to do both, and never both at once.
    if (openDropId) {
      e.preventDefault()
      closeDropCard()
      return
    }
    // The filter is the outermost thing the operator turned on, so it
    // comes off before where they are standing -- the flat map reads the
    // ladder in the same order. It is this handler's rung while the city
    // is the surface being read: Topography stands its own down for a
    // city stop so one press can never take two. The trace (#1050) is
    // the same rung as the port filter -- the two are mutually exclusive
    // already, so at most one of them is ever open to clear.
    if (portFilterState.active || portFilterState.open || mapTraceState.active) {
      e.preventDefault()
      portFilterState.clear()
      mapTraceState.clear()
      return
    }
    if (stand) {
      e.preventDefault()
      standSurface()
    }
  }

  // A flag's own "where" link (#678) lands here when the city is the
  // active side of the slider -- Topography.svelte's own copy of this
  // effect stands down while cityStop is set, so exactly one of the two
  // ever consumes a given request. City is only ever mounted while the
  // city is the active side (Topography.svelte's `{#if cityStop}`), so
  // that alone is the guard this effect needs. Left unconsumed when the
  // address resolves to no drawn building yet (beyond the district's
  // drawn cap, or the ground hasn't caught up with fresh traffic) --
  // the request just waits rather than standing on nothing.
  $effect(() => {
    const pending = topologyNavState.pendingDescend
    if (!pending) return
    const match = allBuildings.find((b) => b.ip === pending.ip)
    if (match) {
      standOn(match.districtId, match.id)
      topologyNavState.pendingDescend = null
    }
  })

  /* ---------------- pan: drag, keys, minimap ---------------- */

  let drag: { x: number; y: number; c: Pt; moved: boolean } | null = null
  /** A drag ends with a click on whatever is under the pointer; that
   * click must not refocus and undo the pan. */
  let dragged = false

  /** Stage units per client pixel, for xMidYMid meet. */
  function stageScale(): number {
    if (!svgEl) return 1
    const r = svgEl.getBoundingClientRect()
    const k = Math.min(r.width / STAGE_W, r.height / STAGE_H) || 1
    return 1 / k
  }

  function onPointerDown(e: PointerEvent) {
    if (e.button !== 0) return
    drag = { x: e.clientX, y: e.clientY, c: centre, moved: false }
    // Capture is taken lazily in onPointerMove, once a real drag is
    // under way -- not here. See the comment there for why (#977).
  }
  function onPointerMove(e: PointerEvent) {
    if (!drag) return
    const k = stageScale()
    const dx = (e.clientX - drag.x) * k
    const dy = (e.clientY - drag.y) * k
    if (Math.abs(dx) + Math.abs(dy) > 3 && !drag.moved) {
      drag.moved = true
      // #977: setPointerCapture keeps a drag tracking the pointer past
      // the svg's own edge, which a plain click never needs -- and
      // capturing unconditionally on pointerdown broke every click,
      // drag or not. Chromium decides a click's target from the
      // capture state at pointerdown/pointerup, not at click-dispatch
      // time, so releasing it in onPointerUp (tried first) was already
      // too late: the click still landed on the capturing svg instead
      // of bubbling through the building or plate under the pointer,
      // and standing on a host or focusing a district did nothing.
      // Taking capture only once a drag is confirmed leaves a plain
      // click never captured at all, so its own click reaches the
      // element it was aimed at exactly as before this existed.
      ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
    }
    if (!drag.moved) return
    if (anim !== null) cancelAnimationFrame(anim)
    anim = null
    centre = clampCentre([drag.c[0] - dx / (IK * S), drag.c[1] - dy / (VK * S)], ground.bounds)
  }
  function onPointerUp() {
    dragged = drag?.moved ?? false
    drag = null
  }
  function onItemClick(f: Focus) {
    if (dragged) return
    void focusItem(f)
  }

  /** A building click stands on it (#868). */
  function onBuildingClick(districtId: string | null, id: string) {
    if (dragged) return
    standOn(districtId, id)
  }

  /** Round 49: a district plate opens its reach too, rather than merely
   * focusing and panning. Its keyboard focus still moves, so the walk
   * that Enter uses is unchanged. */
  function onDistrictClick(id: string) {
    if (dragged) return
    // A ghost opens its own card rather than being stood on: there is
    // nothing to stand in -- the district is a place that was -- and the
    // card is the only thing here with anything to say or ask.
    const g = ground.districts.find((d) => d.id === id)
    if (g?.ghost) {
      ghostError = null
      openGhostId = openGhostId === id ? null : id
      return
    }
    standOnDistrict(id)
  }

  /** Round 49: and so does a road. The card is still hover's, and the
   * pin is still the pin button's -- clicking the road itself is the
   * "click anything for its reach" the mockup's own minimap caption
   * promises. */
  function onRoadClick(id: string) {
    if (dragged) return
    standOnRoad(id)
  }

  function isBuildingFocus(f: Focus): boolean {
    if (!f) return false
    return f.districtId ? (districtOf(f.districtId)?.buildings.some((b) => b.id === f.id) ?? false) : ground.nodes.some((n) => n.id === f.id)
  }

  function panByStage(dx: number, dy: number) {
    moveCamera(S, [centre[0] - dx / (IK * S), centre[1] - dy / (VK * S)])
  }

  function onMinimapClick(e: MouseEvent) {
    const el = e.currentTarget as HTMLElement
    const r = el.getBoundingClientRect()
    const x = ((e.clientX - r.left) / (r.width || 1)) * MINI_W
    const y = ((e.clientY - r.top) / (r.height || 1)) * MINI_H
    moveCamera(S, groundAt(miniCam, x, y))
  }

  /* ---------------- focus: the keyboard walk ---------------- */


  async function focusItem(f: Focus, recentre = true) {
    focus = f
    if (!f) return
    const d = districtOf(f.districtId)
    const b = d?.buildings.find((x) => x.id === f.id) ?? ground.nodes.find((n) => n.id === f.id)
    if (recentre) moveCamera(S, b ? [b.u, b.v] : d ? [d.u, d.v] : centre)
    await tick()
    const el = svgEl?.querySelector<SVGGElement>('[data-cid="' + CSS.escape(f.id) + '"]')
    el?.focus({ preventScroll: true })
  }

  function onKey(e: KeyboardEvent) {
    const f = focus
    const key = e.key
    // Enter stands on the focused building (#868); a focused district
    // plate takes no action here, same as before this build.
    if (key === 'Enter' && isBuildingFocus(f)) {
      e.preventDefault()
      standOn(f!.districtId, f!.id)
      return
    }
    if (e.shiftKey && key.startsWith('Arrow')) {
      e.preventDefault()
      const step = 120
      panByStage(key === 'ArrowLeft' ? step : key === 'ArrowRight' ? -step : 0, key === 'ArrowUp' ? step : key === 'ArrowDown' ? -step : 0)
      return
    }
    const ds = ground.districts
    if (ds.length === 0) return
    const di = Math.max(
      0,
      ds.findIndex((d) => d.id === (f?.districtId ?? f?.id)),
    )
    if (key === 'ArrowDown' || key === 'ArrowUp') {
      e.preventDefault()
      const n = (di + (key === 'ArrowDown' ? 1 : -1) + ds.length) % ds.length
      void focusItem({ districtId: null, id: ds[n].id })
      return
    }
    if (key === 'ArrowRight' || key === 'ArrowLeft') {
      e.preventDefault()
      const d = ds[di]
      const bs = d.buildings
      if (bs.length === 0) return
      const bi = f && f.districtId === d.id ? bs.findIndex((b) => b.id === f.id) : -1
      const n = bi < 0 ? (key === 'ArrowRight' ? 0 : bs.length - 1) : (bi + (key === 'ArrowRight' ? 1 : -1) + bs.length) % bs.length
      void focusItem({ districtId: d.id, id: bs[n].id })
    }
  }

  /* ---------------- drawing ---------------- */

  type Solid =
    | { kind: 'piece'; v: number; paints: Paint[]; flow: Paint | null; label: string; roadId: string }
    | {
        kind: 'other'
        v: number
        paints: Paint[]
        lamps: { x: number; y: number; r: number; rr: number; h: number }[]
        /** Present on a wall piece: which district's edge it is, so
         * pointing at it opens that boundary's card. */
        wall?: { districtId: string; side: WallSide; coverage: Coverage }
        /** Present on a gate post: which gate it belongs to, so pointing
         * at it opens that gate's own card rather than the worst gate
         * standing in the same edge, and so a live check can count the
         * gates the city drew (#1022). */
        gate?: { districtId: string; side: WallSide; gateKey: string; toward: string; coverage: Coverage }
        /** Present on a door (#1055): the leaf swings open for an accept
         * and a bar goes across for a refusal, and the title names the
         * rule that put it there. Not pointable -- a door is a statement
         * about policy, and the rule behind it is read on the gate's own
         * card. */
        door?: { accepts: boolean; title: string; where: string }
      }
    | {
        kind: 'building'
        v: number
        b: Building
        district: District | null
        ink: string
        dim: boolean
        /** What the host register says this building is (round 49,
         * #1016). Always 'live' for a router or a bridge post, which
         * are not hosts the feed hears. */
        pres: HostPresence
        /** The red fill, the flag rim and the watch line this
         * building wears, or null when nothing is behind it (#981). */
        mark: BuildingMark | null
        paints: Paint[]
        stamp: { x: number; y: number; k: number }
        aria: string
      }
    | { kind: 'hamlet'; v: number; id: string; aria: string; attrs: DeviceStampAttrs }

  /** This SVG root's own device-symbol prefix (#864's <use> convention):
   * only one City is ever mounted at a time (Topography.svelte), so a
   * fixed prefix is safe. */
  const DEVICE_PREFIX = 'city'

  function gfaces(b: BoxFaces, ink: string, o: { t?: number; r?: number; l?: number; s?: number; sw?: number; bg?: boolean }): Paint[] {
    const out: Paint[] = []
    if (o.bg) out.push({ d: b.left, fill: VOID }, { d: b.right, fill: VOID }, { d: b.top, fill: VOID })
    out.push(
      { d: b.left, fill: ink, fo: o.l ?? 0.4 },
      { d: b.right, fill: ink, fo: o.r ?? 0.7 },
      { d: b.top, fill: ink, fo: o.t ?? 0.92, stroke: ink, so: o.s ?? 0.8, sw: o.sw ?? 0.6 },
    )
    return out
  }

  const HAMLET_MAX = 6
  const HAMLET_R = 8

  /**
   * Ring positions for a tunnel's peers around a centre point on the far
   * bank -- the same ring formula layout.ts's placeBuildings uses for a
   * district, kept local since this is purely a rendering placement (the
   * hamlet stands on no ground plan any other code depends on). Capped
   * at HAMLET_MAX: a hamlet is a glance across the water, not another
   * district with its own "N more".
   */
  function hamletPositions(peers: CityPeer[], centre: Pt): Pt[] {
    const n = Math.min(peers.length, HAMLET_MAX)
    if (n <= 1) return [centre]
    const out: Pt[] = []
    for (let i = 0; i < n; i++) {
      const th = -Math.PI / 2 + (i / n) * Math.PI * 2
      const cx = Math.cos(th)
      const sy = Math.sin(th)
      const s = Math.abs(cx) + Math.abs(sy) || 1
      out.push([centre[0] + (cx / s) * HAMLET_R, centre[1] + (sy / s) * HAMLET_R])
    }
    return out
  }

  /**
   * An operator's own tags for a peer's address, from the entities
   * register -- read only, never fetched from here (Topography.svelte's
   * mount effect refreshes the zone, policy and tunnel tables; the
   * entities register is refreshed by its own admin page). Undefined
   * until the register has loaded is the generic puck's honest fallback,
   * not a defect: "generic puck unless the entities register knows the
   * kind" already covers "does not know yet".
   */
  function entityTagsFor(peer: CityPeer): string[] | undefined {
    if (!peer.address) return undefined
    const ip = peer.address.split('/')[0]
    return entitiesState.list.find((e) => e.type === 'host' && e.key === ip)?.tags
  }

  const showLanes = $derived(effectiveStop === 'district' || effectiveStop === 'street')
  const showBoroughs = $derived(effectiveStop === 'city' || effectiveStop === 'borough')
  const compact = $derived(effectiveStop === 'city')

  /* ---------------- the reach's own drawing (#868) ---------------- */

  /** Whether a road's own point order already runs away from the
   * standing building (start end nearer it than the end is) -- the
   * diamond metric, same as roads.ts's own `dm`, is exact for these
   * footprints and cheap enough to call per road per render. */
  function dm(p: Pt, q: Pt): number {
    return Math.abs(p[0] - q[0]) + Math.abs(p[1] - q[1])
  }

  /** True when this road's default flow direction (its own point order)
   * reads the opposite of what the strand says -- so the flow paint
   * needs its animation reversed to show dashes moving away when the
   * host spoke, toward when it was spoken to. */
  function flowReversed(pts: Pt[], direction: ReachStrand['direction'], myU: number, myV: number): boolean {
    const near: Pt = [myU, myV]
    const runsAway = dm(pts[0], near) <= dm(pts[pts.length - 1], near)
    return runsAway !== (direction === 'out')
  }

  interface ReachOverlay {
    /** Road ids that are this building's own -- everything else fades. */
    ownRoadIds: Set<string>
    /** Own road ids whose flow should animate reversed (toward, not away). */
    reverseIds: Set<string>
    /** Own road id -> the strand counterpart it draws, which is what
     * `reachLineSummary` merges a line back together by. This is the
     * whole join between the drawing and the line card: hovering a road
     * asks this map which line it is, and asks reach.ts what that line
     * says. Only roads that carry a strand are in it, so an own road
     * with nothing behind it (a peer's lane, a bridge leg) opens no
     * line card rather than an empty one. */
    lineOf: Map<string, string>
    /** The lanes this reach lights, and whose host each belongs to --
     * what lib/city/reachRoads rolls today's off-baseline lines onto so
     * a lane takes part in the brightness rule like every other road
     * the standing host owns. */
    laneSubjects: LaneSubject[]
    /** Buildings the standing host actually reaches or is reached by,
     * always including itself. */
    litBuildingIds: Set<string>
    /** Where a blocked strand's road would have crossed this district's
     * wall, and the counterpart's name -- present only when the strand
     * arrived rather than left (#991: the drop is not on the building
     * you are standing on, so the source is named; your own outbound
     * attempt just reads "dropped"). */
    dropMarks: { id: string; counterpart: string; p: Pt; source?: string }[]
    /** Where the busiest blocked strand's own road crosses the wall --
     * the composer's own pin point -- computed regardless of whether a
     * fresh bollard mark was drawn there or an existing one already
     * stood (#868's "pinned to the wall where the refused road
     * stopped"), so the composer never loses its anchor merely because
     * the mark it is pinned beside was #865's own. */
    composerAnchor: Pt | null
  }

  /** How a drop mark's own subject id is told apart from a road id. A
   * road id is a district pair (`a|b`), a lane (`lane:<id>`) or a bridge
   * leg, so this prefix collides with none of them. */
  const MARK_PREFIX = 'mark:'

  /** True when this road has the given district as one of its ends.
   * Pair roads are keyed `[a,b].sort().join('|')` by layout.ts and lanes
   * name their host in `from`, so both are answered from the ground's
   * own ids rather than from geometry. */
  function roadTouches(r: { id: string; lane?: boolean; from: string | null }, d: District): boolean {
    if (r.lane) return d.buildings.some((b) => b.id === r.from)
    const bar = r.id.indexOf('|')
    if (bar < 0) return false
    return r.id.slice(0, bar) === d.id || r.id.slice(bar + 1) === d.id
  }

  /**
   * The buildings standing at one end of a road -- what the off-baseline
   * arrival mark outlines (#1057).
   *
   * Three cases, all answered from the ground's own ids rather than from
   * geometry:
   *
   * - a lane names its host in `from` (layout.ts), so its start is that
   *   one building -- the reach's own case, where the traffic resolves
   *   to a single host;
   * - a pair road that ends at a router or a bridge post names it in
   *   `from`/`to`, so that node's own building is the end. The WAN road
   *   is this: a line to an address outside every district rings the
   *   wan end, and the wan end is drawn at the router the road runs to;
   * - otherwise the end is a district's wall, and the district's
   *   buildings are what stands there.
   *
   * The issue offers "the district's buildings, or the district plaque's
   * building" for that last case. Only the first exists here: a plaque
   * is a text label under the plate (`plaques`, in the scene below) with
   * no building of its own, so there is no plaque building to outline.
   */
  function endBuildings(r: Road, which: 'start' | 'end', pair: RoadBaselineEntry | null): Building[] {
    const nodeId = which === 'start' ? r.from : r.to
    if (nodeId) {
      const b = allBuildings.find((x) => x.id === nodeId)
      return b ? [b] : []
    }
    const id = pair?.ends[which] ?? null
    const d = id ? ground.districts.find((x) => x.id === id) : null
    return d ? d.buildings : []
  }

  /** Which roads draw the line toward `counterpart` from `token`, and
   * the counterpart they draw -- the join between a strand and the
   * ground's own road ids, shared by all three subjects so none of them
   * invents a second idea of which road a line runs along. */
  function roadsToward(token: string, counterpart: string, ownRoadIds: Set<string>, lineOf: Map<string, string>): void {
    const counterpartToken = counterpart === 'internet' ? (zonesState.wanInterface ?? '') : counterpart
    if (!counterpartToken) return
    const pairId = [token, counterpartToken].sort().join('|')
    const road = ground.roads.find((r) => !r.lane && r.id === pairId)
    if (road) {
      ownRoadIds.add(road.id)
      if (!lineOf.has(road.id)) lineOf.set(road.id, counterpart)
    }
    const bridge = ground.bridges.find((br) => br.iface === counterpartToken)
    if (bridge) {
      for (const id of ['rb-' + bridge.id, bridge.id + '-span']) {
        ownRoadIds.add(id)
        if (!lineOf.has(id)) lineOf.set(id, counterpart)
      }
    }
  }

  /**
   * Where a road toward `counterpartToken` crosses `from`'s own wall:
   * the gate point and its outward normal, from the same gateToward the
   * roads themselves are drawn through, so a mark or a door pinned to a
   * wall stands exactly where the road meets it.
   *
   * The counterpart may be a district, a router or other node, or a
   * bridge head this build has ground for. Nothing resolving it to a
   * place falls back to the district's own router -- gates.ts makes the
   * same choice for the same case -- rather than guessing a direction.
   */
  function wallCrossing(from: District, counterpartToken: string): Gate {
    const d = districtOf(counterpartToken)
    if (d) return gateToward(from, [d.u, d.v])
    const n = ground.nodes.find((x) => x.id === counterpartToken)
    if (n) return gateToward(from, [n.u, n.v])
    const bridge = ground.bridges.find((br) => br.iface === counterpartToken)
    if (bridge) return gateToward(from, bridge.f)
    const rn = ground.nodes.find((x) => x.id === from.routerId)
    return gateToward(from, rn ? [rn.u, rn.v] : [from.u, from.v])
  }

  /**
   * A district's reach (round 49, #1016). DESIGN.md widens the click to
   * "a building, a host dot, a district, a road" and gives one behaviour
   * for all of them: the camera comes to the subject and every road that
   * is not its own fades. The district's subject is its boundary
   * interface, so its lines come from the same `reachFor` a host's do --
   * every road it owns stays lit, and the ones a strand actually runs
   * along open that line's card.
   *
   * No drop marks and no composer: the pair roads already end at the
   * wall where the pair was refused, and a drafted rule names one
   * machine as its source.
   */
  function placeOverlay(d: District, summary: ReachSummary | null): ReachOverlay {
    const ownRoadIds = new Set<string>()
    const lineOf = new Map<string, string>()
    const litBuildingIds = new Set<string>(d.buildings.map((b) => b.id))
    for (const r of ground.roads) {
      if (!roadTouches(r, d)) continue
      ownRoadIds.add(r.id)
      // The far end of each pair road is a place this district reaches,
      // so it stays lit rather than fading with the rest of the map.
      const bar = r.id.indexOf('|')
      if (!r.lane && bar >= 0) {
        const far = r.id.slice(0, bar) === d.id ? r.id.slice(bar + 1) : r.id.slice(0, bar)
        for (const b of districtOf(far)?.buildings ?? []) litBuildingIds.add(b.id)
        litBuildingIds.add(far)
      }
    }
    for (const s of summary?.strands ?? []) roadsToward(d.id, s.counterpart, ownRoadIds, lineOf)
    return { ownRoadIds, reverseIds: new Set(), lineOf, laneSubjects: [], litBuildingIds, dropMarks: [], composerAnchor: null }
  }

  /** A road's own reach (round 49, #1016): that road alone, everything
   * else faded, which is the same sentence every other reach reads.
   *
   * A road between two districts is a rib, and its subject lights the
   * machines that actually used it -- `peerAddrs` names the hosts at
   * both ends, which is the one thing a rib knows that the drawing does
   * not. A lane or a bridge leg is no rib and has no summary, so it
   * lights the ends it joins and stops there. */
  function roadOverlay(r: (typeof ground.roads)[number], summary: ReachSummary | null): ReachOverlay {
    const litBuildingIds = new Set<string>()
    const lineOf = new Map<string, string>()
    if (r.from) litBuildingIds.add(r.from)
    if (r.to) litBuildingIds.add(r.to)
    if (summary) {
      for (const s of summary.strands) {
        if (!lineOf.has(r.id)) lineOf.set(r.id, s.counterpart)
        for (const addr of s.peerAddrs) {
          const peer = allBuildings.find((x) => x.ip === addr)
          if (peer) litBuildingIds.add(peer.id)
        }
      }
    } else {
      for (const d of ground.districts) if (roadTouches(r, d)) for (const b of d.buildings) litBuildingIds.add(b.id)
    }
    return {
      ownRoadIds: new Set([r.id]),
      reverseIds: new Set(),
      lineOf,
      laneSubjects: [],
      litBuildingIds,
      dropMarks: [],
      composerAnchor: null,
    }
  }

  /**
   * The one place standing on a building turns its own reachFor summary
   * into what the scene fades, lights and labels. `ownRoadIds` matches
   * against the ground's own road ids: this building's lane (from
   * layout.ts's `lane:<id>`), the district-pair road toward each
   * strand's counterpart (layout.ts's own `[a,b].sort().join('|')`
   * key), and a boundary bridge's two legs (`rb-<id>`, `<id>-span`)
   * when the counterpart is the WAN or a tunnel -- the same ids the
   * ground model already draws, never a second road invented for the
   * occasion.
   */
  const reachOverlay = $derived.by((): ReachOverlay | null => {
    if (standDistrict) return placeOverlay(standDistrict, standReach)
    if (standRoad) return roadOverlay(standRoad, standReach)
    const b = standBuilding
    const summary = standReach
    if (!b || !summary) return null
    const ownRoadIds = new Set<string>()
    const reverseIds = new Set<string>()
    const lineOf = new Map<string, string>()
    const laneSubjects: LaneSubject[] = []
    const litBuildingIds = new Set<string>([b.id])
    const dropMarks: { id: string; counterpart: string; p: Pt; source?: string }[] = []
    const wan = zonesState.wanInterface
    const myToken = b.districtId ?? b.id
    const myDistrict = b.districtId ? districtOf(b.districtId) : null

    const lane = b.districtId ? (ground.roads.find((r) => r.lane && r.from === b.id) ?? null) : null
    if (lane && summary.busiest) {
      ownRoadIds.add(lane.id)
      laneSubjects.push({ roadId: lane.id, ip: b.ip })
      // The standing host's own lane carries every line it has, so the
      // card it opens is the busiest of them -- the same strand the
      // flow's own direction already reads.
      lineOf.set(lane.id, summary.busiest.counterpart)
      if (flowReversed(lane.pts, summary.busiest.direction, b.u, b.v)) reverseIds.add(lane.id)
    }

    let composerAnchor: Pt | null = null
    if (myDistrict && summary.topBlocked) {
      const counterpartToken = summary.topBlocked.counterpart === 'internet' ? (wan ?? '') : summary.topBlocked.counterpart
      composerAnchor = wallCrossing(myDistrict, counterpartToken || myToken).p
    }

    for (const s of summary.strands) {
      const counterpartToken = s.counterpart === 'internet' ? (wan ?? '') : s.counterpart
      if (counterpartToken) {
        const pairId = [myToken, counterpartToken].sort().join('|')
        const road = ground.roads.find((r) => !r.lane && r.id === pairId)
        if (road) {
          ownRoadIds.add(road.id)
          // First strand on this road wins the line card. `strands` is
          // sorted busiest first, so that is the busiest line the road
          // carries -- and every strand on one counterpart resolves to
          // the same road anyway, so the only case this decides is two
          // counterparts sharing a road, where busiest is the honest
          // pick rather than whichever was found last.
          if (!lineOf.has(road.id)) lineOf.set(road.id, s.counterpart)
          if (flowReversed(road.pts, s.direction, b.u, b.v)) reverseIds.add(road.id)
        }
        const bridge = ground.bridges.find((br) => br.iface === counterpartToken)
        if (bridge) {
          ownRoadIds.add('rb-' + bridge.id)
          ownRoadIds.add(bridge.id + '-span')
          if (!lineOf.has('rb-' + bridge.id)) lineOf.set('rb-' + bridge.id, s.counterpart)
          if (!lineOf.has(bridge.id + '-span')) lineOf.set(bridge.id + '-span', s.counterpart)
        }
      }
      if (s.outcome === 'accepted') {
        for (const addr of s.peerAddrs) {
          const peer = allBuildings.find((x) => x.ip === addr)
          if (!peer) continue
          litBuildingIds.add(peer.id)
          const peerLane = ground.roads.find((r) => r.lane && r.from === peer.id)
          if (peerLane) {
            ownRoadIds.add(peerLane.id)
            laneSubjects.push({ roadId: peerLane.id, ip: peer.ip })
            // A lit peer's lane is the last stretch of this strand's own
            // line, so it opens the same card the pair road does rather
            // than none -- one line, however many roads draw it.
            if (!lineOf.has(peerLane.id)) lineOf.set(peerLane.id, s.counterpart)
          }
        }
      }
      if (s.outcome === 'blocked' && myDistrict) {
        // The aggregate district-pair road already ends at the wall with
        // its own mark when the pair's overall verdict is a drop or the
        // one escalated unplanned pair (#865) -- drawing a second one on
        // top of it would just double the bollards, so this strand's own
        // mark is only new ground when that road stayed standing.
        const pairId = counterpartToken ? [myToken, counterpartToken].sort().join('|') : null
        const already = pairId ? ground.roads.some((r) => !r.lane && r.id === pairId && r.stop === 'drop') : false
        // #991: named only when the drop is not on the building you are
        // standing on -- an 'in' strand arrived from the counterpart and
        // was refused here, so the counterpart is named; an 'out' strand
        // was this building's own attempt, so it just reads "dropped".
        const source = s.direction === 'in' ? (s.peers[0] ?? (s.counterpart === 'internet' ? 'the internet' : s.counterpart)) : undefined
        // Round 49, DESIGN.md "The reach" and the metaphor table: a
        // refused road ends at the wall with bollards and the red mark,
        // and nothing is written on the drawing. The refusing rule's
        // name is the strand's own `refusedBy` -- the event's rule
        // label, absent when no refusal on this strand carried one, and
        // then said plainly rather than guessed (#865/#967) -- but
        // #1036 keeps it on the card the mark opens, so the mark itself
        // carries no name to write.
        // The mark carries its own line, so a refused strand whose road
        // was never drawn -- an unlogged boundary draws none, and a road
        // there would claim a log line nobody wrote -- still has
        // somewhere to open its card from. That is the wall the design
        // pins the composer to anyway.
        if (!already) {
          const id = MARK_PREFIX + s.counterpart
          if (!lineOf.has(id)) {
            lineOf.set(id, s.counterpart)
            dropMarks.push({ id, counterpart: s.counterpart, p: wallCrossing(myDistrict, counterpartToken || myToken).p, source })
          }
        }
      }
    }

    return { ownRoadIds, reverseIds, lineOf, laneSubjects, litBuildingIds, dropMarks, composerAnchor }
  })

  /* ---------------- the port filter (#1055, round 54) ---------------- */

  // Round 53's tool (#1018) on this surface, drawn as round 54 draws it.
  // The rule is round 53's own and does not change with the camera:
  // nothing is added to the map for the filter, the map dims to the
  // answer. So there is no port layer here -- the roads, the buildings
  // and the plaques already on the ground say it, and what the filter
  // adds is the doors, which are policy and have their own vocabulary.
  //
  // The store is the flat map's, unchanged: one selection, one answer,
  // and the pill that drives it is drawn once in Topography at every
  // altitude. A port cannot be lit here and dim there.
  const portOn = $derived(portFilterState.active && portFilterState.settled)

  /** A door standing in a gate: where it is, and which way its wall
   * runs. The drawing is the scene's, below -- this half is geometry the
   * camera has no part in, so panning never re-runs it. */
  interface DoorSpot {
    door: PortDoor
    /** The gate point the rule's road crosses at, in ground units. */
    p: Pt
    /** Along the opening, unit length: the two posts stand either side
     * of the gate point on this axis. */
    axis: Pt
    /** Half the opening, in ground units. */
    half: number
    /** Which wall it stands in: `<district>:<toward>`, or `bridge:<iface>`. */
    where: string
  }

  interface PortOverlay {
    /** Every road the port travelled, in the ground's own road ids. */
    litRoadIds: Set<string>
    /** Every building on the port. */
    litBuildingIds: Set<string>
    /** The one line under each district's plaque. */
    tallies: Map<string, string>
    doors: DoorSpot[]
    /** The one line under the city when the window carried none. */
    note: string | null
  }

  /**
   * What the filter dims the city to.
   *
   * Roads come from `litRibs` -- the same set the flat map lights, keyed
   * the same way -- put through the same `roadsToward` a reach uses, so
   * the two surfaces cannot disagree about which road a line ran along.
   * A rib names interfaces and a road is a district pair, and that join
   * lives in one function for exactly this reason.
   *
   * Buildings come from the answer's own host list, and so does the
   * numerator of a district's tally -- but only through the buildings
   * the district actually has (#1056). A host the register has never
   * answered for is not a building and is not counted: counting it
   * against a district that draws none of it put `1 of 0 · 445/tcp`
   * under the Servers plaque on #1055's live capture. Its road and its
   * door still light, which is what says the traffic was there.
   *
   * The denominator is the district's own host count -- the buildings
   * on the plate plus the `more` beyond it (layout.ts's MAX_BUILDINGS)
   * -- because "1 of 5" is a claim about the district and a plate that
   * draws four of its five machines must not turn it into a claim about
   * four. A district the register knows no host in gets no tally at all.
   */
  const portOverlay = $derived.by((): PortOverlay | null => {
    if (!portOn) return null
    const litRoadIds = new Set<string>()
    const seen = new Map<string, string>()
    for (const key of litRibs(portFilterState.ribs).keys()) {
      const [from, to] = key.split('|')
      // A direction with no out-interface died at the router, and the
      // city draws no road from a district to its own router -- so there
      // is nothing to light. Lighting the road toward the WAN instead
      // would draw a packet reaching a boundary it never got to.
      if (!from || !to) continue
      // Both ways round: the pair id is order-free, but which end is the
      // bridge is not, and a boundary the river carries is two legs.
      roadsToward(from, to, litRoadIds, seen)
      roadsToward(to, from, litRoadIds, seen)
    }
    const onPort = portFilterState.hostIps
    const litBuildingIds = new Set<string>()
    const tallies = new Map<string, string>()
    for (const d of ground.districts) {
      let on = 0
      for (const b of d.buildings) {
        if (!b.ip || !onPort.has(b.ip)) continue
        litBuildingIds.add(b.id)
        on++
      }
      const line = plaqueTally(on, d.buildings.length + d.more, portFilterState.label)
      if (line) tallies.set(d.id, line)
    }
    const doors: DoorSpot[] = []
    for (const door of portFilterState.placedDoors) {
      const spot = doorSpot(door)
      if (spot) doors.push(spot)
    }
    return {
      litRoadIds,
      litBuildingIds,
      tallies,
      doors,
      note: portFilterState.nothingSeen ? emptyNote(portFilterState.label, portFilterState.doors) : null,
    }
  })

  /* ---------------- the event trace (#1050, rounds 54 & 56) ---------------- */

  // Round 53's other tool (#1018) on this surface, drawn as rounds 54
  // and 56 draw it. Mutually exclusive with the port filter by
  // construction -- Topography's openTrace/openPortPicker each clear
  // the other -- so at most one of portOverlay/traceOverlay is ever
  // non-null.
  const traceOn = $derived(mapTraceState.active)

  interface TraceOverlay {
    /** Every road the traced pair travelled -- kept at full colour and
     * flow; everything else dims exactly as the port filter dims it. */
    litRoadIds: Set<string>
    litBuildingIds: Set<string>
    /** The one line under each end's own district plaque, the same
     * shape the port filter's tally takes: `cam-porch · 14× in the
     * window`, `tom-desktop · never reached`. */
    tallies: Map<string, string>
  }

  const traceOverlay = $derived.by((): TraceOverlay | null => {
    if (!traceOn) return null
    const litRoadIds = new Set<string>()
    const litBuildingIds = new Set<string>()
    const tallies = new Map<string, string>()
    const e = mapTraceState.event
    // A log, mark or NAT line says which kind of rule logged the packet,
    // not whether it passed (mapTrace.svelte.ts's own doc comment) --
    // there is no path to light for one, so the city dims to nothing
    // rather than guessing a route.
    if (!e || !mapTraceState.verdict) return { litRoadIds, litBuildingIds, tallies }
    const inIface = e.inInterface ?? ''
    const outIface = e.outInterface ?? ''
    if (inIface !== '' && outIface !== '') {
      // Both ways round: the pair id is order-free, but which end is a
      // bridge leg is not -- the same reasoning portOverlay's own loop
      // above uses.
      roadsToward(inIface, outIface, litRoadIds, new Map())
      roadsToward(outIface, inIface, litRoadIds, new Map())
    } else if (inIface !== '') {
      // No out-interface: the line never left the router. The city
      // draws no road from a plain district straight to its own router
      // (portOverlay's own note, above) -- only a boundary the WAN
      // bridge carries has a road object independent of any one pair's
      // traffic, so that is the only case with an "in road" to light.
      const bridge = ground.bridges.find((br) => br.iface === inIface)
      if (bridge) {
        litRoadIds.add('rb-' + bridge.id)
        litRoadIds.add(bridge.id + '-span')
      }
    }
    const srcBuilding = e.srcIp ? allBuildings.find((b) => b.ip === e.srcIp) : undefined
    const dstBuilding = e.dstIp ? allBuildings.find((b) => b.ip === e.dstIp) : undefined
    if (srcBuilding) {
      litBuildingIds.add(srcBuilding.id)
      if (srcBuilding.districtId) tallies.set(srcBuilding.districtId, `${srcBuilding.name} · ${mapTraceState.srcSeen}× in the window`)
    }
    if (dstBuilding) {
      litBuildingIds.add(dstBuilding.id)
      if (dstBuilding.districtId) {
        tallies.set(
          dstBuilding.districtId,
          mapTraceState.dstReached > 0 ? `${dstBuilding.name} · reached ${mapTraceState.dstReached}×` : `${dstBuilding.name} · never reached`,
        )
      }
    }
    return { litRoadIds, litBuildingIds, tallies }
  })

  interface TraceMark {
    x: number
    y: number
  }
  interface TraceChip {
    x: number
    y: number
    w: number
    l1: string
    l2: string
    refused: boolean
  }
  interface TraceGhost {
    d: string
    tx: number
    ty: number
    text: string
  }
  interface TraceRing {
    x: number
    y: number
    r: number
    alarm: boolean
  }
  interface TraceDrawing {
    mark: TraceMark | null
    chip: TraceChip | null
    leader: string | null
    ghost: TraceGhost | null
    halo: TraceRing | null
    ring: TraceRing | null
  }

  const EMPTY_TRACE_DRAWING: TraceDrawing = { mark: null, chip: null, leader: null, ghost: null, halo: null, ring: null }

  /**
   * Where the trace's own mark, chip, ghost and haloes stand (#1050,
   * rounds 54 & 56, C1). Not part of `scene` above: nothing here takes
   * part in the isometric paint order (`solids`/`paintOrder`) -- like
   * the flat map's own `traceChip`/`traceGhost`, these are a floating
   * annotation over the drawing, not a physical object in it, so they
   * are always on top and never occluded by a building.
   *
   * C1 (owner, 2026-09-09): the ✕ stands where the rule stopped it --
   * the destination district's own gate when the log names an
   * out-interface (round 54's own city drawing already put it there,
   * gate-to-gate roads dying at the far wall), the router's own door
   * when it names none (round 56's addition). A dashed ghost from the
   * gate to the named destination only makes sense in the first case --
   * there is nowhere left to draw one in the second.
   */
  const traceDrawing = $derived.by((): TraceDrawing => {
    if (!traceOn) return EMPTY_TRACE_DRAWING
    const e = mapTraceState.event
    const verdict = mapTraceState.verdict
    if (!e || !verdict) return EMPTY_TRACE_DRAWING
    // Positioned on the geometry camera, same as the rest of the
    // isometric drawing (`scene`, buildings, plates) -- their own tests
    // read a `.trace-stop`/`.trace-chip` translate straight against a
    // plate's own drawn path, which only lines up when both are in the
    // same pre-pan/zoom units. #1050 round 56 defect 3 is a rendered
    // *size* bug, not a position one, and the render block below fixes
    // that with a counter-scale on each piece's own content instead of
    // moving the anchor to a different camera.
    const c = geomCam
    const g = ground
    const refused = verdict === 'refused'
    const inIface = e.inInterface ?? ''
    const outIface = e.outInterface ?? ''
    const rule = e.ruleName || e.ruleLabel || 'no rule named'
    const proto = (e.protocol ?? '').toLowerCase()
    const port = e.dstPort ? `${e.dstPort}/${proto || '?'}` : proto
    const l1 = `${refused ? '✕ REFUSED' : '✓ ACCEPTED'} · ${rule}`
    const l2 = `in: ${inIface || '—'} → out: ${outIface || '—'} · ${port} · ${formatHM(e.time)}`
    const w = Math.max(190, Math.max(l1.length, l2.length) * 6.2 + 24)
    const srcBuilding = e.srcIp ? allBuildings.find((b) => b.ip === e.srcIp) : undefined
    const dstBuilding = e.dstIp ? allBuildings.find((b) => b.ip === e.dstIp) : undefined
    const primaryRouter = g.nodes.find((n) => n.kind === 'router') ?? null

    let mark: TraceMark | null = null
    let chip: TraceChip | null = null
    let leader: string | null = null
    let ghost: TraceGhost | null = null

    if (refused) {
      const dstDistrict = outIface ? districtOf(outIface) : null
      if (dstDistrict) {
        const gate = wallCrossing(dstDistrict, inIface)
        const mx = X(c, gate.p[0])
        const my = Y(c, gate.p[1])
        mark = { x: mx, y: my }
        const cx = mx + 26
        const cy = my - 78
        chip = { x: cx, y: cy, w, l1, l2, refused: true }
        leader = `M${R2(mx)} ${R2(my - 6)}L${R2(cx)} ${R2(cy + 34)}`
        if (dstBuilding) {
          const bx = X(c, dstBuilding.u)
          const by = Y(c, dstBuilding.v)
          const midx = (mx + bx) / 2
          const midy = (my + by) / 2 - 10
          ghost = {
            d: `M${R2(mx)} ${R2(my)}Q${R2(midx)} ${R2(midy)} ${R2(bx)} ${R2(by)}`,
            tx: R2(midx),
            ty: R2(midy - 6),
            text: `would have reached ${e.dstHostName || e.dstIp || 'the host'} · stopped at the ${dstDistrict.name} wall`,
          }
        }
      } else {
        // No out-interface (round 56's input-drop case): the ✕ stands
        // on the router's own door, where the in-interface's road meets
        // it -- the bridge's town-bank head off the WAN, since a plain
        // district has no drawable spoke of its own (traceOverlay's own
        // note, above).
        const inDistrict = districtOf(inIface)
        const bridge = !inDistrict ? g.bridges.find((br) => br.iface === inIface) : null
        const routerNode = (inDistrict ? g.nodes.find((n) => n.id === inDistrict.routerId) : null) ?? primaryRouter
        const doorPt: Pt | null = bridge ? bridge.t : routerNode ? [routerNode.u, routerNode.v] : null
        if (doorPt) {
          const dx = X(c, doorPt[0])
          const dy = Y(c, doorPt[1])
          mark = { x: dx, y: dy }
          const cx = dx - w - 26
          const cy = dy - 78
          chip = { x: cx, y: cy, w, l1, l2, refused: true }
          leader = `M${R2(dx)} ${R2(dy - 6)}L${R2(cx + w)} ${R2(cy + 34)}`
        }
      }
    } else {
      // Accepted: both roads already keep their colour and flow above;
      // the chip stands beside the router that decided, no ✕ and no
      // ghost.
      const inDistrict = districtOf(inIface)
      const routerNode = (inDistrict ? g.nodes.find((n) => n.id === inDistrict.routerId) : null) ?? primaryRouter
      if (routerNode) {
        const rx = X(c, routerNode.u)
        const ry = Y(c, routerNode.v)
        const cx = rx + 30
        const cy = ry - 86
        chip = { x: cx, y: cy, w, l1, l2, refused: false }
        leader = `M${R2(rx)} ${R2(ry - 6)}L${R2(cx)} ${R2(cy + 34)}`
      }
    }

    const halo: TraceRing | null = srcBuilding
      ? { x: X(c, srcBuilding.u), y: Y(c, srcBuilding.v) - 6, r: Math.max(7, c.S * 1.1), alarm: refused }
      : null
    // The ring at the far end, only where the line actually arrived: a
    // refusal's "never reached" is already said by the plaque tally and,
    // when it has one, the ghost's own note -- a second mark on the
    // building itself would say the same thing twice.
    const ring: TraceRing | null =
      !refused && dstBuilding ? { x: X(c, dstBuilding.u), y: Y(c, dstBuilding.v) - 6, r: Math.max(7, c.S * 1.1), alarm: false } : null

    return { mark, chip, leader, ghost, halo, ring }
  })

  /**
   * Where one rule's door stands. `doorHalf` names the side the rule
   * acts on -- the way in for a refusal, the way out for an accept --
   * and that side decides the wall: a district's own wall at the gate
   * the rule's road crosses, or the deck of the bridge when the side is
   * a boundary the river carries.
   *
   * A side this build has no ground for gets no door. The map draws
   * where a rule sits or says nothing; it never picks a plausible wall.
   */
  function doorSpot(door: PortDoor): DoorSpot | null {
    const half = doorHalf(door)
    if (!half) return null
    const d = districtOf(half.from)
    if (d) {
      const gate = wallCrossing(d, half.to || half.from)
      // The wall runs across the road, so the opening is the gate's own
      // normal turned a quarter -- and the posts stand the same width
      // apart as the gate posts already in that wall.
      return { door, p: gate.p, axis: [-gate.n1[1], gate.n1[0]], half: GATE_HALF_WIDTH, where: d.id + ':' + (half.to || half.from) }
    }
    const bridge = ground.bridges.find((br) => br.iface === half.from)
    if (bridge) {
      const dx = bridge.f[0] - bridge.t[0]
      const dy = bridge.f[1] - bridge.t[1]
      const len = Math.hypot(dx, dy) || 1
      // Across the deck, at its middle: the bridge is the boundary, and
      // its width is the opening the rule stands in.
      return { door, p: bridge.mid, axis: [-dy / len, dx / len], half: bridge.w * 0.5, where: 'bridge:' + bridge.iface }
    }
    return null
  }

  /** The reach's own lanes under the brightness rule (round 49, #1016).
   *
   * `rollUpRoads` covers district-pair roads only and says why: a lane
   * belongs to the reach, which is the one surface that draws per line.
   * This is that half, kept out of the scene derived so panning never
   * re-runs it -- the same reasoning `roadBaseline` gives above. */
  const reachLaneBaseline = $derived(
    reachOverlay === null || baselineState.off.lines.length === 0 ? EMPTY_REACH_LANES : rollUpLanes(baselineState.off, reachOverlay.laneSubjects),
  )

  /** Everything on the ground, in the geometry camera. */
  const scene = $derived.by(() => {
    const c = geomCam
    const g = ground
    const groundPaints: Paint[] = []
    const glows: Paint[] = []
    const solids: Solid[] = []

    if (g.river) {
      groundPaints.push(...riverScene(c, g.river))
      for (const b of g.bridges) {
        // A road bridge only ever reads up (lamped) or unknown (unlit) --
        // never down/quiet. A footbridge with no state at all (unknown)
        // draws exactly like down: piers only, no deck lighting, because
        // this build makes no claim either way about a tunnel nothing
        // pushed a state for.
        // Round 49 (#1016): a bridge is an interface, and its deck wears
        // its boundary's own state -- accent with lamps when a rule logs
        // it, white when the operator declared it quiet on purpose, grey
        // with dashed rails when nothing logs. That is a different fact
        // from `state`, which is whether the tunnel is up; the chip
        // below still carries that, and the road bridge never reads
        // up/down at all.
        const cov = b.coverage
        const ink = COVERAGE_INK[cov]
        for (const t of [0.3, 0.7]) {
          const p = lerpP(b.f, b.t, t)
          const pr = gbox(c, p[0], p[1], b.w * 0.5, b.w * 0.5, -0.9, 1.5)
          solids.push({ kind: 'other', v: p[1] + 1, paints: gfaces(pr, 'var(--fg-dim)', { t: 0.5, r: 0.42, l: 0.26, s: 0.4 }), lamps: [] })
        }
        const deck = gbox(c, b.mid[0], b.mid[1], b.half, b.w, 0.55, 0.3)
        const deckOp =
          cov === 'logged' ? { t: 0.3, r: 0.5, l: 0.3, s: 0.55, bg: true } : cov === 'quiet' ? { t: 0.2, r: 0.16, l: 0.1, s: 0.45, bg: true } : { t: 0.14, r: 0.12, l: 0.08, s: 0.35, bg: true }
        const paints = gfaces(deck, ink, deckOp)
        const rail = (sgn: number) => {
          const a: Pt = [b.f[0] + sgn * b.w, b.f[1] - sgn * b.w]
          const z: Pt = [b.t[0] + sgn * b.w, b.t[1] - sgn * b.w]
          return 'M' + P(c, a, 0.85) + 'L' + P(c, z, 0.85)
        }
        const railDash = cov === 'dark' ? '3 4' : undefined
        paints.push(
          { d: rail(1), stroke: ink, so: cov === 'logged' ? 0.75 : 0.5, sw: 1, dash: railDash },
          { d: rail(-1), stroke: ink, so: cov === 'logged' ? 0.55 : 0.35, sw: 1, dash: railDash },
        )
        const lamps: { x: number; y: number; r: number; rr: number; h: number }[] = []
        // A bridge's lamps are coverage, never traffic and never the
        // tunnel's state: a rule logs this boundary, and nothing more.
        // Two on the wide road bridge, one on a footbridge, as drawn.
        if (cov === 'logged') {
          for (let i = 0; i < (b.kind === 'road' ? 2 : 1); i++) {
            const p = lerpP(b.f, b.t, 0.24 + 0.52 * i)
            lamps.push({ x: R2(X(c, p[0] + b.w)), y: R2(Y(c, p[1] - b.w, 0.85)), h: Math.max(7, c.S * 1.5), r: R2(Math.max(2, c.S * 0.36)), rr: R2(Math.max(5, c.S * 0.9)) })
          }
        }
        solids.push({ kind: 'other', v: b.mid[1] + b.w, paints, lamps })

        // The far-bank hamlet (#866): this tunnel's peers, across the
        // water past the footbridge's far head, using the device
        // library's own shapes (#864) -- generic puck unless the
        // entities register already knows the kind for that address.
        if (b.kind === 'foot' && b.peers.length > 0) {
          const dx = b.f[0] - b.t[0]
          const dy = b.f[1] - b.t[1]
          const dl = Math.hypot(dx, dy) || 1
          const centreU = b.f[0] + (dx / dl) * 16
          const centreV = b.f[1] + (dy / dl) * 16
          for (const [i, hp] of hamletPositions(b.peers, [centreU, centreV]).entries()) {
            const peer = b.peers[i]
            const kind = deviceKindFor({ name: peer.name, tags: entityTagsFor(peer) })
            const scale = deviceScale(2.2, c.S)
            solids.push({
              kind: 'hamlet',
              v: hp[1] + 2.2,
              id: b.id + '/' + peer.id,
              aria: peer.name + (peer.address ? ' at ' + peer.address : '') + ', across the water on ' + b.iface,
              attrs: deviceStampAttrs(kind, DEVICE_PREFIX, { ink: 'var(--fg-dim)', scale, x: X(c, hp[0]), y: Y(c, hp[1]) }),
            })
          }
        }
      }
    }

    // Plates.
    const plates = g.districts.map((d) => ({
      d,
      ink: inkOf(d),
      outer: diamond(c, d.u, d.v, d.r, 0),
      inner: diamond(c, d.u, d.v, d.r - 1.8, 0),
      aria:
        d.name +
        ' district' +
        (d.cidr ? ' ' + d.cidr : '') +
        ', ' +
        (d.buildings.length + d.more) +
        // #1165: "1 hosts" on a district holding one.
        (d.buildings.length + d.more === 1 ? ' host' : ' hosts') +
        (!d.rulesPushed
          ? ', no rule table has been pushed yet -- walls show no gates'
          : d.dark
            ? ', nothing logs here'
            : ''),
    }))

    // Walls and gates (#865, and round 49's material rule #1016): every
    // plate's own low prism, broken open only where a pushed accept rule
    // actually crosses that boundary. What an edge is drawn in is what
    // its own boundary logs -- the district's ink where a rule logs,
    // white and translucent where the operator declared the gap quiet on
    // purpose, grey and dashed where nothing logs. There is no coverage
    // lens and no badge: the material is the statement. A wall has no
    // direction, so an edge takes the worse of the gates standing in it
    // (walls.ts's faceCoverage) and the gate's card lists both.
    //
    // A gate that resolves to a point on one of the two back edges the
    // camera cannot see draws nothing -- the same silence a hidden
    // building face keeps -- but still keeps its lamp and rule count for
    // the plaque.
    for (const d of g.districts) {
      const visible: { g: DistrictGate; f: WallBreak }[] = []
      for (const gate of d.gates) {
        const f = faceOf(d, gate.p)
        if (f) visible.push({ g: gate, f })
      }
      const faces = faceCoverage(visible.map((v) => ({ side: v.f.side, coverage: v.g.coverage })))
      const segs = wallSegments(
        d,
        visible.map((v) => v.f),
      )
      for (const seg of segs) {
        const cov = faces[seg.side]
        // Every edge of a ghost's wall wears the watch state (#460,
        // round 55): the wall is dashed all round in the state's ink,
        // whatever each boundary used to read, because a boundary that
        // is gone has no coverage left to report.
        const gs = d.ghost ? ghostStateOf(ghostWatchFor(d.id), nowMs) : null
        const t = gs ? GHOST_WALL[gs] : WALL_TREATMENT[cov]
        const ink = gs ? GHOST_INK[gs] : cov === 'logged' ? inkOf(d) : COVERAGE_INK[cov]
        const mid = (seg.t0 + seg.t1) / 2
        const midV = seg.side === 'l' ? d.v + d.r * mid : d.v + d.r * (1 - mid)
        const path = wallPiece(c, d, seg)
        solids.push({
          kind: 'other',
          v: midV,
          paints: [
            { d: path, fill: VOID, fo: t.back },
            { d: path, fill: ink, fo: t.fo, stroke: ink, so: t.so, sw: t.sw, dash: t.dash },
          ],
          lamps: [],
          wall: { districtId: d.id, side: seg.side, coverage: cov },
        })
      }
      for (const { g: gate, f } of visible) {
        // The gate's own two posts, one either side of the break: accent
        // and lamped when the boundary logs, grey and unlit otherwise.
        // One lamp, on the far post, exactly as the mockup draws it.
        const lit = gate.coverage === 'logged'
        const ink = lit ? 'var(--accent)' : COVERAGE_INK[gate.coverage]
        const op = lit ? { t: 0.6, r: 0.5, l: 0.32, s: 0.7, bg: true } : { t: 0.3, r: 0.22, l: 0.14, s: 0.5, bg: true }
        const dt = d.r > 0 ? GATE_HALF_WIDTH / d.r : 0
        for (const sgn of [-1, 1]) {
          const t = Math.max(0, Math.min(1, f.t + sgn * dt))
          const m = facePoint(d, f.side, t)
          const paints = gfaces(gbox(c, m[0], m[1], GATE_POST_HALF, GATE_POST_HALF, 0, GATE_POST_H), ink, op)
          const lamps =
            lit && sgn === 1
              ? [{ x: R2(X(c, m[0])), y: R2(Y(c, m[1], GATE_POST_H)), h: Math.max(2, c.S * 0.3), r: R2(Math.max(1.9, c.S * 0.3)), rr: R2(Math.max(4.5, c.S * 0.8)) }]
              : []
          solids.push({
            kind: 'other',
            v: m[1] + 0.8,
            paints,
            lamps,
            gate: { districtId: d.id, side: f.side, gateKey: gate.key, toward: gate.toward, coverage: gate.coverage },
          })
        }
      }
      // A gate whose break falls on a face the camera cannot see still
      // stands its lamp at the gate point (#1034): nearly every logging
      // accept rule crosses toward the WAN, so its gate aims at the
      // bridge post and lands on a back face, and skipping those left
      // the one mark that tells a logged gate from a dark one off the
      // whole city. The gate point's own v is the depth: it is exactly
      // the face point wallSegments would have carved.
      for (const gate of d.gates) {
        if (gate.coverage !== 'logged' || visible.some((v) => v.g === gate)) continue
        solids.push({
          kind: 'other',
          v: gate.p[1],
          paints: [],
          lamps: [{ x: R2(X(c, gate.p[0])), y: R2(Y(c, gate.p[1], GATE_POST_H)), h: Math.max(2, c.S * 0.3), r: R2(Math.max(1.9, c.S * 0.3)), rr: R2(Math.max(4.5, c.S * 0.8)) }],
        })
      }
    }

    // The doors (#1055, round 54): every pushed rule that names the
    // selected port, two posts across the opening it guards, with the
    // leaf swung open for an accept and a bar across for anything that
    // refuses. Policy, never traffic -- the same two-post vocabulary the
    // flat map uses for the same rule, moved from the rib to the wall,
    // so a door can never be misread as a line that happened. An unused
    // door still stands: knowing where one is open even when nobody
    // knocked is the point of interrogating a port (owner, 2026-09-08).
    const doorLabels: { x: number; y: number; label: string; act: string; accepts: boolean; key: string }[] = []
    for (const spot of portOverlay?.doors ?? []) {
      const accepts = doorAccepts(spot.door)
      const ink = accepts ? 'var(--accept)' : 'var(--alarm)'
      const foot = (sgn: number): Pt => [spot.p[0] + spot.axis[0] * spot.half * sgn, spot.p[1] + spot.axis[1] * spot.half * sgn]
      const a = foot(1)
      const b = foot(-1)
      const px = (q: Pt, z = 0) => R2(X(c, q[0])) + ' ' + R2(Y(c, q[1], z))
      const posts = 'M' + px(a) + 'L' + px(a, DOOR_H) + 'M' + px(b) + 'L' + px(b, DOOR_H)
      // The leaf hangs from the near post and swings in over the
      // opening; the bar runs post to post at the same height, so the
      // two read as one drawing with one thing changed.
      const leaf = accepts
        ? 'M' + px(a, DOOR_H) + 'L' + R2(X(c, spot.p[0]) + (X(c, a[0]) - X(c, spot.p[0])) * 0.15) + ' ' + R2(Y(c, spot.p[1], DOOR_H * 0.55))
        : 'M' + px(a, DOOR_H * 0.55) + 'L' + px(b, DOOR_H * 0.55)
      solids.push({
        kind: 'other',
        v: spot.p[1] + 0.8,
        paints: [
          { d: posts, stroke: ink, sw: 2, cls: 'round' },
          { d: leaf, stroke: ink, sw: 2, cls: 'round' },
        ],
        lamps: [],
        door: {
          accepts,
          title: spot.door.label + ' · ' + spot.door.who + ' — a pushed rule names this port here',
          where: spot.where,
        },
      })
      doorLabels.push({
        x: R2(X(c, spot.p[0])),
        y: R2(Y(c, spot.p[1], DOOR_H) - 8),
        label: spot.door.label,
        act: accepts ? 'accept' : spot.door.action,
        accepts,
        key: spot.door.device + '#' + spot.door.ordinal,
      })
    }

    // Roads, cut into pieces that carry their own depth.
    const dropLabels: { x: number; y: number; text: string; alarm: boolean }[] = []
    /** The aggregate marks that have a breakdown to open, as boxes over
     * the mark and its word (#1002). The mark is the control, so this is
     * where the reader points; a mark with no rules to name gets none,
     * and stays exactly as it was drawn rather than offering a dead
     * click. */
    const dropHits: { id: string; x: number; y: number; w: number; h: number }[] = []
    const ents = new Map<string, Entity>()
    for (const b of allBuildings) ents.set(b.id, { u: b.u, v: b.v, R: b.R })

    /**
     * Building id -> the ink of the road whose off-baseline traffic
     * arrived there (#1057). Filled by the road loop below and read by
     * `building()`, which draws the outline on the footprint path it is
     * already drawing, so the mark cannot drift off the building.
     *
     * One entry per building however many roads arrive at it: two roads
     * carrying different verdicts into the same building is one mark in
     * the later road's ink, not two outlines fighting over one edge.
     */
    const arrivedInk = new Map<string, string>()

    // The bollards, cross and red mark a dropped road ends at (#865) --
    // pulled out so standing on a building (#868) can pin the same mark
    // exactly where a blocked strand's own road would have crossed the
    // wall, not just where the district-pair aggregate already draws
    // one. `e` is the ground point the mark centres on (depth reads its
    // v, same as every other solid).
    // What the label says is the plain word "dropped", and nothing else
    // (#1036, DESIGN.md's metaphor table: "the plain mark only, reading
    // `dropped`; the refusing rule's name is not written on the drawing
    // -- it lives in the card"). Nothing is written on a road or a
    // strand: if it names something, it is in a card. The refusing rule
    // is still carried -- `Road.refusedBy` on the ground model and
    // `ReachStrand.refusedBy` in the reach -- and the line card and the
    // composer are where it is read.
    //
    // The source is prefixed, and only when the drop is not on the
    // building you are standing on -- #991's own rule, unchanged.
    //
    // `hotFor` is the road whose breakdown this mark opens (#1002), when
    // it has one. Nothing about the mark changes for it: the card is one
    // interaction deeper, and the drawing still says only "dropped".
    function dropMarkAt(e: Pt, alarm: boolean, source?: string, hotFor?: string) {
      const text = source ? source + ' · dropped' : 'dropped'
      const col2 = alarm ? 'var(--alarm)' : 'var(--drop)'
      const px = X(c, e[0])
      const py = Y(c, e[1])
      const bo = (dx: number): Paint => ({ cx: R2(px + dx), cy: R2(py - 2.2 * c.S * ZK * 0.4), rx: R2(0.5 * c.S * IK), ry: R2(1.5 * c.S * ZK * 0.4), fill: col2, fo: 0.5 })
      const k = Math.max(0.9, c.S / 8)
      const mx = R2(px)
      const my = R2(py - 1.8 * c.S * ZK)
      const cross = 'M' + R2(mx - 6 * k) + ' ' + R2(my - 6 * k) + 'L' + R2(mx + 6 * k) + ' ' + R2(my + 6 * k) + 'M' + R2(mx - 6 * k) + ' ' + R2(my + 6 * k) + 'L' + R2(mx + 6 * k) + ' ' + R2(my - 6 * k)
      solids.push({
        kind: 'other',
        v: e[1] + 8,
        paints: [
          bo(-1.6 * c.S * IK),
          bo(1.6 * c.S * IK),
          { d: cross, stroke: col2, sw: R2(2.1 * k), cls: 'round' },
          { cx: mx, cy: my, rx: R2(9.5 * k), ry: R2(9.5 * k), stroke: col2, so: 0.45, sw: 1 },
        ],
        lamps: [],
      })
      dropLabels.push({ x: mx, y: my - 14 * k, text, alarm })
      // One target over the mark and the word above it, never two: they
      // are one thing to point at. Wide enough for the word, and always
      // taller than the 24px a gate pill stands at.
      if (hotFor) {
        const top = my - 22 * k
        const bottom = my + 12 * k
        const half = Math.max(30, 12 * k + text.length * 3)
        dropHits.push({ id: hotFor, x: R2(mx - half), y: R2(top), w: R2(half * 2), h: R2(bottom - top) })
      }
    }

    for (const r of g.roads) {
      if (r.lane && !showLanes) continue
      const own = !reachOverlay || reachOverlay.ownRoadIds.has(r.id)
      // The port filter (#1055) and the trace (#1050) ask the same
      // question of a road that standing does, and answer it the same
      // way: the roads the answer travelled keep their verdict colour,
      // the rest recede. Nothing is taken off the map -- the answer is
      // read against the whole city. The two are mutually exclusive
      // (opening one clears the other), so at most one is ever active.
      const onPort = !portOverlay || portOverlay.litRoadIds.has(r.id)
      const onTrace = traceOverlay !== null && traceOverlay.litRoadIds.has(r.id)
      const onFilter = portOverlay ? onPort : traceOverlay ? onTrace : true
      // The traced line's own verdict, not the pair's aggregate one:
      // `r.k` is every event this pair ever carried, and the one line
      // being traced may disagree with it (an otherwise-accepted pair
      // with one refused line among many, say) -- round 54/56's rule is
      // that this one road takes this one line's colour.
      const traceKind: RoadKind | null = onTrace ? (mapTraceState.verdict === 'refused' ? 'x' : 'a') : null
      // Round 49, DESIGN.md "The reach": the standing building's own
      // roads take the brightness rule "the same rule as everywhere
      // else", and its lanes are among them. A lane is laid down in
      // unjudged ink (layout.ts, `k: 'q'`) because outside the reach it
      // is scenery with no line behind it; inside the reach it is a
      // drawn line, so it is judged like one -- and takes the verdict's
      // own colour rather than staying grey.
      const laneInReach = reachOverlay !== null && own && !!r.lane
      const laneNb: ReachLaneEntry | null = laneInReach ? (reachLaneBaseline.get(r.id) ?? null) : null
      // A road off the answer goes grey -- the city's own unjudged ink,
      // the one it already lays a lane down in, rather than a second
      // grey invented for either filter.
      const col = !onFilter ? VERDICT.q : traceKind ? VERDICT[traceKind] : laneNb ? VERDICT[laneNb.kind] : VERDICT[r.k]
      // Brightness is the baseline (round 49, #1016). An accepted road
      // carrying nothing off today's pattern is *established*: thin, dim
      // and with no flow, so it recedes without ever leaving the map.
      // One off-baseline line among a thousand established ones brings
      // the whole road up to full width and full brightness -- the road
      // is as bright as the brightest line it carries.
      //
      // Only accepted roads take part. A refused road and the escalated
      // unplanned pair are unchanged: their colour is already the point,
      // and dimming a refusal because it happens every day would hide
      // exactly the traffic this screen exists to show.
      const pairNb: RoadBaselineEntry | null = roadBaseline.get(r.id) ?? null
      const nb: { lines: OffBaselineLine[]; ring: RoadRing } | null = pairNb ?? laneNb
      // Which roads the rule judges at all: accepted pair roads always,
      // and the reach's own lanes while it is open.
      const judged = r.k === 'a' || laneInReach
      const est = judged && nb === null
      // A road off the answer thins exactly as an established one does:
      // one visual word for "not what is being asked about", never a
      // second (round 54's own rule, and #868's before it). The traced
      // line is a single answered question, not a volume reading, so its
      // own road never thins for being otherwise established.
      const faded = onTrace ? false : est || !onFilter
      const w = Math.max(faded ? 1 : 1.2, r.w * c.S * (faded ? 0.18 : 0.3))
      // Standing on a building (#868) fades every road that is not its
      // own; either filter fades every road it never travelled.
      const opBase = r.k === 'x' ? 0.95 : !judged && r.k === 'q' ? 0.42 : !judged && r.k === 'd' ? 0.52 : est ? 0.26 : 0.8
      const op = (onTrace ? Math.max(opBase, 0.8) : opBase) * (own ? 1 : 0.16) * (onFilter ? 1 : 0.16)
      // A road flows exactly when it carries something off the baseline
      // or is the escalated unplanned pair -- the flow dashes are part
      // of the bright treatment, not a separate signal. Volume does not
      // earn flow, so a settled network is still, however busy it is.
      // The trace draws its own flow instead, forward on an accepted
      // line and never on a refused one -- the read this screen exists
      // to show for one line rather than for a baseline.
      //
      // Standing narrows that to this building's own roads; it does not
      // widen it. The round-49 mockup flows every own road in the reach
      // (`flow: own ? mine : ...`), but DESIGN.md's brightness rule --
      // which wins where the two disagree -- puts established roads at
      // "thin, dim, no flow" and says the reach follows "the same rule
      // as everywhere else". So an established road the standing host
      // owns recedes exactly like any other, and the dashes stay the
      // mark of a line off the pattern rather than of ownership.
      const flow = traceOverlay ? onTrace && traceKind === 'a' : (!reachOverlay || own) && onFilter && (r.k === 'x' || nb !== null)
      const reversed = !!reachOverlay?.reverseIds.has(r.id)
      let cum = 0
      const pieces = roadPieces(r, ents)
      const glowD: string[] = []
      for (const p of pieces) {
        const q = p.q
        const d = 'M' + P(c, q[0]) + 'C' + P(c, q[1]) + ' ' + P(c, q[2]) + ' ' + P(c, q[3])
        const fade = r.fade ? Math.max(0, 1 - Math.max(0, p.gt - 0.35) * 1.75) : 1
        if (fade > 0) glowD.push(d)
        const paints: Paint[] = [{ d, stroke: col, sw: R2(w), so: RO(op * fade), cls: r.k === 'x' ? 'road-alarm' : undefined }]
        const fl: Paint | null = flow
          ? { d, stroke: col, sw: R2(Math.max(1.1, w * 0.42)), so: RO(0.9 * fade), cls: reversed ? 'flow flow-rev' : 'flow', dash: String(R2(-cum)) }
          : null
        cum += Math.hypot(X(c, q[3][0]) - X(c, q[0][0]), Y(c, q[3][1]) - Y(c, q[0][1]))
        solids.push({ kind: 'piece', v: pieceDepth(p), paints, flow: fl, label: r.label, roadId: r.id })
      }
      // The wide faint casing is part of the bright treatment: an
      // established road drops it along with its width, which is what
      // makes a dim road read as one hairline rather than as a soft band
      // (round-49/index.html:869-871).
      if (glowD.length && !faded) glows.push({ d: glowD.join(''), stroke: col, sw: R2(w + 4), so: 0.07 })
      // The arrival mark, at the end the traffic arrived at. #1057: it
      // is the arrived-at building's own outline, not a circle on the
      // ground beside it -- a ring round a dot is how the flat map marks
      // a node, and the city has a silhouette to draw instead. Nothing
      // else about the mark changes: same ink, same weight, same states,
      // same lifetime, and the same `.halo` rule the flag pill uses, so
      // it still throbs in place and never pulses outward (DESIGN.md,
      // owner 2026-09-07) and there is one motion in the city, not two.
      //
      // The outline itself is drawn by `building()` below, off this map,
      // so it is the footprint path the building already draws rather
      // than a second idea of where the building is.
      if (nb && judged) {
        if (nb.ring.start) for (const b of endBuildings(r, 'start', pairNb)) arrivedInk.set(b.id, col)
        if (nb.ring.end) for (const b of endBuildings(r, 'end', pairNb)) arrivedInk.set(b.id, col)
      }
      // #991: the district-pair aggregate has no per-building source to
      // name (only the reach's own strands, below, resolve to one host),
      // so this mark names no source. Nor does it name the refusing rule
      // -- `Road.refusedBy` is the events' own rule label, and #1036
      // keeps it off the drawing and in the card.
      //
      // That card is this mark's own (#1002): the pair is the one level
      // that knows every rule which refused here, so the mark opens the
      // breakdown `Road.dropBreakdown` carries. Not while standing --
      // the reach fades every road but its own, and its marks answer
      // for their own strand instead.
      if (r.stop === 'drop') dropMarkAt(r.pts[r.pts.length - 1], r.k === 'x', undefined, !reachOverlay && (r.dropBreakdown?.length ?? 0) > 0 ? r.id : undefined)
    }
    // #991: "gone from the street stop" -- the road port chips (#868's
    // "ports on the road") are dropped entirely; the ports live on the
    // building's card and the gate's card instead. Nothing else about
    // roads changes.
    // The reach's own marks are pointable, and open the same line card
    // the road would have. A refused strand across a boundary nothing
    // logs draws no road at all -- one there would claim a log line
    // nobody wrote -- so without this its line would have nowhere to
    // open its card, and `draft the rule ▸` with it.
    const markHits: { id: string; x: number; y: number }[] = []
    if (reachOverlay)
      for (const dm2 of reachOverlay.dropMarks) {
        dropMarkAt(dm2.p, false, dm2.source)
        markHits.push({ id: dm2.id, x: R2(X(c, dm2.p[0])), y: R2(Y(c, dm2.p[1]) - 1.8 * c.S * ZK) })
      }

    // Buildings flat on the district plate (#986 dropped height and the
    // plinth, #867's own concept): the device symbol stamped on top
    // keeps deviceScale's footprint-only size, and the plate below it
    // is the one diamond at ground level, tinted by district ink --
    // never a raised wall or roof face.
    const building = (b: Building, d: District | null) => {
      // The same dim styling a dark district already draws its
      // buildings in also carries standing on a building (#868): every
      // building that is neither the standing host nor one it reaches
      // or is reached by fades, reusing one visual word for "not what
      // matters right now" rather than inventing a second.
      // ... and the port filter and the trace with them (#1055, #1050):
      // a host not on the answer dims inside a lit district, and a
      // district with none goes to outline by every one of its
      // buildings dimming at once. Only a machine the answer could name
      // is asked: a router or a bridge post is not a host, and dimming
      // it would be the drawing answering a question nobody put to it.
      const dim =
        (d?.dark ?? false) ||
        (reachOverlay ? !reachOverlay.litBuildingIds.has(b.id) : false) ||
        (portOverlay && b.host ? !portOverlay.litBuildingIds.has(b.id) : false) ||
        (traceOverlay && b.host ? !traceOverlay.litBuildingIds.has(b.id) : false)
      // Presence is the building's own ink (round 49, #1016): a host not
      // heard for the quiet window goes grey with a dashed footprint, one
      // marked quiet on purpose goes white and translucent. Both stay on
      // the map -- only a dismissal removes a building, and that happens
      // upstream in presence.ts, where the host never becomes one.
      // Presence outranks the district's ink because it is a statement
      // about this machine, not about the boundary it stands behind.
      const pres = b.host?.presence ?? 'live'
      const presInk = pres === 'quiet' ? 'var(--fg-dim)' : pres === 'intended' ? 'var(--fg)' : null
      const ink = presInk ?? (dim ? 'var(--fg-dim)' : d ? inkOf(d) : 'var(--accent)')
      const R = b.R
      const h = 0
      const pin = (hh: number) => diamond(c, b.u, b.v, R, hh)
      // Only quiet is dashed. Quiet on purpose is solid and white, the
      // same word the declared boundary and the white footbridge deck
      // use: the operator said something, so the drawing is not missing
      // anything. Dashed means nobody has.
      const dashed = pres === 'quiet' || (dim && pres === 'live')
      const paints: Paint[] = [
        { d: pin(0), fill: '#0a0f1c', fo: dim || pres !== 'live' ? 0.7 : 0.94 },
        {
          d: pin(0),
          fill: ink,
          fo: pres === 'intended' ? 0.1 : pres === 'quiet' ? 0.12 : dim ? 0.12 : 0.26,
          stroke: ink,
          so: pres === 'quiet' ? 0.6 : pres === 'intended' ? 0.35 : dim ? 0.55 : 0.95,
          sw: 1,
          dash: dashed ? '3 3' : undefined,
        },
      ]
      // The off-baseline arrival mark (#1057): this building's own
      // outline, in the ink of the road that carried the traffic, over
      // the footprint the building already draws. `.halo` is the flag
      // pill's own rule, so it throbs in place and never pulses outward
      // (DESIGN.md, owner 2026-09-07); `arrived` is what a check points
      // at. The animation drives stroke-width, so `sw` here is only what
      // a reduced-motion reader sees held still -- the ring's own 1.4.
      const arrived = arrivedInk.get(b.id)
      if (arrived) paints.push({ d: pin(0), fill: 'none', stroke: arrived, sw: 1.4, cls: 'halo arrived' })
      const what = b.kind === 'router' ? 'router' : b.kind === 'router-ant' ? 'router with antennas' : b.kind === 'post' ? 'bridge post' : 'host'
      // The mark (#981, round 46): whatever the flag and watchlist
      // ledgers say about this machine, and nothing else. There is no
      // pill in front of it any more -- a mark is drawn while there is
      // something behind it and gone when there is not (owner,
      // 2026-09-08), so what a screen reader is told and what is drawn
      // are the same fact by construction.
      const mark = b.host ? markFor(b.kind, b.host) : null
      // A ghost's host says what it is rather than how long it has been
      // quiet: "not heard for 26 h" is true of every address in a
      // retired range and tells the operator nothing.
      const note = d?.ghost ? 'last known here; the range is retired' : b.host ? presenceNote(b.host, nowTick) : null
      const aria =
        b.name +
        (b.ip ? ' at ' + b.ip : '') +
        ', ' +
        what +
        (d ? ' in ' + d.name : '') +
        (note ? ' · ' + note : '') +
        (mark && mark.flags ? ' · ' + mark.flags + (mark.flags === 1 ? ' flag' : ' flags') : '') +
        (mark && mark.watch ? ' · watched' : '') +
        (mark?.spike ? ' · activity spike' : '')
      const k = (R * 0.74 * c.S) / SREF
      solids.push({
        kind: 'building',
        v: buildingDepth(b),
        b,
        district: d,
        ink,
        dim,
        pres,
        mark,
        paints,
        stamp: { x: R2(X(c, b.u)), y: R2(Y(c, b.v, h)), k: R2(k) },
        aria,
      })
    }
    for (const d of g.districts) for (const b of d.buildings) building(b, d)
    for (const n of g.nodes) building(n, null)

    // Borough rings: a dashed hull round everything one router owns.
    const rings: { d: string; label: string; x: number; y: number }[] = []
    if (showBoroughs) {
      for (const bo of g.boroughs) {
        const pts: Pt[] = []
        const corners = (u: number, v: number, r: number) => {
          for (const o of [
            [0, -r],
            [r, 0],
            [0, r],
            [-r, 0],
          ])
            pts.push([X(c, u + o[0]), Y(c, v + o[1])])
        }
        for (const d of g.districts) if (bo.districtIds.includes(d.id)) corners(d.u, d.v, d.r)
        for (const n of g.nodes) if (n.id === bo.routerId) corners(n.u, n.v, n.R)
        if (pts.length < 3) continue
        pts.sort((a, b) => a[0] - b[0] || a[1] - b[1])
        const cross = (o: Pt, a: Pt, b: Pt) => (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0])
        const lower: Pt[] = []
        const upper: Pt[] = []
        for (const p of pts) {
          while (lower.length >= 2 && cross(lower[lower.length - 2], lower[lower.length - 1], p) <= 0) lower.pop()
          lower.push(p)
        }
        for (let i = pts.length - 1; i >= 0; i--) {
          const p = pts[i]
          while (upper.length >= 2 && cross(upper[upper.length - 2], upper[upper.length - 1], p) <= 0) upper.pop()
          upper.push(p)
        }
        let hull = lower.slice(0, -1).concat(upper.slice(0, -1))
        const cx = hull.reduce((s, p) => s + p[0], 0) / hull.length
        const cy = hull.reduce((s, p) => s + p[1], 0) / hull.length
        hull = hull.map((p): Pt => {
          const dx = p[0] - cx
          const dy = p[1] - cy
          const m = Math.hypot(dx, dy) || 1
          return [p[0] + (dx / m) * 34, p[1] + (dy / m) * 26]
        })
        const d = hull.map((p, i) => (i ? 'L' : 'M') + R2(p[0]) + ' ' + R2(p[1])).join('') + 'Z'
        // A router with more zones than the ground plan has slots for
        // loses the rest off the map -- no district, no roads, no signal
        // -- so the borough's own label says how many, the same
        // convention a district's plaque follows for hosts beyond its
        // own cap (#1073).
        const zoneNote = bo.moreZones > 0 ? ` · +${bo.moreZones} zone${bo.moreZones === 1 ? '' : 's'} not shown` : ''
        rings.push({
          d,
          // A ghost is inside the ring -- it is still a place on the
          // map -- but it is not a district any more, so the label
          // counts the two apart (#460, round 55: "4 districts · 1
          // ghost").
          label:
            boroughLabel(
              bo.name.toUpperCase() + ' BOROUGH',
              bo.districtIds.filter((id) => !g.districts.find((d) => d.id === id)?.ghost).length,
              bo.districtIds.filter((id) => g.districts.find((d) => d.id === id)?.ghost).length,
            ) + zoneNote,
          x: R2(Math.max(...hull.map((p) => p[0])) - 60),
          y: R2(Math.min(...hull.map((p) => p[1])) + 10),
        })
      }
    }
    clearDropLabels(dropLabels, rings)

    // Labels claim their rectangle: anything that would land on one
    // already placed is dropped rather than drawn over it.
    const placed: [number, number, number, number][] = []
    const claim = (x: number, y: number, w: number, h: number) => {
      const r: [number, number, number, number] = [x - w / 2, y, x + w / 2, y + h]
      for (const p of placed) if (r[0] < p[2] - 4 && r[2] > p[0] + 4 && r[1] < p[3] - 4 && r[3] > p[1] + 4) return false
      placed.push(r)
      return true
    }
    // A plaque is name and subnet, and (dim, on its own line) whether a
    // rule table was ever pushed. It carries no coverage word any more,
    // so it needs no room for one.
    const plaques: { d: District; x: number; y: number; w: number; ink: string }[] = []
    // The filter's own line under a plaque (#1055): `2 of 12 · 445/tcp`,
    // in the same chip the footbridge's state wears. It stands under the
    // plaque it belongs to, claims its own rectangle like every other
    // label here, and is drawn only where the plaque itself was -- a
    // tally floating over a district whose name was dropped for space
    // would belong to nothing.
    const portTallies: { id: string; x: number; y: number; w: number; t: string }[] = []
    for (const d of g.districts) {
      const x = R2(X(c, d.u))
      const y = R2(Y(c, d.v + d.r) + 5)
      const w = compact ? d.name.length * 7.2 + 26 : 200
      const h = compact ? 20 : d.rulesPushed ? 28 : 40
      if (!claim(x, y, w, h)) continue
      plaques.push({ d, x, y, w: R2(w), ink: inkOf(d) })
      // The trace's own tallies (#1050) take the same rectangle the port
      // filter's do -- the two never carry one at once, since only one
      // of the overlays is ever active.
      const t = portOverlay?.tallies.get(d.id) ?? traceOverlay?.tallies.get(d.id)
      if (!t) continue
      const tw = t.length * 6 + 16
      const ty = R2(y + h + 15)
      if (claim(x, ty - 9, tw, 18)) portTallies.push({ id: d.id, x, y: ty, w: R2(tw), t })
    }
    // Only a footbridge carries a state chip: the road bridge (the WAN)
    // never reads up/down/quiet, it is only ever lamped or unlit, and
    // that is shown by the lamp itself, not a label.
    const bridgeChips: { x: number; y: number; w: number; t: string; stroke: string }[] = []
    for (const b of g.bridges) {
      if (b.kind !== 'foot') continue
      const t = b.iface + ' · tunnel · ' + bridgeStateLabel(b.state)
      const w = t.length * 5.9 + 18
      const x = R2(X(c, b.mid[0]) + c.S * 2.2)
      const y = R2(Y(c, b.mid[1]) + Math.max(20, c.S * 3.2))
      if (!claim(x, y - 9, w, 18)) continue
      const stroke = b.state === 'up' ? 'rgba(232,176,90,0.45)' : b.state === 'down' ? 'rgba(255,84,112,0.4)' : 'var(--hair-2)'
      bridgeChips.push({ x, y, w: R2(w), t, stroke })
    }

    return { groundPaints, glows, plates, solids: paintOrder(solids), rings, plaques, portTallies, bridgeChips, dropLabels, doorLabels, markHits, dropHits, claim }
  })

  /** Names float over buildings at the street stop, for what the
   * viewport shows; the claim is re-run so they never overlap. */
  const toppers = $derived.by(() => {
    if (effectiveStop !== 'street') return []
    const c = geomCam
    const vp = viewport
    const out: { b: Building; x: number; y: number; w: number; sub: string; quiet: boolean }[] = []
    const placed: [number, number, number, number][] = []
    for (const b of allBuildings) {
      if (b.u < vp.u0 || b.u > vp.u1 || b.v < vp.v0 || b.v > vp.v1) continue
      const k = (b.R * 0.74 * c.S) / SREF
      const x = R2(X(c, b.u))
      const y = R2(Y(c, b.v, 0) - symbolFor(b.kind).top * k - 10)
      // A quiet building says how long instead of its address (round 49,
      // #1016): the address is on its card, and how long it has been
      // silent is the thing the street stop is there to show.
      const note = b.host ? presenceNote(b.host, nowTick) : null
      const sub = note ?? b.ip
      const w = Math.max(b.name.length, sub.length) * 6.6 + 22
      const r: [number, number, number, number] = [x - w / 2, y - 28, x + w / 2, y + 8]
      if (placed.some((p) => r[0] < p[2] - 4 && r[2] > p[0] + 4 && r[1] < p[3] - 4 && r[3] > p[1] + 4)) continue
      placed.push(r)
      out.push({ b, x, y, w, sub, quiet: note !== null })
    }
    return out
  })

  /* ---------------- the boundary card, and declaring ---------------- */

  // Cards are the one interaction (DESIGN.md "Cards"): hover opens one,
  // and the pin keeps it when the pointer leaves. The wording here is
  // the 2D map's, because the two surfaces are drawn and worded to one
  // rule -- a boundary reads the same whichever side of the slider you
  // are on.
  /** Which wall a card is open on. One object, never an id without a
   * side: `openWallCard` used to take the two apart, and the card's own
   * `onpointerenter` passed it `openWall!.side` against an `openWall`
   * that was already null by then (#1027). There is no longer anywhere
   * to write that. */
  type WallRef = {
    districtId: string
    side: WallSide
    /** Which gate in that edge the reader pointed at. A wall piece names
     * none -- an edge takes the worse of the gates standing in it, and
     * that is the one its card describes -- but a gate post names its
     * own, so a card opened from a post is about the gate under the
     * pointer (#1016). */
    gateKey?: string
  }

  let hoverWall = $state<WallRef | null>(null)
  let pinnedWall = $state<WallRef | null>(null)
  let declareReason = $state('')
  /** Declaring covers both directions by default: one direction declared
   * and the other still dark would leave the wall grey and the card
   * explaining why (round 49's item 7, ratified as drawn). */
  let declareBoth = $state(true)
  let declareBusy = $state(false)

  const openWall = $derived(pinnedWall ?? hoverWall)
  const wallPinned = $derived(pinnedWall !== null)

  function sameWall(a: WallRef | null, b: WallRef | null): boolean {
    return a !== null && b !== null && a.districtId === b.districtId && a.side === b.side
  }

  const COVERAGE_WORD: Record<Coverage, string> = {
    logged: 'logged',
    quiet: 'quiet on purpose',
    dark: 'dark',
  }

  /** What one direction across the boundary actually says, in plain
   * words -- never more than the pushed table supports. */
  function directionDetail(dir: { coverage: Coverage; ruleCount: number }): string {
    if (dir.coverage === 'logged') return dir.ruleCount > 0 ? `${dir.ruleCount} accept ${dir.ruleCount === 1 ? 'rule' : 'rules'}, logging` : 'a rule on it logs'
    if (dir.coverage === 'quiet') return 'declared quiet on purpose'
    return dir.ruleCount > 0 ? `${dir.ruleCount} accept ${dir.ruleCount === 1 ? 'rule' : 'rules'}, none with log=yes` : 'nothing on it logs'
  }

  /** The card for whichever wall edge is open: the gate that gave the
   * edge its reading, and both of that boundary's directions. An edge
   * with no gate has no boundary to describe, and opens nothing. */
  const wallCard = $derived.by(() => {
    const w = openWall
    if (!w) return null
    const d = ground.districts.find((x) => x.id === w.districtId)
    if (!d) return null
    const here = d.gates.filter((gt) => faceOf(d, gt.p)?.side === w.side)
    if (here.length === 0) return null
    // Pointed at from a gate post, the card is that gate's. Pointed at
    // from the wall it stands in, it is the worst-covered gate in the
    // edge -- the one that gave the edge its material.
    let gate = (w.gateKey ? here.find((g) => g.key === w.gateKey) : null) ?? here[0]
    if (!w.gateKey) for (const g of here) if (worseCoverage(gate.coverage, g.coverage) === g.coverage && g.coverage !== gate.coverage) gate = g
    const declaration = gate.directions.map((x) => coverageState.byKey.get(x.edgeKey)).find((x) => x !== undefined) ?? null
    return { d, gate, declaration }
  })

  /** The card's grace period (#1027): shared with the 2D map, so the
   * two surfaces cannot drift apart on how long the pointer has to get
   * from a boundary to its own card. */
  const cardGrace = grace()

  function openWallCard(w: WallRef) {
    if (drag?.moved) return
    cardGrace.hold()
    hoverWall = w
  }

  /** The pointer has left the wall, or the card. It may be on its way to
   * the other one, so the card is not taken down until the grace period
   * has passed with the pointer arriving nowhere. */
  function releaseWallCard() {
    const w = hoverWall
    if (!w) return
    cardGrace.release(() => {
      if (sameWall(hoverWall, w)) hoverWall = null
    })
  }
  function toggleWallPin() {
    const w = openWall
    if (!w) return
    if (wallPinned) {
      pinnedWall = null
      return
    }
    pinnedWall = { districtId: w.districtId, side: w.side, gateKey: w.gateKey }
    // Pinned, the card opens the declare form with whatever reason is
    // already on record, so an existing declaration is edited rather
    // than silently replaced by an empty one.
    coverageState.error = null
    declareReason = wallCard?.declaration?.reason ?? ''
    declareBoth = true
  }

  /** Which keys a declaration writes: this direction, or both -- a wall
   * has no direction, so both is the default. */
  function declareKeys(): string[] {
    const c = wallCard
    if (!c) return []
    const dirs = c.gate.directions
    return declareBoth ? dirs.map((x) => x.edgeKey) : dirs.slice(0, 1).map((x) => x.edgeKey)
  }

  async function submitDeclaration() {
    if (!declareReason.trim() || declareBusy) return
    declareBusy = true
    let ok = true
    for (const key of declareKeys()) ok = (await coverageState.declare(key, declareReason.trim())) && ok
    declareBusy = false
    if (ok) pinnedWall = null
  }

  async function removeDeclaration() {
    const c = wallCard
    if (!c || declareBusy) return
    declareBusy = true
    let ok = true
    for (const dir of c.gate.directions) if (coverageState.byKey.has(dir.edgeKey)) ok = (await coverageState.undeclare(dir.edgeKey)) && ok
    declareBusy = false
    if (ok) pinnedWall = null
  }

  /** The same second way in the 2D map's declare panel offers (#435): a
   * dark boundary is exactly what Log every rule exists to fix. */
  function openRulesForBoundary() {
    const c = wallCard
    if (!c || !primaryDevice) return
    logEveryRuleNavState.request(primaryDevice.id, c.gate.key)
    appState.view = 'tune-logging'
  }

  /* ---------------- where the card floats ---------------- */

  // Round 49 puts the card beside the boundary it describes, joined to
  // it by a leader with an accent dot at the boundary's end. The drawing
  // places each card by hand, per scene (round-49/index.html:1340-1350);
  // here the subject moves, so the placement is worked out from the live
  // camera and re-worked every time anything moves it. The rules
  // themselves are in lib/cardAnchor.ts, shared with the 2D map.
  let cityEl: HTMLDivElement | undefined = $state()
  let bcardEl: HTMLDivElement | undefined = $state()
  let cardPlace = $state<Placement | null>(null)
  /** Bumped when the stage changes size under us, which no camera or
   * stop change reports. */
  let stageTick = $state(0)
  /** Bumped when the card itself changes size -- the declare form going
   * in makes it taller, and where it can sit depends on how tall it is
   * (#1028). */
  let cardTick = $state(0)

  /**
   * The open boundary's own wall, as boxes the card keeps off (#1030).
   *
   * The 2D map's card kept clear of the plates its title named and then
   * sat on the boundary line between them, hiding the tail of the one
   * thing it was describing. The rule is the shared module's, so the
   * city keeps off its own wall by the same call: `drawnPathRects`
   * walks a stroked line into a chain of boxes and takes a solid shape
   * -- which a wall panel is -- as its own box.
   */
  function openWallRects(host: Element): Rect[] {
    const out: Rect[] = []
    for (const el of host.querySelectorAll('g.wall-hot.on path')) out.push(...drawnPathRects(el, host))
    return out
  }

  /** A district's plate as a box on the stage, its wall included. */
  function plateBox(d: { u: number; v: number; r: number }): Rect {
    const x = X(viewCam, d.u - d.r)
    const y = Y(viewCam, d.v - d.r, WALL_H)
    return { x, y, w: X(viewCam, d.u + d.r) - x, h: Y(viewCam, d.v + d.r, 0) - y }
  }

  $effect(() => {
    // Read first, so this re-runs on everything that moves the subject:
    // which wall is open, the camera (pan and zoom alike), the stop, the
    // card's own arrival in the DOM, and the stage's size.
    const w = openWall
    const c = viewCam
    const svg = svgEl
    const host = cityEl
    const card = bcardEl
    void effectiveStop
    void stageTick
    void cardTick

    if (!w || !svg || !host || !card) {
      cardPlace = null
      return
    }
    const d = districtOf(w.districtId)
    if (!d) {
      cardPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      cardPlace = null
      return
    }
    const a = facePoint(d, w.side, 0.5)
    // Half way up the wall, and half way along it: the point the
    // drawing's accent dot sits on.
    const anchor = map({ x: X(c, a[0]), y: Y(c, a[1], WALL_H / 2) })
    // Both ends of the boundary, not just this one. The card names two
    // districts, and sitting on either of them hides half of what it is
    // describing -- which is exactly what the corner panel did to
    // DarkLane.
    const toward = wallCard ? (ground.districts.find((x) => x.id === wallCard.gate.toward || x.name === wallCard.gate.toward) ?? null) : null
    const ends = toward && toward.id !== d.id ? [d, toward] : [d]
    // Only the plates a reader can actually see (#1028). The city draws
    // every district at every stop, so this changes nothing today; it
    // stops the card ever keeping off a plate that is not on screen,
    // which is the mistake the 2D map made when its two zone layers
    // swapped under the same placement.
    const shown = (x: { id: string }) => host.querySelector(`g.plate[data-cid="${CSS.escape(x.id)}"]`)
    const drawnPlate = (x: { id: string; u: number; v: number; r: number }) => {
      const el = shown(x)
      return el !== null && drawnRect(el, host) !== null ? [mapRect(map, plateBox(x))] : []
    }
    // The wall itself goes in with them (#1030): the plates alone never
    // stopped the card coming down on the boundary line it is about.
    const avoid = ends.flatMap(drawnPlate).concat(openWallRects(host))
    // Every other plate is worth keeping clear too, but only as a
    // tie-break: at the city stop the whole estate is on screen and
    // insisting would leave nowhere to put the card at all.
    const softAvoid = ground.districts.filter((x) => !ends.some((e) => e.id === x.id)).flatMap(drawnPlate)
    cardPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  // The stage's size is the one input nothing else reports: a window
  // resize, a sidebar opening, the browser's own zoom. ResizeObserver
  // where there is one, and the window as the fallback -- jsdom has
  // neither laid out nor observed anything, and the card falls back to
  // its unplaced position there rather than to a wrong one.
  $effect(() => {
    const host = cityEl
    if (!host || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => stageTick++)
    ro.observe(host)
    return () => ro.disconnect()
  })

  // And the card's own size, which nothing else reports either: the
  // declare form goes in behind the pin and the card gets taller
  // (#1028). watchCardSize is the 2D map's too -- one rule, both
  // surfaces -- and it says there why this cannot move the card round in
  // circles. The city has room where it sits today, which is luck
  // rather than the rule holding; the same wiring makes it the rule.
  $effect(() => {
    const card = bcardEl
    if (!card) return
    return watchCardSize(card, () => cardTick++)
  })

  /* ---------------- the host card (round 49, #1016) ---------------- */

  // The same card, the same rules: hover opens it, the pin keeps it, and
  // it floats beside its building on a leader (lib/cardAnchor.ts, shared
  // with the boundary card above and with the 2D map). Its wording is
  // the 2D map's host card word for word -- presence, last and first
  // seen, events, and `mark quiet on purpose ▸ · dismiss ▸` -- because
  // the two surfaces are one product and a host must read the same on
  // either side of the slider.
  let hoverHost = $state<string | null>(null)
  let pinnedHost = $state<string | null>(null)
  let markReason = $state('')
  let markBusy = $state(false)

  const openHostId = $derived(pinnedHost ?? hoverHost)
  const hostPinned = $derived(pinnedHost !== null)
  const hostGrace = grace()

  /** The building a host card is open on, with its district and the
   * register's record. Only a host has one: a router or a bridge post is
   * not something the syslog feed hears, so it has no presence to show
   * and opens nothing. */
  const hostCard = $derived.by(() => {
    const id = openHostId
    if (!id) return null
    for (const d of ground.districts) {
      const b = d.buildings.find((x) => x.id === id)
      if (b?.host) return { b, d, h: b.host }
    }
    return null
  })

  function openHostCard(b: Building) {
    if (drag?.moved || !b.host) return
    hostGrace.hold()
    hoverHost = b.id
  }

  /** The pointer has left the building, or the card. It may be crossing
   * from one to the other, so nothing is taken down until the grace
   * period passes with it arriving nowhere -- the same 180 ms the
   * boundary card and the 2D map use (#1027). */
  function releaseHostCard() {
    const id = hoverHost
    if (!id) return
    hostGrace.release(() => {
      if (hoverHost === id) hoverHost = null
    })
  }

  function toggleHostPin() {
    const c = hostCard
    if (!c) return
    if (hostPinned) {
      pinnedHost = null
      return
    }
    pinnedHost = c.b.id
    hostsState.error = null
    markReason = c.h.reason ?? ''
  }

  /** `mark quiet on purpose` needs a reason, so it pins the card and
   * opens the form behind the pin -- exactly what declaring a boundary
   * does, one interaction for both. */
  function openMarkForm() {
    const c = hostCard
    if (!c) return
    pinnedHost = c.b.id
    hostsState.error = null
    markReason = c.h.reason ?? ''
  }

  async function submitHostMark() {
    const c = hostCard
    if (!c || !markReason.trim() || markBusy || !c.h.key) return
    markBusy = true
    const ok = await hostsState.mark(c.h.key, 'intended', markReason.trim())
    markBusy = false
    if (ok) pinnedHost = null
  }

  /** Dismissing takes the building off the map. It is not a deletion and
   * nothing has to remember it: the server clears the dismissal the
   * moment the feed hears the host again, so a dismissed host that comes
   * back comes back by itself. */
  async function dismissHost() {
    const c = hostCard
    if (!c || markBusy || !c.h.key) return
    markBusy = true
    const ok = await hostsState.mark(c.h.key, 'dismissed')
    markBusy = false
    if (ok) {
      pinnedHost = null
      hoverHost = null
    }
  }

  /** Taking the mark back, whichever it was. DESIGN.md names the two
   * marks and not their undo; this is the boundary card's `undeclare ▸`
   * read across to a host, because a statement you cannot withdraw is
   * worse than one you never made. */
  async function unmarkHost() {
    const c = hostCard
    if (!c || markBusy || !c.h.key) return
    markBusy = true
    const ok = await hostsState.unmark(c.h.key)
    markBusy = false
    if (ok) pinnedHost = null
  }

  /** What the card's first line says the host is. */
  const PRESENCE_WORD: Record<string, string> = { live: 'live', quiet: 'quiet', intended: 'quiet on purpose', dismissed: 'dismissed' }

  /** #1165: a host only the event buffer has seen carries no stamps and
   * no count, so the card said "live" directly above "0 events · last
   * seen not recorded" and contradicted itself. The feed is all that has
   * heard it, and that is what the card says -- the register line below
   * already explains why there is nothing else. One function so the
   * card's aria-label and its first line cannot disagree. */
  const hostWord = (h: CityHost): string => (h.presence === 'live' && h.events === 0 && !h.lastSeen ? 'seen in the feed' : PRESENCE_WORD[h.presence])

  const stamp = (iso: string | null): string => (iso ? new Date(iso).toLocaleString() : 'not recorded')

  /** The quiet window, in the card's own words -- the configured figure,
   * not a constant repeated here, so the card cannot disagree with the
   * rule that drew the building. */
  const quietWindow = $derived(Math.round(HOST_QUIET_AFTER_MS / 3_600_000) + ' h')

  /* ---- where the host card floats ---- */

  let hcardEl: HTMLDivElement | undefined = $state()
  let hostPlace = $state<Placement | null>(null)
  let hostCardTick = $state(0)

  /** A building as a box on the stage, its stamp included -- what the
   * card must not sit on top of. */
  function buildingBox(b: Building): Rect {
    const k = (b.R * 0.74 * viewCam.S) / SREF
    const x = X(viewCam, b.u - b.R)
    const y = Y(viewCam, b.v - b.R, 0) - symbolFor(b.kind).top * k
    return { x, y, w: X(viewCam, b.u + b.R) - x, h: Y(viewCam, b.v + b.R, 0) - y }
  }

  $effect(() => {
    // Same reads as the boundary card's placement, for the same reason:
    // everything that moves the subject has to move the card.
    const c = hostCard
    const vc = viewCam
    const svg = svgEl
    const host = cityEl
    const card = hcardEl
    void effectiveStop
    void stageTick
    void hostCardTick

    if (!c || !svg || !host || !card) {
      hostPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      hostPlace = null
      return
    }
    const k = (c.b.R * 0.74 * vc.S) / SREF
    // The top of the device, which is the point the leader's accent dot
    // sits on: the card is about the building, not the ground under it.
    const anchor = map({ x: X(vc, c.b.u), y: Y(vc, c.b.v, 0) - symbolFor(c.b.kind).top * k })
    const avoid = [mapRect(map, buildingBox(c.b))]
    // Its own plate is worth keeping clear, but only as a tie-break: at
    // the street stop the plate fills the stage, and insisting would
    // leave nowhere at all to put the card.
    const softAvoid = ground.districts.map((x) => mapRect(map, plateBox(x)))
    hostPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  // The card grows when the mark form opens behind the pin, and where it
  // can sit depends on how tall it is (#1028) -- the same wiring the
  // boundary card has.
  $effect(() => {
    const card = hcardEl
    if (!card) return
    return watchCardSize(card, () => hostCardTick++)
  })

  /* ------------- the off-baseline card (round 49, #1016) ------------- */

  // The card on a bright road: what it carries that is off today's
  // pattern, and the one way a line leaves the bright state early.
  // Placement is cardAnchor's, exactly as the boundary and host cards do
  // it -- the card sits beside its road on a leader, keeps off the
  // plates actually drawn, survives the pointer travelling to it (#1027)
  // and re-places when the reason form makes it taller (#1028).
  let hoverRoad = $state<string | null>(null)
  let pinnedRoad = $state<string | null>(null)
  /** Which lines the open reason form will speak for: one named line
   * (the default), or every line this road carries off the baseline.
   * Null means no form is open. Shared with the 2D rib card through
   * lib/city/expected, so the two surfaces cannot drift apart. */
  let expectedScope = $state<ExpectedScope | null>(null)
  let expectedReason = $state('')
  let expectedBusy = $state(false)
  const roadGrace = grace()
  let rcardEl: HTMLDivElement | undefined = $state()
  let roadPlace = $state<Placement | null>(null)
  let roadCardTick = $state(0)

  const openRoadId = $derived(pinnedRoad ?? hoverRoad)
  const roadPinned = $derived(pinnedRoad !== null)

  /** The open road, its roll-up and the geometry the leader points at.
   *
   * Null while standing: in the reach the same road opens the line card
   * below instead, which is the card DESIGN.md "Cards" gives that
   * surface. One road never has two cards at once. */
  const roadCard = $derived.by(() => {
    const id = openRoadId
    if (!id || reachOverlay) return null
    const entry = roadBaseline.get(id)
    if (!entry) return null
    const road = ground.roads.find((r) => r.id === id)
    if (!road) return null
    return { road, entry }
  })

  /* ---------------- the line card (round 49, #1016) ----------------
     Hovering one of the standing building's roads: what that line
     carried, port by port, from lib/reach's own `reachLineSummary`.
     The 2D map builds its line card from the same function, so the two
     surfaces read as one product rather than as two summaries that
     happened to agree on the day they were written (DESIGN.md, #865's
     `worstUnplannedOf` reasoning applied to this card).

     It reuses the road card's hover state, grace, element ref and
     placement: only one of the two can be open, because one needs the
     reach and the other refuses to draw inside it. */

  /** The hovered road's line, and everything the card says about it. */
  const lineCard = $derived.by(() => {
    const id = openRoadId
    const overlay = reachOverlay
    const summary = standReach
    if (!id || !overlay || !summary) return null
    const counterpart = overlay.lineOf.get(id)
    if (counterpart === undefined) return null
    // The subject is a line, and two things can draw one: the road it
    // runs along, and -- where the boundary logs nothing and so draws no
    // road -- the mark at the wall it stopped at. The leader points at
    // whichever of the two the reader is looking at.
    const road = ground.roads.find((r) => r.id === id) ?? null
    const mark = road ? null : (overlay.dropMarks.find((m) => m.id === id) ?? null)
    const anchor: Pt | null = road ? (road.pts[Math.floor(road.pts.length / 2)] ?? road.pts[0] ?? null) : (mark?.p ?? null)
    if (!anchor) return null

    const line = reachLineSummary(summary.strands, counterpart)
    // The strands on this counterpart, busiest first -- `strands` is
    // already in that order, so the first is the one whose direction and
    // peer the title reads. `reachLineSummary` deliberately drops
    // direction (the drawn flow says it), so the title takes it from
    // here rather than inventing one.
    const lead = summary.strands.find((s) => s.counterpart === counterpart) ?? null
    const peerName = lead?.peers[0] ?? (counterpart === 'internet' ? 'the internet' : counterpart)
    const peerAddr = lead?.peerAddrs[0] ?? null
    // The port the refusal was about: the busiest one anything was
    // dropped on. `ports` is already busiest first.
    const refusedPort = line.ports.find((p) => p.dropped > 0) ?? null
    // The plates this line runs between, for the card to keep off --
    // the standing building's own district and the counterpart's, the
    // same two ends `roadEnds` gives the off-baseline card.
    const token = counterpart === 'internet' ? (zonesState.wanInterface ?? '') : counterpart
    const endIds = new Set([standDistrictId, districtOf(token)?.id].filter((x): x is string => typeof x === 'string'))
    return { id, anchor, line, lead, peerName, peerAddr, refusedPort, hostName: standSideName, endIds }
  })

  /** The line card's title, in the order the traffic went: the same
   * `a → b` the composer already prints for a strand. */
  const lineTitle = (hostName: string, peerName: string, direction: ReachStrand['direction'] | null): string =>
    direction === 'in' ? `${peerName} → ${hostName}` : `${hostName} → ${peerName}`

  /** The card's accessible name, and the road's while the reach is open. */
  function lineAria(id: string): string | null {
    const overlay = reachOverlay
    const summary = standReach
    if (!id || !overlay || !summary) return null
    const counterpart = overlay.lineOf.get(id)
    if (counterpart === undefined) return null
    const lead = summary.strands.find((s) => s.counterpart === counterpart) ?? null
    const peerName = lead?.peers[0] ?? (counterpart === 'internet' ? 'the internet' : counterpart)
    return `${lineTitle(standSideName, peerName, lead?.direction ?? null)} line, ports and what each drew`
  }

  /** The composer, shown only when asked for (round 49): two cards
   * offer `draft the rule ▸` and this is what either opens -- the
   * refused line's own card, and the standing host's card, which is the
   * one that is always there to be asked (#1035).
   * Reset on surfacing, so standing on the next building starts from the
   * drawing rather than from the last building's draft. */
  let composerOpen = $state(false)

  /** How many lines a road carries off the baseline, in plain words. */
  const offCount = (n: number) => `${n} off the baseline today`

  /** The road's accessible name: the two ends, and what it carries.
   * Inside the reach it is the line's name instead, because that is the
   * card the road opens there. */
  function roadAria(id: string): string {
    const inReach = lineAria(id)
    if (inReach) return inReach
    const e = roadBaseline.get(id)
    if (!e) return 'road'
    return `${endName(ground, e.ends.start)} → ${endName(ground, e.ends.end)} road, ${offCount(e.lines.length)}`
  }

  /** Which roads are pointable: one carrying something off the baseline,
   * and -- while standing -- one of the standing building's own that has
   * a line behind it. */
  function roadHasCard(id: string): boolean {
    if (reachOverlay) return reachOverlay.lineOf.has(id)
    return roadBaseline.has(id)
  }

  function openRoadCard(id: string) {
    if (drag?.moved) return
    // Only a road with something to say has a card. Outside the reach
    // that is a road carrying something off the baseline: an established
    // road's answer is the drawing itself, and a card saying "nothing to
    // report" on every road in the city would be noise of exactly the
    // kind this screen exists to remove. Inside the reach it is a road
    // the standing building owns, whose card is its line.
    if (!roadHasCard(id)) return
    roadGrace.hold()
    hoverRoad = id
  }

  function releaseRoadCard() {
    const id = hoverRoad
    if (!id) return
    roadGrace.release(() => {
      if (hoverRoad === id) hoverRoad = null
    })
  }

  function toggleRoadPin(id: string) {
    if (pinnedRoad === id) {
      pinnedRoad = null
      expectedScope = null
    } else {
      pinnedRoad = id
      hoverRoad = id
    }
  }

  /** The lines the open form is about to speak for, and how many. */
  const expectedLines = $derived(expectedScope && roadCard ? expectedTargets(expectedScope, roadCard.entry.lines) : [])

  /** Open the reason form for one line -- the default action, one per
   * listed line. The reason is required either way. */
  function startExpectedOne(line: OffBaselineLine, roadId: string) {
    openExpectedForm({ kind: 'one', key: line.key }, roadId)
  }

  /** Open the reason form for every line this road carries off the
   * baseline. Only ever reached from the control that said how many. */
  function startExpectedAll(roadId: string) {
    openExpectedForm({ kind: 'all' }, roadId)
  }

  function openExpectedForm(scope: ExpectedScope, roadId: string) {
    pinnedRoad = roadId
    hoverRoad = roadId
    expectedScope = scope
    expectedReason = ''
    baselineState.error = null
  }

  async function submitExpected() {
    const targets = expectedLines
    const reason = expectedReason.trim()
    if (targets.length === 0 || !reason || expectedBusy) return
    expectedBusy = true
    try {
      // One write per line, the same reason against each: the endpoint's
      // key is a single line, and a bulk mark is that write repeated
      // rather than a second kind of record.
      //
      // baselineState.expected re-reads the register on success, so the
      // road goes dim by itself the moment the last of its lines is
      // spoken for -- nothing here has to model that. Marking one of
      // several leaves the rest in the register, so the road stays
      // bright.
      for (const l of targets) {
        // Stops at the first refusal rather than pressing on: the error
        // is shown, and a half-written statement the operator cannot see
        // the shape of is worse than none.
        if (!(await baselineState.expected(l.key, reason))) return
      }
      expectedScope = null
      expectedReason = ''
    } finally {
      expectedBusy = false
    }
  }

  $effect(() => {
    // Everything that moves the subject, read first: which road is open,
    // the camera, the stop, the card's arrival, and both size ticks.
    //
    // The road card and the line card are the same subject placed the
    // same way -- a road, its middle waypoint, the plates it joins --
    // and only one of them is ever open, so they share this placement
    // rather than keeping two copies of it that could drift.
    const c = roadCard ?? lineCard
    const vc = viewCam
    const svg = svgEl
    const host = cityEl
    const card = rcardEl
    void effectiveStop
    void stageTick
    void roadCardTick

    if (!c || !svg || !host || !card) {
      roadPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      roadPlace = null
      return
    }
    // The leader points at the road's middle waypoint, which is the part
    // of it a reader is looking at -- not at either end, where the ring
    // already has something to say. The line card brings its own anchor,
    // because its subject can be a mark at a wall rather than a road.
    const at: Pt = 'entry' in c ? (c.road.pts[Math.floor(c.road.pts.length / 2)] ?? c.road.pts[0]) : c.anchor
    const anchor = map({ x: X(vc, at[0]), y: Y(vc, at[1]) })
    // Keep off the two plates this road joins, and off the others only
    // as a tie-break -- the same reasoning the boundary card gives.
    const shown = (x: { id: string }) => host.querySelector(`g.plate[data-cid="${CSS.escape(x.id)}"]`)
    const drawnPlate = (x: { id: string; u: number; v: number; r: number }) => {
      const el = shown(x)
      return el !== null && drawnRect(el, host) !== null ? [mapRect(map, plateBox(x))] : []
    }
    const endIds = 'entry' in c ? new Set([c.entry.ends.start, c.entry.ends.end]) : c.endIds
    const avoid = ground.districts.filter((x) => endIds.has(x.id)).flatMap(drawnPlate)
    const softAvoid = ground.districts.filter((x) => !endIds.has(x.id)).flatMap(drawnPlate)
    roadPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = rcardEl
    if (!card) return
    return watchCardSize(card, () => roadCardTick++)
  })

  /* ---------------- the drop card (#1002) ----------------
     Where the refusing rules live. The drawing says only "dropped"
     (#1036, DESIGN.md's metaphor table), so the names and the counts
     are one interaction deeper: the aggregate mark is the control, and
     what it opens is a card in the same family as every other one here
     -- hover opens it, the pin keeps it, and it floats beside the mark
     on a leader (lib/cardAnchor, shared with the boundary, host, road
     and line cards, and with the 2D map).

     The pair is the level that can answer this. A single strand knows
     one catcher (`ReachStrand.refusedBy`, the latest drop's label) and
     the composer already says it; `Road.dropBreakdown` keeps every rule
     that refused on the pair, with a count each, from the same events
     the drawing counts. */

  let hoverDrop = $state<string | null>(null)
  let pinnedDrop = $state<string | null>(null)
  const dropGrace = grace()
  let dcardEl: HTMLDivElement | undefined = $state()
  let dropPlace = $state<Placement | null>(null)
  let dropCardTick = $state(0)

  const openDropId = $derived(pinnedDrop ?? hoverDrop)
  const dropPinned = $derived(pinnedDrop !== null)

  /** The open mark's road, what refused there, and the two names the
   * header reads. Null when the road has gone -- a fresh rule table can
   * stop drawing it, and a card about a mark nobody can see is a card
   * about nothing. */
  const dropCard = $derived.by(() => {
    const id = openDropId
    if (!id) return null
    const road = ground.roads.find((r) => r.id === id && r.stop === 'drop')
    if (!road) return null
    const rules = road.dropBreakdown ?? []
    if (rules.length === 0) return null
    const ends = roadEnds(road, ground.districts)
    if (!ends) return null
    const anchor = road.pts[road.pts.length - 1]
    if (!anchor) return null
    // Largest first is the card's own promise, so the card is what keeps
    // it rather than trusting whatever order it was handed.
    const rows = [...rules].sort((a, b) => b.count - a.count)
    return {
      id,
      anchor,
      rows,
      total: rows.reduce((n, r) => n + r.count, 0),
      from: endName(ground, ends.start),
      to: endName(ground, ends.end),
      endIds: new Set([ends.start, ends.end]),
      alarm: road.k === 'x',
    }
  })

  /** The card's title, in the composer's own words: a refusal reads the
   * same wherever it is said. */
  const dropTitle = $derived(dropCard ? `${dropCard.from} → ${dropCard.to} · refused at this wall` : '')

  /** What one row says. A drop that carried no rule label at all is said
   * plainly, in the composer's phrase, and keeps its place in the
   * busiest-first order rather than being folded into a named rule or
   * pushed to the end (#865/#967: never guessed, never hidden). */
  const dropRow = (r: { rule: string | null; count: number }): string => `${r.rule ?? 'caught, no rule named'} · ${r.count}`

  /** The mark's accessible name carries the answer up front, so it never
   * takes opening the card to learn what happened here. */
  function dropAria(id: string): string {
    const road = ground.roads.find((r) => r.id === id)
    const rules = road?.dropBreakdown ?? []
    const ends = road ? roadEnds(road, ground.districts) : null
    const total = rules.reduce((n, r) => n + r.count, 0)
    const where = ends ? `${endName(ground, ends.start)} → ${endName(ground, ends.end)}` : 'this boundary'
    return `${where}, dropped ${total}× by ${rules.length} ${rules.length === 1 ? 'rule' : 'rules'}`
  }

  function openDropCard(id: string) {
    if (drag?.moved) return
    dropGrace.hold()
    hoverDrop = id
  }

  /** The pointer has left the mark, or the card. The same grace the
   * other cards take, for the same reason (#1027): it may be on its way
   * from one to the other. */
  function releaseDropCard() {
    const id = hoverDrop
    if (!id) return
    dropGrace.release(() => {
      if (hoverDrop === id) hoverDrop = null
    })
  }

  function toggleDropPin(id: string) {
    if (pinnedDrop === id) {
      pinnedDrop = null
      return
    }
    pinnedDrop = id
    hoverDrop = id
  }

  /** Escape's first rung (#1002). A card is opened on purpose, so it is
   * the first thing Escape takes back; surfacing from standing is the
   * rung below, and `onWindowKeydown` reads them in that order. */
  function closeDropCard() {
    pinnedDrop = null
    hoverDrop = null
  }

  $effect(() => {
    // The same reads as every other card's placement: the subject, the
    // camera, the stop, the card's arrival, and both size ticks.
    const c = dropCard
    const vc = viewCam
    const svg = svgEl
    const host = cityEl
    const card = dcardEl
    void effectiveStop
    void stageTick
    void dropCardTick

    if (!c || !svg || !host || !card) {
      dropPlace = null
      return
    }
    const map = unitMapper(svg, host)
    const stage = stageRect(svg, host)
    if (!map || !stage) {
      dropPlace = null
      return
    }
    // The leader points at the mark itself, which is the thing the
    // reader clicked and the thing the card is about.
    const anchor = map({ x: X(vc, c.anchor[0]), y: Y(vc, c.anchor[1]) })
    const shown = (x: { id: string }) => host.querySelector(`g.plate[data-cid="${CSS.escape(x.id)}"]`)
    const drawnPlate = (x: { id: string; u: number; v: number; r: number }) => {
      const el = shown(x)
      return el !== null && drawnRect(el, host) !== null ? [mapRect(map, plateBox(x))] : []
    }
    const avoid = ground.districts.filter((x) => c.endIds.has(x.id)).flatMap(drawnPlate)
    const softAvoid = ground.districts.filter((x) => !c.endIds.has(x.id)).flatMap(drawnPlate)
    dropPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = dcardEl
    if (!card) return
    return watchCardSize(card, () => dropCardTick++)
  })

  /* ---------------- the minimap ---------------- */

  const MINI_W = 264
  const MINI_H = 148
  const miniCam = $derived(minimapCam(ground.bounds, MINI_W, MINI_H))
  const mini = $derived.by(() => {
    const mc = miniCam
    const g = ground
    const bank = (a: Pt[]) => a.map((p, i) => (i ? 'L' : 'M') + R2(X(mc, p[0])) + ' ' + R2(Y(mc, p[1]))).join('')
    const river = g.river ? bank(g.river.bankN) + bank(g.river.bankF.slice().reverse()).replace('M', 'L') + 'Z' : ''
    const plates = g.districts.map((d) => ({
      d: diamond(mc, d.u, d.v, d.r, 0),
      ink: inkOf(d),
      fo: d.plateDark ? 0.22 : 0.5,
      name: d.name,
      x: R2(X(mc, d.u)),
      // The name sits under the plate's bottom vertex (#978), not over
      // the diamond and its device dots -- clamped so a plate at the
      // panel's own bottom edge keeps its name inside the svg.
      y: R2(Math.min(MINI_H - 3, Y(mc, d.v + d.r) + 8)),
    }))
    const nodes = g.nodes.filter((n) => n.kind !== 'post').map((n) => ({ x: R2(X(mc, n.u)), y: R2(Y(mc, n.v)) }))
    return { river, plates, nodes }
  })
  const miniView = $derived.by(() => {
    const mc = miniCam
    const vp = viewport
    // Clamped into the panel (#1000): at the city stop the viewport can
    // be wider than the estate itself (cityFitS is capped, and the fit
    // leaves slack on the non-binding axis), which used to push the
    // rectangle's edges outside the svg where they clipped invisible.
    // The owner's ask is a big rectangle covering everything at the
    // default framing, shrinking as you zoom -- so an off-panel edge
    // stops at the panel's inset instead of vanishing.
    const inset = 1.5
    const x0 = Math.max(inset, X(mc, vp.u0))
    const y0 = Math.max(inset, Y(mc, vp.v0))
    const x1 = Math.min(MINI_W - inset, X(mc, vp.u1))
    const y1 = Math.min(MINI_H - inset, Y(mc, vp.v1))
    return { x: R2(x0), y: R2(y0), w: R2(Math.max(0, x1 - x0)), h: R2(Math.max(0, y1 - y0)) }
  })
  const viewShare = $derived.by(() => {
    const b = ground.bounds
    const vp = viewport
    const a = Math.max(0, Math.min(vp.u1, b.u1) - Math.max(vp.u0, b.u0)) * Math.max(0, Math.min(vp.v1, b.v1) - Math.max(vp.v0, b.v0))
    const whole = (b.u1 - b.u0) * (b.v1 - b.v0) || 1
    return Math.round((a / whole) * 100)
  })

  const tabbable = (id: string) => (focus ? focus.id === id : ground.districts[0]?.id === id) ? 0 : -1
</script>

<svelte:window onkeydown={onWindowKeydown} />

{#snippet otherPaints(paints: Paint[], lamps: { x: number; y: number; r: number; rr: number; h: number }[])}
  {#each paints as p, j (j)}
    {#if p.d}
      <path
        d={p.d}
        fill={p.fill ?? 'none'}
        fill-opacity={p.fo}
        stroke={p.stroke}
        stroke-opacity={p.so}
        stroke-width={p.sw}
        stroke-dasharray={p.dash}
        stroke-linecap={p.cls === 'round' ? 'round' : undefined}
      />
    {:else}
      <!-- No ellipse carries `halo` any more (#1057 moved the arrival
           mark onto the building's own outline); `cls` stays because a
           Paint carries one and this is the sink that renders it. -->
      <ellipse class={p.cls} cx={p.cx} cy={p.cy} rx={p.rx} ry={p.ry} fill={p.fill ?? 'none'} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} />
    {/if}
  {/each}
  {#each lamps as l, j (j)}
    <path d="M{l.x} {l.y}V{R2(l.y - l.h)}" stroke="var(--accent)" stroke-opacity="0.6" stroke-width="1" />
    <circle class="lamp" cx={l.x} cy={R2(l.y - l.h)} r={l.r} fill="var(--now)" />
    <circle class="lamp" cx={l.x} cy={R2(l.y - l.h)} r={l.rr} fill="var(--now)" fill-opacity="0.13" />
  {/each}
{/snippet}

<div class="city" data-stop={effectiveStop} bind:this={cityEl}>
  <!-- The traced line's own crumb, plus its list (#1018 round 53, #1050
       round 56 A1) -- the same component Topography mounts, so the same
       markup and behaviour appear on the city too. -->
  <TraceCrumb />
  <svg
    bind:this={svgEl}
    viewBox="0 0 {STAGE_W} {STAGE_H}"
    preserveAspectRatio="xMidYMid meet"
    role="application"
    aria-label={stand
      ? `Standing on ${standName}${standSub ? ' at ' + standSub : ''}: Escape surfaces to where you were`
      : `The estate as a city at the ${effectiveStop} stop: drag or hold Shift with the arrow keys to pan; arrow keys walk the districts and their buildings`}
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
  >
    <CityDeviceDefs prefix={DEVICE_PREFIX} />
    <g transform={viewTransform}>
      <g class="ground">
        {#if ground.river}
          <g role="img" aria-label="The Internet as a river along the north edge of town">
            {#each scene.groundPaints as p, i (i)}
              <path d={p.d} fill={p.fill ?? 'none'} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} class={p.cls} />
            {/each}
          </g>
        {/if}
        {#each scene.rings as r (r.label)}
          <path d={r.d} fill="none" stroke="var(--accent)" stroke-opacity="0.3" stroke-width="1" stroke-dasharray="2 6" stroke-linejoin="round" />
        {/each}
        {#each scene.plates as p (p.d.id)}
          {@const gs = p.d.ghost ? ghostStateOf(ghostWatchFor(p.d.id), nowMs) : null}
          <g
            class="plate"
            class:focused={focus?.id === p.d.id}
            role="button"
            tabindex={tabbable(p.d.id)}
            data-cid={p.d.id}
            aria-label={p.aria}
            onclick={() => onDistrictClick(p.d.id)}
            onkeydown={onKey}
          >
            <title>{p.aria}</title>
            <!-- The plate keeps its own ink unless every one of the
                 district's boundaries is dark (round 49, #1016); then,
                 and only then, it goes grey and dashed.
                 A ghost is a third case (#460, round 55): no fill of its
                 own, an outline in the watch's ink, dashed all round,
                 and an alarm rim when a straggler has broken it. -->
            {#if gs}
              <path d={p.outer} fill={GHOST_INK[gs]} fill-opacity={gs === 'broken' ? 0.07 : 0.04} />
              <path
                d={p.inner}
                fill="none"
                stroke={GHOST_INK[gs]}
                stroke-opacity={gs === 'none' ? 0.55 : 0.85}
                stroke-width="1.3"
                stroke-dasharray="3 4"
              />
              {#if gs === 'broken'}
                <path d={p.outer} fill="none" stroke="var(--alarm)" stroke-opacity="0.35" stroke-width="1.2" />
              {/if}
            {:else}
              <path d={p.outer} fill={p.d.plateDark ? 'var(--fg-dim)' : p.ink} fill-opacity={p.d.plateDark ? 0.06 : 0.1} />
              <path
                d={p.inner}
                fill="none"
                stroke={p.d.plateDark ? 'var(--fg-dim)' : p.ink}
                stroke-opacity={p.d.plateDark ? 0.2 : 0.17}
                stroke-width="0.7"
                stroke-dasharray={p.d.plateDark ? '3 4' : undefined}
              />
            {/if}
            <path class="ring" d={p.outer} fill="none" stroke="var(--accent)" stroke-width="1.2" stroke-dasharray="4 5" />
          </g>
        {/each}
        {#each scene.glows as p, i (i)}
          <path d={p.d} fill="none" stroke={p.stroke} stroke-width={p.sw} stroke-opacity={p.so} />
        {/each}
      </g>
      <g class="solids">
        {#each scene.solids as s, i (i)}
          {#if s.kind === 'piece'}
            {#each s.paints as p, j (j)}
              <path d={p.d} fill="none" stroke={p.stroke} stroke-width={p.sw} stroke-opacity={p.so} class={p.cls} data-v={R2(s.v)} data-road={s.roadId} />
            {/each}
            {#if s.flow}
              <path d={s.flow.d} fill="none" stroke={s.flow.stroke} stroke-width={s.flow.sw} stroke-opacity={s.flow.so} class={s.flow.cls ?? 'flow'} stroke-dashoffset={s.flow.dash} data-road={s.roadId} />
            {/if}
            {#if s.roadId && roadHasCard(s.roadId) && s.paints[0]?.d}
              <!-- A road with a card is pointable, on a wide transparent
                   stroke because the road itself is too thin to hit
                   (round-49/index.html:1204). Outside the reach that
                   card is the off-baseline roll-up; inside it, it is the
                   standing building's own line card. While either is
                   open this stroke goes visible, which is how the
                   subject stays marked. -->
              {@const rid = s.roadId}
              <path
                class="road-hot"
                class:on={openRoadId === rid}
                d={s.paints[0].d}
                fill="none"
                stroke="transparent"
                stroke-width="14"
                role="button"
                tabindex="-1"
                aria-label={roadAria(rid)}
                data-road-hot={rid}
                onpointerenter={() => openRoadCard(rid)}
                onpointerleave={releaseRoadCard}
                onclick={() => onRoadClick(rid)}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onRoadClick(rid)
                  }
                }}
              />
            {/if}
          {:else if s.kind === 'other'}
            {#if s.wall}
              <!-- A wall piece is pointable: hovering it opens its own
                   boundary's card, which is where the declare path
                   starts (DESIGN.md "Cards"). Everything else in this
                   branch is scenery and takes no pointer. -->
              {@const w = s.wall}
              <g
                class="wall-hot"
                class:on={sameWall(openWall, { districtId: w.districtId, side: w.side })}
                role="button"
                tabindex="-1"
                aria-label="{districtOf(w.districtId)?.name ?? w.districtId} wall, {COVERAGE_WORD[w.coverage]}"
                data-wall="{w.districtId}:{w.side}"
                onpointerenter={() => openWallCard({ districtId: w.districtId, side: w.side })}
                onpointerleave={releaseWallCard}
                onclick={() => openWallCard({ districtId: w.districtId, side: w.side })}
                onkeydown={(e) => e.key === 'Enter' && openWallCard({ districtId: w.districtId, side: w.side })}
              >
                {@render otherPaints(s.paints, s.lamps)}
              </g>
            {:else if s.gate}
              <!-- A gate post is pointable too, and opens the same card
                   the wall it stands in opens -- about this gate rather
                   than the worst one in the edge. `data-gate` is what a
                   live check counts gates by (#1022): the posts are the
                   only thing the city draws per gate, and before this
                   they were anonymous geometry with nothing to ask. -->
              {@const gt = s.gate}
              <g
                class="wall-hot"
                class:on={sameWall(openWall, { districtId: gt.districtId, side: gt.side })}
                role="button"
                tabindex="-1"
                aria-label="{districtOf(gt.districtId)?.name ?? gt.districtId} gate toward {gt.toward}, {COVERAGE_WORD[gt.coverage]}"
                data-gate="{gt.districtId}:{gt.gateKey}"
                onpointerenter={() => openWallCard(gt)}
                onpointerleave={releaseWallCard}
                onclick={() => openWallCard(gt)}
                onkeydown={(e) => e.key === 'Enter' && openWallCard(gt)}
              >
                {@render otherPaints(s.paints, s.lamps)}
              </g>
            {:else if s.door}
              <!-- `data-door` names the wall the rule put this door in,
                   the same way a gate post carries `data-gate` (#1022):
                   the door is the only thing drawn per rule, and without
                   it a check has nothing to ask where it stands. -->
              <g class="door" class:shut={!s.door.accepts} data-door={s.door.where}>
                <title>{s.door.title}</title>
                {@render otherPaints(s.paints, s.lamps)}
              </g>
            {:else}
              {@render otherPaints(s.paints, s.lamps)}
            {/if}
          {:else if s.kind === 'hamlet'}
            <g class="hamlet" role="img" aria-label={s.aria}>
              <title>{s.aria}</title>
              <use href={s.attrs.href} transform={s.attrs.transform} style={s.attrs.style} />
            </g>
          {:else}
            <g
              class="blk"
              class:focused={focus?.id === s.b.id}
              role="button"
              tabindex={s.district ? tabbable(s.b.id) : -1}
              class:hot={!!s.b.host}
              data-cid={s.b.id}
              data-near={R2(s.b.v + s.b.R)}
              data-presence={s.pres}
              aria-label={s.aria}
              onclick={() => onBuildingClick(s.district?.id ?? null, s.b.id)}
              onkeydown={onKey}
              onpointerenter={() => openHostCard(s.b)}
              onpointerleave={releaseHostCard}
              onfocus={() => openHostCard(s.b)}
              onblur={releaseHostCard}
            >
              <title>{s.aria}</title>
              {#if focus?.id === s.b.id}
                <path d={diamond(geomCam, s.b.u, s.b.v, s.b.R * 1.9, 0)} fill="var(--accent)" fill-opacity="0.07" stroke="var(--accent)" stroke-opacity="0.5" stroke-width="1" stroke-dasharray="4 5" />
              {/if}
              {#each s.paints as p, j (j)}
                <!-- `cls` carries the off-baseline arrival outline's
                     `halo arrived` (#1057), which is what makes it throb
                     in place; the animation sets stroke-width, so it
                     overrides the attribute below by design. -->
                <path d={p.d} fill={p.fill} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} stroke-dasharray={p.dash} class={p.cls} />
              {/each}
              <g transform="translate({s.stamp.x} {s.stamp.y})">
                <!-- A ghost's hosts are drawn in the quiet material and
                     then faded (#460, round 55): they are names the range
                     last had, not machines anyone can see now. -->
                <g
                  transform="scale({s.stamp.k})"
                  style:color={s.ink}
                  opacity={s.district?.ghost ? 0.45 : s.pres === 'quiet' ? 0.62 : s.pres === 'intended' ? 0.55 : s.dim ? 0.62 : undefined}
                >
                  {#each symbolFor(s.b.kind).paths as p, j (j)}
                    <path d={p.d} fill={p.fill === 'void' ? VOID : 'currentColor'} fill-opacity={p.fillOpacity} stroke={p.fill === 'body' ? 'currentColor' : undefined} stroke-opacity={p.strokeOpacity} stroke-width={p.strokeWidth} />
                  {/each}
                </g>
                {#if s.mark}
                  <!-- The mark (#981, round 46): the symbol re-stamped in
                       alarm ink over its ordinary self, one convex-hull
                       silhouette per fact, and nothing else. No disc, no
                       ring, no number, no glyph -- the counts are words
                       on the click card and nowhere else. -->
                  <g class="mark" transform="scale({s.stamp.k})">
                    {#if s.mark.flags > 0}
                      <!-- The re-stamp is a sibling of the base stamp, so
                           it inherits no ink of its own: every shape the
                           symbol is built from takes the alarm colour at
                           its own normal per-face shading, over the
                           district-ink stamp below it. -->
                      <g class="mk-fill" opacity={s.mark.fillOpacity} style:color="var(--alarm)">
                        {#each symbolFor(s.b.kind).paths as p, j (j)}
                          <path d={p.d} fill={p.fill === 'void' ? VOID : 'currentColor'} fill-opacity={p.fillOpacity} stroke={p.fill === 'body' ? 'currentColor' : undefined} stroke-opacity={p.strokeOpacity} stroke-width={p.strokeWidth} />
                        {/each}
                      </g>
                      {#if s.mark.spike}
                        <!-- An activity spike is the one flag kind that is
                             happening now, so its rim breathes and a soft
                             blurred copy swells alongside it. Red
                             throughout: the animation moves opacity and
                             width, never hue (owner, ratifying #981). -->
                        <path class="mk-glow mk-spike-glow" d={s.mark.rimD} fill="none" stroke="var(--alarm)" stroke-width={s.mark.glowWidth} stroke-linejoin="round" />
                      {/if}
                      <path class="mk-rim" class:mk-spike-rim={s.mark.spike} d={s.mark.rimD} fill="none" stroke="var(--alarm)" stroke-width={s.mark.rimWidth} stroke-linejoin="round" />
                    {/if}
                    {#if s.mark.watch > 0}
                      <!-- A touch proud of the flag rim when there is one,
                           so the two lines sit side by side rather than on
                           top of one another. -->
                      <path class="mk-watch" d={s.mark.watchD} fill="none" stroke="var(--marked)" stroke-width={s.mark.watchWidth} stroke-linejoin="round" />
                    {/if}
                  </g>
                {/if}
              </g>
            </g>
          {/if}
        {/each}
      </g>
      <g class="flat" aria-hidden="true">
        {#each scene.plaques as p (p.d.id)}
          {@const gs = p.d.ghost ? ghostStateOf(ghostWatchFor(p.d.id), nowMs) : null}
          <g transform="translate({p.x} {p.y})">
            <!-- The plaque says name · subnet, and nothing else (round
                 49, #1016): LOGGED, DARK and NO RULES PUSHED are gone
                 from it because the wall now says what the coverage is,
                 and a plaque that ignored declarations said the wrong
                 thing anyway (#1014). `no rule table pushed` stays, dim,
                 because it is a different fact from dark: there, a table
                 exists and nothing on it logs. -->
            <!-- A ghost's plaque says what it is: compact, the name
                 carries `· ghost`; full, the note line under it is the
                 same sentence the flat map's lane tally says, in the
                 ghost's own ink. Its dot is a ring rather than a disc,
                 the same hollowing the lane dot takes. -->
            {#if compact}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height="20" rx="10" fill="#0a0f1c" fill-opacity="0.9" stroke="var(--border)" />
              {#if gs}
                <circle cx={R2(-p.w / 2 + 11)} cy="10" r="3.2" fill="none" stroke={GHOST_INK[gs]} stroke-width="1.2" />
                <text x={R2(-p.w / 2 + 19)} y="14" class="p-name small gname">{p.d.name} · ghost</text>
              {:else}
                <circle cx={R2(-p.w / 2 + 11)} cy="10" r="3.2" fill={p.ink} />
                <text x={R2(-p.w / 2 + 19)} y="14" class="p-name small">{p.d.name}</text>
              {/if}
            {:else}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height={gs || !p.d.rulesPushed ? 40 : 28} rx="8" fill="#0a0f1c" fill-opacity="0.93" stroke="var(--border)" />
              {#if gs}
                <circle cx={R2(-p.w / 2 + 13)} cy="14" r="3.4" fill="none" stroke={GHOST_INK[gs]} stroke-width="1.2" />
              {:else}
                <circle cx={R2(-p.w / 2 + 13)} cy="14" r="3.4" fill={p.ink} />
              {/if}
              <text x={R2(-p.w / 2 + 22)} y="18" class="p-name" class:gname={gs}>{p.d.name}</text>
              <text x={R2(p.w / 2 - 11)} y="17.5" text-anchor="end" class="p-cidr">{p.d.cidr ?? 'no address pushed'}</text>
              {#if gs}
                <text x={R2(-p.w / 2 + 13)} y="32" class="p-note" style:fill={GHOST_INK[gs]}
                  >{ghostNote(gs, ghostWatchFor(p.d.id), ghostOfferFor(p.d.id), nowMs)}</text
                >
              {:else if !p.d.rulesPushed}
                <text x={R2(-p.w / 2 + 13)} y="32" class="p-note">no rule table pushed</text>
              {/if}
            {/if}
          </g>
        {/each}
        {#each scene.portTallies as ch (ch.id)}
          <g transform="translate({ch.x} {ch.y})">
            <rect x={R2(-ch.w / 2)} y="-9" width={ch.w} height="18" rx="9" fill="#080c16" fill-opacity="0.9" stroke="var(--hair)" />
            <text x="0" y="3.5" text-anchor="middle" class="chip-t">{ch.t}</text>
          </g>
        {/each}
        {#each toppers as t (t.b.id)}
          <g transform="translate({t.x} {t.y})">
            <text x="0" y="-13" text-anchor="middle" class="st-name" class:st-dim={t.quiet}>{t.b.name}</text>
            <text x="0" y="0" text-anchor="middle" class="st-ip">{t.sub}</text>
          </g>
        {/each}
        <!-- The straggler (#460, round 55): a road from the ghost's gate
             to the gate of the district its peer stands in, in the
             reserved alarm ink, with the callout naming the host the
             address last belonged to. Drawn only where a pushed address
             table claims the peer; where it does not, the callout still
             says what happened and no road claims a destination the map
             cannot prove. -->
        {#if stragglerRoad}
          {@const sr = stragglerRoad}
          {#if sr.from && sr.to}
            <path
              class="straggler-road"
              d="M{R2(X(geomCam, sr.from[0]))} {R2(Y(geomCam, sr.from[1]))}L{R2(X(geomCam, sr.to[0]))} {R2(Y(geomCam, sr.to[1]))}"
            />
          {/if}
          {@const call = stragglerCallout(sr.w, sr.st, sr.st.peer ?? '')}
          {@const cx = R2(X(geomCam, sr.from ? (sr.from[0] + (sr.to ? sr.to[0] : sr.from[0])) / 2 : sr.g.u))}
          {@const cy = R2(Y(geomCam, sr.from ? (sr.from[1] + (sr.to ? sr.to[1] : sr.from[1])) / 2 : sr.g.v) + 34)}
          <g
            class="straggler-call"
            role="button"
            tabindex="0"
            aria-label="{call.head} — open this straggler"
            onclick={() => (openGhostId = openGhostId === sr.g.id ? null : sr.g.id)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                openGhostId = openGhostId === sr.g.id ? null : sr.g.id
              }
            }}
          >
            <rect x={cx - 167} y={cy} width="334" height="46" rx="10" fill="#170a12" stroke="var(--alarm)" stroke-opacity="0.85" />
            <circle cx={cx - 152} cy={cy + 17} r="3.4" fill="var(--alarm)" />
            <text x={cx - 141} y={cy + 20} class="alarm-t">{call.head}</text>
            <text x={cx - 141} y={cy + 35} class="chip-t">{call.detail} · open ▸</text>
          </g>
        {/if}
        {#each scene.rings as r (r.label)}
          <text x={r.x} y={r.y} text-anchor="middle" class="boro-t">{r.label}</text>
        {/each}
        {#each scene.bridgeChips as ch (ch.t)}
          <g transform="translate({ch.x} {ch.y})">
            <rect x={R2(-ch.w / 2)} y="-9" width={ch.w} height="18" rx="9" fill="#080c16" fill-opacity="0.9" stroke={ch.stroke} />
            <text x="0" y="3.5" text-anchor="middle" class="chip-t">{ch.t}</text>
          </g>
        {/each}
        {#each scene.dropLabels as dl, i (i)}
          <text x={dl.x} y={dl.y} text-anchor="middle" class="drop-t" class:alarm-t={dl.alarm}>{dl.text}</text>
        {/each}
        {#each scene.doorLabels as dl (dl.key)}
          <!-- `#12 accept`: the rule's number and what it does, the flat
               map's own two tokens. Above the door rather than beside
               it, because a gate has a wall on both sides. -->
          <text x={dl.x} y={dl.y} text-anchor="middle" class="door-t" class:shut={!dl.accepts}
            >{dl.label} <tspan class="door-act">{dl.act}</tspan></text
          >
        {/each}
        <!-- The event trace (#1050, rounds 54 & 56): the ghost first, so
             the mark and the chip sit over its dashes rather than under
             them. Never drawn together with a door or a drop label --
             the trace clears the port filter, and standing, before it
             opens. Positioned on the geometry camera like the rest of
             this group (`traceDrawing`'s own comment has why); the
             stop mark, the ghost's own note and the chip each wrap
             their content in `scale({traceK})`, cancelling this
             group's ancestor scale so they render at this file's local
             pixel size regardless of the camera's zoom -- #1050 round
             56 defect 3, and the same nested translate+scale shape a
             building's own stamp uses further up for the same reason.
             The dashed ghost path and the halo/ring below stay plain:
             a line and a building-tracking ring are meant to scale with
             the map the way a road does. -->
        {#if traceDrawing.ghost}
          <path class="trace-ghost" d={traceDrawing.ghost.d} />
          <g transform="translate({traceDrawing.ghost.tx} {traceDrawing.ghost.ty}) scale({R2(traceK)})">
            <text text-anchor="middle" class="ghost-t">{traceDrawing.ghost.text}</text>
          </g>
        {/if}
        {#if traceDrawing.mark}
          <!-- Where the rule stopped it: the wall's own gate when the log
               names an out-interface, the router's own door when it does
               not (C1, owner 2026-09-09). -->
          <g class="trace-stop" transform="translate({R2(traceDrawing.mark.x)} {R2(traceDrawing.mark.y)}) scale({R2(traceK)})">
            <circle r="9" fill="none" stroke="var(--alarm)" stroke-opacity="0.5" />
            <path d="M-5 -5L5 5M-5 5L5 -5" stroke="var(--alarm)" stroke-width="2.2" stroke-linecap="round" />
          </g>
        {/if}
        {#if traceDrawing.halo}
          <!-- The sender's own halo -- alarm red for a refusal, accept
               green for a crossing, the same pulsing ring every other
               off-baseline arrival on this map wears. -->
          <circle
            class="halo"
            cx={R2(traceDrawing.halo.x)}
            cy={R2(traceDrawing.halo.y)}
            r={R2(traceDrawing.halo.r)}
            fill="none"
            stroke={traceDrawing.halo.alarm ? 'var(--alarm)' : 'var(--accept)'}
            stroke-width="1.4"
          />
        {/if}
        {#if traceDrawing.ring}
          <circle class="halo" cx={R2(traceDrawing.ring.x)} cy={R2(traceDrawing.ring.y)} r={R2(traceDrawing.ring.r)} fill="none" stroke="var(--accept)" stroke-width="1.2" />
        {/if}
        {#if traceDrawing.leader}
          <path class="trace-leader" d={traceDrawing.leader} />
        {/if}
        {#if traceDrawing.chip}
          <!-- The router's own decision, beside wherever it was made
             (#1050, rounds 54 & 56's `verdictChip`). -->
          <g class="trace-chip" transform="translate({R2(traceDrawing.chip.x)} {R2(traceDrawing.chip.y)}) scale({R2(traceK)})">
            <rect
              x="0"
              y="-16"
              width={R2(traceDrawing.chip.w)}
              height="40"
              rx="9"
              fill={traceDrawing.chip.refused ? '#170a12' : '#0a1712'}
              stroke={traceDrawing.chip.refused ? 'var(--alarm)' : 'var(--accept)'}
              stroke-opacity="0.8"
            />
            <text x="12" y="0" class="chip-verdict" class:refused={traceDrawing.chip.refused}>{traceDrawing.chip.l1}</text>
            <text x="12" y="15" class="chip-t">{traceDrawing.chip.l2}</text>
          </g>
        {/if}
        {#each scene.dropHits as dh (dh.id)}
          <!-- The aggregate mark as the control (#1002): one target over
               the mark and the word above it, in the keyboard order the
               way a district plate and a building already are. Nothing
               is added to the drawing -- the rules it opens are the
               card's, and the mark still reads only "dropped". -->
          <rect
            class="road-hot"
            class:on={openDropId === dh.id}
            x={dh.x}
            y={dh.y}
            width={dh.w}
            height={dh.h}
            fill="transparent"
            role="button"
            tabindex="0"
            aria-expanded={openDropId === dh.id}
            aria-label="{dropAria(dh.id)}. Opens which rules, and how many each caught."
            data-drop-hot={dh.id}
            onpointerenter={() => openDropCard(dh.id)}
            onpointerleave={releaseDropCard}
            onfocus={() => openDropCard(dh.id)}
            onblur={releaseDropCard}
            onclick={() => !dragged && toggleDropPin(dh.id)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                toggleDropPin(dh.id)
              }
            }}
          >
            <title>{dropAria(dh.id)}</title>
          </rect>
        {/each}
        {#each scene.markHits as mh (mh.id)}
          <!-- The mark where a refused line stopped, pointable so that
               line has a card even when its boundary draws no road. -->
          <circle
            class="road-hot"
            class:on={openRoadId === mh.id}
            cx={mh.x}
            cy={mh.y}
            r="14"
            fill="transparent"
            role="button"
            tabindex="-1"
            aria-label={roadAria(mh.id)}
            data-road-hot={mh.id}
            onpointerenter={() => openRoadCard(mh.id)}
            onpointerleave={releaseRoadCard}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                toggleRoadPin(mh.id)
              }
            }}
          />
        {/each}
      </g>
    </g>
    <!-- Nothing seen: the same one line the flat map writes, under the
         city rather than under the map, and outside the panned group --
         it is about the answer, not about a place, so it does not move
         when the camera does. The doors stay drawn behind it. -->
    {#if portOverlay?.note}
      <text x={STAGE_W / 2} y={STAGE_H - 18} text-anchor="middle" class="note-t">{portOverlay.note}</text>
    {/if}
  </svg>

  {#if standBuilding || standDistrict || standRoad}
    <!-- The crumb (#868, round 49 #1016, DESIGN.md "The reach"): name ·
         ip · reaches N · reached by N · refused N · Esc surfaces ▸ --
         the one wording on both surfaces, and now for all three
         subjects. `refused N` and the ▸ are round 49's; the rest is the
         2D map's own "reaches <b>N</b> · reached by <b>N</b>" wording
         (Topography.svelte's reach crumb), the round-40 mockup's layout
         and its own literal "Esc surfaces" for the rest. It is itself
         the other way to surface (#868's own "Esc or the crumb"),
         restoring the exact camera standing started from, same as
         Escape.

         The three counts are counts of distinct counterparts, and
         `reachFor` now answers them for a zone and a rib as well as for
         a host, so they say one thing on every subject. A lane or a
         bridge leg is neither, so it has no summary: its crumb names it
         and says how to surface, rather than stating a count derived
         some other way. The second cell is the host's address or the
         district's pushed subnet -- a road has neither, and is not
         given an invented one. -->
    <button type="button" class="crumb" aria-label="Standing on {standName}. Activate to surface." onclick={standSurface}>
      <b>{standName}</b>
      {#if standSub}<span>{standSub}</span>{/if}
      <i></i>
      {#if standReach}
        <span>reaches <b>{standReach.reaches}</b></span>
        <span>reached by <b>{standReach.reachedBy}</b></span>
        <span>refused <b>{standRefused}</b></span>
        <i></i>
      {/if}
      <span class="esc">Esc surfaces ▸</span>
    </button>
  {/if}

  {#if composerOpen && standBuilding && standReach?.topBlocked}
    {@const s = standReach.topBlocked}
    {@const peerName = s.peers[0] ?? (s.counterpart === 'internet' ? 'the internet' : s.counterpart)}
    {@const top = s.portHits[0]}
    <!-- The composer (#868, DESIGN.md "The reach"): a card pinned to
         the wall where the busiest blocked strand's road stopped.
         Drafted, never run -- the same invariant, and the same
         reachComposeInput/composeCommand, as the 2D composer
         (Topography.svelte); a vitest proves the printed line is
         byte-identical for the same strand. -->
    <div class="composer" role="note" aria-label="The rule that would let this through, drafted, never run">
      <div class="cm-h">
        <span class="dot"></span>
        {s.direction === 'out' ? `${standBuilding.name} → ${peerName}` : `${peerName} → ${standBuilding.name}`} · refused at this wall
      </div>
      <div class="cm-b">
        it's been asking{top ? ` · ${top.proto}/${top.port}` : ''} · {s.count}×{s.refusedBy ? ` · caught by ${s.refusedBy}` : ' · caught, no rule named'}
      </div>
      {#if standComposeCommand}
        <pre class="cm-code">{standComposeCommand}</pre>
      {:else}
        <p class="cm-b">nothing to draft from yet -- no destination port observed on this strand.</p>
      {/if}
      <div class="cm-f"><span>drafted · never run</span></div>
    </div>
  {/if}

  {#if wallCard}
    {@const c = wallCard}
    <!-- The boundary card (DESIGN.md "Cards", round 49 #1016). Dark: what
         the rule does, both directions, and the three actions; pinned, it
         opens the declare form. Quiet: the reason quoted, who and when,
         and undeclare. The wording is the 2D map's, so a boundary reads
         the same on either side of the slider. -->
    <!-- The decommission card (#460, round 55): the same component the
         flat map draws, beside the ghost, with its own heavier leader. -->
    {#if cityDecommCard}
      {@const gd = cityDecommCard.district}
      <DecommissionCard
        bind:element={gcardEl}
        kind={cityDecommCard.kind}
        offer={ghostOfferFor(gd.id)}
        watch={ghostWatchFor(gd.id)}
        peerName={ghostWatchFor(gd.id)?.lastStraggler?.peer ?? ''}
        {nowMs}
        place={ghostPlace}
        busy={ghostBusy}
        error={ghostError}
        onaccept={() => answerGhostOffer(gd, true)}
        ondismiss={() => answerGhostOffer(gd, false)}
        onforce={(why) => forceRemoveGhostDistrict(gd, why)}
        onclose={() => (openGhostId = null)}
        ontrace={() => {
          const st = ghostWatchFor(gd.id)?.lastStraggler
          if (st) topologyNavState.pendingTrace = { in: st.interface || undefined, port: st.port, proto: st.protocol }
          appState.view = 'topography'
        }}
        onwatchhost={() => {
          const st = ghostWatchFor(gd.id)?.lastStraggler
          if (!st) return
          topologyNavState.requestWatchDraft({
            who: st.address,
            toward: st.peer ? `${st.peer}${st.port ? `:${st.port}` : ''}` : undefined,
            mode: 'expect',
            provenance: `from a straggler on the retired range ${gd.cidr ?? gd.id}`,
          })
          appState.view = 'watchlist'
        }}
        onstream={() => {
          appState.resetFilters()
          if (gd.cidr) appState.setFilter('srcQuery', gd.cidr)
          appState.view = 'live'
        }}
        onwatchlist={() => {
          // #1069: land on the row this ghost's watch draws in the
          // watchlist, not just the tab -- the card's own copy already
          // promises that's where the watch lives on.
          const id = ghostWatchFor(gd.id)?.id
          if (id) topologyNavState.requestDecommissionWatch(id)
          appState.view = 'watchlist'
        }}
      />
    {/if}

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
      class="bcard"
      class:pinned={wallPinned}
      class:placed={cardPlace !== null}
      style={cardPlace ? `left:${R2(cardPlace.left)}px;top:${R2(cardPlace.top)}px` : undefined}
      bind:this={bcardEl}
      role="dialog"
      tabindex="-1"
      aria-label="{c.d.name} to {c.gate.toward}: this boundary, {COVERAGE_WORD[c.gate.coverage]}"
      onpointerenter={cardGrace.hold}
      onpointerleave={releaseWallCard}
    >
      <div class="bc-t">
        <span class="n">{c.d.name} → {c.gate.toward}<small>boundary</small></span>
        <button
          type="button"
          class="pin"
          class:on={wallPinned}
          aria-pressed={wallPinned}
          title={wallPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={toggleWallPin}>{wallPinned ? '✕' : '⊙'}</button
        >
      </div>

      {#if c.gate.ruleOrdinal >= 0}
        <!-- Which rule this opening in the wall is (owner, 2026-09-08 on
             #1016), numbered as RouterOS numbers it so "go look at rule
             4" means what it says. The name is that rule's own comment;
             a rule with none is shown by its number alone rather than
             given an invented name. It is said here and nowhere else --
             nothing is written on the drawing. -->
        <div class="s" data-gate-rule>rule {c.gate.ruleOrdinal}{c.gate.ruleName ? ` · ${c.gate.ruleName}` : ''}</div>
      {/if}

      {#each c.gate.directions as dir (dir.edgeKey)}
        <div class="s {dir.coverage}">
          <i class="sw {dir.coverage}"></i>{dir.label} · {COVERAGE_WORD[dir.coverage]} — {directionDetail(dir)}
        </div>
      {/each}

      {#if c.gate.coverage === 'dark'}
        <div class="s">nothing drawn across it is a fact; nothing is known</div>
      {/if}

      {#if c.declaration}
        <blockquote class="quote">{c.declaration.reason}</blockquote>
        <div class="s">{c.declaration.declaredBy} · {new Date(c.declaration.declaredAt).toLocaleString()}</div>
      {/if}

      {#if wallPinned && authState.isAdmin && !c.declaration}
        <!-- The declare form: a reason, both directions, and who. Both
             directions is checked by default (round 49 item 7) because
             one direction declared and the other still dark leaves the
             boundary grey and this card explaining why.
             Written out in the 2D map's own order and markup, tag for
             tag (#1031) -- DESIGN.md "Cards" ratifies one interaction,
             the same on both surfaces, and it is the footer's shape a
             reader notices when the slider crosses. -->
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
            <button type="button" class="no" onclick={() => (pinnedWall = null)}>cancel</button>
            <span class="who">as {authState.username}</span>
          </div>
        </div>
      {/if}

      <div class="acts">
        {#if c.declaration}
          {#if authState.isAdmin}
            <button type="button" class="hot" disabled={declareBusy} onclick={removeDeclaration}>undeclare ▸</button>
          {/if}
        {:else if authState.isAdmin && !wallPinned && c.gate.coverage !== 'logged'}
          <button type="button" onclick={toggleWallPin}>declare quiet on purpose ▸</button>
        {/if}
        <button type="button" onclick={openRulesForBoundary}>rules ▸</button>
        <button type="button" class="dim" onclick={() => (appState.view = 'live')}>stream ▸</button>
      </div>
    </div>
  {/if}

  {#if hostCard}
    {@const c = hostCard}
    <!-- The host card (DESIGN.md "Cards", "Living hosts"): presence,
         last and first seen, events, and the two marks. The wording is
         the 2D map's, word for word. -->
    {#if hostPlace}
      <svg class="leader" aria-hidden="true">
        <path d="M{hostPlace.from.x} {hostPlace.from.y}L{hostPlace.to.x} {hostPlace.to.y}" stroke="var(--hair-2)" stroke-width="1" fill="none" />
        <circle cx={hostPlace.from.x} cy={hostPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="bcard hcard"
      class:pinned={hostPinned}
      class:placed={hostPlace !== null}
      style={hostPlace ? `left:${R2(hostPlace.left)}px;top:${R2(hostPlace.top)}px` : undefined}
      bind:this={hcardEl}
      role="dialog"
      tabindex="-1"
      aria-label="{c.b.name}: {hostWord(c.h)}"
      onpointerenter={hostGrace.hold}
      onpointerleave={releaseHostCard}
    >
      <div class="bc-t">
        <!-- #1165: an unnamed host's name is its address, and the card
             printed it twice side by side. The address is a second fact
             only where there is a name in front of it. -->
        <span class="n">{c.b.name}{#if c.b.ip && c.b.ip !== c.b.name}<small>{c.b.ip}</small>{/if}</span>
        <button
          type="button"
          class="pin"
          class:on={hostPinned}
          aria-pressed={hostPinned}
          title={hostPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={toggleHostPin}>{hostPinned ? '✕' : '⊙'}</button
        >
      </div>

      {#if c.h.presence === 'quiet'}
        <div class="s dark">
          <i class="sw dark"></i>quiet · not heard for <b>{quietFor(c.h.lastSeen, nowTick)}</b> (window {quietWindow})
        </div>
      {:else if c.h.presence === 'intended'}
        <div class="s quiet"><i class="sw quiet"></i>quiet on purpose</div>
      {:else if c.h.events === 0 && !c.h.lastSeen}
        <div class="s quiet"><i class="sw quiet"></i>{hostWord(c.h)}</div>
      {:else}
        <div class="s logged"><i class="sw logged"></i>live</div>
      {/if}

      <div class="s">
        last seen {stamp(c.h.lastSeen)} · first seen {stamp(c.h.firstSeen)} · {c.h.events.toLocaleString()}
        {c.h.events === 1 ? 'event' : 'events'} · {c.d.name}
      </div>

      {#if c.h.flags > 0 || c.h.watch > 0}
        <!-- How many, as plain words (#981, round 46): "2 flags · 1
             watch", the flag words in the alarm ink and the watch words
             in the watcher's. Counts exist here and nowhere else -- the
             map carries no number, no disc and no glyph. -->
        <div class="s counts" data-marks>
          {#if c.h.flags > 0}<b class="fw">{c.h.flags} {c.h.flags === 1 ? 'flag' : 'flags'}</b>{/if}{#if c.h.flags > 0 && c.h.watch > 0}<span class="sep">&nbsp;·&nbsp;</span>{/if}{#if c.h.watch > 0}<b class="ww">{c.h.watch} watch</b>{/if}
        </div>
      {/if}

      {#if c.h.presence !== 'live'}
        <div class="s">comes back by itself when the feed hears it again</div>
      {/if}

      {#if c.h.reason}
        <blockquote class="quote">{c.h.reason}</blockquote>
        <div class="s">{c.h.markedBy ?? 'unknown'}{c.h.markedAt ? ' · ' + stamp(c.h.markedAt) : ''}</div>
      {/if}

      {#if !c.h.key}
        <!-- Nothing to write to: the register has not recorded this host
             yet, so the marks would have no key to hang on. Said plainly
             rather than offering a button that cannot work. -->
        <div class="s">not in the host register yet · nothing to mark</div>
      {/if}

      {#if hostPinned && authState.canEdit && c.h.key && !c.h.reason}
        <div class="form">
          <label for="city-host-reason">QUIET ON PURPOSE — WHY?</label>
          <input id="city-host-reason" bind:value={markReason} placeholder="why this machine is expected to be silent…" />
          <div class="btns">
            <button type="button" class="go" disabled={!markReason.trim() || markBusy} onclick={submitHostMark}>Mark</button>
            <button type="button" class="no" onclick={() => (pinnedHost = null)}>cancel</button>
            <span class="who">as {authState.username || 'you'}</span>
          </div>
        </div>
      {/if}
      {#if hostsState.error}<div class="s alarm">{hostsState.error}</div>{/if}

      <div class="acts">
        {#if standBuilding?.id === c.b.id && standReach?.topBlocked}
          <!-- The composer's door (#1035). It used to hang on the line
               card alone, which needs a road or a mark to hover; a
               strand across a district pair that already ends in a drop
               draws neither of its own, so on those hosts -- the ones
               with most to draft -- nothing on screen opened the
               composer at all. The host card is open the moment you are
               standing on the building, so the door is here, on the
               subject the composer is about. Only that host's card: the
               composer drafts `standReach.topBlocked`, and offering it
               on another building's card would draft a rule for a
               building the reader is not looking at. -->
          <button type="button" data-draft-rule onclick={() => (composerOpen = true)}>draft the rule ▸</button>
        {/if}
        {#if authState.canEdit && c.h.key}
          {#if c.h.reason}
            <button type="button" class="hot" disabled={markBusy} onclick={unmarkHost}>unmark ▸</button>
          {:else if !hostPinned}
            <button type="button" onclick={openMarkForm}>mark quiet on purpose ▸</button>
          {/if}
          <button type="button" class="hot" disabled={markBusy} onclick={dismissHost}>dismiss ▸</button>
        {/if}
        <!-- #410: the host click reaches the dossier. One hook, no state
             of its own -- lib/dossier.svelte.ts owns the card. -->
        <button type="button" class="dim" onclick={(e) => dossierState.open(c.b.ip, e.currentTarget)}>dossier ▸</button>
        <button type="button" class="dim" onclick={() => (appState.view = 'live')}>stream ▸</button>
      </div>
    </div>
  {/if}

  <!-- The reason form, written once and rendered wherever it was opened:
       under the line whose own `expected ▸` opened it, or under the bulk
       control. Same words, same order, same wording as the 2D rib card,
       which renders the identical snippet from lib/city/expected. The
       reason is the statement -- the server refuses an empty one, and a
       line quietly leaving the sieve with nothing said for it is the
       failure this whole card exists to avoid. -->
  {#snippet expectedForm()}
    {@const scope = expectedScope}
    {#if scope}
      <div class="form">
        <label for="city-expected-reason">{EXPECTED_LABEL}</label>
        <input id="city-expected-reason" bind:value={expectedReason} placeholder={expectedPlaceholder(scope, expectedLines.length)} />
        <div class="btns">
          <button type="button" class="go" disabled={!expectedReason.trim() || expectedBusy} onclick={submitExpected}>Expected</button>
          <button type="button" class="no" onclick={() => (expectedScope = null)}>cancel</button>
          <span class="who">{expectedWho(authState.username, scope, expectedLines.length)}</span>
        </div>
        {#if baselineState.error}<div class="s alarm">{baselineState.error}</div>{/if}
      </div>
    {/if}
  {/snippet}

  {#if roadCard}
    {@const rc = roadCard}
    {@const cfg = baselineState.off.config}
    <!-- The off-baseline roll-up card (round-49/index.html "flat-new",
         DESIGN.md "Saying it is expected"). What this road carries that
         is off today's pattern, line by line, and the one way a line
         leaves the bright state early. The wording is the 2D map's, so a
         line reads the same on either side of the slider. -->
    {#if roadPlace}
      <svg class="leader" aria-hidden="true">
        <path d="M{roadPlace.from.x} {roadPlace.from.y}L{roadPlace.to.x} {roadPlace.to.y}" stroke="var(--hair-2)" stroke-width="1" fill="none" />
        <circle cx={roadPlace.from.x} cy={roadPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="bcard rcard"
      class:pinned={roadPinned}
      class:placed={roadPlace !== null}
      style={roadPlace ? `left:${R2(roadPlace.left)}px;top:${R2(roadPlace.top)}px` : undefined}
      bind:this={rcardEl}
      role="dialog"
      tabindex="-1"
      aria-label={roadAria(rc.road.id)}
      onpointerenter={roadGrace.hold}
      onpointerleave={releaseRoadCard}
    >
      <div class="bc-t">
        <span class="n">{endName(ground, rc.entry.ends.start)} → {endName(ground, rc.entry.ends.end)}<small>road</small></span>
        <button
          type="button"
          class="pin"
          class:on={roadPinned}
          aria-pressed={roadPinned}
          title={roadPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={() => toggleRoadPin(rc.road.id)}>{roadPinned ? '✕' : '⊙'}</button
        >
      </div>

      <!-- How many lines are established here is deliberately not a
           number. The register carries only today's off-baseline lines,
           never the established ones -- on a busy network the
           established set is the entire traffic set -- so absence from
           it *is* establishment, and a count would be invented rather
           than known (#865: said plainly, never guessed). What is known
           is the threshold the server actually applied, so the card says
           that instead. -->
      <div class="s est"><i class="sw est"></i>established · everything else on this road, seen on {cfg.days} of the last {cfg.of} days</div>
      <div class="s"><i class="sw nb"></i><b>{offCount(rc.entry.lines.length)}</b></div>

      <table class="off">
        <thead>
          <tr><th>LINE</th><th>PORT</th><th class="n">SEEN</th><th class="n">FIRST</th></tr>
        </thead>
        <tbody>
          {#each rc.entry.lines as l (l.key)}
            <tr>
              <td>{addressName(ground, l.srcIp)} → {addressName(ground, l.dstIp)}</td>
              <td>{portWords(l)}</td>
              <td class="n ok">{l.count}</td>
              <td class="n">today {formatHM(new Date(l.firstSeenToday).toISOString())}</td>
            </tr>
            <tr class="why"><td colspan="4">{verdictWords(l.outcome)}</td></tr>
            {#if expectedScope?.kind === 'one' && expectedScope.key === l.key}
              <tr class="formrow">
                <td colspan="4">{@render expectedForm()}</td>
              </tr>
            {:else if authState.isAdmin}
              <!-- Per-line is the default: this button marks the line it
                   sits under and nothing else, so the ones above and
                   below it stay bright and the road stays bright until
                   none of them is left (#1016, owner 2026-09-07). -->
              <tr class="actrow">
                <td colspan="4"
                  ><button type="button" class="linkact" data-expected-one={l.key} onclick={() => startExpectedOne(l, rc.road.id)}
                    >{EXPECTED_ONE_LABEL}</button
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
           in the row already does. -->
      {#if authState.isAdmin && offersExpectedAll(rc.entry.lines.length)}
        {#if expectedScope?.kind === 'all'}
          <div class="allof">{@render expectedForm()}</div>
        {:else}
          <div class="allof">
            <button type="button" class="linkact all" onclick={() => startExpectedAll(rc.road.id)}>{expectedAllLabel(rc.entry.lines.length)}</button>
          </div>
        {/if}
      {/if}
    </div>
  {/if}

  {#if lineCard}
    {@const lc = lineCard}
    {@const l = lc.line}
    <!-- The line card in the reach (round-49/index.html's `nasLine`,
         DESIGN.md "Cards"). Its markup and wording are the mockup's:
         the port / proto / accepted / dropped table, the totals line,
         `:22 refused by #17 default drop`, and on a refused line the
         composer's `draft the rule ▸`. Every number in it comes from
         lib/reach's `reachLineSummary`, which the 2D map's own line card
         also reads -- one function, so the two surfaces cannot drift
         into two readings of the same traffic. Nothing here is written
         on the road: this card is where the ports live. -->
    {#if roadPlace}
      <svg class="leader" aria-hidden="true">
        <path d="M{roadPlace.from.x} {roadPlace.from.y}L{roadPlace.to.x} {roadPlace.to.y}" stroke="var(--hair-2)" stroke-width="1" fill="none" />
        <circle cx={roadPlace.from.x} cy={roadPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="bcard rcard lcard"
      class:pinned={roadPinned}
      class:placed={roadPlace !== null}
      style={roadPlace ? `left:${R2(roadPlace.left)}px;top:${R2(roadPlace.top)}px` : undefined}
      bind:this={rcardEl}
      role="dialog"
      tabindex="-1"
      aria-label={roadAria(lc.id)}
      onpointerenter={roadGrace.hold}
      onpointerleave={releaseRoadCard}
    >
      <div class="bc-t">
        <span class="n"
          >{lineTitle(lc.hostName, lc.peerName, lc.lead?.direction ?? null)}{#if lc.peerAddr}<small>{lc.peerAddr}</small>{/if}</span
        >
        <button
          type="button"
          class="pin"
          class:on={roadPinned}
          aria-pressed={roadPinned}
          title={roadPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={() => toggleRoadPin(lc.id)}>{roadPinned ? '✕' : '⊙'}</button
        >
      </div>

      {#if l.ports.length > 0}
        <table class="ports">
          <thead>
            <tr><th>PORT</th><th>PROTO</th><th class="n">ACCEPTED</th><th class="n">DROPPED</th></tr>
          </thead>
          <tbody>
            {#each l.ports as p (`${p.port}|${p.proto}`)}
              <tr>
                <td>{p.port}</td>
                <!-- '' is "the events named no protocol", which is not
                     the same as tcp: an em dash rather than a guess. -->
                <td class:dim={!p.proto}>{p.proto || '—'}</td>
                <td class="n" class:ok={p.accepted > 0} class:dim={p.accepted === 0}>{p.accepted > 0 ? p.accepted : '—'}</td>
                <td class="n" class:al={p.dropped > 0} class:dim={p.dropped === 0}>{p.dropped > 0 ? p.dropped : '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <!-- Portless traffic only (ICMP and friends) still has a
             protocol split to show, and saying so beats an empty table. -->
        <div class="s">no destination port was named on this line</div>
      {/if}

      <!-- The tcp-versus-udp picture. `other` carries everything that is
           neither, portless traffic included, so the three total the
           line's own events rather than quietly dropping any. -->
      <div class="s totals">tcp <b>{l.tcp}</b> · udp <b>{l.udp}</b> · other {l.other}</div>

      {#if lc.refusedPort}
        <!-- The mockup's own line: `:22 refused by #17 default drop`.
             The rule is the events' own label; where no refusal on this
             line carried one it is said plainly rather than guessed
             (#865/#967), which is also what `refusedBy` being absent
             means. -->
        <div class="s alarm">:{lc.refusedPort.port} {l.refusedBy ? `refused by ${l.refusedBy}` : 'refused, no rule named'}</div>
      {/if}

      {#if lc.refusedPort}
        <div class="acts">
          <!-- The composer, on the card that names the refusal (round
               49). Drafted, never run -- the same invariant as the 2D
               composer, and the same printed line. -->
          <button type="button" class="linkact" data-draft-rule onclick={() => (composerOpen = true)}>draft the rule ▸</button>
        </div>
      {/if}
    </div>
  {/if}

  {#if dropCard}
    {@const dc = dropCard}
    <!-- The drop card (#1002, DESIGN.md "Cards"). Every rule that
         refused on this boundary and how much each caught, largest
         first, under the composer's own header and over the total the
         mark stands for. Same markup and same classes as the boundary
         card: it is one family, not a second card style. -->
    {#if dropPlace}
      <svg class="leader" aria-hidden="true">
        <path d="M{dropPlace.from.x} {dropPlace.from.y}L{dropPlace.to.x} {dropPlace.to.y}" stroke="var(--hair-2)" stroke-width="1" fill="none" />
        <circle cx={dropPlace.from.x} cy={dropPlace.from.y} r="3" fill="var(--accent)" />
      </svg>
    {/if}
    <div
      class="bcard dcard"
      class:pinned={dropPinned}
      class:placed={dropPlace !== null}
      style={dropPlace ? `left:${R2(dropPlace.left)}px;top:${R2(dropPlace.top)}px` : undefined}
      bind:this={dcardEl}
      role="dialog"
      tabindex="-1"
      aria-label={dropTitle}
      onpointerenter={dropGrace.hold}
      onpointerleave={releaseDropCard}
    >
      <div class="bc-t">
        <span class="n">{dropTitle}</span>
        <button
          type="button"
          class="pin"
          class:on={dropPinned}
          aria-pressed={dropPinned}
          title={dropPinned ? 'pinned — click to let it go' : 'pin this card'}
          onclick={() => toggleDropPin(dc.id)}>{dropPinned ? '✕' : '⊙'}</button
        >
      </div>

      <!-- No swatch: the ones this card's family uses say coverage and
           baseline, and a rule is neither. -->
      {#each dc.rows as r (r.rule ?? '')}
        <div class="s" data-drop-rule>{dropRow(r)}</div>
      {/each}

      <!-- The total, so the card reconciles with the mark it came from:
           these rules are all of them, and their counts are all of it.
           It wears the mark's own ink, so the one escalated pair reads
           on the card as it reads on the map. -->
      <div class="s totals" class:alarm={dc.alarm} data-drop-total>dropped <b>{dc.total}</b> in all</div>
    </div>
  {/if}

  <div class="mini" aria-label="Minimap: the viewport is one part of a much larger map">
    <h4>ESTATE MAP</h4>
    <button type="button" class="look" aria-label="Look there: click a place on the estate map to centre on it" onclick={onMinimapClick}>
      <svg width={MINI_W} height={MINI_H} viewBox="0 0 {MINI_W} {MINI_H}" aria-hidden="true">
      <rect x="0" y="0" width={MINI_W} height={MINI_H} rx="4" fill="#070b14" />
      {#if mini.river}<path d={mini.river} fill="#0b1830" />{/if}
      {#each mini.plates as p, i (i)}
        <path d={p.d} fill={p.ink} fill-opacity={p.fo} />
      {/each}
      {#each mini.plates as p, i (i)}
        <text x={p.x} y={p.y} text-anchor="middle" class="mini-name">{p.name}</text>
      {/each}
      {#each mini.nodes as n, i (i)}
        <circle cx={n.x} cy={n.y} r="2" fill="var(--accent)" />
      {/each}
      <rect class="viewport" x={miniView.x} y={miniView.y} width={miniView.w} height={miniView.h} fill="rgba(157,184,232,0.1)" stroke="var(--accent)" stroke-opacity="0.8" stroke-width="1.2" />
      </svg>
    </button>
    <div class="mk"><span>viewport ≈ {viewShare}%</span><span>drag · arrows to walk</span></div>
  </div>
</div>

<style>
  .city {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .city > svg {
    width: 100%;
    height: 100%;
    display: block;
    cursor: grab;
    touch-action: none;
    user-select: none;
  }

  .city > svg:active {
    cursor: grabbing;
  }

  .city > svg:focus-visible {
    outline: none;
  }

  .plate,
  .blk {
    outline: none;
  }

  .plate .ring {
    display: none;
  }

  .plate:focus-visible .ring,
  .plate.focused .ring {
    display: block;
  }

  .blk:focus-visible {
    filter: brightness(1.25);
  }

  /* #1026: `.flat` (template above) is the district plaques, street
     toppers, borough-ring labels, bridge chips and drop labels --
     already declared presentational (aria-hidden="true") and drawn
     after `.plate`/`.blk`, so without this its own painted glyphs and
     pill backgrounds could sit on top of a district plate's or a
     building's clickable area and eat the click meant for it, the same
     shape as Fall.svelte's flag-mark badge (#1026). Nothing in `.flat`
     carries a handler or a focus target, so passing every pointer
     straight through loses nothing. */
  .flat {
    pointer-events: none;
  }

  /* A building with a host behind it is pointable: hovering opens its
     card, clicking stands on it (DESIGN.md "The reach"). */
  .blk.hot {
    cursor: pointer;
  }

  .blk.hot:hover {
    filter: brightness(1.25);
  }

  /* The flagged halo hugs the building and breathes in place. It must
     never ripple outward (DESIGN.md "Honesty and motion", owner
     2026-09-07): a ring that grows reads as something spreading, and
     nothing is spreading -- the flag is already open.

     The off-baseline arrival mark wears the same rule (#1057): it is the
     arrived-at building's own outline, `halo arrived`, so the city has
     one motion and not two. `.arrived` is a selector for checks and
     carries no style of its own -- what the mark looks like is the road
     ink and weight the building's paint already sets. */
  .halo {
    animation: halo 1.6s ease-in-out infinite;
  }

  @keyframes halo {
    0%,
    100% {
      opacity: 0.45;
      stroke-width: 1.2;
    }

    50% {
      opacity: 1;
      stroke-width: 2;
    }
  }

  /* The activity-spike pulse (#981, round 46). The rim's opacity
     breathes and a blurred copy of it swells from almost nothing to a
     wide bloom, on a 2s loop. The swing is deliberately wide: a first
     draft breathed 0.6->1 over 2px->6px and the dim and bright
     freeze-frames were nearly indistinguishable. The red fill itself
     does not animate; only the rim and the glow do, and neither ever
     moves the hue. */
  .mk-spike-rim {
    animation: mk-spike-rim 2s ease-in-out infinite;
  }

  .mk-spike-glow {
    filter: blur(3px);
    animation: mk-spike-glow 2s ease-in-out infinite;
  }

  @keyframes mk-spike-rim {
    0%,
    100% {
      stroke-opacity: 0.45;
    }

    50% {
      stroke-opacity: 1;
    }
  }

  @keyframes mk-spike-glow {
    0%,
    100% {
      stroke-width: 1.5px;
      opacity: 0.12;
    }

    50% {
      stroke-width: 9px;
      opacity: 0.75;
    }
  }

  .p-name {
    font: 600 12.5px var(--font-sans);
    fill: var(--fg);
  }

  .p-name.small {
    font-size: 11.5px;
  }

  .p-cidr {
    font: 10px var(--font-mono);
    fill: var(--fg-dim);
  }

  /* ---- The boundary card, ported from round 49's `.card` ---- */

  .wall-hot {
    cursor: pointer;
  }

  /* The open boundary stays marked for as long as its card is open,
     pinned or not (DESIGN.md "Cards"). Ten grey dashed boundaries can
     be on screen at once, and a card naming two of them in words alone
     does not say which -- the leader points at one end, and this
     accent glow says the wall it points at is the subject. It traces
     the material rather than recolouring it, because the colour is the
     coverage and would be a different statement. */
  .wall-hot.on {
    filter: brightness(1.5) drop-shadow(0 0 3px var(--accent));
  }

  /* The leader, under the card it joins. It covers the whole view and
     takes no pointer, so it can never come between the pointer and
     either end of the journey it is drawing (#1027). */
  .leader {
    position: absolute;
    inset: 0;
    z-index: 8;
    width: 100%;
    height: 100%;
    pointer-events: none;
    overflow: visible;
  }

  .bcard {
    position: absolute;
    z-index: 9;
    /* Where the card sits before it has been placed -- the drawing's
       own corner, used for the frame between the card mounting and
       being measured, and wherever there is nothing to measure. Once
       `placed` lands, left/top come from lib/cardAnchor.ts instead. */
    right: 20px;
    bottom: 32px;
    width: 288px;
    padding: 9px 12px;
    background: rgba(15, 20, 34, 0.95);
    border: 1px solid var(--hair-2);
    border-radius: 10px;
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
    font: 10.5px var(--font-mono);
    color: var(--fg-muted);
  }

  /* Placed, the card is positioned from its own top-left, so the
     corner anchoring above has to be released. */
  .bcard.placed {
    right: auto;
    bottom: auto;
  }

  .bcard.pinned {
    border-color: rgba(157, 184, 232, 0.5);
  }

  .bc-t {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .bc-t .n {
    font: 650 13.5px var(--font-sans);
    color: var(--fg);
    flex: 1;
  }

  .bc-t .n small {
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
    margin-left: 6px;
  }

  .bcard .pin {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1px solid var(--hair-2);
    background: transparent;
    color: var(--fg-dim);
    font: 11px var(--font-sans);
    line-height: 1;
    cursor: pointer;
    align-self: flex-start;
  }

  .bcard .pin.on,
  .bcard .pin:hover {
    color: var(--accent);
    border-color: var(--accent);
  }

  .bcard .s {
    margin-top: 3px;
    color: var(--fg-dim);
  }

  .bcard .s.dark {
    color: var(--fg-dim);
  }

  .bcard .s.quiet {
    color: var(--fg);
    opacity: 0.7;
  }

  .bcard .s.logged {
    color: var(--accept);
  }

  .bcard .s.alarm {
    color: var(--alarm);
  }

  /* The totals sit just clear of the table they sum, as in the mockup. */
  /* The marked host's counts line (#981): flag words in the alarm ink,
     watch words in the watcher's, no glyphs. */
  .bcard .s.counts .fw {
    color: var(--alarm);
    font-weight: 600;
  }

  .bcard .s.counts .ww {
    color: var(--marked);
    font-weight: 600;
  }

  .bcard .s.totals {
    margin-top: 6px;
  }

  .bcard .s.totals b {
    color: var(--fg);
    font-weight: 600;
  }

  /* The same three treatments the wall wears, as a swatch: solid for
     logged, flat white for declared quiet, dashed grey for dark. */
  .bcard .sw {
    display: inline-block;
    width: 22px;
    height: 3px;
    border-radius: 2px;
    vertical-align: middle;
    margin-right: 6px;
  }

  .bcard .sw.dark {
    background: repeating-linear-gradient(90deg, var(--fg-dim) 0 3px, transparent 3px 6px);
  }

  .bcard .sw.quiet {
    background: var(--fg);
    opacity: 0.4;
  }

  .bcard .sw.logged {
    background: var(--accept);
    opacity: 0.8;
  }

  /* The baseline's own two swatches (round-49/index.html:200-201): the
     established line thin and faint, the off-baseline one full strength
     with the ring's halo around it. Same two marks on both surfaces. */
  .bcard .sw.est {
    background: var(--accept);
    height: 2px;
    opacity: 0.3;
  }

  .bcard .sw.nb {
    background: var(--accept);
    box-shadow: 0 0 0 2px rgb(62 207 126 / 30%);
  }

  .bcard .s.est {
    color: var(--fg-dim);
  }

  /* The lines themselves. Narrow type and tight rows because this table
     can be long on a busy day, and it is a list to scan rather than to
     read. */
  .bcard table.off,
  .bcard table.ports {
    width: 100%;
    margin-top: 6px;
    border-collapse: collapse;
    font: 10px/1.5 var(--font-mono);
  }

  .bcard table.off th,
  .bcard table.ports th {
    padding: 2px 4px 2px 0;
    border-bottom: 1px solid var(--hair);
    color: var(--fg-dim);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-align: left;
  }

  .bcard table.off td,
  .bcard table.ports td {
    padding: 3px 4px 3px 0;
    color: var(--fg);
    vertical-align: top;
  }

  .bcard table.off th.n,
  .bcard table.off td.n,
  .bcard table.ports th.n,
  .bcard table.ports td.n {
    text-align: right;
    padding-right: 0;
    font-variant-numeric: tabular-nums;
  }

  .bcard table.off td.ok,
  .bcard table.ports td.ok {
    color: var(--accept);
  }

  /* A count that was refused, and a cell with nothing in it. The em dash
     is "none of these", not zero-as-a-measurement, so it recedes. */
  .bcard table.ports td.al {
    color: var(--alarm);
  }

  .bcard table.ports td.dim {
    color: var(--fg-dim);
  }

  /* The verdict in plain words, under the line it is about. */
  .bcard table.off tr.why td {
    padding-top: 0;
    color: var(--fg-dim);
    font: 10px/1.4 var(--font-sans);
  }

  .bcard table.off tr.actrow td,
  .bcard table.off tr.formrow td {
    padding: 2px 0 8px;
  }

  .bcard .linkact {
    padding: 0;
    border: 0;
    background: none;
    color: var(--accent);
    font: 10px var(--font-sans);
    cursor: pointer;
  }

  .bcard .linkact:hover {
    text-decoration: underline;
  }

  /* The bulk control sits below the table, ruled off from it, so it
     reads as being about the whole list rather than about whichever row
     it happens to sit under. Dim rather than accent: it is the
     deliberate one, never the one the eye lands on first. */
  .bcard .allof {
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px solid var(--hair);
  }

  .bcard .linkact.all {
    color: var(--fg-dim);
  }

  .bcard .linkact.all:hover {
    color: var(--accent);
  }

  /* A bright road is pointable on a wide invisible stroke, and stays
     marked while its card is open -- the same job .wall-hot.on does for
     a boundary, said the same way. */
  .road-hot {
    cursor: pointer;
  }

  .road-hot.on {
    stroke: var(--accent);
    stroke-opacity: 0.22;
  }

  .bcard .quote {
    margin: 5px 0 0;
    padding: 5px 8px;
    border-left: 2px solid var(--hair-2);
    color: var(--fg);
    font: italic 11px var(--font-sans);
  }

  .bcard .acts {
    display: flex;
    gap: 12px;
    margin-top: 8px;
    padding-top: 7px;
    border-top: 1px solid var(--hair);
    flex-wrap: wrap;
  }

  .bcard .acts button {
    background: none;
    border: 0;
    padding: 0;
    color: var(--accent);
    font: 10.5px var(--font-mono);
    cursor: pointer;
  }

  .bcard .acts button:hover {
    text-decoration: underline;
  }

  .bcard .acts button.dim {
    color: var(--fg-dim);
  }

  .bcard .acts button.hot {
    color: var(--alarm);
  }

  /* The declare form, property for property with the 2D map's
     `.card .form` (#1031). The two footers had drifted -- "both
     directions" on its own line here, tucked in beside the buttons
     there -- and the two rule sets had drifted with them, as far as the
     city painting its Declare button `var(--raised)`, a token this app
     never defines. Keep the two blocks in step. */
  .bcard .form {
    margin-top: 7px;
  }

  .bcard .form label {
    display: block;
    margin-bottom: 3px;
    font: 600 9px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-dim);
  }

  .bcard .form input {
    width: 100%;
    padding: 5px 8px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--fg);
    font: 11px var(--font-sans);
    outline: none;
  }

  .bcard .form input:focus {
    border-color: var(--accent);
  }

  .bcard .form label.both {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 7px;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
  }

  .bcard .form label.both input {
    width: auto;
    accent-color: var(--accent);
  }

  .bcard .form .btns {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: 7px;
  }

  .bcard .form .go {
    padding: 4px 12px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-elevated);
    color: var(--fg);
    font: 600 10.5px var(--font-mono);
    cursor: pointer;
  }

  .bcard .form .go:hover:not(:disabled) {
    border-color: var(--accent);
  }

  .bcard .form .go:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .bcard .form .no {
    border: 0;
    background: none;
    padding: 0;
    color: var(--fg-dim);
    font: 10.5px var(--font-mono);
    cursor: pointer;
  }

  .bcard .form .who {
    margin-left: auto;
    color: var(--fg-dim);
  }

  .bcard .d-error {
    margin: 5px 0 0;
    font-size: 11.5px;
    color: var(--reject);
  }

  /* The plaque's dim second line. The coverage words that used to sit
     here went with round 49 (#1016): the wall is the statement. */
  .p-note {
    font: 9.5px var(--font-mono);
    fill: var(--fg-dim);
  }

  .chip-t {
    font: 10.5px var(--font-mono);
    fill: var(--fg-muted);
  }

  .drop-t {
    font: 600 9.5px var(--font-mono);
    fill: var(--drop);
  }

  /* The port filter's doors and its one line (#1055, round 54). The
     posts carry the verdict's own ink and their round cap from the paint
     itself, as every other solid here does, so nothing about them is
     said twice. The label takes the flat map's own weight and its
     painted-over stroke, so a door standing over a road stays readable
     where the road runs under its words. */
  .door-t {
    font: 600 9.5px var(--font-mono);
    fill: var(--fg-dim);
  }

  .door-act {
    fill: var(--accept);
  }

  .door-t.shut .door-act {
    fill: var(--alarm);
  }

  /* Nothing seen: one line under the city, not an empty state. */
  .note-t {
    font: 11px var(--font-mono);
    fill: var(--fg-dim);
  }

  .door-t,
  .note-t,
  .ghost-t {
    paint-order: stroke;
    stroke: var(--bg);
    stroke-width: 3.4px;
    stroke-linejoin: round;
  }

  .drop-t.alarm-t {
    fill: var(--alarm);
  }

  /* The event trace (#1050, rounds 54 & 56). The rib a refused line
     would have taken beyond the gate it stopped at, dashed, with its
     own note beside it -- the same treatment Topography.svelte's own
     copy uses for the flat map. */
  .trace-ghost {
    fill: none;
    stroke: var(--fg-muted);
    stroke-width: 1.4;
    stroke-dasharray: 2 5;
    stroke-linecap: round;
    opacity: 0.7;
  }

  .ghost-t {
    font: italic 9.5px var(--font-mono);
    fill: var(--fg-dim);
  }

  .trace-leader {
    fill: none;
    stroke: var(--hair-2);
    stroke-width: 1;
  }

  /* The router's own decision, beside wherever it was made. */
  .chip-verdict {
    font: 700 10.5px var(--font-mono);
    fill: var(--accept);
  }

  .chip-verdict.refused {
    fill: var(--alarm);
  }

  .st-name {
    font: 600 13px var(--font-sans);
    fill: var(--fg);
  }

  .st-ip {
    font: 10.5px var(--font-mono);
    fill: var(--fg-muted);
  }

  /* A quiet building's own name recedes with it, so the street reads at
     a glance as which machines are still talking. */
  .st-name.st-dim {
    fill: var(--fg-muted);
  }

  /* The straggler's road (#460, round 55): the same reserved saturated
     colour an unplanned road spends, because it is the same statement --
     traffic the map had no reason to expect. */
  .straggler-road {
    fill: none;
    stroke: var(--alarm);
    stroke-width: 1.4;
    stroke-linecap: round;
  }

  .straggler-call {
    cursor: pointer;
  }

  /* A ghost's plaque name reads a step back, like the flat map's: it is
     the name the range had, not one anything answers to now. */
  .gname {
    fill: var(--fg-muted);
  }

  .boro-t {
    font: 600 10px var(--font-mono);
    fill: var(--fg-dim);
    letter-spacing: 0.16em;
  }

  .flow {
    stroke-dasharray: 7 11;
    animation: flow 1.5s linear infinite;
  }

  /* Standing on a building (#868): a strand's own direction reverses
     the same animation rather than a second one -- dashes moving
     toward the host read as the mirror of moving away from it. */
  .flow.flow-rev {
    animation-direction: reverse;
  }

  @keyframes flow {
    to {
      stroke-dashoffset: -18;
    }
  }

  /* The river's ripple texture (#866): a very slow drift, never a dash
   * -- the owner's verdict on the mockup's dashed "current" lines was
   * that they made the river read as a road. */
  .ripple {
    animation: ripple-drift 15s ease-in-out infinite alternate;
  }

  @keyframes ripple-drift {
    to {
      transform: translate(3px, -1.5px);
    }
  }

  .lamp {
    animation: lamp 3.4s ease-in-out infinite;
  }

  @keyframes lamp {
    0%,
    100% {
      opacity: 0.55;
    }
    50% {
      opacity: 1;
    }
  }

  .mini {
    position: absolute;
    z-index: 8;
    left: 20px;
    top: 58px;
    width: 282px;
    padding: 8px 9px 7px;
    background: var(--glass);
    border: 1px solid var(--hair-2);
    border-radius: 9px;
    backdrop-filter: blur(7px);
    box-sizing: border-box;
  }

  .mini h4 {
    font: 600 9px var(--font-mono);
    letter-spacing: 0.14em;
    color: var(--fg-dim);
    margin: 0 0 5px;
  }

  .mini .look {
    display: block;
    padding: 0;
    margin: 0;
    border: 0;
    background: none;
    cursor: pointer;
    line-height: 0;
  }

  .mini .look:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 2px;
  }

  .mini svg {
    display: block;
  }

  .mini .mk {
    font: 9px var(--font-mono);
    color: var(--fg-dim);
    margin-top: 5px;
    display: flex;
    justify-content: space-between;
  }

  /* District names on the minimap (#978): the app's own 8px legibility
     floor, never below it (owner, 2026-09-06) -- the panel grew to give
     six names this size the room, and each sits under its own plate
     rather than over the diamond. */
  .mini-name {
    font: 8px var(--font-mono);
    fill: var(--fg-dim);
    pointer-events: none;
  }

  /* The crumb (#868), round-40's own layout: a pill centred at the top. */
  .crumb {
    position: absolute;
    z-index: 9;
    top: 20px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    gap: 11px;
    align-items: center;
    padding: 6px 14px;
    background: var(--glass);
    border: 1px solid var(--hair-2);
    border-radius: 999px;
    backdrop-filter: blur(7px);
    font: 10.5px var(--font-mono);
    color: var(--fg-dim);
    white-space: nowrap;
    cursor: pointer;
  }

  .crumb:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 2px;
  }

  .crumb > b {
    font: 650 12.5px var(--font-sans);
    color: var(--fg);
  }

  .crumb b {
    color: var(--fg-muted);
    font-weight: 600;
  }

  .crumb i {
    width: 1px;
    height: 12px;
    background: var(--hair-2);
  }

  .crumb .esc {
    color: var(--accent);
  }

  /* The composer (#868), round-40's own card: a wall-side note rather
   * than the 2D map's picker, so it is pinned near where the refused
   * road stopped without needing a second, pixel-tracked overlay
   * geometry -- the composer's own text already says which wall. */
  .composer {
    position: absolute;
    z-index: 9;
    left: 20px;
    bottom: 32px;
    width: 300px;
    padding: 10px 13px 9px;
    background: rgba(23, 10, 18, 0.95);
    border: 1px solid rgba(255, 84, 112, 0.6);
    border-radius: 11px;
    box-shadow: 0 14px 34px rgba(0, 0, 0, 0.55);
  }

  .composer .cm-h {
    font: 600 11.5px var(--font-sans);
    color: var(--fg);
    display: flex;
    align-items: center;
    gap: 7px;
  }

  .composer .cm-h .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--alarm);
    flex: none;
  }

  .composer .cm-b {
    font: 10.5px var(--font-mono);
    color: var(--fg-muted);
    margin: 4px 0 7px;
  }

  .composer .cm-code {
    font: 10px/1.55 var(--font-mono);
    color: var(--accept);
    background: #080c16;
    border: 1px solid var(--hair);
    border-radius: 7px;
    padding: 7px 9px;
    /* Wraps within the composer's fixed 300px width, the same as the 2D
     * composer's own .cmd (Topography.svelte) -- a printed command line
     * routinely runs past 300px, and overflow-x:auto here (the previous
     * rule) showed a horizontal scroll bar rather than fitting it (#974). */
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
  }

  .composer .cm-f {
    font: 9.5px var(--font-mono);
    color: var(--fg-dim);
    margin-top: 7px;
    letter-spacing: 0.06em;
  }

  @media (prefers-reduced-motion: reduce) {
    .flow,
    .lamp,
    .halo,
    .ripple {
      animation: none;
    }

    .flow {
      stroke-dasharray: none;
    }

    /* A pulse cannot reduce to nothing without losing the signal it
       exists to carry -- "is this happening now" -- so the spike holds
       at a steady bright rim and a mid-bright glow instead of being
       switched off (#981). */
    .mk-spike-rim {
      animation: none;
      stroke-opacity: 1;
    }

    .mk-spike-glow {
      animation: none;
      stroke-width: 6px;
      opacity: 0.55;
    }
  }
</style>
