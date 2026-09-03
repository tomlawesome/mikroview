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
  // replaces the plain blocks). Enter and Escape are left alone for the
  // reach (#868). Rule gates and walls are #866's; importance is #867's.
  import { tick, untrack } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { tunnelsState } from '../lib/tunnels.svelte'
  import { policyState } from '../lib/policy.svelte'
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
    viewportRect,
    wallFace,
    type BoxFaces,
    type Cam,
    type Pt,
    type Stop,
  } from '../lib/city/project'
  import { lerpP, roadPieces, segsOf, type Entity } from '../lib/city/roads'
  import { riverScene } from '../lib/city/river'
  import { P, type Paint } from '../lib/city/paint'
  import { bridgeStateLabel } from '../lib/city/tunnelState'
  import { deviceKindFor } from '../lib/city/deviceKind'
  import { deviceScale, deviceStampAttrs, type DeviceStampAttrs } from '../lib/city/devices'
  import { faceOf, wallPiece, wallSegments, type WallBreak } from '../lib/city/walls'
  import { entitiesState } from '../lib/entities.svelte'
  import CityDeviceDefs from './CityDeviceDefs.svelte'
  import type { Building, CityLens, CityPeer, District, DistrictGate, Ground, RoadKind } from '../lib/city/types'

  let { stop, ground: groundProp, lens = 'traffic' }: { stop: Stop; ground?: Ground; lens?: CityLens } = $props()

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

  // Geometry is built at the stop's own height with the camera at the
  // origin; S and centre are what the viewer sees, and the group's
  // transform reconciles the two.
  const Sgeom = $derived(STOP_HEIGHT[stop])
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

  const reducedMotion = () => typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches

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
      const to = centreFor(s, focus)
      if (!started) {
        started = true
        S = STOP_HEIGHT[s]
        centre = clampCentre(to, g.bounds)
        return
      }
      moveCamera(STOP_HEIGHT[s], to)
    })
  })

  $effect(() => () => {
    if (anim !== null) cancelAnimationFrame(anim)
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

  const cid = (f: Focus) => (f ? f.id : null)

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
    | { kind: 'piece'; v: number; paints: Paint[]; flow: Paint | null; label: string }
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

  const showLanes = $derived(stop === 'district' || stop === 'street')
  const showBoroughs = $derived(stop === 'city' || stop === 'borough')
  const compact = $derived(stop === 'city')

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
    const ents = new Map<string, Entity>()
    for (const b of allBuildings) ents.set(b.id, { u: b.u, v: b.v, R: b.R })
    for (const r of g.roads) {
      if (r.lane && !showLanes) continue
      const col = VERDICT[r.k]
      const w = Math.max(1.2, r.w * c.S * 0.3)
      // The policy lens fades every road so the walls and their gates
      // read as the rules; the traffic lens is the reverse (#865).
      const op = (r.k === 'x' ? 0.95 : r.k === 'q' ? 0.42 : 0.52) * (policyLens ? 0.22 : 1)
      const flow = showLanes && (r.w > 1.4 || r.k === 'x') && !r.lane
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
          ? { d, stroke: col, sw: R2(Math.max(1.1, w * 0.42)), so: R2(0.9 * fade), cls: 'flow', dash: String(R2(-cum)) }
          : null
        cum += Math.hypot(X(c, q[3][0]) - X(c, q[0][0]), Y(c, q[3][1]) - Y(c, q[0][1]))
        solids.push({ kind: 'piece', v: pieceDepth(p), paints, flow: fl, label: r.label })
      }
      if (glowD.length) glows.push({ d: glowD.join(''), stroke: col, sw: R2(w + 4), so: 0.07 })
      if (r.stop === 'drop') {
        const e = r.pts[r.pts.length - 1]
        const col2 = r.k === 'x' ? 'var(--alarm)' : 'var(--drop)'
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
        dropLabels.push({ x: mx, y: my - 14 * k, text: r.refusedBy ? 'caught by ' + r.refusedBy : 'caught, no rule named', alarm: r.k === 'x' })
      }
    }

    // Buildings on their plinths.
    const building = (b: Building, d: District | null) => {
      const dim = d?.dark ?? false
      const ink = dim ? 'var(--fg-dim)' : d ? inkOf(d) : 'var(--accent)'
      const R = b.R
      const pin = (hh: number) => diamond(c, b.u, b.v, R, hh)
      const paints: Paint[] = [
        { d: pin(0), fill: '#000', fo: dim ? 0.22 : 0.4 },
        { d: wallFace(c, b.u, b.v, R, b.h, 'l'), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: wallFace(c, b.u, b.v, R, b.h, 'l'), fill: ink, fo: dim ? 0.1 : 0.2 },
        { d: wallFace(c, b.u, b.v, R, b.h, 'r'), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: wallFace(c, b.u, b.v, R, b.h, 'r'), fill: ink, fo: dim ? 0.16 : 0.34 },
        { d: pin(b.h), fill: '#0a0f1c', fo: dim ? 0.7 : 0.94 },
        { d: pin(b.h), fill: ink, fo: dim ? 0.12 : 0.26, stroke: ink, so: dim ? 0.55 : 0.95, sw: 1, dash: dim ? '3 3' : undefined },
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
        stamp: { x: R2(X(c, b.u)), y: R2(Y(c, b.v, b.h)), k: R2((R * 0.74 * c.S) / SREF) },
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

    return { groundPaints, glows, plates, solids: paintOrder(solids), rings, plaques, bridgeChips, gateBadges, dropLabels, claim }
  })

  /** Names float over buildings at the street stop, for what the
   * viewport shows; the claim is re-run so they never overlap. */
  const toppers = $derived.by(() => {
    if (stop !== 'street') return []
    const c = geomCam
    const vp = viewport
    const out: { b: Building; x: number; y: number; w: number }[] = []
    const placed: [number, number, number, number][] = []
    for (const b of allBuildings) {
      if (b.u < vp.u0 || b.u > vp.u1 || b.v < vp.v0 || b.v > vp.v1) continue
      const k = (b.R * 0.74 * c.S) / SREF
      const x = R2(X(c, b.u))
      const y = R2(Y(c, b.v, b.h) - symbolFor(b.kind).top * k - 10)
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

<div class="city" data-stop={stop}>
  <svg
    bind:this={svgEl}
    viewBox="0 0 {STAGE_W} {STAGE_H}"
    preserveAspectRatio="xMidYMid meet"
    role="application"
    aria-label="The estate as a city at the {stop} stop: drag or hold Shift with the arrow keys to pan; arrow keys walk the districts and their buildings"
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
              <path d={p.d} fill="none" stroke={p.stroke} stroke-width={p.sw} stroke-opacity={p.so} class={p.cls} data-v={R2(s.v)} />
            {/each}
            {#if s.flow}
              <path d={s.flow.d} fill="none" stroke={s.flow.stroke} stroke-width={s.flow.sw} stroke-opacity={s.flow.so} class="flow" stroke-dashoffset={s.flow.dash} />
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
              onclick={() => onItemClick({ districtId: s.district?.id ?? null, id: s.b.id })}
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
      </g>
    </g>
  </svg>

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
