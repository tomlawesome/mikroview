// SPDX-License-Identifier: AGPL-3.0-only
//
// The city's device library (#864) -- one isometric symbol per device
// type, all with the same VLAN-tinted three-face shading so the district
// still reads. Ported from round 40's mockup
// (docs/design/concepts/round-40/isometric.html, "THE DEVICE LIBRARY"),
// which is reference for execution quality; the ratified record is
// docs/design/screens/city/DESIGN.md and wins where the two differ.
//
// How a symbol is drawn, and why it needs no transform of its own:
// each symbol is drawn ONCE in local pixels for a device whose
// footprint radius is 1 at S = SREF, using the same projection
// constants as the map. Local px: 1 ground u = IK*SREF across, 1 ground
// v = VK*SREF down, 1 unit of height = ZK*SREF up. The renderer stamps
// it with <use> and scales it by R*S/SREF, so it lands in the isometric
// already correct.
//
// Colour: the body is `currentColor` -- the district's ink -- so one
// symbol serves every VLAN. Port lights, status lights and screens read
// `var(--sig)`, which the stamp sets: the alarm ink on a flagged
// device, otherwise the resting signal ink. Nothing in this file looks
// data up; "flagged" arrives as an option on the stamp and leaves as a
// custom property.
//
// A symbol is a list of DevicePart, not a string of SVG. The mockup
// concatenates markup and the obvious port would too, but this frontend
// bans every API that renders a string as live markup
// (guards/injection-sinks.test.ts, and docs/decisions/injection-audit.md
// records the absence of that sink as what makes XSS inapplicable here
// rather than merely mitigated). Parts render as ordinary Svelte
// elements, so the ban holds, and the shapes stay inspectable from a
// test besides. CityDeviceDefs.svelte turns them into the <defs> a
// <use> points at.
//
// Two shapes deliberately depart from the mockup, and one from both the
// mockup and its own dimensions. The owner's verdict on the first cut
// was "I don't like every single thing being a cube", and the mockup's
// laptop and TV still read as slabs at street zoom (#864): the laptop's
// lid is now a real prism with thickness, a bezel and a keyboard well,
// and the TV is a screen lifted clear of the ground on a narrow neck.
// The phone is drawn upright, the word the record uses, because the
// mockup's is wider than it is tall and reads as a crate.
//
// What this file does NOT do: place symbols, choose a scale from a
// camera, or turn a camera's lens toward its road. Stamping into the
// live city is the ground model's job (#863); the lens facing needs
// per-box face culling at stamp time and is not in this slice.

/** Projection constants, shared with the city's ground model (#863). */
export const IK = 1.02
export const VK = 0.5
export const ZK = 0.72
export const SREF = 10

/** Local-pixel scale of one ground unit in each axis. */
const LU = IK * SREF
const LV = VK * SREF
const LZ = ZK * SREF

/**
 * The void behind every face. A device is a solid object, never
 * coloured glass over the town behind it, so each face is painted on an
 * opaque backing first.
 */
const VOID = 'var(--bg, #06080e)'

/** The resting signal ink, used when a stamp does not name one. */
export const SIG_REST = 'var(--accept, #3ecf7e)'

/** The ink a flagged device's light or lens glows in. */
export const SIG_ALARM = 'var(--alarm, #ff5470)'

/** The pale ink a lit screen falls back to when no --sig is set. */
const SCREEN = 'var(--sig, #cfe0ff)'

/** One drawn piece of a symbol. Rendered by CityDeviceDefs.svelte. */
export interface DevicePart {
  shape: 'path' | 'circle' | 'ellipse'
  /** Path data, for shape 'path'. */
  d?: string
  cx?: number
  cy?: number
  r?: number
  rx?: number
  ry?: number
  fill: string
  fillOpacity?: number
  stroke?: string
  strokeOpacity?: number
  strokeWidth?: number
  strokeLinecap?: 'round'
}

const r2 = (n: number): number => Math.round(n * 10) / 10

/** A point in local pixels, from ground (u, v) and height z. */
const lp = (u: number, v: number, z = 0): string => `${r2(u * LU)} ${r2(v * LV - z * LZ)}`
const px = (u: number): number => r2(u * LU)
const py = (v: number, z = 0): number => r2(v * LV - z * LZ)

type Pt = [number, number]

/**
 * The four ground corners of a box on the diagonal axes: half-extent A
 * along d1 (screen: right and down), B along d2 (right and up).
 *
 * L is the left corner, F the front (nearest the camera), R the right,
 * K the back. The two faces the camera sees are L->F (front-left) and
 * F->R (front-right).
 */
function boxPts(u: number, v: number, A: number, B: number): Record<'L' | 'F' | 'R' | 'K', Pt> {
  return {
    L: [u - A - B, v - A + B],
    F: [u + A - B, v + A + B],
    R: [u + A + B, v + A - B],
    K: [u - A + B, v - A - B],
  }
}

interface LBox {
  top: string
  right: string
  left: string
  p: Record<'L' | 'F' | 'R' | 'K', Pt>
  c: Pt
  z0: number
  zt: number
}

/** A box in local pixels: base z0, height h, centred on (ou, ov). */
function lbox(A: number, B: number, z0: number, h: number, ou = 0, ov = 0): LBox {
  const p = boxPts(ou, ov, A, B)
  const zt = z0 + h
  const P = (k: keyof typeof p, z: number): string => lp(p[k][0], p[k][1], z)
  return {
    top: `M${P('K', zt)}L${P('R', zt)}L${P('F', zt)}L${P('L', zt)}Z`,
    right: `M${P('F', zt)}L${P('R', zt)}L${P('R', z0)}L${P('F', z0)}Z`,
    left: `M${P('L', zt)}L${P('F', zt)}L${P('F', z0)}L${P('L', z0)}Z`,
    p,
    c: [ou, ov],
    z0,
    zt,
  }
}

/** One tinted face: an opaque backing, then the district's ink over it. */
function solidFace(d: string, opacity: number, strokeOpacity: number, strokeWidth: number): DevicePart[] {
  return [
    { shape: 'path', d, fill: VOID },
    {
      shape: 'path',
      d,
      fill: 'currentColor',
      fillOpacity: opacity,
      stroke: 'currentColor',
      strokeOpacity,
      strokeWidth,
    },
  ]
}

interface FaceOpts {
  /** front-left face opacity */
  l?: number
  /** front-right face opacity */
  r?: number
  /** top face opacity */
  t?: number
}

/** The three visible faces of a box, VLAN-tinted, back to front. */
function lfaces(b: LBox, o: FaceOpts = {}): DevicePart[] {
  return [
    ...solidFace(b.left, o.l ?? 0.38, 0.55, 0.35),
    ...solidFace(b.right, o.r ?? 0.72, 0.55, 0.35),
    ...solidFace(b.top, o.t ?? 0.95, 0.9, 0.45),
  ]
}

/**
 * A rectangle inset into one of a box's two visible side faces, given
 * as fractions along the face (f) and up it (k). Screens and bezels are
 * built from this, so a lit screen sits inside a frame instead of
 * flooding a whole face.
 */
function facePatch(b: LBox, side: 'l' | 'r', f0: number, f1: number, k0: number, k1: number): string {
  const [a, c] = side === 'r' ? [b.p.F, b.p.R] : [b.p.L, b.p.F]
  const at = (f: number, k: number): string =>
    lp(a[0] + (c[0] - a[0]) * f, a[1] + (c[1] - a[1]) * f, b.z0 + (b.zt - b.z0) * k)
  return `M${at(f0, k0)}L${at(f1, k0)}L${at(f1, k1)}L${at(f0, k1)}Z`
}

/** A rectangle inset into a box's top face, shrunk toward its centre. */
function topPatch(b: LBox, m: number): string {
  const [cu, cv] = b.c
  const at = (k: keyof LBox['p']): string =>
    lp(cu + (b.p[k][0] - cu) * m, cv + (b.p[k][1] - cv) * m, b.zt)
  return `M${at('K')}L${at('R')}L${at('F')}L${at('L')}Z`
}

/** A lit screen, in whatever ink the stamp put in --sig. */
const litScreen = (d: string, opacity: number): DevicePart => ({
  shape: 'path',
  d,
  fill: SCREEN,
  fillOpacity: opacity,
})

/** Port and status lights strung along a box's front-right face. */
function lLights(b: LBox, n: number, zf: number, rad: number, a: number, z: number): DevicePart[] {
  const out: DevicePart[] = []
  for (let i = 0; i < n; i++) {
    const f = a + (z - a) * (n === 1 ? 0.5 : i / (n - 1))
    const u = b.p.F[0] + (b.p.R[0] - b.p.F[0]) * f
    const v = b.p.F[1] + (b.p.R[1] - b.p.F[1]) * f
    out.push({
      shape: 'circle',
      cx: px(u),
      cy: py(v, b.z0 + (b.zt - b.z0) * zf),
      r: rad,
      fill: `var(--sig, ${SIG_REST})`,
      fillOpacity: 0.9,
    })
  }
  return out
}

/** Drive-bay slits across a box's front-right face. */
function lSlits(b: LBox, n: number, rad: number): DevicePart[] {
  const out: DevicePart[] = []
  for (let i = 0; i < n; i++) {
    const zf = 0.2 + 0.6 * (i / (n - 1))
    const z = b.z0 + (b.zt - b.z0) * zf
    const pt = (f: number): string =>
      lp(b.p.F[0] + (b.p.R[0] - b.p.F[0]) * f, b.p.F[1] + (b.p.R[1] - b.p.F[1]) * f, z)
    out.push({
      shape: 'path',
      d: `M${pt(0.16)}L${pt(0.84)}`,
      fill: 'none',
      stroke: '#06080e',
      strokeOpacity: 0.55,
      strokeWidth: rad,
    })
  }
  return out
}

/** A low disc (the IoT puck), bezier half-ellipses so it never reads as a cube. */
function ldisc(rad: number, z0: number, h: number, o: { r?: number } = {}): DevicePart[] {
  const rx = r2(rad * LU)
  const ry = r2(rad * LV)
  const yT = r2(-(z0 + h) * LZ)
  const yB = r2(-z0 * LZ)
  const half = (y: number): string => `C${-rx} ${r2(y + ry * 1.34)} ${rx} ${r2(y + ry * 1.34)} ${rx} ${y}`
  const side =
    `M${-rx} ${yT}${half(yT)}L${rx} ${yB}C${rx} ${r2(yB + ry * 1.34)} ` +
    `${-rx} ${r2(yB + ry * 1.34)} ${-rx} ${yB}Z`
  return [
    { shape: 'path', d: side, fill: VOID },
    { shape: 'path', d: side, fill: 'currentColor', fillOpacity: o.r ?? 0.6 },
    { shape: 'ellipse', cx: 0, cy: yT, rx, ry, fill: VOID },
    {
      shape: 'ellipse',
      cx: 0,
      cy: yT,
      rx,
      ry,
      fill: 'currentColor',
      fillOpacity: 0.93,
      stroke: 'currentColor',
      strokeOpacity: 0.9,
      strokeWidth: 0.45,
    },
  ]
}

/** A quad from four (u, v, z) points, on the void. */
type P3 = [number, number, number]
function quad(pts: [P3, P3, P3, P3], fill: string, opacity: number, stroke?: number): DevicePart[] {
  const d = `M${lp(...pts[0])}L${lp(...pts[1])}L${lp(...pts[2])}L${lp(...pts[3])}Z`
  return [
    { shape: 'path', d, fill: VOID },
    {
      shape: 'path',
      d,
      fill,
      fillOpacity: opacity,
      ...(stroke === undefined
        ? {}
        : { stroke: 'currentColor', strokeOpacity: stroke, strokeWidth: 0.4 }),
    },
  ]
}

/** Shrink a quad toward its centroid -- how a screen sits inside its bezel. */
function quadInset(pts: [P3, P3, P3, P3], m: number): [P3, P3, P3, P3] {
  const c: P3 = [
    (pts[0][0] + pts[1][0] + pts[2][0] + pts[3][0]) / 4,
    (pts[0][1] + pts[1][1] + pts[2][1] + pts[3][1]) / 4,
    (pts[0][2] + pts[1][2] + pts[2][2] + pts[3][2]) / 4,
  ]
  return pts.map((p) => [
    c[0] + (p[0] - c[0]) * m,
    c[1] + (p[1] - c[1]) * m,
    c[2] + (p[2] - c[2]) * m,
  ]) as [P3, P3, P3, P3]
}

/**
 * The eleven device types. `router-ant` is the downstream/secondary
 * router; `post` is the gateway bollard used for interfaces and tunnel
 * ends; `puck` is both the IoT shape and the fallback for a device
 * whose type is not known.
 */
export const DEVICE_KINDS = [
  'router',
  'router-ant',
  'switch',
  'server',
  'workstation',
  'laptop',
  'phone',
  'tv',
  'camera',
  'puck',
  'post',
] as const

export type DeviceKind = (typeof DEVICE_KINDS)[number]

export interface DeviceSymbol {
  /** How tall the symbol stands in local px -- where a label clears it. */
  top: number
  /** Its pieces, in paint order, back to front. */
  parts: DevicePart[]
}

/** What a symbol is called out loud, for accessible names. */
export const DEVICE_KIND_LABEL: Record<DeviceKind, string> = {
  router: 'router',
  'router-ant': 'router with antennas',
  switch: 'switch',
  server: 'server',
  workstation: 'workstation',
  laptop: 'laptop',
  phone: 'phone',
  tv: 'TV',
  camera: 'camera',
  puck: 'device',
  post: 'gateway post',
}

function buildLibrary(): Record<DeviceKind, DeviceSymbol> {
  const lib = {} as Record<DeviceKind, DeviceSymbol>
  const dev = (id: DeviceKind, top: number, parts: DevicePart[]): void => {
    lib[id] = { top, parts }
  }

  // ROUTER -- flat wide chassis, a row of port lights on the front face.
  const chassis = lbox(0.5, 0.86, 0, 0.34)
  const routerBody: DevicePart[] = [
    ...lfaces(chassis, { t: 0.95, r: 0.74, l: 0.4 }),
    ...lLights(chassis, 6, 0.45, 1.5, 0.16, 0.84),
    {
      shape: 'path',
      d: `M${lp(-0.5 - 0.86, -0.5 + 0.86, 0.34)}L${lp(-0.5 + 0.86, -0.5 - 0.86, 0.34)}`,
      fill: 'none',
      stroke: 'currentColor',
      strokeOpacity: 0.55,
      strokeWidth: 0.8,
    },
  ]
  dev('router', 0.34 * LZ + 4, routerBody)

  // ROUTER WITH ANTENNAS -- two short antennas off the back corners.
  const ant = (ou: number, ov: number): DevicePart[] => [
    {
      shape: 'path',
      d: `M${lp(ou, ov, 0.34)}L${lp(ou, ov, 0.95)}`,
      fill: 'none',
      stroke: 'currentColor',
      strokeOpacity: 0.8,
      strokeWidth: 1.5,
      strokeLinecap: 'round',
    },
    { shape: 'circle', cx: px(ou), cy: py(ov, 0.99), r: 1.5, fill: 'currentColor', fillOpacity: 0.85 },
  ]
  dev('router-ant', 1.0 * LZ + 4, [...routerBody, ...ant(-0.86, -0.16), ...ant(0.16, -0.86)])

  // SWITCH -- the same chassis, longer and thinner, many port lights.
  const sw = lbox(0.3, 1.0, 0, 0.24)
  dev('switch', 0.24 * LZ + 4, [
    ...lfaces(sw, { t: 0.95, r: 0.74, l: 0.4 }),
    ...lLights(sw, 10, 0.5, 1.2, 0.1, 0.9),
  ])

  // SERVER BOX -- tall upright box, drive bays, one status light.
  const sb = lbox(0.45, 0.5, 0, 1.05)
  dev('server', 1.05 * LZ + 5, [
    ...lfaces(sb),
    ...lSlits(sb, 4, 1.5),
    {
      shape: 'circle',
      cx: px(sb.p.F[0] + (sb.p.R[0] - sb.p.F[0]) * 0.82),
      cy: py(sb.p.F[1] + (sb.p.R[1] - sb.p.F[1]) * 0.82, 0.95),
      r: 1.7,
      fill: `var(--sig, ${SIG_REST})`,
    },
  ])

  // WORKSTATION -- a tower beside a monitor on a raised stand. The
  // monitor's screen is inset in a bezel and the stand leaves a gap:
  // two masses, one small screen, which is what tells it apart from the
  // TV's single wide one.
  const tower = lbox(0.26, 0.22, 0, 0.78, -0.32, -0.32)
  const stand = lbox(0.07, 0.12, 0, 0.2, 0.26, 0.26)
  const mon = lbox(0.06, 0.44, 0.2, 0.5, 0.26, 0.26)
  dev('workstation', 0.82 * LZ + 6, [
    ...lfaces(tower),
    ...lLights(tower, 2, 0.75, 1.2, 0.3, 0.7),
    ...lfaces(stand, { t: 0.6, r: 0.5, l: 0.3 }),
    ...lfaces(mon, { t: 0.8, r: 0.34, l: 0.22 }),
    litScreen(facePatch(mon, 'r', 0.1, 0.9, 0.14, 0.88), 0.55),
  ])

  // LAPTOP -- a wedge, not a slab. A thin base with a keyboard well, and
  // a lid that is a real prism: a lit screen in a bezel, a bright top
  // edge and a dim end face, hinged on the back-left edge and leaning
  // away from the camera.
  const base = lbox(0.32, 0.54, 0, 0.075)
  const zTop = 0.075
  const lean = 0.17
  const lidH = 0.54
  // The lid's plane is spanned by the hinge (L->K) and its up-vector, so
  // its outward normal is (-1, -1, -2*lean/lidH) normalised: the screen
  // faces front-right, and the lid's thickness runs along that normal.
  const nl = Math.hypot(1, 1, (2 * lean) / lidH)
  const tk = 0.055
  const off: P3 = [-tk / nl, -tk / nl, (-tk / nl) * ((2 * lean) / lidH)]
  const hL = base.p.L
  const hK = base.p.K
  const face: [P3, P3, P3, P3] = [
    [hL[0], hL[1], zTop],
    [hK[0], hK[1], zTop],
    [hK[0] - lean, hK[1] - lean, zTop + lidH],
    [hL[0] - lean, hL[1] - lean, zTop + lidH],
  ]
  const backOf = (p: P3): P3 => [p[0] + off[0], p[1] + off[1], p[2] + off[2]]
  const screen = quadInset(face, 0.8)
  dev('laptop', (zTop + lidH) * LZ + 5, [
    ...lfaces(base, { t: 0.88, r: 0.58, l: 0.34 }),
    { shape: 'path', d: topPatch(base, 0.72), fill: '#06080e', fillOpacity: 0.5 },
    // the lid's far end and its top edge, behind the screen face
    ...quad([face[0], face[3], backOf(face[3]), backOf(face[0])], 'currentColor', 0.34, 0.5),
    ...quad([face[3], face[2], backOf(face[2]), backOf(face[3])], 'currentColor', 0.9, 0.7),
    // the screen face: bezel, then the lit panel inside it
    ...quad(face, 'currentColor', 0.55, 0.85),
    litScreen(`M${lp(...screen[0])}L${lp(...screen[1])}L${lp(...screen[2])}L${lp(...screen[3])}Z`, 0.62),
  ])

  // PHONE -- a thin upright slab, screen lit inside a bezel. Upright is
  // the word the record uses; the mockup's phone is in fact wider than
  // it is tall, and at city zoom that reads as a crate, so this one is
  // narrowed and stood up until its silhouette is a portrait slab.
  const ph = lbox(0.05, 0.17, 0.03, 0.86)
  dev('phone', 0.89 * LZ + 5, [
    ...lfaces(ph, { t: 0.85, r: 0.42, l: 0.26 }),
    litScreen(facePatch(ph, 'r', 0.14, 0.86, 0.06, 0.94), 0.68),
  ])

  // TV -- a screen lifted clear of the ground on a narrow neck over a
  // wide foot. The gap under the panel is the whole point: the mockup's
  // TV is a long low slab that reads as a girder at street, so this one
  // is roughly two-to-one and stands well above its own foot, and the
  // silhouette becomes a screen on a stand.
  const foot = lbox(0.1, 0.3, 0, 0.045)
  const neck = lbox(0.055, 0.075, 0.045, 0.28)
  const panel = lbox(0.05, 0.52, 0.32, 0.8)
  dev('tv', 1.12 * LZ + 5, [
    ...lfaces(foot, { t: 0.62, r: 0.5, l: 0.3 }),
    ...lfaces(neck, { t: 0.7, r: 0.55, l: 0.34 }),
    ...lfaces(panel, { t: 0.85, r: 0.26, l: 0.18 }),
    litScreen(facePatch(panel, 'r', 0.06, 0.94, 0.09, 0.91), 0.55),
  ])

  // PoE CAMERA -- a bullet body on a short post, lens facing the road.
  // The facing here is fixed toward the camera; turning it toward a
  // building's own road is the stamping side's job (#863).
  const cpost = lbox(0.09, 0.09, 0, 0.86, -0.24, -0.24)
  const body = lbox(0.44, 0.19, 0.72, 0.3, 0.16, 0.16)
  const arm = lbox(0.2, 0.05, 0.8, 0.06, -0.06, -0.06)
  const lensU = px(body.p.F[0] + (body.p.R[0] - body.p.F[0]) * 0.5 + 0.12)
  const lensV = py(body.p.F[1] + (body.p.R[1] - body.p.F[1]) * 0.5 + 0.12, 0.86)
  dev('camera', 1.02 * LZ + 5, [
    ...lfaces(cpost, { t: 0.6, r: 0.5, l: 0.34 }),
    ...lfaces(arm, { t: 0.5, r: 0.45, l: 0.3 }),
    ...lfaces(body),
    {
      shape: 'ellipse',
      cx: lensU,
      cy: lensV,
      rx: 4.2,
      ry: 3.4,
      fill: '#06080e',
      stroke: 'currentColor',
      strokeOpacity: 0.9,
      strokeWidth: 1,
    },
    { shape: 'ellipse', cx: lensU, cy: lensV, rx: 2.1, ry: 1.7, fill: SCREEN, fillOpacity: 0.85 },
  ])

  // IoT PUCK -- a low flat disc with one light. Also the fallback shape.
  dev('puck', 0.2 * LZ + 4, [
    ...ldisc(0.62, 0, 0.2),
    { shape: 'circle', cx: 0, cy: py(0, 0.2), r: 1.5, fill: SCREEN, fillOpacity: 0.95 },
  ])

  // GATEWAY POST -- a bollard with a light, for ether1 / wg0 / l2tp.
  const bol = lbox(0.28, 0.28, 0, 1.0)
  const cap = lbox(0.36, 0.36, 1.0, 0.1)
  dev('post', 1.35 * LZ + 6, [
    ...lfaces(bol, { t: 0.8, r: 0.62, l: 0.4 }),
    ...lfaces(cap, { t: 0.85, r: 0.6, l: 0.38 }),
    { shape: 'circle', cx: 0, cy: py(0, 1.28), r: 4, fill: 'var(--sig, #9db8e8)', fillOpacity: 0.95 },
    {
      shape: 'circle',
      cx: 0,
      cy: py(0, 1.28),
      r: 7.5,
      fill: 'none',
      stroke: 'var(--sig, #9db8e8)',
      strokeOpacity: 0.3,
      strokeWidth: 1,
    },
  ])

  return lib
}

/** The library itself: every symbol, drawn once in local pixels. */
export const DEVICE_LIBRARY: Record<DeviceKind, DeviceSymbol> = buildLibrary()

/** How tall a symbol stands in local px, for placing a label above it. */
export function deviceTop(kind: DeviceKind): number {
  return DEVICE_LIBRARY[kind].top
}

/**
 * The id a stamped <use> points at. Every city SVG emits its own copy
 * of the library under its own prefix, so no <use> ever has to reach
 * across SVG roots -- which does not work.
 */
export function deviceSymbolId(prefix: string, kind: DeviceKind): string {
  return `${prefix}-${kind}`
}

/**
 * The scale a symbol is stamped at: it was drawn for footprint radius 1
 * at S = SREF, and 0.74 keeps a device comfortably inside the plinth it
 * stands on. The device itself never scales with importance (#867) --
 * only R (the footprint) and S (the camera) move it.
 */
export function deviceScale(footprintRadius: number, cameraScale: number): number {
  return (footprintRadius * 0.74 * cameraScale) / SREF
}

export interface DeviceStampOptions {
  /** The district's ink; the body is drawn in it. */
  ink?: string
  /**
   * Whether this device carries an open flag. Purely a stamp-time
   * attribute: it only switches --sig to the alarm ink, and no lookup
   * of any kind happens inside the library.
   */
  flagged?: boolean
  /** An explicit signal ink, overriding the flagged/resting default. */
  sig?: string
  /** Local-px scale, normally from deviceScale(). */
  scale?: number
  /** Where the device's ground point lands, in the scene's pixels. */
  x?: number
  y?: number
}

export interface DeviceStampAttrs {
  href: string
  transform: string
  style: string
}

/**
 * The attributes of one stamped device, to spread onto a <use>. The ink
 * and the signal ink ride in as custom properties, so the same symbol
 * serves every VLAN and every flag state.
 */
export function deviceStampAttrs(
  kind: DeviceKind,
  prefix: string,
  o: DeviceStampOptions = {},
): DeviceStampAttrs {
  const sig = o.sig ?? (o.flagged ? SIG_ALARM : SIG_REST)
  return {
    href: `#${deviceSymbolId(prefix, kind)}`,
    transform: `translate(${r2(o.x ?? 0)} ${r2(o.y ?? 0)}) scale(${o.scale ?? 1})`,
    style: `color:${o.ink ?? 'currentColor'};--sig:${sig}`,
  }
}
