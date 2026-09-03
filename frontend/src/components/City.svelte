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
  // What stands on a plinth comes from one place, symbolFor (#864
  // replaces the plain blocks). Rule gates and walls are #865's; the
  // river and bridges are #866's; importance is #867's; standing on a
  // building (#868) is this file's own "the reach" section below.
  import { tick, untrack } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { tunnelsState } from '../lib/tunnels.svelte'
  import { policyState } from '../lib/policy.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
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
    clampCentre,
    diamond,
    ease,
    gbox,
    groundAt,
    lerpCam,
    minimapCam,
    reducedMotion,
    viewportRect,
    wallFace,
    type BoxFaces,
    type Cam,
    type Pt,
    type Stop,
  } from '../lib/city/project'
  import { gateToward, lerpP, roadPieces, type Entity } from '../lib/city/roads'
  import { portsLine, reachFor, type ReachStrand } from '../lib/reach'
  import { composeCommand, reachComposeInput } from '../lib/compose'
  import { riverScene } from '../lib/city/river'
  import { P, type Paint } from '../lib/city/paint'
  import { bridgeStateLabel } from '../lib/city/tunnelState'
  import { deviceKindFor } from '../lib/city/deviceKind'
  import { deviceScale, deviceStampAttrs, type DeviceStampAttrs } from '../lib/city/devices'
  import { faceOf, wallPiece, wallSegments, type WallBreak } from '../lib/city/walls'
  import {
    IMPORTANCE_FLOOR_H,
    IMPORTANCE_READINGS,
    dependedOnImportance,
    tweenHeights,
    watchedImportance,
    watchedNotice,
    type Importance,
    type ImportanceReading,
  } from '../lib/city/importance'
  import { cityImportanceState } from '../lib/cityImportance.svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import CityDeviceDefs from './CityDeviceDefs.svelte'
  import type { Building, CityLens, CityPeer, District, DistrictGate, Ground, RoadKind } from '../lib/city/types'

  let {
    stop,
    ground: groundProp,
    lens = 'traffic',
    initialS,
    initialCentre,
    onCameraChange,
    onStandChange,
  }: {
    stop: Stop
    ground?: Ground
    lens?: CityLens
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
  /** The policy lens fades roads and lights every gate with its rule
   * number; the traffic lens is the reverse (#865's own lens table). */
  const policyLens = $derived(lens === 'policy')

  /* ---------------- the model ---------------- */

  const primaryDevice = $derived.by(() => {
    const list = appState.devices
    if (list.length === 0) return null
    const configured = list.filter((d) => d.configured)
    const pool = configured.length > 0 ? configured : list
    return [...pool].sort((a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime())[0]
  })

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
        ),
      ),
  )

  const inkOf = (d: District) => LANE_INKS[d.ink % LANE_INKS.length]
  const districtOf = (id: string | null) => (id ? (ground.districts.find((d) => d.id === id) ?? null) : null)
  const allBuildings = $derived<Building[]>([...ground.nodes, ...ground.districts.flatMap((d) => d.buildings)])

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
  let anim: number | null = null
  let svgEl: SVGSVGElement | undefined = $state()

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
      // While standing, the camera belongs to standOn/standSurface --
      // the slider's own stop keeps changing under it unread, so
      // surfacing lands on the position it actually saved rather than
      // wherever the prop drifted to meanwhile.
      if (stand) return
      const to = centreFor(s, focus)
      if (!started) {
        started = true
        S = initialS ?? STOP_HEIGHT[s]
        centre = clampCentre(initialCentre ?? to, g.bounds)
        return
      }
      moveCamera(STOP_HEIGHT[s], to)
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

  /* ---------------- importance: the plinth's height (#867) ---------------- */

  // Reading in, per-building normalised height out -- allBuildings is
  // every building this ground has, whichever stop is showing, so the
  // reading never has to know about districts vs nodes.
  const importance = $derived.by((): Map<string, Importance> => {
    const buildings = allBuildings.map((b) => ({ id: b.id, ip: b.ip }))
    return cityImportanceState.reading === 'watched'
      ? watchedImportance(buildings, flagsState.list, watchlistState.entries)
      : dependedOnImportance(buildings, appState.events)
  })

  const importanceNotice = $derived(cityImportanceState.reading === 'watched' ? watchedNotice(flagsState.loaded, watchlistState.loaded) : null)

  // The plinth heights actually drawn: tweened toward `importance`'s
  // target on every change (a toggle flip, new traffic, a flag raised),
  // snapped instantly under reduced motion -- the same shape moveCamera
  // above uses for the camera, via the same reducedMotion() and the
  // reading's own tweenHeights (city/importance.ts).
  let plinthHeights = $state<Map<string, number>>(new Map())
  let heightAnim: number | null = null

  $effect(() => {
    const target = importance
    untrack(() => {
      if (heightAnim !== null) cancelAnimationFrame(heightAnim)
      heightAnim = null
      if (reducedMotion() || typeof requestAnimationFrame !== 'function') {
        plinthHeights = tweenHeights(plinthHeights, target, 1, true)
        return
      }
      const from = plinthHeights
      const t0 = performance.now()
      const step = (now: number) => {
        const t = ease((now - t0) / MOVE_MS)
        plinthHeights = tweenHeights(from, target, t, false)
        heightAnim = t < 1 ? requestAnimationFrame(step) : null
      }
      heightAnim = requestAnimationFrame(step)
    })
  })

  $effect(() => () => {
    if (heightAnim !== null) cancelAnimationFrame(heightAnim)
  })

  /** The plinth height actually drawn for a building: its tweened
   * importance, or the floor before the first tween has run. */
  const heightOf = (b: Building): number => plinthHeights.get(b.id) ?? IMPORTANCE_FLOOR_H

  const importanceLabel = (id: ImportanceReading) => IMPORTANCE_READINGS.find((r) => r.id === id)!.label
  const otherReading = $derived<ImportanceReading>(cityImportanceState.reading === 'watched' ? 'depended-on' : 'watched')

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
    ;(e.currentTarget as Element).setPointerCapture(e.pointerId)
  }
  function onPointerMove(e: PointerEvent) {
    if (!drag) return
    const k = stageScale()
    const dx = (e.clientX - drag.x) * k
    const dy = (e.clientY - drag.y) * k
    if (Math.abs(dx) + Math.abs(dy) > 3) drag.moved = true
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
    | { kind: 'other'; v: number; paints: Paint[]; lamps: { x: number; y: number; r: number; rr: number; h: number }[] }
    | { kind: 'building'; v: number; b: Building; district: District | null; ink: string; dim: boolean; paints: Paint[]; stamp: { x: number; y: number; k: number }; aria: string }
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
    /** A port label to draw at an accepted own road's midpoint. */
    portChips: { roadId: string; text: string }[]
    /** Where a blocked strand's road would have crossed this district's
     * wall, and the rule that refused it (absent said plainly, never
     * guessed -- #865's own rule, the same source: the event's own
     * ruleLabel). */
    dropMarks: { p: Pt; refusedBy?: string }[]
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
    const portChips: { roadId: string; text: string }[] = []
    const dropMarks: { p: Pt; refusedBy?: string }[] = []
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
          if (s.outcome === 'accepted' && s.ports.length > 0) portChips.push({ roadId: road.id, text: portsLine(s.ports) })
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
        if (!already) dropMarks.push({ p: wallCrossingFor(counterpartToken || myToken), refusedBy: s.refusedBy })
      }
    }

    return { ownRoadIds, reverseIds, litBuildingIds, portChips, dropMarks, composerAnchor }
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
        const lit = b.state === 'up' || b.state === 'quiet'
        const ink = lit ? 'var(--accent)' : 'var(--fg-dim)'
        for (const t of [0.3, 0.7]) {
          const p = lerpP(b.f, b.t, t)
          const pr = gbox(c, p[0], p[1], b.w * 0.5, b.w * 0.5, -0.9, 1.5)
          solids.push({ kind: 'other', v: p[1] + 1, paints: gfaces(pr, 'var(--fg-dim)', { t: 0.5, r: 0.42, l: 0.26, s: 0.4 }), lamps: [] })
        }
        const deck = gbox(c, b.mid[0], b.mid[1], b.half, b.w, 0.55, 0.3)
        const paints = gfaces(deck, ink, { t: 0.3, r: 0.5, l: 0.3, s: 0.55, bg: true })
        const rail = (sgn: number) => {
          const a: Pt = [b.f[0] + sgn * b.w, b.f[1] - sgn * b.w]
          const z: Pt = [b.t[0] + sgn * b.w, b.t[1] - sgn * b.w]
          return 'M' + P(c, a, 0.85) + 'L' + P(c, z, 0.85)
        }
        paints.push({ d: rail(1), stroke: ink, so: 0.75, sw: 1 }, { d: rail(-1), stroke: ink, so: 0.55, sw: 1 })
        const lamps: { x: number; y: number; r: number; rr: number; h: number }[] = []
        // The road bridge's lamp is coverage, not traffic: a logging
        // rule covers the boundary (state 'up' means lamped for a road
        // bridge -- see layout.ts's wanLogged wiring), never events.
        if (b.kind === 'road' && b.state === 'up') {
          for (let i = 0; i < 2; i++) {
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

    // Walls and gates (#865): every plate's own low prism, in its VLAN
    // tint, broken open only where a pushed accept rule actually crosses
    // that boundary. A gate that resolves to a point on one of the two
    // back edges the camera cannot see draws nothing -- the same
    // silence a hidden building face keeps -- but still keeps its lamp
    // and rule count for the plaque and the policy lens.
    const gateBadges: { x: number; y: number; n: number }[] = []
    for (const d of g.districts) {
      const dim = d.dark
      const wallInk = dim ? 'var(--fg-dim)' : inkOf(d)
      const visible: { g: DistrictGate; f: WallBreak }[] = []
      for (const gate of d.gates) {
        const f = faceOf(d, gate.p)
        if (f) visible.push({ g: gate, f })
      }
      const segs = wallSegments(d, visible.map((v) => v.f))
      for (const seg of segs) {
        const mid = (seg.t0 + seg.t1) / 2
        const midV = seg.side === 'l' ? d.v + d.r * mid : d.v + d.r * (1 - mid)
        const path = wallPiece(c, d, seg)
        solids.push({
          kind: 'other',
          v: midV,
          paints: [
            { d: path, fill: VOID, fo: dim ? 0.75 : 0.92 },
            { d: path, fill: wallInk, fo: dim ? 0.14 : 0.3, stroke: wallInk, so: dim ? 0.35 : 0.6, sw: 0.5 },
          ],
          lamps: [],
        })
      }
      for (const { g: gate, f } of visible) {
        const gx = X(c, gate.p[0])
        const gy = Y(c, gate.p[1])
        const lampH = Math.max(6, c.S * 1.1)
        solids.push({
          kind: 'other',
          v: f.side === 'l' ? d.v + d.r * f.t : d.v + d.r * (1 - f.t),
          paints: [],
          lamps: gate.lamp ? [{ x: R2(gx), y: R2(gy), h: lampH, r: R2(Math.max(1.6, c.S * 0.3)), rr: R2(Math.max(4, c.S * 0.7)) }] : [],
        })
        // The policy lens lights every gate with its own rule number,
        // whether or not it happens to log -- the traffic lens leaves
        // the wall quiet and says nothing here at all.
        if (policyLens) gateBadges.push({ x: R2(gx), y: R2(gy - lampH - 6), n: gate.ruleCount })
      }
    }

    // Roads, cut into pieces that carry their own depth.
    const dropLabels: { x: number; y: number; text: string; alarm: boolean }[] = []
    const portChipMarks: { x: number; y: number; text: string }[] = []
    const ents = new Map<string, Entity>()
    for (const b of allBuildings) ents.set(b.id, { u: b.u, v: b.v, R: b.R })

    // The bollards, cross and red mark a dropped road ends at (#865) --
    // pulled out so standing on a building (#868) can pin the same mark
    // exactly where a blocked strand's own road would have crossed the
    // wall, not just where the district-pair aggregate already draws
    // one. `e` is the ground point the mark centres on (depth reads its
    // v, same as every other solid).
    function dropMarkAt(e: Pt, alarm: boolean, text: string) {
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
      const w = Math.max(1.2, r.w * c.S * 0.3)
      // The policy lens fades every road so the walls and their gates
      // read as the rules; the traffic lens is the reverse (#865).
      // Standing on a building (#868) fades every road that is not its
      // own the same way -- the two are independent dimmers on the same
      // opacity, never confused with each other.
      const op = (r.k === 'x' ? 0.95 : r.k === 'q' ? 0.42 : 0.52) * (policyLens ? 0.22 : 1) * (own ? 1 : 0.16)
      // While standing, only this building's own roads flow, in the
      // direction its own strand reads; otherwise the ordinary busy/
      // alarm-road flow from before this build.
      const flow = reachOverlay ? own : showLanes && (r.w > 1.4 || r.k === 'x') && !r.lane
      const reversed = !!reachOverlay?.reverseIds.has(r.id)
      let cum = 0
      const pieces = roadPieces(r, ents)
      const glowD: string[] = []
      for (const p of pieces) {
        const q = p.q
        const d = 'M' + P(c, q[0]) + 'C' + P(c, q[1]) + ' ' + P(c, q[2]) + ' ' + P(c, q[3])
        const fade = r.fade ? Math.max(0, 1 - Math.max(0, p.gt - 0.35) * 1.75) : 1
        if (fade > 0) glowD.push(d)
        const paints: Paint[] = [{ d, stroke: col, sw: R2(w), so: R2(op * fade), cls: r.k === 'x' ? 'road-alarm' : undefined }]
        const fl: Paint | null = flow
          ? { d, stroke: col, sw: R2(Math.max(1.1, w * 0.42)), so: R2(0.9 * fade), cls: reversed ? 'flow flow-rev' : 'flow', dash: String(R2(-cum)) }
          : null
        cum += Math.hypot(X(c, q[3][0]) - X(c, q[0][0]), Y(c, q[3][1]) - Y(c, q[0][1]))
        solids.push({ kind: 'piece', v: pieceDepth(p), paints, flow: fl, label: r.label, roadId: r.id })
      }
      if (glowD.length) glows.push({ d: glowD.join(''), stroke: col, sw: R2(w + 4), so: 0.07 })
      if (r.stop === 'drop') dropMarkAt(r.pts[r.pts.length - 1], r.k === 'x', r.refusedBy ? 'caught by ' + r.refusedBy : 'caught, no rule named')
      const chip = reachOverlay?.portChips.find((pc) => pc.roadId === r.id)
      if (chip) {
        const mid = r.pts[Math.floor(r.pts.length / 2)]
        portChipMarks.push({ x: R2(X(c, mid[0])), y: R2(Y(c, mid[1]) - 10), text: chip.text })
      }
    }
    if (reachOverlay) for (const dm2 of reachOverlay.dropMarks) dropMarkAt(dm2.p, false, dm2.refusedBy ? 'caught by ' + dm2.refusedBy : 'caught, no rule named')

    // Buildings on their plinths. The plinth's own height is the
    // current importance reading (#867), never b.h -- the device
    // symbol stamped on top keeps deviceScale's footprint-only size
    // regardless, so importance never redraws what a building looks
    // like, only how tall its base stands.
    const building = (b: Building, d: District | null) => {
      // The same dim styling a dark district already draws its
      // buildings in also carries standing on a building (#868): every
      // building that is neither the standing host nor one it reaches
      // or is reached by fades, reusing one visual word for "not what
      // matters right now" rather than inventing a second.
      const dim = (d?.dark ?? false) || (reachOverlay ? !reachOverlay.litBuildingIds.has(b.id) : false)
      const ink = dim ? 'var(--fg-dim)' : d ? inkOf(d) : 'var(--accent)'
      const R = b.R
      const h = heightOf(b)
      const pin = (hh: number) => diamond(c, b.u, b.v, R, hh)
      const paints: Paint[] = [
        { d: pin(0), fill: '#000', fo: dim ? 0.22 : 0.4 },
        { d: wallFace(c, b.u, b.v, R, h, 'l'), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: wallFace(c, b.u, b.v, R, h, 'l'), fill: ink, fo: dim ? 0.1 : 0.2 },
        { d: wallFace(c, b.u, b.v, R, h, 'r'), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: wallFace(c, b.u, b.v, R, h, 'r'), fill: ink, fo: dim ? 0.16 : 0.34 },
        { d: pin(h), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: pin(h), fill: ink, fo: dim ? 0.12 : 0.26, stroke: ink, so: dim ? 0.55 : 0.95, sw: 1, dash: dim ? '3 3' : undefined },
      ]
      const what = b.kind === 'router' ? 'router' : b.kind === 'router-ant' ? 'router with antennas' : b.kind === 'post' ? 'bridge post' : 'host'
      const aria = b.name + (b.ip ? ' at ' + b.ip : '') + ', ' + what + (d ? ' in ' + d.name : '')
      solids.push({
        kind: 'building',
        v: buildingDepth(b),
        b,
        district: d,
        ink,
        dim,
        paints,
        stamp: { x: R2(X(c, b.u)), y: R2(Y(c, b.v, h)), k: R2((R * 0.74 * c.S) / SREF) },
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

    // Labels claim their rectangle: anything that would land on one
    // already placed is dropped rather than drawn over it.
    const placed: [number, number, number, number][] = []
    const claim = (x: number, y: number, w: number, h: number) => {
      const r: [number, number, number, number] = [x - w / 2, y, x + w / 2, y + h]
      for (const p of placed) if (r[0] < p[2] - 4 && r[2] > p[0] + 4 && r[1] < p[3] - 4 && r[3] > p[1] + 4) return false
      placed.push(r)
      return true
    }
    const plaques: { d: District; x: number; y: number; w: number; ink: string; tally: string }[] = []
    for (const d of g.districts) {
      const x = R2(X(c, d.u))
      const y = R2(Y(c, d.v + d.r) + 5)
      const w = compact ? d.name.length * 7.2 + (!d.rulesPushed ? 78 : d.dark ? 62 : 26) : 200
      if (!claim(x, y, w, compact ? 20 : 38)) continue
      plaques.push({ d, x, y, w: R2(w), ink: inkOf(d), tally: d.buildings.length + d.more + (d.buildings.length + d.more === 1 ? ' host' : ' hosts') })
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

    return { groundPaints, glows, plates, solids: paintOrder(solids), rings, plaques, bridgeChips, gateBadges, dropLabels, portChipMarks, claim }
  })

  /** Names float over buildings at the street stop, for what the
   * viewport shows; the claim is re-run so they never overlap. */
  const toppers = $derived.by(() => {
    if (effectiveStop !== 'street') return []
    const c = geomCam
    const vp = viewport
    const out: { b: Building; x: number; y: number; w: number }[] = []
    const placed: [number, number, number, number][] = []
    for (const b of allBuildings) {
      if (b.u < vp.u0 || b.u > vp.u1 || b.v < vp.v0 || b.v > vp.v1) continue
      const k = (b.R * 0.74 * c.S) / SREF
      const x = R2(X(c, b.u))
      const y = R2(Y(c, b.v, heightOf(b)) - symbolFor(b.kind).top * k - 10)
      const w = Math.max(b.name.length, b.ip.length) * 6.6 + 22
      const r: [number, number, number, number] = [x - w / 2, y - 28, x + w / 2, y + 8]
      if (placed.some((p) => r[0] < p[2] - 4 && r[2] > p[0] + 4 && r[1] < p[3] - 4 && r[3] > p[1] + 4)) continue
      placed.push(r)
      out.push({ b, x, y, w })
    }
    return out
  })

  /* ---------------- the minimap ---------------- */

  const MINI_W = 214
  const MINI_H = 132
  const miniCam = $derived(minimapCam(ground.bounds, MINI_W, MINI_H))
  const mini = $derived.by(() => {
    const mc = miniCam
    const g = ground
    const bank = (a: Pt[]) => a.map((p, i) => (i ? 'L' : 'M') + R2(X(mc, p[0])) + ' ' + R2(Y(mc, p[1]))).join('')
    const river = g.river ? bank(g.river.bankN) + bank(g.river.bankF.slice().reverse()).replace('M', 'L') + 'Z' : ''
    const plates = g.districts.map((d) => ({ d: diamond(mc, d.u, d.v, d.r, 0), ink: inkOf(d), fo: d.dark ? 0.22 : 0.5 }))
    const nodes = g.nodes.filter((n) => n.kind !== 'post').map((n) => ({ x: R2(X(mc, n.u)), y: R2(Y(mc, n.v)) }))
    return { river, plates, nodes }
  })
  const miniView = $derived.by(() => {
    const mc = miniCam
    const vp = viewport
    const x = X(mc, vp.u0)
    const y = Y(mc, vp.v0)
    return { x: R2(x), y: R2(y), w: R2(X(mc, vp.u1) - x), h: R2(Y(mc, vp.v1) - y) }
  })
  const fr = (a: number, lo: number, hi: number) => Math.max(0, Math.min(100, ((a - lo) / (hi - lo || 1)) * 100))
  const bars = $derived.by(() => {
    const b = ground.bounds
    const vp = viewport
    return {
      left: R2(fr(vp.u0, b.u0, b.u1)),
      right: R2(100 - fr(vp.u1, b.u0, b.u1)),
      top: R2(fr(vp.v0, b.v0, b.v1)),
      bottom: R2(100 - fr(vp.v1, b.v0, b.v1)),
    }
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

<div class="city" data-stop={effectiveStop}>
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
            <path d={p.outer} fill={p.ink} fill-opacity={p.d.dark ? 0.045 : 0.1} />
            <path d={p.inner} fill="none" stroke={p.d.dark ? 'var(--fg-dim)' : p.ink} stroke-opacity={p.d.dark ? 0.13 : 0.17} stroke-width="0.7" />
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
          {:else if s.kind === 'other'}
            {#each s.paints as p, j (j)}
              {#if p.d}
                <path d={p.d} fill={p.fill ?? 'none'} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} stroke-linecap={p.cls === 'round' ? 'round' : undefined} />
              {:else}
                <ellipse cx={p.cx} cy={p.cy} rx={p.rx} ry={p.ry} fill={p.fill ?? 'none'} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} />
              {/if}
            {/each}
            {#each s.lamps as l, j (j)}
              <path d="M{l.x} {l.y}V{R2(l.y - l.h)}" stroke="var(--accent)" stroke-opacity="0.6" stroke-width="1" />
              <circle class="lamp" cx={l.x} cy={R2(l.y - l.h)} r={l.r} fill="var(--now)" />
              <circle class="lamp" cx={l.x} cy={R2(l.y - l.h)} r={l.rr} fill="var(--now)" fill-opacity="0.13" />
            {/each}
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
              data-cid={s.b.id}
              data-near={R2(s.b.v + s.b.R)}
              aria-label={s.aria}
              onclick={() => onBuildingClick(s.district?.id ?? null, s.b.id)}
              onkeydown={onKey}
            >
              <title>{s.aria}</title>
              {#if focus?.id === s.b.id}
                <path d={diamond(geomCam, s.b.u, s.b.v, s.b.R * 1.9, 0)} fill="var(--accent)" fill-opacity="0.07" stroke="var(--accent)" stroke-opacity="0.5" stroke-width="1" stroke-dasharray="4 5" />
              {/if}
              {#each s.paints as p, j (j)}
                <path d={p.d} fill={p.fill} fill-opacity={p.fo} stroke={p.stroke} stroke-opacity={p.so} stroke-width={p.sw} stroke-dasharray={p.dash} />
              {/each}
              <g transform="translate({s.stamp.x} {s.stamp.y}) scale({s.stamp.k})" style:color={s.ink} opacity={s.dim ? 0.62 : undefined}>
                {#each symbolFor(s.b.kind).paths as p, j (j)}
                  <path d={p.d} fill={p.fill === 'void' ? VOID : 'currentColor'} fill-opacity={p.fillOpacity} stroke={p.fill === 'body' ? 'currentColor' : undefined} stroke-opacity={p.strokeOpacity} stroke-width={p.strokeWidth} />
                {/each}
              </g>
            </g>
          {/if}
        {/each}
      </g>
      <g class="flat" aria-hidden="true">
        {#each scene.plaques as p (p.d.id)}
          <g transform="translate({p.x} {p.y})">
            {#if compact}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height="20" rx="10" fill="#0a0f1c" fill-opacity="0.9" stroke={p.d.dark ? 'rgba(255,84,112,0.4)' : 'var(--border)'} />
              <circle cx={R2(-p.w / 2 + 11)} cy="10" r="3.2" fill={p.ink} fill-opacity={p.d.dark ? 0.5 : 1} />
              <text x={R2(-p.w / 2 + 19)} y="14" class="p-name small">{p.d.name}</text>
              {#if !p.d.rulesPushed}
                <text x={R2(p.w / 2 - 10)} y="13.5" text-anchor="end" class="cov cov-q">NO RULES</text>
              {:else if p.d.dark}
                <text x={R2(p.w / 2 - 10)} y="13.5" text-anchor="end" class="cov cov-d">DARK</text>
              {/if}
            {:else}
              <rect x={R2(-p.w / 2)} y="0" width={p.w} height="38" rx="8" fill="#0a0f1c" fill-opacity="0.93" stroke={p.d.dark ? 'rgba(255,84,112,0.32)' : 'var(--border)'} />
              <circle cx={R2(-p.w / 2 + 13)} cy="14" r="3.4" fill={p.ink} fill-opacity={p.d.dark ? 0.5 : 1} />
              <text x={R2(-p.w / 2 + 22)} y="18" class="p-name">{p.d.name}</text>
              <text x={R2(p.w / 2 - 11)} y="17.5" text-anchor="end" class="p-cidr">{p.d.cidr ?? 'no address pushed'}</text>
              <text x={R2(-p.w / 2 + 13)} y="31" class="cov {p.d.dark ? 'cov-d' : 'cov-q'}"
                >{p.d.rulesPushed ? (p.d.dark ? 'DARK' : 'LOGGED') : 'NO RULES PUSHED'}</text
              >
              <text x={R2(p.w / 2 - 11)} y="31.5" text-anchor="end" class="wp">{p.tally}</text>
            {/if}
          </g>
        {/each}
        {#each toppers as t (t.b.id)}
          <g transform="translate({t.x} {t.y})">
            <text x="0" y="-13" text-anchor="middle" class="st-name">{t.b.name}</text>
            <text x="0" y="0" text-anchor="middle" class="st-ip">{t.b.ip}</text>
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
        {#each scene.gateBadges as gb, i (i)}
          <g transform="translate({gb.x} {gb.y})">
            <circle r="8" fill="#0a0f1c" fill-opacity="0.92" stroke="var(--accent)" stroke-opacity="0.7" />
            <text x="0" y="3.5" text-anchor="middle" class="gate-n">{gb.n}</text>
          </g>
        {/each}
        {#each scene.dropLabels as dl, i (i)}
          <text x={dl.x} y={dl.y} text-anchor="middle" class="drop-t" class:alarm-t={dl.alarm}>{dl.text}</text>
        {/each}
        {#each scene.portChipMarks as pc, i (i)}
          <!-- The ports an accepted own road carries, while standing on
               a building (#868) -- the design record's "with the ports
               on the road". -->
          <g transform="translate({pc.x} {pc.y})">
            <rect x={R2(-pc.text.length * 3.1 - 8)} y="-8.5" width={R2(pc.text.length * 6.2 + 16)} height="17" rx="8" fill="#080c16" fill-opacity="0.92" stroke="var(--hair)" stroke-opacity="0.75" />
            <text x="0" y="3.5" text-anchor="middle" class="port-t">{pc.text}</text>
          </g>
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

  {#if stop === 'city'}
    <!-- Height = importance (#867): which reading sets a building's
         plinth height, at the one stop that shows the whole skyline.
         The button's own text states the current reading outright,
         never only a pressed style, so it reads the same to a screen
         reader as it does by eye. -->
    <div class="importance">
      <button
        type="button"
        class="reading"
        aria-pressed={cityImportanceState.reading === 'watched'}
        aria-label="Plinth height reads {importanceLabel(cityImportanceState.reading)}. Activate to switch to {otherReading}."
        onclick={() => cityImportanceState.toggle()}
      >
        height: {importanceLabel(cityImportanceState.reading)}
      </button>
      {#if importanceNotice}
        <p class="notice">{importanceNotice}</p>
      {/if}
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
      {#each mini.nodes as n, i (i)}
        <circle cx={n.x} cy={n.y} r="2" fill="var(--accent)" />
      {/each}
      <rect class="viewport" x={miniView.x} y={miniView.y} width={miniView.w} height={miniView.h} fill="rgba(157,184,232,0.1)" stroke="var(--accent)" stroke-opacity="0.8" stroke-width="1.2" />
      </svg>
    </button>
    <div class="mk"><span>viewport ≈ {viewShare}%</span><span>drag · arrows to walk</span></div>
  </div>
  <div class="sbar h" aria-hidden="true"><i style:left="{bars.left}%" style:right="{bars.right}%"></i></div>
  <div class="sbar v" aria-hidden="true"><i style:top="{bars.top}%" style:bottom="{bars.bottom}%"></i></div>
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

  .cov {
    font: 700 8.5px var(--font-mono);
    letter-spacing: 0.1em;
  }

  .cov-q {
    fill: var(--fg-dim);
  }

  .cov-d {
    fill: var(--alarm);
  }

  .wp {
    font: 9.5px var(--font-mono);
    fill: var(--fg-muted);
  }

  .chip-t {
    font: 10.5px var(--font-mono);
    fill: var(--fg-muted);
  }

  /* The ports on a road while standing on a building (#868). */
  .port-t {
    font: 600 9.5px var(--font-mono);
    fill: var(--fg);
  }

  .gate-n {
    font: 700 9px var(--font-mono);
    fill: var(--accent);
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
    right: 20px;
    top: 58px;
    width: 232px;
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
    white-space: pre;
    overflow-x: auto;
    margin: 0;
  }

  .composer .cm-f {
    font: 9.5px var(--font-mono);
    color: var(--fg-dim);
    margin-top: 7px;
    letter-spacing: 0.06em;
  }

  .importance {
    position: absolute;
    z-index: 8;
    left: 20px;
    top: 58px;
    max-width: 220px;
    padding: 8px 9px;
    background: var(--glass);
    border: 1px solid var(--hair-2);
    border-radius: 9px;
    backdrop-filter: blur(7px);
    box-sizing: border-box;
  }

  .importance .reading {
    font: 600 11px var(--font-mono);
    letter-spacing: 0.02em;
    color: var(--fg);
    background: rgba(157, 184, 232, 0.08);
    border: 1px solid var(--hair-2);
    border-radius: 6px;
    padding: 5px 9px;
    cursor: pointer;
  }

  .importance .reading[aria-pressed='true'] {
    border-color: var(--accent);
    color: var(--accent);
  }

  .importance .reading:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 2px;
  }

  .importance .notice {
    font: 9px var(--font-mono);
    color: var(--fg-dim);
    margin: 6px 0 0;
  }

  .sbar {
    position: absolute;
    z-index: 7;
    background: rgba(160, 185, 230, 0.06);
    border-radius: 3px;
  }

  .sbar.h {
    left: 16px;
    right: 16px;
    bottom: 5px;
    height: 5px;
  }

  .sbar.v {
    top: 66px;
    bottom: 66px;
    right: 6px;
    width: 5px;
  }

  .sbar i {
    position: absolute;
    background: rgba(157, 184, 232, 0.5);
    border-radius: 3px;
    display: block;
  }

  .sbar.h i {
    top: 0;
    bottom: 0;
  }

  .sbar.v i {
    left: 0;
    right: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .flow,
    .lamp,
    .ripple {
      animation: none;
    }

    .flow {
      stroke-dasharray: none;
    }
  }
</style>
