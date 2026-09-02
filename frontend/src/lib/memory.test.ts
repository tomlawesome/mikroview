// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  TRACK_X0,
  bufferRow,
  bytesAtX,
  ceilingCaption,
  describeProposal,
  doublingTicks,
  formatEvents,
  formatHours,
  formatSize,
  midLabel,
  pageStepBytes,
  proposalKind,
  stepBytes,
  trackX,
} from './memory'

const MIB = 1024 * 1024
const MIN = 32 * MIB
const MAX = 3584 * MIB // the 3.5 GiB host round 39 draws
const BYTES_PER_EVENT = 624

// The rate that makes round 39's story internally consistent: 120 MiB
// holds ~201,649 events, which the drawing calls ~9 h.
const RATE = 201649 / (9 * 3600)

// Round 39 is the specification, so these assert its own numbers rather
// than whatever the code happens to produce. If the drawing and the
// build ever disagree, that is a defect (AGENTS.md, "Building a
// ratified design"), and this is where it shows.
describe('round 39 draws these exact figures', () => {
  it('places the handle where the drawing places it', () => {
    // The drawing's own x coordinates: 148 at rest, 295 for the grow,
    // 82 for the shrink.
    expect(Math.round(trackX(120 * MIB, MIN, MAX))).toBe(148)
    expect(Math.round(trackX(480 * MIB, MIN, MAX))).toBe(295)
    expect(Math.round(trackX(MIN, MIN, MAX))).toBe(TRACK_X0)
    // 64 MiB lands on 81.45 and the drawing writes 82 -- the same point
    // taken to the nearest whole unit the other way, half a unit on a
    // 520-wide viewBox. Held to a unit rather than to the integer, so
    // the assertion is about the scale and not about a rounding choice.
    expect(Math.abs(trackX(64 * MIB, MIN, MAX) - 82)).toBeLessThan(1)
  })

  it('marks the same doublings, and labels the same middle figure', () => {
    expect(doublingTicks(MIN, MAX).map((b) => b / MIB)).toEqual([64, 128, 256, 512, 1024, 2048])
    expect(midLabel(MIN, MAX)).toBe(512 * MIB)
    // And the drawing puts that label at x=302.
    expect(Math.round(trackX(512 * MIB, MIN, MAX))).toBe(302)
  })

  it('writes the sizes the way the drawing writes them', () => {
    expect(formatSize(MIN)).toBe('32 MiB')
    expect(formatSize(120 * MIB)).toBe('120 MiB')
    expect(formatSize(480 * MIB)).toBe('480 MiB')
    expect(formatSize(MAX)).toBe('3.5 GiB')
    expect(formatSize(2 * 1024 * MIB)).toBe('2 GiB')
  })

  it('writes the event counts the way the drawing writes them', () => {
    expect(formatEvents(201649)).toBe('201 000')
    expect(formatEvents(806597)).toBe('806 000')
    expect(formatEvents(107546)).toBe('107 000')
  })

  it('writes the hours the way the drawing writes them', () => {
    expect(formatHours(9)).toBe('9 h')
    expect(formatHours(36)).toBe('36 h')
    expect(formatHours(4.8)).toBe('4.8 h')
  })

  // Below an hour it changes unit rather than rounding to "0 h". Round
  // 39 never draws a span this short, but a 32 MiB buffer on a busy
  // instance is one, and "holds ~0 h at today's rate" is a different
  // claim from "holds half an hour", not a rounding of it. Found on the
  // first live run of live-memory-slider.mjs.
  it('does not round half an hour down to nothing', () => {
    expect(formatHours(0.5)).toBe('30 min')
    expect(formatHours(0.9)).toBe('54 min')
    expect(formatHours(0.01)).toBe('36 s')
    expect(formatHours(1)).toBe('1 h')
  })

  it('builds the row the drawing prints', () => {
    expect(bufferRow(120 * MIB, BYTES_PER_EVENT, RATE)).toBe(
      '120 MiB · ~201 000 events · ~9 h at today’s rate'.replace('’', "'"),
    )
    expect(bufferRow(480 * MIB, BYTES_PER_EVENT, RATE)).toBe(
      "480 MiB · ~806 000 events · ~36 h at today's rate",
    )
    expect(bufferRow(64 * MIB, BYTES_PER_EVENT, RATE)).toBe(
      "64 MiB · ~107 000 events · ~4.8 h at today's rate",
    )
  })

  it('says the consequence of the grow the drawing draws', () => {
    const p = describeProposal({
      proposed: 480 * MIB,
      current: 120 * MIB,
      bytesPerEvent: BYTES_PER_EVENT,
      eventsPerSecond: RATE,
      count: 201649, // the 120 MiB ring, full
      now: Date.parse('2026-09-02T14:04:00Z'),
    })
    expect(p.kind).toBe('grow')
    expect(p.sentence).toBe("480 MiB would hold ~36 h at today's rate, filling over the next 27 h")
    expect(p.newOldest).toBeNull()
  })

  it('says the consequence of the shrink the drawing draws, and where the cut falls', () => {
    const now = Date.parse('2026-09-02T14:04:00Z')
    const p = describeProposal({
      proposed: 64 * MIB,
      current: 120 * MIB,
      bytesPerEvent: BYTES_PER_EVENT,
      eventsPerSecond: RATE,
      count: 201649,
      now,
    })
    expect(p.kind).toBe('shrink')
    // 64 MiB holds 4.80 h, so the cut lands 4.80 h before now.
    expect(p.newOldest).not.toBeNull()
    expect((now - (p.newOldest as number)) / 3_600_000).toBeCloseTo(4.8, 1)
    expect(p.sentence).toMatch(/^64 MiB holds ~4\.8 h at today's rate — everything before \d\d:\d\d lets go$/)
  })
})

describe('dragging', () => {
  it('round-trips a position back to the figure it shows', () => {
    for (const mib of [32, 64, 120, 480, 1024]) {
      const x = trackX(mib * MIB, MIN, MAX)
      expect(bytesAtX(x, MIN, MAX) / MIB).toBe(mib)
    }
  })

  it('never proposes anything outside the range, however far the mouse goes', () => {
    expect(bytesAtX(-500, MIN, MAX)).toBe(MIN)
    expect(bytesAtX(9999, MIN, MAX)).toBe(MAX)
  })

  it('snaps, so two drags to the same place give the same figure', () => {
    const a = bytesAtX(200.4, MIN, MAX)
    const b = bytesAtX(200.6, MIN, MAX)
    expect(a).toBe(b)
    expect(a % (8 * MIB)).toBe(0)
  })

  it('moves one snap step per arrow key, in both directions', () => {
    expect(stepBytes(120 * MIB, 1, MIN, MAX)).toBe(128 * MIB)
    expect(stepBytes(120 * MIB, -1, MIN, MAX)).toBe(112 * MIB)
    expect(stepBytes(MIN, -1, MIN, MAX)).toBe(MIN)
    expect(stepBytes(MAX, 1, MIN, MAX)).toBe(MAX)
  })

  // The trap this replaced: an arrow key that moved by a fraction of a
  // doubling stepped from 120 MiB straight over 480 to 504, so a
  // keyboard user could not reach a figure the mouse could -- two
  // different sets of legal values for one control.
  it('can reach every figure a drag can reach, including 480 MiB', () => {
    let v = 120 * MIB
    const seen = new Set<number>()
    for (let i = 0; i < 200 && v < 480 * MIB; i++) {
      v = stepBytes(v, 1, MIN, MAX)
      seen.add(v)
    }
    expect(seen.has(480 * MIB)).toBe(true)
  })

  it('crosses the range in a handful of page steps', () => {
    expect(pageStepBytes(120 * MIB, 1, MIN, MAX)).toBe(240 * MIB)
    expect(pageStepBytes(240 * MIB, 1, MIN, MAX)).toBe(480 * MIB)
    // Halving 120 gives 60, which is not on the 8 MiB grid a drag can
    // land on, so it snaps to 64 -- a page step still lands somewhere a
    // drag could have.
    expect(pageStepBytes(120 * MIB, -1, MIN, MAX)).toBe(64 * MIB)
    expect(pageStepBytes(MIN, -1, MIN, MAX)).toBe(MIN)
    expect(pageStepBytes(MAX, 1, MIN, MAX)).toBe(MAX)
  })
})

describe('honesty about what is not known', () => {
  it('does not claim a reach before any traffic has arrived', () => {
    expect(bufferRow(120 * MIB, BYTES_PER_EVENT, 0)).toBe(
      '120 MiB · ~201 000 events · no rate to reckon from yet',
    )
    const p = describeProposal({
      proposed: 480 * MIB,
      current: 120 * MIB,
      bytesPerEvent: BYTES_PER_EVENT,
      eventsPerSecond: 0,
      count: 0,
      now: Date.now(),
    })
    expect(p.sentence).toContain('nothing has arrived yet')
  })

  it('does not invent a loss when a shrink would evict nothing', () => {
    const p = describeProposal({
      proposed: 64 * MIB,
      current: 120 * MIB,
      bytesPerEvent: BYTES_PER_EVENT,
      eventsPerSecond: RATE,
      count: 500, // nowhere near the 107,546 that 64 MiB holds
      now: Date.now(),
    })
    expect(p.sentence).toBe("64 MiB holds ~4.8 h at today's rate — nothing held falls away")
    expect(p.newOldest).toBeNull()
  })

  it('does not claim to know what the host has when the server could not read it', () => {
    expect(ceilingCaption(MAX, 8 * 1024 * MIB)).toBe('3.5 GiB — all this host can spare')
    expect(ceilingCaption(1024 * MIB, 0)).toBe(
      "1 GiB — a safe default; this host's memory could not be read",
    )
  })

  it('has nothing to say about a proposal that changes nothing', () => {
    expect(proposalKind(120 * MIB, 120 * MIB)).toBe('rest')
    const p = describeProposal({
      proposed: 120 * MIB,
      current: 120 * MIB,
      bytesPerEvent: BYTES_PER_EVENT,
      eventsPerSecond: RATE,
      count: 100,
      now: Date.now(),
    })
    expect(p.sentence).toBe('')
  })
})
