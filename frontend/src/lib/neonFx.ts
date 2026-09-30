// SPDX-License-Identifier: AGPL-3.0-only
//
// The journey's canvas (#1386): a port of round-15's fx.js, the helpers
// an.js draws with. A canvas over the page, sized to the viewport at
// device resolution, inks read from the app's own tokens (never a new
// colour), one requestAnimationFrame loop per journey. The DOM rain
// (Fullfall.svelte) is the door's weather; the canvas takes over only
// for the journey's frames, starting from strokes that look like the
// DOM's (2.5px wide, 13px long, one of the fall's inks).
//
// Ported as drawn (AGENTS.md, "Building a ratified design"); the one
// change is that the canvas element is handed in rather than looked up
// by id, so WizardJourney.svelte owns it.

export type Stroke = {
  x: number
  y: number
  vx: number
  vy: number
  len: number
  w: number
  a: number
  ink: string
  below?: boolean
  tried?: boolean
}

export type Inks = {
  accept: string
  drop: string
  nat: string
  lan: string
  srv: string
  iot: string
  guest: string
  ok: string
  log: string
  now: string
  warn: string
  alarm: string
  fg: string
}

/** readInks reads the journey's palette off :root -- the fall's three
 * first, then the lanes, accept, log, NOW, drop, alarm (DESIGN.md, "The
 * swell"). Every one is an app token already meaning the same thing. */
export function readInks(root: Element = document.documentElement): Inks {
  const css = getComputedStyle(root)
  const tok = (n: string) => css.getPropertyValue(n).trim()
  return {
    accept: tok('--fall-accept'),
    drop: tok('--fall-drop'),
    nat: tok('--fall-nat'),
    lan: tok('--lane-lan'),
    srv: tok('--lane-srv'),
    iot: tok('--lane-iot'),
    guest: tok('--lane-guest'),
    ok: tok('--accept'),
    log: tok('--log'),
    now: tok('--now'),
    warn: tok('--drop'),
    alarm: tok('--alarm'),
    fg: tok('--fg'),
  }
}

/** The fall's own weights: mostly accept, some drop, a little nat. */
export function fallInks(i: Inks): string[] {
  return [i.accept, i.accept, i.accept, i.accept, i.drop, i.drop, i.nat]
}

/** Every ink MikroView owns, for the swell. */
export function allInks(i: Inks): string[] {
  return [i.lan, i.srv, i.iot, i.guest, i.ok, i.log, i.now, i.warn, i.alarm, i.nat, i.accept, i.drop]
}

export const rnd = (a: number, b: number): number => a + Math.random() * (b - a)
export const pick = <T>(arr: T[]): T => arr[(Math.random() * arr.length) | 0]
export const clamp = (v: number, a: number, b: number): number => Math.max(a, Math.min(b, v))
export const smooth = (p: number): number => {
  p = clamp(p, 0, 1)
  return p * p * (3 - 2 * p)
}

export type Canvas = { cx: CanvasRenderingContext2D; W: number; H: number }

/** open sizes the canvas to the viewport at device resolution (capped at
 * 2x) and returns its context. Returns null where there is no 2d context
 * (jsdom), so callers can skip the frames rather than throw. */
export function open(cv: HTMLCanvasElement): Canvas | null {
  const dpr = Math.min(2, window.devicePixelRatio || 1)
  const W = window.innerWidth
  const H = window.innerHeight
  cv.width = W * dpr
  cv.height = H * dpr
  const cx = cv.getContext('2d')
  if (!cx) return null
  cx.setTransform(dpr, 0, 0, dpr, 0, 0)
  cx.lineCap = 'round'
  return { cx, W, H }
}

export function close(c: Canvas): void {
  c.cx.clearRect(0, 0, c.W, c.H)
}

/** a stroke like the DOM rain's */
export function stroke(x: number, y: number, ink: string): Stroke {
  return { x, y, vx: 0, vy: rnd(150, 230), len: 13, w: 2.5, a: rnd(0.3, 0.65), ink }
}

export function drawStroke(cx: CanvasRenderingContext2D, p: Stroke, len: number, w: number, a: number, glow: boolean): void {
  const l = len || p.len
  const n = Math.hypot(p.vx, p.vy) || 1
  const ux = p.vx / n
  const uy = p.vy / n
  if (glow) {
    cx.globalAlpha = Math.min(1, a * 0.28)
    cx.lineWidth = (w || p.w) * 3.2
    cx.strokeStyle = p.ink
    cx.beginPath()
    cx.moveTo(p.x, p.y)
    cx.lineTo(p.x - ux * l, p.y - uy * l)
    cx.stroke()
  }
  cx.globalAlpha = Math.min(1, a)
  cx.lineWidth = w || p.w
  cx.strokeStyle = p.ink
  cx.beginPath()
  cx.moveTo(p.x, p.y)
  cx.lineTo(p.x - ux * l, p.y - uy * l)
  cx.stroke()
}

/** loop runs f(t, dt) each frame until it returns false, then done(). t is
 * milliseconds since the first frame; dt is seconds, capped at 50ms so a
 * hidden tab does not fling everything off screen when it wakes. */
export function loop(f: (t: number, dt: number) => boolean, done?: () => void): void {
  let last = performance.now()
  const t0 = last
  const step = (now: number) => {
    const dt = Math.min(0.05, (now - last) / 1000)
    last = now
    if (f(now - t0, dt) !== false) requestAnimationFrame(step)
    else done?.()
  }
  requestAnimationFrame(step)
}
