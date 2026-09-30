// SPDX-License-Identifier: AGPL-3.0-only
//
// The way in, and the way out (#1386): round 15's AN without sparks,
// ported from docs/design/screens/wizard/round-15/an.js as drawn. The
// wordmark's box is the thing you watch -- a neon sign in the rain, and
// everything that happens to it is what happens to a tube:
//
//   slide   you let go and slide down (the welcome goes up and away; the
//           rain streaks)
//   swell   the rain thickens and takes every ink MikroView owns -- no
//           white wash
//   sign    the box moves to the centre, grows by half; as the swell
//           comes on the sign strikes -- every letter stutters alight,
//           one after another -- and from then on a letter now and then
//           fails like a broken tube: a buzz, a stutter, a drop-out and a
//           flash back, in whatever ink the rain is wearing. The border
//           wobbles, glides through the rain's inks, and hums
//   line    a line grows from each side of the box: the tube extending, a
//           bright head of current running out to the edges; when it gets
//           there the whole line strikes
//   catch   drops meeting the line are caught or slip by -- porous at
//           first, then total. No sparks (owner, round 15)
//   wipe    the line rises with all it holds; the box rides up and becomes
//           the bar's wordmark; the line goes off the top
//   groups  the wizard (or the fall) arrives in groups, each striking on
//           like a tube
//
// Every colour is an app token read off :root (neonFx.ts); the border,
// the line, the current and the beads are the canvas, the letters are
// the DOM. Where the prototype set a letter's colour and text-shadow
// inline, this sets a class and an --ink custom property (DESIGN.md,
// "Build notes"; the CSP allows CSSOM writes, not style attributes).

import { allInks, clamp, close, drawStroke, fallInks, loop, open, pick, readInks, rnd, smooth, stroke, type Stroke } from './neonFx'

const cubic = (p: number) => (p < 0.5 ? 4 * p * p * p : 1 - Math.pow(-2 * p + 2, 3) / 2)
const easeOut = (p: number) => 1 - Math.pow(1 - p, 3)

/* colours: tokens are hex; the border glides between inks rather than snapping */
export function rgb(h: string): [number, number, number] {
  h = h.replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  const v = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16) || 0)
  return [v[0], v[1], v[2]]
}
export const mix = (a: number[], b: number[], q: number): string => `rgb(${a.map((v, i) => Math.round(v + (b[i] - v) * q)).join(',')})`

/* ---- the tube: how a letter fails ---- */
export type LetterState = 'lit' | 'dark' | 'plain'
type Step = { at: number; st: LetterState; ink: string }
type Letter = HTMLElement & { _seq?: Step[] | null; _cur?: Step | null }

function lit(L: HTMLElement, ink: string) {
  L.classList.remove('dark')
  L.classList.add('lit')
  L.style.setProperty('--ink', ink)
}
function dark(L: HTMLElement) {
  L.classList.remove('lit')
  L.classList.add('dark')
}
function plain(L: HTMLElement) {
  L.classList.remove('lit', 'dark')
  L.style.removeProperty('--ink')
}

/* a pattern is a list of [ms, state]; state is 'lit', 'dark' or 'plain' */
export const patterns: Record<string, () => [number, LetterState][]> = {
  buzz: () => {
    const out: [number, LetterState][] = []
    const n = 5 + ((Math.random() * 6) | 0)
    for (let i = 0; i < n; i++) out.push([rnd(18, 42), i % 2 ? 'dark' : 'lit'])
    out.push([0, 'plain'])
    return out
  },
  stutter: () => [[50, 'lit'], [35, 'dark'], [70, 'lit'], [30, 'dark'], [rnd(200, 320), 'lit'], [0, 'plain']],
  dropout: () => [[rnd(140, 320), 'dark'], [45, 'lit'], [25, 'dark'], [rnd(120, 220), 'lit'], [0, 'plain']],
  hold: () => [[rnd(160, 300), 'lit'], [0, 'plain']],
}
const PICK = ['buzz', 'stutter', 'dropout', 'hold', 'buzz', 'stutter']

function schedule(L: Letter, ink: string, kind: string, t: number, delay = 0) {
  let at = t + delay
  L._seq = patterns[kind]().map(([ms, st]) => {
    const step = { at, st, ink }
    at += ms
    return step
  })
  L._cur = null
}
function runLetters(letters: Letter[], t: number) {
  for (const L of letters) {
    if (!L._seq) continue
    let step: Step | null = null
    while (L._seq.length && L._seq[0].at <= t) step = L._seq.shift()!
    if (step && step !== L._cur) {
      L._cur = step
      step.st === 'lit' ? lit(L, step.ink) : step.st === 'dark' ? dark(L) : plain(L)
    }
    if (!L._seq.length && L._cur && L._cur.st === 'plain') {
      L._seq = null
      L._cur = null
    }
  }
}

/** The riding box: a copy of the wordmark in a fixed element the caller
 * has already rendered (WizardJourney.svelte), with its letters. `from`
 * is the rect the wordmark starts on. */
export type Ride = {
  el: HTMLElement
  w: number
  h: number
  wmW: number
  s0: number
  c0: { x: number; y: number }
  letters: Letter[]
  put: (cx: number, cy: number, s: number) => { cx: number; cy: number; hw: number; hh: number }
}
export function makeRide(el: HTMLElement, from: DOMRect): Ride {
  const w = el.offsetWidth
  const h = el.offsetHeight
  const wm = el.querySelector('.wm') as HTMLElement
  const wmW = wm.offsetWidth || 1
  const s0 = from.width / wmW
  const c0 = { x: from.left + from.width / 2, y: from.top + from.height / 2 }
  const letters = [...el.querySelectorAll<HTMLElement>('.l')] as Letter[]
  const put = (cx: number, cy: number, s: number) => {
    el.style.transform = `translate(${cx - w / 2}px, ${cy - h / 2}px) scale(${s})`
    return { cx, cy, hw: (w * s) / 2, hh: (h * s) / 2 }
  }
  return { el, w, h, wmW, s0, c0, letters, put }
}

export type JourneyOptions = {
  /** the canvas to draw on, already in the DOM */
  canvas: HTMLCanvasElement
  /** the riding box element, already in the DOM (aria-hidden) */
  ride: HTMLElement
  /** where the wordmark starts (the door's box, or the bar's) */
  from: DOMRect
  /** where it lands (the bar's wordmark) */
  to: DOMRect
  /** how far the world slides */
  D: number
  /** 1 on the way in; the way out plays at three-quarters speed */
  scale?: number
  slide: (y: number) => void
  /** the page under the cover is swapped here (k >= 0.85) */
  swap: () => void
  /** [ms, fn]: the groups, on the journey's clock */
  groups: [number, () => void][]
  /** the box has become the bar's wordmark */
  landed: () => void
  done: () => void
}

/** journey plays the six beats. It returns false where there is no
 * canvas context (jsdom); the caller then takes the reduced path. */
export function journey(o: JourneyOptions): boolean {
  const c = open(o.canvas)
  if (!c) return false
  const { cx, W: CW, H: CH } = c
  const INKS = readInks()
  const FALL = fallInks(INKS)
  const ALL = allInks(INKS)
  const AMBER = rgb(INKS.now)
  const cl = clamp
  const sm = smooth
  const k_ = o.scale || 1
  const T = (ms: number) => ms * k_
  const SLIDE_END = T(2600),
    SWELL_ON = T(1100),
    LINE_ON = T(2500),
    LINE_FULL = T(3100),
    WIPE_ON = T(3700),
    WIPE_END = T(4600),
    RAIN_OFF = T(4500),
    BOX_GO = T(4050),
    BOX_END = T(4800)
  const box = makeRide(o.ride, o.from)
  let rect = box.put(box.c0.x, box.c0.y, box.s0)
  const target = o.to
  const sT = target.width / box.wmW
  const tC = { x: target.left + target.width / 2, y: target.top + target.height / 2 }
  const ps: Stroke[] = []
  for (let i = 0; i < 70; i++) ps.push(stroke(rnd(0, CW), rnd(0, CH), pick(FALL)))
  const pool: { x: number; ink: string; born: number; w: number }[] = []
  let lineY = CH / 2,
    lineL = 0,
    lineR = 0,
    lineOn = false,
    lineStruck = 0,
    ground = 0,
    swapped = false,
    lastY = 0,
    signLit = false
  let inkA: number[] = AMBER,
    inkB: number[] = AMBER,
    inkT = 0,
    flickerAt = 0,
    dipUntil = 0,
    dipNext = 0
  const wobble = (u: number, t: number, amp: number) =>
    Math.sin(u * 6.3 + t * 0.0021) * amp + Math.sin(u * 13.1 - t * 0.0033) * amp * 0.5 + Math.sin(u * 2.2 + t * 0.0012) * amp * 0.7
  loop(
    (t, dt) => {
      /* --- the curves --- */
      const sp = cl(t / SLIDE_END, 0, 1)
      const y = o.D * cubic(sp)
      const v = (y - lastY) / Math.max(dt, 0.001) / CH
      lastY = y
      o.slide(y)
      const k =
        t < SWELL_ON
          ? 0
          : t < SLIDE_END
            ? Math.pow((t - SWELL_ON) / (SLIDE_END - SWELL_ON), 1.6)
            : t < WIPE_ON - T(300)
              ? 1
              : cl(1 - (t - WIPE_ON + T(300)) / (RAIN_OFF - WIPE_ON + T(300)), 0, 1)
      const grow = t < LINE_ON ? 0 : easeOut(cl((t - LINE_ON) / (LINE_FULL - LINE_ON), 0, 1))
      const wipe = t < WIPE_ON ? 0 : cubic(cl((t - WIPE_ON) / (WIPE_END - WIPE_ON), 0, 1))
      const catchRate = cl((t - LINE_ON) / (WIPE_ON - LINE_ON), 0, 1)
      const go = t < BOX_GO ? 0 : cubic(cl((t - BOX_GO) / (BOX_END - BOX_GO), 0, 1))
      /* --- the box --- */
      const bq = easeOut(cl(t / T(800), 0, 1))
      const bcx = box.c0.x + (CW / 2 - box.c0.x) * bq
      const s1 = box.s0 + (1.5 - box.s0) * bq
      const bcyBase = box.c0.y + (CH / 2 - box.c0.y) * bq
      lineY = CH / 2 + (24 - 40 - CH / 2) * wipe
      const hhNow = (box.h * (s1 + (sT - s1) * go)) / 2
      const bcy = wipe > 0 ? Math.max(lineY, 10 + hhNow) : bcyBase
      rect = box.put(bcx + (tC.x - bcx) * go, bcy + (tC.y - bcy) * go, s1 + (sT - s1) * go)
      /* the tube's ink: amber until the swell, then gliding through the rain's colours */
      if (k > 0.05 && t >= inkT) {
        inkA = inkB
        inkB = rgb(pick(k > 0.3 ? ALL : FALL))
        inkT = t + rnd(380, 720)
      }
      const iq = inkT ? 1 - cl((inkT - t) / 550, 0, 1) : 0
      const ink = k > 0.05 || go > 0 ? mix(inkA, inkB, sm(iq)) : mix(AMBER, AMBER, 0)
      /* --- the rain --- */
      const rising = t < WIPE_ON - T(300)
      const streak = cl(v * 0.9, 0, 5)
      const N = 70 + Math.round(k * 2300)
      const spd = 1 + k * 5.5 + streak * 0.8
      const len = 13 + k * 62 + streak * 14
      const wid = 2.5 + k * 1.4
      const al = 0.42 + k * 0.4
      const wide = cl((k - 0.15) / 0.5, 0, 1)
      const want = rising ? N : Math.round(k * 2300)
      while (ps.length < want) {
        const p = stroke(rnd(-40, CW + 40), rising && k < 0.9 ? rnd(-CH * 0.4, CH * 0.6) : rnd(-CH * 0.3, -10), pick(FALL))
        if (Math.random() < wide) p.ink = pick(ALL)
        p.vx = rnd(-14, 14)
        ps.push(p)
      }
      if (grow > 0) {
        const hw = rect.hw + 4
        lineL = (rect.cx - hw) * (1 - grow) - 20 * grow
        lineR = rect.cx + hw + (CW + 20 - rect.cx - hw) * grow
        if (!lineOn) {
          lineOn = true
          for (const p of ps) if (p.y > lineY) p.below = true
        }
        if (grow >= 1 && !lineStruck) lineStruck = t
      }
      for (let i = ps.length - 1; i >= 0; i--) {
        const p = ps[i]
        p.y += p.vy * spd * dt
        p.x += p.vx * dt
        if (lineOn && !p.below && p.y >= lineY && p.x >= lineL && p.x <= lineR) {
          if (p.tried === undefined) {
            p.tried = true
            if (Math.random() < 0.08 + 0.92 * Math.pow(catchRate, 1.4) || wipe > 0) {
              pool.push({ x: p.x, ink: p.ink, born: t, w: p.w })
              ps.splice(i, 1)
              continue
            } else p.below = true
          }
        }
        if (p.y - len > CH + 10) {
          if (ps.length <= want) {
            p.y = rnd(-CH * 0.3, -10)
            p.x = rnd(-40, CW + 40)
            p.below = false
            p.tried = undefined
          } else ps.splice(i, 1)
        }
      }
      /* --- paint --- */
      cx.globalCompositeOperation = 'source-over'
      cx.clearRect(0, 0, CW, CH)
      ground = rising ? Math.max(ground, cl(k * 1.7, 0, 1)) : ground
      if (ground > 0) {
        cx.fillStyle = '#06080e'
        if (!lineOn) {
          cx.globalAlpha = ground
          cx.fillRect(0, 0, CW, CH)
        } else {
          cx.globalAlpha = ground
          cx.fillRect(0, 0, CW, Math.max(0, lineY))
          const below = ground * (1 - sm((t - LINE_FULL) / T(900)))
          if (below > 0.01) {
            cx.globalAlpha = below
            cx.fillRect(0, Math.max(0, lineY), CW, CH)
          }
        }
      }
      cx.globalCompositeOperation = 'lighter'
      for (const p of ps) drawStroke(cx, p, len * (0.7 + p.a * 0.6), wid, al * (0.55 + p.a * 0.7), k > 0.35 && k < 0.85)
      /* the hum: now and then the tube dips for a few frames */
      if (t > dipNext) {
        dipUntil = t + rnd(25, 70)
        dipNext = dipUntil + rnd(250, 900) * (k > 0.1 ? 1 : 3)
      }
      const hum = (t < dipUntil ? 0.45 : 1) * (0.96 + Math.random() * 0.04)
      const amp = (0.6 + k * 3.2) * (1 - go)
      cx.globalCompositeOperation = 'source-over'
      cx.strokeStyle = ink
      cx.lineJoin = 'round'
      cx.lineCap = 'round'
      const drawBox = (a: number, lw: number, inf: number) => {
        cx.globalAlpha = a
        cx.lineWidth = lw
        cx.beginPath()
        const L = rect.cx - rect.hw - inf,
          R = rect.cx + rect.hw + inf,
          Tp = rect.cy - rect.hh - inf,
          B = rect.cy + rect.hh + inf
        const n = 14
        for (let i = 0; i <= n; i++) {
          const u = i / n
          const px = L + (R - L) * u
          const py = Tp + wobble(u, t, amp)
          i ? cx.lineTo(px, py) : cx.moveTo(px, py)
        }
        for (let i = 1; i <= n; i++) {
          const u = i / n
          cx.lineTo(R + wobble(u + 1, t, amp), Tp + (B - Tp) * u)
        }
        for (let i = 1; i <= n; i++) {
          const u = i / n
          cx.lineTo(R - (R - L) * u, B + wobble(u + 2, t, amp))
        }
        for (let i = 1; i <= n; i++) {
          const u = i / n
          cx.lineTo(L + wobble(u + 3, t, amp), B - (B - Tp) * u)
        }
        cx.closePath()
        cx.stroke()
      }
      const boxA = (1 - sm((go - 0.55) / 0.45)) * hum
      if (boxA > 0) {
        drawBox(boxA * 0.35, 6 + k * 6, 4)
        drawBox(boxA, 1.5 + k * 0.8, 4)
      }
      if (lineOn && lineY > -30) {
        const wet = cl(pool.length / 120, 0, 1)
        let lineA = 1 - sm((t - WIPE_END) / T(300))
        /* the strike: when the current reaches both edges the whole tube flickers on */
        if (lineStruck) {
          const u = (t - lineStruck) / T(340)
          if (u < 1) lineA *= u < 0.15 ? 1 : u < 0.25 ? 0.2 : u < 0.4 ? 1 : u < 0.5 ? 0.4 : 1
        }
        lineA *= hum
        const seg = (x0: number, x1: number, lw: number, a: number) => {
          if (x1 <= x0) return
          cx.globalAlpha = a * lineA
          cx.lineWidth = lw
          cx.beginPath()
          for (let x = x0; x <= x1; x += 24) {
            const py = lineY + wobble((x / CW) * 3, t, amp * 0.5)
            x === x0 ? cx.moveTo(x, py) : cx.lineTo(x, py)
          }
          cx.lineTo(x1, lineY + wobble((x1 / CW) * 3, t, amp * 0.5))
          cx.stroke()
        }
        const whole = wipe > 0 && bcy > lineY + rect.hh
        const gapL = whole ? lineR : rect.cx - rect.hw - 4
        const gapR = whole ? lineR : rect.cx + rect.hw + 4
        cx.strokeStyle = ink
        seg(lineL, gapL, 5 + wet * 8, 0.18 + wet * 0.25)
        seg(gapR, lineR, 5 + wet * 8, 0.18 + wet * 0.25)
        seg(lineL, gapL, 1.6, 0.95)
        seg(gapR, lineR, 1.6, 0.95)
        /* the current: while the tube is still extending, a bright head runs out at each end */
        if (grow > 0 && grow < 1) {
          cx.globalCompositeOperation = 'lighter'
          for (const hx of [lineL, lineR]) {
            const g = cx.createRadialGradient(hx, lineY, 0, hx, lineY, 22)
            g.addColorStop(0, '#fff7e6')
            g.addColorStop(0.35, ink)
            g.addColorStop(1, 'rgba(0,0,0,0)')
            cx.fillStyle = g
            cx.globalAlpha = 0.95
            cx.beginPath()
            cx.arc(hx, lineY, 22, 0, 6.29)
            cx.fill()
          }
          cx.globalCompositeOperation = 'source-over'
        }
        /* what the line has caught: each drop a bead on it, bright when it lands, then part of the wet */
        cx.globalCompositeOperation = 'lighter'
        for (let i = pool.length - 1; i >= 0; i--) {
          const d = pool[i]
          const age = (t - d.born) / T(1100)
          if (age > 1 && pool.length > 900) {
            pool.splice(i, 1)
            continue
          }
          const a = age < 1 ? 0.95 - 0.55 * age : 0.4
          cx.globalAlpha = a * lineA
          cx.fillStyle = d.ink
          const r = 1.6 + Math.min(1, age) * 1.6
          cx.beginPath()
          cx.ellipse(d.x, lineY - 1 + wobble((d.x / CW) * 3, t, amp * 0.5), r * 1.6, r, 0, 0, 6.29)
          cx.fill()
        }
      }
      /* --- the letters --- */
      if (!signLit && k > 0.08) {
        signLit = true
        box.letters.forEach((L, i) => schedule(L, INKS.now, 'stutter', t, i * 55 + rnd(0, 40)))
      } // the sign strikes, letter by letter
      if (signLit && k > 0.1 && go < 0.6 && t >= flickerAt) {
        flickerAt = t + rnd(110, 300)
        const i = (Math.random() * box.letters.length) | 0
        const L = box.letters[i]
        if (!L._seq) {
          const inkL = pick(k > 0.3 ? ALL : FALL)
          const kind = pick(PICK)
          schedule(L, inkL, kind, t)
          const N2 = box.letters[i + 1]
          if (N2 && !N2._seq && Math.random() < 0.3) schedule(N2, inkL, kind, t, rnd(10, 40))
        }
      }
      runLetters(box.letters, t)
      if (go >= 0.6)
        box.letters.forEach((L) => {
          if (L._seq) {
            L._seq = null
            L._cur = null
            plain(L)
          }
        })
      if (!swapped && k >= 0.85 && t > SLIDE_END - T(400)) {
        swapped = true
        o.swap()
      }
      if (t >= BOX_END && !box.el.classList.contains('landed')) {
        box.el.classList.add('landed')
        o.landed()
      }
      return t < T(6500) || (ps.length > 0 && t < WIPE_END + T(2500))
    },
    () => {
      close(c)
      o.done()
    },
  )
  o.groups.forEach(([ms, fn]) => setTimeout(fn, T(ms)))
  return true
}

/** each element strikes on like a tube, one after another. The stagger is
 * an animation-delay written through the CSSOM (allowed under the CSP;
 * a style attribute would not be). */
export function strike(els: HTMLElement[], gap: number): void {
  els.forEach((el, i) => {
    el.style.animationDelay = `${i * gap}ms`
    el.classList.add('strike')
    setTimeout(() => {
      el.style.animationDelay = ''
    }, i * gap + 1000)
  })
}

/** reducedMotion reads the user's preference once per call. */
export function reducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
}
