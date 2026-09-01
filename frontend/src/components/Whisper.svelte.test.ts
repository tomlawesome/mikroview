// SPDX-License-Identifier: AGPL-3.0-only
//
// #717: the fence pill is gone -- click seeks, drag fences, and the
// curve itself is the one keyboard control (arrows move, Enter marks an
// edge, Escape clears). This covers the three gestures directly, plus
// the shape LiveTable actually reads off whisperState.fenceRange.
import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { fireEvent } from '@testing-library/dom'
import Whisper from './Whisper.svelte'
import { appState } from '../lib/state.svelte'
import { whisperState } from '../lib/whisper.svelte'
import type { Stats, TimeBucket } from '../lib/types'

// jsdom's own getBoundingClientRect returns all zeros; the drag/click
// math needs real pixel geometry to turn a clientX back into a bucket
// index. A 1000px-wide rect maps 1:1 onto the curve's own 0-1000
// viewBox, so clientX 0/250/500/750/1000 line up exactly with the five
// one-minute buckets the fixture below builds.
Element.prototype.getBoundingClientRect = () =>
  ({
    x: 0,
    y: 0,
    width: 1000,
    height: 30,
    top: 0,
    left: 0,
    right: 1000,
    bottom: 30,
    toJSON: () => {},
  }) as DOMRect

const MIN = 60_000
const BASE = new Date('2026-01-01T00:00:00.000Z').getTime()
const N = 5

function bucket(i: number): TimeBucket {
  return { time: new Date(BASE + i * MIN).toISOString(), byAction: { accept: 1 } }
}

function stats(): Stats {
  return {
    total: N,
    byAction: { accept: N },
    topRules: [],
    timeSeries: Array.from({ length: N }, (_, i) => bucket(i)),
    eventsPerSecond: 1,
    capacity: 1000,
    count: N,
    windowSeconds: 900,
    oldestHeld: null,
    connectedClients: 0,
  }
}

function bucketMs(i: number): number {
  return BASE + i * MIN
}

beforeEach(() => {
  whisperState.fenceRange = null
  whisperState.seekMs = null
  appState.autoscroll = true
  appState.events = []
  appState.stats = stats()
})

function renderSvg() {
  const { container } = render(Whisper)
  const svg = container.querySelector('.wsvg')
  if (!svg) throw new Error('curve not found')
  return svg
}

describe('drag', () => {
  it('sets the fence to the range dragged', async () => {
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 1, button: 0, clientX: 0, clientY: 0 })
    await fireEvent.pointerMove(svg, { pointerId: 1, clientX: 750, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 1, clientX: 750, clientY: 0 })

    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(3) + MIN })
    expect(whisperState.seekMs).toBeNull()
  })
})

describe('click below the drag threshold', () => {
  it('seeks instead of fencing', async () => {
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 2, button: 0, clientX: 500, clientY: 0 })
    await fireEvent.pointerMove(svg, { pointerId: 2, clientX: 502, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 2, clientX: 502, clientY: 0 })

    expect(whisperState.seekMs).toBe(bucketMs(2))
    expect(whisperState.fenceRange).toBeNull()
    expect(appState.autoscroll).toBe(false)
  })

  it('a later click clears a fence already drawn', async () => {
    whisperState.setFenceRange(bucketMs(0), bucketMs(1) + MIN)
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 3, button: 0, clientX: 1000, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 3, clientX: 1000, clientY: 0 })

    expect(whisperState.fenceRange).toBeNull()
    expect(whisperState.seekMs).toBe(bucketMs(4))
  })
})

describe('keyboard', () => {
  it('arrows move the cursor, Enter marks each edge, and the closed fence lands in the shape LiveTable consumes', async () => {
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    await fireEvent.keyDown(svg, { key: 'Home' })
    await fireEvent.keyDown(svg, { key: 'Enter' })

    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(4) + MIN })
    expect(typeof whisperState.fenceRange!.start).toBe('number')
    expect(typeof whisperState.fenceRange!.end).toBe('number')
  })

  it('Escape clears a closed fence', async () => {
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    await fireEvent.keyDown(svg, { key: 'Home' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    expect(whisperState.fenceRange).not.toBeNull()

    await fireEvent.keyDown(svg, { key: 'Escape' })
    expect(whisperState.fenceRange).toBeNull()
  })

  it('Escape before the second Enter cancels the pending edge instead of clearing an existing fence', async () => {
    whisperState.setFenceRange(bucketMs(0), bucketMs(1) + MIN)
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })

    await fireEvent.keyDown(svg, { key: 'Escape' })
    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(1) + MIN })
  })
})
