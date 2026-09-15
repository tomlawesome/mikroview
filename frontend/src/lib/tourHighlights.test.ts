// SPDX-License-Identifier: AGPL-3.0-only
//
// #1215: the tour ring's three shapes -- traces a box exactly, stands
// off anything else with a clear margin, and (independently of either)
// pads a hairline up to a visible band (#750). jsdom's own
// getBoundingClientRect() is always zero (JourneyTour.rings.test.ts's
// note), so fitRing is tested here as a plain function against real
// numbers rather than through a mounted component.

import { describe, expect, it } from 'vitest'
import { fitRing, HAIRLINE_MIN_PX, RING_MARGIN_PX, TOUR_HIGHLIGHTS } from './tourHighlights'

describe('fitRing (#1215)', () => {
  it('traces a box target exactly -- no margin, no hairline pad', () => {
    const rect = { top: 100, left: 200, width: 256, height: 68 }
    expect(fitRing(rect, true)).toEqual(rect)
  })

  it('stands a non-box target off with a clear margin all round', () => {
    const rect = { top: 100, left: 200, width: 256, height: 68 }
    expect(fitRing(rect, false)).toEqual({
      top: 100 - RING_MARGIN_PX,
      left: 200 - RING_MARGIN_PX,
      width: 256 + RING_MARGIN_PX * 2,
      height: 68 + RING_MARGIN_PX * 2,
    })
  })

  it('pads a hairline (#750) up to a visible band, then stands it off (non-box)', () => {
    // The fall's now line: full width, ~1.6px tall.
    const rect = { top: 300, left: 40, width: 900, height: 1.6 }
    const fitted = fitRing(rect, false)

    // Hairline pad centres the band on the original line first.
    const paddedTop = 300 - (HAIRLINE_MIN_PX - 1.6) / 2
    expect(fitted.height).toBe(HAIRLINE_MIN_PX + RING_MARGIN_PX * 2)
    expect(fitted.top).toBe(paddedTop - RING_MARGIN_PX)
    // Width was already above the hairline floor, so only the margin
    // touches it.
    expect(fitted.width).toBe(900 + RING_MARGIN_PX * 2)
    expect(fitted.left).toBe(40 - RING_MARGIN_PX)
  })

  it('hairline pad applies to a box target too, independently of the margin', () => {
    // A box thinner than the hairline floor: still gets padded to a
    // visible band (#750 item 4 stays regardless of shape), but a box
    // never gets the standoff margin (item 3).
    const rect = { top: 50, left: 10, width: 256, height: 10 }
    const fitted = fitRing(rect, true)

    expect(fitted.height).toBe(HAIRLINE_MIN_PX)
    expect(fitted.top).toBe(50 - (HAIRLINE_MIN_PX - 10) / 2)
    expect(fitted.width).toBe(256)
    expect(fitted.left).toBe(10)
  })
})

// Guards the #1215 item 3 data attribute itself: exactly the entries
// that are genuinely drawn as their own box (their own fill/stroke, a
// bordered card, a field) carry `box: true`. A highlight added later
// without thinking about its shape defaults to false (stands off with
// a margin), which is the safer default for something that is not
// visibly a box.
describe('TOUR_HIGHLIGHTS box classification (#1215 item 3)', () => {
  it('marks only the topography waist -- an SVG rect with its own fill and stroke -- as a box', () => {
    const boxed = Object.entries(TOUR_HIGHLIGHTS).flatMap(([cardKey, list]) =>
      list.filter((h) => h.box === true).map((h) => `${cardKey}: ${h.label}`),
    )
    expect(boxed).toEqual(['topography: the router as the waist — subnets below, the internet above'])
  })
})

// #1235: every stop carries a sentence, and each keeps to the same
// rules -- the tour is the one place the app explains itself, so a stop
// added later without its sentence is a failure here, not a blank line
// in the bar. The rules are the issue's own: one sentence, under ~90
// characters so the bar stays one line per stop on a laptop, a full
// stop and no exclamation, and none of the filler the app's voice
// avoids ("you can", "this is where").
describe('TOUR_HIGHLIGHTS sentences (#1235)', () => {
  const stops = Object.entries(TOUR_HIGHLIGHTS).flatMap(([cardKey, list]) =>
    list.map((h) => ({ name: `${cardKey}: ${h.label}`, says: h.says })),
  )

  it('covers every stop', () => {
    expect(stops.length).toBeGreaterThan(0)
    for (const s of stops) {
      expect(typeof s.says, s.name).toBe('string')
      expect(s.says.trim().length, s.name).toBeGreaterThan(0)
    }
  })

  it('is one plain sentence per stop: under 90 characters, a full stop, no exclamation', () => {
    for (const s of stops) {
      expect(s.says.length, s.name).toBeLessThan(90)
      expect(s.says.endsWith('.'), s.name).toBe(true)
      expect(s.says.includes('!'), s.name).toBe(false)
      // A second full stop would be a second sentence.
      expect(s.says.slice(0, -1).includes('.'), s.name).toBe(false)
    }
  })

  it('keeps to the voice: no "you can", no "this is where"', () => {
    for (const s of stops) {
      const lower = s.says.toLowerCase()
      expect(lower.includes('you can'), s.name).toBe(false)
      expect(lower.includes('this is where'), s.name).toBe(false)
    }
  })
})
