// SPDX-License-Identifier: AGPL-3.0-only
//
// The metrics record's "sharp" clause (#488): "marks are filled paths
// on a whole-pixel grid; axes, baselines and rules use crisp edges so
// hairlines land on device pixels; redrawn on DPR change, never
// upscaled from a raster."
//
// Two halves, both needed:
//
//  - The drawing surfaces size their SVG in real CSS pixels (width and
//    height attributes equal to the measured box, viewBox the same) so
//    one unit is one CSS pixel and nothing is stretched by a viewBox.
//    That alone still leaves a hairline straddling two device pixels on
//    a fractional DPR, which is what makes a 1px rule look like a 2px
//    grey smear.
//  - So coordinates go through the helpers below. A filled mark snaps
//    to a whole device pixel; a stroked hairline snaps to the *middle*
//    of one, because a 1-unit stroke is drawn half either side of its
//    coordinate.
//
// The DPR itself is watched rather than read once: moving a window
// between a laptop screen and an external monitor changes it with no
// resize event on some platforms, and a drum snapped to the old grid
// stays soft until something else happens to redraw it.

function readDpr(): number {
  try {
    const v = window.devicePixelRatio
    return typeof v === 'number' && v > 0 ? v : 1
  } catch {
    return 1
  }
}

/** Snap a filled mark's coordinate to a whole device pixel. */
export function snapFill(v: number, dpr: number): number {
  const r = dpr > 0 ? dpr : 1
  return Math.round(v * r) / r
}

/**
 * Snap a 1-unit stroke's coordinate to the centre of a device pixel, so
 * it paints as one crisp line instead of two half-covered rows.
 */
export function snapLine(v: number, dpr: number): number {
  const r = dpr > 0 ? dpr : 1
  return (Math.round(v * r) + 0.5) / r
}

class DprWatcher {
  value = $state(readDpr())

  constructor() {
    this.#watch()
  }

  // matchMedia on the *current* ratio: it stops matching the moment the
  // ratio changes, which is the only cross-browser notification there
  // is. Re-armed each time, since each query only ever fires once.
  #watch() {
    try {
      const mq = window.matchMedia(`(resolution: ${this.value}dppx)`)
      mq.addEventListener(
        'change',
        () => {
          this.value = readDpr()
          this.#watch()
        },
        { once: true },
      )
    } catch {
      // No matchMedia (jsdom, an old browser): the surfaces still draw,
      // just against the ratio read at load.
    }
  }
}

export const dprState = new DprWatcher()
