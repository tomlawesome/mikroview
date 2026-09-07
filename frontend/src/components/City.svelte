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
  import { zonesState } from '../lib/zones.svelte'
  import { tunnelsState } from '../lib/tunnels.svelte'
  import { policyState } from '../lib/policy.svelte'
  import { coverageState } from '../lib/coverage.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import { tuneLoggingNavState } from '../lib/tuneLoggingNav.svelte'
  import { realityEdges } from '../lib/reality'
  import { symbolFor } from '../lib/city/blocks'
  import { buildingDepth, paintOrder, pieceDepth } from '../lib/city/depth'
  import { cityInputFrom } from '../lib/city/input'
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
  import { gateToward, lerpP, roadPieces, type Entity } from '../lib/city/roads'
  import { reachFor, type ReachStrand } from '../lib/reach'
  import { composeCommand, reachComposeInput } from '../lib/compose'
  import { riverScene } from '../lib/city/river'
  import { P, type Paint } from '../lib/city/paint'
  import { bridgeStateLabel } from '../lib/city/tunnelState'
  import { deviceKindFor } from '../lib/city/deviceKind'
  import { deviceScale, deviceStampAttrs, type DeviceStampAttrs } from '../lib/city/devices'
  import { faceCoverage, faceOf, facePoint, wallPiece, wallSegments, GATE_HALF_WIDTH, WALL_H, type WallBreak, type WallSide } from '../lib/city/walls'
  import { worseCoverage } from '../lib/city/gates'
  import { cardSize, drawnRect, grace, mapRect, placeCard, stageRect, unitMapper, watchCardSize, type Placement, type Rect } from '../lib/cardAnchor'
  import type { Coverage } from '../lib/coverageRule'
  import { authState } from '../lib/auth.svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { extractSourceIp, flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { HOST_QUIET_AFTER_MS, hostsState, presenceOf } from '../lib/hosts.svelte'
  import { baselineState } from '../lib/baseline.svelte'
  import {
    EMPTY_ROAD_BASELINE,
    addressName,
    endName,
    portWords,
    rollUpRoads,
    verdictWords,
    type RoadBaselineEntry,
  } from '../lib/city/baselineRoads'
  import { formatHM } from '../lib/format'
  import type { OffBaselineLine } from '../lib/baseline'
  import { presenceNote, quietFor, type CityHost, type HostPresence } from '../lib/city/presence'
  import CityDeviceDefs from './CityDeviceDefs.svelte'
  import type { Building, CityPeer, District, DistrictGate, Ground, RoadKind } from '../lib/city/types'

  let {
    stop,
    ground: groundProp,
    initialS,
    initialCentre,
    onCameraChange,
    onStandChange,
    flagsOn = true,
    watchOn = true,
  }: {
    stop: Stop
    ground?: Ground
    /** The two overlay pills (DESIGN.md "The always-on picture"), which
     * live in the slider's own row and apply to both views. They are
     * Topography.svelte's state, since that is where the row is drawn;
     * these are how it reaches this side. Both default to on, which is
     * the ratified default, so the city draws flags and watchers even
     * while nothing threads them through. */
    flagsOn?: boolean
    watchOn?: boolean
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
  /** A gate post: half its ground extent, and how tall it stands -- half
   * again the wall's own height (walls.ts's WALL_H), the mockup's ratio. */
  const GATE_POST_HALF = 0.6
  const GATE_POST_H = 2.25

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
    })),
  )

  /** Flagged and watched, read exactly as Topography.svelte's own
   * `nodeWarnings` reads them (its flags are the uncleared ones, matched
   * on the flag target's source address; its watchers are entries with
   * this address at either end). Two readings of one fact would agree
   * today and part on the first change to either. */
  const activeFlags = $derived(flagsState.list.filter((f) => !f.cleared))
  const flagCountFor = (ip: string): number => (ip ? activeFlags.filter((f) => extractSourceIp(f.target) === ip).length : 0)
  const isWatched = (ip: string): boolean => (ip ? watchlistState.entries.some((e) => e.source?.ip === ip || e.destIp === ip) : false)

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
        ),
      ),
  )

  const inkOf = (d: District) => LANE_INKS[d.ink % LANE_INKS.length]
  const districtOf = (id: string | null) => (id ? (ground.districts.find((d) => d.id === id) ?? null) : null)
  const allBuildings = $derived<Building[]>([...ground.nodes, ...ground.districts.flatMap((d) => d.buildings)])

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
  interface Stand {
    districtId: string | null
    id: string
    savedS: number
    savedCentre: Pt
  }
  let stand = $state<Stand | null>(null)
  const effectiveStop = $derived<Stop>(stand ? 'street' : stop)

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
    if (!s) return null
    return s.districtId ? (districtOf(s.districtId)?.buildings.find((b) => b.id === s.id) ?? null) : (ground.nodes.find((n) => n.id === s.id) ?? null)
  })

  // Reports the stood-on building back to whoever asked (#869: crossing
  // the slider's centre while standing on a host hands it to the 2D
  // side's reach, if the same host exists there).
  $effect(() => {
    onStandChange?.(standBuilding)
  })

  // reachFor is #626/#485's own strand model, unchanged: the city draws
  // exactly what it derives, never a second reading of the same events.
  const standReach = $derived(standBuilding ? reachFor(standBuilding.ip, zonesState.wanInterface, appState.events) : null)

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
    // Re-standing on another building (from within the reach) keeps the
    // original saved camera -- surfacing always returns to where you
    // stood before the first click, not to whichever building you last
    // passed through.
    const savedS = stand ? stand.savedS : S
    const savedCentre = stand ? stand.savedCentre : centre
    stand = { districtId, id, savedS, savedCentre }
    focus = { districtId, id }
    moveCamera(STOP_HEIGHT.street, [b.u, b.v])
  }

  function standSurface() {
    if (!stand) return
    const { savedS, savedCentre } = stand
    stand = null
    moveCamera(savedS, savedCentre)
  }

  $effect(() => {
    if (stand && !standBuilding) standSurface()
  })

  function onWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && stand) {
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

  /** A building click stands on it (#868) rather than merely focusing
   * it -- a district plate keeps the plain focus/pan onItemClick above. */
  function onBuildingClick(districtId: string | null, id: string) {
    if (dragged) return
    standOn(districtId, id)
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
        /** The flagged halo or the watcher's ring, when a pill asks for
         * one; null when neither applies. */
        ring: { cy: number; r: number; stroke: string; throb: boolean } | null
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
    /** Buildings the standing host actually reaches or is reached by,
     * always including itself. */
    litBuildingIds: Set<string>
    /** Where a blocked strand's road would have crossed this district's
     * wall, and the counterpart's name -- present only when the strand
     * arrived rather than left (#991: the drop is not on the building
     * you are standing on, so the source is named; your own outbound
     * attempt just reads "dropped"). */
    dropMarks: { p: Pt; source?: string }[]
    /** Where the busiest blocked strand's own road crosses the wall --
     * the composer's own pin point -- computed regardless of whether a
     * fresh bollard mark was drawn there or an existing one already
     * stood (#868's "pinned to the wall where the refused road
     * stopped"), so the composer never loses its anchor merely because
     * the mark it is pinned beside was #865's own. */
    composerAnchor: Pt | null
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
    const b = standBuilding
    const summary = standReach
    if (!b || !summary) return null
    const ownRoadIds = new Set<string>()
    const reverseIds = new Set<string>()
    const litBuildingIds = new Set<string>([b.id])
    const dropMarks: { p: Pt; source?: string }[] = []
    const wan = zonesState.wanInterface
    const myToken = b.districtId ?? b.id
    const myDistrict = b.districtId ? districtOf(b.districtId) : null

    const lane = b.districtId ? (ground.roads.find((r) => r.lane && r.from === b.id) ?? null) : null
    if (lane && summary.busiest) {
      ownRoadIds.add(lane.id)
      if (flowReversed(lane.pts, summary.busiest.direction, b.u, b.v)) reverseIds.add(lane.id)
    }

    // Where a road toward `counterpartToken` would cross this
    // district's own wall -- a district or bridge head this build has
    // ground for, or the district's own router when nothing resolves
    // it to a place (gates.ts's own fallback for the same case).
    function wallCrossingFor(counterpartToken: string): Pt {
      const d = districtOf(counterpartToken)
      if (d) return gateToward(myDistrict!, [d.u, d.v]).p
      const n = ground.nodes.find((x) => x.id === counterpartToken)
      if (n) return gateToward(myDistrict!, [n.u, n.v]).p
      const bridge = ground.bridges.find((br) => br.iface === counterpartToken)
      if (bridge) return gateToward(myDistrict!, bridge.f).p
      const rn = ground.nodes.find((x) => x.id === myDistrict!.routerId)
      return gateToward(myDistrict!, rn ? [rn.u, rn.v] : [myDistrict!.u, myDistrict!.v]).p
    }

    let composerAnchor: Pt | null = null
    if (myDistrict && summary.topBlocked) {
      const counterpartToken = summary.topBlocked.counterpart === 'internet' ? (wan ?? '') : summary.topBlocked.counterpart
      composerAnchor = wallCrossingFor(counterpartToken || myToken)
    }

    for (const s of summary.strands) {
      const counterpartToken = s.counterpart === 'internet' ? (wan ?? '') : s.counterpart
      if (counterpartToken) {
        const pairId = [myToken, counterpartToken].sort().join('|')
        const road = ground.roads.find((r) => !r.lane && r.id === pairId)
        if (road) {
          ownRoadIds.add(road.id)
          if (flowReversed(road.pts, s.direction, b.u, b.v)) reverseIds.add(road.id)
        }
        const bridge = ground.bridges.find((br) => br.iface === counterpartToken)
        if (bridge) {
          ownRoadIds.add('rb-' + bridge.id)
          ownRoadIds.add(bridge.id + '-span')
        }
      }
      if (s.outcome === 'accepted') {
        for (const addr of s.peerAddrs) {
          const peer = allBuildings.find((x) => x.ip === addr)
          if (!peer) continue
          litBuildingIds.add(peer.id)
          const peerLane = ground.roads.find((r) => r.lane && r.from === peer.id)
          if (peerLane) ownRoadIds.add(peerLane.id)
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
        if (!already) dropMarks.push({ p: wallCrossingFor(counterpartToken || myToken), source })
      }
    }

    return { ownRoadIds, reverseIds, litBuildingIds, dropMarks, composerAnchor }
  })

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
        ' hosts' +
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
        const t = WALL_TREATMENT[cov]
        const ink = cov === 'logged' ? inkOf(d) : COVERAGE_INK[cov]
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
          solids.push({ kind: 'other', v: m[1] + 0.8, paints, lamps })
        }
      }
    }

    // Roads, cut into pieces that carry their own depth.
    const dropLabels: { x: number; y: number; text: string; alarm: boolean }[] = []
    const ents = new Map<string, Entity>()
    for (const b of allBuildings) ents.set(b.id, { u: b.u, v: b.v, R: b.R })

    // The bollards, cross and red mark a dropped road ends at (#865) --
    // pulled out so standing on a building (#868) can pin the same mark
    // exactly where a blocked strand's own road would have crossed the
    // wall, not just where the district-pair aggregate already draws
    // one. `e` is the ground point the mark centres on (depth reads its
    // v, same as every other solid).
    // #991: the label is one plain word, "dropped" -- the source named
    // only when the drop is not on the building you are standing on
    // (`source` absent otherwise), never the refusing rule (that detail
    // moved to the composer card, #868's own click card for the reach).
    function dropMarkAt(e: Pt, alarm: boolean, source?: string) {
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
      // The refusing rule's own name, beside the mark -- the event's
      // rule label, exactly as the 2D reach does; said plainly when
      // no event on this pair carried one, never guessed (#865).
      dropLabels.push({ x: mx, y: my - 14 * k, text, alarm })
    }

    for (const r of g.roads) {
      if (r.lane && !showLanes) continue
      const own = !reachOverlay || reachOverlay.ownRoadIds.has(r.id)
      const col = VERDICT[r.k]
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
      const nb = roadBaseline.get(r.id) ?? null
      const est = r.k === 'a' && nb === null
      const w = Math.max(est ? 1 : 1.2, r.w * c.S * (est ? 0.18 : 0.3))
      // Standing on a building (#868) fades every road that is not its
      // own.
      const op = (r.k === 'x' ? 0.95 : r.k === 'q' ? 0.42 : r.k === 'd' ? 0.52 : est ? 0.26 : 0.8) * (own ? 1 : 0.16)
      // While standing, only this building's own roads flow, in the
      // direction its own strand reads. Otherwise a road flows exactly
      // when it carries something off the baseline, or when it is the
      // escalated unplanned pair -- the flow dashes are part of the
      // bright treatment, not a separate signal. Volume does not earn
      // flow: the ratified drawing (round 49, `flow: own ? mine :
      // (r.k === 'x' || !!r.nb)`) animates only those two, so a settled
      // network is still, however busy it is.
      const flow = reachOverlay ? own : r.k === 'x' || nb !== null
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
      if (glowD.length && !est) glows.push({ d: glowD.join(''), stroke: col, sw: R2(w + 4), so: 0.07 })
      // The ring, at the end the traffic arrived at. It hugs the road's
      // end and throbs in place -- it never pulses outward, because a
      // ring that grows reads as something spreading and nothing is
      // spreading (DESIGN.md, owner 2026-09-07). The same `.halo` rule
      // the flag pill uses, so there is one motion in the city, not two.
      if (nb && r.k === 'a') {
        const ringAt = (e: Pt) => {
          const rr = R2(Math.max(5, c.S * 0.7))
          solids.push({
            kind: 'other',
            v: e[1] + 8,
            paints: [{ cx: R2(X(c, e[0])), cy: R2(Y(c, e[1])), rx: rr, ry: rr, stroke: col, sw: 1.4, cls: 'halo' }],
            lamps: [],
          })
        }
        if (nb.ring.start) ringAt(r.pts[0])
        if (nb.ring.end) ringAt(r.pts[r.pts.length - 1])
      }
      // #991: the district-pair aggregate has no per-building source to
      // name (only the reach's own strands, below, resolve to one host),
      // so this mark reads as the one plain word, "dropped".
      if (r.stop === 'drop') dropMarkAt(r.pts[r.pts.length - 1], r.k === 'x')
    }
    // #991: "gone from the street stop" -- the road port chips (#868's
    // "ports on the road") are dropped entirely; the ports live on the
    // building's card and the gate's card instead. Nothing else about
    // roads changes.
    if (reachOverlay) for (const dm2 of reachOverlay.dropMarks) dropMarkAt(dm2.p, false, dm2.source)

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
      const dim = (d?.dark ?? false) || (reachOverlay ? !reachOverlay.litBuildingIds.has(b.id) : false)
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
      const what = b.kind === 'router' ? 'router' : b.kind === 'router-ant' ? 'router with antennas' : b.kind === 'post' ? 'bridge post' : 'host'
      // The overlay pills gate the marks on the drawing, so what a
      // screen reader is told and what the ring shows stay one fact.
      const flagCount = flagsOn ? flagCountFor(b.ip) : 0
      const watched = watchOn && isWatched(b.ip)
      const note = b.host ? presenceNote(b.host, nowTick) : null
      const aria =
        b.name +
        (b.ip ? ' at ' + b.ip : '') +
        ', ' +
        what +
        (d ? ' in ' + d.name : '') +
        (note ? ' · ' + note : '') +
        (flagCount ? ' · ' + flagCount + (flagCount === 1 ? ' flag' : ' flags') : '') +
        (watched ? ' · watched' : '')
      // The ring hugs the shape and throbs in place; it never pulses
      // outward (DESIGN.md "Honesty and motion", owner 2026-09-07). Red
      // for a flag, the watcher's own ink for a watcher, and a flag wins
      // when a building is both -- the louder fact is the one to see.
      const k = (R * 0.74 * c.S) / SREF
      const ring =
        flagCount || watched
          ? {
              cy: R2(-symbolFor(b.kind).top * k - 2),
              r: R2(Math.max(3.5, 4 * k)),
              stroke: flagCount ? 'var(--alarm)' : 'var(--marked)',
              // Only the flag throbs. A watcher is a standing statement,
              // not something that just happened, so its ring is still.
              throb: flagCount > 0,
            }
          : null
      solids.push({
        kind: 'building',
        v: buildingDepth(b),
        b,
        district: d,
        ink,
        dim,
        pres,
        ring,
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
        rings.push({
          d,
          label: bo.name.toUpperCase() + ' BOROUGH · ' + bo.districtIds.length + (bo.districtIds.length === 1 ? ' DISTRICT' : ' DISTRICTS'),
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
    for (const d of g.districts) {
      const x = R2(X(c, d.u))
      const y = R2(Y(c, d.v + d.r) + 5)
      const w = compact ? d.name.length * 7.2 + 26 : 200
      if (!claim(x, y, w, compact ? 20 : d.rulesPushed ? 28 : 40)) continue
      plaques.push({ d, x, y, w: R2(w), ink: inkOf(d) })
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

    return { groundPaints, glows, plates, solids: paintOrder(solids), rings, plaques, bridgeChips, dropLabels, claim }
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
  type WallRef = { districtId: string; side: WallSide }

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
    let gate = here[0]
    for (const g of here) if (worseCoverage(gate.coverage, g.coverage) === g.coverage && g.coverage !== gate.coverage) gate = g
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
    pinnedWall = { districtId: w.districtId, side: w.side }
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
   * dark boundary is exactly what tune-logging exists to fix. */
  function openRulesForBoundary() {
    const c = wallCard
    if (!c || !primaryDevice) return
    tuneLoggingNavState.request(primaryDevice.id, c.gate.key)
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
    const avoid = ends.flatMap(drawnPlate)
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
  let expectedKey = $state<string | null>(null)
  let expectedReason = $state('')
  let expectedBusy = $state(false)
  const roadGrace = grace()
  let rcardEl: HTMLDivElement | undefined = $state()
  let roadPlace = $state<Placement | null>(null)
  let roadCardTick = $state(0)

  const openRoadId = $derived(pinnedRoad ?? hoverRoad)
  const roadPinned = $derived(pinnedRoad !== null)

  /** The open road, its roll-up and the geometry the leader points at. */
  const roadCard = $derived.by(() => {
    const id = openRoadId
    if (!id) return null
    const entry = roadBaseline.get(id)
    if (!entry) return null
    const road = ground.roads.find((r) => r.id === id)
    if (!road) return null
    return { road, entry }
  })

  /** How many lines a road carries off the baseline, in plain words. */
  const offCount = (n: number) => `${n} off the baseline today`

  /** The road's accessible name: the two ends, and what it carries. */
  function roadAria(id: string): string {
    const e = roadBaseline.get(id)
    if (!e) return 'road'
    return `${endName(ground, e.ends.start)} → ${endName(ground, e.ends.end)} road, ${offCount(e.lines.length)}`
  }

  function openRoadCard(id: string) {
    if (drag?.moved) return
    // Only a road with something off the baseline has this card: an
    // established road's answer is the drawing itself, and a card saying
    // "nothing to report" on every road in the city would be noise of
    // exactly the kind this screen exists to remove.
    if (!roadBaseline.has(id)) return
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
      expectedKey = null
    } else {
      pinnedRoad = id
      hoverRoad = id
    }
  }

  /** Open the reason form for one line; the reason is required. */
  function startExpected(line: OffBaselineLine, roadId: string) {
    pinnedRoad = roadId
    hoverRoad = roadId
    expectedKey = line.key
    expectedReason = ''
    baselineState.error = null
  }

  async function submitExpected() {
    const key = expectedKey
    const reason = expectedReason.trim()
    if (!key || !reason || expectedBusy) return
    expectedBusy = true
    try {
      // baselineState.expected re-reads the register on success, so the
      // road goes dim by itself the moment the last of its lines is
      // spoken for -- nothing here has to model that.
      const ok = await baselineState.expected(key, reason)
      if (ok) {
        expectedKey = null
        expectedReason = ''
      }
    } finally {
      expectedBusy = false
    }
  }

  $effect(() => {
    // Everything that moves the subject, read first: which road is open,
    // the camera, the stop, the card's arrival, and both size ticks.
    const c = roadCard
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
    // already has something to say.
    const pts = c.road.pts
    const mid = pts[Math.floor(pts.length / 2)] ?? pts[0]
    const anchor = map({ x: X(vc, mid[0]), y: Y(vc, mid[1]) })
    // Keep off the two plates this road joins, and off the others only
    // as a tie-break -- the same reasoning the boundary card gives.
    const shown = (x: { id: string }) => host.querySelector(`g.plate[data-cid="${CSS.escape(x.id)}"]`)
    const drawnPlate = (x: { id: string; u: number; v: number; r: number }) => {
      const el = shown(x)
      return el !== null && drawnRect(el, host) !== null ? [mapRect(map, plateBox(x))] : []
    }
    const endIds = new Set([c.entry.ends.start, c.entry.ends.end])
    const avoid = ground.districts.filter((x) => endIds.has(x.id)).flatMap(drawnPlate)
    const softAvoid = ground.districts.filter((x) => !endIds.has(x.id)).flatMap(drawnPlate)
    roadPlace = placeCard({ anchor, card: cardSize(card), stage, avoid, softAvoid })
  })

  $effect(() => {
    const card = rcardEl
    if (!card) return
    return watchCardSize(card, () => roadCardTick++)
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
      <!-- `cls` carries the off-baseline ring's `halo`, which is what
           makes it throb in place; the animation sets stroke-width, so
           it overrides the attribute below by design. -->
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
  <svg
    bind:this={svgEl}
    viewBox="0 0 {STAGE_W} {STAGE_H}"
    preserveAspectRatio="xMidYMid meet"
    role="application"
    aria-label={standBuilding
      ? `Standing on ${standBuilding.name}${standBuilding.ip ? ' at ' + standBuilding.ip : ''}: Escape surfaces to where you were`
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
          <g
            class="plate"
            class:focused={focus?.id === p.d.id}
            role="button"
            tabindex={tabbable(p.d.id)}
            data-cid={p.d.id}
            aria-label={p.aria}
            onclick={() => onItemClick({ districtId: null, id: p.d.id })}
            onkeydown={onKey}
          >
            <title>{p.aria}</title>
            <!-- The plate keeps its own ink unless every one of the
                 district's boundaries is dark (round 49, #1016); then,
                 and only then, it goes grey and dashed. -->
            <path d={p.outer} fill={p.d.plateDark ? 'var(--fg-dim)' : p.ink} fill-opacity={p.d.plateDark ? 0.06 : 0.1} />
            <path
              d={p.inner}
              fill="none"
              stroke={p.d.plateDark ? 'var(--fg-dim)' : p.ink}
              stroke-opacity={p.d.plateDark ? 0.2 : 0.17}
              stroke-width="0.7"
              stroke-dasharray={p.d.plateDark ? '3 4' : undefined}
            />
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
            {#if s.roadId && roadBaseline.has(s.roadId) && s.paints[0]?.d}
              <!-- A bright road is pointable, on a wide transparent
                   stroke because the road itself is too thin to hit
                   (round-49/index.html:1204). Its card is the off-
                   baseline roll-up; while that card is open this stroke
                   goes visible, which is how the subject stays marked. -->
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
                onclick={() => toggleRoadPin(rid)}
                onkeydown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    toggleRoadPin(rid)
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
                <path d={p.d} fill={p.fill} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} stroke-dasharray={p.dash} />
              {/each}
              <g transform="translate({s.stamp.x} {s.stamp.y})">
                <g transform="scale({s.stamp.k})" style:color={s.ink} opacity={s.pres === 'quiet' ? 0.62 : s.pres === 'intended' ? 0.55 : s.dim ? 0.62 : undefined}>
                  {#each symbolFor(s.b.kind).paths as p, j (j)}
                    <path d={p.d} fill={p.fill === 'void' ? VOID : 'currentColor'} fill-opacity={p.fillOpacity} stroke={p.fill === 'body' ? 'currentColor' : undefined} stroke-opacity={p.strokeOpacity} stroke-width={p.strokeWidth} />
                  {/each}
                </g>
                {#if s.ring}
                  <!-- The flagged halo and the watcher ring (round 49's
                       two pills): both hug the building, and only the
                       flag's throbs -- in place, never outward. -->
                  <circle
                    class:halo={s.ring.throb}
                    cx="0"
                    cy={s.ring.cy}
                    r={s.ring.r}
                    fill="none"
                    stroke={s.ring.stroke}
                    stroke-width="1.4"
                    stroke-opacity={s.ring.throb ? undefined : 0.8}
                  />
                {/if}
              </g>
            </g>
          {/if}
        {/each}
      </g>
      <g class="flat" aria-hidden="true">
        {#each scene.plaques as p (p.d.id)}
          <g transform="translate({p.x} {p.y})">
            <!-- The plaque says name · subnet, and nothing else (round
                 49, #1016): LOGGED, DARK and NO RULES PUSHED are gone
                 from it because the wall now says what the coverage is,
                 and a plaque that ignored declarations said the wrong
                 thing anyway (#1014). `no rule table pushed` stays, dim,
                 because it is a different fact from dark: there, a table
                 exists and nothing on it logs. -->
            {#if compact}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height="20" rx="10" fill="#0a0f1c" fill-opacity="0.9" stroke="var(--border)" />
              <circle cx={R2(-p.w / 2 + 11)} cy="10" r="3.2" fill={p.ink} />
              <text x={R2(-p.w / 2 + 19)} y="14" class="p-name small">{p.d.name}</text>
            {:else}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height={p.d.rulesPushed ? 28 : 40} rx="8" fill="#0a0f1c" fill-opacity="0.93" stroke="var(--border)" />
              <circle cx={R2(-p.w / 2 + 13)} cy="14" r="3.4" fill={p.ink} />
              <text x={R2(-p.w / 2 + 22)} y="18" class="p-name">{p.d.name}</text>
              <text x={R2(p.w / 2 - 11)} y="17.5" text-anchor="end" class="p-cidr">{p.d.cidr ?? 'no address pushed'}</text>
              {#if !p.d.rulesPushed}
                <text x={R2(-p.w / 2 + 13)} y="32" class="p-note">no rule table pushed</text>
              {/if}
            {/if}
          </g>
        {/each}
        {#each toppers as t (t.b.id)}
          <g transform="translate({t.x} {t.y})">
            <text x="0" y="-13" text-anchor="middle" class="st-name" class:st-dim={t.quiet}>{t.b.name}</text>
            <text x="0" y="0" text-anchor="middle" class="st-ip">{t.sub}</text>
          </g>
        {/each}
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
      </g>
    </g>
  </svg>

  {#if standBuilding && standReach}
    <!-- The crumb (#868, DESIGN.md "The reach"): name · address ·
         reaches N · reached by N · Esc surfaces -- the 2D map's own
         "reaches <b>N</b> · reached by <b>N</b>" wording (Topography.svelte's
         reach crumb), the round-40 mockup's layout and its own literal
         "Esc surfaces" for the rest. It is itself the other way to
         surface (#868's own "Esc or the crumb"), restoring the exact
         camera standing started from, same as Escape. -->
    <button type="button" class="crumb" aria-label="Standing on {standBuilding.name}. Activate to surface." onclick={standSurface}>
      <b>{standBuilding.name}</b>
      <span>{standBuilding.ip}</span>
      <i></i>
      <span>reaches <b>{standReach.reaches}</b></span>
      <span>reached by <b>{standReach.reachedBy}</b></span>
      <i></i>
      <span class="esc">Esc surfaces</span>
    </button>
  {/if}

  {#if standBuilding && standReach?.topBlocked}
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
        <div class="form">
          <label for="city-declare-reason">QUIET ON PURPOSE — WHY?</label>
          <input id="city-declare-reason" bind:value={declareReason} placeholder="why this gap is intentional…" />
          <div class="btns">
            <button type="button" class="go" disabled={!declareReason.trim() || declareBusy} onclick={submitDeclaration}>Declare</button>
            <button type="button" class="no" onclick={() => (pinnedWall = null)}>cancel</button>
            <label class="who">
              <input type="checkbox" bind:checked={declareBoth} />
              as {authState.username || 'you'} · both directions
            </label>
          </div>
          {#if coverageState.error}<div class="s alarm">{coverageState.error}</div>{/if}
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
      aria-label="{c.b.name}: {PRESENCE_WORD[c.h.presence]}"
      onpointerenter={hostGrace.hold}
      onpointerleave={releaseHostCard}
    >
      <div class="bc-t">
        <span class="n">{c.b.name}<small>{c.b.ip}</small></span>
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
      {:else}
        <div class="s logged"><i class="sw logged"></i>live</div>
      {/if}

      <div class="s">
        last seen {stamp(c.h.lastSeen)} · first seen {stamp(c.h.firstSeen)} · {c.h.events.toLocaleString()}
        {c.h.events === 1 ? 'event' : 'events'} · {c.d.name}
      </div>

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
        {#if authState.canEdit && c.h.key}
          {#if c.h.reason}
            <button type="button" class="hot" disabled={markBusy} onclick={unmarkHost}>unmark ▸</button>
          {:else if !hostPinned}
            <button type="button" onclick={openMarkForm}>mark quiet on purpose ▸</button>
          {/if}
          <button type="button" class="hot" disabled={markBusy} onclick={dismissHost}>dismiss ▸</button>
        {/if}
        <button type="button" class="dim" onclick={() => (appState.view = 'live')}>stream ▸</button>
      </div>
    </div>
  {/if}

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
            {#if expectedKey === l.key}
              <tr class="formrow">
                <td colspan="4">
                  <div class="form">
                    <label for="city-expected-reason">EXPECTED — WHY?</label>
                    <input id="city-expected-reason" bind:value={expectedReason} placeholder="why this line is meant to be here…" />
                    <div class="btns">
                      <button type="button" class="go" disabled={!expectedReason.trim() || expectedBusy} onclick={submitExpected}>Expected</button>
                      <button type="button" class="no" onclick={() => (expectedKey = null)}>cancel</button>
                      <span class="who">as {authState.username || 'you'} · this line only</span>
                    </div>
                    {#if baselineState.error}<div class="s alarm">{baselineState.error}</div>{/if}
                  </div>
                </td>
              </tr>
            {:else if authState.isAdmin}
              <tr class="actrow">
                <td colspan="4"><button type="button" class="linkact" onclick={() => startExpected(l, rc.road.id)}>expected ▸</button></td>
              </tr>
            {/if}
          {/each}
        </tbody>
      </table>
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
     nothing is spreading -- the flag is already open. */
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
  .bcard table.off {
    width: 100%;
    margin-top: 6px;
    border-collapse: collapse;
    font: 10px/1.5 var(--font-mono);
  }

  .bcard table.off th {
    padding: 2px 4px 2px 0;
    border-bottom: 1px solid var(--hair);
    color: var(--fg-dim);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-align: left;
  }

  .bcard table.off td {
    padding: 3px 4px 3px 0;
    color: var(--fg);
    vertical-align: top;
  }

  .bcard table.off th.n,
  .bcard table.off td.n {
    text-align: right;
    padding-right: 0;
  }

  .bcard table.off td.ok {
    color: var(--accept);
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

  .bcard .form {
    margin-top: 7px;
  }

  .bcard .form > label {
    display: block;
    font: 600 9px var(--font-mono);
    letter-spacing: 0.1em;
    color: var(--fg-dim);
    margin-bottom: 3px;
  }

  .bcard .form input:not([type]) {
    width: 100%;
    padding: 5px 8px;
    background: #080c16;
    border: 1px solid var(--hair-2);
    border-radius: 6px;
    color: var(--fg);
    font: 11px var(--font-sans);
    outline: none;
  }

  .bcard .form input:focus {
    border-color: var(--accent);
  }

  .bcard .form .btns {
    display: flex;
    gap: 8px;
    margin-top: 7px;
    align-items: center;
  }

  .bcard .form .go {
    padding: 4px 12px;
    border-radius: 999px;
    border: 1px solid var(--hair-2);
    background: var(--raised);
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
    background: none;
    border: 0;
    padding: 0;
    color: var(--fg-dim);
    font: 10.5px var(--font-mono);
    cursor: pointer;
  }

  .bcard .form .who {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--fg-dim);
    font: 10.5px var(--font-mono);
    cursor: pointer;
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

  .drop-t.alarm-t {
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
  }
</style>
